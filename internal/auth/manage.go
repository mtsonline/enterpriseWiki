package auth

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// User is the administrative view of a user.
type User struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Provider    string     `json:"provider"`
	Handle      string     `json:"handle"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Language    string     `json:"language"`
	Disabled    bool       `json:"disabled"`
	LockedUntil *time.Time `json:"locked_until"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
	Groups      []int64    `json:"groups"`
	Roles       []int64    `json:"roles"`
	// Alias is the optional wiki user name; AliasSource the provider that
	// manages it ("" = set in the wiki).
	Alias       *string `json:"alias"`
	AliasSource string  `json:"alias_source"`
	// MFA: the person has set up a further factor.
	MFA bool `json:"mfa"`
	// Networks the account may be used from (empty = anywhere).
	Networks []string `json:"networks"`
}

func requireAdmin(ctx context.Context) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if !p.Admin() {
		return apperr.ErrForbidden
	}
	return nil
}

func (s *Service) bump(ctx context.Context) { s.bus.Publish(ctx, "auth", "") }

// ErrInstanceOnly is returned when someone who is not an instance
// administrator changes shared data on a further site.
var ErrInstanceOnly = apperr.Forbidden("users, groups and roles are shared by all sites: only instance administrators can change them here (or use the site main)")

// requireShared allows changes to users, groups and role definitions: on the
// site "main" with the normal permissions, elsewhere only for instance
// administrators.
func (s *Service) requireShared(ctx context.Context) error {
	if s.SeparateAccounts || identity.Site(ctx) == "main" || identity.From(ctx).Instance() {
		return nil
	}
	return ErrInstanceOnly
}

// guardRoles prevents privilege escalation: who is not an administrator can
// only assign roles (directly or through groups) without the admin flag and
// with permissions they have themselves.
func guardRoles(ctx context.Context, q db.Querier, roles, groups []int64) error {
	p := identity.From(ctx)
	if p.Admin() || (len(roles) == 0 && len(groups) == 0) {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT r.name, r.is_admin, r.permissions FROM roles r
		WHERE r.id = ANY($1) OR r.id IN (SELECT role_id FROM group_roles WHERE group_id = ANY($2))`, roles, groups)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var admin bool
		var perms []string
		if err := rows.Scan(&name, &admin, &perms); err != nil {
			return err
		}
		if admin {
			return apperr.Forbidden("only administrators can assign the administrator role %q", name)
		}
		for _, k := range perms {
			if !perm.HasP(p, k) {
				return apperr.Forbidden("role %q includes the permission %s, which you do not have", name, k)
			}
		}
	}
	return rows.Err()
}

// guardTarget prevents changes to administrators by non-administrators.
func guardTarget(ctx context.Context, q db.Querier, uid int64) error {
	if identity.From(ctx).Admin() {
		return nil
	}
	var admin bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles r WHERE r.is_admin AND (
			r.id IN (SELECT role_id FROM user_roles WHERE user_id=$1) OR
			r.id IN (SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=$1)))`, uid).Scan(&admin); err != nil {
		return err
	}
	if admin {
		return apperr.Forbidden("only administrators can change administrators")
	}
	return nil
}

// ListUsers lists users matching q (admin only).
func (s *Service) ListUsers(ctx context.Context, q string, limit, offset int) ([]User, error) {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.username, u.provider, u.display_name, u.email, u.language, u.disabled, u.locked_until,
			u.created_at, u.last_login_at,
			COALESCE((SELECT array_agg(group_id ORDER BY group_id) FROM user_groups WHERE user_id=u.id), '{}'),
			COALESCE((SELECT array_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id=u.id), '{}'), u.alias, u.alias_source
		FROM users u WHERE $1 = '' OR u.username ILIKE '%'||$1||'%' OR u.display_name ILIKE '%'||$1||'%' OR u.email ILIKE '%'||$1||'%'
			OR u.alias ILIKE '%'||$1||'%'
		ORDER BY u.username, u.provider LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanUser)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	on := s.mfaAmong(ctx, ids)
	for i := range list {
		list[i].MFA = on[list[i].ID]
	}
	return list, nil
}

func scanUser(r pgx.CollectableRow) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Username, &u.Provider, &u.DisplayName, &u.Email, &u.Language, &u.Disabled, &u.LockedUntil,
		&u.CreatedAt, &u.LastLoginAt, &u.Groups, &u.Roles, &u.Alias, &u.AliasSource)
	u.Handle = u.Username + "@" + u.Provider
	return u, err
}

