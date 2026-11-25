// Administration → Overview: the start page of the administration. Tiles
// with the state of the wiki, updates (with the commands to install them on
// the server; the wiki never installs the core itself), security, plugins,
// users and license, each linking to its administration page. Problems are
// marked with a word, not only with a colour (WCAG 1.4.1). The server sends
// only the parts the person may see (GET admin/status).
import { api } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { licenseSeal } from '../components/brand';
import { errorText, pageHeader } from '../components/ui';
import { adminSections } from './admin';
import { statusList, type AdminStatus } from './dashboard';
import { licenseNotice, type LicenseStatus } from './adminLicense';

interface UpdateInfo { id: string; name: string; current: string; latest?: string; url?: string; notes?: string; available: boolean; error?: string; security?: boolean; security_notes?: string }
interface UpdateCommand { kind: 'docker-published' | 'docker-self-built' | 'systemd'; lines: string[] }
interface Install { kind: '' | 'docker' | 'systemd'; image?: string; origin?: string; dir?: string; commands: UpdateCommand[] }
/** what the update helper on the host (scripts/update-docker.sh) reports, checked by the server */
interface TimerState { mode: 'systemd' | 'cron'; next_run: string | null; window_minutes: number; last_run: string | null; last_result?: string; last_version?: string; updated_at: string; fresh: boolean }

interface Overview extends AdminStatus {
  updates?: { core: UpdateInfo | null; security: boolean; install: Install; timer?: TimerState | null; timer_error?: string };
  plugins?: { total: number; enabled: number; active: number; in_process: number };
  users?: { active: number; disabled: number; invited: number; directory: { provider: string; status: string; started_at: string; error?: string }[] };
  security?: { providers: { id: string; name: string; required: number }[]; factors: number };
  license?: LicenseStatus;
}

type Level = 'ok' | 'info' | 'warn' | 'error';

/** badge: the word that marks a state (colour only in addition). */
function badge(level: Level, text: string): HTMLElement {
  const cls = { ok: 'ew-badge--ok', info: 'ew-badge--info', warn: 'ew-badge--warn', error: 'ew-badge--danger' }[level];
  return h('span', { class: ['ew-badge', cls] }, text);
}

/** A tile of the overview; the heading links to the administration page
 * when the person may open it. */
function ovTile(id: string, title: string, href: string | null, ...content: (Node | null)[]): HTMLElement {
  const open = href !== null && adminSections().some((s) => `/admin/${s.id}` === href);
  return h('section', { class: 'ew-tile ew-ov-tile', 'aria-labelledby': `ov-${id}` },
    h('h2', { id: `ov-${id}`, class: 'ew-tile-title' }, open ? h('a', { href }, title) : title), ...content);
}

function facts(rows: [string, Node | string, ...(Node | null)[]][]): HTMLElement {
  return h('dl', { class: 'ew-ov-facts' }, ...rows.flatMap(([k, ...v]) => [h('dt', null, k), h('dd', null, ...v)]));
}

/** When the core update is installed: the next run the update helper
 * reports (Docker), else the fixed facts of the timers. */
function autoInstallText(up: NonNullable<Overview['updates']>): HTMLElement {
  const { install, timer } = up;
  const L = state.lang;
  const out = h('div', { class: 'ew-help' });
  const p = (text: string, ...extra: (Node | string | null)[]) => out.append(h('p', null, text, ...extra));
  if (install.kind !== 'docker') p(t('overview.auto_systemd'));
  if (install.kind === 'systemd') return out;
  if (up.timer_error) {
    p(t('overview.timer_unreadable'));
  } else if (timer && timer.fresh && timer.next_run) {
    p(t(timer.window_minutes ? 'overview.timer_next_window' : 'overview.timer_next', { date: fmtDate(timer.next_run, L), minutes: timer.window_minutes }));
  } else if (timer) {
    p(t('overview.timer_stale', { date: fmtDate(timer.updated_at, L) }));
  } else {
    p(t(install.kind === 'docker' ? 'overview.timer_none' : 'overview.auto_docker'));
  }
  if (timer?.last_run) {
    const r = timer.last_result ?? '';
    const bad = ['failed', 'backup-failed', 'invalid-marker', 'not-reached'].includes(r);
    p(t('overview.timer_last', { date: fmtDate(timer.last_run, L) }), ' ',
      badge(bad ? 'error' : r === 'updated' ? 'ok' : 'info', t(`overview.timer_result_${r.replace(/-/g, '_') || 'none'}`, { version: timer.last_version ?? '' })));
  }
  return out;
}

