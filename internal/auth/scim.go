package auth

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// SCIM 2.0 provisioning (RFC 7643/7644): a directory such as Entra ID or
// Okta creates, changes, disables and removes people and groups ahead of
// their sign-in. Each client has a bearer token (only its hash is stored)
// and belongs to the sign-in its people use (provider), so the account is
// found again at the first sign-in (provider + subject).
//
// What a client may touch is limited: people it provisioned, or people of
// its provider not provisioned by anyone else, never administrators;
// groups it created (never roles). Administrators decide which roles those
// groups get.

// ScimClient is a provisioning client.
type ScimClient struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	SubjectFrom string     `json:"subject_from"` // userName | externalId: becomes the subject at the provider
	OnDelete    string     `json:"on_delete"`    // disable | delete
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// ScimEmail is an address of a provisioned person.
type ScimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// ScimRef references a user (group member) or a group (of a user).
type ScimRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

// ScimUser is a provisioned person.
type ScimUser struct {
	ID          int64
	UserName    string
	ExternalID  string
	DisplayName string
	GivenName   string
	FamilyName  string
	Emails      []ScimEmail
	Active      bool
	Groups      []ScimRef
	Created     time.Time
	Modified    time.Time
}

// ScimGroup is a provisioned group.
type ScimGroup struct {
	ID          int64
	DisplayName string
	ExternalID  string
	Members     []ScimRef
	Created     time.Time
	Modified    time.Time
}

// ScimFilter is a simple equality filter (attribute eq "value").
type ScimFilter struct {
	Attr, Value string
}

const scimTokenPrefix = "scim_"

// usernameRe: allowed user names (as the users table checks).
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)

// IsScimToken reports whether a bearer token is a SCIM token.
func IsScimToken(tok string) bool { return strings.HasPrefix(tok, scimTokenPrefix) }

// ---------------------------------------------------------------- clients (administrators)

// ScimClients lists the provisioning clients.
func (s *Service) ScimClients(ctx context.Context) ([]ScimClient, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name, provider, subject_from, on_delete, created_at, last_used_at FROM scim_clients ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ScimClient, error) {
		var c ScimClient
		err := r.Scan(&c.ID, &c.Name, &c.Provider, &c.SubjectFrom, &c.OnDelete, &c.CreatedAt, &c.LastUsedAt)
		return c, err
	})
}

// CreateScimClient adds a client and returns its token (shown only once).
func (s *Service) CreateScimClient(ctx context.Context, in ScimClient) (*ScimClient, string, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, "", err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return nil, "", apperr.BadRequest("name is required (at most 100 characters)")
	}
	if in.SubjectFrom == "" {
		in.SubjectFrom = "userName"
	}
	if in.OnDelete == "" {
		in.OnDelete = "disable"
	}
	if in.SubjectFrom != "userName" && in.SubjectFrom != "externalId" {
		return nil, "", apperr.BadRequest("subject_from must be userName or externalId")
	}
	if in.OnDelete != "disable" && in.OnDelete != "delete" {
		return nil, "", apperr.BadRequest("on_delete must be disable or delete")
	}
	if !s.knownProvider(in.Provider) {
		return nil, "", apperr.BadRequest("unknown sign-in provider %q", in.Provider)
	}
	tok := NewToken(scimTokenPrefix)
	err := s.pool.QueryRow(ctx, `INSERT INTO scim_clients(name, provider, subject_from, on_delete, token_hash) VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`, in.Name, in.Provider, in.SubjectFrom, in.OnDelete, HashToken(tok)).Scan(&in.ID, &in.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	_ = audit.Log(ctx, s.pool, "scim.client_create", "scim_client", in.ID, nil, in)
	return &in, tok, nil
}

// knownProvider reports whether id is a sign-in provider people can use
// (not the local accounts: those have passwords, not a directory).
func (s *Service) knownProvider(id string) bool {
	if id == "" || id == "local" || id == InviteProvider {
		return false
	}
	for _, p := range s.Providers() {
		if p.ID() == id {
			return true
		}
	}
	for _, p := range s.RedirectProviders() {
		if p.ID() == id {
			return true
		}
	}
	return false
}

// RenewScimToken replaces the token of a client (the old one stops working).
func (s *Service) RenewScimToken(ctx context.Context, id int64) (string, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return "", err
	}
	tok := NewToken(scimTokenPrefix)
	tag, err := s.pool.Exec(ctx, `UPDATE scim_clients SET token_hash=$2 WHERE id=$1`, id, HashToken(tok))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", apperr.NotFound("SCIM client")
	}
	_ = audit.Log(ctx, s.pool, "scim.client_token", "scim_client", id, nil, nil)
	return tok, nil
}

