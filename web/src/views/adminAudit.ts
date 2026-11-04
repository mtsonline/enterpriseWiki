// Audit log: search and filter, details, export, settings (what is logged)
// and forwarding to other systems (syslog, webhook, OpenTelemetry, file).
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate } from '../core/router';
import { hasPerm, state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select, textArea, textInput } from '../components/ui';

interface Entry { id: number; at: string; user_id: number | null; actor: string; action: string; entity_type: string; entity_id: number | null; before?: unknown; after?: unknown; ip?: string; user_agent?: string; via?: string }
interface Forward { id: string; name: string; enabled: boolean; type: string; address: string; protocol: string; secret: string; headers: Record<string, string>; areas: string[] }
interface Status { id: string; last_id: number; last_at: string | null; last_error: string; errors: number; pending: number }
interface SettingsResp { settings: { disabled: string[]; log_logins: boolean; log_denied: boolean; log_views: boolean; forward: Forward[] }; status: Status[]; areas: string[]; always: string[]; types: string[] }

const FILTERS = ['q', 'area', 'action', 'actor', 'ip', 'from', 'to', 'entity_type', 'entity_id'] as const;
const PAGE = 100;

/** label translates a key with a fallback. */
function label(key: string, fallback: string): string {
  const v = t(key);
  return v === key ? fallback : v;
}

export const areaLabel = (a: string) => label(`audit.area.${a}`, a);

function objectLink(e: Entry): HTMLElement | string {
  if (e.entity_id == null) return e.entity_type;
  const href = e.entity_type === 'element' ? `/e/${e.entity_id}` : e.entity_type === 'category' ? `/c/${e.entity_id}`
    : e.entity_type === 'user' && hasPerm('core.users.manage') ? `/admin/users?id=${e.entity_id}` : '';
  const text = `${label(`audit.type.${e.entity_type}`, e.entity_type)} #${e.entity_id}`;
  return href ? h('a', { href }, text) : text;
}

function pretty(v: unknown): string {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}

export async function auditAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const tab = query.get('tab') === 'settings' && hasPerm('core.audit.manage') ? 'settings' : 'log';
  const tabs = h('nav', { class: 'ew-tabs', 'aria-label': t('admin.audit') }, h('ul', null,
    h('li', null, h('a', { href: '/admin/audit', 'aria-current': tab === 'log' ? 'page' : undefined }, t('audit.tab_log'))),
    hasPerm('core.audit.manage') ? h('li', null, h('a', { href: '/admin/audit?tab=settings', 'aria-current': tab === 'settings' ? 'page' : undefined }, t('audit.tab_settings'))) : null));
  const body = h('div');
  mount(panel, pageHeader(t('admin.audit')), tabs, body);
  if (tab === 'settings') await settings(body);
  else await log(body, query);
}

