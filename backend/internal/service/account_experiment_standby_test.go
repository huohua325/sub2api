package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountStandbyTestRepo struct {
	AccountRepository
	primaries []Account
	targets   []*Account
}

func (r accountStandbyTestRepo) ListByGroup(context.Context, int64) ([]Account, error) {
	return r.primaries, nil
}

func (r accountStandbyTestRepo) ListCodingStandbyAccounts(context.Context, int64) ([]*Account, error) {
	return r.targets, nil
}

func TestAccountExperimentStandbyDispatch(t *testing.T) {
	for _, scenario := range []struct {
		name, platform, model, role  string
		expired, hasTarget, dispatch bool
	}{
		{"codex", PlatformOpenAI, "gpt-test", RoleAdmin, true, true, true},
		{"unified GPT", PlatformComposite, "gpt-test", RoleAdmin, true, true, true},
		{"healthy subscription", PlatformOpenAI, "gpt-test", RoleAdmin, false, true, false},
		{"unconfigured", PlatformOpenAI, "gpt-test", RoleAdmin, true, false, false},
		{"unsupported model", PlatformOpenAI, "gpt-other", RoleAdmin, true, true, false},
		{"ordinary user", PlatformOpenAI, "gpt-test", RoleUser, true, true, false},
		{"Grok route", PlatformGrok, "gpt-test", RoleAdmin, true, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			expiry := time.Now().Add(time.Hour)
			if scenario.expired {
				expiry = time.Now().Add(-time.Hour)
			}
			primary := Account{ID: 10, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, AutoPauseOnExpired: true, ExpiresAt: &expiry}
			experiment := &Group{ID: 7, Platform: PlatformOpenAI, UsageKind: UsageKindExperiment, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
			target := &Account{ID: 20, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
				GroupIDs: []int64{7}, Groups: []*Group{experiment},
				Extra:       map[string]any{"openai_responses_mode": "force_responses"},
				Credentials: map[string]any{"api_key": "test-only", "model_mapping": map[string]any{"gpt-test": "gpt-test"}}}
			repo := accountStandbyTestRepo{primaries: []Account{primary}}
			if scenario.hasTarget {
				repo.targets = []*Account{target}
			}
			service := &OpenAIGatewayService{accountRepo: repo}
			group := &Group{ID: 8, Platform: scenario.platform, UsageKind: UsageKindCoding, Status: StatusActive}
			key := &APIKey{ID: 9, UserID: 2, User: &User{ID: 2, Role: scenario.role, Status: StatusActive}, Group: group, GroupID: &group.ID}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resolved := scenario.platform
			if resolved == PlatformComposite {
				resolved = PlatformOpenAI
			}
			ctx := WithResolvedTargetPlatform(c.Request.Context(), resolved)
			called := false
			handled, err := service.TryAPIFallback(ctx, c, NewUsageMonitorService(&fallbackGatewayControl{}), key, scenario.model,
				[]byte(`{"model":"`+scenario.model+`","input":"test"}`), time.Now(), func(policy *APIFallbackPolicy, _ []byte) error {
					called = true
					require.Equal(t, int64(20), policy.ExperimentAccountID)
					require.Equal(t, int64(7), policy.ExperimentGroupID)
					require.Zero(t, policy.ExperimentKeyID)
					require.NoError(t, service.CheckAccountExperimentStandby(ctx, policy, key.User))
					require.Error(t, (&OpenAIGatewayService{accountRepo: accountStandbyTestRepo{}}).CheckAccountExperimentStandby(ctx, policy, key.User))
					return nil
				})
			require.NoError(t, err)
			require.Equal(t, scenario.dispatch, handled)
			require.Equal(t, scenario.dispatch, called)
		})
	}
}