/** The commands that install the update, each with its label. */
function commands(install: Install): HTMLElement {
  return h('div', { class: 'ew-ov-commands' },
    install.kind === '' ? h('p', null, t('overview.cmd_unknown')) : null,
    ...install.commands.map((c) => h('div', null,
      h('p', { class: 'ew-ov-cmd-label' }, t(`overview.cmd_${c.kind.replace(/-/g, '_')}`)),
      h('pre', { class: 'ew-code', tabindex: '0' }, h('code', null, c.lines.join('\n'))))),
    install.kind !== 'systemd' && install.origin !== 'self-built' ? h('p', { class: 'ew-help' }, t('overview.cmd_fixed_tag')) : null);
}

function updatesTile(up: NonNullable<Overview['updates']>, st: Overview): HTMLElement {
  const body = h('div', { 'aria-live': 'polite', 'aria-busy': 'true' }, h('p', null, t('overview.checking')));
  // the check may ask GitHub (cached for an hour), so the tile fills later
  void api.get<{ checked_at: string | null; items: UpdateInfo[] }>('admin/updates').then((rep) => {
    const core = rep.items.find((i) => i.id === 'core') ?? null;
    const plugins = rep.items.filter((i) => i.id !== 'core' && i.available).length;
    let state_: HTMLElement;
    if (!rep.checked_at) state_ = badge('info', t('overview.check_off'));
    else if (!core || core.error) state_ = h('span', null, badge('warn', t('overview.check_failed')), core?.error ? ` ${core.error}` : '');
    else if (core.available) state_ = badge(core.security ? 'error' : 'warn', core.security ? t('overview.security_update', { version: core.latest ?? '' }) : t('overview.new_version', { version: core.latest ?? '' }));
    else state_ = badge('ok', t('updates.current'));
    const rows: [string, Node | string, ...(Node | null)[]][] = [
      [t('updates.installed'), st.version],
      [t('common.status'), state_],
    ];
    if (rep.checked_at) rows.push([t('overview.checked'), fmtDate(rep.checked_at, state.lang)]);
    if (plugins) rows.push([t('overview.plugin_updates'), t('overview.plugin_updates_n', { count: plugins })]);
    const avail = core?.available === true;
    mount(body,
      facts(rows),
      avail && core?.security_notes ? h('div', { class: 'ew-notice ew-notice--danger' },
        h('h3', { class: 'ew-ov-h3' }, t('overview.security_notes')),
        h('pre', { class: 'ew-ov-notes', tabindex: '0' }, core.security_notes)) : null,
      avail && core?.notes ? h('details', null, h('summary', null, t('overview.all_notes')),
        h('pre', { class: 'ew-ov-notes', tabindex: '0' }, core.notes),
        core.url ? h('p', null, h('a', { href: core.url, rel: 'external noopener', target: '_blank' }, t('overview.release_page'))) : null) : null,
      avail ? h('div', null, h('h3', { class: 'ew-ov-h3' }, t('overview.how_install')), autoInstallText(up), commands(up.install))
        : h('details', null, h('summary', null, t('overview.how_install')), autoInstallText(up), commands(up.install)));
    body.setAttribute('aria-busy', 'false');
  }).catch((e) => {
    mount(body, h('p', null, badge('warn', t('overview.check_failed')), ' ', errorText(e)));
    body.setAttribute('aria-busy', 'false');
  });
  return ovTile('updates', t('admin.updates'), '/admin/updates', body);
}

