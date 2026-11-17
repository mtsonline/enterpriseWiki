package content

import (
	"context"
	"slices"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Content on behalf of an instance ("via"): a blog, a FAQ … lets people with
// its instance permissions create and change content in its own categories,
// without page permissions there. The core checks everything; plugins only
// say where their content lives (InstanceScoper).

// InstanceScope is where the content of an instance lives.
type InstanceScope struct {
	// Categories that new content may go into (e.g. the category of a blog
	// and its subcategories).
	Categories []int64
	// Types of elements created on behalf of the instance (e.g. core.html).
	Types []string
}

// InstanceScoper is implemented by element types with instance permissions
// whose instances hold content (blog, FAQ …).
type InstanceScoper interface {
	InstanceScope(ctx context.Context, s *Service, instanceID int64) (*InstanceScope, error)
}

// instanceKey returns the instance permission of typ whose key ends in
// "."+name (manage, edit, contribute); "" if the type has none.
func instanceKey(typ, name string) string {
	for _, k := range perm.InstanceKeys(typ) {
		if strings.HasSuffix(k, "."+name) {
			return k
		}
	}
	return ""
}

// isInstanceType reports whether elements of typ have instance permissions.
func isInstanceType(typ string) bool { return len(perm.InstanceKeys(typ)) > 0 }

// scopeOf returns the scope of instance inst (nil if the type has none).
func (s *Service) scopeOf(ctx context.Context, inst acl.Instance) (*InstanceScope, error) {
	sc, ok := s.Type(inst.Type).(InstanceScoper)
	if !ok {
		return nil, nil
	}
	// the scope is resolved with the rights of the system: it is the
	// configuration of the instance, checked against the caller below
	return sc.InstanceScope(ctx, s, inst.ID)
}

// requireVia checks that the caller may create an element of type typ in
// categoryID on behalf of instance via, and returns the instance.
func (s *Service) requireVia(ctx context.Context, via, categoryID int64, typ string) (acl.Instance, error) {
	p := identity.From(ctx)
	if p == nil {
		return acl.Instance{}, apperr.ErrUnauthorized
	}
	inst, err := s.Instance(ctx, via)
	if err != nil {
		return acl.Instance{}, err
	}
	scope, err := s.scopeOf(ctx, inst)
	if err != nil {
		return acl.Instance{}, err
	}
	if scope == nil {
		return acl.Instance{}, apperr.BadRequest("elements of type %q hold no content of their own (via)", inst.Type)
	}
	if !slices.Contains(scope.Categories, categoryID) {
		return acl.Instance{}, apperr.BadRequest("the category is not part of this %s", inst.Type)
	}
	if !slices.Contains(scope.Types, typ) {
		return acl.Instance{}, apperr.BadRequest("this %s does not hold elements of type %q", inst.Type, typ)
	}
	// the target: the caller sees it, it is not archived
	if err := s.require(ctx, categoryID, acl.View); err != nil {
		return acl.Instance{}, err
	}
	if s.acl.Archived(ctx, categoryID) {
		return acl.Instance{}, apperr.BadRequest("the category is archived (read-only)")
	}
	if key := instanceKey(inst.Type, "contribute"); key == "" || !s.acl.CanInstance(ctx, p, inst, key) {
		return acl.Instance{}, apperr.Forbidden("writing here needs the permission %q of this %s", instanceKey(inst.Type, "contribute"), inst.Type)
	}
	return inst, nil
}

// instancePerms adds what instance permissions allow on element e that was
// created through an instance: with "edit" changing and deleting it, with
// "contribute" only for the own elements. Only while the element is still
// in the scope of the instance, never in archived categories and never
// without seeing it.
func (s *Service) instancePerms(ctx context.Context, e *Element, cat acl.Perm) acl.Perm {
	if e.OwnerInstance == nil || cat&acl.View == 0 {
		return acl.None
	}
	p := identity.From(ctx)
	if p == nil || p.UserID <= 0 || s.acl.Archived(ctx, e.CategoryID) {
		return acl.None
	}
	inst, err := s.Instance(ctx, *e.OwnerInstance)
	if err != nil {
		return acl.None
	}
	scope, err := s.scopeOf(ctx, inst)
	if err != nil || scope == nil || !slices.Contains(scope.Categories, e.CategoryID) || !slices.Contains(scope.Types, e.Type) {
		return acl.None
	}
	acts, err := s.acl.InstanceActions(ctx, p, inst)
	if err != nil {
		return acl.None
	}
	if slices.Contains(acts, instanceKey(inst.Type, "edit")) ||
		(slices.Contains(acts, instanceKey(inst.Type, "contribute")) && e.CreatedByID != nil && *e.CreatedByID == p.UserID) {
		return acl.Edit | acl.Delete
	}
	return acl.None
}

// elementPerms are the caller's permissions on element e: those of its
// category, plus those of the instance it was created through. For an
// instance itself (a blog), changing it needs the manage permission of the
// instance – editing pages in the category is not enough.
func (s *Service) elementPerms(ctx context.Context, e *Element) acl.Perm {
	p := identity.From(ctx)
	cat, err := s.acl.Perms(ctx, p, e.CategoryID)
	if err != nil {
		return acl.None
	}
	out := cat | s.instancePerms(ctx, e, cat)
	if isInstanceType(e.Type) {
		out &^= acl.Edit
		if key := instanceKey(e.Type, "manage"); key != "" && cat&acl.View != 0 &&
			s.acl.CanInstance(ctx, p, acl.Instance{ID: e.ID, Type: e.Type, CategoryID: e.CategoryID}, key) {
			out |= acl.Edit
		}
	}
	return out
}

// requireElement checks want on element e (see elementPerms): 401 for
// guests, 404 without seeing it, else 403.
func (s *Service) requireElement(ctx context.Context, e *Element, want acl.Perm) error {
	got := s.elementPerms(ctx, e)
	if got&want == want {
		return nil
	}
	if identity.From(ctx) == nil {
		return apperr.ErrUnauthorized
	}
	if got&acl.View == 0 {
		return apperr.NotFound("element")
	}
	if isInstanceType(e.Type) && want&acl.Edit != 0 {
		return apperr.Forbidden("changing this %s needs the permission %q", e.Type, instanceKey(e.Type, "manage"))
	}
	return apperr.ErrForbidden
}

// requireMove checks moving element e to categoryID: Delete on the source
// and Create on the target, or – for an element created through an
// instance – its instance permissions, as long as both categories belong
// to the instance.
func (s *Service) requireMove(ctx context.Context, e *Element, categoryID int64) error {
	if e.OwnerInstance != nil {
		if s.elementPerms(ctx, e)&acl.Delete != 0 {
			if inst, err := s.Instance(ctx, *e.OwnerInstance); err == nil {
				if scope, err := s.scopeOf(ctx, inst); err == nil && scope != nil && slices.Contains(scope.Categories, categoryID) {
					if err := s.require(ctx, categoryID, acl.View); err != nil {
						return err
					}
					if s.acl.Archived(ctx, categoryID) {
						return apperr.BadRequest("the category is archived (read-only)")
					}
					return nil
				}
			}
		}
	}
	if err := s.requireElement(ctx, e, acl.Delete); err != nil {
		return err
	}
	return s.require(ctx, categoryID, acl.Create)
}

// CanEditElement returns nil if the caller may change element id (page
// permissions of its category or permissions of the instance it belongs
// to), e.g. for live editing.
func (s *Service) CanEditElement(ctx context.Context, id int64) error {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		if identity.From(ctx) == nil {
			return apperr.ErrUnauthorized
		}
		return err
	}
	return s.requireElement(ctx, e, acl.Edit)
}

