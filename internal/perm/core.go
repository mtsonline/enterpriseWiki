package perm

// Global permissions of the core. The category permissions view, edit,
// create, delete and manage are the bits of acl.Perm (see acl.Names).
func init() {
	MustRegister("core",
		Def{Key: "core.categories.admin", Scope: Global, Label: "Category administrator",
			Description: "Create top-level categories, move categories to the top level and manage the structure, settings and permissions of all categories (content only with editor permissions)."},
		Def{Key: "core.users.manage", Scope: Global, Label: "Manage users", Description: "Create, edit, disable and delete users, including their profiles."},
		Def{Key: "core.groups.manage", Scope: Global, Label: "Manage groups", Description: "Create and edit groups and their members."},
		Def{Key: "core.roles.manage", Scope: Global, Label: "Manage roles and their permissions",
			Description: "Create and edit roles, assign global permissions. Only administrators may grant permissions they do not have themselves."},
		Def{Key: "core.settings.manage", Scope: Global, Label: "Change settings", Description: "Site, languages, sign-in, retention and plugin settings."},
		Def{Key: "core.plugins.manage", Scope: Global, Label: "Manage plugins and updates", Description: "Enable, disable, install, build and update plugins, restart the server, background jobs."},
		Def{Key: "core.mail.manage", Scope: Global, Label: "Manage e-mail", Description: "E-mail settings, outbox and test messages."},
		Def{Key: "core.webhooks.manage", Scope: Global, Label: "Manage webhooks"},
		Def{Key: "core.license.manage", Scope: Global, Label: "Manage the license"},
		Def{Key: "core.system.manage", Scope: Global, Label: "System status", Description: "Database, guest cache and system information."},
		Def{Key: "core.audit.view", Scope: Global, Label: "View the audit log"},
		Def{Key: "core.audit.manage", Scope: Global, Label: "Configure the audit log", Description: "What is logged, retention and forwarding to other systems.",
			Implies: []string{"core.audit.view"}},
		Def{Key: "core.i18n.manage", Scope: Global, Label: "Translate the user interface"},
		Def{Key: "core.trash.manage", Scope: Global, Label: "Empty the trash", Description: "Permanently delete objects in the trash."},
		Def{Key: "core.elements.admin", Scope: Global, Label: "Element overview", Description: "List all elements with their short URLs and tags in the administration."},
		Def{Key: "core.watches.assign", Scope: Global, Label: "Assign watches", Description: "Make users, groups and roles watch categories and elements (mandatory or optional)."},
		Def{Key: "core.templates.global", Scope: Global, Label: "Save templates for everybody"},
	)
}