// GetUser returns one user (admin only).
func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.username, u.provider, u.display_name, u.email, u.language, u.disabled, u.locked_until,
			u.created_at, u.last_login_at,
			COALESCE((SELECT array_agg(group_id ORDER BY group_id) FROM user_groups WHERE user_id=u.id), '{}'),
			COALESCE((SELECT array_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id=u.id), '{}'), u.alias, u.alias_source
		FROM users u WHERE u.id=$1`, id)
	if err != nil {
		return nil, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("user")
	}
	u.MFA = len(s.EnrolledMFA(ctx, u.ID)) > 0
	return &u, err
}

// NewUser is the input for CreateLocalUser.
type NewUser struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Language    string  `json:"language"`
	Groups      []int64 `json:"groups"`
	Roles       []int64 `json:"roles"`
}

// CreateLocalUser creates a local account (admin only).
func (s *Service) CreateLocalUser(ctx context.Context, in NewUser) (*User, error) {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return nil, err
	}
	if err := s.requireShared(ctx); err != nil {
		return nil, err
	}
	return s.createLocalUser(ctx, in)
}

// CreateLocalUserUnchecked is used by setup to create the first admin.
func (s *Service) CreateLocalUserUnchecked(ctx context.Context, in NewUser) (*User, error) {
	return s.createLocalUser(identity.With(ctx, identity.CLI()), in)
}

func (s *Service) createLocalUser(ctx context.Context, in NewUser) (*User, error) {
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || strings.Contains(in.Username, "@") {
		return nil, apperr.BadRequest("username is required and must not contain '@'")
	}
	if err := CheckPasswordPolicy(in.Password, s.cfg.Get().Auth.MinPasswordLen); err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		// a local user name never equals somebody's wiki user name
		var alias bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(alias)=lower($1))`, in.Username).Scan(&alias); err != nil {
			return err
		}
		if alias {
			return apperr.Conflict("%s is the user name of another account", in.Username)
		}
		err := tx.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, language, password_hash)
			VALUES ($1,'local',$1,$2,$3,$4) RETURNING id`, in.Username, in.DisplayName, in.Language, hash).Scan(&id)
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("user %s@local already exists", in.Username)
		}
		if err != nil {
			return apperr.BadRequest("invalid user: %v", err)
		}
		// given by an administrator (or setup): confirmed
		if err := setAdminPrimary(ctx, tx, id, in.Email); err != nil {
			return err
		}
		if err := guardRoles(ctx, tx, in.Roles, in.Groups); err != nil {
			return err
		}
		if err := setMembership(ctx, tx, id, in.Groups, in.Roles); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.create", "user", id, nil, map[string]any{"username": in.Username, "groups": in.Groups, "roles": in.Roles})
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(identity.With(ctx, identity.CLI()), id)
}

func setMembership(ctx context.Context, tx pgx.Tx, uid int64, groups, roles []int64) error {
	if groups != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND source='local'`, uid); err != nil {
			return err
		}
		for _, g := range groups {
			if _, err := tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, g); err != nil {
				if db.IsForeignKeyViolation(err) {
					return apperr.BadRequest("group %d does not exist", g)
				}
				return err
			}
		}
	}
	if roles != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND source='local'`, uid); err != nil {
			return err
		}
		for _, r := range roles {
			if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, r); err != nil {
				if db.IsForeignKeyViolation(err) {
					return apperr.BadRequest("role %d does not exist", r)
				}
				return err
			}
		}
	}
	return nil
}

// UserUpdate holds optional changes to a user.
type UserUpdate struct {
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Language    *string `json:"language"`
	Disabled    *bool   `json:"disabled"`
	Unlock      bool    `json:"unlock"`
	Groups      []int64 `json:"groups"`
	Roles       []int64 `json:"roles"`
	Password    *string `json:"password"`
	ResetMFA    bool    `json:"reset_mfa"` // removes all factors (MFA plugins)
}

// UpdateUser changes a user (admin only). The last active admin cannot be
// disabled or stripped of the admin role.
func (s *Service) UpdateUser(ctx context.Context, id int64, in UserUpdate) (*User, error) {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return nil, err
	}
	// role assignments belong to the site; everything else is shared
	if in.DisplayName != nil || in.Email != nil || in.Language != nil || in.Disabled != nil || in.Unlock ||
		in.Groups != nil || in.Password != nil || in.ResetMFA {
		if err := s.requireShared(ctx); err != nil {
			return nil, err
		}
	}
	before, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	var hash *string
	if in.Password != nil {
		if before.Provider != "local" {
			return nil, apperr.BadRequest("passwords can only be set for local accounts")
		}
		if err := CheckPasswordPolicy(*in.Password, s.cfg.Get().Auth.MinPasswordLen); err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		h, err := HashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		hash = &h
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET
				display_name = COALESCE($2, display_name), language = COALESCE($3, language),
				disabled = COALESCE($4, disabled), disabled_by = CASE WHEN $4 IS NULL THEN disabled_by ELSE '' END,
				password_hash = COALESCE($5, password_hash),
				failed_logins = CASE WHEN $6 THEN 0 ELSE failed_logins END,
				locked_until = CASE WHEN $6 THEN NULL ELSE locked_until END, updated_at = now()
			WHERE id=$1`, id, in.DisplayName, in.Language, in.Disabled, hash, in.Unlock); err != nil {
			return err
		}
		if in.Email != nil {
			// set by an administrator: confirmed, and the primary address
			if err := setAdminPrimary(ctx, tx, id, *in.Email); err != nil {
				return err
			}
		}
		if err := guardTarget(ctx, tx, id); err != nil {
			return err
		}
		if err := guardRoles(ctx, tx, in.Roles, in.Groups); err != nil {
			return err
		}
		if err := setMembership(ctx, tx, id, in.Groups, in.Roles); err != nil {
			return err
		}
		if hash != nil || (in.Disabled != nil && *in.Disabled) {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
				return err
			}
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		after := in
		after.Password = nil
		return audit.Log(ctx, tx, "user.update", "user", id, before, after)
	})
	if err != nil {
		return nil, err
	}
	if in.ResetMFA {
		// second factors of plugins too (security keys, passkeys)
		for _, m := range s.MFAMethods() {
			if r, ok := m.(MFAResetter); ok {
				if err := r.ResetMFA(ctx, id); err != nil {
					return nil, err
				}
			}
		}
	}
	s.bump(ctx)
	return s.GetUser(ctx, id)
}

