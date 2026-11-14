// Admin page for LDAP / Active Directory (several directories possible).
import { api, button, confirmDialog, errorText, field, fmtDate, h, MAPPING_FIELDS, mount, registerAdminPage, renderAuthProviderPage, t, textInput, toast, uiLang } from '@ewiki/sdk';

export function register(): void {
  registerAdminPage({
    permission: 'core.settings.manage',
    id: 'ldap', label: () => t('auth.ldap.title'), order: 20,
    render: (el) => renderAuthProviderPage(el, {
      pluginId: 'auth.ldap', base: 'ldap', title: t('auth.ldap.title'), help: t('auth.ldap.help'),
      fields: () => [
        { key: 'name', label: t('auth.oidc.name'), placeholder: 'LDAP' },
        { key: 'url', label: t('auth.ldap.url'), help: t('auth.ldap.url_help'), placeholder: 'ldaps://ldap.example.com:636' },
        { key: 'start_tls', label: t('auth.ldap.start_tls'), type: 'bool' },
        { key: 'ca_cert', label: t('auth.ldap.ca_cert'), type: 'textarea', help: t('auth.ldap.ca_cert_help') },
        { key: 'insecure_skip_verify', label: t('auth.ldap.insecure'), type: 'bool' },
        { key: 'bind_dn', label: t('auth.ldap.bind_dn'), placeholder: 'cn=wiki,ou=services,dc=example,dc=com' },
        { key: 'bind_password', label: t('auth.ldap.bind_password'), type: 'secret', placeholder: '${LDAP_BIND_PASSWORD}' },
        { key: 'base_dn', label: t('auth.ldap.base_dn'), placeholder: 'ou=people,dc=example,dc=com' },
        { key: 'user_filter', label: t('auth.ldap.user_filter'), help: t('auth.ldap.user_filter_help'), placeholder: '(uid={username})' },
        { key: 'attr_username', label: t('auth.ldap.attr_username'), placeholder: 'uid' },
        { key: 'attr_email', label: t('auth.ldap.attr_email'), placeholder: 'mail' },
        { key: 'attr_display_name', label: t('auth.ldap.attr_display_name'), placeholder: 'cn' },
        { key: 'attr_subject', label: t('auth.ldap.attr_subject'), help: t('auth.ldap.attr_subject_help'), placeholder: 'entryUUID' },
        { key: 'group_attribute', label: t('auth.ldap.group_attribute'), placeholder: 'memberOf' },
        { key: 'group_base_dn', label: t('auth.ldap.group_base_dn'), placeholder: 'ou=groups,dc=example,dc=com' },
        { key: 'group_filter', label: t('auth.ldap.group_filter'), placeholder: '(|(member={dn})(uniqueMember={dn})(memberUid={username}))' },
        { key: 'group_name_attribute', label: t('auth.ldap.group_name_attribute'), placeholder: 'cn' },
        ...MAPPING_FIELDS(),
        { key: 'sync_enabled', label: t('auth.ldap.sync_enabled'), type: 'bool', help: t('auth.ldap.sync_enabled_help') },
        { key: 'sync_interval_minutes', label: t('auth.ldap.sync_interval'), type: 'number', placeholder: '60', help: t('auth.ldap.sync_interval_help') },
        { key: 'sync_filter', label: t('auth.ldap.sync_filter'), help: t('auth.ldap.sync_filter_help'), placeholder: '(objectClass=inetOrgPerson)' },
        { key: 'disabled_filter', label: t('auth.ldap.disabled_filter'), help: t('auth.ldap.disabled_filter_help'), placeholder: '(userAccountControl:1.2.840.113556.1.4.803:=2)' },
        { key: 'sync_create_users', label: t('auth.ldap.sync_create_users'), type: 'bool', default: true, help: t('auth.ldap.sync_create_users_help') },
        { key: 'sync_disable_missing', label: t('auth.ldap.sync_disable_missing'), type: 'bool', default: true, help: t('auth.ldap.sync_disable_missing_help') },
        { key: 'sync_max_disable_percent', label: t('auth.ldap.sync_max_disable'), type: 'number', placeholder: '10', help: t('auth.ldap.sync_max_disable_help') },
      ],
      extra: (provider, instance) => {
        const user = textInput({ autocomplete: 'off' });
        const pw = textInput({ type: 'password', autocomplete: 'off' });
        const result = h('div', { role: 'status', class: 'ew-test-result' });
        const test = h('form', { class: 'ew-form ew-panel', novalidate: true }, h('h2', null, t('auth.ldap.test')),
          field(t('auth.username'), user), field(t('auth.password'), pw),
          h('div', { class: 'ew-form-actions' }, button(t('auth.ldap.test'), () => undefined, { type: 'submit' })), result);
        test.addEventListener('submit', async (ev) => {
          ev.preventDefault();
          result.textContent = '';
          try {
            const r = await api.post<{ ok: boolean; message?: string; identity?: unknown }>('/api/v1/x/auth.ldap/test', { instance: provider, username: user.value, password: pw.value });
            mount(result, r.ok ? h('div', null, h('p', null, h('strong', null, t('auth.ldap.test_ok'))), h('pre', { class: 'ew-code', tabindex: '0' }, JSON.stringify(r.identity, null, 2)))
              : h('p', { class: 'ew-field-error' }, t('auth.ldap.test_failed', { message: r.message ?? '' })));
          } catch (e) { result.textContent = errorText(e); }
        });
        return h('div', null, test, syncPanel(provider, instance));
      },
    }),
  });
}

