//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistrationMergePreservesExactAllowlistAndBlacklist(t *testing.T) {
	for _, tc := range []struct {
		name    string
		email   string
		blocked bool
		want    error
	}{
		{name: "approved student bypasses domain restriction", email: "student@school.edu"},
		{name: "unapproved email remains denied", email: "other@school.edu", want: ErrEmailRegistrationNotApproved},
		{name: "explicit blacklist overrides student approval", email: "student@school.edu", blocked: true, want: ErrEmailBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]string{
				SettingKeyRegistrationEmailAllowlistEnabled: "true",
				SettingKeyRegistrationEmailAllowlist:        `["student@school.edu"]`,
				SettingKeyRegistrationEmailSuffixWhitelist:  `["@unrelated.example"]`,
			}
			if tc.blocked {
				settings[SettingKeyRegistrationEmailBlacklist] = `["student@school.edu"]`
			}
			svc := newAuthService(&userRepoStub{}, settings, nil, nil)
			for name, check := range map[string]func(context.Context, string) error{
				"email policy":       svc.validateRegistrationEmailPolicy,
				"registration quota": svc.validateRegistrationEmailQuota,
			} {
				t.Run(name, func(t *testing.T) {
					err := check(context.Background(), tc.email)
					if tc.want == nil {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, tc.want)
					}
				})
			}
		})
	}
}