func ensureAdminRemains(ctx context.Context, tx pgx.Tx) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users u WHERE NOT u.disabled AND EXISTS (
			SELECT 1 FROM roles r WHERE r.is_admin AND (
				r.id IN (SELECT role_id FROM user_roles WHERE user_id=u.id) OR
				r.id IN (SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=u.id)))`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return apperr.Conflict("this change would leave the system without an active administrator")
	}
	return nil
}

// DeleteUser deletes a user (admin only). Content stays; authorship is cleared.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return err
	}
	if err := s.requireShared(ctx); err != nil {
		return err
	}
	before, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := guardTarget(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM acl_rules WHERE subject_type='user' AND subject_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
			return err
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.delete", "user", id, before, nil)
	})
	if err == nil {
		s.bump(ctx)
		s.bus.Publish(ctx, "acl", "")
	}
	return err
}

// ChangeOwnPassword changes the caller's local password and ends all other
// sessions of the caller (keep is the token of the current one).
func (s *Service) ChangeOwnPassword(ctx context.Context, current, newPw, keep string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if p.Provider != "local" {
		return apperr.BadRequest("password is managed by %s", p.Provider)
	}
	var hash *string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	if hash == nil || !VerifyPassword(*hash, current) {
		return apperr.BadRequest("current password is wrong")
	}
	if err := CheckPasswordPolicy(newPw, s.cfg.Get().Auth.MinPasswordLen); err != nil {
		return apperr.BadRequest("%s", err.Error())
	}
	h, err := HashPassword(newPw)
	if err != nil {
		return err
	}
	keepHash := curHash(keep)
	if keepHash == nil {
		keepHash = []byte{}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, p.UserID, h); err != nil {
			return err
		}
		// whoever knew the old password is signed out everywhere else
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash <> $2`, p.UserID, keepHash); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.password", "user", p.UserID, nil, nil)
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}

