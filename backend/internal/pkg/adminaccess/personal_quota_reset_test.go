package adminaccess

import "testing"

func TestPersonalQuotaResetRequiresDedicatedGlobalPermission(t *testing.T) {
	const path = "/api/v1/admin/users/:id/purpose-quota-resets"
	for _, policy := range []Policy{{}, {GlobalPermissions: []string{"users.quotas.write"}}, {GlobalPermissions: []string{"users.purpose_quotas.read"}}} {
		if RouteAllowed(policy, "POST", path) {
			t.Fatal("personal reset inherited an unrelated permission")
		}
	}
	if !RouteAllowed(Policy{GlobalPermissions: []string{"users.purpose_quotas.reset"}}, "POST", path) {
		t.Fatal("explicit personal-reset permission was denied")
	}
	if RouteAllowed(Policy{GlobalPermissions: []string{"users.purpose_quotas.reset"}}, "GET", path) {
		t.Fatal("write permission leaked audit history")
	}
}
