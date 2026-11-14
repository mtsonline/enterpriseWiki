package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// DirectorySync is one run of the directory sync: the people a directory
// (LDAP, Active Directory, later others) reports for a sign-in provider,
// mapped like at a sign-in (Mapping.Apply). The sync makes the accounts of
// that provider match the directory without anyone signing in.
type DirectorySync struct {
	Provider string              // sign-in provider id (ldap, ldap-corp, ...)
	Entries  []*ExternalIdentity // active people of the directory
	// Disabled are subjects the directory reports as disabled (for example
	// userAccountControl in Active Directory): their accounts are disabled.
	Disabled []string
	// CreateUsers creates accounts for people who never signed in, so
	// permissions can be granted before the first sign-in.
	CreateUsers bool
	// DisableMissing disables accounts of the provider that the directory
	// no longer reports, and ends their sessions.
	DisableMissing bool
	// MaxDisablePercent refuses a run that would disable more than this
	// share of the active accounts (a wrong filter or an outage must not
	// lock everyone out); Force overrides it.
	MaxDisablePercent int
	Force             bool
	DryRun            bool
	TriggeredBy       string // user name or "schedule"
}

// DirectoryChange is one change of a run.
type DirectoryChange struct {
	Username string   `json:"username"`
	Action   string   `json:"action"` // created, updated, disabled, enabled, groups, roles
	Added    []string `json:"added,omitempty"`
	Removed  []string `json:"removed,omitempty"`
}