// DeleteScimClient removes a client; the people and groups it provisioned
// stay (and can be managed by administrators).
func (s *Service) DeleteScimClient(ctx context.Context, id int64) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM scim_clients WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("SCIM client")
	}
	return audit.Log(ctx, s.pool, "scim.client_delete", "scim_client", id, nil, nil)
}

// ScimAuthenticate returns the client of a bearer token and a context in
// which its changes are audited (as "scim:<name>"). After 30 wrong tokens
// in 5 minutes, ip is refused for a while.
func (s *Service) ScimAuthenticate(ctx context.Context, token, ip string) (*ScimClient, context.Context, error) {
	const maxFail, window = 30, 5 * time.Minute
	if s.limiter.exceeded("scim:"+ip, maxFail, window) {
		return nil, ctx, apperr.TooMany("too many failed attempts")
	}
	if !IsScimToken(token) || len(token) > 200 {
		s.limiter.allow("scim:"+ip, maxFail, window)
		return nil, ctx, apperr.ErrUnauthorized
	}
	var c ScimClient
	err := s.pool.QueryRow(ctx, `UPDATE scim_clients SET last_used_at=now() WHERE token_hash=$1
		RETURNING id, name, provider, subject_from, on_delete, created_at, last_used_at`, HashToken(token)).
		Scan(&c.ID, &c.Name, &c.Provider, &c.SubjectFrom, &c.OnDelete, &c.CreatedAt, &c.LastUsedAt)
	if err != nil {
		s.limiter.allow("scim:"+ip, maxFail, window)
		return nil, ctx, apperr.ErrUnauthorized
	}
	// shared accounts are provisioned on the main site only
	if err := s.requireShared(ctx); err != nil {
		return nil, ctx, apperr.Forbidden("SCIM is available on the main site (or on sites with their own accounts)")
	}
	ctx = identity.With(ctx, &identity.Principal{Username: "scim:" + c.Name, Provider: "scim", Via: "scim"})
	return &c, ctx, nil
}

// ---------------------------------------------------------------- users

// scimUserScope: the people a client may see and change ($1 client, $2
// its provider): never administrators of this site, nor (with shared
// accounts) instance administrators.
func (s *Service) scimUserScope() string {
	scope := `(u.scim_client = $1 OR (u.scim_client IS NULL AND u.provider = $2)) AND NOT EXISTS (
	SELECT 1 FROM roles r WHERE r.is_admin AND (r.id IN (SELECT role_id FROM user_roles WHERE user_id=u.id)
		OR r.id IN (SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=u.id)))`
	if !s.SeparateAccounts {
		scope += ` AND NOT EXISTS (SELECT 1 FROM public.instance_admins ia WHERE ia.user_id=u.id)`
	}
	return scope
}

const scimUserCols = `u.id, u.username, u.external_id, u.display_name, u.attributes, NOT u.disabled, u.created_at, u.updated_at`

func (s *Service) scanScimUsers(ctx context.Context, rows pgx.Rows) ([]ScimUser, error) {
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ScimUser, error) {
		var u ScimUser
		var attrs map[string]any
		err := r.Scan(&u.ID, &u.UserName, &u.ExternalID, &u.DisplayName, &attrs, &u.Active, &u.Created, &u.Modified)
		u.GivenName, _ = attrs["scim_given_name"].(string)
		u.FamilyName, _ = attrs["scim_family_name"].(string)
		return u, err
	})
	if err != nil || len(out) == 0 {
		return out, err
	}
	ids := make([]int64, len(out))
	idx := map[int64]int{}
	for i, u := range out {
		ids[i], idx[u.ID] = u.ID, i
	}
	er, err := s.pool.Query(ctx, `SELECT user_id, email, is_primary FROM user_emails WHERE user_id = ANY($1) AND verified ORDER BY is_primary DESC, id`, ids)
	if err != nil {
		return nil, err
	}
	for er.Next() {
		var uid int64
		var e ScimEmail
		if err := er.Scan(&uid, &e.Value, &e.Primary); err != nil {
			er.Close()
			return nil, err
		}
		out[idx[uid]].Emails = append(out[idx[uid]].Emails, e)
	}
	er.Close()
	gr, err := s.pool.Query(ctx, `SELECT ug.user_id, g.id, g.name FROM user_groups ug JOIN groups g ON g.id=ug.group_id
		WHERE ug.user_id = ANY($1) ORDER BY g.name`, ids)
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	for gr.Next() {
		var uid, gid int64
		var name string
		if err := gr.Scan(&uid, &gid, &name); err != nil {
			return nil, err
		}
		out[idx[uid]].Groups = append(out[idx[uid]].Groups, ScimRef{Value: strconv.FormatInt(gid, 10), Display: name})
	}
	return out, gr.Err()
}

