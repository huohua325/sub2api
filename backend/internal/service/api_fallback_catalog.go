package service

import "context"

// Backup API members never participate in the ordinary unified-coding model
// catalog. Adding a disabled standby must not change names, Lite/tool protocol,
// context limits or ownership declared by the subscription accounts.
func unifiedCodingCatalogAccounts(group *Group, accounts []Account) []Account {
	if !IsUnifiedCodingGroup(group) {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.IsOAuth() {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func (s *GatewayService) codingCatalogAccountsForGroupID(ctx context.Context, groupID *int64, accounts []Account) ([]Account, error) {
	if groupID == nil || s.groupRepo == nil {
		return accounts, nil
	}
	group, err := s.groupRepo.GetByID(ctx, *groupID)
	if err != nil {
		return nil, err
	}
	if !IsAPIFallbackCodingGroup(group) {
		return accounts, nil
	}
	accounts, err = filterAPIFallbackCatalogAccounts(ctx, s.accountRepo, groupID, accounts)
	if err != nil {
		return nil, err
	}
	return unifiedCodingCatalogAccounts(group, accounts), nil
}