// DirectoryReport is the result of a run.
type DirectoryReport struct {
	Run       int64  `json:"run"`
	Provider  string `json:"provider"`
	DryRun    bool   `json:"dry_run"`
	Status    string `json:"status"` // ok, failed, refused
	Error     string `json:"error,omitempty"`
	Directory int    `json:"directory"` // active people in the directory
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Disabled  int    `json:"disabled"`
	Enabled   int    `json:"enabled"`
	Skipped   int    `json:"skipped"` // new people without an account (CreateUsers off)
	// Conflicts counts people whose user name belongs to another account of
	// the provider (for example deleted and created again in the directory);
	// they are left out until an administrator decides
	Conflicts    int               `json:"conflicts"`
	GroupChanges int               `json:"group_changes"`
	RoleChanges  int               `json:"role_changes"`
	Changes      []DirectoryChange `json:"changes"`
	Truncated    bool              `json:"truncated,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	TriggeredBy  string            `json:"triggered_by"`
}

// maxReportedChanges limits the changes listed in a report (the counts are complete).
const maxReportedChanges = 1000

// keptRuns is the number of runs kept per provider.
const keptRuns = 100

var errDryRun = errors.New("dry run")

type dirAccount struct {
	id                    int64
	username, name, email string
	disabled, scim        bool
	disabledBy            string
	groups, roles         []string // set by this provider
	allGroups, allRoles   []string // from every source (local memberships stay)
}

// SyncDirectory makes the accounts of a provider match its directory. The
// whole run is one transaction; a dry run computes the same report and
// rolls back. Local memberships (assigned in the wiki) are never touched,
// nor are accounts provisioned by SCIM.
func (s *Service) SyncDirectory(ctx context.Context, in DirectorySync) (*DirectoryReport, error) {
	rep := &DirectoryReport{Provider: in.Provider, DryRun: in.DryRun, StartedAt: time.Now(), TriggeredBy: in.TriggeredBy, Changes: []DirectoryChange{}}
	if in.Provider == "" || in.Provider == "local" {
		return nil, apperr.BadRequest("directory sync needs an external sign-in provider")
	}
	entries, runErr := uniqueEntries(in)
	rep.Directory = len(entries)
	if runErr == nil {
		runErr = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.syncDirectoryTx(ctx, tx, in, entries, rep); err != nil {
				return err
			}
			if in.DryRun {
				return errDryRun
			}
			return audit.Log(ctx, tx, "auth.directory.sync", "provider", 0, nil, map[string]any{"provider": in.Provider,
				"created": rep.Created, "updated": rep.Updated, "disabled": rep.Disabled, "enabled": rep.Enabled,
				"group_changes": rep.GroupChanges, "role_changes": rep.RoleChanges, "conflicts": rep.Conflicts, "forced": in.Force})
		})
	}
	rep.Status = "ok"
	switch {
	case errors.Is(runErr, errDryRun):
	case runErr != nil:
		rep.Status = "failed"
		var ae *apperr.Error
		if errors.As(runErr, &ae) && ae.Status == 409 {
			rep.Status = "refused"
		}
		rep.Error = runErr.Error()
	default:
		s.bump(ctx) // permissions change at once, also in running sessions
		s.afterDirectorySync(ctx, entries, rep)
	}
	rep.FinishedAt = time.Now()
	if err := s.recordRun(ctx, rep); err != nil {
		s.log.Warn("directory sync: run not recorded", "provider", in.Provider, "err", err)
	}
	if rep.Status != "ok" {
		return rep, runErr
	}
	return rep, nil
}

// uniqueEntries drops entries without a subject and refuses duplicates:
// two directory entries for one account would make the result depend on
// their order.
func uniqueEntries(in DirectorySync) (map[string]*ExternalIdentity, error) {
	out := make(map[string]*ExternalIdentity, len(in.Entries))
	names := make(map[string]string, len(in.Entries))
	for _, e := range in.Entries {
		if e == nil || e.Subject == "" || e.Username == "" {
			continue
		}
		if _, dup := out[e.Subject]; dup {
			return nil, apperr.BadRequest("the directory reports the id %q twice", e.Subject)
		}
		if other, dup := names[strings.ToLower(e.Username)]; dup && other != e.Subject {
			return nil, apperr.BadRequest("the directory reports the user name %q twice", e.Username)
		}
		names[strings.ToLower(e.Username)] = e.Subject
		out[e.Subject] = e
	}
	for _, d := range in.Disabled {
		delete(out, d)
	}
	return out, nil
}

func (s *Service) syncDirectoryTx(ctx context.Context, tx pgx.Tx, in DirectorySync, entries map[string]*ExternalIdentity, rep *DirectoryReport) error {
	p := in.Provider
	current, err := loadDirAccounts(ctx, tx, p)
	if err != nil {
		return err
	}
	disabledInDir := map[string]bool{}
	for _, d := range in.Disabled {
		disabledInDir[d] = true
	}
	note := func(c DirectoryChange) {
		if len(rep.Changes) < maxReportedChanges {
			rep.Changes = append(rep.Changes, c)
		} else {
			rep.Truncated = true
		}
	}

	// accounts to disable and to enable again
	var disable, enable []int64
	active := 0
	for subject, a := range current {
		if a.scim {
			continue // SCIM manages these accounts
		}
		_, inDir := entries[subject]
		switch {
		case !a.disabled:
			active++
			if disabledInDir[subject] || (!inDir && in.DisableMissing) {
				disable = append(disable, a.id)
				note(DirectoryChange{Username: a.username, Action: "disabled"})
			}
		case a.disabledBy == "directory" && inDir:
			enable = append(enable, a.id)
			note(DirectoryChange{Username: a.username, Action: "enabled"})
		}
	}
	if limit := in.MaxDisablePercent; !in.Force && len(disable) > 0 && limit < 100 {
		if len(disable)*100 > active*limit {
			return apperr.Conflict("the sync would disable %d of %d active accounts (more than %d %%); check the filter or run it with force", len(disable), active, limit)
		}
	}

	// new and changed accounts; a user name another account of the provider
	// holds is a conflict, never a merge (it may be another person)
	holder := make(map[string]string, len(current))
	for subject, a := range current {
		holder[a.username] = subject
	}
	var newSubj, newUser, newName, newMail []string
	var updSubj, updUser, updName, updMail []string
	for subject, e := range entries {
		a, ok := current[subject]
		if h, taken := holder[e.Username]; taken && h != subject && (ok || in.CreateUsers) {
			rep.Conflicts++
			note(DirectoryChange{Username: e.Username, Action: "conflict"})
			delete(entries, subject) // no groups or roles for it either
			continue
		}
		switch {
		case !ok && !in.CreateUsers:
			rep.Skipped++
		case !ok:
			newSubj, newUser, newName, newMail = append(newSubj, subject), append(newUser, e.Username), append(newName, e.DisplayName), append(newMail, e.Email)
		case !a.scim && (a.username != e.Username || (e.DisplayName != "" && a.name != e.DisplayName) || (e.Email != "" && a.email != e.Email)):
			updSubj, updUser, updName, updMail = append(updSubj, subject), append(updUser, e.Username), append(updName, e.DisplayName), append(updMail, e.Email)
			note(DirectoryChange{Username: e.Username, Action: "updated"})
		}
	}
	if len(newSubj) > 0 {
		rows, err := tx.Query(ctx, `INSERT INTO users(username, provider, subject, display_name, email)
			SELECT u, $1, s, n, m FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS t(s, u, n, m)
			RETURNING id, subject`, p, newSubj, newUser, newName, newMail)
		if err != nil {
			return fmt.Errorf("create accounts: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
			ID      int64
			Subject string
		}])
		if err != nil {
			return fmt.Errorf("create accounts: %w", err)
		}
		for _, r := range ids {
			e := entries[r.Subject]
			current[r.Subject] = &dirAccount{id: r.ID, username: e.Username, name: e.DisplayName, email: e.Email}
			note(DirectoryChange{Username: e.Username, Action: "created"})
		}
		rep.Created = len(ids)
	}
	if len(updSubj) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE users u SET username = t.u,
				display_name = CASE WHEN t.n <> '' THEN t.n ELSE u.display_name END,
				email = CASE WHEN t.m <> '' THEN t.m ELSE u.email END, updated_at = now()
			FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS t(s, u, n, m)
			WHERE u.provider = $1 AND u.subject = t.s AND u.scim_client IS NULL`, p, updSubj, updUser, updName, updMail)
		if err != nil {
			return fmt.Errorf("update accounts: %w", err)
		}
		rep.Updated = int(tag.RowsAffected())
	}
	if len(disable) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE users SET disabled = true, disabled_by = 'directory', updated_at = now() WHERE id = ANY($1)`, disable); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = ANY($1)`, disable); err != nil {
			return err
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		rep.Disabled = len(disable)
	}
	if len(enable) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE users SET disabled = false, disabled_by = '', updated_at = now() WHERE id = ANY($1)`, enable); err != nil {
			return err
		}
		rep.Enabled = len(enable)
	}

	// roles must exist in the wiki; unknown names are skipped as at a sign-in
	known := map[string]bool{}
	rrows, err := tx.Query(ctx, `SELECT lower(name) FROM roles`)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rrows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		known[n] = true
	}
	unknown := map[string]bool{}
	for _, e := range entries {
		e.Roles = slices.DeleteFunc(slices.Clone(e.Roles), func(r string) bool {
			if !known[strings.ToLower(strings.TrimSpace(r))] {
				unknown[r] = true
				return true
			}
			return false
		})
	}
	for r := range unknown {
		s.log.Warn("role mapping: unknown local role", "role", r, "provider", p)
	}

	// memberships set by this provider
	var gAddU, gDelU, rAddU, rDelU []int64
	var gAddN, gDelN, rAddN, rDelN []string
	for subject, e := range entries {
		a, ok := current[subject]
		if !ok {
			continue
		}
		add, _ := diff(a.allGroups, e.Groups)
		_, del := diff(a.groups, e.Groups)
		for _, g := range add {
			gAddU, gAddN = append(gAddU, a.id), append(gAddN, g)
		}
		for _, g := range del {
			gDelU, gDelN = append(gDelU, a.id), append(gDelN, g)
		}
		if len(add)+len(del) > 0 {
			rep.GroupChanges += len(add) + len(del)
			note(DirectoryChange{Username: e.Username, Action: "groups", Added: add, Removed: del})
		}
		radd, _ := diff(a.allRoles, e.Roles)
		_, rdel := diff(a.roles, e.Roles)
		for _, r := range radd {
			rAddU, rAddN = append(rAddU, a.id), append(rAddN, r)
		}
		for _, r := range rdel {
			rDelU, rDelN = append(rDelU, a.id), append(rDelN, r)
		}
		if len(radd)+len(rdel) > 0 {
			rep.RoleChanges += len(radd) + len(rdel)
			note(DirectoryChange{Username: e.Username, Action: "roles", Added: radd, Removed: rdel})
		}
	}
	if len(gDelU) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups ug USING unnest($2::bigint[], $3::text[]) AS t(u, n), groups g
			WHERE ug.user_id = t.u AND g.name = t.n AND ug.group_id = g.id AND ug.source = $1`, p, gDelU, gDelN); err != nil {
			return err
		}
	}
	if len(gAddU) > 0 {
		// groups are created as at a sign-in (by name)
		if _, err := tx.Exec(ctx, `INSERT INTO groups(name, source) SELECT DISTINCT n, $1 FROM unnest($2::text[]) AS t(n)
			ON CONFLICT (name) DO NOTHING`, p, gAddN); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id, source)
			SELECT t.u, g.id, $1 FROM unnest($2::bigint[], $3::text[]) AS t(u, n) JOIN groups g ON g.name = t.n
			ON CONFLICT DO NOTHING`, p, gAddU, gAddN); err != nil {
			return err
		}
	}
	if len(rDelU) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles ur USING unnest($2::bigint[], $3::text[]) AS t(u, n), roles r
			WHERE ur.user_id = t.u AND lower(r.name) = lower(t.n) AND ur.role_id = r.id AND ur.source = $1`, p, rDelU, rDelN); err != nil {
			return err
		}
	}
	if len(rAddU) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id, source)
			SELECT t.u, r.id, $1 FROM unnest($2::bigint[], $3::text[]) AS t(u, n) JOIN roles r ON lower(r.name) = lower(t.n)
			ON CONFLICT DO NOTHING`, p, rAddU, rAddN); err != nil {
			return err
		}
	}
	if len(disable) > 0 || len(rDelU) > 0 || len(gDelU) > 0 {
		// removing a group or role must not remove the last administrator
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	// addresses of new and changed accounts, as at a sign-in (only changes: no query per unchanged person)
	subjects, mails := append(slices.Clone(newSubj), updSubj...), append(slices.Clone(newMail), updMail...)
	for i, subject := range subjects {
		mail := mails[i]
		e, a := entries[subject], current[subject]
		if mail == "" || a == nil {
			continue
		}
		if _, err := adoptEmail(ctx, tx, a.id, mail, e.TrustEmail && e.EmailVerified); err != nil {
			s.log.Warn("directory sync: e-mail not taken over", "provider", p, "user", e.Username, "err", err)
		}
	}
	sort.SliceStable(rep.Changes, func(i, j int) bool { return rep.Changes[i].Username < rep.Changes[j].Username })
	return nil
}

// afterDirectorySync sets the wiki user names (username_from) of new and
// changed accounts, as at a sign-in.
func (s *Service) afterDirectorySync(ctx context.Context, entries map[string]*ExternalIdentity, rep *DirectoryReport) {
	changed := map[string]bool{}
	for _, c := range rep.Changes {
		if c.Action == "created" || c.Action == "updated" {
			changed[c.Username] = true
		}
	}
	for _, e := range entries {
		if e.Alias == "" || !changed[e.Username] {
			continue
		}
		var uid int64
		if err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE provider=$1 AND subject=$2`, rep.Provider, e.Subject).Scan(&uid); err == nil {
			s.syncAlias(ctx, uid, e)
		}
	}
}

