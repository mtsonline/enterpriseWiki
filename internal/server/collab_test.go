package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// lib0 helpers for awareness messages in the test
func varUint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func awareness(client, clock uint64, state string) []byte {
	u := varUint(varUint(varUint(nil, 1), client), clock)
	u = append(varUint(u, uint64(len(state))), state...)
	return append(varUint([]byte{1}, uint64(len(u))), u...)
}

func (c *client) dial(t *testing.T, path string, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.e.srv.URL, "http")+path, &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: h})
}

func readMsg(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return typ, b
}

// readBinary skips control messages (peers) and returns the next relayed message.
func readBinary(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	for {
		typ, b := readMsg(t, c)
		if typ == websocket.MessageBinary {
			return b
		}
	}
}

func TestCollab(t *testing.T) {
	e := setup(t)
	ctx := identity.With(context.Background(), identity.CLI())
	if _, err := e.app.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: "reader", Password: "Reader-Passw0rd!", DisplayName: "Reader"}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var cat, el struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Live"}}, &cat, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Live", "body": htmlBody("<p>x</p>")}, &el, 201)
	path := "/api/v1/collab/elements/" + strconv.FormatInt(el.ID, 10) + "/en"
	origin := e.srv.URL

	// guests, readers without the right to edit, other origins and API keys are refused
	if _, res, err := e.client().dial(t, path, origin); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("guest: %v %v", err, res)
	}
	reader := e.client()
	reader.login("reader", "Reader-Passw0rd!")
	if _, res, err := reader.dial(t, path, origin); err == nil || res == nil || (res.StatusCode != 403 && res.StatusCode != 404) {
		t.Fatalf("reader: %v %v", err, res)
	}
	if _, res, err := admin.dial(t, path, "https://evil.example"); err == nil || res == nil || res.StatusCode != 403 {
		t.Fatalf("cross-origin: %v %v", err, res)
	}
	if _, res, err := admin.dial(t, "/api/v1/collab/elements/"+strconv.FormatInt(el.ID, 10)+"/xx", origin); err == nil || res == nil || res.StatusCode != 400 {
		t.Fatalf("bad language: %v %v", err, res)
	}

	// two editors: peers, relaying, awareness with the real name
	a, _, err := admin.dial(t, path, origin)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseNow()
	var ctl struct {
		Type   string `json:"type"`
		Peers  int    `json:"peers"`
		Leader bool   `json:"leader"`
		First  bool   `json:"first"`
		Users  []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	_, b := readMsg(t, a)
	if err := json.Unmarshal(b, &ctl); err != nil || ctl.Type != "peers" || !ctl.First || !ctl.Leader || ctl.Peers != 0 {
		t.Fatalf("first control: %s", b)
	}
	admin2 := e.client()
	admin2.login("admin", adminPW)
	bConn, _, err := admin2.dial(t, path, origin)
	if err != nil {
		t.Fatal(err)
	}
	defer bConn.CloseNow()
	_, b = readMsg(t, bConn)
	if err := json.Unmarshal(b, &ctl); err != nil || ctl.First || ctl.Leader || ctl.Peers != 1 || ctl.Users[0].Name != "Admin" {
		t.Fatalf("second control: %s", b)
	}
	_, b = readMsg(t, a) // a learns about b
	wctx, wc := context.WithTimeout(context.Background(), 5*time.Second)
	defer wc()
	sync := []byte{0, 0, 1, 0} // sync step 1 (empty state vector)
	if err := bConn.Write(wctx, websocket.MessageBinary, sync); err != nil {
		t.Fatal(err)
	}
	if got := readBinary(t, a); string(got) != string(sync) {
		t.Fatalf("relayed %v", got)
	}
	// awareness: the name is replaced by the signed-in user
	if err := a.Write(wctx, websocket.MessageBinary, awareness(42, 1, `{"user":{"name":"Mallory"},"cursor":null}`)); err != nil {
		t.Fatal(err)
	}
	got := readBinary(t, bConn)
	if !strings.Contains(string(got), `"name":"Admin"`) || strings.Contains(string(got), "Mallory") {
		t.Fatalf("awareness not stamped: %q", got)
	}
	// the state of another client id is not relayed; unknown types neither
	_ = a.Write(wctx, websocket.MessageBinary, awareness(43, 1, `null`))
	_ = a.Write(wctx, websocket.MessageBinary, []byte{9, 1, 2})
	_ = a.Write(wctx, websocket.MessageBinary, sync)
	if got := readBinary(t, bConn); string(got) != string(sync) {
		t.Fatalf("expected only the sync message, got %v", got)
	}
	// leaving removes the cursor at once and updates the peers
	_ = a.Close(websocket.StatusNormalClosure, "")
	got = readBinary(t, bConn)
	if !strings.HasSuffix(string(got), "null") {
		t.Fatalf("no removal of the cursor: %q", got)
	}
}