// UpdateProfile changes the caller's display name and language. A new
// e-mail address is added unconfirmed (confirmation link); it becomes the
// primary address once confirmed (see emails.go).
func (s *Service) UpdateProfile(ctx context.Context, displayName, email, language *string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if email != nil {
		addr, err := NormalizeEmail(*email)
		if err != nil {
			return err
		}
		var known bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails WHERE user_id=$1 AND email=$2)`, p.UserID, addr).Scan(&known); err != nil {
			return err
		}
		if addr != "" && !known {
			if _, err := s.AddMyEmail(ctx, addr); err != nil {
				return err
			}
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET display_name=COALESCE($2,display_name),
		language=COALESCE($3,language), updated_at=now() WHERE id=$1`, p.UserID, displayName, language)
	s.bump(ctx)
	return err
}

// Prefs are the personal settings a user may store, with their allowed
// values. Plugins and the core register them (RegisterPref).
var (
	prefsMu sync.RWMutex
	prefs   = map[string][]string{}
)

// RegisterPref allows a personal setting with the given values ("" always
// removes the setting).
func RegisterPref(key string, values ...string) {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	prefs[key] = values
}

func init() {
	RegisterPref("editor", "visual", "simple")
}

// PrefOptions returns the personal settings and their allowed values.
func PrefOptions() map[string][]string {
	prefsMu.RLock()
	defer prefsMu.RUnlock()
	out := make(map[string][]string, len(prefs))
	for k, v := range prefs {
		out[k] = slices.Clone(v)
	}
	return out
}

// SetPrefs changes personal settings of the signed-in user; "" removes one.
func (s *Service) SetPrefs(ctx context.Context, in map[string]string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	patch := map[string]any{}
	prefsMu.RLock()
	for k, v := range in {
		allowed, ok := prefs[k]
		if !ok {
			prefsMu.RUnlock()
			return apperr.BadRequest("unknown setting %q", k)
		}
		if v == "" {
			patch[k] = nil
			continue
		}
		if !slices.Contains(allowed, v) {
			prefsMu.RUnlock()
			return apperr.BadRequest("invalid value %q for %s (allowed: %s)", v, k, strings.Join(allowed, ", "))
		}
		patch[k] = v
	}
	prefsMu.RUnlock()
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	// jsonb_strip_nulls removes the settings set to "" (null in the patch)
	_, err = s.pool.Exec(ctx, `UPDATE users SET prefs=jsonb_strip_nulls(prefs || $2::jsonb), updated_at=now() WHERE id=$1`, p.UserID, b)
	s.bump(ctx)
	return err
}

// ---------------------------------------------------------------- groups & roles

// Group is a user group.
type Group struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	Members     int     `json:"members"`
	Roles       []int64 `json:"roles"`
}

// Role is a role; admin roles bypass ACL checks.
type Role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsAdmin     bool     `json:"is_admin"`
	Members     int      `json:"members"`
	Permissions []string `json:"permissions"`
}

// ListGroups lists all groups (any authenticated user, needed for ACL editing).
func (s *Service) ListGroups(ctx context.Context) ([]Group, error) {
	if identity.From(ctx) == nil {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT g.id, g.name, g.description, g.source,
		(SELECT count(*) FROM user_groups WHERE group_id=g.id),
		COALESCE((SELECT array_agg(role_id ORDER BY role_id) FROM group_roles WHERE group_id=g.id), '{}')
		FROM groups g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Group, error) {
		var g Group
		return g, r.Scan(&g.ID, &g.Name, &g.Description, &g.Source, &g.Members, &g.Roles)
	})
}

