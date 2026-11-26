package server_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestDeleteSiteDoesNotStallTheWiki: deleting a site with shared accounts
// drops foreign keys to the shared table users. While another transaction
// holds a lock on users, the deletion must not queue every further write
// to users behind it (sign-ins and sessions of all sites would hang).
func TestDeleteSiteDoesNotStallTheWiki(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "gone", "hostnames": []string{"gone.example.test"}}, nil, 201)

	ctx := context.Background()
	url := os.Getenv("EWIKI_TEST_DATABASE_URL")
	hold, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close(ctx)
	other, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(ctx)

	// a transaction of the wiki that wrote to users and is still open
	tx, err := hold.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE public.users SET language=language WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- admin.do("DELETE", "/api/v1/instance/sites/gone", nil, nil) }()
	time.Sleep(700 * time.Millisecond)
	// the synchronisation of sites runs meanwhile (every 15 s in the
	// service): it must not open the site again while it is being deleted
	if err := e.app.Instance.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// another write to users (a session elsewhere) must go through quickly
	start := time.Now()
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = other.Exec(wctx, `UPDATE public.users SET language=language WHERE username<>'admin'`)
	cancel()
	took := time.Since(start)
	_ = tx.Commit(ctx)
	if err != nil {
		t.Fatalf("a write to users waited for the deletion of a site: %v after %s", err, took)
	}
	if took > 1500*time.Millisecond {
		t.Fatalf("a write to users waited %s for the deletion of a site", took)
	}
	if st := <-done; st != 204 {
		t.Fatalf("delete: status %d", st)
	}
	if e.app.Instance.Site("gone") != nil {
		t.Fatal("the deleted site is still open")
	}
}