func loadDirAccounts(ctx context.Context, tx pgx.Tx, provider string) (map[string]*dirAccount, error) {
	rows, err := tx.Query(ctx, `SELECT u.id, u.subject, u.username, u.display_name, u.email, u.disabled, u.disabled_by, u.scim_client IS NOT NULL,
			COALESCE((SELECT array_agg(g.name ORDER BY g.name) FROM user_groups ug JOIN groups g ON g.id = ug.group_id
				WHERE ug.user_id = u.id AND ug.source = $1), '{}'),
			COALESCE((SELECT array_agg(r.name ORDER BY r.name) FROM user_roles ur JOIN roles r ON r.id = ur.role_id
				WHERE ur.user_id = u.id AND ur.source = $1), '{}'),
			COALESCE((SELECT array_agg(g.name ORDER BY g.name) FROM user_groups ug JOIN groups g ON g.id = ug.group_id
				WHERE ug.user_id = u.id), '{}'),
			COALESCE((SELECT array_agg(r.name ORDER BY r.name) FROM user_roles ur JOIN roles r ON r.id = ur.role_id
				WHERE ur.user_id = u.id), '{}')
		FROM users u WHERE u.provider = $1 FOR UPDATE OF u`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*dirAccount{}
	for rows.Next() {
		a := &dirAccount{}
		var subject string
		if err := rows.Scan(&a.id, &subject, &a.username, &a.name, &a.email, &a.disabled, &a.disabledBy, &a.scim, &a.groups, &a.roles, &a.allGroups, &a.allRoles); err != nil {
			return nil, err
		}
		out[subject] = a
	}
	return out, rows.Err()
}

// diff returns what to add to have and what to remove from it to get want
// (groups compare like at a sign-in: exact names; roles without case).
func diff(have, want []string) (add, del []string) {
	h := map[string]bool{}
	for _, x := range have {
		h[strings.ToLower(x)] = true
	}
	w := map[string]bool{}
	for _, x := range want {
		x = strings.TrimSpace(x)
		if x == "" || w[strings.ToLower(x)] {
			continue
		}
		w[strings.ToLower(x)] = true
		if !h[strings.ToLower(x)] {
			add = append(add, x)
		}
	}
	for _, x := range have {
		if !w[strings.ToLower(x)] {
			del = append(del, x)
		}
	}
	sort.Strings(add)
	sort.Strings(del)
	return add, del
}

func (s *Service) recordRun(ctx context.Context, rep *DirectoryReport) error {
	sum, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO directory_sync_runs(provider, dry_run, status, triggered_by, started_at, finished_at, summary, error)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, rep.Provider, rep.DryRun, rep.Status, rep.TriggeredBy,
			rep.StartedAt, rep.FinishedAt, sum, rep.Error).Scan(&rep.Run); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE directory_sync_runs SET summary = jsonb_set(summary, '{run}', to_jsonb(id)) WHERE id=$1`, rep.Run); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM directory_sync_runs WHERE provider=$1 AND id NOT IN (
			SELECT id FROM directory_sync_runs WHERE provider=$1 ORDER BY started_at DESC, id DESC LIMIT $2)`, rep.Provider, keptRuns)
		return err
	})
}