async function log(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const params: Record<string, string> = {};
  for (const k of FILTERS) if (query.get(k)) params[k] = query.get(k)!;
  const offset = Number(query.get('offset') ?? 0);
  const res = await api.get<{ entries: Entry[]; total: number; areas: string[] }>(`admin/audit/search${qs({ ...params, limit: PAGE, offset })}`);
  const inputs: Record<string, HTMLInputElement | HTMLSelectElement> = {
    q: textInput({ value: params.q ?? '', type: 'search' }),
    area: select([{ value: '', label: t('audit.all') }, ...res.areas.map((a) => ({ value: a, label: areaLabel(a) }))], params.area ?? ''),
    action: textInput({ value: params.action ?? '', placeholder: 'user.update, element.*' }),
    actor: textInput({ value: params.actor ?? '', placeholder: 'name@local' }),
    ip: textInput({ value: params.ip ?? '' }),
    from: textInput({ type: 'date', value: params.from ?? '' }),
    to: textInput({ type: 'date', value: params.to ?? '' }),
    entity_type: textInput({ value: params.entity_type ?? '', placeholder: 'element' }),
    entity_id: textInput({ type: 'number', value: params.entity_id ?? '' }),
  };
  const form = h('form', { class: 'ew-form ew-panel ew-audit-filter', role: 'search', 'aria-label': t('audit.filter') },
    h('div', { class: 'ew-filter-grid' },
      field(t('audit.text'), inputs.q, { help: t('audit.text_help') }), field(t('audit.area'), inputs.area), field(t('audit.action'), inputs.action),
      field(t('audit.actor'), inputs.actor), field(t('audit.ip'), inputs.ip), field(t('audit.from'), inputs.from), field(t('audit.to'), inputs.to),
      field(t('audit.object_type'), inputs.entity_type), field(t('audit.object_id'), inputs.entity_id)),
    h('div', { class: 'ew-form-actions' }, button(t('audit.apply'), () => undefined, { type: 'submit', variant: 'primary' }),
      h('a', { href: '/admin/audit', class: 'ew-btn ew-btn--secondary' }, t('audit.reset'))));
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const p: Record<string, string> = {};
    for (const k of FILTERS) if (inputs[k].value.trim()) p[k] = inputs[k].value.trim();
    navigate(`/admin/audit${qs(p)}`);
  });
  const exportLinks = h('p', { class: 'ew-actions' }, t('audit.export'), ' ',
    h('a', { href: `/api/v1/admin/audit/export${qs({ ...params, format: 'csv' })}`, class: 'ew-btn ew-btn--secondary', download: '' }, 'CSV'),
    h('a', { href: `/api/v1/admin/audit/export${qs({ ...params, format: 'jsonl' })}`, class: 'ew-btn ew-btn--secondary', download: '' }, 'JSON Lines'));
  const rows = res.entries.map((e) => {
    const details = e.before !== undefined || e.after !== undefined || e.user_agent;
    return h('tr', null,
      h('td', null, h('time', { datetime: e.at }, fmtDate(e.at, state.lang))),
      h('td', null, e.actor, e.via ? h('div', { class: 'ew-help' }, label(`audit.via.${e.via}`, e.via), e.ip ? ` · ${e.ip}` : '') : null),
      h('th', { scope: 'row' }, label(`audit.action.${e.action}`, e.action), h('div', null, h('code', null, e.action))),
      h('td', null, objectLink(e)),
      h('td', null, details ? h('details', null, h('summary', null, t('audit.details')),
        e.user_agent ? h('p', { class: 'ew-help' }, e.user_agent) : null,
        e.before !== undefined ? h('div', null, h('strong', null, t('audit.before')), h('pre', { class: 'ew-code', tabindex: '0' }, pretty(e.before))) : null,
        e.after !== undefined ? h('div', null, h('strong', null, t('audit.after')), h('pre', { class: 'ew-code', tabindex: '0' }, pretty(e.after))) : null) : '–'));
  });
  const nav = h('nav', { class: 'ew-actions', 'aria-label': t('audit.pages') },
    offset > 0 ? h('a', { href: `/admin/audit${qs({ ...params, offset: Math.max(0, offset - PAGE) })}` }, t('common.previous')) : null,
    offset + PAGE < res.total ? h('a', { href: `/admin/audit${qs({ ...params, offset: offset + PAGE })}` }, t('common.next')) : null);
  mount(panel, form,
    h('p', { role: 'status' }, t('audit.count', { n: res.total, from: res.total ? offset + 1 : 0, to: Math.min(res.total, offset + PAGE) })),
    exportLinks,
    h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table ew-audit-table' }, h('caption', { class: 'sr-only' }, t('admin.audit')),
      h('thead', null, h('tr', null, ...[t('common.date'), t('audit.actor'), t('audit.action'), t('audit.object'), t('audit.details')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '5' }, t('audit.none')))])))),
    nav);
}