// SaveGroup creates (id==0) or updates a group (admin only).
func (s *Service) SaveGroup(ctx context.Context, id int64, name, description string, roles []int64) (int64, error) {
	if err := perm.Require(ctx, "core.groups.manage"); err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, apperr.BadRequest("name is required")
	}
	if s.requireShared(ctx) != nil { // site administrators only assign roles to existing groups
		var curName, curDesc string
		if id == 0 || s.pool.QueryRow(ctx, `SELECT name, description FROM groups WHERE id=$1`, id).Scan(&curName, &curDesc) != nil ||
			curName != name || curDesc != description {
			return 0, ErrInstanceOnly
		}
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := guardRoles(ctx, tx, roles, nil); err != nil {
			return err
		}
		if id != 0 {
			if err := guardRoles(ctx, tx, nil, []int64{id}); err != nil {
				return err
			}
		}
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO groups(name, description) VALUES ($1,$2) RETURNING id`, name, description).Scan(&id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE groups SET name=$2, description=$3 WHERE id=$1`, id, name, description)
			if err == nil && tag.RowsAffected() == 0 {
				return apperr.NotFound("group")
			}
		}
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("group %q already exists", name)
		}
		if err != nil {
			return err
		}
		if roles != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM group_roles WHERE group_id=$1`, id); err != nil {
				return err
			}
			for _, r := range roles {
				if _, err := tx.Exec(ctx, `INSERT INTO group_roles(group_id, role_id) VALUES ($1,$2)`, id, r); err != nil {
					return apperr.BadRequest("role %d does not exist", r)
				}
			}
			if err := ensureAdminRemains(ctx, tx); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "group.save", "group", id, nil, map[string]any{"name": name, "description": description, "roles": roles})
	})
	s.bump(ctx)
	return id, err
}

// DeleteGroup deletes a group and its ACL rules (admin only).
func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	if err := perm.Require(ctx, "core.groups.manage"); err != nil {
		return err
	}
	if err := s.requireShared(ctx); err != nil {
		return err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM acl_rules WHERE subject_type='group' AND subject_id=$1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM groups WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("group")
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "group.delete", "group", id, nil, nil)
	})
	if err == nil {
		s.bump(ctx)
		s.bus.Publish(ctx, "acl", "")
	}
	return err
}

// GroupMembers lists the user ids of a group.
func (s *Service) GroupMembers(ctx context.Context, id int64) ([]int64, error) {
	if err := perm.Require(ctx, "core.groups.manage"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM user_groups WHERE group_id=$1 ORDER BY user_id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// SetGroupMember adds (member=true) or removes a person from a group. The
// same guards as for changing a user apply: no administrators changed by
// others, no roles passed on that the caller does not have, and the last
// administrator stays. Returns whether something changed.
func (s *Service) SetGroupMember(ctx context.Context, group, user int64, member bool) (bool, error) {
	if err := perm.Require(ctx, "core.groups.manage"); err != nil {
		return false, err
	}
	if err := s.requireShared(ctx); err != nil {
		return false, err
	}
	changed := false
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1) AND EXISTS(SELECT 1 FROM users WHERE id=$2)`, group, user).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound("group or user")
		}
		if err := guardTarget(ctx, tx, user); err != nil {
			return err
		}
		if err := guardRoles(ctx, tx, nil, []int64{group}); err != nil {
			return err
		}
		var tag pgconn.CommandTag
		var err error
		if member {
			tag, err = tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, user, group)
		} else {
			tag, err = tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND group_id=$2`, user, group)
		}
		if err != nil {
			return err
		}
		if changed = tag.RowsAffected() > 0; !changed {
			return nil
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		action := "group.member_add"
		if !member {
			action = "group.member_remove"
		}
		return audit.Log(ctx, tx, action, "group", group, nil, map[string]int64{"user": user})
	})
	if err == nil && changed {
		s.bump(ctx)
		s.bus.Publish(ctx, "acl", "")
	}
	return changed, err
}

// ListRoles lists all roles.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	if identity.From(ctx) == nil {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.name, r.description, r.is_admin, (SELECT count(*) FROM user_roles WHERE role_id=r.id), r.permissions
		FROM roles r ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Role, error) {
		var x Role
		return x, r.Scan(&x.ID, &x.Name, &x.Description, &x.IsAdmin, &x.Members, &x.Permissions)
	})
}

// RoleInput creates or changes a role.
type RoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsAdmin     bool     `json:"is_admin"`
	Permissions []string `json:"permissions"` // global permissions (package perm)
}

// SaveRole creates (id==0) or updates a role (core.roles.manage). Who is not
// an administrator cannot touch administrator roles and can only grant
// permissions they have themselves.
func (s *Service) SaveRole(ctx context.Context, id int64, in RoleInput) (int64, error) {
	if err := perm.Require(ctx, "core.roles.manage"); err != nil {
		return 0, err
	}
	if err := s.requireShared(ctx); err != nil {
		return 0, err
	}
	name, description, isAdmin := strings.TrimSpace(in.Name), in.Description, in.IsAdmin
	if name == "" {
		return 0, apperr.BadRequest("name is required")
	}
	if in.Permissions == nil {
		in.Permissions = []string{}
	}
	if err := perm.Validate(perm.Global, in.Permissions); err != nil {
		return 0, err
	}
	p := identity.From(ctx)
	if !p.Admin() {
		if isAdmin {
			return 0, apperr.Forbidden("only administrators can create administrator roles")
		}
		for _, k := range in.Permissions {
			if !perm.HasP(p, k) {
				return 0, apperr.Forbidden("you cannot grant the permission %s, which you do not have", k)
			}
		}
	}
	var before *Role
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if id != 0 {
			before = &Role{ID: id}
			if err := tx.QueryRow(ctx, `SELECT name, description, is_admin, permissions FROM roles WHERE id=$1`, id).
				Scan(&before.Name, &before.Description, &before.IsAdmin, &before.Permissions); err != nil {
				return apperr.NotFound("role")
			}
			if !p.Admin() {
				if before.IsAdmin {
					return apperr.Forbidden("only administrators can change administrator roles")
				}
				for _, k := range before.Permissions { // nor remove permissions they cannot grant
					if !perm.HasP(p, k) && !slices.Contains(in.Permissions, k) {
						return apperr.Forbidden("you cannot remove the permission %s, which you do not have", k)
					}
				}
			}
		}
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO roles(name, description, is_admin, permissions) VALUES ($1,$2,$3,$4) RETURNING id`, name, description, isAdmin, in.Permissions).Scan(&id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE roles SET name=$2, description=$3, is_admin=$4, permissions=$5 WHERE id=$1`, id, name, description, isAdmin, in.Permissions)
			if err == nil && tag.RowsAffected() == 0 {
				return apperr.NotFound("role")
			}
		}
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("role %q already exists", name)
		}
		if err != nil {
			return err
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "role.save", "role", id, before, map[string]any{"name": name, "description": description, "is_admin": isAdmin, "permissions": in.Permissions})
	})
	s.bump(ctx)
	return id, err
}

// SetDefaultPermissions sets the global permissions of every signed-in user.
func (s *Service) SetDefaultPermissions(ctx context.Context, perms []string) error {
	if err := perm.Require(ctx, "core.roles.manage"); err != nil {
		return err
	}
	if perms == nil {
		perms = []string{}
	}
	if err := perm.Validate(perm.Global, perms); err != nil {
		return err
	}
	p := identity.From(ctx)
	before, err := s.DefaultPermissions(ctx)
	if err != nil {
		return err
	}
	if !p.Admin() {
		for _, k := range append(slices.Clone(perms), before...) {
			if !perm.HasP(p, k) && slices.Contains(perms, k) != slices.Contains(before, k) {
				return apperr.Forbidden("you cannot change the permission %s, which you do not have", k)
			}
		}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO permission_defaults(subject, permissions) VALUES ('authenticated', $1)
			ON CONFLICT (subject) DO UPDATE SET permissions=EXCLUDED.permissions, updated_at=now()`, perms); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "permissions.defaults", "role", 0, before, perms)
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}

