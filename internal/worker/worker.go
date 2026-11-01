// Package worker runs periodic background jobs of the core and of plugins
// (sending e-mail, workflow timers, search index synchronization).
//
// In cluster mode a job runs on one node at a time (PostgreSQL advisory lock
// per job name); jobs must nevertheless be safe to run concurrently
// (use FOR UPDATE SKIP LOCKED).
package worker

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Job is a periodic job.
type Job struct {
	Name     string
	Interval time.Duration
	// Active reports whether the job should run (e.g. its plugin is enabled).
	Active func() bool
	Run    func(ctx context.Context) error
}

// Registry holds the jobs.
type Registry struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	pool    *pgxpool.Pool
	log     *slog.Logger
	cluster func() bool
	running map[string]*sync.Mutex
}

// New creates a registry.
func New(pool *pgxpool.Pool, log *slog.Logger, cluster func() bool) *Registry {
	return &Registry{jobs: map[string]*Job{}, pool: pool, log: log, cluster: cluster, running: map[string]*sync.Mutex{}}
}

// Register adds a job (the name must be unique).
func (r *Registry) Register(j Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.Interval < time.Second {
		j.Interval = time.Second
	}
	if j.Active == nil {
		j.Active = func() bool { return true }
	}
	r.jobs[j.Name] = &j
	r.running[j.Name] = &sync.Mutex{}
}

// Names lists the registered jobs.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.jobs))
	for n := range r.jobs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func lockID(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("ewiki-job:" + name))
	return int64(h.Sum64() >> 1)
}

// RunOnce runs a job now (also used by tests and the admin "run now").
func (r *Registry) RunOnce(ctx context.Context, name string) error {
	r.mu.Lock()
	j, ok := r.jobs[name]
	mu := r.running[name]
	r.mu.Unlock()
	if !ok {
		return apperr.NotFound("job " + name)
	}
	if !j.Active() {
		return nil
	}
	mu.Lock() // one run per process at a time
	defer mu.Unlock()
	ctx = identity.With(ctx, identity.CLI())
	if r.cluster != nil && r.cluster() {
		conn, err := r.pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockID(name)).Scan(&got); err != nil || !got {
			return err
		}
		defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID(name)) //nolint:errcheck
	}
	return j.Run(ctx)
}

// Start runs all jobs periodically until ctx is cancelled.
func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	jobs := make([]*Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		jobs = append(jobs, j)
	}
	r.mu.Unlock()
	for _, j := range jobs {
		go func(j *Job) {
			// small initial delay so the server is up first
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			t := time.NewTicker(j.Interval)
			defer t.Stop()
			for {
				if err := r.RunOnce(ctx, j.Name); err != nil && ctx.Err() == nil {
					r.log.Error("background job failed", "job", j.Name, "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}(j)
	}
}