// DirectoryRun is a recorded run (without its list of changes).
type DirectoryRun struct {
	ID          int64           `json:"id"`
	Provider    string          `json:"provider"`
	DryRun      bool            `json:"dry_run"`
	Status      string          `json:"status"`
	TriggeredBy string          `json:"triggered_by"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	Error       string          `json:"error,omitempty"`
	Summary     json.RawMessage `json:"summary"`
}

// DirectoryRuns lists the newest runs of a provider (limit ≤ 100), each
// with its full report.
func (s *Service) DirectoryRuns(ctx context.Context, provider string, limit int) ([]DirectoryRun, error) {
	if limit <= 0 || limit > keptRuns {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT id, provider, dry_run, status, triggered_by, started_at, finished_at, error, summary
		FROM directory_sync_runs WHERE provider=$1 ORDER BY started_at DESC, id DESC LIMIT $2`, provider, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DirectoryRun])
}

// LastDirectorySync returns the start of the newest real (not dry) run of
// a provider, zero if there is none.
func (s *Service) LastDirectorySync(ctx context.Context, provider string) (time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx, `SELECT max(started_at) FROM directory_sync_runs WHERE provider=$1 AND NOT dry_run`, provider).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, err
	}
	return *t, nil
}

// SyncTriggeredBy names who starts a run: the signed-in person or "schedule".
func SyncTriggeredBy(ctx context.Context) string {
	if p := identity.From(ctx); p != nil {
		return p.Handle()
	}
	return "schedule"
}