// DeleteRole deletes a role (admin only).
func (s *Service) DeleteRole(ctx context.Context, id int64) error {
	if err := perm.Require(ctx, "core.roles.manage"); err != nil {
		return err
	}
	if err := s.requireShared(ctx); err != nil {
		return err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if !identity.From(ctx).Admin() {
			var admin bool
			var perms []string
			if err := tx.QueryRow(ctx, `SELECT is_admin, permissions FROM roles WHERE id=$1`, id).Scan(&admin, &perms); err != nil {
				return apperr.NotFound("role")
			}
			if err := guardRoles(ctx, tx, []int64{id}, nil); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM acl_rules WHERE subject_type='role' AND subject_id=$1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM roles WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("role")
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "role.delete", "role", id, nil, nil)
	})
	if err == nil {
		s.bump(ctx)
		s.bus.Publish(ctx, "acl", "")
	}
	return err
}

// EnsureRole returns the id of role name, creating it if needed (setup).
func (s *Service) EnsureRole(ctx context.Context, name, description string, isAdmin bool) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO roles(name, description, is_admin) VALUES ($1,$2,$3)
		ON CONFLICT (name) DO UPDATE SET is_admin = roles.is_admin OR EXCLUDED.is_admin RETURNING id`, name, description, isAdmin).Scan(&id)
	return id, err
}

// ---------------------------------------------------------------- API keys

// APIKey describes an API key (the secret is only returned on creation).
type APIKey struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Permissions int        `json:"permissions"`
	CategoryID  *int64     `json:"category_id"`
	AllowAdmin  bool       `json:"allow_admin"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	// RatePerSecond is the key's own limit of requests (nil: that of the
	// account). RateOwn: granted by an administrator, a budget of its own.
	RatePerSecond *int   `json:"rate_per_second"`
	RateOwn       bool   `json:"rate_own"`
	Owner         string `json:"owner,omitempty"` // lists of administrators
	Token         string `json:"token,omitempty"`
}

