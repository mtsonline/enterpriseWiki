package collab

import (
	"strings"
	"testing"
)

func msg(client, clock uint64, state string) []byte {
	u := appendVarUint(appendVarUint(appendVarUint(nil, 1), client), clock)
	return appendBytes([]byte{MsgAwareness}, appendBytes(u, []byte(state)))
}

func TestStampAwareness(t *testing.T) {
	p := &peer{user: User{ID: 7, Name: "Anna"}}
	out, ok := p.stampAwareness(msg(300, 2, `{"user":{"name":"Eve","id":1},"cursor":{"anchor":1}}`))
	if !ok || !strings.Contains(string(out), `"user":{"id":7,"name":"Anna"}`) || !strings.Contains(string(out), `"cursor"`) {
		t.Fatalf("%q %v", out, ok)
	}
	if p.client != 300 || p.clock != 2 {
		t.Fatalf("client %d clock %d", p.client, p.clock)
	}
	if _, ok := p.stampAwareness(msg(301, 1, `{}`)); ok {
		t.Error("state of another client accepted")
	}
	if _, ok := p.stampAwareness(msg(300, 3, `null`)); !ok {
		t.Error("removal of the own state refused")
	}
	for _, bad := range [][]byte{{1}, {1, 5, 1}, {1, 3, 1, 1, 1}, msg(300, 1, `{"a":`), append(msg(300, 1, `{}`), 0)} {
		if _, ok := p.stampAwareness(bad); ok {
			t.Errorf("invalid message accepted: %v", bad)
		}
	}
}

func FuzzStampAwareness(f *testing.F) {
	f.Add(msg(1, 1, `{"user":{}}`))
	f.Add([]byte{1, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) == 0 {
			return
		}
		p := &peer{user: User{ID: 1, Name: "x"}}
		out, ok := p.stampAwareness(b)
		if ok && (len(out) == 0 || out[0] != MsgAwareness) {
			t.Fatalf("bad output %v", out)
		}
	})
}
