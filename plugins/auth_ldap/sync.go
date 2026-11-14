package auth_ldap

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// Directory sync: users and groups from the directory without a sign-in.
//
//	sync_enabled: false            # regular runs (the button works always)
//	sync_interval_minutes: 60      # at least 5
//	sync_filter: ""                # people to sync; empty = user_filter with {username} = *
//	disabled_filter: ""            # people the directory has disabled,
//	                               # AD: (userAccountControl:1.2.840.113556.1.4.803:=2)
//	sync_create_users: true        # accounts before the first sign-in
//	sync_disable_missing: true     # disable accounts no longer in the directory
//	sync_max_disable_percent: 10   # refuse runs that would disable more

const (
	limitMaxEntries = "auth.ldap.sync_max_entries"
	pageSize        = 500
	minInterval     = 5 * time.Minute
)

// syncSettings are the settings of a run.
type syncSettings struct {
	filter, disabledFilter string
	create, disableMissing bool
	maxDisablePercent      int
	interval               time.Duration
	enabled                bool
}

func readSyncSettings(s map[string]any) syncSettings {
	out := syncSettings{
		filter:            auth.Setting(s, "sync_filter", ""),
		disabledFilter:    auth.Setting(s, "disabled_filter", ""),
		create:            auth.BoolSetting(s, "sync_create_users", true),
		disableMissing:    auth.BoolSetting(s, "sync_disable_missing", true),
		maxDisablePercent: 10,
		interval:          time.Hour,
		enabled:           auth.BoolSetting(s, "sync_enabled", false),
	}
	if out.filter == "" {
		out.filter = strings.ReplaceAll(auth.Setting(s, "user_filter", "(uid={username})"), "{username}", "*")
	}
	if v, err := strconv.Atoi(auth.Setting(s, "sync_max_disable_percent", "")); err == nil && v >= 0 && v <= 100 {
		out.maxDisablePercent = v
	}
	if v, err := strconv.Atoi(auth.Setting(s, "sync_interval_minutes", "")); err == nil && v > 0 {
		out.interval = max(time.Duration(v)*time.Minute, minInterval)
	}
	return out
}

// directory reads all people of the directory with their groups, mapped as
// at a sign-in, and the ids of disabled people.
func (p *Provider) directory(maxEntries int) ([]*auth.ExternalIdentity, []string, error) {
	s := p.settings()
	if auth.Setting(s, "url", "") == "" {
		return nil, nil, apperr.BadRequest("LDAP is not configured")
	}
	st := readSyncSettings(s)
	conn, err := p.dial(s)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = conn.Close() }()
	if err := p.serviceBind(conn, s); err != nil {
		return nil, nil, err
	}
	base := auth.Setting(s, "base_dn", "")
	search := func(filter string, attrs []string) ([]*ldap.Entry, error) {
		// size limit maxEntries+1: more entries than allowed stop the run
		// instead of silently syncing a part
		res, err := conn.SearchWithPaging(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
			maxEntries+1, 0, false, filter, attrs, nil), pageSize)
		if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
				return nil, apperr.Conflict("the directory has more than %d matching entries (limit %s)", maxEntries, limitMaxEntries)
			}
			return nil, fmt.Errorf("directory search: %w", err)
		}
		if res == nil {
			return nil, nil
		}
		if len(res.Entries) > maxEntries {
			return nil, apperr.Conflict("the directory has more than %d matching entries (limit %s)", maxEntries, limitMaxEntries)
		}
		return res.Entries, nil
	}
	var disabled []string
	if st.disabledFilter != "" {
		subjAttr := auth.Setting(s, "attr_subject", "")
		attrs := []string{"dn"}
		if subjAttr != "" {
			attrs = append(attrs, subjAttr)
		}
		entries, err := search("(&"+st.filter+st.disabledFilter+")", attrs)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			if subjAttr == "" {
				disabled = append(disabled, e.DN)
			} else {
				disabled = append(disabled, subjectOf(e, subjAttr))
			}
		}
	}
	entries, err := search(st.filter, entryAttributes(s))
	if err != nil {
		return nil, nil, err
	}
	mapping := auth.MappingFromSettings(s)
	out := make([]*auth.ExternalIdentity, 0, len(entries))
	for _, e := range entries {
		id := p.entryIdentity(s, e, "")
		if id.Username == "" {
			continue
		}
		// the same lookup as at a sign-in, so both give the same groups
		if err := p.lookupGroups(conn, s, e, id); err != nil {
			return nil, nil, err
		}
		mapping.Apply(id)
		out = append(out, id)
	}
	return out, disabled, nil
}

