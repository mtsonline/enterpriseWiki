package auth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityViewHidesSessionData(t *testing.T) {
	x := &ExternalIdentity{Provider: "oidc", Subject: "s", Username: "u", Email: "u@example.com",
		SessionID: "session-123", LogoutHint: "eyJ.id-token.sig", TrustEmail: true, NoJIT: true}
	raw, err := json.Marshal(x.View())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"session-123", "id-token", "trust", "jit"} {
		if strings.Contains(strings.ToLower(string(raw)), secret) {
			t.Fatalf("view shows %q: %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), `"provider":"oidc"`) || !strings.Contains(string(raw), `"groups":[]`) {
		t.Fatalf("view %s", raw)
	}
}
