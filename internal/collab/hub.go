// Package collab relays the messages of live editing (Yjs sync and
// awareness, y-protocols) between the editors of one document.
//
// The server keeps no document state: the first editor of a room starts
// from the saved content, later editors receive the state from the others.
// Messages are relayed unchanged after a type check; the server only adds
// JSON control messages (text frames) with the peers of the room.
//
// Limits (security): editors must keep their right to edit (checked again
// periodically), message size, message rate, peers per room and the send
// queue per peer are bounded; slow peers are disconnected.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Message types of y-protocols (first byte of a binary message) that are
// relayed: sync, awareness and saved (custom: "a new revision was saved").
const (
	MsgSync      = 0
	MsgAwareness = 1
	MsgSaved     = 3
)

// Limits.
const (
	MaxMessage   = 4 << 20 // bytes per message (the initial sync of a large page)
	MaxPeers     = 50      // persons per document
	MaxPerUser   = 5       // connections of one person per document (tabs, devices)
	queueSize    = 256     // messages waiting per peer
	ratePerSec   = 100     // messages per second and peer (burst: 2x)
	RecheckEvery = 30 * time.Second
	pingEvery    = 25 * time.Second
)

// User identifies an editor.
type User struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Hub holds the rooms (one per document).
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*room
	seq   uint64
}

// NewHub creates a hub.
func NewHub() *Hub { return &Hub{rooms: map[string]*room{}} }

type room struct {
	peers map[*peer]bool
}

type peer struct {
	user User
	seq  uint64
	// Yjs client id and last awareness clock (see stampAwareness)
	client, clock uint64
	bound         bool
	send          chan frame
	quit          chan struct{}
	once          sync.Once
}

type frame struct {
	typ  websocket.MessageType
	data []byte
}

func (p *peer) close() { p.once.Do(func() { close(p.quit) }) }

// ErrTooManyConnections: the person has MaxPerUser connections to the room.
var ErrTooManyConnections = errors.New("too many connections of this person to the document")

// ErrFull is returned when a room has MaxPeers editors.
var ErrFull = errors.New("too many editors of this document")

// Peers returns the number of editors in a room.
func (h *Hub) Peers(key string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[key]; r != nil {
		return len(r.peers)
	}
	return 0
}

func (h *Hub) join(key string, u User) (*peer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[key]
	if r == nil {
		r = &room{peers: map[*peer]bool{}}
		h.rooms[key] = r
	}
	// S-06: persons are counted, not connections; one person cannot fill
	// a room with connections
	mine, persons := 0, map[int64]bool{}
	for q := range r.peers {
		persons[q.user.ID] = true
		if q.user.ID == u.ID {
			mine++
		}
	}
	if mine >= MaxPerUser {
		return nil, ErrTooManyConnections
	}
	if !persons[u.ID] && len(persons) >= MaxPeers {
		return nil, ErrFull
	}
	h.seq++
	p := &peer{user: u, seq: h.seq, send: make(chan frame, queueSize), quit: make(chan struct{})}
	r.peers[p] = true
	h.announce(r)
	return p, nil
}

func (h *Hub) leave(key string, p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[key]
	if r == nil || !r.peers[p] {
		return
	}
	delete(r.peers, p)
	p.close()
	if len(r.peers) == 0 {
		delete(h.rooms, key)
		return
	}
	if p.bound {
		// the cursor disappears at once (not only after the awareness timeout)
		u := appendVarUint(appendVarUint(appendVarUint(nil, 1), p.client), p.clock+1)
		gone := appendBytes([]byte{MsgAwareness}, appendBytes(u, []byte("null")))
		for o := range r.peers {
			h.deliver(o, frame{websocket.MessageBinary, gone})
		}
	}
	h.announce(r)
}

// control is the JSON message about the peers of a room. Leader is the
// editor that joined first (it saves automatically where a type does so).
type control struct {
	Type   string `json:"type"`
	Peers  int    `json:"peers"`
	Leader bool   `json:"leader"`
	First  bool   `json:"first"`
	Users  []User `json:"users"`
}

// announce sends every peer the list of the others (h.mu held).
func (h *Hub) announce(r *room) {
	all := make([]*peer, 0, len(r.peers))
	for p := range r.peers {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].seq < all[j].seq })
	for _, p := range all {
		users := []User{}
		for _, o := range all {
			if o != p {
				users = append(users, o.user)
			}
		}
		b, _ := json.Marshal(control{Type: "peers", Peers: len(all) - 1, Leader: all[0] == p, First: len(all) == 1, Users: users})
		h.deliver(p, frame{websocket.MessageText, b})
	}
}

// deliver queues a frame; a peer whose queue is full is disconnected.
func (h *Hub) deliver(p *peer, f frame) {
	select {
	case p.send <- f:
	default:
		p.close()
	}
}

func (h *Hub) broadcast(key string, from *peer, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[key]
	if r == nil {
		return
	}
	for p := range r.peers {
		if p != from {
			h.deliver(p, frame{websocket.MessageBinary, data})
		}
	}
}

// Serve relays the messages of one editor until the connection ends, the
// context is cancelled or allowed reports false (checked every RecheckEvery).
func (h *Hub) Serve(ctx context.Context, c *websocket.Conn, key string, u User, allowed func(context.Context) bool) error {
	c.SetReadLimit(MaxMessage)
	p, err := h.join(key, u)
	if err != nil {
		_ = c.Close(websocket.StatusTryAgainLater, err.Error())
		return err
	}
	defer h.leave(key, p)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// writer
	go func() {
		defer cancel()
		ping := time.NewTicker(pingEvery)
		recheck := time.NewTicker(RecheckEvery)
		defer ping.Stop()
		defer recheck.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.quit:
				_ = c.Close(websocket.StatusPolicyViolation, "too slow")
				return
			case f := <-p.send:
				wctx, wc := context.WithTimeout(ctx, 10*time.Second)
				err := c.Write(wctx, f.typ, f.data)
				wc()
				if err != nil {
					return
				}
			case <-ping.C:
				pctx, pc := context.WithTimeout(ctx, 10*time.Second)
				err := c.Ping(pctx)
				pc()
				if err != nil {
					return
				}
			case <-recheck.C:
				if !allowed(ctx) {
					_ = c.Close(websocket.StatusPolicyViolation, "no longer allowed to edit")
					return
				}
			}
		}
	}()

	// reader
	tokens, last := float64(2*ratePerSec), time.Now()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) || websocket.CloseStatus(err) != -1 {
				return nil
			}
			return err
		}
		now := time.Now()
		tokens = min(2*ratePerSec, tokens+now.Sub(last).Seconds()*ratePerSec)
		last = now
		if tokens < 1 {
			_ = c.Close(websocket.StatusPolicyViolation, "too many messages")
			return nil
		}
		tokens--
		if typ != websocket.MessageBinary || len(data) == 0 {
			continue // clients send binary y-protocols messages only
		}
		switch data[0] {
		case MsgSync:
			h.broadcast(key, p, data)
		case MsgSaved:
			// [3, rev, user]: the user is the sender, not what it claims
			if rev, _, err := readVarUint(data[1:]); err == nil {
				h.broadcast(key, p, appendVarUint(appendVarUint([]byte{MsgSaved}, rev), uint64(max(p.user.ID, 0))))
			}
		case MsgAwareness:
			h.mu.Lock() // client and clock of p are read by leave
			out, ok := p.stampAwareness(data)
			h.mu.Unlock()
			if ok {
				h.broadcast(key, p, out)
			}
		}
	}
}