// syncRun reads the directory of a provider and syncs its accounts.
func (p *Provider) syncRun(ctx context.Context, host *sdk.Host, dryRun, force bool) (*auth.DirectoryReport, error) {
	st := readSyncSettings(p.settings())
	entries, disabled, err := p.directory(int(host.Limit(limitMaxEntries)))
	if err != nil {
		return nil, err
	}
	return host.Auth.SyncDirectory(ctx, auth.DirectorySync{Provider: p.id, Entries: entries, Disabled: disabled,
		CreateUsers: st.create, DisableMissing: st.disableMissing, MaxDisablePercent: st.maxDisablePercent,
		Force: force, DryRun: dryRun, TriggeredBy: auth.SyncTriggeredBy(ctx)})
}

// registerSync adds the API routes and the background job of the sync.
func registerSync(host *sdk.Host, set *auth.InstanceSet[*Provider]) {
	// people and their groups are changed: managing users is required, as is
	// managing settings (the sync uses the stored connection)
	allowed := func(r *http.Request) error {
		if err := perm.Require(r.Context(), "core.users.manage"); err != nil {
			return err
		}
		return perm.Require(r.Context(), "core.settings.manage")
	}
	// action: auth.ldap.sync.list (settings and newest runs of an instance)
	host.Handle("GET", "/sync", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := allowed(r); err != nil {
			return nil, err
		}
		prov, ok := set.Get(r.URL.Query().Get("instance"))
		if !ok {
			return nil, apperr.NotFound("instance")
		}
		st := readSyncSettings(prov.settings())
		runs, err := host.Auth.DirectoryRuns(r.Context(), prov.ID(), 20)
		if err != nil {
			return nil, err
		}
		if runs == nil {
			runs = []auth.DirectoryRun{}
		}
		return map[string]any{"provider": prov.ID(), "enabled": st.enabled, "interval_minutes": int(st.interval / time.Minute),
			"filter": st.filter, "runs": runs}, nil
	})
	// action: auth.ldap.sync.run (dry_run: preview without changes)
	host.Handle("POST", "/sync", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := allowed(r); err != nil {
			return nil, err
		}
		var in struct {
			Instance string `json:"instance"`
			DryRun   bool   `json:"dry_run"`
			Force    bool   `json:"force"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		prov, ok := set.Get(in.Instance)
		if !ok {
			return nil, apperr.NotFound("instance")
		}
		rep, err := prov.syncRun(r.Context(), host, in.DryRun, in.Force)
		if rep != nil && rep.Status == "refused" {
			// the report says why; the interface offers to run it with force
			return rep, nil
		}
		return rep, err
	})
	// action: auth.ldap.sync.adopt (same person with a new directory id)
	host.Handle("POST", "/sync/adopt", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := allowed(r); err != nil {
			return nil, err
		}
		var in struct {
			Instance string `json:"instance"`
			Username string `json:"username"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		prov, ok := set.Get(in.Instance)
		if !ok {
			return nil, apperr.NotFound("instance")
		}
		// the id comes from the directory, never from the request
		entries, _, err := prov.directory(int(host.Limit(limitMaxEntries)))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Username == in.Username {
				if err := host.Auth.AdoptDirectoryIdentity(r.Context(), prov.ID(), e.Username, e.Subject); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true, "username": e.Username}, nil
			}
		}
		return nil, apperr.NotFound("directory entry")
	})
	host.RegisterWorker("sync", time.Minute, func(ctx context.Context) error {
		for _, prov := range set.All() {
			st := readSyncSettings(prov.settings())
			if !st.enabled || !prov.Configured() {
				continue
			}
			last, err := host.Auth.LastDirectorySync(ctx, prov.ID())
			if err != nil || time.Since(last) < st.interval {
				continue
			}
			if _, err := prov.syncRun(ctx, host, false, false); err != nil {
				host.Log.Warn("directory sync failed", "provider", prov.ID(), "err", err)
			}
		}
		return nil
	})
}
