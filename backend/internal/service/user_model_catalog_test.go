package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type userCatalogGroupsStub struct {
	groups []Group
	err    error
}

func (s *userCatalogGroupsStub) GetAvailableGroups(_ context.Context, userID int64) ([]Group, error) {
	if userID != 7 {
		return nil, errors.New("unexpected user")
	}
	return s.groups, s.err
}

type userCatalogAccountsStub struct {
	AccountRepository
	byGroup map[int64][]Account
	seen    []int64
}

func (s *userCatalogAccountsStub) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, _ []string, includeGrouped bool) ([]Account, error) {
	if groupID == nil || includeGrouped {
		return nil, errors.New("unscoped account scan")
	}
	s.seen = append(s.seen, *groupID)
	return s.byGroup[*groupID], nil
}

type userCatalogSourceStub struct {
	mu     sync.Mutex
	models map[int64][]catalogModel
	errs   map[int64]error
	calls  atomic.Int32
}

func (s *userCatalogSourceStub) FetchCatalog(_ context.Context, a *Account) ([]catalogModel, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.models[a.ID], s.errs[a.ID]
}
func TestUserModelCatalogUnionsAuthorizedAccountsAndAppliesPublicNames(t *testing.T) {
	groups := &userCatalogGroupsStub{groups: []Group{
		{ID: 1, Name: "Codex", Platform: PlatformOpenAI, Status: StatusActive, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6.1-*", "lab-model"}}},
		{ID: 2, Name: "API", Platform: PlatformOpenAI, Status: StatusActive},
	}}
	repo := &userCatalogAccountsStub{byGroup: map[int64][]Account{
		1:  {{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"lab-model": "upstream-private", "gpt-6.1-terra": "gpt-6.1-terra"}}}},
		2:  {{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}, {ID: 13, Platform: PlatformOpenAI, Status: StatusDisabled, Schedulable: true}},
		99: {{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}},
	}}
	source := &userCatalogSourceStub{models: map[int64][]catalogModel{
		11: {{ID: "gpt-6.1-terra"}, {ID: "upstream-private"}, {ID: "gpt-5.5"}},
		12: {{ID: "gpt-6.1-terra"}, {ID: "brand-new-model-2027"}},
		13: {{ID: "disabled-secret"}}, 99: {{ID: "private-secret"}},
	}}
	s := &UserModelCatalogService{groups: groups, accounts: repo, source: source}
	result, err := s.List(context.Background(), 7)
	require.NoError(t, err)
	require.False(t, result.Partial)
	require.Equal(t, []int64{1, 2}, repo.seen)
	require.Equal(t, int32(2), source.calls.Load())
	require.Len(t, result.Models, 3)
	require.Equal(t, "brand-new-model-2027", result.Models[0].ID)
	require.Equal(t, "gpt-6.1-terra", result.Models[1].ID)
	require.Len(t, result.Models[1].Groups, 2)
	require.Equal(t, "lab-model", result.Models[2].ID)
	require.Equal(t, "upstream", result.Models[2].Groups[0].Source)
	// Permissions are re-read, not cached with a previous user's union.
	groups.groups = groups.groups[1:]
	result, err = s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, result.Models, 2)
	require.Equal(t, int32(2), source.calls.Load())
}

func TestUserModelCatalogFailureIsExplicitAndNeverInventsDefaultModels(t *testing.T) {
	groups := &userCatalogGroupsStub{groups: []Group{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive}}}
	repo := &userCatalogAccountsStub{byGroup: map[int64][]Account{1: {
		{ID: 11, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"configured-alias": "private-target", "gpt-*": "gpt-*"}}},
		{ID: 12, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
	}}}
	source := &userCatalogSourceStub{errs: map[int64]error{11: errors.New("secret upstream error"), 12: errors.New("offline")}}
	s := &UserModelCatalogService{groups: groups, accounts: repo, source: source}
	result, err := s.List(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.Len(t, result.Models, 1)
	require.Equal(t, "configured-alias", result.Models[0].ID)
	require.Equal(t, "configured", result.Models[0].Groups[0].Source)
	require.Nil(t, result.Models[0].Groups[0].ObservedAt)
	_, err = s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int32(2), source.calls.Load(), "failed discovery must back off")
}

func TestUserModelCatalogCachesAccountDiscoveryAcrossConcurrentUsers(t *testing.T) {
	source := &userCatalogSourceStub{models: map[int64][]catalogModel{11: {{ID: "future-model"}}}}
	s := &UserModelCatalogService{source: source}
	account := Account{ID: 11, Platform: PlatformOpenAI, Credentials: map[string]any{"api_key": "old-key"}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := s.discover(context.Background(), &account)
			require.NoError(t, err)
			require.Equal(t, "future-model", entry.models[0].ID)
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), source.calls.Load())
	account.Credentials = map[string]any{"api_key": "rotated-key"}
	_, err := s.discover(context.Background(), &account)
	require.NoError(t, err)
	require.Equal(t, int32(2), source.calls.Load(), "credential changes invalidate account discovery")
}

func TestUserModelCatalogStaleDiscoverySurvivesOutageWithoutClaimingFreshness(t *testing.T) {
	source := &userCatalogSourceStub{errs: map[int64]error{11: errors.New("offline")}}
	s := &UserModelCatalogService{source: source}
	a := Account{ID: 11, Platform: PlatformOpenAI}
	s.cache = map[string]userCatalogCacheEntry{userCatalogAccountKey(&a): {models: []catalogModel{{ID: "last-known"}}, observedAt: time.Now().Add(-2 * time.Minute)}}
	entry, err := s.discover(context.Background(), &a)
	require.NoError(t, err)
	require.True(t, entry.stale)
	require.Equal(t, "last-known", entry.models[0].ID)
}

func TestUserModelCatalogSourceUsesAuthenticatedDiscoveryWithoutInference(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			var calls atomic.Int32
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls.Add(1)
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/v1/models", req.URL.Path)
				require.Equal(t, "Bearer sk-upstream", req.Header.Get("Authorization"))
				return ordinaryModelsUpstreamResponse(`{"data":[{"id":"future-model","display_name":"Future","instructions":"private prompt"}]}`), nil
			}}
			tests := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
			tests.SetOpenAIGatewayService(newCodexModelsAPIKeyTestService(upstream))
			a := newCodexModelsAPIKeyTestAccount("https://models.example/v1")
			a.Platform = platform
			source := accountCatalogSource{tests: tests}
			models, err := source.FetchCatalog(context.Background(), a)
			require.NoError(t, err)
			require.Len(t, models, 1)
			require.Equal(t, "future-model", models[0].ID)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
