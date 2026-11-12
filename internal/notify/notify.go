// Package notify stores notifications in the wiki (the bell in the header)
// and sends them by e-mail unless the recipient switched that off for the
// kind of notification in the profile (default: on). Plugins use it through
// sdk.Host.Notify (e.g. mentions, sharing).
package notify

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// Notification is one message for a user.
type Notification struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"` // e.g. mention, share (plugins: <plugin id>.<kind>)
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Link       string     `json:"link"` // path in the wiki, e.g. /e/12
	Actor      string     `json:"actor"`
	EntityType string     `json:"entity_type"`
	EntityID   *int64     `json:"entity_id"`
	CreatedAt  time.Time  `json:"created_at"`
	ReadAt     *time.Time `json:"read_at"`
}

// Message is what is sent; Title and Body are built per recipient (language).
type Message struct {
	Kind       string
	EntityType string
	EntityID   int64
	Link       string
	// Text returns title and body in the language of the recipient.
	Text func(lang string) (title, body string)
}

// RegisterKind declares a kind of notification; users can switch its
// e-mails on and off in the profile (UI string notify.kind.<kind>, or the
// key labelKey of the plugin).
func RegisterKind(kind, labelKey string) { registerKind(kind, labelKey, true) }

// RegisterKindOff declares a kind whose e-mails are off until a user
// switches them on (notifications in the wiki are always shown).
func RegisterKindOff(kind, labelKey string) { registerKind(kind, labelKey, false) }

func registerKind(kind, labelKey string, on bool) {
	mail.KindsOffByDefault[kind] = !on
	if !slices.Contains(mail.NotificationKinds, kind) {
		mail.NotificationKinds = append(mail.NotificationKinds, kind)
	}
	mail.KindLabels[kind] = labelKey
}

// Service stores and sends notifications.
type Service struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	mail *mail.Service
}

// New creates the service.
func New(pool *pgxpool.Pool, cfg *config.Store, m *mail.Service) *Service {
	return &Service{pool: pool, cfg: cfg, mail: m}
}

// Send notifies users (the actor is never notified). It returns the number
// of recipients. All of them or none: the notifications and their e-mails
// are written in one transaction.
func (s *Service) Send(ctx context.Context, userIDs []int64, m Message) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		n, err = s.SendQ(ctx, tx, userIDs, m)
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// SendQ is Send inside the transaction q of the caller: the notification
// is stored together with what it is about, or not at all. After an error
// the transaction cannot be committed.
func (s *Service) SendQ(ctx context.Context, q db.Querier, userIDs []int64, m Message) (int, error) {
	actor := identity.From(ctx).ID()
	seen := map[int64]bool{}
	var ids []int64
	for _, id := range userIDs {
		if id > 0 && id != actor && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	us, err := s.mail.Users(ctx, q, ids)
	if err != nil {
		return 0, err
	}
	base := strings.TrimSuffix(s.cfg.Get().Server.BaseURL, "/")
	var eid *int64
	if m.EntityID != 0 {
		eid = &m.EntityID
	}
	var actorID *int64
	if actor > 0 {
		actorID = &actor
	}
	n := 0
	for _, u := range us {
		title, body := m.Text(u.Lang)
		if _, err := q.Exec(ctx, `INSERT INTO notifications(user_id, kind, title, body, link, actor_id, entity_type, entity_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			u.ID, m.Kind, title, body, m.Link, actorID, m.EntityType, eid); err != nil {
			return 0, err
		}
		n++
		if !u.Wants(m.Kind) || !s.mail.Enabled() {
			continue
		}
		text := body
		if m.Link != "" {
			text += "\n\n" + base + m.Link + "\n"
		}
		if _, err := s.mail.Enqueue(ctx, q, mail.Message{To: []string{u.Email}, Subject: title, Text: text, Source: "notify", Ref: fmt.Sprintf("%s:%s:%d", m.Kind, m.EntityType, m.EntityID), Owner: -1}); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// List returns the latest notifications of the caller and the number of unread ones.
func (s *Service) List(ctx context.Context, limit int) ([]Notification, int, error) {
	uid := identity.From(ctx).ID()
	if uid <= 0 {
		return nil, 0, apperr.ErrUnauthorized
	}
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id, n.kind, n.title, n.body, n.link, COALESCE(u.display_name, ''), n.entity_type, n.entity_id, n.created_at, n.read_at
		FROM notifications n LEFT JOIN users u ON u.id = n.actor_id WHERE n.user_id=$1 ORDER BY n.created_at DESC, n.id DESC LIMIT $2`, uid, limit)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, r.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Actor, &n.EntityType, &n.EntityID, &n.CreatedAt, &n.ReadAt)
	})
	if err != nil {
		return nil, 0, err
	}
	var unread int
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, uid).Scan(&unread)
	return list, unread, err
}

// MarkRead marks notifications (nil: all) of the caller as read.
func (s *Service) MarkRead(ctx context.Context, ids []int64) error {
	uid := identity.From(ctx).ID()
	if uid <= 0 {
		return apperr.ErrUnauthorized
	}
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL AND ($2::bigint[] IS NULL OR id = ANY($2))`, uid, ids)
	return err
}

// Delete removes a notification of the caller.
func (s *Service) Delete(ctx context.Context, id int64) error {
	uid := identity.From(ctx).ID()
	if uid <= 0 {
		return apperr.ErrUnauthorized
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM notifications WHERE id=$1 AND user_id=$2`, id, uid)
	return err
}

// Cleanup removes read notifications older than days.
func (s *Service) Cleanup(ctx context.Context, days int) error {
	if days <= 0 {
		days = 90
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM notifications WHERE created_at < now() - make_interval(days => $1) AND read_at IS NOT NULL`, days)
	return err
}
