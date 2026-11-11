package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// SCIM 2.0 service provider (RFC 7643, RFC 7644) at /scim/v2: Users and
// Groups with filter (attribute eq "value"), paging, PUT and PATCH. The
// bearer token of a SCIM client authenticates; browser sessions and API
// keys never do. Data rules: see internal/auth/scim.go.

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimMaxResults  = 200
	scimContentType = "application/scim+json"
)

// scimInbound reports whether path belongs to the SCIM service provider.
func scimInbound(path string) bool { return path == "/scim/v2" || strings.HasPrefix(path, "/scim/v2/") }

type scimName struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Formatted  string `json:"formatted,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type scimRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created"`
	LastModified string `json:"lastModified"`
	Location     string `json:"location"`
}

type scimUserDoc struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id,omitempty"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        *scimName   `json:"name,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []scimEmail `json:"emails,omitempty"`
	Active      *bool       `json:"active,omitempty"`
	Groups      []scimRef   `json:"groups,omitempty"`
	Meta        *scimMeta   `json:"meta,omitempty"`
}

type scimGroupDoc struct {
	Schemas     []string  `json:"schemas"`
	ID          string    `json:"id,omitempty"`
	ExternalID  string    `json:"externalId,omitempty"`
	DisplayName string    `json:"displayName"`
	Members     []scimRef `json:"members"`
	Meta        *scimMeta `json:"meta,omitempty"`
}