// ScimUsers lists the people of client c matching f (start is 1-based).
func (s *Service) ScimUsers(ctx context.Context, c *ScimClient, f *ScimFilter, start, count int) ([]ScimUser, int, error) {
	where, args := s.scimUserScope(), []any{c.ID, c.Provider}
	if f != nil {
		switch f.Attr {
		case "username":
			where += ` AND lower(u.username) = lower($3)`
		case "externalid":
			where += ` AND u.external_id = $3`
		case "id":
			where += ` AND u.id::text = $3`
		case "emails.value", "emails":
			where += ` AND EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id=u.id AND e.verified AND e.email = lower($3))`
		case "displayname":
			where += ` AND u.display_name = $3`
		default:
			return nil, 0, apperr.BadRequest("unsupported filter attribute %q", f.Attr)
		}
		args = append(args, f.Value)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users u WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	n := len(args)
	rows, err := s.pool.Query(ctx, `SELECT `+scimUserCols+` FROM users u WHERE `+where+` ORDER BY u.id LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2),
		append(args, count, start-1)...)
	if err != nil {
		return nil, 0, err
	}
	list, err := s.scanScimUsers(ctx, rows)
	return list, total, err
}

// ScimUser returns a person of client c.
func (s *Service) ScimUser(ctx context.Context, c *ScimClient, id int64) (*ScimUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+scimUserCols+` FROM users u WHERE `+s.scimUserScope()+` AND u.id=$3`, c.ID, c.Provider, id)
	if err != nil {
		return nil, err
	}
	list, err := s.scanScimUsers(ctx, rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperr.NotFound("user")
	}
	return &list[0], nil
}

func (c *ScimClient) subject(u *ScimUser) (string, error) {
	sub := u.UserName
	if c.SubjectFrom == "externalId" {
		sub = u.ExternalID
	}
	if sub == "" || len(sub) > 256 {
		return "", apperr.BadRequest("%s is required (at most 256 characters)", c.SubjectFrom)
	}
	return sub, nil
}

func checkScimUser(u *ScimUser) error {
	if !usernameRe.MatchString(u.UserName) {
		return apperr.BadRequest("userName must be 1-128 letters, digits and . _ @ + -")
	}
	if len(u.ExternalID) > 256 || len(u.DisplayName) > 200 || len(u.GivenName) > 100 || len(u.FamilyName) > 100 || len(u.Emails) > 10 {
		return apperr.BadRequest("value too long")
	}
	if u.DisplayName == "" {
		u.DisplayName = strings.TrimSpace(u.GivenName + " " + u.FamilyName)
	}
	return nil
}

// ScimCreateUser provisions a person. An existing account of the client's
// provider with that user name and no other client is taken over.
func (s *Service) ScimCreateUser(ctx context.Context, c *ScimClient, u ScimUser) (*ScimUser, error) {
	if err := checkScimUser(&u); err != nil {
		return nil, err
	}
	sub, err := c.subject(&u)
	if err != nil {
		return nil, err
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var other *int64
		err := tx.QueryRow(ctx, `SELECT id, scim_client FROM users WHERE provider=$1 AND (lower(username)=lower($2) OR subject=$3)`,
			c.Provider, u.UserName, sub).Scan(&id, &other)
		switch {
		case err == nil && other != nil:
			return apperr.Conflict("the user %s exists", u.UserName)
		case err == nil:
			var visible bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE `+s.scimUserScope()+` AND u.id=$3)`, c.ID, c.Provider, id).Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return apperr.Conflict("the user %s exists", u.UserName)
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET scim_client=$2 WHERE id=$1`, id, c.ID); err != nil {
				return err
			}
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `INSERT INTO users(username, provider, subject, scim_client) VALUES ($1,$2,$3,$4) RETURNING id`,
				u.UserName, c.Provider, sub, c.ID).Scan(&id); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("the user %s exists", u.UserName)
				}
				return err
			}
		default:
			return err
		}
		if err := s.scimWriteUser(ctx, tx, c, id, &u, sub); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "scim.user_create", "user", id, nil, map[string]any{"userName": u.UserName, "active": u.Active})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.ScimUser(ctx, c, id)
}

// ScimReplaceUser sets all attributes of a person (PUT, and PATCH after the
// operations were applied to the current state).
func (s *Service) ScimReplaceUser(ctx context.Context, c *ScimClient, id int64, u ScimUser) (*ScimUser, error) {
	if err := checkScimUser(&u); err != nil {
		return nil, err
	}
	sub, err := c.subject(&u)
	if err != nil {
		return nil, err
	}
	if _, err := s.ScimUser(ctx, c, id); err != nil {
		return nil, err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET username=$2, subject=$3, scim_client=$4 WHERE id=$1`, id, u.UserName, sub, c.ID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("the user %s exists", u.UserName)
			}
			return err
		}
		if err := s.scimWriteUser(ctx, tx, c, id, &u, sub); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "scim.user_update", "user", id, nil, map[string]any{"userName": u.UserName, "active": u.Active})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.ScimUser(ctx, c, id)
}

