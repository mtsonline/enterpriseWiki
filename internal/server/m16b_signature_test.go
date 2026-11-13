package server_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWorkflowSignatures: decisions that need a signature are signed with
// a passkey over exactly the decision; the evidence is sealed, verifiable,
// visible only to who may see the case, and every change shows.
func TestWorkflowSignatures(t *testing.T) {
	e := setup(t)
	origin := "http://localhost:" + mustPort(e.srv.URL)
	e.setPluginSettings(t, "mfa.webauthn", map[string]any{"rp_id": "localhost", "origins": []any{origin}})
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "wendy", "password": "Wendy-Passw0rd!", "display_name": "Wendy"}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "gus", "password": "Gus-Passw0rd!x", "display_name": "Gus"}, nil, 201)
	w := e.client()
	w.login("wendy", "Wendy-Passw0rd!")
	g := e.client()
	g.login("gus", "Gus-Passw0rd!x")
	key := newAuthenticator(t, origin)
	enrollWebAuthn(t, w, key, "Laptop", false)

	var methods []struct {
		ID        string `json:"id"`
		Level     string `json:"level"`
		Available bool   `json:"available"`
	}
	w.must("GET", "/api/v1/signature-methods", nil, &methods, 200)
	if len(methods) == 0 || methods[0].ID != "signature.passkey" || !methods[0].Available || methods[0].Level != "advanced" {
		t.Fatalf("methods of wendy: %+v", methods)
	}
	g.must("GET", "/api/v1/signature-methods", nil, &methods, 200)
	if methods[0].Available {
		t.Fatalf("gus has no passkey: %+v", methods)
	}

	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Contracts"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Bad", "body": map[string]any{
		"description": "", "fields": []map[string]any{}, "steps": []map[string]any{
			{"id": "m", "name": "Mail", "type": "email", "sign": "advanced", "next": "done", "email": map[string]any{"to": "a@example.com", "subject": "x", "body": "y"}},
			{"id": "done", "name": "Done", "type": "end"}}}}, nil); code != 400 {
		t.Fatalf("signature on an automatic step: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Contract", "body": map[string]any{
		"description": "", "fields": []map[string]any{{"id": "amount", "label": "Amount", "type": "number"}},
		"steps": []map[string]any{
			{"id": "check", "name": "Check", "type": "task", "sign": "advanced", "assignee": map[string]string{"type": "user", "value": "wendy"},
				"actions": []map[string]any{{"id": "ok", "label": "Agree", "next": "approve"}, {"id": "back", "label": "Not mine", "next": "done", "sign": "none"}}},
			{"id": "approve", "name": "Approve", "type": "approval", "sign": "qualified", "assignee": map[string]string{"type": "user", "value": "wendy"}, "next": "done", "next_else": "done"},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}, &wf, 201)
	var x wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"amount": 5000}}, &x, 201)
	base := fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", x.ID)

	var cs struct {
		Actions []struct {
			ID   string `json:"id"`
			Sign string `json:"sign"`
		} `json:"actions"`
	}
	w.must("GET", base, nil, &cs, 200)
	if len(cs.Actions) < 2 || cs.Actions[0].Sign != "advanced" || cs.Actions[1].Sign != "" {
		t.Fatalf("offered actions: %+v", cs.Actions)
	}
	// without a signature: 428
	if code := w.do("POST", base+"/actions", map[string]any{"action": "ok", "note": "fine"}, nil); code != 428 {
		t.Fatalf("unsigned decision: %d", code)
	}
	sign := func(c *client, action, note string) (string, map[string]any) {
		t.Helper()
		var r struct {
			Token     string `json:"token"`
			Challenge struct {
				Kind   string         `json:"kind"`
				Public map[string]any `json:"public"`
			} `json:"challenge"`
		}
		c.must("POST", base+"/sign", map[string]any{"action": action, "note": note, "method": "signature.passkey"}, &r, 200)
		if r.Challenge.Kind != "webauthn" || r.Token == "" {
			t.Fatalf("challenge %+v", r)
		}
		return r.Token, key.get(r.Challenge.Public)
	}
	// signed "fine" but sent "other": the record differs, nothing happens
	tok, ans := sign(w, "ok", "fine")
	if code := w.do("POST", base+"/actions", map[string]any{"action": "ok", "note": "other", "signature": map[string]any{"token": tok, "answer": ans}}, nil); code != 409 {
		t.Fatalf("changed after signing: %d", code)
	}
	// the token is gone after an attempt
	if code := w.do("POST", base+"/actions", map[string]any{"action": "ok", "note": "fine", "signature": map[string]any{"token": tok, "answer": ans}}, nil); code != 400 {
		t.Fatalf("token reused: %d", code)
	}
	// someone else cannot use wendy's signature
	tok, ans = sign(w, "ok", "fine")
	if code := g.do("POST", base+"/actions", map[string]any{"action": "ok", "note": "fine", "signature": map[string]any{"token": tok, "answer": ans}}, nil); code != 404 && code != 403 {
		t.Fatalf("gus with wendy's signature: %d", code)
	}
	// the organisation seals evidence additionally with its S/MIME certificate
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"from": "wiki@example.com"}}, nil, 200)
	ca := newTestCA(t)
	orgCert, orgKey := ca.issue(t, []string{"wiki@example.com"}, 365*24*time.Hour, true)
	admin.must("POST", "/api/v1/x/signature.smime/certs", map[string]any{"scope": "org", "file": ca.p12(t, orgCert, orgKey, "s"), "password": "s", "seal": true}, nil, 201)
	admin.must("PUT", "/api/v1/x/signature.smime/settings", map[string]any{"mail_signing": true, "on_expired": "unsigned", "personal": true, "seal_evidence": true}, nil, 200)
	tok, ans = sign(w, "ok", "fine")
	var after struct {
		Step   string `json:"step"`
		Events []struct {
			Action    string `json:"action"`
			Signature *int64 `json:"signature"`
		} `json:"events"`
	}
	w.must("POST", base+"/actions", map[string]any{"action": "ok", "note": "fine", "signature": map[string]any{"token": tok, "answer": ans}}, &after, 200)
	var sigID int64
	for _, ev := range after.Events {
		if ev.Action == "ok" && ev.Signature != nil {
			sigID = *ev.Signature
		}
	}
	if after.Step != "approve" || sigID == 0 {
		t.Fatalf("signed decision: %+v", after)
	}
	// the approval needs a qualified signature: the passkey is not enough
	if code := w.do("POST", base+"/sign", map[string]any{"action": "approve", "method": "signature.passkey"}, nil); code != 400 {
		t.Fatalf("passkey for qualified: %d", code)
	}

	// verification and export
	var v struct {
		Valid  bool `json:"valid"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	w.must("GET", fmt.Sprintf("/api/v1/signatures/%d/verify", sigID), nil, &v, 200)
	if !v.Valid || len(v.Checks) < 7 {
		t.Fatalf("verify: %+v", v)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/signatures/%d", sigID), nil, nil, 200)
	if code := g.do("GET", fmt.Sprintf("/api/v1/signatures/%d", sigID), nil, nil); code != 404 {
		t.Fatalf("gus sees the signature: %d", code)
	}
	code, raw := w.rawBody("GET", fmt.Sprintf("/api/v1/signatures/%d/export", sigID), "")
	if code != 200 || !strings.Contains(raw, `"public_key"`) || !strings.Contains(raw, `"ml_dsa_65"`) {
		t.Fatalf("export %d: %.200s", code, raw)
	}
	var exp map[string]any
	_ = json.Unmarshal([]byte(raw), &exp)
	w.must("POST", "/api/v1/signatures/verify", exp, &v, 200)
	if !v.Valid {
		t.Fatalf("export does not verify: %+v", v)
	}
	cmsOK := false
	for _, c := range v.Checks {
		cmsOK = cmsOK || (c.Name == "seal.cms" && c.OK)
	}
	if !cmsOK {
		t.Fatalf("no valid S/MIME seal: %+v", v.Checks)
	}
	// the seal can be checked with OpenSSL over the sealed bytes of the export
	if bin, err := exec.LookPath("openssl"); err == nil {
		seal := exp["seal"].(map[string]any)
		der, _ := base64.StdEncoding.DecodeString(seal["cms"].(string))
		sealed, _ := base64.StdEncoding.DecodeString(exp["sealed"].(string))
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "seal.p7s"), der, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "sealed.json"), sealed, 0o600)
		out, err := exec.Command(bin, "cms", "-verify", "-binary", "-inform", "DER", "-in", filepath.Join(dir, "seal.p7s"), // #nosec G204 -- test
			"-content", filepath.Join(dir, "sealed.json"), "-CAfile", ca.pemFile(t), "-purpose", "any", "-out", os.DevNull).CombinedOutput()
		if err != nil {
			t.Fatalf("openssl cms -verify: %v\n%s", err, out)
		}
	} else if os.Getenv("CI") != "" {
		t.Fatal("openssl is needed for the S/MIME check")
	}
	rec := exp["record"].(map[string]any)
	if rec["decision"].(map[string]any)["note"] != "fine" || fmt.Sprint(rec["data"].(map[string]any)["amount"]) != "5000" {
		t.Fatalf("record: %v", rec)
	}
	rec["decision"].(map[string]any)["note"] = "changed"
	exp["record"] = rec
	w.must("POST", "/api/v1/signatures/verify", exp, &v, 200)
	if v.Valid {
		t.Fatal("changed record still valid")
	}
	// the stored evidence cannot be changed unnoticed either
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE signatures SET signer = jsonb_set(signer, '{name}', '"Mallory"') WHERE id=$1`, sigID); err != nil {
		t.Fatal(err)
	}
	w.must("GET", fmt.Sprintf("/api/v1/signatures/%d/verify", sigID), nil, &v, 200)
	if v.Valid {
		t.Fatal("tampered evidence still valid")
	}
}
