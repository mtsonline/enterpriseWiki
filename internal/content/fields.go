package content

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// ElementField is data of a plugin that is edited and saved together with
// an element of any type (e.g. properties): the editors send it as
// fields[<id>], drafts keep it, and it is stored in the transaction of the
// element, so the element and its fields change together or not at all.
type ElementField interface {
	ID() string
	// Check validates a value for an element (elementID 0: a new element
	// of type typ in category) and returns the value to store. It runs
	// with the rights of the caller; nil means: nothing to store.
	Check(ctx context.Context, svc *Service, elementID, categoryID int64, typ string, value json.RawMessage) (json.RawMessage, error)
	// Store saves a checked value in the transaction of the element.
	Store(ctx context.Context, tx pgx.Tx, elementID int64, value json.RawMessage) error
}

// ElementFieldAlways is an ElementField that is also checked (with a nil
// value) when a save does not send it, e.g. to set defaults and computed
// values of elements created by the API or other plugins.
type ElementFieldAlways interface {
	Always() bool
}

type elementField struct {
	plugin string
	f      ElementField
}

// RegisterElementField adds a field of a plugin.
func (s *Service) RegisterElementField(pluginID string, f ElementField) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fields == nil {
		s.fields = map[string]elementField{}
	}
	s.fields[f.ID()] = elementField{pluginID, f}
}

// MaxFieldSize is the largest value of one field.
const MaxFieldSize = 256 << 10

// checkFields validates the fields of a save; unknown or disabled fields
// are refused (a typo must not silently drop data).
func (s *Service) checkFields(ctx context.Context, elementID, categoryID int64, typ string, in map[string]json.RawMessage, always bool) (map[string]json.RawMessage, error) {
	s.mu.Lock()
	reg := s.fields
	s.mu.Unlock()
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	if always {
		for k, f := range reg {
			if a, ok := f.f.(ElementFieldAlways); ok && a.Always() && (s.enabled == nil || s.enabled(f.plugin)) {
				if _, sent := in[k]; !sent {
					keys = append(keys, k)
				}
			}
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	slices.Sort(keys)
	out := map[string]json.RawMessage{}
	for _, k := range keys {
		v := in[k]
		f, ok := reg[k]
		if !ok || (s.enabled != nil && !s.enabled(f.plugin)) {
			return nil, apperr.BadRequest("unknown field %q", k)
		}
		if len(v) > MaxFieldSize {
			return nil, apperr.BadRequest("field %q is too large", k)
		}
		if (len(v) == 0 || string(v) == "null") && !always {
			continue // unchanged
		}
		c, err := f.f.Check(ctx, s, elementID, categoryID, typ, v)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out[k] = c
		}
	}
	return out, nil
}

// draftFields are the sent fields that passed the check: drafts keep what
// people entered (checked again on publishing).
func draftFields(in, checked map[string]json.RawMessage) []byte {
	out := map[string]json.RawMessage{}
	for k, v := range in {
		if _, ok := checked[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// storeFields saves checked fields in the transaction of the element.
func (s *Service) storeFields(ctx context.Context, tx pgx.Tx, elementID int64, checked map[string]json.RawMessage) error {
	if len(checked) == 0 {
		return nil
	}
	s.mu.Lock()
	reg := s.fields
	s.mu.Unlock()
	keys := make([]string, 0, len(checked))
	for k := range checked {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := reg[k].f.Store(ctx, tx, elementID, checked[k]); err != nil {
			return err
		}
	}
	return nil
}
