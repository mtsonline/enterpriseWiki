// Workflow 2, part 3a: steps for accounts and pages – permissions on a
// category (optionally for some days), group membership, moving a page.
// All of them act through the wiki's API with the rights of the definer
// (or starter): nobody gets more through a workflow than they could do
// themselves, and every change is in the audit log.
package workflow_process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// AccessSpec grants or revokes permissions on a category.
type AccessSpec struct {
	Mode     string `json:"mode"`     // grant | revoke
	Category int64  `json:"category"` // the category
	Subject  string `json:"subject"`  // user | group
	// User: the person (user name, alias, e-mail or id; with placeholders,
	// e.g. {{trigger.data.username}} or {{starter.username}}).
	User  string `json:"user,omitempty"`
	Group int64  `json:"group,omitempty"`
	Level string `json:"level,omitempty"` // grant: reader | editor | manager
	// Days: the permission is removed again after this many days (0: stays).
	Days int `json:"days,omitempty"`
}

// MemberSpec puts a person into a group or takes them out.
type MemberSpec struct {
	Mode  string `json:"mode"` // add | remove
	Group int64  `json:"group"`
	User  string `json:"user"`
}

// MoveSpec moves a page to another category (e.g. an archive).
type MoveSpec struct {
	Element  string `json:"element"` // id or address, with placeholders
	Category int64  `json:"category"`
}

// MaxGrantDays limits permissions for a time.
const MaxGrantDays = 3650

func (s *Step) validatePart3(ac *AccessSpec, mb *MemberSpec, mv *MoveSpec) error {
	var err error
	s.NextElse = ""
	switch s.Type {
	case StepAccess:
		if ac == nil {
			return apperr.BadRequest("step %q: the permissions are missing", s.ID)
		}
		a := *ac
		if !in(a.Mode, "grant", "revoke") || a.Category <= 0 || !in(a.Subject, "user", "group") {
			return apperr.BadRequest("step %q: choose grant or revoke, the category and a person or group", s.ID)
		}
		if a.Subject == "user" {
			a.Group = 0
			if a.User, err = templ(a.User, 200, "person", s.ID); err != nil || a.User == "" {
				return apperr.BadRequest("step %q: the person is required (user name or placeholder)", s.ID)
			}
		} else {
			a.User = ""
			if a.Group <= 0 {
				return apperr.BadRequest("step %q: the group is required", s.ID)
			}
		}
		if a.Mode == "grant" {
			if !in(a.Level, "reader", "editor", "manager") {
				return apperr.BadRequest("step %q: the permission must be reader, editor or manager", s.ID)
			}
			if a.Days < 0 || a.Days > MaxGrantDays {
				return apperr.BadRequest("step %q: 0 to %d days", s.ID, MaxGrantDays)
			}
		} else {
			a.Level, a.Days = "", 0
		}
		s.Access = &a
	case StepMember:
		if mb == nil {
			return apperr.BadRequest("step %q: the group membership is missing", s.ID)
		}
		m := *mb
		if !in(m.Mode, "add", "remove") || m.Group <= 0 {
			return apperr.BadRequest("step %q: choose add or remove and the group", s.ID)
		}
		if m.User, err = templ(m.User, 200, "person", s.ID); err != nil || m.User == "" {
			return apperr.BadRequest("step %q: the person is required (user name or placeholder)", s.ID)
		}
		s.Member = &m
	case StepMove:
		if mv == nil {
			return apperr.BadRequest("step %q: the page and the category are missing", s.ID)
		}
		m := *mv
		if m.Element, err = templ(m.Element, 200, "page", s.ID); err != nil || m.Element == "" || m.Category <= 0 {
			return apperr.BadRequest("step %q: the page (id or placeholder) and the category are required", s.ID)
		}
		s.Move = &m
	}
	return nil
}

// person finds the account named by a text: id, user name, alias or
// e-mail address. Several accounts with that name are an error.
func person(ctx context.Context, tx pgx.Tx, ref string) (int64, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, "", fmt.Errorf("no person given (the placeholder is empty)")
	}
	rows, err := tx.Query(ctx, `SELECT id, username FROM users
		WHERE ($2 > 0 AND id = $2) OR lower(username) = lower($1) OR lower(alias) = lower($1) OR (position('@' in $1) > 0 AND lower(email) = lower($1))
		LIMIT 2`, ref, parseID(ref))
	if err != nil {
		return 0, "", err
	}
	type acc struct {
		id   int64
		name string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (acc, error) {
		var a acc
		return a, r.Scan(&a.id, &a.name)
	})
	if err != nil {
		return 0, "", err
	}
	switch len(list) {
	case 0:
		return 0, "", fmt.Errorf("there is no account %q", ref)
	case 1:
		return list[0].id, list[0].name, nil
	}
	return 0, "", fmt.Errorf("%q names several accounts; use the id", ref)
}

