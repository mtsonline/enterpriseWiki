package server_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// TestDirectorySync checks the rules of the directory sync in the core
// (accounts, groups, roles, disabling, protection against mass disabling).
// The tests against a real LDAP/AD are in realauth_test.go.
func TestDirectorySync(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	a := e.app.Auth
	if _, err := a.EnsureRole(ctx, "Editors", "", false); err != nil {
		t.Fatal(err)
	}
	person := func(subject, name string, groups ...string) *auth.ExternalIdentity {
		return &auth.ExternalIdentity{Provider: "ldap", Subject: subject, Username: name, DisplayName: strings.ToUpper(name[:1]) + name[1:],
			Email: name + "@example.com", Groups: groups}
	}
	state := func(name string) (disabled bool, by string, groups []string) {
		t.Helper()
		if err := e.app.DB.QueryRow(ctx, `SELECT u.disabled, u.disabled_by,
				COALESCE((SELECT array_agg(g.name ORDER BY g.name) FROM user_groups ug JOIN groups g ON g.id=ug.group_id WHERE ug.user_id=u.id), '{}')
			FROM users u WHERE provider='ldap' AND username=$1`, name).Scan(&disabled, &by, &groups); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return
	}
	run := func(in auth.DirectorySync) *auth.DirectoryReport {
		t.Helper()
		in.Provider = "ldap"
		rep, err := a.SyncDirectory(ctx, in)
		if err != nil {
			t.Fatalf("sync: %v (%+v)", err, rep)
		}
		return rep
	}
	people := func() []*auth.ExternalIdentity {
		ps := []*auth.ExternalIdentity{person("s-anna", "anna", "Sales"), person("s-ben", "ben", "Sales", "IT")}
		for i := range 18 {
			ps = append(ps, person("s-p"+string(rune('a'+i)), "p"+string(rune('a'+i))))
		}
		return ps
	}

	// a preview changes nothing
	rep := run(auth.DirectorySync{Entries: people(), CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10, DryRun: true})
	if rep.Created != 20 || !rep.DryRun || rep.Run == 0 {
		t.Fatalf("preview %+v", rep)
	}
	var n int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM users WHERE provider='ldap'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("preview created %d accounts", n)
	}

	// accounts and groups before anyone signs in
	rep = run(auth.DirectorySync{Entries: people(), CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if rep.Created != 20 || rep.GroupChanges != 3 {
		t.Fatalf("first run %+v", rep)
	}
	if _, _, g := state("ben"); !slices.Equal(g, []string{"IT", "Sales"}) {
		t.Fatalf("ben groups %v", g)
	}
	// a second run with the same directory changes nothing
	if rep = run(auth.DirectorySync{Entries: people(), CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10}); rep.Created+rep.Updated+rep.GroupChanges+rep.Disabled != 0 || len(rep.Changes) != 0 {
		t.Fatalf("second run changed %+v", rep)
	}

	// local memberships stay; a provider group also set locally is no change
	var benID, itID int64
	if err := e.app.DB.QueryRow(ctx, `SELECT id FROM users WHERE provider='ldap' AND username='ben'`).Scan(&benID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.DB.QueryRow(ctx, `INSERT INTO groups(name) VALUES ('Wiki team') RETURNING id`).Scan(&itID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO user_groups(user_id, group_id) VALUES ($1,$2)`, benID, itID); err != nil {
		t.Fatal(err)
	}
	ps := people()
	ps[1].Groups = []string{"IT"} // ben leaves Sales
	ps[0].Roles = []string{"editors", "Unknown role"}
	ps[0].DisplayName = "Anna Example"
	rep = run(auth.DirectorySync{Entries: ps, CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if _, _, g := state("ben"); !slices.Equal(g, []string{"IT", "Wiki team"}) {
		t.Fatalf("ben after leaving Sales %v", g)
	}
	if rep.Updated != 1 || rep.RoleChanges != 1 || rep.GroupChanges != 1 {
		t.Fatalf("changes %+v", rep)
	}
	var roles int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE u.username='anna' AND ur.source='ldap'`).Scan(&roles); err != nil || roles != 1 {
		t.Fatalf("anna roles %d %v", roles, err)
	}

	// a person leaves the directory: account disabled, sessions ended
	c := e.client()
	sessions := func() int {
		var n int
		_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, benID).Scan(&n)
		return n
	}
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, kind, expires_at) VALUES ('\x01', $1, 'browser', now()+interval '1 hour')`, benID); err != nil {
		t.Fatal(err)
	}
	_ = c
	without := slices.DeleteFunc(slices.Clone(ps), func(p *auth.ExternalIdentity) bool { return p.Username == "ben" })
	rep = run(auth.DirectorySync{Entries: without, CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if d, by, _ := state("ben"); !d || by != "directory" || rep.Disabled != 1 || sessions() != 0 {
		t.Fatalf("ben not disabled: %v %q %+v sessions %d", d, by, rep, sessions())
	}
	// back in the directory: enabled again
	rep = run(auth.DirectorySync{Entries: ps, CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if d, by, _ := state("ben"); d || by != "" || rep.Enabled != 1 {
		t.Fatalf("ben not enabled again: %v %q %+v", d, by, rep)
	}
	// disabled by an administrator: the sync does not enable it
	if _, err := e.app.DB.Exec(ctx, `UPDATE users SET disabled=true, disabled_by='' WHERE id=$1`, benID); err != nil {
		t.Fatal(err)
	}
	if rep = run(auth.DirectorySync{Entries: ps, CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10}); rep.Enabled != 0 {
		t.Fatalf("enabled an account an administrator disabled: %+v", rep)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE users SET disabled=false WHERE id=$1`, benID); err != nil {
		t.Fatal(err)
	}

	// the directory reports a person as disabled
	rep = run(auth.DirectorySync{Entries: ps, Disabled: []string{"s-anna"}, CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if d, _, _ := state("anna"); !d || rep.Disabled != 1 {
		t.Fatalf("anna not disabled: %+v", rep)
	}

	// protection: an empty or wrong result must not disable everyone
	rep, err := a.SyncDirectory(ctx, auth.DirectorySync{Provider: "ldap", Entries: ps[:2], CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if err == nil || rep.Status != "refused" || !strings.Contains(rep.Error, "would disable") {
		t.Fatalf("mass disabling not refused: %v %+v", err, rep)
	}
	if d, _, _ := state("pa"); d {
		t.Fatal("refused run disabled accounts")
	}
	// ... unless forced
	rep = run(auth.DirectorySync{Entries: ps[:2], CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10, Force: true})
	if rep.Disabled != 18 {
		t.Fatalf("forced run %+v", rep)
	}
	// without DisableMissing nothing is disabled
	rep = run(auth.DirectorySync{Entries: ps, CreateUsers: true, MaxDisablePercent: 10})
	if rep.Enabled != 18 {
		t.Fatalf("enable after forced run %+v", rep)
	}
	if rep = run(auth.DirectorySync{Entries: ps[:2], CreateUsers: true, MaxDisablePercent: 10}); rep.Disabled != 0 {
		t.Fatalf("disabled without DisableMissing: %+v", rep)
	}

	// without CreateUsers new people are skipped
	rep = run(auth.DirectorySync{Entries: append(slices.Clone(ps), person("s-new", "newbie")), MaxDisablePercent: 10})
	if rep.Skipped != 1 || rep.Created != 0 {
		t.Fatalf("skip %+v", rep)
	}

	// a new id with a name another account holds is a conflict, not a merge;
	// renaming an account to a taken name as well
	renamed := slices.Clone(ps)
	renamed[2] = person("s-other", ps[2].Username, "IT")
	renamed[3] = &auth.ExternalIdentity{Provider: "ldap", Subject: ps[3].Subject, Username: ps[4].Username}
	renamed = slices.Delete(renamed, 4, 5) // its old holder is gone from the directory, the account remains
	rep = run(auth.DirectorySync{Entries: renamed, CreateUsers: true, MaxDisablePercent: 10})
	if rep.Conflicts != 2 || rep.Created != 0 || rep.Updated != 0 || rep.GroupChanges != 0 {
		t.Fatalf("conflicts %+v", rep)
	}

	// SCIM-managed accounts are left alone
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO scim_clients(name, provider, token_hash) VALUES ('x','ldap','\x02')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE users SET scim_client=(SELECT id FROM scim_clients LIMIT 1) WHERE provider='ldap' AND username='pa'`); err != nil {
		t.Fatal(err)
	}
	rep = run(auth.DirectorySync{Entries: slices.DeleteFunc(slices.Clone(ps), func(p *auth.ExternalIdentity) bool { return p.Username == "pa" }),
		CreateUsers: true, DisableMissing: true, MaxDisablePercent: 10})
	if d, _, _ := state("pa"); d || rep.Disabled != 0 {
		t.Fatalf("SCIM account disabled: %+v", rep)
	}

	// duplicates in the directory stop the run
	if _, err := a.SyncDirectory(ctx, auth.DirectorySync{Provider: "ldap", Entries: []*auth.ExternalIdentity{person("x1", "dup"), person("x2", "DUP")}}); err == nil {
		t.Fatal("duplicate user names accepted")
	}
	// runs are recorded with their report
	runs, err := a.DirectoryRuns(ctx, "ldap", 50)
	if err != nil || len(runs) < 10 || runs[0].Status != "failed" {
		t.Fatalf("runs %d %v %+v", len(runs), err, runs[:1])
	}
	if last, err := a.LastDirectorySync(ctx, "ldap"); err != nil || last.IsZero() {
		t.Fatalf("last run %v %v", last, err)
	}
}

// TestDirectorySyncKeepsAdmin: a sync never disables the last administrator.
func TestDirectorySyncKeepsAdmin(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	a := e.app.Auth
	// the only administrator signs in through the directory
	if _, err := a.SyncDirectory(ctx, auth.DirectorySync{Provider: "ldap", CreateUsers: true, MaxDisablePercent: 100,
		Entries: []*auth.ExternalIdentity{{Provider: "ldap", Subject: "boss", Username: "boss", Roles: []string{"Administrators"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE users SET disabled=true WHERE provider='local'`); err != nil {
		t.Fatal(err)
	}
	rep, err := a.SyncDirectory(ctx, auth.DirectorySync{Provider: "ldap", DisableMissing: true, MaxDisablePercent: 100, Force: true})
	if err == nil || rep.Disabled != 0 && rep.Status == "ok" {
		t.Fatalf("last administrator disabled: %v %+v", err, rep)
	}
	var d bool
	if err := e.app.DB.QueryRow(ctx, `SELECT disabled FROM users WHERE provider='ldap' AND username='boss'`).Scan(&d); err != nil || d {
		t.Fatalf("boss disabled %v %v", d, err)
	}
}

// TestDirectoryCLI: the CLI reaches the sync of the right instance through
// the API and refuses unknown providers.
func TestDirectoryCLI(t *testing.T) {
	e := setup(t)
	if code, out := runCLI(t, e.cfg, "directory", "runs", "ldap"); code != 0 || !strings.Contains(out, `"provider": "ldap"`) {
		t.Fatalf("runs ldap: exit %d %s", code, out)
	}
	for _, args := range [][]string{{"runs", "ldap-corp"}, {"runs", "oidc"}, {"runs", "ldap-"}, {"adopt", "ldap"}, {"sync"}} {
		if code, out := runCLI(t, e.cfg, append([]string{"directory"}, args...)...); code == 0 {
			t.Errorf("directory %v succeeded: %s", args, out)
		}
	}
	// a sync without a configured directory says so
	if code, _ := runCLI(t, e.cfg, "directory", "sync", "ldap", "--dry-run"); code == 0 {
		t.Error("sync without a directory succeeded")
	}
}

// TestAdoptDirectoryIdentity: only administrators, never onto an id another
// account has.
func TestAdoptDirectoryIdentity(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	a := e.app.Auth
	if _, err := a.SyncDirectory(ctx, auth.DirectorySync{Provider: "ldap", CreateUsers: true, MaxDisablePercent: 100,
		Entries: []*auth.ExternalIdentity{{Provider: "ldap", Subject: "s1", Username: "anna"}, {Provider: "ldap", Subject: "s2", Username: "ben"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.AdoptDirectoryIdentity(ctx, "ldap", "anna", "s3"); err == nil {
		t.Fatal("adopt without permission")
	}
	admin := identity.With(ctx, identity.CLI())
	if err := a.AdoptDirectoryIdentity(admin, "ldap", "anna", "s2"); err == nil {
		t.Fatal("adopted the id of another account")
	}
	if err := a.AdoptDirectoryIdentity(admin, "ldap", "nobody", "s3"); err == nil {
		t.Fatal("adopted for a missing account")
	}
	if err := a.AdoptDirectoryIdentity(admin, "ldap", "anna", "s3"); err != nil {
		t.Fatal(err)
	}
	var subject string
	if err := e.app.DB.QueryRow(ctx, `SELECT subject FROM users WHERE provider='ldap' AND username='anna'`).Scan(&subject); err != nil || subject != "s3" {
		t.Fatalf("subject %q %v", subject, err)
	}
	var n int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='auth.directory.adopt'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit %d %v", n, err)
	}
}