type scimPatch struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"Operations"`
}

type scimError struct {
	Status   int
	ScimType string
	Detail   string
}

func (e *scimError) Error() string { return e.Detail }

func scimBad(scimType, format string, a ...any) error {
	return &scimError{Status: 400, ScimType: scimType, Detail: fmt.Sprintf(format, a...)}
}

func (s *Server) routesSCIM() {
	for _, r := range []string{
		"GET /scim/v2/ServiceProviderConfig", "GET /scim/v2/ResourceTypes", "GET /scim/v2/Schemas",
		"GET /scim/v2/Users", "POST /scim/v2/Users", "GET /scim/v2/Users/{id}", "PUT /scim/v2/Users/{id}", "PATCH /scim/v2/Users/{id}", "DELETE /scim/v2/Users/{id}",
		"GET /scim/v2/Groups", "POST /scim/v2/Groups", "GET /scim/v2/Groups/{id}", "PUT /scim/v2/Groups/{id}", "PATCH /scim/v2/Groups/{id}", "DELETE /scim/v2/Groups/{id}",
	} {
		h.RecordRoute(r)
	}
	s.app.Mux.HandleFunc("/scim/v2/", s.serveSCIM)
}

func (s *Server) serveSCIM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
		scimWriteError(w, &scimError{Status: 401, Detail: "a SCIM bearer token is required"})
		return
	}
	c, ctx, err := s.app.Auth.ScimAuthenticate(r.Context(), strings.TrimSpace(tok), s.clientIP(r))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="scim", error="invalid_token"`)
		scimWriteError(w, err)
		return
	}
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	base := strings.TrimRight(s.app.Config.Get().Server.BaseURL, "/") + "/scim/v2"
	res, status, err := s.scimDispatch(ctx, c, r, base)
	if err != nil {
		scimWriteError(w, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", scimContentType)
	if status == http.StatusCreated {
		if loc := scimLocation(res); loc != "" {
			w.Header().Set("Location", loc)
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}

func scimLocation(v any) string {
	switch d := v.(type) {
	case *scimUserDoc:
		return d.Meta.Location
	case *scimGroupDoc:
		return d.Meta.Location
	}
	return ""
}

func (s *Server) scimDispatch(ctx context.Context, c *auth.ScimClient, r *http.Request, base string) (any, int, error) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/scim/v2"), "/")
	kind, idStr, _ := strings.Cut(rest, "/")
	if strings.Contains(idStr, "/") {
		return nil, 0, &scimError{Status: 404, Detail: "not found"}
	}
	var id int64
	if idStr != "" {
		var err error
		if id, err = strconv.ParseInt(idStr, 10, 64); err != nil || id <= 0 {
			return nil, 0, &scimError{Status: 404, Detail: "resource not found"}
		}
	}
	m := r.Method
	switch {
	case kind == "ServiceProviderConfig" && m == http.MethodGet && id == 0:
		return scimProviderConfig(base), 200, nil
	case kind == "ResourceTypes" && m == http.MethodGet && id == 0:
		return scimResourceTypes(base), 200, nil
	case kind == "Schemas" && m == http.MethodGet && id == 0:
		return scimSchemas(), 200, nil
	case kind == "Users":
		return s.scimUsers(ctx, c, r, base, id)
	case kind == "Groups":
		return s.scimGroups(ctx, c, r, base, id)
	}
	return nil, 0, &scimError{Status: 404, Detail: "not found"}
}

// ---------------------------------------------------------------- users

func (s *Server) scimUsers(ctx context.Context, c *auth.ScimClient, r *http.Request, base string, id int64) (any, int, error) {
	a := s.app.Auth
	switch {
	case r.Method == http.MethodGet && id == 0:
		f, err := scimParseFilter(r.URL.Query().Get("filter"))
		if err != nil {
			return nil, 0, err
		}
		start, count := scimPaging(r)
		list, total, err := a.ScimUsers(ctx, c, f, start, count)
		if err != nil {
			return nil, 0, err
		}
		docs := make([]any, len(list))
		for i := range list {
			docs[i] = scimUserOut(&list[i], base)
		}
		return scimList(docs, total, start), 200, nil
	case r.Method == http.MethodGet:
		u, err := a.ScimUser(ctx, c, id)
		if err != nil {
			return nil, 0, err
		}
		return scimUserOut(u, base), 200, nil
	case r.Method == http.MethodPost && id == 0:
		in, err := scimDecodeUser(r)
		if err != nil {
			return nil, 0, err
		}
		u, err := a.ScimCreateUser(ctx, c, in)
		if err != nil {
			return nil, 0, err
		}
		return scimUserOut(u, base), 201, nil
	case r.Method == http.MethodPut && id > 0:
		in, err := scimDecodeUser(r)
		if err != nil {
			return nil, 0, err
		}
		u, err := a.ScimReplaceUser(ctx, c, id, in)
		if err != nil {
			return nil, 0, err
		}
		return scimUserOut(u, base), 200, nil
	case r.Method == http.MethodPatch && id > 0:
		p, err := scimDecodePatch(r)
		if err != nil {
			return nil, 0, err
		}
		cur, err := a.ScimUser(ctx, c, id)
		if err != nil {
			return nil, 0, err
		}
		if err := scimPatchUser(cur, p); err != nil {
			return nil, 0, err
		}
		u, err := a.ScimReplaceUser(ctx, c, id, *cur)
		if err != nil {
			return nil, 0, err
		}
		return scimUserOut(u, base), 200, nil
	case r.Method == http.MethodDelete && id > 0:
		return nil, 204, a.ScimDeleteUser(ctx, c, id)
	}
	return nil, 0, &scimError{Status: 405, Detail: "method not allowed"}
}

func scimTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func scimUserOut(u *auth.ScimUser, base string) *scimUserDoc {
	id := strconv.FormatInt(u.ID, 10)
	d := &scimUserDoc{Schemas: []string{scimUserSchema}, ID: id, ExternalID: u.ExternalID, UserName: u.UserName,
		DisplayName: u.DisplayName, Active: &u.Active,
		Meta: &scimMeta{ResourceType: "User", Created: scimTime(u.Created), LastModified: scimTime(u.Modified), Location: base + "/Users/" + id}}
	if u.GivenName != "" || u.FamilyName != "" {
		d.Name = &scimName{GivenName: u.GivenName, FamilyName: u.FamilyName, Formatted: strings.TrimSpace(u.GivenName + " " + u.FamilyName)}
	}
	for _, e := range u.Emails {
		d.Emails = append(d.Emails, scimEmail{Value: e.Value, Type: "work", Primary: e.Primary})
	}
	for _, g := range u.Groups {
		d.Groups = append(d.Groups, scimRef{Value: g.Value, Display: g.Display, Ref: base + "/Groups/" + g.Value})
	}
	return d
}

func scimDecodeUser(r *http.Request) (auth.ScimUser, error) {
	var d scimUserDoc
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		return auth.ScimUser{}, scimBad("invalidSyntax", "invalid JSON: %v", err)
	}
	u := auth.ScimUser{UserName: d.UserName, ExternalID: d.ExternalID, DisplayName: d.DisplayName, Active: d.Active == nil || *d.Active}
	if d.Name != nil {
		u.GivenName, u.FamilyName = d.Name.GivenName, d.Name.FamilyName
		if u.DisplayName == "" {
			u.DisplayName = d.Name.Formatted
		}
	}
	for _, e := range d.Emails {
		u.Emails = append(u.Emails, auth.ScimEmail{Value: e.Value, Primary: e.Primary})
	}
	if u.UserName == "" {
		return u, scimBad("invalidValue", "userName is required")
	}
	return u, nil
}

func scimDecodePatch(r *http.Request) (*scimPatch, error) {
	var p scimPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		return nil, scimBad("invalidSyntax", "invalid JSON: %v", err)
	}
	if len(p.Operations) == 0 || len(p.Operations) > 1000 {
		return nil, scimBad("invalidValue", "1 to 1000 operations are required")
	}
	return &p, nil
}

