package service

import (
	"context"
	"time"
)

// T3BrowserSession is the non-secret identity snapshot verified against
// live administrator state and the browser's Redis refresh family.
type T3BrowserSession struct {
	UserID       int64  `json:"user_id"`
	SessionID    string `json:"session_id"`
	TokenVersion int64  `json:"token_version"`
	BindingHash  string `json:"binding_hash,omitempty"`
}

// ValidateT3BrowserSession checks the current admin and the browser's live refresh family.
// MILE logout revokes the active refresh token before the browser clears its credentials.
func (s *AuthService) ValidateT3BrowserSession(ctx context.Context, token string) error {
	_, err := s.InspectT3BrowserSession(ctx, token)
	return err
}

// InspectT3BrowserSession validates a fresh admin JWT and returns the durable
// browser session identity for ongoing workspace authorization.
func (s *AuthService) InspectT3BrowserSession(ctx context.Context, token string) (T3BrowserSession, error) {
	claims, err := s.ValidateToken(token)
	if err != nil {
		return T3BrowserSession{}, err
	}
	identity := T3BrowserSession{
		UserID:       claims.UserID,
		SessionID:    claims.SessionID,
		TokenVersion: claims.TokenVersion,
		BindingHash:  claims.BindingHash,
	}
	if err := s.ValidateT3SessionIdentity(ctx, identity); err != nil {
		return T3BrowserSession{}, err
	}
	return identity, nil
}

// ValidateT3SessionIdentity verifies that the administrator is still active and
// that the originating browser refresh-token family has not been logged out or revoked.
func (s *AuthService) ValidateT3SessionIdentity(ctx context.Context, identity T3BrowserSession) error {
	if identity.UserID <= 0 || identity.SessionID == "" || s.refreshTokenCache == nil {
		return ErrInvalidToken
	}
	user, err := s.userRepo.GetByID(ctx, identity.UserID)
	if err != nil || user == nil || !user.IsActive() || !user.IsAdmin() ||
		identity.TokenVersion != resolvedTokenVersion(user) {
		return ErrInvalidToken
	}
	if s.settingService != nil && s.settingService.IsSessionBindingEnabled(ctx) &&
		identity.BindingHash != "" && sessionBindingHashFromContext(ctx) != identity.BindingHash {
		return ErrInvalidToken
	}

	hashes, err := s.refreshTokenCache.GetFamilyTokenHashes(ctx, identity.SessionID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, hash := range hashes {
		// A consumed token can still be listed in its family set.
		data, err := s.refreshTokenCache.GetRefreshToken(ctx, hash)
		if err == nil && data != nil && data.FamilyID == identity.SessionID && data.UserID == user.ID &&
			data.TokenVersion == identity.TokenVersion && now.Before(data.ExpiresAt) {
			return nil
		}
	}
	return ErrInvalidToken
}
