package pluginmgr

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cluster connects the manager to the other nodes (nil fields: single node).
type Cluster struct {
	Enabled   func() bool // cluster mode
	DB        *pgxpool.Pool
	Publish   func(ctx context.Context, topic, payload string)
	Subscribe func(topic string, fn func(payload string))
}

const topicRestart = "plugins.restart"

// Attach connects the manager to the database and the cluster bus.
func (m *Manager) Attach(c Cluster, log *slog.Logger) {
	m.cluster = c
	if log != nil {
		m.log = log
	}
	if c.Subscribe != nil {
		c.Subscribe(topicRestart, func(string) { go m.rollingRestart(context.Background()) })
	}
}

// RestartPending reports whether a finished build waits for a restart.
func (m *Manager) RestartPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartPending
}

// RestartAll restarts this server, in a cluster all nodes one after the
// other (each waits until the previous one is ready again).
func (m *Manager) RestartAll(ctx context.Context) {
	if m.cluster.Enabled != nil && m.cluster.Enabled() && m.cluster.Publish != nil {
		m.cluster.Publish(ctx, topicRestart, "")
		return
	}
	_ = m.ScheduleRestart()
}

func nodeName() string {
	h, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", h, os.Getpid()) // the pid stays the same across the re-exec
}

// rollingRestart waits for the restart lease (one node at a time) and
// restarts. The new process releases the lease once it accepts requests
// (ReleaseRestartLease); a lease of a node that died expires.
func (m *Manager) rollingRestart(ctx context.Context) {
	if m.cluster.DB == nil {
		_ = m.ScheduleRestart()
		return
	}
	me := nodeName()
	for i := 0; i < 360; i++ { // at most 30 minutes
		var holder string
		err := m.cluster.DB.QueryRow(ctx, `INSERT INTO cluster_lease(name, holder, until) VALUES ('restart', $1, now() + interval '3 minutes')
			ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, until = EXCLUDED.until
			WHERE cluster_lease.until < now() OR cluster_lease.holder = EXCLUDED.holder RETURNING holder`, me).Scan(&holder)
		if err == nil && holder == me {
			m.log.Info("restarting (rolling restart of the cluster)", "node", me)
			_ = m.ScheduleRestart()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	m.log.Error("rolling restart: no turn within 30 minutes; restart this node manually", "node", me)
}

// ReleaseRestartLease is called after the start: once the server accepts
// connections on addr, the next node may restart.
func ReleaseRestartLease(ctx context.Context, db *pgxpool.Pool, addr string) {
	if db == nil {
		return
	}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		for i := 0; i < 120; i++ {
			if c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second); err == nil {
				c.Close()
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	_, _ = db.Exec(ctx, `DELETE FROM cluster_lease WHERE name = 'restart' AND holder = $1`, nodeName())
}

// Watch notices plugins copied into the plugin folder (or installed with
// the CLI) and builds them once the folder no longer changes.
func (m *Manager) Watch(ctx context.Context, every time.Duration) {
	dir := m.PluginsDir()
	m.mu.Lock()
	m.fp = fingerprint(dir)
	m.mu.Unlock()
	var seen string
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		fp := fingerprint(dir)
		m.mu.Lock()
		changed, running := fp != m.fp, m.build.Running
		m.mu.Unlock()
		if !changed || running {
			seen = ""
			continue
		}
		if fp != seen { // still being copied? wait for one more round without changes
			seen = fp
			continue
		}
		seen = ""
		if !m.CanBuild() {
			m.mu.Lock()
			m.fp = fp
			m.mu.Unlock()
			continue
		}
		trigger := m.takeTrigger()
		if trigger == "" {
			trigger = "drop"
		}
		if len(m.Pending()) == 0 && !m.layout.Image() {
			m.mu.Lock()
			m.fp = fingerprint(dir)
			m.mu.Unlock()
			continue
		}
		m.log.Info("plugin folder changed, building", "dir", dir, "trigger", trigger)
		if _, err := m.StartBuild(ctx, trigger); err != nil {
			m.log.Error("plugin build", "err", err)
			m.mu.Lock()
			m.fp = fingerprint(dir)
			m.mu.Unlock()
		}
	}
}
