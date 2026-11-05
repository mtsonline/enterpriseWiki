// Package events is a small pub/sub bus used for cache invalidation.
//
// In single mode events are dispatched in-process. In cluster mode they are
// broadcast to all nodes via PostgreSQL LISTEN/NOTIFY, so no additional
// infrastructure (Redis, NATS) is required for a multi-node deployment.
package events

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const sharedChannel = "ewiki_events"

// SharedTopics concern data of the shared schema (users, groups, roles):
// they reach the buses of all sites.
var SharedTopics = map[string]bool{"auth": true}

// Bus dispatches events to subscribers. Each site has its own bus.
type Bus struct {
	mu      sync.RWMutex
	subs    map[string][]func(payload string)
	pool    *pgxpool.Pool
	cluster bool
	log     *slog.Logger
	channel string
}

var (
	busesMu sync.RWMutex
	buses   []*Bus
)

// New creates a bus for the site "main". If cluster is true, events are
// sent via NOTIFY.
func New(pool *pgxpool.Pool, cluster bool, log *slog.Logger) *Bus {
	return NewSite(pool, cluster, log, "main")
}

// NewSite creates the bus of a site.
func NewSite(pool *pgxpool.Pool, cluster bool, log *slog.Logger, site string) *Bus {
	b := &Bus{subs: map[string][]func(string){}, pool: pool, cluster: cluster, log: log, channel: sharedChannel + "_" + site}
	busesMu.Lock()
	buses = append(buses, b)
	busesMu.Unlock()
	return b
}

// Close stops delivering shared events to this bus.
func (b *Bus) Close() {
	busesMu.Lock()
	defer busesMu.Unlock()
	for i, x := range buses {
		if x == b {
			buses = append(buses[:i], buses[i+1:]...)
			return
		}
	}
}

func dispatchAll(topic, payload string) {
	busesMu.RLock()
	all := append([]*Bus{}, buses...)
	busesMu.RUnlock()
	for _, b := range all {
		b.dispatch(topic, payload)
	}
}

// Subscribe registers fn for topic.
func (b *Bus) Subscribe(topic string, fn func(payload string)) {
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], fn)
	b.mu.Unlock()
}

func (b *Bus) dispatch(topic, payload string) {
	b.mu.RLock()
	fns := b.subs[topic]
	b.mu.RUnlock()
	for _, f := range fns {
		f(payload)
	}
}

// Publish sends an event to all nodes (including this one).
func (b *Bus) Publish(ctx context.Context, topic, payload string) {
	shared := SharedTopics[topic]
	if !b.cluster || b.pool == nil {
		if shared {
			dispatchAll(topic, payload)
		} else {
			b.dispatch(topic, payload)
		}
		return
	}
	ch := b.channel
	if shared {
		ch = sharedChannel
	}
	if _, err := b.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, ch, topic+"|"+payload); err != nil {
		b.log.Error("notify failed, dispatching locally", "err", err)
		if shared {
			dispatchAll(topic, payload)
		} else {
			b.dispatch(topic, payload)
		}
	}
}

// Run listens for cluster notifications until ctx is done. No-op in single mode.
func (b *Bus) Run(ctx context.Context) {
	if !b.cluster || b.pool == nil {
		return
	}
	for ctx.Err() == nil {
		if err := b.listen(ctx); err != nil && ctx.Err() == nil {
			b.log.Error("event listener", "err", err)
			// Missed events while disconnected: invalidate everything.
			b.dispatch("*", "")
			time.Sleep(2 * time.Second)
		}
	}
}

func (b *Bus) listen(ctx context.Context) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+sharedChannel+"; LISTEN "+b.channel); err != nil {
		return err
	}
	b.dispatch("*", "")
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		topic, payload, _ := strings.Cut(n.Payload, "|")
		b.dispatch(topic, payload)
	}
}
