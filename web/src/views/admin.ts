// Administration: users, groups, roles, plugins, settings, languages, audit.
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { isAdmin, state } from '../core/state';
import type { AuditEntry, Group, Role } from '../core/types';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, pageHeader, select, textArea, textInput } from '../components/ui';
import { langName, setContext } from '../components/widgets';
import { listAdminPages, listTemplates } from '../sdk';
import { errorPage } from './content';

interface User { id: number; username: string; provider: string; handle: string; display_name: string; email: string; language: string; disabled: boolean; locked_until: string | null; created_at: string; last_login_at: string | null; groups: number[]; roles: number[]; totp_enabled: boolean }

const CORE_SECTIONS = ['users', 'groups', 'roles', 'plugins', 'updates', 'settings', 'mail', 'languages', 'audit'];

const reload = () => navigate(location.pathname + location.search, { replace: true });

export async function adminView({ params, query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  if (!state.user) {
    navigate(`/login?next=${encodeURIComponent(location.pathname)}`, { replace: true });
    return;
  }
  if (!isAdmin()) {
    mount(main, h('h1', null, t('error.forbidden')));
    return;
  }
  const section = params.section ?? 'users';
  const pages = [...CORE_SECTIONS.map((s) => ({ id: s, label: t(`admin.${s}`) })), ...listAdminPages().map((p) => ({ id: p.id, label: p.label() }))];
  const panel = h('div', { class: 'ew-admin-panel' });
  mount(main, h('div', { class: 'ew-admin' },
    h('nav', { class: 'ew-subnav', 'aria-label': t('admin.title') }, h('h2', { class: 'ew-subnav-title' }, t('admin.title')),
      h('ul', null, ...pages.map((p) => h('li', null, h('a', { href: `/admin/${p.id}`, 'aria-current': p.id === section ? 'page' : undefined }, p.label))))),
    panel));
  try {
    switch (section) {
      case 'users': await users(panel, query); break;
      case 'groups': await groups(panel); break;
      case 'roles': await roles(panel); break;
      case 'plugins': await plugins(panel); break;
      case 'updates': await updates(panel, false); break;
      case 'settings': await settings(panel); break;
      case 'mail': await mailAdmin(panel, query); break;
      case 'languages': await languages(panel, query); break;
      case 'audit': await auditLog(panel, query); break;
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
  mount(panel, pageHeader(t('admin.users'), button(t('admin.user_create'), () => userDialog(null, gs, rs), { variant: 'primary' })), sf,
    table(t('admin.users'), [t('admin.handle'), t('profile.display_name'), t('admin.groups'), t('admin.roles'), t('admin.last_login'), t('common.status'), t('common.actions')],
      list.map((u) => h('tr', null,
        h('th', { scope: 'row' }, u.handle, u.totp_enabled ? h('span', { class: 'ew-badge ew-badge--ok' }, ' 2FA') : null), h('td', null, u.display_name), h('td', null, name(u.groups, gs)), h('td', null, name(u.roles, rs)),
        h('td', null, u.last_login_at ? fmtDate(u.last_login_at, state.lang) : '–'),
        h('td', null, u.disabled ? h('span', { class: 'ew-badge ew-badge--warn' }, t('admin.disabled')) : (u.locked_until && new Date(u.locked_until) > new Date() ? h('span', { class: 'ew-badge ew-badge--warn' }, t('admin.locked')) : h('span', { class: 'ew-badge ew-badge--ok' }, t('admin.active')))),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), async () => {
            try { userDialog(await api.get<User>(`admin/users/${u.id}`), gs, rs); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.edit_named', { name: u.handle }) }),
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
  const fields = u
    ? [h('p', null, h('strong', null, u.handle)), field(t('profile.display_name'), display), field(t('profile.email'), email),
      u.provider === 'local' ? field(t('admin.new_password'), pw, { help: t('admin.new_password_help') }) : null, g.el, r.el, disabled.el, unlock.el,
      u.totp_enabled ? resetTotp.el : null]
    : [field(t('auth.username'), username, { required: true }), field(t('profile.display_name'), display), field(t('profile.email'), email),
      field(t('auth.password'), pw, { required: true, help: t('profile.password_rules') }), g.el, r.el];
  formDialog(u ? t('admin.user_edit') : t('admin.user_create'), fields, t('common.save'), async () => {
    if (u) {
      await api.put(`admin/users/${u.id}`, { display_name: display.value, email: email.value, groups: g.get(), roles: r.get(), disabled: disabled.input.checked, unlock: unlock.input.checked, password: pw.value || undefined, reset_totp: resetTotp.input.checked });
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
  mount(panel, pageHeader(t('admin.groups'), button(t('admin.group_create'), () => dialog(null), { variant: 'primary' })), h('p', null, t('admin.groups_help')),
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
  const rs = await api.get<Role[]>('roles');
  const dialog = (r: Role | null) => {
    const name = textInput({ value: r?.name ?? '', required: true });
    const desc = textInput({ value: r?.description ?? '' });
    const adm = checkbox(t('admin.is_admin'), r?.is_admin ?? false, { help: t('admin.is_admin_help') });
    formDialog(r ? t('admin.role_edit') : t('admin.role_create'), [field(t('common.name'), name, { required: true }), field(t('common.description'), desc), adm.el], t('common.save'), async () => {
      const body = { name: name.value, description: desc.value, is_admin: adm.input.checked };
      if (r) await api.put(`admin/roles/${r.id}`, body);
      else await api.post('admin/roles', body);
      toast(t('common.saved'));
      reload();
    });
  };
  mount(panel, pageHeader(t('admin.roles'), button(t('admin.role_create'), () => dialog(null), { variant: 'primary' })), h('p', null, t('admin.roles_help')),
    table(t('admin.roles'), [t('common.name'), t('common.description'), t('admin.is_admin'), t('admin.members'), t('common.actions')],
      rs.map((r) => h('tr', null, h('th', { scope: 'row' }, r.name), h('td', null, r.description), h('td', null, r.is_admin ? t('common.yes') : t('common.no')), h('td', null, String(r.members)),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => dialog(r), { variant: 'link', ariaLabel: t('admin.edit_named', { name: r.name }) }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('admin.role_delete_confirm', { name: r.name }), t('common.delete'), true))) return;
            try { await api.del(`admin/roles/${r.id}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('admin.delete_named', { name: r.name }) })))), t('admin.no_roles')));
}

// ---------------------------------------------------------------- plugins

interface PluginInfo { id: string; name: string; version: string; description: string; author: string; kinds: string[]; extends: string[]; required: boolean; enabled: boolean; active: boolean; missing: string[]; dependents: string[] }
interface PluginsResponse {
  installed: PluginInfo[];
  catalog: { id: string; name: string; repo: string; ref?: string; description?: string }[];
  pending: { id: string; action: string; version: string }[];
  build: { running: boolean; ok: boolean; started_at: string | null; finished_at: string | null; log: string };
  can_build: boolean;
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
      const res = await api.post<{ id: string; version: string; upgrade: boolean }>('admin/plugins/install', { source, ref });
      toast(t(res.upgrade ? 'plugins.upgraded' : 'plugins.installed', { id: res.id, version: res.version }));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const src = textInput({ placeholder: 'github.com/owner/repo' });
  const ref = textInput({ placeholder: 'v1.0.0' });
  const inf = h('form', { class: 'ew-form ew-panel', novalidate: true }, h('h2', null, t('plugins.install_title')), h('p', null, t('plugins.install_help')),
    field(t('plugins.source'), src, { required: true, help: t('plugins.source_help') }), field(t('plugins.ref'), ref),
    h('div', { class: 'ew-form-actions' }, button(t('plugins.install'), () => undefined, { type: 'submit', variant: 'primary' })));
  inf.addEventListener('submit', (ev) => { ev.preventDefault(); if (src.value.trim()) void install(src.value.trim(), ref.value.trim()); });
  const b = data.build;
  const buildBox = h('section', { class: 'ew-panel', 'aria-labelledby': 'build-h' }, h('h2', { id: 'build-h' }, t('plugins.build_title')),
    data.pending.length ? h('div', null, h('p', null, t('plugins.pending')), h('ul', null, ...data.pending.map((c) => h('li', null, `${t('plugins.action.' + c.action)}: ${c.id} ${c.version}`)))) : h('p', null, t('plugins.no_pending')),
    data.can_build
      ? h('div', { class: 'ew-actions' },
        button(t('plugins.build'), async () => { try { await api.post('admin/plugins/build'); toast(t('plugins.build_started')); setTimeout(reload, 1500); } catch (e) { toast(errorText(e), 'error'); } }, { variant: 'primary', disabled: b.running }),
        b.ok ? button(t('plugins.restart'), async () => {
          if (!(await confirmDialog(t('plugins.restart_confirm'), t('plugins.restart')))) return;
          try { await api.post('admin/restart'); toast(t('plugins.restarting')); setTimeout(() => location.reload(), 4000); } catch (e) { toast(errorText(e), 'error'); }
        }) : null)
      : h('p', { class: 'ew-help' }, t('plugins.cannot_build')),
    b.started_at ? h('p', { role: 'status' }, b.running ? t('plugins.build_running') : (b.ok ? t('plugins.build_ok') : t('plugins.build_failed'))) : null,
    b.log ? h('details', null, h('summary', null, t('plugins.build_log')), h('pre', { class: 'ew-code', tabindex: '0' }, b.log)) : null);
  mount(panel, pageHeader(t('admin.plugins')), h('p', null, t('plugins.help')),
    table(t('admin.plugins'), [t('common.name'), t('plugins.version'), t('plugins.kinds'), t('plugins.extends'), t('common.status'), t('common.actions')],
      data.installed.map((p) => h('tr', null,
        h('th', { scope: 'row' }, h('span', null, p.name), h('br'), h('code', null, p.id), h('p', { class: 'ew-help' }, p.description)),
        h('td', null, p.version), h('td', null, p.kinds.map((k) => t(`plugins.kind.${k}`)).join(', ')),
        h('td', null, p.extends.join(', ') || '–', p.missing.length ? h('p', { class: 'ew-field-error' }, t('plugins.missing', { ids: p.missing.join(', ') })) : null),
        h('td', null, p.required ? h('span', { class: 'ew-badge ew-badge--info' }, t('plugins.required')) : null, ' ',
          p.active ? h('span', { class: 'ew-badge ew-badge--ok' }, t('plugins.active')) : h('span', { class: 'ew-badge ew-badge--warn' }, p.enabled ? t('plugins.inactive_deps') : t('plugins.inactive'))),
        h('td', { class: 'ew-actions' },
          p.required ? null : (p.enabled
            ? button(t('plugins.disable'), () => void toggle(p, false), { variant: 'link', ariaLabel: t('plugins.disable_named', { name: p.name }) })
            : button(t('plugins.enable'), () => void toggle(p, true), { variant: 'link', ariaLabel: t('plugins.enable_named', { name: p.name }) })),
          p.required ? null : button(t('plugins.remove'), async () => {
            if (!(await confirmDialog(t('plugins.remove_confirm', { name: p.name }), t('plugins.remove'), true))) return;
            try { await api.del(`admin/plugins/${encodeURIComponent(p.id)}`); toast(t('plugins.removed')); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('plugins.remove_named', { name: p.name }) })))), '–'),
    buildBox,
    data.catalog.length ? h('section', { class: 'ew-section', 'aria-labelledby': 'cat-h' }, h('h2', { id: 'cat-h' }, t('plugins.catalog')),
      table(t('plugins.catalog'), [t('common.name'), t('common.description'), t('plugins.source'), t('common.actions')],
        data.catalog.map((c) => h('tr', null, h('th', { scope: 'row' }, c.name, h('br'), h('code', null, c.id)), h('td', null, c.description ?? ''), h('td', null, c.repo),
          h('td', null, button(installedIds.has(c.id) ? t('plugins.upgrade') : t('plugins.install'), () => void install(c.id), { variant: 'link', ariaLabel: `${t('plugins.install')} ${c.name}` })))), '–')) : null,
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
    ui: { site_name: string; template: string; home_category: number };
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
  const tpl = select(tpls.map((x) => ({ value: x.id, label: x.id })), c.ui.template);
  const home = num(c.ui.home_category);
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
    fs(t('settings.site'), field(t('settings.site_name'), siteName, { required: true }), field(t('settings.template'), tpl), field(t('settings.home_category'), home, { help: t('settings.home_category_help') }),
      field(t('settings.default_language'), defLang, { help: t('settings.languages_help') })),
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
        ui: { site_name: siteName.value, template: tpl.value, home_category: Number(home.value) || 0 },
        i18n: { default_language: defLang.value, languages: c.i18n.languages },
        auth: { order: order.value.split(',').map((x) => x.trim()).filter(Boolean), session_ttl: sessionTTL.value, api_key_max_ttl: keyTTL.value, ssh_token_ttl: sshTTL.value, max_login_failures: Number(fails.value), lockout_duration: lockout.value, min_password_length: Number(minPw.value) },
        retention: { revisions_keep_days: Number(keepDays.value), revisions_keep_min: Number(keepMin.value), trash_days: Number(trashDays.value), audit_days: Number(auditDays.value) },
      });
      toast(t('settings.saved'));
      setTimeout(() => location.reload(), 800);
    } catch (e) { alert.textContent = errorText(e); }
  });
  mount(panel, pageHeader(t('admin.settings')), h('p', null, t('settings.file_note', { path: s.path })),
    h('dl', { class: 'ew-dl' }, h('dt', null, t('settings.mode')), h('dd', null, c.mode), h('dt', null, t('settings.listen')), h('dd', null, c.server.listen), h('dt', null, t('settings.base_url')), h('dd', null, c.server.base_url)),
    form);
}

// ---------------------------------------------------------------- e-mail

interface MailConfig {
  transport: string; from: string; from_name: string; reply_to: string; allowed_from: string[] | null; subject_prefix: string; sendmail: string;
  smtp: { host: string; port: number; security: string; username: string; password: string; skip_verify: boolean; helo_name: string };
}
interface MailStatus { transport: string; from: string; counts: Record<string, number>; problem?: string }
interface OutboxEntry { id: number; status: string; from: string; to: string[]; cc: string[]; subject: string; body: string; source: string; ref: string; attempts: number; last_error: string; created_at: string; sent_at: string | null }

async function mailAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const filter = query.get('status') ?? '';
  const [cfg, st, outbox, jobs] = await Promise.all([
    api.get<{ config: { mail: MailConfig } }>('admin/settings'), api.get<MailStatus>('admin/mail'),
    api.get<OutboxEntry[]>(`admin/mail/outbox${qs({ status: filter, limit: 100 })}`), api.get<string[]>('admin/jobs')]);
  const m = cfg.config.mail;
  const transport = select(['auto', 'smtp', 'sendmail', 'log', 'none'].map((x) => ({ value: x, label: t(`mail.transport.${x}`) })), m.transport || 'auto');
  const from = textInput({ value: m.from, type: 'email', placeholder: 'wiki@example.com' });
  const fromName = textInput({ value: m.from_name });
  const replyTo = textInput({ value: m.reply_to, type: 'email' });
  const prefix = textInput({ value: m.subject_prefix, placeholder: '[Wiki]' });
  const allowed = textArea({ value: (m.allowed_from ?? []).join('\n'), rows: 3 });
  const host = textInput({ value: m.smtp.host, placeholder: 'smtp.example.com' });
  const port = textInput({ value: m.smtp.port ? String(m.smtp.port) : '', type: 'number', placeholder: '587' });
  const security = select(['starttls', 'tls', 'none'].map((x) => ({ value: x, label: t(`mail.security.${x}`) })), m.smtp.security || 'starttls');
  const user = textInput({ value: m.smtp.username, autocomplete: 'off' });
  const pw = textInput({ value: m.smtp.password, type: 'password', autocomplete: 'new-password' });
  const skip = checkbox(t('mail.skip_verify'), m.smtp.skip_verify, { help: t('mail.skip_verify_help') });
  const helo = textInput({ value: m.smtp.helo_name });
  const sendmail = textInput({ value: m.sendmail, placeholder: '/usr/sbin/sendmail' });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const fs = (legend: string, ...kids: (HTMLElement | null)[]) => h('fieldset', null, h('legend', null, legend), ...kids);
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    fs(t('mail.sender'), field(t('mail.transport'), transport, { help: t('mail.transport_help') }), field(t('mail.from'), from, { help: t('mail.from_help') }),
      field(t('mail.from_name'), fromName), field(t('mail.reply_to'), replyTo), field(t('mail.subject_prefix'), prefix),
      field(t('mail.allowed_from'), allowed, { help: t('mail.allowed_from_help') })),
    fs(t('mail.smtp'), field(t('mail.host'), host), field(t('mail.port'), port, { help: t('mail.port_help') }), field(t('mail.security'), security),
      field(t('mail.username'), user), field(t('mail.password'), pw, { help: t('mail.password_help') }), field(t('mail.helo'), helo), skip.el),
    fs(t('mail.sendmail'), field(t('mail.sendmail_path'), sendmail, { help: t('mail.sendmail_help') })),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put('admin/settings', { mail: {
        transport: transport.value, from: from.value.trim(), from_name: fromName.value.trim(), reply_to: replyTo.value.trim(), subject_prefix: prefix.value.trim(),
        allowed_from: allowed.value.split(/[\n,;]/).map((x) => x.trim()).filter(Boolean), sendmail: sendmail.value.trim(),
        smtp: { host: host.value.trim(), port: Number(port.value) || 0, security: security.value, username: user.value.trim(), password: pw.value, skip_verify: skip.input.checked, helo_name: helo.value.trim() },
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
  const statusSel = select(['', 'queued', 'held', 'failed', 'sent'].map((x) => ({ value: x, label: x ? t(`mail.status.${x}`) : t('mail.status.all') })), filter);
  statusSel.addEventListener('change', () => navigate(`/admin/mail${qs({ status: statusSel.value })}`));
  const badge = (x: string) => h('span', { class: ['ew-badge', x === 'sent' ? 'ew-badge--ok' : x === 'failed' || x === 'held' ? 'ew-badge--warn' : 'ew-badge--info'] }, t(`mail.status.${x}`));
  const runJob = async (name: string) => {
    try { await api.post(`admin/jobs/${encodeURIComponent(name)}/run`); toast(t('mail.job_done', { name })); reload(); } catch (e) { toast(errorText(e), 'error'); }
  };
  mount(panel, pageHeader(t('admin.mail')), h('p', null, t('mail.help')),
    h('div', { class: ['ew-notice', st.problem ? 'ew-notice--warning' : 'ew-notice--success'], role: 'status' },
      st.transport ? t('mail.active', { transport: t(`mail.transport.${st.transport}`) }) : t('mail.inactive'),
      st.problem ? h('p', null, st.problem) : null),
    h('dl', { class: 'ew-dl' }, ...['queued', 'held', 'failed', 'sent'].flatMap((k) => [h('dt', null, t(`mail.status.${k}`)), h('dd', null, String(st.counts[k] ?? 0))])),
    testForm, form,
    h('section', { class: 'ew-section', 'aria-labelledby': 'ob-h' }, h('h2', { id: 'ob-h' }, t('mail.outbox')),
      h('div', { class: 'ew-actions' }, field(t('mail.filter'), statusSel, { inline: true }), button(t('mail.send_now'), () => void runJob('mail'))),
      table(t('mail.outbox'), [t('mail.subject'), t('mail.to'), t('common.status'), t('mail.created'), t('common.actions')],
        outbox.map((o) => h('tr', null,
          h('th', { scope: 'row' }, button(o.subject, () => openDialog(o.subject, h('div', null,
            h('dl', { class: 'ew-dl' }, h('dt', null, t('mail.from')), h('dd', null, o.from), h('dt', null, t('mail.to')), h('dd', null, [...o.to, ...o.cc].join(', ')),
              h('dt', null, t('mail.source')), h('dd', null, `${o.source} ${o.ref}`)),
            h('pre', { class: 'ew-code ew-mail-body', tabindex: '0' }, o.body)), [], { wide: true }), { variant: 'link' })),
          h('td', null, [...o.to, ...o.cc].join(', ')),
          h('td', null, badge(o.status), o.last_error ? h('p', { class: 'ew-help' }, `${t('mail.attempts', { n: o.attempts })}: ${o.last_error}`) : null),
          h('td', null, fmtDate(o.sent_at ?? o.created_at, state.lang)),
          h('td', { class: 'ew-actions' },
            o.status === 'failed' || o.status === 'held' ? button(t('mail.retry'), async () => {
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

interface LangStatus { lang: string; total: number; translated: number; percent: number; default: boolean }
interface UIString { key: string; source: string; reference: string; value: string; origin: string }

async function languages(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const edit = query.get('edit');
  if (edit) return translationEditor(panel, edit, query.get('filter') ?? 'missing');
  const st = await api.get<LangStatus[]>('i18n/status');
  const code = textInput({ placeholder: 'fr', maxlength: 8 });
  const add = h('form', { class: 'ew-inline-form', novalidate: true }, field(t('languages.code'), code, { inline: true, help: t('languages.code_help') }), button(t('languages.add'), () => undefined, { type: 'submit', variant: 'primary' }));
  add.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try { await api.post('admin/languages', { code: code.value.trim() }); toast(t('languages.added')); setTimeout(() => location.reload(), 600); } catch (e) { toast(errorText(e), 'error'); }
  });
  mount(panel, pageHeader(t('admin.languages')), h('p', null, t('languages.help')),
    table(t('admin.languages'), [t('common.language'), t('languages.progress'), t('common.actions')],
      st.map((l) => h('tr', null,
        h('th', { scope: 'row' }, langName(l.lang), ' ', h('code', null, l.lang), l.default ? h('span', { class: 'ew-badge ew-badge--info' }, ` ${t('languages.default')}`) : null),
        h('td', null, h('progress', { max: '100', value: String(l.percent), 'aria-label': t('languages.progress_of', { lang: langName(l.lang) }) }), ` ${t('languages.percent', { percent: l.percent, translated: l.translated, total: l.total })}`),
        h('td', { class: 'ew-actions' },
          h('a', { href: `/admin/languages?edit=${l.lang}` }, t('languages.translate'), h('span', { class: 'sr-only' }, ` ${langName(l.lang)}`)),
          l.default ? null : button(t('common.remove'), async () => {
            if (!(await confirmDialog(t('languages.remove_confirm', { lang: langName(l.lang) }), t('common.remove'), true))) return;
            try { await api.del(`admin/languages/${l.lang}`); setTimeout(() => location.reload(), 300); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.remove')} ${langName(l.lang)}` })))), '–'),
    h('h2', null, t('languages.add_title')), add);
}

async function translationEditor(panel: HTMLElement, lang: string, filter: string): Promise<void> {
  const strings = await api.get<UIString[]>(`admin/i18n/${encodeURIComponent(lang)}`);
  const shown = strings.filter((s) => filter === 'all' || (filter === 'missing' ? s.origin === 'missing' : s.origin === 'custom'));
  const changedVals = new Map<string, string>();
  const filterSel = select(['missing', 'custom', 'all'].map((f) => ({ value: f, label: t(`languages.filter.${f}`) })), filter);
  filterSel.addEventListener('change', () => navigate(`/admin/languages${qs({ edit: lang, filter: filterSel.value })}`, { replace: true }));
  const rows = shown.slice(0, 500).map((s) => {
    const inp = textInput({ value: s.origin === 'custom' || s.origin === 'file' ? s.value : '', lang });
    inp.setAttribute('aria-label', `${t('languages.translation_of')} ${s.reference}`);
    inp.addEventListener('input', () => changedVals.set(s.key, inp.value));
    return h('tr', null, h('td', null, h('code', null, s.key), h('br'), h('span', { class: 'ew-help' }, s.source)), h('td', { lang: 'en' }, s.reference), h('td', null, inp));
  });
  const save = button(t('common.save'), async () => {
    if (!changedVals.size) return;
    try { await api.put(`admin/i18n/${encodeURIComponent(lang)}`, Object.fromEntries(changedVals)); toast(t('common.saved')); reload(); } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'primary' });
  mount(panel, pageHeader(t('languages.editor_title', { lang: langName(lang) }), save),
    h('p', null, h('a', { href: '/admin/languages' }, t('languages.back'))), field(t('languages.filter'), filterSel, { inline: true }),
    table(t('languages.editor_title', { lang: langName(lang) }), [t('languages.key'), t('languages.reference'), t('languages.translation')], rows, t('languages.nothing')),
    shown.length > 500 ? h('p', null, t('languages.truncated')) : null);
}

// ---------------------------------------------------------------- audit

async function auditLog(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const type = query.get('entity_type') ?? '';
  const offset = Number(query.get('offset') ?? 0);
  const entries = await api.get<AuditEntry[]>(`admin/audit${qs({ entity_type: type, limit: 100, offset })}`);
  const sel = select(['', 'category', 'element', 'user', 'group', 'role', 'apikey', 'sshkey', 'settings', 'menu'].map((x) => ({ value: x, label: x ? x : t('audit.all') })), type);
  sel.addEventListener('change', () => navigate(`/admin/audit${qs({ entity_type: sel.value })}`));
  mount(panel, pageHeader(t('admin.audit')), field(t('audit.object'), sel, { inline: true }),
    table(t('admin.audit'), [t('common.date'), t('common.author'), t('audit.action'), t('audit.object')],
      entries.map((e) => h('tr', null, h('td', null, fmtDate(e.at, state.lang)), h('td', null, e.actor), h('td', null, h('code', null, e.action)), h('td', null, `${e.entity_type} ${e.entity_id ?? ''}`))), '–'),
    h('div', { class: 'ew-actions' },
      offset > 0 ? h('a', { href: `/admin/audit${qs({ entity_type: type, offset: Math.max(0, offset - 100) })}` }, t('common.previous')) : null,
      entries.length === 100 ? h('a', { href: `/admin/audit${qs({ entity_type: type, offset: offset + 100 })}` }, t('common.next')) : null));
}