interface Change { username: string; action: string; added?: string[]; removed?: string[] }
interface Report {
  run: number; dry_run: boolean; status: string; error?: string; directory: number;
  created: number; updated: number; disabled: number; enabled: number; skipped: number; conflicts: number;
  group_changes: number; role_changes: number; changes: Change[]; truncated: boolean;
}
interface Run { id: number; dry_run: boolean; status: string; triggered_by: string; started_at: string; error?: string; summary: Report }
interface SyncInfo { enabled: boolean; interval_minutes: number; filter: string; runs: Run[] }

/** Directory sync: preview, run now, show the result and the last runs. */
function syncPanel(provider: string, instance: string): HTMLElement {
  const info = h('p', { class: 'ew-help' });
  const result = h('div', { role: 'status', class: 'ew-test-result' });
  const runs = h('div');
  const actions = h('div', { class: 'ew-form-actions' });
  const panel = h('section', { class: 'ew-panel', 'aria-labelledby': 'ew-ldap-sync-h' },
    h('h2', { id: 'ew-ldap-sync-h' }, t('auth.ldap.sync')), h('p', null, t('auth.ldap.sync_help')), info, actions, result, runs);

  const counts = (r: Report) => h('dl', { class: 'ew-dl' },
    ...(['directory', 'created', 'updated', 'disabled', 'enabled', 'skipped', 'conflicts', 'group_changes', 'role_changes'] as const)
      .flatMap((k) => [h('dt', null, t(`auth.ldap.sync_count.${k}`)), h('dd', null, String(r[k] ?? 0))]));
  const changes = (r: Report) => !r.changes?.length ? h('p', null, t('auth.ldap.sync_no_changes')) :
    h('table', { class: 'ew-table' }, h('caption', null, t('auth.ldap.sync_changes')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('auth.username')), h('th', { scope: 'col' }, t('auth.ldap.sync_action')),
        h('th', { scope: 'col' }, t('auth.ldap.sync_added')), h('th', { scope: 'col' }, t('auth.ldap.sync_removed')))),
      h('tbody', null, ...r.changes.map((c) => h('tr', null, h('td', null, c.username),
        h('td', null, t(`auth.ldap.sync_action.${c.action}`), c.action === 'conflict' ? [' ', adopt(c.username)] : ''),
        h('td', null, (c.added ?? []).join(', ')), h('td', null, (c.removed ?? []).join(', '))))));
  // same person with a new directory id: the old account keeps pages and rights
  const adopt = (username: string) => button(t('auth.ldap.sync_adopt'), async (ev) => {
    if (!(await confirmDialog(t('auth.ldap.sync_adopt_confirm', { user: username }), t('auth.ldap.sync_adopt')))) return;
    const b = ev.currentTarget as HTMLButtonElement | null;
    try {
      await api.post('/api/v1/x/auth.ldap/sync/adopt', { instance, username });
      toast(t('auth.ldap.sync_adopted', { user: username }));
      if (b) b.replaceWith(h('span', null, t('auth.ldap.sync_adopted_short')));
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'secondary', ariaLabel: t('auth.ldap.sync_adopt_for', { user: username }) });

  const run = async (dryRun: boolean, force = false) => {
    actions.querySelectorAll('button').forEach((b) => { b.disabled = true; });
    result.setAttribute('aria-busy', 'true');
    result.textContent = t('auth.ldap.sync_running');
    try {
      const r = await api.post<Report>('/api/v1/x/auth.ldap/sync', { instance, dry_run: dryRun, force });
      if (r.status === 'refused') {
        const again = button(t('auth.ldap.sync_force'), async () => {
          if (await confirmDialog(t('auth.ldap.sync_force_confirm', { n: r.disabled || '' }), t('auth.ldap.sync_force'), true)) await run(false, true);
        }, { variant: 'danger' });
        mount(result, h('div', { class: 'ew-form-alert', role: 'alert' }, h('p', null, h('strong', null, t('auth.ldap.sync_refused'))),
          h('p', null, r.error ?? ''), h('p', null, t('auth.ldap.sync_refused_help'))), again);
      } else {
        mount(result, h('p', null, h('strong', null, t(r.dry_run ? 'auth.ldap.sync_preview_done' : 'auth.ldap.sync_done'))),
          r.conflicts ? h('p', { class: 'ew-field-error' }, t('auth.ldap.sync_conflicts_help')) : '',
          counts(r), changes(r), r.truncated ? h('p', { class: 'ew-help' }, t('auth.ldap.sync_truncated')) : '');
      }
    } catch (e) {
      mount(result, h('p', { class: 'ew-field-error' }, errorText(e)));
    } finally {
      result.removeAttribute('aria-busy');
      actions.querySelectorAll('button').forEach((b) => { b.disabled = false; });
      void load();
    }
  };
  mount(actions, button(t('auth.ldap.sync_preview'), () => run(true), { variant: 'secondary' }), button(t('auth.ldap.sync_now'), () => run(false), { variant: 'primary' }));

  const load = async () => {
    try {
      const s = await api.get<SyncInfo>(`/api/v1/x/auth.ldap/sync?instance=${encodeURIComponent(instance)}`);
      info.textContent = s.enabled ? t('auth.ldap.sync_scheduled', { minutes: s.interval_minutes, provider }) : t('auth.ldap.sync_manual');
      mount(runs, !s.runs.length ? '' : h('table', { class: 'ew-table' }, h('caption', null, t('auth.ldap.sync_runs')),
        h('thead', null, h('tr', null, ...['sync_when', 'sync_by', 'sync_kind', 'sync_status', 'sync_summary']
          .map((k) => h('th', { scope: 'col' }, t(`auth.ldap.${k}`))))),
        h('tbody', null, ...s.runs.slice(0, 20).map((x) => h('tr', null, h('td', null, fmtDate(x.started_at, uiLang())),
          h('td', null, x.triggered_by === 'schedule' ? t('auth.ldap.sync_by_schedule') : x.triggered_by),
          h('td', null, t(x.dry_run ? 'auth.ldap.sync_kind_preview' : 'auth.ldap.sync_kind_run')),
          h('td', null, t(`auth.ldap.sync_status.${x.status}`)),
          h('td', null, x.error ? x.error : t('auth.ldap.sync_summary_text', {
            created: x.summary.created ?? 0, disabled: x.summary.disabled ?? 0, enabled: x.summary.enabled ?? 0,
            changes: (x.summary.group_changes ?? 0) + (x.summary.role_changes ?? 0), conflicts: x.summary.conflicts ?? 0,
          })))))));
    } catch (e) { info.textContent = errorText(e); }
  };
  void load();
  return panel;
}
