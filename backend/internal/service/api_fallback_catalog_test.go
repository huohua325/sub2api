package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type fallbackCatalogGroupRepo struct {
	GroupRepository
	group *Group
}

func (r fallbackCatalogGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	return r.group, nil
}

func fallbackCatalogAccounts() (primary []Account, backup []Account) {
	withReasoning := true
	main := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"base_url": "https://chatgpt.com", "model_mapping": map[string]any{"coding-alias": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6.1-sol"}}}
	main.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-6.1-sol": {ID: "gpt-6.1-sol", Reasoning: &withReasoning, SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh"},
			InputModalities: []string{"text", "image"}, ContextWindow: 500000, MaxOutputTokens: 65536,
			CodexToolCapabilities: map[string]json.RawMessage{"use_responses_lite": json.RawMessage("true"), "tool_mode": json.RawMessage(`"code_mode_only"`)}}}})
	grok := Account{ID: 2, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"grok-4.7": "grok-4.7"}}}
	api := Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"base_url": "https://relay.example/v1", "model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol", "api-only-model": "api-only-model"}}}
	api.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-6.1-sol": {ID: "gpt-6.1-sol", Reasoning: &withReasoning, SupportedReasoningLevels: []string{"medium"}, InputModalities: []string{"text"}, ContextWindow: 8192,
			CodexToolCapabilities: map[string]json.RawMessage{"use_responses_lite": json.RawMessage("false"), "tool_mode": json.RawMessage("null")}}}})
	conflict := Account{ID: 4, Platform: PlatformGrok, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"coding-alias": "grok-4.7", "api-only-grok": "grok-4.7"}}}
	return []Account{main, grok}, []Account{api, conflict}
}

func TestAPIFallbackCatalogAddingStandbyDoesNotChangeUnifiedNamesOrCapabilities(t *testing.T) {
	ctx := context.Background()
	group := &Group{ID: 88, Platform: PlatformComposite, UsageKind: UsageKindCoding}
	primary, backup := fallbackCatalogAccounts()
	accounts := map[int64][]Account{group.ID: primary}
	s := &GatewayService{accountRepo: codexModelsVisibilityAccountRepo{byGroup: accounts}, groupRepo: fallbackCatalogGroupRepo{group: group}}
	beforeNames := s.GetAvailableModels(ctx, &group.ID, "")
	before, err := s.BuildCodexModelsManifestForGroup(ctx, group, "", beforeNames)
	require.NoError(t, err)
	beforeOwner, err := s.resolveCompositeModelOwnership(ctx, group.ID, "coding-alias")
	require.NoError(t, err)
	require.True(t, beforeOwner.Matched)
	require.Equal(t, PlatformOpenAI, beforeOwner.TargetPlatform)
	accounts[group.ID] = append(append([]Account(nil), primary...), backup...)
	afterNames := s.GetAvailableModels(ctx, &group.ID, "")
	require.Equal(t, beforeNames, afterNames, "standby membership must not add ordinary model names")
	after, err := s.BuildCodexModelsManifestForGroup(ctx, group, "", afterNames)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "standby metadata must not narrow Lite, tools, images, reasoning or context")
	afterOwner, err := s.resolveCompositeModelOwnership(ctx, group.ID, "coding-alias")
	require.NoError(t, err)
	require.Equal(t, beforeOwner, afterOwner, "API aliases cannot introduce cross-platform ownership conflicts")
	beforeMetadata, beforeOK := groupCodexModelMetadata(PlatformComposite, "gpt-6.1-sol", primary, group, nil, true)
	afterMetadata, afterOK := groupCodexModelMetadata(PlatformComposite, "gpt-6.1-sol", accounts[group.ID], group, nil, true)
	require.Equal(t, beforeOK, afterOK)
	require.Equal(t, beforeMetadata, afterMetadata)
}

func TestAPIFallbackCatalogAPIOnlyPlatformDoesNotAppearInUnifiedPicker(t *testing.T) {
	group := &Group{ID: 88, Platform: PlatformComposite, UsageKind: UsageKindCoding}
	primary, backup := fallbackCatalogAccounts()
	accounts := []Account{primary[0], backup[1]}
	s := &GatewayService{accountRepo: codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{group.ID: accounts}}, groupRepo: fallbackCatalogGroupRepo{group: group}}
	platforms := s.GetSchedulablePlatforms(context.Background(), &group.ID)
	require.Contains(t, platforms, PlatformOpenAI)
	require.NotContains(t, platforms, PlatformGrok, "API-only platform cannot trigger default catalog supplementation")
	require.NotContains(t, s.GetAvailableModels(context.Background(), &group.ID, ""), "api-only-grok")
}

func TestAPIFallbackCatalogPreservesOrdinaryCompositeAPIAccounts(t *testing.T) {
	ctx := context.Background()
	group := &Group{ID: 88, Platform: PlatformComposite, UsageKind: UsageKindExperiment}
	primary, backup := fallbackCatalogAccounts()
	accounts := append(append([]Account(nil), primary...), backup...)
	s := &GatewayService{accountRepo: codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{group.ID: accounts}}, groupRepo: fallbackCatalogGroupRepo{group: group}}
	models := s.GetAvailableModels(ctx, &group.ID, "")
	require.Contains(t, models, "api-only-model")
	require.Contains(t, models, "api-only-grok")
	owner, err := s.resolveCompositeModelOwnership(ctx, group.ID, "coding-alias")
	require.NoError(t, err)
	require.True(t, owner.Ambiguous, "ordinary composite routing keeps existing API ownership semantics")
	filtered := unifiedCodingCatalogAccounts(group, accounts)
	require.Len(t, filtered, len(accounts))
}
