// Administration → Sites (instance administrators): sites of the instance
// with their host names, and the instance administrators.
import { api } from '../core/api';
import { fmtDate, h, mount, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select, textInput } from '../components/ui';

interface Site { name: string; title: string; hostnames: string[]; disabled: boolean; running: boolean; accounts: 'shared' | 'separate'; created_at: string }
interface InstanceAdmin { user_id: number; username: string; display_name: string; implicit: boolean }

const reload = () => navigate(location.pathname + location.search, { replace: true });

export async function sitesAdmin(panel: HTMLElement): Promise<void> {
  const [sites, admins] = await Promise.all([api.get<Site[]>('instance/sites'), api.get<InstanceAdmin[]>('instance/admins')]);
  const rows = sites.map((s) => h('tr', null,
    h('th', { scope: 'row' }, s.title || s.name, h('br'), h('small', { class: 'ew-muted' }, s.name)),
    h('td', null, s.hostnames.length ? h('ul', { class: 'ew-plain-list' }, ...s.hostnames.map((n) => h('li', null, h('a', { href: `${location.protocol}//${n}${location.port ? `:${location.port}` : ''}/` }, n)))) : t('sites.any_host')),
    h('td', null, s.accounts === 'separate' ? t('sites.accounts_separate') : t('sites.accounts_shared')),
    h('td', null, s.disabled ? t('sites.disabled') : s.running ? t('sites.running') : t('sites.starting')),
    h('td', null, fmtDate(s.created_at, state.lang)),
    h('td', null, h('div', { class: 'ew-actions' },
      button(t('common.edit'), () => siteDialog(s), { variant: 'link', ariaLabel: `${t('common.edit')}: ${s.title || s.name}` }),
      s.name !== 'main' && s.name !== state.site.site ? button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('sites.delete_confirm', { name: s.title || s.name }), t('common.delete'), true))) return;
        try { await api.del(`instance/sites/${encodeURIComponent(s.name)}`); toast(t('sites.deleted')); reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${s.title || s.name}` }) : null))));
  const adminRows = admins.map((a) => h('tr', null,
    h('th', { scope: 'row' }, a.display_name || a.username, h('br'), h('small', { class: 'ew-muted' }, a.username)),
    h('td', null, a.implicit ? t('sites.admin_implicit') : t('sites.admin_explicit')),
    h('td', null, !a.implicit && a.user_id !== state.user?.id ? button(t('sites.admin_remove'), async () => {
      try { await api.del(`instance/admins/${a.user_id}`); toast(t('sites.admin_removed')); reload(); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: `${t('sites.admin_remove')}: ${a.display_name || a.username}` }) : null)));
  mount(panel, pageHeader(t('admin.sites'), button(t('sites.new'), () => siteDialog(null), { variant: 'primary' })),
    h('p', null, t('sites.help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('admin.sites')),
      h('thead', null, h('tr', null, ...[t('sites.site'), t('sites.hostnames'), t('sites.accounts'), t('sites.status'), t('sites.created'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...rows)),
    h('h2', null, t('sites.admins')),
    h('p', null, t('sites.admins_help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('sites.admins')),
      h('thead', null, h('tr', null, ...[t('acl.user'), t('sites.admin_kind'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...adminRows)),
    button(t('sites.admin_add'), () => addAdminDialog()));
}

function siteDialog(s: Site | null): void {
  const name = textInput({ value: s?.name ?? '', required: true, maxlength: 31, placeholder: 'docs' });
  name.pattern = '[a-z][a-z0-9_]{0,30}';
  if (s) name.readOnly = true;
  const title = textInput({ value: s?.title ?? '', maxlength: 100 });
  const hosts = textInput({ value: s?.hostnames.join(', ') ?? '', required: s?.name !== 'main', placeholder: 'docs.example.org' });
  const disabled = checkbox(t('sites.disable'), s?.disabled ?? false, { help: t('sites.disable_help') });
  // accounts: chosen once when the site is created
  const accounts = select([{ value: 'shared', label: t('sites.accounts_shared') }, { value: 'separate', label: t('sites.accounts_separate') }], state.site.sites_accounts ?? 'shared');
  const admUser = textInput({ autocomplete: 'off', maxlength: 128 });
  const admPw = textInput({ type: 'password', autocomplete: 'new-password' });
  const admBox = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('sites.first_admin')),
    h('p', { class: 'ew-help' }, t('sites.first_admin_help')),
    field(t('auth.username'), admUser, { required: true }), field(t('auth.password'), admPw, { required: true, help: t('sites.first_admin_pw_help') }));
  const sync = () => { admBox.hidden = accounts.value !== 'separate'; admUser.required = admPw.required = !admBox.hidden; };
  accounts.addEventListener('change', sync);
  sync();
  const fields = [
    field(t('sites.name'), name, { required: true, help: t('sites.name_help') }),
    field(t('sites.title'), title),
    field(t('sites.hostnames'), hosts, { required: s?.name !== 'main', help: t('sites.hostnames_help') }),
    s ? h('p', null, h('strong', null, t('sites.accounts')), ': ', s.accounts === 'separate' ? t('sites.accounts_separate') : t('sites.accounts_shared'))
      : field(t('sites.accounts'), accounts, { help: t('sites.accounts_help') }),
    s ? null : admBox,
    s && s.name !== 'main' ? disabled.el : null,
  ];
  formDialog(s ? t('sites.edit') : t('sites.new'), fields, t('common.save'), async () => {
    const body: Record<string, unknown> = { name: name.value.trim(), title: title.value, hostnames: hosts.value.split(/[\s,]+/).filter(Boolean) };
    if (s && s.name !== 'main') body.disabled = disabled.input.checked;
    if (!s) {
      body.accounts = accounts.value;
      if (accounts.value === 'separate') body.admin = { username: admUser.value.trim(), password: admPw.value };
    }
    if (s) await api.patch(`instance/sites/${encodeURIComponent(s.name)}`, body);
    else await api.post('instance/sites', body);
    toast(t('sites.saved'));
    reload();
  });
}

function addAdminDialog(): void {
  const input = textInput({ placeholder: t('acl.user_search'), required: true });
  const list = h('datalist', { id: uid('instadm') });
  input.setAttribute('list', list.id);
  const ids = new Map<string, number>();
  input.addEventListener('input', async () => {
    if (input.value.length < 2) return;
    try {
      const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(input.value)}`);
      list.replaceChildren(...res.map((u) => { ids.set(u.handle, u.id); return h('option', { value: u.handle }, u.display_name); }));
    } catch { /* lookup is optional */ }
  });
  formDialog(t('sites.admin_add'), [field(t('acl.user'), input, { required: true, help: t('acl.user_help') }), list], t('sites.admin_add'), async () => {
    const id = ids.get(input.value.trim());
    if (!id) throw new Error(t('sites.admin_unknown'));
    await api.post('instance/admins', { user_id: id });
    toast(t('sites.admin_added'));
    reload();
  });
}