// scimBool accepts true/false and the strings some clients send ("False").
func scimBool(raw json.RawMessage) (bool, error) {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, nil
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		if v, err := strconv.ParseBool(strings.ToLower(str)); err == nil {
			return v, nil
		}
	}
	return false, scimBad("invalidValue", "a boolean is required")
}

func scimString(raw json.RawMessage) (string, error) {
	var str string
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if err := json.Unmarshal(raw, &str); err != nil {
		return "", scimBad("invalidValue", "a string is required")
	}
	return str, nil
}

var scimEmailPath = regexp.MustCompile(`^emails\[type eq "[a-z]+"\]\.value$`)

// scimPatchUser applies PATCH operations to u. Attributes the wiki does
// not keep (title, phone numbers, the enterprise extension, ...) are
// ignored, as RFC 7644 allows a service provider to.
func scimPatchUser(u *auth.ScimUser, p *scimPatch) error {
	for _, op := range p.Operations {
		kind := strings.ToLower(op.Op)
		if kind != "add" && kind != "replace" && kind != "remove" {
			return scimBad("invalidValue", "unknown operation %q", op.Op)
		}
		if op.Path == "" {
			// value is an object of attributes
			var attrs map[string]json.RawMessage
			if err := json.Unmarshal(op.Value, &attrs); err != nil {
				return scimBad("invalidValue", "value must be an object without path")
			}
			for k, v := range attrs {
				if err := scimSetUserAttr(u, kind, k, v); err != nil {
					return err
				}
			}
			continue
		}
		if err := scimSetUserAttr(u, kind, op.Path, op.Value); err != nil {
			return err
		}
	}
	return nil
}

func scimSetUserAttr(u *auth.ScimUser, kind, path string, v json.RawMessage) error {
	p := strings.ToLower(strings.TrimPrefix(path, scimUserSchema+":"))
	remove := kind == "remove"
	str := func() (string, error) {
		if remove {
			return "", nil
		}
		return scimString(v)
	}
	var err error
	switch {
	case p == "active":
		if remove {
			return scimBad("mutability", "active cannot be removed")
		}
		u.Active, err = scimBool(v)
	case p == "username":
		if remove {
			return scimBad("mutability", "userName cannot be removed")
		}
		u.UserName, err = str()
	case p == "externalid":
		u.ExternalID, err = str()
	case p == "displayname":
		u.DisplayName, err = str()
	case p == "name.givenname":
		u.GivenName, err = str()
	case p == "name.familyname":
		u.FamilyName, err = str()
	case p == "name.formatted":
		var f string
		if f, err = str(); err == nil && f != "" {
			u.DisplayName = f
		}
	case p == "name":
		var n scimName
		if !remove {
			if err := json.Unmarshal(v, &n); err != nil {
				return scimBad("invalidValue", "name must be an object")
			}
		}
		u.GivenName, u.FamilyName = n.GivenName, n.FamilyName
	case p == "emails":
		var list []scimEmail
		if !remove {
			if err := json.Unmarshal(v, &list); err != nil {
				return scimBad("invalidValue", "emails must be a list")
			}
		}
		next := []auth.ScimEmail{}
		if kind == "add" {
			next = append(next, u.Emails...)
		}
		for _, e := range list {
			next = append(next, auth.ScimEmail{Value: e.Value, Primary: e.Primary})
		}
		u.Emails = next
	case scimEmailPath.MatchString(p):
		// the (work) address: replace the primary one
		var a string
		if a, err = str(); err != nil {
			return err
		}
		rest := []auth.ScimEmail{}
		for _, e := range u.Emails {
			if !e.Primary {
				rest = append(rest, e)
			}
		}
		if a != "" {
			rest = append([]auth.ScimEmail{{Value: a, Primary: true}}, rest...)
		}
		u.Emails = rest
	}
	return err
}

