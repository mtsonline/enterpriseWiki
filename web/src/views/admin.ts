// Administration: users, groups, roles, plugins, settings, languages, audit.
import { profileCardForm } from '../components/profilecard';
import { elementsAdmin } from './adminElements';
import { webhooksAdmin } from './adminWebhooks';
import { licenseAdmin, licenseNotice, type LicenseStatus } from './adminLicense';
import { databaseFacts, databaseNotice, type DatabaseStatus } from './adminDatabase';
import { categorySelect, elementSelect } from '../components/pickers';
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { canTranslateAny, hasPerm, isAdmin, isInstanceAdmin, state } from '../core/state';
import { globalEditor, loadCatalog, permLabel } from '../components/permissions';
import { permissionsAdmin } from './adminPermissions';
import { auditAdmin } from './adminAudit';
import type { Group, Role } from '../core/types';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, pageHeader, select, textArea, textInput } from '../components/ui';
import { langName, setContext } from '../components/widgets';
import { previewImageInput } from '../components/previewimage';
import { listAdminPages, listTemplates, templateLabel } from '../sdk';
import { errorPage } from './content';
import { watchesAdmin } from './adminWatches';
import { languagesAdmin } from './adminLanguages';
import { sitesAdmin } from './adminSites';

interface User { id: number; username: string; provider: string; handle: string; display_name: string; email: string; language: string; disabled: boolean; locked_until: string | null; created_at: string; last_login_at: string | null; groups: number[]; roles: number[]; totp_enabled: boolean; mfa_email: boolean; networks: string[] }

const CORE_SECTIONS = ['users', 'groups', 'roles', 'permissions', 'elements', 'plugins', 'updates', 'settings', 'mail', 'webhooks', 'cache', 'languages', 'audit', 'watches', 'license', 'sites'];

/** global permission of each administration page ('' = category managers too) */
const SECTION_PERMS: Record<string, string> = {
  users: 'core.users.manage', groups: 'core.groups.manage', roles: 'core.roles.manage', permissions: '',
  elements: 'core.elements.admin', plugins: 'core.plugins.manage', updates: 'core.plugins.manage', settings: 'core.settings.manage',
  mail: 'core.mail.manage', webhooks: 'core.webhooks.manage', cache: 'core.system.manage', languages: 'core.i18n.manage',
  audit: 'core.audit.view', watches: 'core.watches.assign', license: 'core.license.manage',
};

/** adminSections lists the administration pages the user may open. */
export function adminSections(): { id: string; label: string }[] {
  const core = CORE_SECTIONS.filter((s) => {
    const k = SECTION_PERMS[s];
    if (s === 'languages') return canTranslateAny();
    if (s === 'sites') return isInstanceAdmin();
    // license and plugin installation concern the whole instance
    if (s === 'license' || s === 'updates') return hasPerm(k) && (state.site.site === 'main' || isInstanceAdmin());
    return k === '' ? !!state.user && (isAdmin() || hasPerm('core.categories.admin') || state.managesCategories) : hasPerm(k);
  }).map((s) => ({ id: s, label: t(`admin.${s}`) }));
  const plugin = listAdminPages().filter((p) => (p.permission ? hasPerm(p.permission) : isAdmin())).map((p) => ({ id: p.id, label: p.label() }));
  return [...core, ...plugin];
}

const reload = () => navigate(location.pathname + location.search, { replace: true });

export async function adminView({ params, query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  if (!state.user) {
    navigate(`/login?next=${encodeURIComponent(location.pathname)}`, { replace: true });
    return;
  }
  const pages = adminSections();
  const section = params.section ?? pages[0]?.id ?? 'users';
  if (!pages.some((p) => p.id === section)) {
    mount(main, h('h1', null, t('error.forbidden')));
    return;
  }
  const panel = h('div');
  const notice = h('div');
  mount(main, h('div', { class: 'ew-admin' },
    h('nav', { class: 'ew-subnav', 'aria-label': t('admin.title') }, h('h2', { class: 'ew-subnav-title' }, t('admin.title')),
      h('ul', null, ...pages.map((p) => h('li', null, h('a', { href: `/admin/${p.id}`, 'aria-current': p.id === section ? 'page' : undefined }, p.label))))),
    h('div', { class: 'ew-admin-panel' }, notice, panel)));
  // problems with the license on every administration page (not on the license page itself)
  // and problems with the PostgreSQL version (end of support)
  void Promise.all([
    section === 'license' || !hasPerm('core.license.manage') ? null : api.get<LicenseStatus>('admin/license').catch(() => null),
    section === 'settings' || !hasPerm('core.system.manage') ? null : api.get<DatabaseStatus>('admin/database').catch(() => null),
  ]).then(([lic, db]) => {
    const items = [lic && (lic.level === 'warning' || lic.level === 'error') ? licenseNotice(lic) : null, db ? databaseNotice(db) : null].filter(Boolean) as HTMLElement[];
    if (items.length) mount(notice, ...items);
  });
  try {
    switch (section) {
      case 'users': await users(panel, query); break;
      case 'groups': await groups(panel); break;
      case 'roles': await roles(panel); break;
      case 'permissions': await permissionsAdmin(panel, query); break;
      case 'plugins': await plugins(panel); break;
      case 'updates': await updates(panel, false); break;
      case 'settings': await settings(panel); break;
      case 'mail': await mailAdmin(panel, query); break;
      case 'elements': await elementsAdmin(panel); break;
      case 'webhooks': await webhooksAdmin(panel); break;
      case 'cache': await cacheAdmin(panel); break;
      case 'languages': await languagesAdmin(panel, query); break;
      case 'audit': await auditAdmin(panel, query); break;
      case 'watches': await watchesAdmin(panel); break;
      case 'license': await licenseAdmin(panel); break;
      case 'sites': await sitesAdmin(panel); break;
      default: {
        const p = listAdminPages().find((x) => x.id === section);
        if (p) await p.render(panel);
        else mount(panel, h('h1', null, t('error.not_found')));
      }
    }
  } catch (e) {
    errorPage(panel, e);
  }
}

/** sharedEditable: users, groups and role definitions are shared by all
 * sites; on further sites only instance administrators change them. */
function sharedEditable(): boolean {
  return state.site.site === 'main' || isInstanceAdmin();
}

function sharedNotice(): HTMLElement | null {
  return sharedEditable() ? null : h('p', { class: 'ew-notice' }, t('sites.shared_notice'));
}

function table(caption: string, headers: string[], rows: HTMLElement[], emptyText: string): HTMLElement {
  return h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, caption),
    h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, emptyText))])));
}

// ---------------------------------------------------------------- users

