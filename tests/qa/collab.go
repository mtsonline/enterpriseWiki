package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Messages of the live editing relay (first byte, see internal/collab).
const (
	msgSync      = 0
	msgAwareness = 1
)

var wsHTTP = &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 0, DisableKeepAlives: true}}

// dialCollab opens the live editing connection of a page. origin "" means
// the wiki's own origin.
func dialCollab(ctx context.Context, c *Client, element int64, lang, origin string) (*websocket.Conn, int, error) {
	u := "ws" + strings.TrimPrefix(c.Base, "http") + fmt.Sprintf("/api/v1/collab/elements/%d/%s", element, lang)
	h := http.Header{}
	if c.Cookie != "" {
		h.Set("Cookie", "ewiki_session="+c.Cookie)
	}
	if c.Bearer != "" {
		h.Set("Authorization", "Bearer "+c.Bearer)
	}
	if origin == "" {
		origin = c.Base
	}
	h.Set("Origin", origin)
	if c.IP != "" {
		h.Set("X-Forwarded-For", c.IP)
	}
	conn, res, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h, HTTPClient: wsHTTP})
	st := 0
	if res != nil {
		st = res.StatusCode
	}
	if err != nil {
		return nil, st, err
	}
	conn.SetReadLimit(8 << 20)
	return conn, st, nil
}

func appendVarUint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func readVarUint(b []byte) (uint64, []byte, bool) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, b[i+1:], true
		}
	}
	return 0, nil, false
}

// qaMagic marks the payload of the test messages.
var qaMagic = []byte("QAEW")

// syncMsg builds a sync message (y-protocols "update") whose payload
// carries the sender and the time of sending: [0, 2, len, magic, sender, ns, padding].
func syncMsg(sender uint64, pad int) []byte {
	p := append([]byte{}, qaMagic...)
	p = binary.BigEndian.AppendUint64(p, sender)
	p = binary.BigEndian.AppendUint64(p, uint64(time.Now().UnixNano())) // #nosec G115 -- a time after 1970
	p = append(p, make([]byte, pad)...)
	out := []byte{msgSync, 2}
	out = appendVarUint(out, uint64(len(p)))
	return append(out, p...)
}

// parseSync returns the sender and the age of a test message.
func parseSync(b []byte) (sender uint64, age time.Duration, ok bool) {
	if len(b) < 2 || b[0] != msgSync {
		return 0, 0, false
	}
	_, p, good := readVarUint(b[2:])
	if !good || len(p) < 20 || string(p[:4]) != string(qaMagic) {
		return 0, 0, false
	}
	sender = binary.BigEndian.Uint64(p[4:12])
	sent := int64(binary.BigEndian.Uint64(p[12:20])) // #nosec G115 -- written by syncMsg
	return sender, time.Duration(time.Now().UnixNano() - sent), true
}

// awarenessMsg builds the awareness state of one Yjs client claiming name.
func awarenessMsg(client, clock uint64, name string) []byte {
	state, _ := json.Marshal(map[string]any{"user": map[string]any{"id": 1, "name": name}, "cursor": map[string]int{"anchor": 1, "head": 1}})
	u := appendVarUint(nil, 1)
	u = appendVarUint(u, client)
	u = appendVarUint(u, clock)
	u = appendVarUint(u, uint64(len(state)))
	u = append(u, state...)
	out := appendVarUint([]byte{msgAwareness}, uint64(len(u)))
	return append(out, u...)
}

// parseAwarenessUser returns the name in the first state of an awareness message.
func parseAwarenessUser(b []byte) (string, bool) {
	if len(b) < 2 || b[0] != msgAwareness {
		return "", false
	}
	_, u, ok := readVarUint(b[1:])
	if !ok {
		return "", false
	}
	if _, u, ok = readVarUint(u); !ok { // number of states
		return "", false
	}
	if _, u, ok = readVarUint(u); !ok { // client
		return "", false
	}
	if _, u, ok = readVarUint(u); !ok { // clock
		return "", false
	}
	n, u, ok := readVarUint(u)
	if !ok || n > uint64(len(u)) {
		return "", false
	}
	var st struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if json.Unmarshal(u[:n], &st) != nil {
		return "", false
	}
	return st.User.Name, true
}
