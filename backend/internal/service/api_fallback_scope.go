package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
)

// IsAPIFallbackCodingGroup identifies the three explicitly supported coding
// entrances. Experiment groups never inherit a coding standby policy.
func IsAPIFallbackCodingGroup(group *Group) bool {
	return group != nil && group.UsageKind == UsageKindCoding &&
		(group.Platform == PlatformComposite || group.Platform == PlatformOpenAI || group.Platform == PlatformGrok)
}

// APIFallbackPlatformForGroup resolves a concrete standby platform without
// guessing from a model name or moving a standalone request to another wallet.
func APIFallbackPlatformForGroup(group *Group, resolved string) (string, bool) {
	if !IsAPIFallbackCodingGroup(group) {
		return "", false
	}
	if group.Platform == PlatformComposite {
		return resolved, resolved == PlatformOpenAI || resolved == PlatformGrok
	}
	if resolved != "" && resolved != group.Platform {
		return "", false
	}
	return group.Platform, true
}

// apiFallbackRoleReader deliberately exposes only internal account IDs. The
// production repository implements it; old test/custom repositories without
// standby persistence retain their existing scheduling semantics.
type apiFallbackRoleReader interface {
	ListAPIFallbackBackupAccountIDs(context.Context, int64) ([]int64, error)
}

func apiFallbackBackupAccountIDs(ctx context.Context, repo AccountRepository, groupID *int64) ([]int64, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, nil
	}
	if group := apiFallbackRequestGroup(ctx, nil); group != nil && group.ID == *groupID && !IsAPIFallbackCodingGroup(group) {
		return nil, nil
	}
	reader, ok := repo.(apiFallbackRoleReader)
	if !ok {
		return nil, nil
	}
	ids, err := reader.ListAPIFallbackBackupAccountIDs(ctx, *groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: standby account roles cannot be verified", ErrCodingQuotaUnavailable)
	}
	return ids, nil
}

func (s *OpenAIGatewayService) apiFallbackSchedulingExclusions(ctx context.Context, groupID *int64, existing map[int64]struct{}) (map[int64]struct{}, error) {
	ids, err := apiFallbackBackupAccountIDs(ctx, s.accountRepo, groupID)
	if err != nil || len(ids) == 0 {
		return existing, err
	}
	merged := make(map[int64]struct{}, len(existing)+len(ids))
	for id := range existing {
		merged[id] = struct{}{}
	}
	for _, id := range ids {
		merged[id] = struct{}{}
	}
	return merged, nil
}

func apiFallbackRequestGroup(ctx context.Context, c *gin.Context) *Group {
	if c != nil {
		if value, ok := c.Get("api_key"); ok {
			if key, ok := value.(*APIKey); ok && key != nil && key.Group != nil {
				return key.Group
			}
		}
	}
	if ctx != nil {
		group, _ := ctx.Value(ctxkey.Group).(*Group)
		return group
	}
	return nil
}

func guardAPIFallbackNormalAccount(ctx context.Context, repo AccountRepository, group *Group, account *Account) error {
	if account == nil || !IsAPIFallbackCodingGroup(group) {
		return nil
	}
	ids, err := apiFallbackBackupAccountIDs(ctx, repo, &group.ID)
	if err != nil {
		return err
	}
	if slices.Contains(ids, account.ID) {
		return fmt.Errorf("%w: designated standby accounts require approved paid admission", ErrCodingQuotaUnavailable)
	}
	return nil
}

func filterAPIFallbackCatalogAccounts(ctx context.Context, repo AccountRepository, groupID *int64, accounts []Account) ([]Account, error) {
	ids, err := apiFallbackBackupAccountIDs(ctx, repo, groupID)
	if err != nil || len(ids) == 0 {
		return accounts, err
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if !slices.Contains(ids, account.ID) {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}

func apiFallbackModelCacheKey(ctx context.Context, repo AccountRepository, groupID *int64, key string) (string, error) {
	ids, err := apiFallbackBackupAccountIDs(ctx, repo, groupID)
	if err != nil || len(ids) == 0 {
		return key, err
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return key + ":api-standby:" + fmt.Sprint(ids), nil
}
