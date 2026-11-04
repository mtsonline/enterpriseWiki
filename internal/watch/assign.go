package watch

// Watches assigned centrally (Administration → Watches, permission
// core.watches.assign) to users, groups or roles for categories (with
// subcategories) or elements. They apply to later members of the group or
// role as well. Optional assignments can be unsubscribed by each person in
// the profile, mandatory ones cannot.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Assignment is a watch assigned to a user, group or role.
type Assignment struct {
	ID          int64             `json:"id"`
	SubjectType string            `json:"subject_type"`
	SubjectID   int64             `json:"subject_id"`
	SubjectName string            `json:"subject_name"`
	TargetType  string            `json:"target_type"`
	TargetID    int64             `json:"target_id"`
	TargetTitle map[string]string `json:"target_titles"`
	Mode        string            `json:"mode"`
	Recursive   bool              `json:"recursive"`
	Frequency   string            `json:"frequency"`
	Mandatory   bool              `json:"mandatory"`
	Note        string            `json:"note"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	Members     int               `json:"members"`
	// for the profile: whether the caller unsubscribed (optional assignments)
	Unsubscribed bool `json:"unsubscribed"`
}

// AssignmentInput creates or changes an assignment.
type AssignmentInput struct {
	SubjectType string `json:"subject_type"`
	SubjectID   int64  `json:"subject_id"`
	TargetType  string `json:"target_type"`
	TargetID    int64  `json:"target_id"`
	Mode        string `json:"mode"`
	Recursive   *bool  `json:"recursive"`
	Frequency   string `json:"frequency"`
	Mandatory   bool   `json:"mandatory"`
	Note        string `json:"note"`
}

const assignCols = `a.id, a.subject_type, a.subject_id,
	COALESCE(CASE a.subject_type WHEN 'user' THEN (SELECT COALESCE(NULLIF(u.display_name, ''), u.username) FROM users u WHERE u.id=a.subject_id)
		WHEN 'group' THEN (SELECT g.name FROM groups g WHERE g.id=a.subject_id) ELSE (SELECT r.name FROM roles r WHERE r.id=a.subject_id) END, '#' || a.subject_id),
	a.target_type, a.target_id,
	COALESCE(CASE a.target_type WHEN 'category' THEN (SELECT c.titles FROM categories c WHERE c.id=a.target_id) ELSE (SELECT e.titles FROM elements e WHERE e.id=a.target_id) END, '{}'::jsonb),
	a.mode, a.recursive, a.frequency, a.mandatory, a.note, COALESCE((SELECT u.display_name FROM users u WHERE u.id=a.created_by), ''), a.created_at,
	(SELECT count(DISTINCT w.user_id) FROM watch_effective w WHERE w.assignment_id=a.id)`

func scanAssignment(r pgx.CollectableRow) (Assignment, error) {
	var a Assignment
	err := r.Scan(&a.ID, &a.SubjectType, &a.SubjectID, &a.SubjectName, &a.TargetType, &a.TargetID, &a.TargetTitle, &a.Mode, &a.Recursive,
		&a.Frequency, &a.Mandatory, &a.Note, &a.CreatedBy, &a.CreatedAt, &a.Members)
	return a, err
}

// Assignments lists all assignments (core.watches.assign).
func (s *Service) Assignments(ctx context.Context) ([]Assignment, error) {
	if err := perm.Require(ctx, "core.watches.assign"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+assignCols+` FROM watch_assignments a ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAssignment)
}

func (in *AssignmentInput) validate() error {
	switch in.SubjectType {
	case "user", "group", "role":
	default:
		return apperr.BadRequest("subject_type must be user, group or role")
	}
	if in.TargetType != "category" && in.TargetType != "element" {
		return apperr.BadRequest("target_type must be category or element")
	}
	if in.Mode == "" {
		in.Mode = "link"
	}
	if in.Mode != "link" && in.Mode != "content" {
		return apperr.BadRequest("mode must be link or content")
	}
	if in.Frequency == "" {
		in.Frequency = "instant"
	}
	if in.Frequency != "instant" && in.Frequency != "daily" && in.Frequency != "weekly" {
		return apperr.BadRequest("frequency must be instant, daily or weekly")
	}
	return nil
}