async function settings(panel: HTMLElement): Promise<void> {
  const data = await api.get<SettingsResp>('admin/audit/settings');
  const s = data.settings;
  const areaBoxes = data.areas.map((a) => {
    const always = data.always.includes(a);
    const cb = checkbox(areaLabel(a), always || !s.disabled.includes(a), { help: always ? t('audit.always') : undefined });
    if (always) cb.input.disabled = true;
    return { a, cb, always };
  });
  const logins = checkbox(t('audit.log_logins'), s.log_logins, { help: t('audit.log_logins_help') });
  const denied = checkbox(t('audit.log_denied'), s.log_denied, { help: t('audit.log_denied_help') });
  const views = checkbox(t('audit.log_views'), s.log_views, { help: t('audit.log_views_help') });
  let forward = s.forward;
  const save = async () => {
    await api.put('admin/audit/settings', {
      disabled: areaBoxes.filter((b) => !b.always && !b.cb.input.checked).map((b) => b.a),
      log_logins: logins.input.checked, log_denied: denied.input.checked, log_views: views.input.checked, forward,
    });
    toast(t('common.saved'));
    navigate('/admin/audit?tab=settings', { replace: true });
  };
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form ew-panel', novalidate: true }, alert,
    h('fieldset', null, h('legend', null, t('audit.what')), h('p', { class: 'ew-help' }, t('audit.what_help')), h('div', { class: 'ew-check-grid' }, ...areaBoxes.map((b) => b.cb.el))),
    h('fieldset', null, h('legend', null, t('audit.extra')), logins.el, denied.el, views.el),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try { await save(); } catch (e) { alert.textContent = errorText(e); }
  });
  const statusOf = (id: string) => data.status.find((x) => x.id === id);
  const fwTable = h('table', { class: 'ew-table' }, h('caption', null, t('audit.forward')),
    h('thead', null, h('tr', null, ...[t('common.name'), t('audit.fw_type'), t('audit.fw_address'), t('common.status'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...(forward.length ? forward.map((f) => {
      const st = statusOf(f.id);
      return h('tr', null, h('th', { scope: 'row' }, f.name, f.enabled ? null : h('span', { class: 'ew-badge' }, ' ', t('audit.fw_disabled'))),
        h('td', null, label(`audit.fw.${f.type}`, f.type)), h('td', null, h('code', null, f.address || '–'), f.protocol ? ` (${f.protocol})` : ''),
        h('td', null, st ? [st.last_at ? t('audit.fw_last', { date: fmtDate(st.last_at, state.lang) }) : t('audit.fw_never'), h('br'), t('audit.fw_pending', { n: st.pending }),
          st.last_error ? h('p', { class: 'ew-field-error' }, t('audit.fw_error', { n: st.errors, error: st.last_error })) : null] : '–'),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => void forwardDialog(data, f, async (nf) => { forward = forward.map((x) => (x.id === f.id ? nf : x)); await save(); }), { variant: 'link', ariaLabel: `${t('common.edit')}: ${f.name}` }),
          button(t('audit.fw_test'), async () => {
            try { await api.post('admin/audit/forward/test', f); toast(t('audit.fw_test_ok')); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('audit.fw_test')}: ${f.name}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('audit.fw_delete_confirm', { name: f.name }), t('common.delete'), true))) return;
            forward = forward.filter((x) => x.id !== f.id);
            try { await save(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${f.name}` })));
    }) : [h('tr', null, h('td', { colspan: '5' }, t('audit.fw_none')))])));
  mount(panel, form,
    h('section', { class: 'ew-panel', 'aria-labelledby': 'fw-h' }, h('h2', { id: 'fw-h' }, t('audit.forward')), h('p', null, t('audit.forward_help')), fwTable,
      button(t('audit.fw_add'), () => void forwardDialog(data, null, async (nf) => { forward = [...forward, nf]; await save(); }), { variant: 'primary' })));
}

async function forwardDialog(data: SettingsResp, f: Forward | null, done: (f: Forward) => Promise<void>): Promise<void> {
  const name = textInput({ value: f?.name ?? '', required: true });
  const type = select(data.types.map((x) => ({ value: x, label: label(`audit.fw.${x}`, x) })), f?.type ?? 'syslog');
  const address = textInput({ value: f?.address ?? '' });
  const protocol = select(['udp', 'tcp', 'tls'].map((x) => ({ value: x, label: x.toUpperCase() })), f?.protocol || 'tls');
  const secret = textInput({ value: f?.secret ?? '', type: 'password', autocomplete: 'off' });
  const headers = textArea({ value: Object.entries(f?.headers ?? {}).map(([k, v]) => `${k}: ${v}`).join('\n'), rows: 3 });
  const enabled = checkbox(t('audit.fw_enabled'), f?.enabled ?? true);
  const areaBoxes = data.areas.map((a) => ({ a, cb: checkbox(areaLabel(a), f?.areas?.includes(a) ?? false) }));
  const fProto = field(t('audit.fw_protocol'), protocol);
  const fSecret = field(t('audit.fw_secret'), secret, { help: t('audit.fw_secret_help') });
  const fHeaders = field(t('audit.fw_headers'), headers, { help: t('audit.fw_headers_help') });
  const help = h('p', { class: 'ew-help', 'aria-live': 'polite' });
  const sync = () => {
    fProto.hidden = type.value !== 'syslog';
    fSecret.hidden = type.value !== 'webhook';
    fHeaders.hidden = type.value !== 'webhook' && type.value !== 'otlp';
    address.disabled = type.value === 'stdout';
    help.textContent = t(`audit.fw_help.${type.value}`);
  };
  type.addEventListener('change', sync);
  sync();
  formDialog(f ? t('audit.fw_edit') : t('audit.fw_add'), [field(t('common.name'), name, { required: true }), field(t('audit.fw_type'), type), help,
    field(t('audit.fw_address'), address), fProto, fSecret, fHeaders, enabled.el,
    h('fieldset', null, h('legend', null, t('audit.fw_areas')), h('p', { class: 'ew-help' }, t('audit.fw_areas_help')), h('div', { class: 'ew-check-grid' }, ...areaBoxes.map((b) => b.cb.el)))],
  t('common.save'), async () => {
    const hs: Record<string, string> = {};
    for (const line of headers.value.split('\n')) {
      const i = line.indexOf(':');
      if (i > 0) hs[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    }
    await done({ id: f?.id ?? '', name: name.value.trim(), type: type.value, address: address.value.trim(), protocol: type.value === 'syslog' ? protocol.value : '',
      secret: secret.value, headers: hs, enabled: enabled.input.checked, areas: areaBoxes.filter((b) => b.cb.input.checked).map((b) => b.a) });
  });
}