// scimWriteUser stores the attributes and addresses of a provisioned person.
func (s *Service) scimWriteUser(ctx context.Context, tx pgx.Tx, c *ScimClient, id int64, u *ScimUser, _ string) error {
	if _, err := tx.Exec(ctx, `UPDATE users SET external_id=$2, display_name=$3, disabled=$4,
		attributes = attributes || jsonb_build_object('scim_given_name', $5::text, 'scim_family_name', $6::text), updated_at=now()
		WHERE id=$1`, id, u.ExternalID, u.DisplayName, !u.Active, u.GivenName, u.FamilyName); err != nil {
		return err
	}
	if !u.Active {
		// disabled: signed out everywhere at once
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return err
		}
	}
	// addresses: confirmed by the directory; those it no longer sends go
	addrs := []string{}
	primary := ""
	for _, e := range u.Emails {
		a, err := NormalizeEmail(e.Value)
		if err != nil || a == "" {
			return apperr.BadRequest("invalid e-mail address %q", e.Value)
		}
		var owner int64
		err = tx.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, a).Scan(&owner)
		if err == nil && owner != id {
			return apperr.Conflict("the address %s belongs to another account", a)
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := adoptEmail(ctx, tx, id, a, true); err != nil {
			return err
		}
		addrs = append(addrs, a)
		if e.Primary || primary == "" {
			primary = a
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_emails WHERE user_id=$1 AND scim AND NOT (email = ANY($2))`, id, addrs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE user_emails SET scim = true WHERE user_id=$1 AND email = ANY($2)`, id, addrs); err != nil {
		return err
	}
	if primary != "" {
		return setAdminPrimary(ctx, tx, id, primary)
	}
	return choosePrimary(ctx, tx, id)
}