// SaveAssignment creates (id 0) or changes an assignment.
func (s *Service) SaveAssignment(ctx context.Context, id int64, in AssignmentInput) (*Assignment, error) {
	if err := perm.Require(ctx, "core.watches.assign"); err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	table := map[string]string{"user": "users", "group": "groups", "role": "roles"}[in.SubjectType]
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, in.SubjectID).Scan(&ok); err != nil || !ok {
		return nil, apperr.BadRequest("%s %d does not exist", in.SubjectType, in.SubjectID)
	}
	ttable := map[string]string{"category": "categories", "element": "elements"}[in.TargetType]
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+ttable+` WHERE id=$1 AND deleted_at IS NULL)`, in.TargetID).Scan(&ok); err != nil || !ok {
		return nil, apperr.BadRequest("%s %d does not exist", in.TargetType, in.TargetID)
	}
	recursive := true
	if in.Recursive != nil {
		recursive = *in.Recursive
	}
	uid, _ := me(ctx)
	var err error
	if id == 0 {
		err = s.pool.QueryRow(ctx, `INSERT INTO watch_assignments(subject_type, subject_id, target_type, target_id, mode, recursive, frequency, mandatory, note, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, in.SubjectType, in.SubjectID, in.TargetType, in.TargetID, in.Mode, recursive, in.Frequency, in.Mandatory, in.Note, nullID(uid)).Scan(&id)
		if err != nil && isUnique(err) {
			return nil, apperr.Conflict("this watch is already assigned")
		}
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = s.pool.Exec(ctx, `UPDATE watch_assignments SET subject_type=$2, subject_id=$3, target_type=$4, target_id=$5, mode=$6, recursive=$7, frequency=$8, mandatory=$9, note=$10 WHERE id=$1`,
			id, in.SubjectType, in.SubjectID, in.TargetType, in.TargetID, in.Mode, recursive, in.Frequency, in.Mandatory, in.Note)
		if err == nil && tag.RowsAffected() == 0 {
			return nil, apperr.NotFound("assignment")
		}
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "watch.assign", "watch_assignment", id, nil, in)
	rows, err := s.pool.Query(ctx, `SELECT `+assignCols+` FROM watch_assignments a WHERE a.id=$1`, id)
	if err != nil {
		return nil, err
	}
	a, err := pgx.CollectExactlyOneRow(rows, scanAssignment)
	return &a, err
}

// DeleteAssignment removes an assignment.
func (s *Service) DeleteAssignment(ctx context.Context, id int64) error {
	if err := perm.Require(ctx, "core.watches.assign"); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM watch_assignments WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("assignment")
	}
	return audit.Log(ctx, s.pool, "watch.unassign", "watch_assignment", id, nil, nil)
}

// MyAssignments lists the assignments that apply to the caller (profile).
func (s *Service) MyAssignments(ctx context.Context) ([]Assignment, error) {
	uid, err := me(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+assignCols+`, EXISTS(SELECT 1 FROM watch_optouts o WHERE o.assignment_id=a.id AND o.user_id=$1)
		FROM watch_assignments a WHERE (a.subject_type='user' AND a.subject_id=$1)
			OR (a.subject_type='group' AND a.subject_id IN (SELECT group_id FROM user_groups WHERE user_id=$1))
			OR (a.subject_type='role' AND a.subject_id IN (SELECT role_id FROM user_roles WHERE user_id=$1
				UNION SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=$1))
		ORDER BY a.created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Assignment, error) {
		var a Assignment
		err := r.Scan(&a.ID, &a.SubjectType, &a.SubjectID, &a.SubjectName, &a.TargetType, &a.TargetID, &a.TargetTitle, &a.Mode, &a.Recursive,
			&a.Frequency, &a.Mandatory, &a.Note, &a.CreatedBy, &a.CreatedAt, &a.Members, &a.Unsubscribed)
		return a, err
	})
}

// SetSubscribed unsubscribes from (or subscribes again to) an optional
// assignment of the caller.
func (s *Service) SetSubscribed(ctx context.Context, id int64, subscribed bool) error {
	uid, err := me(ctx)
	if err != nil {
		return err
	}
	list, err := s.MyAssignments(ctx)
	if err != nil {
		return err
	}
	for _, a := range list {
		if a.ID != id {
			continue
		}
		if a.Mandatory {
			return apperr.Forbidden("this watch is mandatory")
		}
		if subscribed {
			_, err = s.pool.Exec(ctx, `DELETE FROM watch_optouts WHERE assignment_id=$1 AND user_id=$2`, id, uid)
		} else {
			_, err = s.pool.Exec(ctx, `INSERT INTO watch_optouts(assignment_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, uid)
		}
		if err == nil {
			_ = audit.Log(ctx, s.pool, "watch.subscribe", "watch_assignment", id, nil, map[string]bool{"subscribed": subscribed})
		}
		return err
	}
	return apperr.NotFound("assignment")
}

func isUnique(err error) bool { return db.IsUniqueViolation(err) }