// NewAPIKey is the input for CreateAPIKey.
type NewAPIKey struct {
	Name        string    `json:"name"`
	Permissions int       `json:"permissions"`
	CategoryID  *int64    `json:"category_id"`
	AllowAdmin  bool      `json:"allow_admin"`
	ExpiresAt   time.Time `json:"expires_at"`
	// RatePerSecond limits the key below the limit of the account.
	RatePerSecond *int `json:"rate_per_second"`
}

// KeyRateMax returns the highest limit of requests per second the owner
// of a key may choose (that of accounts) and the highest an administrator
// may grant; set by the application (expert settings).
var KeyRateMax = func() (owner, admin int) { return 50, 10000 }

// CreateAPIKey creates a key for the caller. Keys can never exceed the
// caller's own rights: permissions are intersected at request time.
func (s *Service) CreateAPIKey(ctx context.Context, in NewAPIKey) (*APIKey, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if p.Via == "apikey" {
		return nil, apperr.ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, apperr.BadRequest("name is required")
	}
	if in.Permissions <= 0 || in.Permissions > 31 {
		return nil, apperr.BadRequest("permissions must be a bitmask between 1 and 31")
	}
	maxTTL := config.Duration(s.cfg.Get().Auth.APIKeyMaxTTL, 365*24*time.Hour)
	if in.ExpiresAt.IsZero() {
		in.ExpiresAt = time.Now().Add(90 * 24 * time.Hour)
	}
	if in.ExpiresAt.Before(time.Now()) || in.ExpiresAt.After(time.Now().Add(maxTTL)) {
		return nil, apperr.BadRequest("expires_at must be in the future and within %s", maxTTL)
	}
	if in.AllowAdmin && !p.IsAdmin {
		return nil, apperr.ErrForbidden
	}
	if own, _ := KeyRateMax(); in.RatePerSecond != nil && (*in.RatePerSecond < 1 || *in.RatePerSecond > own) {
		return nil, apperr.BadRequest("rate_per_second must be between 1 and %d", own)
	}
	tok := NewToken("ewk_")
	k := &APIKey{Name: in.Name, Prefix: tok[:12], Permissions: in.Permissions, CategoryID: in.CategoryID,
		AllowAdmin: in.AllowAdmin, ExpiresAt: in.ExpiresAt, RatePerSecond: in.RatePerSecond, Token: tok}
	err := s.pool.QueryRow(ctx, `INSERT INTO api_keys(user_id, name, prefix, token_hash, permissions, category_id, allow_admin, expires_at, rate_per_second)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`, p.UserID, k.Name, k.Prefix, HashToken(tok), k.Permissions,
		k.CategoryID, k.AllowAdmin, k.ExpiresAt, k.RatePerSecond).Scan(&k.ID, &k.CreatedAt)
	if db.IsForeignKeyViolation(err) {
		return nil, apperr.BadRequest("category does not exist")
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "apikey.create", "apikey", k.ID, nil, map[string]any{"name": k.Name, "permissions": k.Permissions, "category_id": k.CategoryID, "allow_admin": k.AllowAdmin, "expires_at": k.ExpiresAt, "rate_per_second": k.RatePerSecond})
	return k, nil
}

// ListAPIKeys lists the caller's keys.
func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT `+keyCols+`, '' FROM api_keys k WHERE k.user_id=$1 ORDER BY k.created_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKey)
}

const keyCols = `k.id, k.name, k.prefix, k.permissions, k.category_id, k.allow_admin, k.expires_at, k.created_at, k.last_used_at, k.revoked_at, k.rate_per_second, k.rate_own`

func scanKey(r pgx.CollectableRow) (APIKey, error) {
	var k APIKey
	return k, r.Scan(&k.ID, &k.Name, &k.Prefix, &k.Permissions, &k.CategoryID, &k.AllowAdmin, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt, &k.RatePerSecond, &k.RateOwn, &k.Owner)
}

