package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
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

// ListUsers lists users matching q (admin only).
func (s *Service) ListUsers(ctx context.Context, q string, limit, offset int) ([]User, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.username, u.provider, u.display_name, u.email, u.language, u.disabled, u.locked_until,
			u.created_at, u.last_login_at,
			COALESCE((SELECT array_agg(group_id ORDER BY group_id) FROM user_groups WHERE user_id=u.id), '{}'),
			COALESCE((SELECT array_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id=u.id), '{}')
		FROM users u WHERE $1 = '' OR u.username ILIKE '%'||$1||'%' OR u.display_name ILIKE '%'||$1||'%' OR u.email ILIKE '%'||$1||'%'
		ORDER BY u.username, u.provider LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUser)
}

func scanUser(r pgx.CollectableRow) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Username, &u.Provider, &u.DisplayName, &u.Email, &u.Language, &u.Disabled, &u.LockedUntil,
		&u.CreatedAt, &u.LastLoginAt, &u.Groups, &u.Roles)
	u.Handle = u.Username + "@" + u.Provider
	return u, err
}

// GetUser returns one user (admin only).
func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.username, u.provider, u.display_name, u.email, u.language, u.disabled, u.locked_until,
			u.created_at, u.last_login_at,
			COALESCE((SELECT array_agg(group_id ORDER BY group_id) FROM user_groups WHERE user_id=u.id), '{}'),
			COALESCE((SELECT array_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id=u.id), '{}')
		FROM users u WHERE u.id=$1`, id)
	if err != nil {
		return nil, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("user")
	}
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
	if err := requireAdmin(ctx); err != nil {
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
		err := tx.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, email, language, password_hash)
			VALUES ($1,'local',$1,$2,$3,$4,$5) RETURNING id`, in.Username, in.DisplayName, in.Email, in.Language, hash).Scan(&id)
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("user %s@local already exists", in.Username)
		}
		if err != nil {
			return apperr.BadRequest("invalid user: %v", err)
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
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, uid); err != nil {
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
}

// UpdateUser changes a user (admin only). The last active admin cannot be
// disabled or stripped of the admin role.
func (s *Service) UpdateUser(ctx context.Context, id int64, in UserUpdate) (*User, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
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
				display_name = COALESCE($2, display_name), email = COALESCE($3, email), language = COALESCE($4, language),
				disabled = COALESCE($5, disabled), password_hash = COALESCE($6, password_hash),
				failed_logins = CASE WHEN $7 THEN 0 ELSE failed_logins END,
				locked_until = CASE WHEN $7 THEN NULL ELSE locked_until END, updated_at = now()
			WHERE id=$1`, id, in.DisplayName, in.Email, in.Language, in.Disabled, hash, in.Unlock); err != nil {
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
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	before, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
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

// ChangeOwnPassword changes the caller's local password.
func (s *Service) ChangeOwnPassword(ctx context.Context, current, newPw string) error {
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
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, p.UserID, h); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.password", "user", p.UserID, nil, nil)
	})
}

// UpdateProfile changes the caller's display name, email and language.
func (s *Service) UpdateProfile(ctx context.Context, displayName, email, language *string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET display_name=COALESCE($2,display_name), email=COALESCE($3,email),
		language=COALESCE($4,language), updated_at=now() WHERE id=$1`, p.UserID, displayName, email, language)
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
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsAdmin     bool   `json:"is_admin"`
	Members     int    `json:"members"`
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
	if err := requireAdmin(ctx); err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, apperr.BadRequest("name is required")
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
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
	if err := requireAdmin(ctx); err != nil {
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
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM user_groups WHERE group_id=$1 ORDER BY user_id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// ListRoles lists all roles.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	if identity.From(ctx) == nil {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.name, r.description, r.is_admin, (SELECT count(*) FROM user_roles WHERE role_id=r.id)
		FROM roles r ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Role, error) {
		var x Role
		return x, r.Scan(&x.ID, &x.Name, &x.Description, &x.IsAdmin, &x.Members)
	})
}

// SaveRole creates (id==0) or updates a role (admin only).
func (s *Service) SaveRole(ctx context.Context, id int64, name, description string, isAdmin bool) (int64, error) {
	if err := requireAdmin(ctx); err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, apperr.BadRequest("name is required")
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO roles(name, description, is_admin) VALUES ($1,$2,$3) RETURNING id`, name, description, isAdmin).Scan(&id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE roles SET name=$2, description=$3, is_admin=$4 WHERE id=$1`, id, name, description, isAdmin)
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
		return audit.Log(ctx, tx, "role.save", "role", id, nil, map[string]any{"name": name, "is_admin": isAdmin})
	})
	s.bump(ctx)
	return id, err
}

// DeleteRole deletes a role (admin only).
func (s *Service) DeleteRole(ctx context.Context, id int64) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
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
	Token       string     `json:"token,omitempty"`
}

// NewAPIKey is the input for CreateAPIKey.
type NewAPIKey struct {
	Name        string    `json:"name"`
	Permissions int       `json:"permissions"`
	CategoryID  *int64    `json:"category_id"`
	AllowAdmin  bool      `json:"allow_admin"`
	ExpiresAt   time.Time `json:"expires_at"`
}

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
	tok := NewToken("ewk_")
	k := &APIKey{Name: in.Name, Prefix: tok[:12], Permissions: in.Permissions, CategoryID: in.CategoryID,
		AllowAdmin: in.AllowAdmin, ExpiresAt: in.ExpiresAt, Token: tok}
	err := s.pool.QueryRow(ctx, `INSERT INTO api_keys(user_id, name, prefix, token_hash, permissions, category_id, allow_admin, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`, p.UserID, k.Name, k.Prefix, HashToken(tok), k.Permissions,
		k.CategoryID, k.AllowAdmin, k.ExpiresAt).Scan(&k.ID, &k.CreatedAt)
	if db.IsForeignKeyViolation(err) {
		return nil, apperr.BadRequest("category does not exist")
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "apikey.create", "apikey", k.ID, nil, map[string]any{"name": k.Name, "permissions": k.Permissions, "category_id": k.CategoryID, "allow_admin": k.AllowAdmin, "expires_at": k.ExpiresAt})
	return k, nil
}

// ListAPIKeys lists the caller's keys.
func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name, prefix, permissions, category_id, allow_admin, expires_at, created_at, last_used_at, revoked_at
		FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIKey, error) {
		var k APIKey
		return k, r.Scan(&k.ID, &k.Name, &k.Prefix, &k.Permissions, &k.CategoryID, &k.AllowAdmin, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	})
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
