package auth

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// PermPeopleView lets signed-in people open other people (person page,
// menu on names) – only the people they may find (core.users.list_all, or
// the members of the own groups with core.users.list_own).
const PermPeopleView = "core.people.view"

// PermPeopleEmail shows e-mail addresses when people.email_visibility is
// "permission".
const PermPeopleEmail = "core.people.email"

// PeopleMax is the most people Viewable checks at once.
const PeopleMax = 1000

// Person is what the caller sees of a person: the business card and, if
// allowed, the primary confirmed e-mail address.
type Person struct {
	ID          int64             `json:"id"`
	DisplayName string            `json:"display_name"`
	Handle      string            `json:"handle"`
	Card        map[string]string `json:"card"`            // job_title, department, phone, mobile, office, about, photo
	Email       string            `json:"email,omitempty"` // only when the caller may see it
	Self        bool              `json:"self"`
}

// cardFields are the fields of the business card shown on a person page.
var cardFields = []string{"job_title", "department", "phone", "mobile", "office", "about"}

// mayOpenPeople reports whether p may open people at all (signed in, with
// the permission and a list scope for users).
func mayOpenPeople(p *identity.Principal) bool {
	return p != nil && p.ID() > 0 && perm.HasP(p, PermPeopleView) && ListScope(p, "users") != ListNone
}

// Viewable returns those of the users ids the caller may open: active
// people the caller may find (themselves, everybody with
// core.users.list_all, the members of shared groups with
// core.users.list_own), only with core.people.view. Guests: none. One
// query for all ids.
func (s *Service) Viewable(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	p := identity.From(ctx)
	if len(ids) == 0 || !mayOpenPeople(p) {
		return out, nil
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > PeopleMax {
		ids = ids[:PeopleMax]
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id FROM users u
		WHERE u.id = ANY($1) AND NOT u.disabled AND (u.id = $2 OR $3
		  OR EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = u.id AND b.user_id = $2))`,
		ids, p.ID(), ListScope(p, "users") == ListAll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ViewableID returns id if the caller may open the person, else nil (for
// the *_id fields next to names in responses).
func ViewableID(ok map[int64]bool, id *int64) *int64 {
	if id == nil || !ok[*id] {
		return nil
	}
	v := *id
	return &v
}

// EmailPolicy is the rule for e-mail addresses of people (for the
// profile form).
type EmailPolicy struct {
	Visibility  string `json:"visibility"` // users | permission | nobody
	HideAllowed bool   `json:"hide_allowed"`
}

// EmailPolicy returns the configured rule.
func (s *Service) EmailPolicy() EmailPolicy {
	c := s.cfg.Get().People
	return EmailPolicy{Visibility: c.Email(), HideAllowed: c.HideAllowed()}
}

// EmailVisible reports whether the caller may see the e-mail address of
// person uid, who hid it (hidden) or not. The person always sees their own
// address; guests never. A hidden address stays hidden for everybody else
// (also administrators) as long as hiding is allowed.
func (s *Service) EmailVisible(ctx context.Context, uid int64, hidden bool) bool {
	p := identity.From(ctx)
	if p == nil || p.ID() <= 0 {
		return false
	}
	if p.ID() == uid {
		return true
	}
	pol := s.EmailPolicy()
	if hidden && pol.HideAllowed {
		return false
	}
	switch pol.Visibility {
	case "users":
		return true
	case "permission":
		return perm.HasP(p, PermPeopleEmail)
	}
	return false
}

// Person returns what the caller may see of person id. Guests get 401,
// people without core.people.view or a list scope 403; a person the caller
// may not find answers like one that does not exist (404), so nothing is
// told about them. Each call counts against core.people.views_per_hour
// (not for administrators): no faster way to collect cards and addresses
// than one person at a time.
func (s *Service) Person(ctx context.Context, id int64) (*Person, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() <= 0 {
		return nil, apperr.ErrUnauthorized
	}
	if !mayOpenPeople(p) {
		return nil, apperr.Forbidden("opening people needs the permission %s and core.users.list_all or core.users.list_own", PermPeopleView)
	}
	if !p.Admin() {
		max := int(limits.Value(s.cfg.Get(), limits.PeoplePerHour))
		if !s.limiter.allow("people:"+itoa(p.ID()), max, time.Hour) {
			return nil, apperr.TooMany("at most %d people per hour", max)
		}
	}
	ok, err := s.Viewable(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	if !ok[id] {
		return nil, apperr.NotFound("person")
	}
	var x Person
	var profile map[string]string
	var email *string
	err = s.pool.QueryRow(ctx, `SELECT u.id, u.display_name, u.username || '@' || u.provider, u.profile,
		(SELECT e.email FROM user_emails e WHERE e.user_id = u.id AND e.is_primary AND e.verified)
		FROM users u WHERE u.id = $1`, id).Scan(&x.ID, &x.DisplayName, &x.Handle, &profile, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("person")
	}
	if err != nil {
		return nil, err
	}
	x.Self = p.ID() == id
	x.Card = map[string]string{}
	for _, f := range cardFields {
		if v := profile[f]; v != "" {
			x.Card[f] = v
		}
	}
	if v := profile["photo"]; v != "" && s.PhotoVisible(ctx, id, profile["photo_visibility"]) {
		x.Card["photo"] = v
	}
	if email != nil && s.EmailVisible(ctx, id, profile["email_hidden"] == "true") {
		x.Email = *email
	}
	return &x, nil
}