async function users(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const q = query.get('q') ?? '';
  const [list, gs, rs] = await Promise.all([api.get<User[]>(`admin/users${qs({ q })}`), api.get<Group[]>('groups'), api.get<Role[]>('roles')]);
  const search = textInput({ value: q, type: 'search' });
  const sf = h('form', { role: 'search', class: 'ew-inline-form', 'aria-label': t('admin.user_search') }, field(t('admin.user_search'), search, { inline: true }), button(t('search.submit'), () => undefined, { type: 'submit' }));
  sf.addEventListener('submit', (ev) => { ev.preventDefault(); navigate(`/admin/users${qs({ q: search.value })}`); });
  const name = (ids: number[], all: { id: number; name: string }[]) => ids.map((id) => all.find((x) => x.id === id)?.name ?? `#${id}`).join(', ');
  mount(panel, pageHeader(t('admin.users'), sharedEditable() ? button(t('admin.user_create'), () => userDialog(null, gs, rs), { variant: 'primary' }) : null), sharedNotice(), sf,
    table(t('admin.users'), [t('admin.handle'), t('profile.display_name'), t('admin.groups'), t('admin.roles'), t('admin.last_login'), t('common.status'), t('common.actions')],
      list.map((u) => h('tr', null,
        h('th', { scope: 'row' }, u.handle, u.totp_enabled || u.mfa_email ? h('span', { class: 'ew-badge ew-badge--ok' }, ' 2FA') : null), h('td', null, u.display_name), h('td', null, name(u.groups, gs)), h('td', null, name(u.roles, rs)),
        h('td', null, u.last_login_at ? fmtDate(u.last_login_at, state.lang) : '–'),
        h('td', null, u.disabled ? h('span', { class: 'ew-badge ew-badge--warn' }, t('admin.disabled')) : (u.locked_until && new Date(u.locked_until) > new Date() ? h('span', { class: 'ew-badge ew-badge--warn' }, t('admin.locked')) : h('span', { class: 'ew-badge ew-badge--ok' }, t('admin.active')))),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), async () => {
            try { userDialog(await api.get<User>(`admin/users/${u.id}`), gs, rs); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.edit_named', { name: u.handle }) }),
          button(t('profile.card_title'), async () => {
            try {
              openDialog(`${t('profile.card_title')}: ${u.display_name || u.handle}`, await profileCardForm(`admin/users/${u.id}/profile`, u.display_name || u.handle), [], { wide: true });
            } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('profile.card_title')}: ${u.handle}` }),
          u.id !== state.user?.id ? button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('admin.user_delete_confirm', { name: u.handle }), t('common.delete'), true))) return;
            try { await api.del(`admin/users/${u.id}`); toast(t('admin.deleted')); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.delete_named', { name: u.handle }) }) : null))), t('admin.no_users')));
}

function membership(label: string, all: { id: number; name: string }[], selected: number[]): { el: HTMLElement; get: () => number[] } {
  const boxes = all.map((x) => ({ id: x.id, cb: checkbox(x.name, selected.includes(x.id)) }));
  return { el: h('fieldset', null, h('legend', null, label), ...(boxes.length ? boxes.map((b) => b.cb.el) : [h('p', { class: 'ew-muted' }, '–')])), get: () => boxes.filter((b) => b.cb.input.checked).map((b) => b.id) };
}

function userDialog(u: User | null, gs: Group[], rs: Role[]): void {
  const username = textInput({ value: u?.username ?? '', required: true, autocomplete: 'off' });
  const display = textInput({ value: u?.display_name ?? '' });
  const email = textInput({ value: u?.email ?? '', type: 'email' });
  const pw = textInput({ type: 'password', autocomplete: 'new-password' });
  const g = membership(t('admin.groups'), gs, u?.groups ?? []);
  const r = membership(t('admin.roles'), rs, u?.roles ?? []);
  const disabled = checkbox(t('admin.disabled'), u?.disabled ?? false);
  const unlock = checkbox(t('admin.unlock'), false);
  const resetTotp = checkbox(t('admin.reset_totp'), false, { help: t('admin.reset_totp_help') });
  const nets = textArea({ value: u?.networks.join('\n') ?? '', rows: 2 });
  const shared = sharedEditable();
  const fields = u && !shared
    ? [h('p', null, h('strong', null, u.handle), ' – ', u.display_name), sharedNotice(), r.el]
    : u
    ? [h('p', null, h('strong', null, u.handle)), field(t('profile.display_name'), display), field(t('profile.email'), email),
      u.provider === 'local' ? field(t('admin.new_password'), pw, { help: t('admin.new_password_help') }) : null, g.el, r.el, disabled.el, unlock.el,
      u.totp_enabled || u.mfa_email ? resetTotp.el : null,
      field(t('mfa.networks'), nets, { help: t('mfa.networks_admin_help') })]
    : [field(t('auth.username'), username, { required: true }), field(t('profile.display_name'), display), field(t('profile.email'), email),
      field(t('auth.password'), pw, { required: true, help: t('profile.password_rules') }), g.el, r.el];
  formDialog(u ? t('admin.user_edit') : t('admin.user_create'), fields, t('common.save'), async () => {
    if (u && !shared) {
      await api.put(`admin/users/${u.id}`, { roles: r.get() });
    } else if (u) {
      await api.put(`admin/users/${u.id}`, { display_name: display.value, email: email.value, groups: g.get(), roles: r.get(), disabled: disabled.input.checked, unlock: unlock.input.checked, password: pw.value || undefined, reset_totp: resetTotp.input.checked,
        networks: nets.value.split(/[\s,;]+/).filter(Boolean) });
    } else {
      await api.post('admin/users', { username: username.value, password: pw.value, display_name: display.value, email: email.value, groups: g.get(), roles: r.get() });
    }
    toast(t('common.saved'));
    reload();
  }, { wide: true });
}

// ---------------------------------------------------------------- groups & roles

async function groups(panel: HTMLElement): Promise<void> {
  const [gs, rs] = await Promise.all([api.get<Group[]>('groups'), api.get<Role[]>('roles')]);
  const dialog = (g: Group | null) => {
    const name = textInput({ value: g?.name ?? '', required: true });
    const desc = textInput({ value: g?.description ?? '' });
    const r = membership(t('admin.roles'), rs, g?.roles ?? []);
    formDialog(g ? t('admin.group_edit') : t('admin.group_create'), [field(t('common.name'), name, { required: true }), field(t('common.description'), desc), r.el], t('common.save'), async () => {
      const body = { name: name.value, description: desc.value, roles: r.get() };
      if (g) await api.put(`admin/groups/${g.id}`, body);
      else await api.post('admin/groups', body);
      toast(t('common.saved'));
      reload();
    });
  };
  mount(panel, pageHeader(t('admin.groups'), sharedEditable() ? button(t('admin.group_create'), () => dialog(null), { variant: 'primary' }) : null), sharedNotice(), h('p', null, t('admin.groups_help')),
    table(t('admin.groups'), [t('common.name'), t('common.description'), t('admin.source'), t('admin.members'), t('admin.roles'), t('common.actions')],
      gs.map((g) => h('tr', null, h('th', { scope: 'row' }, g.name), h('td', null, g.description), h('td', null, g.source), h('td', null, String(g.members)),
        h('td', null, g.roles.map((id) => rs.find((r) => r.id === id)?.name ?? id).join(', ')),
        h('td', { class: 'ew-actions' },
          button(t('admin.show_members'), async () => {
            try {
              const [ids, us] = await Promise.all([api.get<number[]>(`admin/groups/${g.id}/members`), api.get<User[]>('admin/users?limit=500')]);
              const names = ids.map((id) => us.find((u) => u.id === id)?.handle ?? `#${id}`);
              const d = openDialog(t('admin.members_of', { name: g.name }), names.length ? h('ul', null, ...names.map((n) => h('li', null, n))) : h('p', null, t('admin.no_members')),
                [button(t('common.close'), () => d.close(), { variant: 'primary' })]);
            } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.members_of', { name: g.name }) }),
          button(t('common.edit'), () => dialog(g), { variant: 'link', ariaLabel: t('admin.edit_named', { name: g.name }) }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('admin.group_delete_confirm', { name: g.name }), t('common.delete'), true))) return;
            try { await api.del(`admin/groups/${g.id}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.delete_named', { name: g.name }) })))), t('admin.no_groups')));
}

async function roles(panel: HTMLElement): Promise<void> {
  const [rs, cat, defs] = await Promise.all([api.get<Role[]>('roles'), loadCatalog(), api.get<{ authenticated: string[] }>('admin/permissions')]);
  const mine = isAdmin() ? null : (state.user?.permissions ?? []);
  const label = (k: string) => { const d = cat.permissions.find((x) => x.key === k); return d ? permLabel(d) : k; };
  const dialog = async (r: Role | null) => {
    const name = textInput({ value: r?.name ?? '', required: true });
    const desc = textInput({ value: r?.description ?? '' });
    const adm = checkbox(t('admin.is_admin'), r?.is_admin ?? false, { help: t('admin.is_admin_help') });
    if (!isAdmin()) adm.input.disabled = true;
    const perms = await globalEditor(r?.permissions ?? [], mine);
    const permBox = h('fieldset', null, h('legend', null, t('admin.role_permissions')), h('p', { class: 'ew-help' }, t('admin.role_permissions_help')), perms.el);
    const sync = () => { permBox.hidden = adm.input.checked; };
    adm.input.addEventListener('change', sync);
    sync();
    formDialog(r ? t('admin.role_edit') : t('admin.role_create'), [field(t('common.name'), name, { required: true }), field(t('common.description'), desc), adm.el, permBox], t('common.save'), async () => {
      const body = { name: name.value, description: desc.value, is_admin: adm.input.checked, permissions: perms.value() };
      if (r) await api.put(`admin/roles/${r.id}`, body);
      else await api.post('admin/roles', body);
      toast(t('common.saved'));
      reload();
    });
  };
  const defaultsDialog = async () => {
    const ed = await globalEditor(defs.authenticated, mine);
    formDialog(t('admin.default_permissions'), [h('p', null, t('admin.default_permissions_help')), ed.el], t('common.save'), async () => {
      await api.put('admin/permissions/authenticated', { permissions: ed.value() });
      toast(t('common.saved'));
      reload();
    });
  };
  mount(panel, pageHeader(t('admin.roles'), sharedEditable() ? button(t('admin.role_create'), () => void dialog(null), { variant: 'primary' }) : null), sharedNotice(), h('p', null, t('admin.roles_help')),
    table(t('admin.roles'), [t('common.name'), t('common.description'), t('admin.role_permissions'), t('admin.members'), t('common.actions')],
      rs.map((r) => h('tr', null, h('th', { scope: 'row' }, r.name), h('td', null, r.description),
        h('td', null, r.is_admin ? h('strong', null, t('admin.is_admin')) : (r.permissions.map(label).join(', ') || '–')), h('td', null, String(r.members)),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => void dialog(r), { variant: 'link', ariaLabel: t('admin.edit_named', { name: r.name }) }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('admin.role_delete_confirm', { name: r.name }), t('common.delete'), true))) return;
            try { await api.del(`admin/roles/${r.id}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.delete_named', { name: r.name }) })))), t('admin.no_roles')),
    h('section', { class: 'ew-panel', 'aria-labelledby': 'defperm-h' }, h('h2', { id: 'defperm-h' }, t('admin.default_permissions')),
      h('p', null, t('admin.default_permissions_help')),
      h('p', null, defs.authenticated.map(label).join(', ') || '–'),
      button(t('common.edit'), () => void defaultsDialog(), { variant: 'secondary', ariaLabel: `${t('common.edit')}: ${t('admin.default_permissions')}` })));
}

// ---------------------------------------------------------------- plugins

interface PluginInfo { id: string; name: string; version: string; description: string; author: string; kinds: string[]; extends: string[]; required: boolean; enabled: boolean; active: boolean; missing: string[]; dependents: string[] }
interface PluginsResponse {
  installed: PluginInfo[];
  catalog: { id: string; name: string; repo: string; ref?: string; description?: string }[];
  pending: { id: string; action: string; version: string }[];
  build: { running: boolean; ok: boolean; started_at: string | null; finished_at: string | null; log: string };
  can_build: boolean;
  build_unavailable: '' | 'lite' | 'toolchain';
  mode: 'source' | 'full' | 'lite';
  plugins_dir: string;
  restart_pending: boolean;
  auto_restart_on_drop: boolean;
  cluster: boolean;
  removable: string[];
}

/** waitForRestart reloads the page once the server answers again after a restart. */
function waitForRestart(): void {
  let seenDown = false;
  const tick = async () => {
    try {
      const r = await fetch('/readyz', { cache: 'no-store' });
      if (r.ok && seenDown) { location.reload(); return; }
      if (!r.ok) seenDown = true;
    } catch { seenDown = true; }
    setTimeout(() => void tick(), 2000);
  };
  setTimeout(() => void tick(), 1000);
}

/** followBuild polls the build of a plugin change; it restarts automatically when it succeeded. */
function followBuild(reload: () => void): void {
  const tick = async () => {
    try {
      const d = await api.get<PluginsResponse>('admin/plugins');
      if (d.build.running) { setTimeout(() => void tick(), 3000); return; }
      if (!d.build.ok) { toast(t('plugins.build_failed'), 'error'); reload(); return; }
      toast(t('plugins.restarting'));
      waitForRestart();
    } catch { waitForRestart(); } // the server is already restarting
  };
  setTimeout(() => void tick(), 2000);
}

/** Makes a template the active one (ui.template in config.yaml). */
async function useTemplate(id: string): Promise<void> {
  try {
    const s = await api.get<Settings>('admin/settings');
    await api.put('admin/settings', { ui: { ...s.config.ui, template: id } });
    toast(t('plugins.template_switched', { name: templateLabel(id) }));
    setTimeout(() => location.reload(), 600);
  } catch (e) { toast(errorText(e), 'error'); }
}

async function plugins(panel: HTMLElement): Promise<void> {
  const data = await api.get<PluginsResponse>('admin/plugins');
  const installedIds = new Set(data.installed.map((p) => p.id));
  const toggle = async (p: PluginInfo, on: boolean) => {
    try {
      const id = encodeURIComponent(p.id);
      if (on) await api.post(`admin/plugins/${id}/enable`);
      else await api.post(`admin/plugins/${id}/disable`);
      toast(t(on ? 'plugins.enabled_msg' : 'plugins.disabled_msg', { name: p.name }));
      setTimeout(() => location.reload(), 600); // reload frontend so plugin UI parts follow
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const install = async (source: string, ref = '') => {
    try {
      const res = await api.post<{ id: string; version: string; upgrade: boolean; building: boolean }>('admin/plugins/install', { source, ref });
      if (res.building) {
        toast(t(res.upgrade ? 'plugins.upgraded_building' : 'plugins.installed_building', { id: res.id, version: res.version }));
        followBuild(reload);
        reload();
        return;
      }
      toast(t(res.upgrade ? 'plugins.upgraded' : 'plugins.installed', { id: res.id, version: res.version }));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const lite = data.build_unavailable === 'lite';
  const removable = new Set(data.removable);
  const src = textInput({ placeholder: 'github.com/owner/repo' });
  const ref = textInput({ placeholder: 'v1.0.0' });
  const inf = h('form', { class: 'ew-form ew-panel', novalidate: true }, h('h2', null, t('plugins.install_title')),
    lite ? h('p', { class: 'ew-notice ew-notice--info', id: 'lite-hint' }, t('plugins.lite_hint')) : h('p', null, t('plugins.install_help')),
    h('fieldset', { class: 'ew-fieldset-plain', disabled: lite, 'aria-describedby': lite ? 'lite-hint' : undefined },
      h('legend', { class: 'sr-only' }, t('plugins.install_title')),
      field(t('plugins.source'), src, { required: true, help: t('plugins.source_help') }), field(t('plugins.ref'), ref),
      h('div', { class: 'ew-form-actions' }, button(t('plugins.install'), () => undefined, { type: 'submit', variant: 'primary' }))));
  inf.addEventListener('submit', (ev) => { ev.preventDefault(); if (src.value.trim()) void install(src.value.trim(), ref.value.trim()); });
  const b = data.build;
  const autoDrop = checkbox(t('plugins.auto_restart_on_drop'), data.auto_restart_on_drop, { help: t('plugins.auto_restart_on_drop_help') });
  autoDrop.input.addEventListener('change', async () => {
    try { await api.put('admin/plugins/options', { auto_restart_on_drop: autoDrop.input.checked }); toast(t('common.saved')); } catch (e) { toast(errorText(e), 'error'); }
  });
  const restartBtn = () => button(t('plugins.restart'), async () => {
    if (!(await confirmDialog(t(data.cluster ? 'plugins.restart_confirm_cluster' : 'plugins.restart_confirm'), t('plugins.restart')))) return;
    try { await api.post('admin/restart'); toast(t('plugins.restarting')); waitForRestart(); } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: data.restart_pending ? 'primary' : 'secondary' });
  const buildBox = h('section', { class: 'ew-panel', 'aria-labelledby': 'build-h' }, h('h2', { id: 'build-h' }, t('plugins.build_title')),
    data.can_build ? h('p', { class: 'ew-help' }, t(`plugins.mode.${data.mode}`), data.cluster && data.mode === 'full' ? ' ' + t('plugins.mode_cluster') : '') : null,
    data.restart_pending && !b.running ? h('p', { class: 'ew-notice ew-notice--warning', role: 'status' }, t('plugins.restart_pending')) : null,
    data.pending.length ? h('div', null, h('p', null, t('plugins.pending')), h('ul', null, ...data.pending.map((c) => h('li', null, `${t('plugins.action.' + c.action)}: ${c.id} ${c.version}`)))) : h('p', null, t('plugins.no_pending')),
    data.can_build
      ? h('div', { class: 'ew-actions' },
        button(t('plugins.build'), async () => { try { await api.post('admin/plugins/build'); toast(t('plugins.build_started')); setTimeout(reload, 1500); } catch (e) { toast(errorText(e), 'error'); } }, { variant: data.restart_pending ? 'secondary' : 'primary', disabled: b.running }),
        data.restart_pending || b.ok ? restartBtn() : null)
      : h('p', { class: 'ew-help' }, t(lite ? 'plugins.lite_hint' : 'plugins.cannot_build')),
    data.can_build ? h('div', null, autoDrop.el, h('p', { class: 'ew-help' }, t('plugins.drop_help', { dir: data.plugins_dir }))) : null,
    b.started_at ? h('p', { role: 'status' }, b.running ? t('plugins.build_running') : (b.ok ? t('plugins.build_ok') : t('plugins.build_failed'))) : null,
    b.log ? h('details', null, h('summary', null, t('plugins.build_log')), h('pre', { class: 'ew-code', tabindex: '0' }, b.log)) : null);
  mount(panel, pageHeader(t('admin.plugins')), h('p', null, t('plugins.help')),
    table(t('admin.plugins'), [t('common.name'), t('plugins.version'), t('plugins.kinds'), t('plugins.extends'), t('common.status'), t('common.actions')],
      data.installed.map((p) => h('tr', null,
        h('th', { scope: 'row' }, h('span', null, p.name), h('br'), h('code', null, p.id), h('p', { class: 'ew-help' }, p.description)),
        h('td', null, p.version), h('td', null, p.kinds.map((k) => t(`plugins.kind.${k}`)).join(', ')),
        h('td', null, p.extends.join(', ') || '–', p.missing.length ? h('p', { class: 'ew-field-error' }, t('plugins.missing', { ids: p.missing.join(', ') })) : null),
        h('td', null, p.required ? h('span', { class: 'ew-badge ew-badge--info' }, t('plugins.required')) : null, ' ',
          p.active ? h('span', { class: 'ew-badge ew-badge--ok' }, t('plugins.active')) : h('span', { class: 'ew-badge ew-badge--warn' }, p.enabled ? t('plugins.inactive_deps') : t('plugins.inactive')),
          p.kinds.includes('template') ? h('p', { class: 'ew-help' }, p.id === state.site.template
            ? h('strong', null, t('plugins.template_in_use')) : t('plugins.template_available')) : null),
        h('td', { class: 'ew-actions' },
          p.kinds.includes('template') && p.active && p.id !== state.site.template && listTemplates().some((x) => x.id === p.id)
            ? button(t('plugins.use_template'), () => void useTemplate(p.id), { variant: 'link', ariaLabel: `${t('plugins.use_template')}: ${p.name}` }) : null,
          p.required ? null : (p.enabled
            ? button(t('plugins.disable'), () => void toggle(p, false), { variant: 'link', ariaLabel: t('plugins.disable_named', { name: p.name }) })
            : button(t('plugins.enable'), () => void toggle(p, true), { variant: 'link', ariaLabel: t('plugins.enable_named', { name: p.name }) })),
          p.required || !removable.has(p.id) ? null : button(t('plugins.remove'), async () => {
            if (!(await confirmDialog(t('plugins.remove_confirm', { name: p.name }), t('plugins.remove'), true))) return;
            try {
              const res = await api.del<{ building?: boolean }>(`admin/plugins/${encodeURIComponent(p.id)}`);
              if (res?.building) { toast(t('plugins.removed_building')); followBuild(reload); } else toast(t('plugins.removed'));
              reload();
            } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('plugins.remove_named', { name: p.name }) })))), '–'),
    buildBox,
    data.catalog.length ? h('section', { class: 'ew-section', 'aria-labelledby': 'cat-h' }, h('h2', { id: 'cat-h' }, t('plugins.catalog')),
      table(t('plugins.catalog'), [t('common.name'), t('common.description'), t('plugins.source'), t('common.actions')],
        data.catalog.map((c) => h('tr', null, h('th', { scope: 'row' }, c.name, h('br'), h('code', null, c.id)), h('td', null, c.description ?? ''), h('td', null, c.repo),
          h('td', null, button(installedIds.has(c.id) ? t('plugins.upgrade') : t('plugins.install'), () => void install(c.id), { variant: 'link', ariaLabel: `${t('plugins.install')} ${c.name}`, disabled: lite })))), '–')) : null,
    inf);
}

// ---------------------------------------------------------------- updates

interface UpdateInfo { id: string; name: string; repo: string; current: string; latest?: string; tag?: string; url?: string; notes?: string; available: boolean; error?: string }

async function updates(panel: HTMLElement, force: boolean): Promise<void> {
  const rep = await api.get<{ checked_at: string | null; items: UpdateInfo[] }>(`admin/updates${force ? '?force=1' : ''}`);
  const check = button(t('updates.check'), async () => {
    check.disabled = true;
    try { await updates(panel, true); announceDone(); } catch (e) { toast(errorText(e), 'error'); check.disabled = false; }
  }, { variant: 'primary' });
  const announceDone = () => toast(t('updates.checked'));
  const update = async (it: UpdateInfo) => {
    if (!(await confirmDialog(t('updates.confirm', { name: it.name, version: it.latest ?? '' }), t('updates.update')))) return;
    try {
      await api.post(`admin/updates/${encodeURIComponent(it.id)}`);
      toast(t('updates.installing', { name: it.name }));
      navigate('/admin/plugins');
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const avail = rep.items.filter((i) => i.available).length;
  mount(panel, pageHeader(t('admin.updates'), check), h('p', null, t('updates.help')),
    h('p', { role: 'status' }, rep.checked_at ? t('updates.checked_at', { date: fmtDate(rep.checked_at, state.lang), count: avail }) : t('updates.never')),
    table(t('admin.updates'), [t('common.name'), t('updates.installed'), t('updates.latest'), t('common.status'), t('common.actions')],
      rep.items.map((it) => h('tr', null,
        h('th', { scope: 'row' }, it.name, h('br'), h('code', null, it.id === 'core' ? it.repo : it.id)),
        h('td', null, it.current), h('td', null, it.url ? h('a', { href: it.url, rel: 'external noopener', target: '_blank' }, it.latest ?? it.tag ?? '') : (it.latest ?? '–')),
        h('td', null, it.error ? h('span', { class: 'ew-badge ew-badge--warn' }, it.error)
          : it.available ? h('span', { class: 'ew-badge ew-badge--info' }, t('updates.available')) : h('span', { class: 'ew-badge ew-badge--ok' }, t('updates.current')),
          it.notes && it.available ? h('details', null, h('summary', null, t('updates.notes')), h('pre', { class: 'ew-code', tabindex: '0' }, it.notes)) : null),
        h('td', { class: 'ew-actions' }, it.available && it.id !== 'core' ? button(t('updates.update'), () => void update(it), { variant: 'primary', ariaLabel: t('updates.update_named', { name: it.name }) })
          : it.available ? h('span', { class: 'ew-help' }, t('updates.core_hint')) : null))), t('updates.none')));
}

// ---------------------------------------------------------------- settings

interface Settings {
  config: {
    ui: { site_name: string; template: string; home_category: number; home_element: number; prerender: boolean; preview_image: string; readable_urls: boolean };
    i18n: { default_language: string; languages: string[] };
    auth: { order: string[]; session_ttl: string; api_key_max_ttl: string; ssh_token_ttl: string; max_login_failures: number; lockout_duration: string; min_password_length: number };
    retention: { revisions_keep_days: number; revisions_keep_min: number; trash_days: number; audit_days: number };
    mode: string;
    server: { listen: string; base_url: string };
  };
  path: string;
}

async function settings(panel: HTMLElement): Promise<void> {
  const s = await api.get<Settings>('admin/settings');
  const c = s.config;
  const num = (v: number) => textInput({ type: 'number', value: String(v) });
  const siteName = textInput({ value: c.ui.site_name, required: true });
  const tpls = listTemplates().filter((x) => x.layout || x.extends);
  const tpl = select(tpls.map((x) => ({ value: x.id, label: `${templateLabel(x.id)} (${x.id})` })), c.ui.template);
  const homeMode = select(['overview', 'category', 'element'].map((v) => ({ value: v, label: t(`settings.home_${v}`) })),
    c.ui.home_element ? 'element' : c.ui.home_category ? 'category' : 'overview');
  const homeCat = await categorySelect({ selected: c.ui.home_category, perm: 'view' });
  const homeEl = await elementSelect({ selected: c.ui.home_element, allowCreate: true });
  const fHomeCat = homeCat.field(t('settings.home_category'));
  const fHomeEl = h('fieldset', { class: 'ew-subfield' }, h('legend', null, t('settings.home_element')), homeEl.el);
  const syncHome = () => { fHomeCat.hidden = homeMode.value !== 'category'; fHomeEl.hidden = homeMode.value !== 'element'; };
  homeMode.addEventListener('change', syncHome);
  syncHome();
  const prerender = checkbox(t('settings.prerender'), c.ui.prerender, { help: t('settings.prerender_help') });
  const readable = checkbox(t('settings.readable_urls'), c.ui.readable_urls, { help: t('settings.readable_urls_help') });
  const sitePreview = previewImageInput(c.ui.preview_image ?? '', { label: t('settings.preview_image'), help: t('settings.preview_image_help') });
  const guestPath = textInput({ value: '/' });
  const guestView = h('div', null, button(t('settings.guest_view'), async () => {
    try {
      const r = await api.get<{ available: boolean; page?: { title: string; description: string; canonical: string; lang: string; html: string } }>(`prerender?path=${encodeURIComponent(guestPath.value || '/')}`);
      const p = r.page;
      openDialog(t('settings.guest_view'), p
        ? h('div', null,
          h('dl', { class: 'ew-dl' }, h('dt', null, t('settings.guest_title')), h('dd', null, p.title || state.site.name), h('dt', null, t('settings.guest_description')), h('dd', null, p.description || '–'),
            h('dt', null, t('settings.guest_canonical')), h('dd', null, p.canonical), h('dt', null, t('lang.label')), h('dd', null, langName(p.lang))),
          h('pre', { class: 'ew-code', tabindex: '0' }, p.html))
        : h('p', null, t('settings.guest_unavailable')), [], { wide: true });
    } catch (e) { toast(errorText(e), 'error'); }
  }));
  const defLang = select(c.i18n.languages.map((l) => ({ value: l, label: langName(l) })), c.i18n.default_language);
  const order = textInput({ value: c.auth.order.join(', ') });
  const sessionTTL = textInput({ value: c.auth.session_ttl });
  const keyTTL = textInput({ value: c.auth.api_key_max_ttl });
  const sshTTL = textInput({ value: c.auth.ssh_token_ttl });
  const fails = num(c.auth.max_login_failures);
  const lockout = textInput({ value: c.auth.lockout_duration });
  const minPw = num(c.auth.min_password_length);
  const keepDays = num(c.retention.revisions_keep_days);
  const keepMin = num(c.retention.revisions_keep_min);
  const trashDays = num(c.retention.trash_days);
  const auditDays = num(c.retention.audit_days);
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const fs = (legend: string, ...kids: HTMLElement[]) => h('fieldset', null, h('legend', null, legend), ...kids);
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    fs(t('settings.site'), field(t('settings.site_name'), siteName, { required: true }), field(t('settings.template'), tpl, { help: t('settings.template_help') }), field(t('settings.home'), homeMode, { help: t('settings.home_help') }), fHomeCat, fHomeEl,
      field(t('settings.default_language'), defLang, { help: t('settings.languages_help') }), prerender.el, readable.el, sitePreview.el, field(t('settings.guest_view_path'), guestPath, { help: t('settings.guest_view_help') }), guestView),
    fs(t('settings.auth'), field(t('settings.auth_order'), order, { help: t('settings.auth_order_help') }), field(t('settings.session_ttl'), sessionTTL, { help: t('settings.duration_help') }),
      field(t('settings.api_key_max_ttl'), keyTTL), field(t('settings.ssh_token_ttl'), sshTTL), field(t('settings.max_login_failures'), fails), field(t('settings.lockout_duration'), lockout), field(t('settings.min_password_length'), minPw)),
    fs(t('settings.retention'), h('p', { class: 'ew-help' }, t('settings.retention_help')), field(t('settings.revisions_keep_days'), keepDays, { help: t('settings.zero_forever') }), field(t('settings.revisions_keep_min'), keepMin),
      field(t('settings.trash_days'), trashDays, { help: t('settings.zero_never') }), field(t('settings.audit_days'), auditDays, { help: t('settings.zero_forever') })),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' }),
      button(t('settings.run_retention'), async () => {
        try { const r = await api.post<{ revisions: number; trash: number; audit: number }>('admin/retention/run'); toast(t('settings.retention_done', r)); } catch (e) { toast(errorText(e), 'error'); }
      })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put('admin/settings', {
        ui: { site_name: siteName.value, template: tpl.value, home_category: homeMode.value === 'category' ? homeCat.value() : 0, home_element: homeMode.value === 'element' ? homeEl.value() : 0, prerender: prerender.input.checked, readable_urls: readable.input.checked, preview_image: sitePreview.value() },
        i18n: { default_language: defLang.value, languages: c.i18n.languages },
        auth: { order: order.value.split(',').map((x) => x.trim()).filter(Boolean), session_ttl: sessionTTL.value, api_key_max_ttl: keyTTL.value, ssh_token_ttl: sshTTL.value, max_login_failures: Number(fails.value), lockout_duration: lockout.value, min_password_length: Number(minPw.value) },
        retention: { revisions_keep_days: Number(keepDays.value), revisions_keep_min: Number(keepMin.value), trash_days: Number(trashDays.value), audit_days: Number(auditDays.value) },
      });
      toast(t('settings.saved'));
      setTimeout(() => location.reload(), 800);
    } catch (e) { alert.textContent = errorText(e); }
  });
  const db = await api.get<DatabaseStatus>('admin/database').catch(() => null);
  mount(panel, pageHeader(t('admin.settings')), h('p', null, t('settings.file_note', { path: s.path })),
    db ? databaseFacts(db) : null,
    h('dl', { class: 'ew-dl' }, h('dt', null, t('settings.mode')), h('dd', null, c.mode), h('dt', null, t('settings.listen')), h('dd', null, c.server.listen), h('dt', null, t('settings.base_url')), h('dd', null, c.server.base_url)),
    form);
}

// ---------------------------------------------------------------- e-mail

interface MailConfig {
  enabled: boolean | null; transport: string; from: string; from_name: string; reply_to: string; allowed_from: string[] | null; subject_prefix: string; sendmail: string;
  concurrency: number; rate_per_minute: number; digest_hour: number; digest_weekday: number; digest_timezone: string;
  smtp: { host: string; port: number; security: string; username: string; password: string; skip_verify: boolean; helo_name: string; ca_cert?: string };
  bounce: { address: string; verp: boolean; imap_host: string; imap_port: number; imap_security: string; imap_username: string; imap_password: string; imap_folder: string; delete_processed: boolean; skip_verify: boolean; ca_cert?: string };
}
interface MailStatus { enabled: boolean; transport: string; from: string; counts: Record<string, number>; recipients: Record<string, number>; oldest_pending: string | null; bounces: boolean; problem?: string }
interface MailRecipient { id: number; addr: string; kind: string; status: string; attempts: number; last_code: number; last_error: string; next_attempt_at: string; sent_at: string | null; failed_at: string | null; bounced: boolean }
interface OutboxEntry {
  id: number; status: string; from: string; to: string[]; cc: string[]; subject: string; body: string; source: string; ref: string; attempts: number; last_error: string;
  created_at: string; sent_at: string | null; next_attempt_at: string; owner: string | null; pending: number; delivered: number; failed: number; recipients?: MailRecipient[];
}

const mailBadge = (x: string) => h('span', { class: ['ew-badge', x === 'sent' ? 'ew-badge--ok' : x === 'failed' || x === 'held' || x === 'partial' ? 'ew-badge--warn' : 'ew-badge--info'] }, t(`mail.status.${x}`));

async function mailMessageDialog(id: number, reload: () => void): Promise<void> {
  const o = await api.get<OutboxEntry>(`admin/mail/outbox/${id}`);
  const d = openDialog(o.subject, h('div', null,
    h('dl', { class: 'ew-dl' }, h('dt', null, t('mail.from')), h('dd', null, o.from), h('dt', null, t('common.status')), h('dd', null, mailBadge(o.status)),
      h('dt', null, t('mail.source')), h('dd', null, `${o.source} ${o.ref}`), h('dt', null, t('mail.owner')), h('dd', null, o.owner ?? '–'),
      h('dt', null, t('mail.created')), h('dd', null, fmtDate(o.created_at, state.lang))),
    table(t('mail.recipients'), [t('mail.to'), t('common.status'), t('mail.attempts_col'), t('mail.last_answer')],
      (o.recipients ?? []).map((r) => h('tr', null, h('th', { scope: 'row' }, r.addr, r.kind === 'cc' ? ' (Cc)' : ''),
        h('td', null, h('span', { class: ['ew-badge', r.status === 'sent' ? 'ew-badge--ok' : r.status === 'failed' ? 'ew-badge--warn' : 'ew-badge--info'] }, t(`mail.rstatus.${r.status}`)),
          r.bounced ? ` (${t('mail.bounced')})` : ''),
        h('td', null, String(r.attempts), r.status === 'pending' ? h('div', { class: 'ew-help' }, t('mail.next_try', { date: fmtDate(r.next_attempt_at, state.lang) })) : null),
        h('td', null, r.last_code ? `${r.last_code} ` : '', r.last_error || (r.sent_at ? fmtDate(r.sent_at, state.lang) : '–')))), '–'),
    h('h3', null, t('mail.body')), h('pre', { class: 'ew-code ew-mail-body', tabindex: '0' }, o.body)), [
    o.failed > 0 || o.status === 'held' || o.pending > 0 ? button(t('mail.retry'), async () => {
      try { await api.post(`admin/mail/outbox/${o.id}/retry`); toast(t('mail.requeued')); d.close(); reload(); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'primary' }) : null,
  ], { wide: true });
}

async function mailAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const filter = query.get('status') ?? '';
  const q = query.get('q') ?? '';
  const [cfg, st, outbox, jobs] = await Promise.all([
    api.get<{ config: { mail: MailConfig } }>('admin/settings'), api.get<MailStatus>('admin/mail'),
    api.get<OutboxEntry[]>(`admin/mail/outbox${qs({ status: filter, q, limit: 100 })}`), api.get<string[]>('admin/jobs')]);
  const m = cfg.config.mail;
  const enabled = checkbox(t('mail.enabled'), m.enabled !== false, { help: t('mail.enabled_help') });
  const transport = select(['auto', 'smtp', 'sendmail', 'log', 'none'].map((x) => ({ value: x, label: t(`mail.transport.${x}`) })), m.transport || 'auto');
  const from = textInput({ value: m.from, type: 'email', placeholder: 'wiki@example.com' });
  const fromName = textInput({ value: m.from_name });
  const replyTo = textInput({ value: m.reply_to, type: 'email' });
  const prefix = textInput({ value: m.subject_prefix, placeholder: '[Wiki]' });
  const allowed = textArea({ value: (m.allowed_from ?? []).join('\n'), rows: 3 });
  const conc = textInput({ value: m.concurrency ? String(m.concurrency) : '', type: 'number', placeholder: '4' });
  const rate = textInput({ value: m.rate_per_minute ? String(m.rate_per_minute) : '', type: 'number', placeholder: '0' });
  const host = textInput({ value: m.smtp.host, placeholder: 'smtp.example.com' });
  const port = textInput({ value: m.smtp.port ? String(m.smtp.port) : '', type: 'number', placeholder: '587' });
  const security = select(['starttls', 'tls', 'none'].map((x) => ({ value: x, label: t(`mail.security.${x}`) })), m.smtp.security || 'starttls');
  const user = textInput({ value: m.smtp.username, autocomplete: 'off' });
  const pw = textInput({ value: m.smtp.password, type: 'password', autocomplete: 'new-password' });
  const skip = checkbox(t('mail.skip_verify'), m.smtp.skip_verify, { help: t('mail.skip_verify_help') });
  const ca = textArea({ value: m.smtp.ca_cert ?? '', rows: 3 });
  const helo = textInput({ value: m.smtp.helo_name });
  const sendmail = textInput({ value: m.sendmail, placeholder: '/usr/sbin/sendmail' });
  const b = m.bounce ?? { address: '', verp: false, imap_host: '', imap_port: 0, imap_security: 'tls', imap_username: '', imap_password: '', imap_folder: '', delete_processed: false, skip_verify: false };
  const bAddr = textInput({ value: b.address, type: 'email', placeholder: 'bounces@example.com' });
  const bVerp = checkbox(t('mail.bounce_verp'), b.verp, { help: t('mail.bounce_verp_help') });
  const bHost = textInput({ value: b.imap_host, placeholder: 'imap.example.com' });
  const bPort = textInput({ value: b.imap_port ? String(b.imap_port) : '', type: 'number', placeholder: '993' });
  const bSec = select(['tls', 'starttls', 'none'].map((x) => ({ value: x, label: t(`mail.security.${x}`) })), b.imap_security || 'tls');
  const bUser = textInput({ value: b.imap_username, autocomplete: 'off' });
  const bPw = textInput({ value: b.imap_password, type: 'password', autocomplete: 'new-password' });
  const bFolder = textInput({ value: b.imap_folder, placeholder: 'INBOX' });
  const bDel = checkbox(t('mail.bounce_delete'), b.delete_processed);
  const dHour = select(Array.from({ length: 24 }, (_, i) => ({ value: String(i), label: `${String(i).padStart(2, '0')}:00` })), String(m.digest_hour ?? 7));
  const dDay = select([1, 2, 3, 4, 5, 6, 0].map((d) => ({ value: String(d), label: new Date(2024, 0, 7 + d).toLocaleDateString(state.lang, { weekday: 'long' }) })), String(m.digest_weekday ?? 1));
  const dTZ = textInput({ value: m.digest_timezone ?? '', placeholder: Intl.DateTimeFormat().resolvedOptions().timeZone });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const fs = (legend: string, ...kids: (HTMLElement | null)[]) => h('fieldset', null, h('legend', null, legend), ...kids);
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    fs(t('mail.sender'), enabled.el, field(t('mail.transport'), transport, { help: t('mail.transport_help') }), field(t('mail.from'), from, { help: t('mail.from_help') }),
      field(t('mail.from_name'), fromName), field(t('mail.reply_to'), replyTo), field(t('mail.subject_prefix'), prefix),
      field(t('mail.allowed_from'), allowed, { help: t('mail.allowed_from_help') })),
    fs(t('mail.smtp'), field(t('mail.host'), host), field(t('mail.port'), port, { help: t('mail.port_help') }), field(t('mail.security'), security),
      field(t('mail.username'), user), field(t('mail.password'), pw, { help: t('mail.password_help') }), field(t('mail.helo'), helo), field(t('mail.ca_cert'), ca, { help: t('mail.ca_cert_help') }), skip.el,
      field(t('mail.concurrency'), conc, { help: t('mail.concurrency_help') }), field(t('mail.rate'), rate, { help: t('mail.rate_help') })),
    fs(t('mail.sendmail'), field(t('mail.sendmail_path'), sendmail, { help: t('mail.sendmail_help') })),
    fs(t('mail.bounce'), h('p', { class: 'ew-help' }, t('mail.bounce_help')), field(t('mail.bounce_address'), bAddr), bVerp.el,
      field(t('mail.imap_host'), bHost), field(t('mail.port'), bPort), field(t('mail.security'), bSec), field(t('mail.username'), bUser),
      field(t('mail.password'), bPw), field(t('mail.imap_folder'), bFolder), bDel.el),
    fs(t('mail.digest'), h('p', { class: 'ew-help' }, t('mail.digest_help')), field(t('mail.digest_hour'), dHour), field(t('mail.digest_weekday'), dDay),
      field(t('mail.digest_timezone'), dTZ, { help: t('mail.digest_timezone_help') })),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put('admin/settings', { mail: {
        enabled: enabled.input.checked ? null : false,
        transport: transport.value, from: from.value.trim(), from_name: fromName.value.trim(), reply_to: replyTo.value.trim(), subject_prefix: prefix.value.trim(),
        allowed_from: allowed.value.split(/[\n,;]/).map((x) => x.trim()).filter(Boolean), sendmail: sendmail.value.trim(),
        concurrency: Number(conc.value) || 0, rate_per_minute: Number(rate.value) || 0,
        digest_hour: Number(dHour.value), digest_weekday: Number(dDay.value), digest_timezone: dTZ.value.trim(),
        smtp: { host: host.value.trim(), port: Number(port.value) || 0, security: security.value, username: user.value.trim(), password: pw.value, skip_verify: skip.input.checked, helo_name: helo.value.trim(), ca_cert: ca.value.trim() },
        bounce: { address: bAddr.value.trim(), verp: bVerp.input.checked, imap_host: bHost.value.trim(), imap_port: Number(bPort.value) || 0, imap_security: bSec.value,
          imap_username: bUser.value.trim(), imap_password: bPw.value, imap_folder: bFolder.value.trim(), delete_processed: bDel.input.checked, skip_verify: b.skip_verify, ca_cert: b.ca_cert },
      } });
      toast(t('settings.saved'));
      reload();
    } catch (e) { alert.textContent = errorText(e); }
  });
  const testTo = textInput({ type: 'email', value: state.user?.email ?? '', autocomplete: 'email' });
  const testForm = h('form', { class: 'ew-inline-form', novalidate: true, 'aria-label': t('mail.test') }, field(t('mail.test_to'), testTo, { inline: true }),
    button(t('mail.test_send'), () => undefined, { type: 'submit' }));
  testForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try { await api.post('admin/mail/test', { to: testTo.value.trim() }); toast(t('mail.test_sent', { to: testTo.value })); } catch (e) { toast(errorText(e), 'error'); }
  });
  const statusSel = select(['', 'queued', 'held', 'partial', 'failed', 'sent'].map((x) => ({ value: x, label: x ? t(`mail.status.${x}`) : t('mail.status.all') })), filter);
  const search = textInput({ value: q, type: 'search' });
  const filterForm = h('form', { class: 'ew-inline-form', role: 'search', 'aria-label': t('mail.filter') }, field(t('mail.filter'), statusSel, { inline: true }),
    field(t('mail.search'), search, { inline: true }), button(t('search.submit'), () => undefined, { type: 'submit' }));
  filterForm.addEventListener('submit', (ev) => { ev.preventDefault(); navigate(`/admin/mail${qs({ status: statusSel.value, q: search.value })}`); });
  statusSel.addEventListener('change', () => navigate(`/admin/mail${qs({ status: statusSel.value, q: search.value })}`));
  const runJob = async (name: string) => {
    try { await api.post(`admin/jobs/${encodeURIComponent(name)}/run`); toast(t('mail.job_done', { name })); reload(); } catch (e) { toast(errorText(e), 'error'); }
  };
  const old = st.oldest_pending ? (Date.now() - new Date(st.oldest_pending).getTime()) / 3600000 : 0;
  mount(panel, pageHeader(t('admin.mail')), h('p', null, t('mail.help')),
    h('div', { class: ['ew-notice', st.problem ? 'ew-notice--warning' : 'ew-notice--success'], role: 'status' },
      !st.enabled ? t('mail.disabled') : st.transport ? t('mail.active', { transport: t(`mail.transport.${st.transport}`) }) : t('mail.inactive'),
      st.problem ? h('p', null, st.problem) : null,
      old > 24 ? h('p', null, t('mail.old_pending', { hours: Math.round(old) })) : null),
    h('dl', { class: 'ew-dl' },
      ...['pending', 'sent', 'failed'].flatMap((k) => [h('dt', null, t(`mail.rcount.${k}`)), h('dd', null, String(st.recipients[k] ?? 0))]),
      h('dt', null, t('mail.held_messages')), h('dd', null, String(st.counts.held ?? 0)),
      h('dt', null, t('mail.bounce')), h('dd', null, st.bounces ? t('mail.bounce_on') : t('mail.bounce_off'))),
    testForm, form,
    h('section', { class: 'ew-section', 'aria-labelledby': 'ob-h' }, h('h2', { id: 'ob-h' }, t('mail.outbox')),
      filterForm, h('div', { class: 'ew-actions' }, button(t('mail.send_now'), () => void runJob('mail')), st.bounces ? button(t('mail.bounces_now'), () => void runJob('mail.bounces')) : null),
      table(t('mail.outbox'), [t('mail.subject'), t('mail.to'), t('common.status'), t('mail.created'), t('common.actions')],
        outbox.map((o) => h('tr', null,
          h('th', { scope: 'row' }, button(o.subject, () => void mailMessageDialog(o.id, reload).catch((e) => toast(errorText(e), 'error')), { variant: 'link' })),
          h('td', null, [...o.to, ...o.cc].slice(0, 3).join(', '), o.to.length + o.cc.length > 3 ? ` +${o.to.length + o.cc.length - 3}` : ''),
          h('td', null, mailBadge(o.status), h('div', { class: 'ew-help' }, t('mail.counts', { sent: o.delivered, pending: o.pending, failed: o.failed })),
            o.last_error && o.status !== 'sent' ? h('p', { class: 'ew-help' }, o.last_error) : null),
          h('td', null, fmtDate(o.sent_at ?? o.created_at, state.lang)),
          h('td', { class: 'ew-actions' },
            o.failed > 0 || o.status === 'held' || o.pending > 0 ? button(t('mail.retry'), async () => {
              try { await api.post(`admin/mail/outbox/${o.id}/retry`); toast(t('mail.requeued')); reload(); } catch (e) { toast(errorText(e), 'error'); }
            }, { variant: 'link', ariaLabel: `${t('mail.retry')}: ${o.subject}` }) : null,
            button(t('common.delete'), async () => {
              if (!(await confirmDialog(t('mail.delete_confirm'), t('common.delete'), true))) return;
              try { await api.del(`admin/mail/outbox/${o.id}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
            }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${o.subject}` })))), t('mail.empty'))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'jobs-h' }, h('h2', { id: 'jobs-h' }, t('jobs.title')), h('p', { class: 'ew-help' }, t('jobs.help')),
      h('ul', { class: 'ew-list' }, ...jobs.map((j) => h('li', null, h('code', null, j), ' ',
        button(t('jobs.run'), () => void runJob(j), { variant: 'link', ariaLabel: `${t('jobs.run')}: ${j}` }))))));
}

// ---------------------------------------------------------------- languages

// ---------------------------------------------------------------- audit

// ---------------------------------------------------------------- guest cache

interface CacheStatus { enabled: boolean; ttl_seconds: number; entries: number; pages: number; bytes: number; max_entries: number; max_bytes: number; hits: number; misses: number; hit_rate: number; stores: number; clears: number; generation: number }

async function cacheAdmin(panel: HTMLElement): Promise<void> {
  const [st, s] = await Promise.all([api.get<CacheStatus>('admin/cache'), api.get<Settings>('admin/settings')]);
  const c = (s.config as unknown as { cache: { enabled: boolean; ttl_seconds: number; max_entries: number; max_mb: number } }).cache;
  const enabled = checkbox(t('cache.enabled'), c.enabled, { help: t('cache.enabled_help') });
  const ttl = textInput({ type: 'number', value: String(c.ttl_seconds) });
  const maxE = textInput({ type: 'number', value: String(c.max_entries) });
  const maxMB = textInput({ type: 'number', value: String(c.max_mb) });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert, enabled.el,
    field(t('cache.ttl'), ttl, { help: t('cache.ttl_help') }), field(t('cache.max_entries'), maxE), field(t('cache.max_mb'), maxMB),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put('admin/settings', { cache: { enabled: enabled.input.checked, ttl_seconds: Number(ttl.value) || 0, max_entries: Number(maxE.value) || 0, max_mb: Number(maxMB.value) || 0 } });
      toast(t('common.saved'));
      reload();
    } catch (e) { alert.textContent = errorText(e); }
  });
  const mb = (n: number) => `${(n / 1048576).toFixed(1)} MB`;
  const rows: [string, string][] = [
    [t('cache.state'), st.enabled ? t('cache.on') : t('cache.off')], [t('cache.entries'), `${st.entries} / ${st.max_entries || '∞'}`],
    [t('cache.pages'), String(st.pages)], [t('cache.size'), `${mb(st.bytes)} / ${st.max_bytes ? mb(st.max_bytes) : '∞'}`],
    [t('cache.hit_rate'), `${Math.round(st.hit_rate * 100)} % (${st.hits} / ${st.hits + st.misses})`], [t('cache.clears'), String(st.clears)],
  ];
  mount(panel, pageHeader(t('admin.cache')),
    h('p', null, t('cache.help')),
    h('section', { class: 'ew-section', 'aria-labelledby': 'cs-h' }, h('h2', { id: 'cs-h' }, t('cache.status')),
      h('dl', { class: 'ew-facts' }, ...rows.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)])),
      h('p', { class: 'ew-help' }, t('cache.per_process')),
      h('div', { class: 'ew-actions' }, button(t('cache.clear'), async () => {
        try { await api.del('admin/cache'); toast(t('cache.cleared')); reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'secondary' }), button(t('cache.refresh'), () => reload(), { variant: 'subtle' }))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'cc-h' }, h('h2', { id: 'cc-h' }, t('cache.settings')), form));
}