// requireViaCategory checks that the caller may create a section below
// parentID on behalf of instance via: the manage permission of the
// instance, the parent belongs to it, is visible and not archived.
func (s *Service) requireViaCategory(ctx context.Context, via, parentID int64) (acl.Instance, error) {
	p := identity.From(ctx)
	if p == nil {
		return acl.Instance{}, apperr.ErrUnauthorized
	}
	inst, err := s.Instance(ctx, via)
	if err != nil {
		return acl.Instance{}, err
	}
	scope, err := s.scopeOf(ctx, inst)
	if err != nil {
		return acl.Instance{}, err
	}
	if scope == nil || !slices.Contains(scope.Categories, parentID) {
		return acl.Instance{}, apperr.BadRequest("the category is not part of this %s", inst.Type)
	}
	if err := s.require(ctx, parentID, acl.View); err != nil {
		return acl.Instance{}, err
	}
	if err := s.notArchived(ctx, parentID); err != nil {
		return acl.Instance{}, err
	}
	if key := instanceKey(inst.Type, "manage"); key == "" || !s.acl.CanInstance(ctx, p, inst, key) {
		return acl.Instance{}, apperr.Forbidden("adding sections needs the permission %q of this %s", instanceKey(inst.Type, "manage"), inst.Type)
	}
	return inst, nil
}