// ---------------------------------------------------------------- groups

func (s *Server) scimGroups(ctx context.Context, c *auth.ScimClient, r *http.Request, base string, id int64) (any, int, error) {
	a := s.app.Auth
	members := !strings.Contains(strings.ToLower(r.URL.Query().Get("excludedAttributes")), "members")
	switch {
	case r.Method == http.MethodGet && id == 0:
		f, err := scimParseFilter(r.URL.Query().Get("filter"))
		if err != nil {
			return nil, 0, err
		}
		start, count := scimPaging(r)
		list, total, err := a.ScimGroupList(ctx, c, f, start, count, members)
		if err != nil {
			return nil, 0, err
		}
		docs := make([]any, len(list))
		for i := range list {
			docs[i] = scimGroupOut(&list[i], base, members)
		}
		return scimList(docs, total, start), 200, nil
	case r.Method == http.MethodGet:
		g, err := a.ScimGroup(ctx, c, id)
		if err != nil {
			return nil, 0, err
		}
		return scimGroupOut(g, base, members), 200, nil
	case r.Method == http.MethodPost && id == 0:
		in, err := scimDecodeGroup(r)
		if err != nil {
			return nil, 0, err
		}
		g, err := a.ScimCreateGroup(ctx, c, in)
		if err != nil {
			return nil, 0, err
		}
		return scimGroupOut(g, base, true), 201, nil
	case r.Method == http.MethodPut && id > 0:
		in, err := scimDecodeGroup(r)
		if err != nil {
			return nil, 0, err
		}
		g, err := a.ScimReplaceGroup(ctx, c, id, in)
		if err != nil {
			return nil, 0, err
		}
		return scimGroupOut(g, base, true), 200, nil
	case r.Method == http.MethodPatch && id > 0:
		p, err := scimDecodePatch(r)
		if err != nil {
			return nil, 0, err
		}
		cur, err := a.ScimGroup(ctx, c, id)
		if err != nil {
			return nil, 0, err
		}
		if err := scimPatchGroup(cur, p); err != nil {
			return nil, 0, err
		}
		g, err := a.ScimReplaceGroup(ctx, c, id, *cur)
		if err != nil {
			return nil, 0, err
		}
		return scimGroupOut(g, base, true), 200, nil
	case r.Method == http.MethodDelete && id > 0:
		return nil, 204, a.ScimDeleteGroup(ctx, c, id)
	}
	return nil, 0, &scimError{Status: 405, Detail: "method not allowed"}
}

func scimGroupOut(g *auth.ScimGroup, base string, members bool) *scimGroupDoc {
	id := strconv.FormatInt(g.ID, 10)
	d := &scimGroupDoc{Schemas: []string{scimGroupSchema}, ID: id, ExternalID: g.ExternalID, DisplayName: g.DisplayName, Members: []scimRef{},
		Meta: &scimMeta{ResourceType: "Group", Created: scimTime(g.Created), LastModified: scimTime(g.Modified), Location: base + "/Groups/" + id}}
	if members {
		for _, m := range g.Members {
			d.Members = append(d.Members, scimRef{Value: m.Value, Display: m.Display, Ref: base + "/Users/" + m.Value})
		}
	} else {
		d.Members = nil
	}
	return d
}

