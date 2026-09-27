package authn

import (
	"slices"
	"testing"
)

func TestClaimsSeparatesClientRolesFromRealmRoles(t *testing.T) {
	var claims Claims
	claims.RealmAccess.Roles = []string{"admin", "realm-viewer"}
	claims.ResourceAccess = map[string]struct {
		Roles []string `json:"roles"`
	}{
		"eagle-system": {Roles: []string{"system-admin", "editor"}},
		"other":        {Roles: []string{"foreign-admin"}},
	}

	if got := claims.ClientRoles("eagle-system"); !slices.Equal(got, []string{"system-admin", "editor"}) {
		t.Fatalf("ClientRoles = %v", got)
	}
	wantAll := []string{
		"realm:admin",
		"realm:realm-viewer",
		"client:eagle-system:system-admin",
		"client:eagle-system:editor",
	}
	if got := claims.Roles("eagle-system"); !slices.Equal(got, wantAll) {
		t.Fatalf("Roles = %v, want %v", got, wantAll)
	}
}
