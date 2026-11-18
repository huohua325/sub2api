package adminaccess

import "testing"

func TestExperimentConcurrencyPermissionsAreIndependent(t *testing.T) {
	const path = "/api/v1/admin/users/:id/experiment-concurrency"
	for _, permission := range []string{"users.read", "users.quotas.write", "users.purpose_quotas.reset", "users.experiment_concurrency.read"} {
		if RouteAllowed(Policy{GlobalPermissions: []string{permission}}, "PUT", path) {
			t.Fatal("experiment admission inherited unrelated permission: " + permission)
		}
	}
	if !RouteAllowed(Policy{GlobalPermissions: []string{"users.experiment_concurrency.write"}}, "PUT", path) ||
		RouteAllowed(Policy{GlobalPermissions: []string{"users.experiment_concurrency.write"}}, "GET", path) {
		t.Fatal("read and write grants must remain independent")
	}
}