// AdoptDirectoryIdentity gives the account with this user name the new
// directory id of the same person, for example after the person was deleted
// and created again in the directory. Only an administrator can decide that
// it is the same person; the caller checks that the directory reports this
// user name with this id.
func (s *Service) AdoptDirectoryIdentity(ctx context.Context, provider, username, subject string) error {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return err
	}
	if provider == "" || username == "" || subject == "" {
		return apperr.BadRequest("provider, user name and id are required")
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		var old string
		var scim bool
		err := tx.QueryRow(ctx, `SELECT id, subject, scim_client IS NOT NULL FROM users WHERE provider=$1 AND username=$2 FOR UPDATE`,
			provider, username).Scan(&id, &old, &scim)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("user")
		}
		if err != nil {
			return err
		}
		if scim {
			return apperr.Conflict("SCIM manages the account %q", username)
		}
		if old == subject {
			return apperr.BadRequest("the account %q already has this id", username)
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE provider=$1 AND subject=$2)
				OR EXISTS(SELECT 1 FROM user_identities WHERE provider=$1 AND subject=$2)`, provider, subject).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict("another account already has this id")
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET subject=$2, updated_at=now() WHERE id=$1`, id, subject); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "auth.directory.adopt", "user", id, nil,
			map[string]any{"provider": provider, "username": username, "old_id": old, "new_id": subject})
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}
