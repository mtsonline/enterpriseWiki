package main

// trap is a deliberate bug in a security-critical place. With it built in,
// the tests named must fail; if they stay green, they do not check what
// they claim (a finding). Old must occur exactly once in File; otherwise
// the trap is outdated and must be adjusted to the code (also a finding:
// the place it guarded has moved).
type trap struct {
	ID    string
	About string // what goes wrong (English, for the report)
	File  string
	Old   string
	New   string
	Pkg   string // Go package with the tests
	Tests string // -run pattern
}

// traps is the fixed list (plan "Qualität der Tests", section 3). It grows
// with every fixed security bug.
var traps = []trap{
	// ---------------------------------------------------------------- ACL
	{
		ID: "acl.element-hidden", About: "elements with own rights only are shown to everybody who may see the category",
		File: "internal/acl/element.go",
		Old:  `".id = ANY(" + b + "::bigint[]))) OR "`,
		New:  `".id = ANY(" + b + "::bigint[]) AND FALSE)) OR "`,
		Pkg:  "./internal/server/", Tests: "^TestElementRights$",
	},
	{
		ID: "acl.element-hidden-allows", About: "the filter for results not filtered in the query lets hidden elements through",
		File: "internal/acl/element.go",
		Old:  "_, hidden := slices.BinarySearch(f.Hide, elementID)\n\treturn !hidden",
		New:  "_, hidden := slices.BinarySearch(f.Hide, elementID)\n\treturn !hidden || true",
		Pkg:  "./internal/server/", Tests: "^(TestElementRights|TestInstanceLabels)$",
	},
	// ---------------------------------------------------------------- copies
	{
		ID: "copy.element-strict", About: "a strictly confidential element can be duplicated",
		File: "internal/content/elements.go",
		Old:  "if s.acl.ElementClassification(ctx, e.ID, e.CategoryID) >= acl.StrictConfidential {",
		New:  "if false && s.acl.ElementClassification(ctx, e.ID, e.CategoryID) >= acl.StrictConfidential {",
		Pkg:  "./internal/server/", Tests: "^(TestElementRights|TestClassification)$",
	},
	{
		ID: "copy.category-strict", About: "a strictly confidential category can be duplicated",
		File: "internal/content/categories.go",
		Old:  "if s.acl.Classification(ctx, id) >= acl.StrictConfidential {\n\t\treturn nil, apperr.Forbidden(\"strictly confidential content is not copied\")",
		New:  "if false && s.acl.Classification(ctx, id) >= acl.StrictConfidential {\n\t\treturn nil, apperr.Forbidden(\"strictly confidential content is not copied\")",
		Pkg:  "./internal/server/", Tests: "^(TestClassification|TestProtectionOptions|TestElementRights)$",
	},
	{
		ID: "copy.elements-strict", About: "duplicating a category copies its strictly confidential elements",
		File: "internal/content/categories.go",
		Old:  "|| s.acl.ElementClassification(ctx, eid, from) >= acl.StrictConfidential {",
		New:  "|| (false && s.acl.ElementClassification(ctx, eid, from) >= acl.StrictConfidential) {",
		Pkg:  "./internal/server/", Tests: "^(TestClassification|TestElementRights|TestProtectionOptions|TestDuplicateSkipsStrictlyConfidentialPages)$",
	},
	{
		ID: "copy.category-protection", About: "a copied category loses its classification, watermark and downloads setting",
		File: "internal/content/categories.go",
		Old:  "c.Template, c.Classification, c.Watermark, c.Downloads, c.EditorTools)",
		New:  "c.Template, nil, nil, nil, c.EditorTools)",
		Pkg:  "./internal/server/", Tests: "^TestDuplicateKeepsProtection$",
	},
	// ---------------------------------------------------------------- instances
	{
		ID: "instance.arrange-manage", About: "arranging a blog does not need the permission to manage it",
		File: "internal/content/placements.go",
		Old:  `if mk == "" || !slices.Contains(mine, mk) {`,
		New:  `if false && (mk == "" || !slices.Contains(mine, mk)) {`,
		Pkg:  "./internal/server/", Tests: "^TestPlacementsInstance$",
	},
	{
		ID: "instance.labels-manage", About: "changing the labels of an instance does not need the permission to manage it",
		File: "internal/server/api_instances.go",
		Old:  "if !manage {\n\t\t\treturn nil, apperr.Forbidden(\"changing the labels needs the permission to manage this instance\")",
		New:  "if false && !manage {\n\t\t\treturn nil, apperr.Forbidden(\"changing the labels needs the permission to manage this instance\")",
		Pkg:  "./internal/server/", Tests: "^TestInstanceLabels$",
	},
	{
		ID: "instance.labels-list-visible", About: "the translation list shows labels of instances the translator may not see",
		File: "internal/i18n/i18n.go",
		Old:  "ok && visible[id] && s.knownInstanceKey(k)",
		New:  "ok && (visible[id] || true) && s.knownInstanceKey(k)",
		Pkg:  "./internal/server/", Tests: "^TestInstanceLabels$",
	},
	{
		ID: "instance.labels-set-visible", About: "translators change labels of instances they may not see",
		File: "internal/i18n/i18n.go",
		Old:  "isInst && visible[iid] && s.knownInstanceKey(k)",
		New:  "isInst && (visible[iid] || true) && s.knownInstanceKey(k)",
		Pkg:  "./internal/server/", Tests: "^TestInstanceLabels$",
	},
	{
		ID: "placement.instance-positions", About: "one page places blocks at positions of the site",
		File: "internal/content/placements.go",
		Old:  `if level == LevelInstance && pl.Position != "" && !strings.HasPrefix(pl.Position, ElementPositionPrefix) {`,
		New:  `if false && level == LevelInstance && pl.Position != "" && !strings.HasPrefix(pl.Position, ElementPositionPrefix) {`,
		Pkg:  "./internal/server/", Tests: "^TestPlacementsInstance$",
	},
	{
		ID: "placement.user-dashboard-only", About: "without personal placement the own level moves blocks of the interface (not only the dashboard)",
		File: "internal/content/placements.go",
		Old:  `if level == LevelUser && !ui.PersonalPlacement && pl.Position != "" && !dash {`,
		New:  `if false && level == LevelUser && !ui.PersonalPlacement && pl.Position != "" && !dash {`,
		Pkg:  "./internal/server/", Tests: "^TestPlacements$",
	},
	{
		ID: "placement.offered-tiles", About: "a person puts a tile on the own dashboard that the administration does not offer",
		File: "internal/content/placements.go",
		Old:  `if level == LevelUser && dash && len(ui.DashboardTiles) > 0 && !slices.Contains(ui.DashboardTiles, pl.Block) {`,
		New:  `if false && level == LevelUser && dash && len(ui.DashboardTiles) > 0 && !slices.Contains(ui.DashboardTiles, pl.Block) {`,
		Pkg:  "./internal/server/", Tests: "^TestPlacements$",
	},
	// ---------------------------------------------------------------- sign-in
	{
		ID: "auth.password-change-sessions", About: "changing the own password leaves the other sessions open",
		File: "internal/auth/manage.go",
		Old:  "DELETE FROM sessions WHERE user_id=$1 AND token_hash <> $2",
		New:  "DELETE FROM sessions WHERE user_id=$1 AND token_hash <> $2 AND FALSE",
		Pkg:  "./internal/server/", Tests: "^TestSessions$",
	},
	{
		ID: "auth.reset-sessions", About: "resetting a forgotten password leaves the sessions open",
		File: "internal/auth/reset.go",
		Old:  "`DELETE FROM sessions WHERE user_id=$1`, uid",
		New:  "`DELETE FROM sessions WHERE user_id=$1 AND FALSE`, uid",
		Pkg:  "./internal/server/", Tests: "^TestPasswordReset$",
	},
	{
		ID: "auth.disabled-account", About: "a disabled account keeps working with its session or key",
		File: "internal/auth/service.go",
		Old:  "if disabled {\n\t\treturn nil, apperr.ErrUnauthorized",
		New:  "if false && disabled {\n\t\treturn nil, apperr.ErrUnauthorized",
		Pkg:  "./internal/server/", Tests: "^(TestAPI|TestWorkflow3Accounts|TestMultiSite|TestDirectorySync|TestDisabledAccountIsLockedOut)$",
	},
	{
		ID: "auth.csrf", About: "requests with the session cookie are accepted without the X-Requested-With header",
		File: "internal/server/middleware.go",
		Old:  "strings.HasPrefix(r.URL.Path, \"/api/\") && !safeMethod(r.Method) && r.Header.Get(csrfHeader) != csrfValue",
		New:  "false && strings.HasPrefix(r.URL.Path, \"/api/\") && !safeMethod(r.Method) && r.Header.Get(csrfHeader) != csrfValue",
		Pkg:  "./internal/server/", Tests: "^(TestSecurityBaseline|TestHardening|TestAPI)$",
	},
	// ---------------------------------------------------------------- --as and keys
	{
		ID: "cli.as", About: "--as USER runs with the rights of the CLI superuser instead of the user's",
		File: "internal/cli/commands.go",
		Old:  "up.Via = \"cli-as\"\n\t\t\tp = up",
		New:  "up.Via = \"cli-as\"\n\t\t\t_ = up",
		Pkg:  "./internal/server/", Tests: "^TestCLIAsNeverEscalates$",
	},
	{
		ID: "apikey.owner-budget", About: "the owner of an API key grants it a budget above the limit of the account",
		File: "internal/auth/manage.go",
		Old:  "own, admin := s.keyRateMax()\n\tmax := own",
		New:  "own, admin := s.keyRateMax()\n\tmax := admin\n\t_ = own",
		Pkg:  "./internal/server/", Tests: "^TestAPIKeyRateLimit$",
	},
	// ---------------------------------------------------------------- dashboard
	{
		ID: "dashboard.forms-view", About: "the tile of new submissions lists forms the caller may not view (creator without the right)",
		File: "plugins/content_forms/submissions.go",
		Old:  "if p.host.Content.ElementPerms(ctx, c.ElementID, c.cat)&acl.View == 0 {",
		New:  "if false && p.host.Content.ElementPerms(ctx, c.ElementID, c.cat)&acl.View == 0 {",
		Pkg:  "./internal/server/", Tests: "^TestRecentSubmissions$",
	},
	{
		ID: "dashboard.forms-permission", About: "the tile of new submissions lists forms to everybody who may view them",
		File: "plugins/content_forms/submissions.go",
		Old:  "if (c.creator == nil || *c.creator != me.ID()) && !p.host.CanElement(ctx, c.ElementID, c.cat, permSubmissions) {",
		New:  "if false && (c.creator == nil || *c.creator != me.ID()) && !p.host.CanElement(ctx, c.ElementID, c.cat, permSubmissions) {",
		Pkg:  "./internal/server/", Tests: "^TestRecentSubmissions$",
	},
	{
		ID: "dashboard.status-admin", About: "the state of the wiki is shown to every signed-in person",
		File: "internal/server/api_m3.go",
		Old:  "if err := requireAdmin(r); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tout := map[string]any{",
		New:  "if err := requireAdmin(r); err != nil && r.Context() == nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tout := map[string]any{",
		Pkg:  "./internal/server/", Tests: "^TestDashboardHomeAndStatus$",
	},
	// ---------------------------------------------------------------- category page (M19)
	{
		ID: "category.overview-filter", About: "the overview of a category lists pages the reader may not view",
		File: "internal/content/categoryview.go",
		Old:  "		AND ` + f.SQL(\"e\", &args)",
		New:  "		AND (` + f.SQL(\"e\", &args) + ` OR TRUE)`",
		Pkg:  "./internal/server/", Tests: "^TestCategoryOverview$",
	},
	{
		ID: "category.overview-hidden-subs", About: "the overview with subcategories lists pages of hidden or archived subcategories",
		File: "internal/content/categoryview.go",
		Old:  "AND (a.deleted_at IS NOT NULL OR a.hidden OR a.archived_at IS NOT NULL)",
		New:  "AND (a.deleted_at IS NOT NULL)",
		Pkg:  "./internal/server/", Tests: "^TestCategoryOverview$",
	},
	{
		ID: "category.page-manage", About: "the page of a category can be arranged without the permission to manage it",
		File: "internal/content/placements.go",
		Old:  "			return apperr.BadRequest(\"target must be a category id\")\n		}\n		if _, err := s.loadCategory(ctx, s.pool, id, false); err != nil {\n			return err\n		}\n		return s.require(ctx, id, acl.Manage)\n	case LevelInstance:",
		New:  "			return apperr.BadRequest(\"target must be a category id\")\n		}\n		if _, err := s.loadCategory(ctx, s.pool, id, false); err != nil {\n			return err\n		}\n		return s.require(ctx, id, acl.Edit)\n	case LevelInstance:",
		Pkg:  "./internal/server/", Tests: "^TestCategoryPagePlacement$",
	},
	{
		ID: "category.featured-visible", About: "a manager features a page he may not view",
		File: "internal/content/placements.go",
		Old:  "		if !vis[id] {",
		New:  "		if false && !vis[id] {",
		Pkg:  "./internal/server/", Tests: "^TestCategoryPagePlacement$",
	},
	{
		ID: "category.full-limit", About: "whole pages of an overview are not limited (accordion, tabs, all)",
		File: "internal/content/categoryview.go",
		Old:  "size = min(size, int(limits.Value(s.cfg.Get(), limits.CategoryFull)))",
		New:  "size = max(size, int(limits.Value(s.cfg.Get(), limits.CategoryFull)))",
		Pkg:  "./internal/server/", Tests: "^TestCategoryOverview$",
	},
	{
		ID: "category.columns-key", About: "the columns of a table take any text",
		File: "internal/content/categoryview.go",
		Old:  "if !columnRe.MatchString(c) {",
		New:  "if c == \"\" && !columnRe.MatchString(c) {",
		Pkg:  "./internal/server/", Tests: "^TestCategoryOverview$",
	},
	// ---------------------------------------------------------------- templates of data
	{
		ID: "template.design-perm", About: "templates of data can be changed without the permission core.template.design",
		File: "plugins/core_template/store.go",
		Old:  "design := func(r *http.Request) error { return perm.Require(r.Context(), permDesign) }",
		New:  "design := func(r *http.Request) error { _ = perm.Require; return nil }",
		Pkg:  "./internal/server/", Tests: "^TestDataTemplates$",
	},
	{
		ID: "template.read-only", About: "the shipped templates of data can be changed",
		File: "plugins/core_template/store.go",
		Old:  "	if cur.ReadOnly {\n		return nil, apperr.Forbidden",
		New:  "	if cur.ReadOnly && false {\n		return nil, apperr.Forbidden",
		Pkg:  "./internal/server/", Tests: "^TestDataTemplates$",
	},
	{
		ID: "template.in-use", About: "a template in use goes to the trash (pages fall back silently)",
		File: "plugins/core_template/store.go",
		Old:  "		if len(used) > 0 {",
		New:  "		if false && len(used) > 0 {",
		Pkg:  "./internal/server/", Tests: "^TestDataTemplates$",
	},
	{
		ID: "template.contrast", About: "colors without enough contrast are saved",
		File: "plugins/core_template/design.go",
		Old:  "if c := Contrast(colors[p.Fore], colors[p.Back]); c < p.Min() {",
		New:  "if c := Contrast(colors[p.Fore], colors[p.Back]); c < 0 {",
		Pkg:  "./plugins/core_template/", Tests: "^TestDesignProblems$",
	},
	{
		ID: "template.unknown-fields", About: "a design takes fields nobody checks",
		File: "plugins/core_template/design.go",
		Old:  "	dec.DisallowUnknownFields()\n	var d Design\n	if err := dec.Decode(&d); err != nil {\n		return nil, apperr.BadRequest",
		New:  "	var d Design\n	if err := dec.Decode(&d); err != nil {\n		return nil, apperr.BadRequest",
		Pkg:  "./plugins/core_template/", Tests: "^TestParseDesignRefusesUnknownFields$",
	},
	{
		ID: "template.positions-room", About: "positions side by side in a block are saved without room",
		File: "plugins/core_template/design.go",
		Old:  "	return float64(n)*limits.PositionMinRem + float64(n-1)*limits.PositionGapRem",
		New:  "	return 0 * (float64(n)*limits.PositionMinRem + float64(n-1)*limits.PositionGapRem)",
		Pkg:  "./plugins/core_template/", Tests: "^TestDesignProblems$",
	},
	{
		ID: "template.room", About: "blocks are saved side by side on the desktop without room",
		File: "plugins/core_template/design.go",
		Old:  `if need, room := d.needRoom(r, "desktop", nil), d.room(r, "desktop"); need > room {`,
		New:  `if need, room := d.needRoom(r, "desktop", nil), d.room(r, "desktop"); need > room*10 {`,
		Pkg:  "./plugins/core_template/", Tests: "^TestDesignProblems$",
	},
}
