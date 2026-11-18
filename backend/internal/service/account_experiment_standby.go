package service

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

const AccountStandbyGroupIDsExtra = "coding_standby_group_ids"

type accountExperimentStandbyRepository interface {
	ListCodingStandbyAccounts(context.Context, int64) ([]*Account, error)
}

func (s *adminServiceImpl) validateAccountExperimentStandby(ctx context.Context, account *Account, groupIDs []int64) error {
	raw, ok := account.Extra[AccountStandbyGroupIDsExtra]
	if !ok || raw == nil {
		return nil
	}
	invalid := infraerrors.BadRequest("INVALID_ACCOUNT_STANDBY", "Select active Codex or Unified Coding groups for a Responses-capable GPT experiment account without a normal coding group binding")
	payload, err := json.Marshal(raw)
	var ids []int64
	if err != nil || json.Unmarshal(payload, &ids) != nil {
		return invalid
	}
	if len(ids) == 0 {
		return nil
	}
	if !account.IsOpenAIApiKey() || account.ParentAccountID != nil ||
		openai_compat.ResolveResponsesSupport(account.Extra) != openai_compat.ResponsesSupportYes ||
		len(ids) > 50 {
		return invalid
	}
	hasExperimentGroup := false
	for _, id := range groupIDs {
		g, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if g == nil || g.UsageKind == UsageKindCoding {
			return invalid
		}
		hasExperimentGroup = hasExperimentGroup || (g.IsActive() && g.Platform == PlatformOpenAI && g.UsageKind == UsageKindExperiment && !g.IsSubscriptionType())
	}
	if !hasExperimentGroup {
		return invalid
	}
	for _, id := range ids {
		if id <= 0 {
			return invalid
		}
		g, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if !IsAPIFallbackCodingGroup(g) || !g.IsActive() || g.Platform == PlatformGrok {
			return invalid
		}
	}
	return nil
}

func accountStandbyExperimentGroup(account *Account, sourceGroupID int64, model string, user *User, targetID int64) *Group {
	if account == nil || user == nil || !user.IsAdmin() || !user.IsActive() ||
		!account.IsOpenAIApiKey() || account.ParentAccountID != nil || !account.IsSchedulable() ||
		account.routesByModel() || account.RoutesProtocolByInbound() ||
		account.GetCredential("api_key") == "" || slices.Contains(account.GroupIDs, sourceGroupID) ||
		openai_compat.ResolveResponsesSupport(account.Extra) != openai_compat.ResponsesSupportYes ||
		!account.IsModelSupported(model) || ResolveOpenAIAccountUpstreamModelForRequest(account, model, false) != model {
		return nil
	}
	var target *Group
	for _, group := range account.Groups {
		if group != nil && group.UsageKind == UsageKindCoding {
			return nil
		}
		if group == nil || group.Platform != PlatformOpenAI || group.UsageKind != UsageKindExperiment ||
			!group.IsActive() || group.IsSubscriptionType() || !user.CanBindLabGroup(group) ||
			!group.ModelAllowlist.Allows(model) || targetID > 0 && group.ID != targetID {
			continue
		}
		if target == nil || group.ID < target.ID {
			target = group
		}
	}
	return target
}

func (s *OpenAIGatewayService) findAccountExperimentStandby(ctx context.Context, key *APIKey, platform, model string) (*APIFallbackPolicy, error) {
	repo, ok := s.accountRepo.(accountExperimentStandbyRepository)
	if !ok || platform != PlatformOpenAI || !key.User.IsActive() || key.Quota > 0 || key.HasRateLimits() {
		return nil, nil
	}
	accounts, err := repo.ListCodingStandbyAccounts(ctx, *key.GroupID)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if target := accountStandbyExperimentGroup(account, *key.GroupID, model, key.User, 0); target != nil {
			return &APIFallbackPolicy{
				GroupID: *key.GroupID, Platform: platform, Model: model, Enabled: true,
				CapabilitiesVerified: true, ApprovedBy: key.UserID,
				ExperimentGroupID: target.ID, ExperimentAccountID: account.ID,
			}, nil
		}
	}
	return nil, nil
}

// CheckAccountExperimentStandby rechecks the explicit relationship and account
// capabilities against the database immediately before a paid send.
func (s *OpenAIGatewayService) CheckAccountExperimentStandby(ctx context.Context, policy *APIFallbackPolicy, user *User) error {
	repo, ok := s.accountRepo.(accountExperimentStandbyRepository)
	if !ok || policy == nil || policy.Platform != PlatformOpenAI || !policy.ActiveAt(time.Now()) {
		return ErrExperimentAccountingUnavailable
	}
	accounts, err := repo.ListCodingStandbyAccounts(ctx, policy.GroupID)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if account.ID == policy.ExperimentAccountID &&
			accountStandbyExperimentGroup(account, policy.GroupID, policy.Model, user, policy.ExperimentGroupID) != nil {
			return nil
		}
	}
	return ErrExperimentAccountingUnavailable
}

// BindAccountStandbyKey derives an internal experiment billing scope from the
// fresh coding key. Its key/user IDs, credentials and ACLs remain unchanged.
func (s *APIKeyService) BindAccountStandbyKey(ctx context.Context, standby *ExperimentStandby, source *APIKey, req *http.Request) (*APIKey, error) {
	if standby == nil || standby.AccountID <= 0 || standby.Platform != PlatformOpenAI ||
		req == nil || req.Method != http.MethodPost || req.URL == nil || req.URL.Path != "/v1/responses" ||
		!standby.MatchesSourceKey(source) || s.groupRepo == nil {
		return nil, ErrExperimentAccountingUnavailable
	}
	group, err := s.groupRepo.GetByID(ctx, standby.GroupID)
	if err != nil || group == nil || !source.User.CanBindLabGroup(group) || !group.ModelAllowlist.Allows(standby.Model) {
		return nil, ErrExperimentAccountingUnavailable
	}
	key := *source
	key.Group, key.GroupID = group, &group.ID
	if !standby.MatchesKey(&key) {
		return nil, ErrExperimentAccountingUnavailable
	}
	return &key, nil
}

func (s *ExperimentStandby) MatchesSourceKey(key *APIKey) bool {
	return key != nil && key.ID == s.SourceKeyID && key.UserID == s.UserID &&
		key.User != nil && key.User.ID == s.UserID && key.User.IsAdmin() && key.User.IsActive() &&
		key.IsActive() && !key.IsExpired() && !key.IsQuotaExhausted() && key.Quota <= 0 && !key.HasRateLimits() &&
		key.GroupID != nil && *key.GroupID == s.SourceGroupID && key.Group != nil && key.Group.ID == s.SourceGroupID &&
		key.Group.IsActive() && IsAPIFallbackCodingGroup(key.Group) &&
		(key.Group.Platform == s.Platform || key.Group.Platform == PlatformComposite) &&
		(key.Group.IsSubscriptionType() || key.User.CanBindLabGroup(key.Group))
}

func (s *OpenAIGatewayService) AccountStandbyExclusions(ctx context.Context, standby *ExperimentStandby) (map[int64]struct{}, error) {
	accounts, err := s.accountRepo.ListByGroup(ctx, standby.GroupID)
	if err != nil {
		return nil, err
	}
	excluded := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		if account.ID != standby.AccountID {
			excluded[account.ID] = struct{}{}
		}
	}
	return excluded, nil
}
