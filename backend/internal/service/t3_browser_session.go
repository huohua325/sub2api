package service

import (
	"context"
	"time"
)

// ValidateT3BrowserSession checks the current admin and the browser's live refresh family.
// MILE logout revokes the active refresh token before the browser clears its credentials.
func (s *AuthService) ValidateT3BrowserSession(ctx context.Context, token string) error {
	claims, err := s.ValidateToken(token)
	if err != nil {
		return err
	}
	if claims.SessionID == "" || s.refreshTokenCache == nil {
		return ErrInvalidToken
	}
	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil || user == nil || !user.IsActive() || !user.IsAdmin() ||
		claims.TokenVersion != resolvedTokenVersion(user) {
		return ErrInvalidToken
	}
	if s.settingService != nil && s.settingService.IsSessionBindingEnabled(ctx) &&
		claims.BindingHash != "" && sessionBindingHashFromContext(ctx) != claims.BindingHash {
		return ErrInvalidToken
	}

	hashes, err := s.refreshTokenCache.GetFamilyTokenHashes(ctx, claims.SessionID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, hash := range hashes {
		// A consumed token can still be listed in its family set.
		data, err := s.refreshTokenCache.GetRefreshToken(ctx, hash)
		if err == nil && data != nil && data.FamilyID == claims.SessionID && data.UserID == user.ID &&
			data.TokenVersion == claims.TokenVersion && now.Before(data.ExpiresAt) {
			return nil
		}
	}
	return ErrInvalidToken
}