func parseID(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (pl *Plugin) runAccess(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	a := s.Access
	subject, who := a.Group, fmt.Sprintf("group %d", a.Group)
	if a.Subject == "user" {
		uid, name, err := person(ctx, tx, render(a.User, pl.vars(ctx, tx, x, actorInfo{})))
		if err != nil {
			return result{}, err
		}
		subject, who = uid, name
	}
	if a.Mode == "revoke" {
		rows, err := tx.Query(ctx, `SELECT id FROM acl_rules WHERE category_id=$1 AND subject_type=$2 AND subject_id=$3`, a.Category, a.Subject, subject)
		if err != nil {
			return result{}, err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return result{}, err
		}
		for _, id := range ids { // the API checks the right to manage the category
			code, b, err := pl.call(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/acl/%d", id), nil)
			if err != nil {
				return result{}, err
			}
			if code != http.StatusNoContent && code != http.StatusOK && code != http.StatusNotFound {
				return result{}, apiError(code, b)
			}
		}
		return result{next: s.Next, action: "access", label: "revoke", note: fmt.Sprintf("%s · /c/%d", who, a.Category)}, nil
	}
	code, b, err := pl.call(ctx, http.MethodPost, fmt.Sprintf("/api/v1/categories/%d/acl", a.Category),
		map[string]any{"subject_type": a.Subject, "subject_id": subject, "preset": a.Level})
	if err != nil {
		return result{}, err
	}
	if code != http.StatusCreated {
		return result{}, apiError(code, b)
	}
	var rule struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &rule); err != nil {
		return result{}, err
	}
	note := fmt.Sprintf("%s · %s · /c/%d", who, a.Level, a.Category)
	if a.Days > 0 {
		until := time.Now().Add(time.Duration(a.Days) * 24 * time.Hour)
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_workflow_grants(rule_id, expires_at, instance_id) VALUES ($1,$2,$3)
			ON CONFLICT (rule_id) DO UPDATE SET expires_at=EXCLUDED.expires_at`, rule.ID, until, x.ID); err != nil {
			return result{}, err
		}
		note += " · " + until.Local().Format("2006-01-02")
	}
	return result{next: s.Next, action: "access", label: "grant", note: note}, nil
}

func (pl *Plugin) runMember(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	m := s.Member
	uid, name, err := person(ctx, tx, render(m.User, pl.vars(ctx, tx, x, actorInfo{})))
	if err != nil {
		return result{}, err
	}
	method := http.MethodPut
	if m.Mode == "remove" {
		method = http.MethodDelete
	}
	code, b, err := pl.call(ctx, method, fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", m.Group, uid), nil)
	if err != nil {
		return result{}, err
	}
	if code != http.StatusOK {
		return result{}, apiError(code, b)
	}
	return result{next: s.Next, action: "membership", label: m.Mode, note: fmt.Sprintf("%s · group %d", name, m.Group)}, nil
}

func (pl *Plugin) runMove(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	ref := strings.TrimSpace(render(s.Move.Element, pl.vars(ctx, tx, x, actorInfo{})))
	mm := elementRefRe.FindStringSubmatch(ref)
	if mm == nil {
		return result{}, fmt.Errorf("%q is no page (id or address)", ref)
	}
	code, b, err := pl.call(ctx, http.MethodPost, "/api/v1/elements/"+mm[1]+"/move", map[string]any{"category_id": s.Move.Category})
	if err != nil {
		return result{}, err
	}
	if code != http.StatusOK {
		return result{}, apiError(code, b)
	}
	return result{next: s.Next, action: "move", note: fmt.Sprintf("/e/%s → /c/%d", mm[1], s.Move.Category)}, nil
}

// expireGrants removes permissions granted for some days once their time
// is over. Taking permissions away is always allowed: the system does it,
// even when the person who saved the workflow lost their rights since.
func (pl *Plugin) expireGrants(ctx context.Context) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT rule_id, instance_id FROM plugin_workflow_grants WHERE expires_at <= now() ORDER BY expires_at LIMIT 500`)
	if err != nil {
		return err
	}
	type grant struct{ rule, inst int64 }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (grant, error) {
		var g grant
		var inst *int64
		err := r.Scan(&g.rule, &inst)
		if inst != nil {
			g.inst = *inst
		}
		return g, err
	})
	if err != nil {
		return err
	}
	sys := identity.With(ctx, identity.CLI())
	for _, g := range list {
		if err := pl.host.Content.DeleteRule(sys, g.rule); err != nil && !notFound(err) {
			pl.host.Log.Warn("workflow: permission not removed", "rule", g.rule, "err", err)
			continue
		}
		if _, err := pl.host.DB.Exec(ctx, `DELETE FROM plugin_workflow_grants WHERE rule_id=$1`, g.rule); err != nil {
			return err
		}
		_ = audit.Log(sys, pl.host.DB, "workflow.grant_expired", "workflow_case", g.inst, nil, map[string]int64{"rule": g.rule})
	}
	return nil
}

func notFound(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}