// AllAPIKeys lists the valid keys of all accounts (administrators): who
// owns them and their limits, never the secrets.
func (s *Service) AllAPIKeys(ctx context.Context) ([]APIKey, error) {
	if p := identity.From(ctx); !p.Admin() {
		return nil, apperr.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+keyCols+`, u.username || '@' || u.provider FROM api_keys k JOIN users u ON u.id=k.user_id
		WHERE k.revoked_at IS NULL AND k.expires_at > now() ORDER BY u.username, k.created_at DESC LIMIT 5000`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKey)
}

// SetAPIKeyRate sets the own limit of requests per second of a key (nil:
// none, the limit of the account applies). The owner may limit a key below
// the limit of the account; a limit set by an administrator is a budget of
// its own and may be higher (integrations).
func (s *Service) SetAPIKeyRate(ctx context.Context, id int64, rate *int, asAdmin bool) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if p.Via == "apikey" || (asAdmin && !p.Admin()) {
		return apperr.ErrForbidden // a key never changes limits, not even its own
	}
	own, admin := KeyRateMax()
	max := own
	if asAdmin {
		max = admin
	}
	if rate != nil && (*rate < 1 || *rate > max) {
		return apperr.BadRequest("rate_per_second must be between 1 and %d", max)
	}
	var before *int
	// the owner cannot take over a budget granted by an administrator
	err := s.pool.QueryRow(ctx, `UPDATE api_keys k SET rate_per_second=$2, rate_own=($3 AND $2::int IS NOT NULL)
		FROM (SELECT id, rate_per_second FROM api_keys WHERE id=$1 FOR UPDATE) o
		WHERE k.id=o.id AND k.revoked_at IS NULL AND ($3 OR (k.user_id=$4 AND NOT k.rate_own)) RETURNING o.rate_per_second`, id, rate, asAdmin, p.UserID).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("api key")
	}
	if err != nil {
		return err
	}
	s.bump(ctx) // cached sign-ins carry the limit
	return audit.Log(ctx, s.pool, "apikey.rate", "apikey", id, map[string]any{"rate_per_second": before}, map[string]any{"rate_per_second": rate, "own_budget": asAdmin && rate != nil})
}

// RevokeAPIKey revokes one of the caller's keys (admins may revoke any key).
func (s *Service) RevokeAPIKey(ctx context.Context, id int64) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL AND (user_id=$2 OR $3)`, id, p.UserID, p.Admin())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("api key")
	}
	s.bump(ctx)
	return audit.Log(ctx, s.pool, "apikey.revoke", "apikey", id, nil, nil)
}

// ---------------------------------------------------------------- SSH keys

// SSHKey is a registered public key.
type SSHKey struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	PublicKey   string     `json:"public_key"`
	Fingerprint string     `json:"fingerprint"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// AddSSHKey registers a public key for the caller.
func (s *Service) AddSSHKey(ctx context.Context, name, publicKey string) (*SSHKey, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	_, fp, err := ParseAuthorizedKey(publicKey)
	if err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = fp
	}
	k := &SSHKey{Name: name, PublicKey: strings.TrimSpace(publicKey), Fingerprint: fp}
	err = s.pool.QueryRow(ctx, `INSERT INTO ssh_keys(user_id, name, public_key, fingerprint) VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		p.UserID, k.Name, k.PublicKey, fp).Scan(&k.ID, &k.CreatedAt)
	if db.IsUniqueViolation(err) {
		return nil, apperr.Conflict("this key is already registered")
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "sshkey.add", "sshkey", k.ID, nil, map[string]any{"fingerprint": fp})
	return k, nil
}

// ListSSHKeys lists the caller's keys.
func (s *Service) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name, public_key, fingerprint, created_at, last_used_at FROM ssh_keys WHERE user_id=$1 ORDER BY created_at`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SSHKey, error) {
		var k SSHKey
		return k, r.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.LastUsedAt)
	})
}

// DeleteSSHKey removes one of the caller's keys.
func (s *Service) DeleteSSHKey(ctx context.Context, id int64) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM ssh_keys WHERE id=$1 AND (user_id=$2 OR $3)`, id, p.UserID, p.Admin())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("ssh key")
	}
	return audit.Log(ctx, s.pool, "sshkey.delete", "sshkey", id, nil, nil)
}
