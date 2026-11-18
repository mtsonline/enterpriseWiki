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
}