// ScimDeleteUser disables or deletes a person (client setting on_delete).
func (s *Service) ScimDeleteUser(ctx context.Context, c *ScimClient, id int64) error {
	if _, err := s.ScimUser(ctx, c, id); err != nil {
		return err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if c.OnDelete == "delete" {
			if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE users SET disabled=true, updated_at=now() WHERE id=$1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "scim.user_delete", "user", id, nil, map[string]any{"mode": c.OnDelete})
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}

// ---------------------------------------------------------------- groups

func (s *Service) scimGroups(ctx context.Context, c *ScimClient, where string, args []any, members bool) ([]ScimGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id, g.name, g.external_id, g.created_at FROM groups g WHERE g.scim_client=$1`+where+` ORDER BY g.id`,
		append([]any{c.ID}, args...)...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ScimGroup, error) {
		var g ScimGroup
		err := r.Scan(&g.ID, &g.DisplayName, &g.ExternalID, &g.Created)
		g.Modified = g.Created
		return g, err
	})
	if err != nil || !members || len(out) == 0 {
		return out, err
	}
	for i := range out {
		mr, err := s.pool.Query(ctx, `SELECT u.id, u.display_name FROM user_groups ug JOIN users u ON u.id=ug.user_id WHERE ug.group_id=$1 ORDER BY u.id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members, err = pgx.CollectRows(mr, func(r pgx.CollectableRow) (ScimRef, error) {
			var id int64
			var ref ScimRef
			err := r.Scan(&id, &ref.Display)
			ref.Value = strconv.FormatInt(id, 10)
			return ref, err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ScimGroupList lists the groups of client c matching f.
func (s *Service) ScimGroupList(ctx context.Context, c *ScimClient, f *ScimFilter, start, count int, members bool) ([]ScimGroup, int, error) {
	where, args := "", []any{}
	if f != nil {
		switch f.Attr {
		case "displayname":
			where = ` AND lower(g.name) = lower($2)`
		case "externalid":
			where = ` AND g.external_id = $2`
		case "id":
			where = ` AND g.id::text = $2`
		default:
			return nil, 0, apperr.BadRequest("unsupported filter attribute %q", f.Attr)
		}
		args = append(args, f.Value)
	}
	all, err := s.scimGroups(ctx, c, where, args, false)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if start-1 >= total {
		return []ScimGroup{}, total, nil
	}
	end := min(start-1+count, total)
	page := all[start-1 : end]
	if members {
		for i := range page {
			g, err := s.ScimGroup(ctx, c, page[i].ID)
			if err != nil {
				return nil, 0, err
			}
			page[i] = *g
		}
	}
	return page, total, nil
}

// ScimGroup returns a group of client c with its members.
func (s *Service) ScimGroup(ctx context.Context, c *ScimClient, id int64) (*ScimGroup, error) {
	list, err := s.scimGroups(ctx, c, ` AND g.id=$2`, []any{id}, true)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperr.NotFound("group")
	}
	return &list[0], nil
}

func checkScimGroup(g *ScimGroup) error {
	g.DisplayName = strings.TrimSpace(g.DisplayName)
	if g.DisplayName == "" || len(g.DisplayName) > 200 || len(g.ExternalID) > 256 {
		return apperr.BadRequest("displayName is required (at most 200 characters)")
	}
	if len(g.Members) > 50000 {
		return apperr.BadRequest("too many members")
	}
	return nil
}

// ScimCreateGroup creates a group managed by client c.
func (s *Service) ScimCreateGroup(ctx context.Context, c *ScimClient, g ScimGroup) (*ScimGroup, error) {
	if err := checkScimGroup(&g); err != nil {
		return nil, err
	}
	var id int64
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO groups(name, source, scim_client, external_id) VALUES ($1,'scim',$2,$3) RETURNING id`,
			g.DisplayName, c.ID, g.ExternalID).Scan(&id)
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("the group %s exists", g.DisplayName)
		}
		if err != nil {
			return err
		}
		if err := s.scimSetMembers(ctx, tx, c, id, g.Members); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "scim.group_create", "group", id, nil, map[string]any{"name": g.DisplayName, "members": len(g.Members)})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.ScimGroup(ctx, c, id)
}

// ScimReplaceGroup sets name and members of a group of client c.
func (s *Service) ScimReplaceGroup(ctx context.Context, c *ScimClient, id int64, g ScimGroup) (*ScimGroup, error) {
	if err := checkScimGroup(&g); err != nil {
		return nil, err
	}
	if _, err := s.ScimGroup(ctx, c, id); err != nil {
		return nil, err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE groups SET name=$2, external_id=$3 WHERE id=$1 AND scim_client=$4`, id, g.DisplayName, g.ExternalID, c.ID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("the group %s exists", g.DisplayName)
			}
			return err
		}
		if err := s.scimSetMembers(ctx, tx, c, id, g.Members); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "scim.group_update", "group", id, nil, map[string]any{"name": g.DisplayName, "members": len(g.Members)})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.ScimGroup(ctx, c, id)
}

// scimSetMembers makes members the members of group gid. New members must
// be people the client may see; removing works for anybody.
func (s *Service) scimSetMembers(ctx context.Context, tx pgx.Tx, c *ScimClient, gid int64, members []ScimRef) error {
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		id, err := strconv.ParseInt(m.Value, 10, 64)
		if err != nil || id <= 0 {
			return apperr.BadRequest("invalid member %q", m.Value)
		}
		ids = append(ids, id)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE group_id=$1 AND NOT (user_id = ANY($2))`, gid, ids); err != nil {
		return err
	}
	var bad int64
	err := tx.QueryRow(ctx, `SELECT x FROM unnest($3::bigint[]) x WHERE NOT EXISTS (SELECT 1 FROM user_groups WHERE group_id=$4 AND user_id=x)
		AND NOT EXISTS (SELECT 1 FROM users u WHERE `+s.scimUserScope()+` AND u.id=x) LIMIT 1`, c.ID, c.Provider, ids, gid).Scan(&bad)
	if err == nil {
		return apperr.BadRequest("member %d is unknown or not managed by this client", bad)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id, source) SELECT x, $2, 'scim' FROM unnest($1::bigint[]) x ON CONFLICT DO NOTHING`, ids, gid)
	return err
}

// ScimDeleteGroup deletes a group of client c.
func (s *Service) ScimDeleteGroup(ctx context.Context, c *ScimClient, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id=$1 AND scim_client=$2`, id, c.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("group")
	}
	s.bump(ctx)
	return audit.Log(ctx, s.pool, "scim.group_delete", "group", id, nil, nil)
}
