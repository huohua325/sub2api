package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"golang.org/x/sync/singleflight"
)

// UserModelCatalog deliberately exposes no account IDs, credentials, upstream
// URLs, prompt templates or provider errors. Group authorization is read afresh
// on every request, independently of the account discovery cache.
type UserModelCatalog struct {
	Models  []UserCatalogModel `json:"models"`
	Partial bool               `json:"partial"`
}

type UserCatalogModel struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"display_name"`
	Platform    string             `json:"platform"`
	Groups      []UserCatalogGroup `json:"groups"`
}

type UserCatalogGroup struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Source     string     `json:"source"` // upstream, stale, configured
	ObservedAt *time.Time `json:"observed_at"`
}

type catalogModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type userCatalogGroups interface {
	GetAvailableGroups(context.Context, int64) ([]Group, error)
}

type userCatalogAccounts interface {
	ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error)
}

type userCatalogSource interface {
	FetchCatalog(context.Context, *Account) ([]catalogModel, error)
}

type accountCatalogSource struct{ tests *AccountTestService }

func (s accountCatalogSource) FetchCatalog(ctx context.Context, account *Account) ([]catalogModel, error) {
	if account.IsOpenAI() && s.tests.openaiGatewayService != nil {
		// Reuse the same cached, account-authenticated source as /v1/models.
		response, err := s.tests.openaiGatewayService.FetchOpenAIModelsList(ctx, account)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Data []catalogModel `json:"data"`
		}
		if err := json.Unmarshal(response.Body, &payload); err != nil {
			return nil, err
		}
		return payload.Data, nil
	}
	ids, err := s.tests.FetchUpstreamSupportedModels(ctx, account)
	if err != nil {
		return nil, err
	}
	models := make([]catalogModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, catalogModel{ID: id})
	}
	return models, nil
}

type userCatalogCacheEntry struct {
	models     []catalogModel
	observedAt time.Time
	retryAt    time.Time
	failed     bool
	stale      bool
}

// UserModelCatalogService unions all persistently enabled accounts in bindable
// groups. Temporary quota exhaustion/rate limits must not make models disappear.
// Discovery is bounded to four upstream requests across all panel users; it only
// calls model-list endpoints, never paid inference or metadata-registry probes.
type UserModelCatalogService struct {
	groups   userCatalogGroups
	accounts userCatalogAccounts
	source   userCatalogSource
	mu       sync.Mutex
	cache    map[string]userCatalogCacheEntry
	flights  singleflight.Group
	slots    chan struct{}
}

func NewUserModelCatalogService(groups *APIKeyService, accounts AccountRepository, tests *AccountTestService) *UserModelCatalogService {
	return &UserModelCatalogService{groups: groups, accounts: accounts, source: accountCatalogSource{tests: tests}}
}

func userCatalogAccountKey(account *Account) string {
	// Do not retain plaintext secrets in cache keys. A rotated credential or
	// endpoint must not inherit the old account catalog or its failure backoff.
	encoded, _ := json.Marshal([]any{account.ID, account.Platform, account.Type, account.Credentials, account.ProxyID})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func (s *UserModelCatalogService) discover(ctx context.Context, account *Account) (userCatalogCacheEntry, error) {
	if err := ctx.Err(); err != nil {
		return userCatalogCacheEntry{}, err
	}
	key := userCatalogAccountKey(account)
	now := time.Now()
	s.mu.Lock()
	entry, exists := s.cache[key]
	s.mu.Unlock()
	if exists {
		entry.stale = !entry.observedAt.IsZero() && now.Sub(entry.observedAt) >= time.Minute
		if now.Sub(entry.observedAt) >= 5*time.Minute {
			entry.models = nil
		}
		if now.Before(entry.retryAt) || (!entry.failed && !entry.stale) {
			return entry, nil
		}
	}
	previous := entry
	result := s.flights.DoChan(key, func() (any, error) {
		s.mu.Lock()
		cached, ok := s.cache[key]
		s.mu.Unlock()
		if ok && (time.Now().Before(cached.retryAt) || (!cached.failed && time.Since(cached.observedAt) < time.Minute)) {
			return cached, nil
		}
		// A disconnected viewer must not cancel shared discovery for others.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 6*time.Second)
		defer cancel()
		s.mu.Lock()
		if s.slots == nil {
			s.slots = make(chan struct{}, 4)
		}
		slots := s.slots
		s.mu.Unlock()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-fetchCtx.Done():
			return nil, fetchCtx.Err()
		}
		models, err := s.source.FetchCatalog(fetchCtx, account)
		updated := userCatalogCacheEntry{models: models, observedAt: time.Now()}
		if err != nil {
			updated = previous
			updated.failed = true
			updated.stale = true
			updated.retryAt = time.Now().Add(30 * time.Second)
			if time.Since(updated.observedAt) >= 5*time.Minute {
				updated.models = nil
			}
		}
		s.mu.Lock()
		if s.cache == nil {
			s.cache = make(map[string]userCatalogCacheEntry)
		}
		if len(s.cache) >= 512 {
			// Bound memory even when accounts and credentials churn.
			oldestKey := ""
			var oldest time.Time
			for k, v := range s.cache {
				if oldestKey == "" || v.observedAt.Before(oldest) {
					oldestKey = k
					oldest = v.observedAt
				}
			}
			delete(s.cache, oldestKey)
		}
		s.cache[key] = updated
		s.mu.Unlock()
		return updated, nil
	})
	if exists && len(entry.models) > 0 {
		entry.stale = true
		return entry, nil
	}
	select {
	case <-ctx.Done():
		return userCatalogCacheEntry{}, ctx.Err()
	case value := <-result:
		if value.Err != nil {
			return userCatalogCacheEntry{}, value.Err
		}
		entry, ok := value.Val.(userCatalogCacheEntry)
		if !ok {
			return userCatalogCacheEntry{}, fmt.Errorf("invalid model catalog cache entry")
		}
		return entry, nil
	}
}

func (s *UserModelCatalogService) List(ctx context.Context, userID int64) (*UserModelCatalog, error) {
	groups, err := s.groups.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	type accountRoute struct {
		account Account
		groups  []int
	}
	routes := make([]accountRoute, 0)
	byAccount := make(map[int64]int)
	for g := range groups {
		group := &groups[g]
		if group.Status != StatusActive {
			continue
		}
		platforms := []string{group.Platform}
		if group.Platform == PlatformComposite {
			platforms = domain.ConcretePlatformIDs()
		}
		if group.Platform == PlatformGemini || group.Platform == PlatformAnthropic {
			platforms = append(platforms, PlatformAntigravity)
		}
		accounts, listErr := s.accounts.ListModelAvailabilityCandidates(ctx, &group.ID, platforms, false)
		if listErr != nil {
			return nil, listErr
		}
		for _, account := range accounts {
			if account.Status != StatusActive || !account.Schedulable {
				continue
			}
			if group.RequireOAuthOnly && account.Type == AccountTypeAPIKey {
				continue
			}
			if group.RequirePrivacySet && !account.IsPrivacySet() {
				continue
			}
			mixed := account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled() && (group.Platform == PlatformGemini || group.Platform == PlatformAnthropic)
			if group.Platform != PlatformComposite && account.Platform != group.Platform && !mixed {
				continue
			}
			if idx, ok := byAccount[account.ID]; ok {
				routes[idx].groups = append(routes[idx].groups, g)
			} else {
				byAccount[account.ID] = len(routes)
				routes = append(routes, accountRoute{account: account, groups: []int{g}})
			}
		}
	}
	// A bounded worker pool avoids spawning a goroutine per account on a large
	// installation. The outer deadline bounds cold discovery for the whole page.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	entries := make([]userCatalogCacheEntry, len(routes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(4, len(routes)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				entry, fetchErr := s.discover(ctx, &routes[i].account)
				if fetchErr != nil {
					entry.failed = true
				}
				entries[i] = entry
			}
		}()
	}
	for i := range routes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	result := &UserModelCatalog{Models: []UserCatalogModel{}}
	byModel := make(map[string]int)
	for i, route := range routes {
		entry := entries[i]
		source := "upstream"
		if entry.failed || entry.stale {
			result.Partial = true
		}
		if entry.stale {
			source = "stale"
		}
		models := entry.models
		if entry.observedAt.IsZero() || (entry.failed && len(models) == 0) {
			source = "configured"
			models = configuredUserCatalogModels(&route.account)
		}
		for _, g := range route.groups {
			group := &groups[g]
			public := models
			if source != "configured" {
				public, err = projectUserCatalogModels(models, &route.account, group)
				if err != nil {
					result.Partial = true
					continue
				}
			}
			for _, model := range public {
				if model.ID == "" || strings.Contains(model.ID, "*") || !group.ModelAllowlist.Allows(model.ID) || !route.account.IsModelSupported(model.ID) {
					continue
				}
				if group.Platform == PlatformGemini && route.account.Platform == PlatformAntigravity && !strings.HasPrefix(model.ID, "gemini-") {
					continue
				}
				if group.Platform == PlatformAnthropic && route.account.Platform == PlatformAntigravity && !strings.HasPrefix(model.ID, "claude-") {
					continue
				}
				key := route.account.Platform + "\x00" + model.ID
				idx, ok := byModel[key]
				if !ok {
					idx = len(result.Models)
					byModel[key] = idx
					result.Models = append(result.Models, UserCatalogModel{ID: model.ID, DisplayName: model.DisplayName, Platform: route.account.Platform, Groups: []UserCatalogGroup{}})
				}
				visible := UserCatalogGroup{ID: group.ID, Name: group.Name, Source: source}
				if source != "configured" && !entry.observedAt.IsZero() {
					observed := entry.observedAt
					visible.ObservedAt = &observed
				}
				found := false
				for j, existing := range result.Models[idx].Groups {
					if existing.ID != group.ID {
						continue
					}
					found = true
					if userCatalogSourceRank(source) > userCatalogSourceRank(existing.Source) {
						result.Models[idx].Groups[j] = visible
					}
					break
				}
				if !found {
					result.Models[idx].Groups = append(result.Models[idx].Groups, visible)
				}
			}
		}
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].ID == result.Models[j].ID {
			return result.Models[i].Platform < result.Models[j].Platform
		}
		return result.Models[i].ID < result.Models[j].ID
	})
	return result, nil
}

func userCatalogSourceRank(source string) int {
	switch source {
	case "upstream":
		return 3
	case "stale":
		return 2
	default:
		return 1
	}
}

func configuredUserCatalogModels(account *Account) []catalogModel {
	// Only explicit administrator configuration is a fallback. Built-in default
	// model lists must never masquerade as a discovered account catalog.
	models := make([]catalogModel, 0)
	if mapping, ok := account.Credentials["model_mapping"].(map[string]any); ok {
		for id, target := range mapping {
			if value, ok := target.(string); ok && strings.TrimSpace(value) != "" && !strings.Contains(id, "*") {
				models = append(models, catalogModel{ID: strings.TrimSpace(id)})
			}
		}
	}
	return models
}

func projectUserCatalogModels(models []catalogModel, account *Account, group *Group) ([]catalogModel, error) {
	body, err := json.Marshal(struct {
		Data []catalogModel `json:"data"`
	}{models})
	if err != nil {
		return nil, err
	}
	body, err = projectAccountModelAliases(body, account, group, false)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []catalogModel `json:"data"`
	}
	err = json.Unmarshal(body, &payload)
	return payload.Data, err
}
