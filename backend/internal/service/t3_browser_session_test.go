//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type t3RefreshFamily struct {
	RefreshTokenCache
	active *RefreshTokenData
}

func (r *t3RefreshFamily) GetFamilyTokenHashes(context.Context, string) ([]string, error) {
	return []string{"stale-hash", "active-hash"}, nil
}

func (r *t3RefreshFamily) GetRefreshToken(_ context.Context, hash string) (*RefreshTokenData, error) {
	if hash != "active-hash" || r.active == nil {
		return nil, ErrRefreshTokenNotFound
	}
	return r.active, nil
}

func TestNativeT3RequiresLiveAdminSession(t *testing.T) {
	ctx := context.Background()
	user := &User{ID: 12, Email: "admin@example.test", Role: RoleAdmin, Status: StatusActive}
	family := &t3RefreshFamily{active: &RefreshTokenData{
		UserID: user.ID, FamilyID: "family", TokenVersion: resolvedTokenVersion(user), ExpiresAt: time.Now().Add(time.Hour),
	}}
	auth := &AuthService{
		cfg:      &config.Config{JWT: config.JWTConfig{Secret: "t3-test-only-secret", AccessTokenExpireMinutes: 5}},
		userRepo: &userRepoStub{user: user}, refreshTokenCache: family,
	}
	access, err := auth.generateAccessToken(user, "family", "")
	require.NoError(t, err)
	require.NoError(t, auth.ValidateT3BrowserSession(ctx, access))

	user.Role = RoleUser
	require.ErrorIs(t, auth.ValidateT3BrowserSession(ctx, access), ErrInvalidToken)
	user.Role = RoleAdmin
	family.active = nil
	require.ErrorIs(t, auth.ValidateT3BrowserSession(ctx, access), ErrInvalidToken)
}
