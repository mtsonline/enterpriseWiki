package pluginmgr

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestCredentialPrefixes(t *testing.T) {
	for in, want := range map[string]string{
		"https://GitHub.com/acme/":      "https://github.com/acme",
		"https://git.example.com":       "https://git.example.com",
		"https://github.com/acme/x.git": "https://github.com/acme/x",
	} {
		if got, err := NormalizeCredentialPrefix(in); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://github.com/acme", "ssh://git@github.com/acme", "/srv/plugins", "https://user:pw@github.com/acme", "https://github.com/acme?x=1", ""} {
		if _, err := NormalizeCredentialPrefix(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	list := []config.SourceCredential{
		{Prefix: "https://github.com/acme", Token: "org"},
		{Prefix: "https://github.com/acme/private", Token: "repo"},
		{Prefix: "https://git.example.com", Token: "host"},
	}
	for src, want := range map[string]string{
		"https://github.com/acme/private.git":      "repo", // the longest prefix wins
		"https://github.com/acme/other":            "org",
		"https://github.com/acme-evil/x":           "", // path boundary
		"https://github.com.evil.example/acme/x":   "", // host boundary
		"http://github.com/acme/x":                 "", // never without TLS
		"https://git.example.com/team/plugin.git":  "host",
		"https://GIT.example.com/team/../x/plugin": "", // not normalisable
	} {
		c, ok := credentialFor(list, src)
		if got := map[bool]string{true: c.Token}[ok]; got != want {
			t.Errorf("%s: %q, want %q", src, got, want)
		}
	}
}

// gitHTTPS serves the bare repositories below root over HTTPS with the real
// git http-backend; requests must carry user:token, others get 401. hits
// records the paths and whether they were authorised.
func gitHTTPS(t *testing.T, root, user, token string) (*httptest.Server, func() []string) {
	t.Helper()
	exe, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("git not installed")
	}
	backend := &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(exe)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	var mu sync.Mutex
	hits := &[]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		mu.Lock()
		*hits = append(*hits, r.URL.Path+map[bool]string{true: " auth", false: " anon"}[ok && u == user && p == token])
		mu.Unlock()
		if !ok || u != user || p != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	// git trusts the test certificate (for its process only)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", ca)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), *hits...)
	}
}

// TestInstallWithSourceCredentials installs from a private repository over
// HTTPS: only with access data, the token never shows in errors.
func TestInstallWithSourceCredentials(t *testing.T) {
	repos := t.TempDir()
	work := filepath.Join(t.TempDir(), "kanban")
	gitRepo(t, work, "acme.kanban")
	if out, err := exec.Command("git", "clone", "-q", "--bare", work, filepath.Join(repos, "team", "kanban.git")).CombinedOutput(); err != nil {
		t.Fatalf("bare: %v %s", err, out)
	}
	const token = "ghp_TestToken1234567890"
	srv, hits := gitHTTPS(t, repos, DefaultCredentialUser, token)
	source := srv.URL + "/team/kanban.git"

	st, err := config.Open(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(fakeRoot(t), st, nil)
	ctx := context.Background()
	opt := InstallOptions{AcceptExternal: true}

	// without access data: refused by the server, no prompt, a clear error
	if _, err := m.Install(ctx, source, "", opt); err == nil || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("without access data: %v", err)
	}
	// wrong token: refused, and the token is not in the message
	if err := st.Update(func(c *config.Config) error {
		_, err := SetCredential(c, config.SourceCredential{Prefix: srv.URL + "/team", Token: "wrong-" + token})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Install(ctx, source, "", opt)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("wrong token: %v", err)
	}
	// the right one (replaces the entry of the same prefix)
	if err := st.Update(func(c *config.Config) error {
		_, err := SetCredential(c, config.SourceCredential{Prefix: srv.URL + "/team/", Token: token, Note: "team"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(st.Get().Plugins.Credentials); n != 1 {
		t.Fatalf("%d entries for one prefix", n)
	}
	res, err := m.Install(ctx, source, "", opt)
	if err != nil || res.ID != "acme.kanban" {
		t.Fatalf("with access data: %+v %v", res, err)
	}
	authorised := false
	for _, h := range hits() {
		authorised = authorised || strings.HasSuffix(h, " auth")
	}
	if !authorised {
		t.Fatalf("no authorised request: %v", hits())
	}
	// a prefix naming the repository itself also covers its ".git" address
	// (found by tests/plugins/lifecycle.sh against GitHub)
	if err := st.Update(func(c *config.Config) error {
		c.Plugins.Credentials = nil
		_, err := SetCredential(c, config.SourceCredential{Prefix: srv.URL + "/team/kanban", Token: token})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if res, err := m.Install(ctx, source, "", opt); err != nil || res.ID != "acme.kanban" {
		t.Fatalf("prefix of the repository itself: %+v %v", res, err)
	}
	// access data of another prefix are not sent
	if _, err := m.Install(ctx, srv.URL+"/other/kanban.git", "", opt); err == nil {
		t.Fatal("installed from a prefix without access data")
	}
}

// The header with the token goes to git through the environment, never
// through arguments (the process list) or the address.
func TestGitAuthEnv(t *testing.T) {
	c := config.SourceCredential{Prefix: "https://git.example.com/team", Token: "secret-token"}
	env := strings.Join(gitAuthEnv(c, "https://git.example.com/team/kanban.git"), "\n")
	for _, want := range []string{"http.https://git.example.com/team/kanban.git/.extraHeader", "http.followRedirects", "credential.helper"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s in %s", want, env)
		}
	}
	if strings.Contains(env, "secret-token") {
		t.Error("token in plain text (it is sent base64 encoded in the header)")
	}
	encoded := strings.SplitN(strings.Split(env, "GIT_CONFIG_VALUE_0=Authorization: Basic ")[1], "\n", 2)[0]
	msg := redact("fatal: header Authorization: Basic "+encoded+" secret-token", c)
	if strings.Contains(msg, "secret-token") || strings.Contains(msg, encoded) {
		t.Errorf("not redacted: %s", msg)
	}
	if _, err := SetCredential(&config.Config{}, config.SourceCredential{Prefix: "https://git.example.com", Token: "a\nb"}); err == nil {
		t.Error("token with a line break accepted (header injection)")
	}
	if _, err := SetCredential(&config.Config{}, config.SourceCredential{Prefix: "https://git.example.com", Username: "a:b", Token: "x"}); err == nil {
		t.Error("user name with a colon accepted")
	}
}