func scimDecodeGroup(r *http.Request) (auth.ScimGroup, error) {
	var d scimGroupDoc
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		return auth.ScimGroup{}, scimBad("invalidSyntax", "invalid JSON: %v", err)
	}
	g := auth.ScimGroup{DisplayName: d.DisplayName, ExternalID: d.ExternalID}
	for _, m := range d.Members {
		g.Members = append(g.Members, auth.ScimRef{Value: m.Value})
	}
	return g, nil
}

var scimMemberPath = regexp.MustCompile(`^members\[value eq "([^"]+)"\]$`)

func scimPatchGroup(g *auth.ScimGroup, p *scimPatch) error {
	set := map[string]bool{}
	order := []string{}
	for _, m := range g.Members {
		if !set[m.Value] {
			set[m.Value] = true
			order = append(order, m.Value)
		}
	}
	refs := func(v json.RawMessage) ([]string, error) {
		var list []scimRef
		if len(v) == 0 || string(v) == "null" {
			return nil, nil
		}
		if err := json.Unmarshal(v, &list); err != nil {
			var one scimRef
			if err := json.Unmarshal(v, &one); err != nil {
				return nil, scimBad("invalidValue", "members must be a list of {value}")
			}
			list = []scimRef{one}
		}
		out := make([]string, len(list))
		for i, x := range list {
			out[i] = x.Value
		}
		return out, nil
	}
	for _, op := range p.Operations {
		kind := strings.ToLower(op.Op)
		path := strings.TrimSpace(op.Path)
		lp := strings.ToLower(path)
		switch {
		case kind != "add" && kind != "replace" && kind != "remove":
			return scimBad("invalidValue", "unknown operation %q", op.Op)
		case lp == "members":
			ids, err := refs(op.Value)
			if err != nil {
				return err
			}
			if kind == "replace" || (kind == "remove" && ids == nil) {
				set = map[string]bool{}
			}
			for _, id := range ids {
				set[id] = kind != "remove"
				if kind != "remove" {
					order = append(order, id)
				}
			}
		case scimMemberPath.MatchString(path) && kind == "remove":
			set[scimMemberPath.FindStringSubmatch(path)[1]] = false
		case lp == "displayname" && kind != "remove":
			v, err := scimString(op.Value)
			if err != nil {
				return err
			}
			g.DisplayName = v
		case lp == "externalid":
			v, err := scimString(op.Value)
			if err != nil {
				return err
			}
			g.ExternalID = v
		case path == "" && kind != "remove":
			var attrs struct {
				DisplayName *string   `json:"displayName"`
				ExternalID  *string   `json:"externalId"`
				Members     []scimRef `json:"members"`
			}
			if err := json.Unmarshal(op.Value, &attrs); err != nil {
				return scimBad("invalidValue", "value must be an object without path")
			}
			if attrs.DisplayName != nil {
				g.DisplayName = *attrs.DisplayName
			}
			if attrs.ExternalID != nil {
				g.ExternalID = *attrs.ExternalID
			}
			if kind == "replace" && attrs.Members != nil {
				set = map[string]bool{}
			}
			for _, m := range attrs.Members {
				set[m.Value] = true
				order = append(order, m.Value)
			}
		default:
			return scimBad("invalidPath", "unsupported path %q", path)
		}
	}
	g.Members = nil
	seen := map[string]bool{}
	for _, id := range order {
		if set[id] && !seen[id] {
			seen[id] = true
			g.Members = append(g.Members, auth.ScimRef{Value: id})
		}
	}
	return nil
}

// ---------------------------------------------------------------- common

var scimFilterRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9.:\[\] "]*?)\s+(?i:eq)\s+("(?:[^"\\]|\\.)*")\s*$`)

// scimParseFilter supports one equality comparison, as the directories use
// it to find a resource (userName eq "anna@example.org").
func scimParseFilter(f string) (*auth.ScimFilter, error) {
	if strings.TrimSpace(f) == "" {
		return nil, nil
	}
	if len(f) > 500 {
		return nil, scimBad("invalidFilter", "filter too long")
	}
	m := scimFilterRe.FindStringSubmatch(f)
	if m == nil {
		return nil, scimBad("invalidFilter", "only attribute eq \"value\" is supported")
	}
	var v string
	if err := json.Unmarshal([]byte(m[2]), &v); err != nil {
		return nil, scimBad("invalidFilter", "invalid value")
	}
	attr := strings.ToLower(strings.TrimSpace(m[1]))
	attr = strings.TrimPrefix(attr, strings.ToLower(scimUserSchema)+":")
	attr = strings.TrimPrefix(attr, strings.ToLower(scimGroupSchema)+":")
	if scimEmailPath.MatchString(attr) {
		attr = "emails.value"
	}
	return &auth.ScimFilter{Attr: attr, Value: v}, nil
}

func scimPaging(r *http.Request) (int, int) {
	start, err := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if err != nil || start < 1 {
		start = 1
	}
	count, err := strconv.Atoi(r.URL.Query().Get("count"))
	if err != nil || count < 0 || count > scimMaxResults {
		count = scimMaxResults
	}
	return start, count
}

func scimList(docs []any, total, start int) map[string]any {
	return map[string]any{"schemas": []string{scimListSchema}, "totalResults": total, "startIndex": start,
		"itemsPerPage": len(docs), "Resources": docs}
}

func scimWriteError(w http.ResponseWriter, err error) {
	se := &scimError{Status: 500, Detail: "internal error"}
	var ae *apperr.Error
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &se):
	case errors.As(err, &mbe):
		se = &scimError{Status: 413, Detail: "request too large"}
	case errors.As(err, &ae):
		se = &scimError{Status: ae.Status, Detail: ae.Msg}
		switch ae.Status {
		case 400:
			se.ScimType = "invalidValue"
		case 409:
			se.ScimType = "uniqueness"
		}
	}
	w.Header().Set("Content-Type", scimContentType)
	w.WriteHeader(se.Status)
	body := map[string]any{"schemas": []string{scimErrorSchema}, "status": strconv.Itoa(se.Status), "detail": se.Detail}
	if se.ScimType != "" {
		body["scimType"] = se.ScimType
	}
	_ = json.NewEncoder(w).Encode(body)
}

func scimProviderConfig(base string) map[string]any {
	return map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": base,
		"patch":            map[string]bool{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": scimMaxResults},
		"changePassword":   map[string]bool{"supported": false},
		"sort":             map[string]bool{"supported": false},
		"etag":             map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "Bearer token",
			"description": "SCIM token of the client (Administration → SCIM)", "primary": true}},
		"meta": map[string]string{"resourceType": "ServiceProviderConfig", "location": base + "/ServiceProviderConfig"},
	}
}

func scimResourceTypes(base string) map[string]any {
	rt := func(name, schema string) map[string]any {
		return map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": name, "name": name,
			"endpoint": "/" + name + "s", "schema": schema, "meta": map[string]string{"resourceType": "ResourceType", "location": base + "/ResourceTypes/" + name}}
	}
	docs := []any{rt("User", scimUserSchema), rt("Group", scimGroupSchema)}
	return scimList(docs, len(docs), 1)
}

func scimSchemas() map[string]any {
	attr := func(name, typ string, required bool) map[string]any {
		return map[string]any{"name": name, "type": typ, "multiValued": false, "required": required, "mutability": "readWrite", "returned": "default"}
	}
	multi := func(name string) map[string]any {
		a := attr(name, "complex", false)
		a["multiValued"] = true
		return a
	}
	docs := []any{
		map[string]any{"id": scimUserSchema, "name": "User", "attributes": []any{attr("userName", "string", true), attr("externalId", "string", false),
			attr("displayName", "string", false), attr("name", "complex", false), multi("emails"), attr("active", "boolean", false)}},
		map[string]any{"id": scimGroupSchema, "name": "Group", "attributes": []any{attr("displayName", "string", true), attr("externalId", "string", false), multi("members")}},
	}
	return scimList(docs, len(docs), 1)
}
