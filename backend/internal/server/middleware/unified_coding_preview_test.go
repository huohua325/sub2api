package middleware

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUnifiedCodingPreviewGatewayAuthorization(t *testing.T) {
	id := int64(50)
	for _, subscriptionType := range []string{service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription} {
		key := &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformComposite, UsageKind: service.UsageKindCoding, IsExclusive: true, SubscriptionType: subscriptionType}, User: &service.User{ID: 2, Role: service.RoleUser}}
		require.False(t, validateAPIKeyGroupAllowed(key), "direct calls must reject uninvited users")
		key.User.AllowedGroups = []int64{id}
		require.True(t, validateAPIKeyGroupAllowed(key))
		key.User.AllowedGroups = nil
		require.False(t, validateAPIKeyGroupAllowed(key), "revocation applies to existing keys")
		key.User.Role = service.RoleAdmin
		require.True(t, validateAPIKeyGroupAllowed(key))
	}
}
