package collab

import (
	"encoding/json"
	"errors"
)

// Awareness messages carry the cursor and the name of an editor. The relay
// does not trust the name a client sends: it replaces the field "user" of
// every state with the signed-in user, and a peer may only send the state
// of its own Yjs client (the first client id it used), so nobody can show a
// cursor under another name or remove the cursors of others.

var errAwareness = errors.New("invalid awareness message")

// lib0 encoding (variable length unsigned integers, length-prefixed data)

func readVarUint(b []byte) (uint64, []byte, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, b[i+1:], nil
		}
	}
	return 0, nil, errAwareness
}

func readBytes(b []byte) ([]byte, []byte, error) {
	n, rest, err := readVarUint(b)
	if err != nil || n > uint64(len(rest)) {
		return nil, nil, errAwareness
	}
	return rest[:n], rest[n:], nil
}

func appendVarUint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendBytes(b, data []byte) []byte {
	return append(appendVarUint(b, uint64(len(data))), data...)
}

// stampAwareness checks and rewrites an awareness message of peer p
// ([1, varUint8Array(update)]). It returns false for messages to drop.
func (p *peer) stampAwareness(msg []byte) ([]byte, bool) {
	update, rest, err := readBytes(msg[1:])
	if err != nil || len(rest) != 0 {
		return nil, false
	}
	n, update, err := readVarUint(update)
	if err != nil || n == 0 || n > 16 {
		return nil, false
	}
	user, _ := json.Marshal(p.user)
	out := appendVarUint(nil, n)
	for range n {
		var client, clock uint64
		var state []byte
		if client, update, err = readVarUint(update); err != nil {
			return nil, false
		}
		if clock, update, err = readVarUint(update); err != nil {
			return nil, false
		}
		if state, update, err = readBytes(update); err != nil || len(state) > 64<<10 {
			return nil, false
		}
		if !p.bound {
			p.client, p.bound = client, true
		} else if client != p.client {
			return nil, false // only the own state
		}
		if string(state) != "null" {
			var st map[string]json.RawMessage
			if json.Unmarshal(state, &st) != nil {
				return nil, false
			}
			st["user"] = user
			if state, err = json.Marshal(st); err != nil {
				return nil, false
			}
		}
		p.clock = clock
		out = appendVarUint(out, client)
		out = appendVarUint(out, clock)
		out = appendBytes(out, state)
	}
	if len(update) != 0 {
		return nil, false
	}
	return appendBytes([]byte{MsgAwareness}, out), true
}