function securityTile(sec: NonNullable<Overview['security']>, st: Overview): HTMLElement {
  return ovTile('security', t('overview.security'), '/admin/mfa',
    h('h3', { class: 'ew-ov-h3' }, t('overview.mfa_by_method')),
    sec.providers.length ? h('ul', { class: 'ew-ov-list' }, ...sec.providers.map((p) => h('li', null, `${p.name} (${p.id}): `,
      p.required >= 2 ? badge('ok', t('overview.mfa_required', { n: p.required })) : badge('info', t('overview.mfa_one'))))) : h('p', null, '–'),
    sec.factors === 0 ? h('p', { class: 'ew-help' }, t('overview.mfa_no_factors')) : null,
    statusList(st, ['logins', 'forward']));
}

function pluginsTile(pl: NonNullable<Overview['plugins']>, st: Overview): HTMLElement {
  return ovTile('plugins', t('admin.plugins'), '/admin/plugins',
    facts([
      [t('overview.plugins_installed'), String(pl.total)],
      [t('overview.plugins_enabled'), String(pl.enabled)],
      [t('overview.plugins_active'), String(pl.active)],
    ]),
    h('p', { class: 'ew-help' }, t('overview.plugins_isolation')),
    statusList(st, ['plugins']));
}

function usersTile(u: NonNullable<Overview['users']>): HTMLElement {
  return ovTile('users', t('admin.users'), '/admin/users',
    facts([
      [t('overview.users_active'), String(u.active)],
      [t('overview.users_disabled'), String(u.disabled)],
      [t('overview.users_invited'), String(u.invited)],
    ]),
    h('h3', { class: 'ew-ov-h3' }, t('overview.directory')),
    u.directory.length ? h('ul', { class: 'ew-ov-list' }, ...u.directory.map((d) => h('li', null,
      `${d.provider}: ${fmtDate(d.started_at, state.lang)} `,
      d.status === 'ok' ? badge('ok', t('overview.sync_ok')) : badge('error', t('overview.sync_failed')),
      d.error ? ` ${d.error}` : ''))) : h('p', null, t('overview.directory_none')));
}

function licenseTile(lic: LicenseStatus): HTMLElement {
  const l = lic.license;
  return ovTile('license', t('admin.license'), '/admin/license',
    lic.valid ? licenseSeal('ew-ov-seal', 72) : null,
    lic.level === 'ok' ? facts([
      [t('common.status'), badge('ok', t('overview.license_valid'))],
      ...(l ? [[t('overview.license_for'), l.licensee] as [string, string], [t('overview.license_users'), `${lic.active_users} / ${l.users}`] as [string, string]] : []),
      ...(l?.expires ? [[t('overview.license_until'), new Date(`${l.expires}T00:00`).toLocaleDateString(state.lang)] as [string, string]] : []),
    ]) : h('div', null, lic.level === 'info' ? null : badge(lic.level === 'error' ? 'error' : 'warn', t(lic.level === 'error' ? 'dashboard.st_error' : 'dashboard.st_warn')),
      licenseNotice(lic)));
}

export async function overviewAdmin(panel: HTMLElement): Promise<void> {
  const st = await api.get<Overview>('admin/status');
  const tiles = [
    ovTile('health', t('overview.health'), null, statusList(st, ['version', 'mail', 'queue', 'database'])),
    st.updates ? updatesTile(st.updates, st) : null,
    st.security ? securityTile(st.security, st) : null,
    st.plugins ? pluginsTile(st.plugins, st) : null,
    st.users ? usersTile(st.users) : null,
    st.license ? licenseTile(st.license) : null,
  ].filter((x): x is HTMLElement => x !== null);
  mount(panel, pageHeader(t('admin.overview')), h('p', { class: 'ew-help' }, t('overview.help')),
    h('div', { class: 'ew-overview' }, ...tiles));
}
