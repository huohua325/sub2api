//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnifiedCodingPurposeAndBilling(t *testing.T) {
	group := &Group{Platform: PlatformComposite, UsageKind: UsageKindCoding}
	if _, err := NormalizeUsageKind(group.Platform, group.UsageKind); err != nil {
		t.Fatalf("explicit composite coding group must be supported: %v", err)
	}
	if !groupHasPurposeWallet(group) || PurposeOfGroup(group.Platform, group.UsageKind) != "" {
		t.Fatal("unified coding has purpose wallets, but no static provider wallet")
	}
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			ctx := WithResolvedTargetPlatform(context.Background(), platform)
			if got := PurposeForRequest(ctx, group); got != platform {
				t.Fatalf("resolved purpose = %q, want %q", got, platform)
			}
			account := &Account{ID: 3, Platform: platform, Type: AccountTypeOAuth}
			p := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 1, ActualCost: 0.2}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2, Group: group}, Account: account}
			log := &UsageLog{OfficialAccountID: &account.ID, CreatedAt: time.Now()}
			cmd := buildUsageBillingCommand("unified-"+platform, log, p)
			if cmd.Purpose != platform || cmd.BalanceCost != 0 || cmd.PurposeCost != 0.2 || cmd.CodingQuota == nil || cmd.CodingQuota.Purpose != platform {
				t.Fatalf("wrong wallet or shared-balance fallback: %+v, coding %+v", cmd, cmd.CodingQuota)
			}
		})
	}
	for _, platform := range []string{"", PlatformAnthropic, PlatformComposite} {
		ctx := WithResolvedTargetPlatform(context.Background(), platform)
		if got := PurposeForRequest(ctx, group); got != "" {
			t.Fatalf("unsupported %q must not resolve to a wallet: %q", platform, got)
		}
	}
	group.UsageKind = UsageKindExperiment
	if got := PurposeForRequest(WithResolvedTargetPlatform(context.Background(), PlatformGrok), group); got != PurposeExperiment {
		t.Fatalf("composite experiment must keep its wallet: %q", got)
	}
}

func TestUnifiedCodingSelectedAccountFailsClosed(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformAnthropic} {
		t.Run(platform, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite, UsageKind: UsageKindCoding}})
			ctx := WithResolvedTargetPlatform(c.Request.Context(), PlatformGrok)
			// A missing metering service must fail closed even in shadow mode.
			_, err := prepareCodingRequest(ctx, c, nil, nil, &Account{ID: 3, Platform: platform, Type: AccountTypeOAuth}, nil)
			if err == nil {
				t.Fatal("unified coding must not forward without a metering service")
			}
		})
	}
}

func TestUnifiedCodingRejectsUnmeteredAccountAndMissingStore(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_GROK_MODE", "off")
	group := &Group{Platform: PlatformComposite, UsageKind: UsageKindCoding}
	ctx := WithResolvedTargetPlatform(context.Background(), PlatformGrok)
	serviceWithoutStore := &APIKeyService{officialPoints: NewOfficialPointService(nil, nil, nil)}
	if err := serviceWithoutStore.CheckUnifiedCodingQuota(ctx, 1, group); !errors.Is(err, ErrCodingQuotaUnavailable) {
		t.Errorf("missing store must reject: %v", err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", &APIKey{Group: group})
	points := NewOfficialPointService(nil, codingReadTestStore{}, nil)
	_, err := prepareCodingRequest(ctx, c, points, nil, &Account{ID: 3, Platform: PlatformGrok, Type: AccountTypeAPIKey}, nil)
	if err == nil {
		t.Fatal("unified subscription coding must reject an API-key account lacking subscription metering")
	}
}

func TestUnifiedCodingQuotaIsolation(t *testing.T) {
	group := &Group{Platform: PlatformComposite, UsageKind: UsageKindCoding}
	for _, exhausted := range []string{PurposeOpenAI, PurposeGrok} {
		rows := []CodingQuotaReading{}
		for _, purpose := range []string{PurposeOpenAI, PurposeGrok} {
			used := "0"
			if purpose == exhausted {
				used = "1000000"
			}
			rows = append(rows, CodingQuotaReading{Purpose: purpose, Used: used, Pending: "0"})
		}
		svc := &APIKeyService{officialPoints: NewOfficialPointService(nil, codingReadTestStore{rows: rows}, nil)}
		if blocked, checked := svc.PurposeBlocks(context.Background(), 1, group); blocked || !checked {
			t.Fatal("model discovery before resolution must not use shared balance or block another wallet")
		}
		for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
			ctx := WithResolvedTargetPlatform(context.Background(), platform)
			if blocked, checked := svc.PurposeBlocks(ctx, 1, group); !checked || blocked != (platform == exhausted) {
				t.Fatalf("exhausted=%s target=%s blocked=%v checked=%v", exhausted, platform, blocked, checked)
			}
		}
	}
}

func TestUnifiedCodingCatalogResolvesActualPlatform(t *testing.T) {
	resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 1, PublicModel: "gpt-looking-grok-alias", TargetPlatform: PlatformGrok, Enabled: true},
		{ID: 2, GroupID: 1, PublicModel: "grok-looking-claude-alias", TargetPlatform: PlatformAnthropic, Enabled: true},
	}})
	got, err := resolver.FilterCodingModels(context.Background(), 1, []string{"gpt-6.1-sol", "grok-4.6", "gpt-looking-grok-alias", "grok-looking-claude-alias", "claude-sonnet-4-6", "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2] != "gpt-looking-grok-alias" {
		t.Fatalf("catalog must use real route platform: %v", got)
	}
}
