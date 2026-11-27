// Admin page of CrowdSec: state, blocked addresses (lift a block), allowed
// addresses and the latest alerts. Changes need the permission "Manage
// CrowdSec" and a fresh sign-in (the server checks both).
import {
  api, button, confirmDialog, errorText, field, fmtDate, h, hasPerm, mount, pageHeader, pluginSettingsForm, registerAdminPage, state, t, textInput, toast,
} from '@ewiki/coreplugin';

const BASE = 'x/security.crowdsec';

interface Status { main_site: boolean; configured: boolean; connected: boolean; error?: string; machine: string; url: string; exchange: boolean; blocked: number }
interface Decision { id: number; type: string; scope: string; value: string; origin: string; scenario: string; remaining: string; simulated: boolean }
interface Alert { id: number; scenario: string; message: string; source: string; country?: string; network?: string; events: number; decisions: number; simulated: boolean; created_at: string }
interface Entry { value: string; note: string }
interface Allowlist { entries: Entry[]; applied: string[]; exchange: boolean; in_sync: boolean; error?: string }

function table(caption: string, head: string[], rows: HTMLElement[]): HTMLElement {
  return h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table' },
    h('caption', { class: 'sr-only' }, caption),
    h('thead', null, h('tr', null, ...head.map((c) => h('th', { scope: 'col' }, c)))),
    h('tbody', null, ...rows)));
}

function notice(kind: 'info' | 'success' | 'warning', text: string): HTMLElement {
  return h('div', { class: ['ew-notice', `ew-notice--${kind}`], role: 'status' }, h('p', null, text));
}

async function render(el: HTMLElement): Promise<void> {
  const st = await api.get<Status>(`${BASE}/status`);
  const refresh = () => void render(el).catch((e) => toast(errorText(e), 'error'));
  if (!st.main_site) {
    mount(el, pageHeader(t('crowdsec.title')), h('p', null, t('crowdsec.help')), notice('info', t('crowdsec.other_site')));
    return;
  }
  const manage = hasPerm('security.crowdsec.manage');
  const form = await pluginSettingsForm('security.crowdsec', [
    { key: 'url', label: t('crowdsec.url'), placeholder: 'http://crowdsec:8080', help: t('crowdsec.url_help') },
    { key: 'machine_id', label: t('crowdsec.machine'), placeholder: 'ewiki', help: t('crowdsec.machine_help') },
    { key: 'password', label: t('crowdsec.password'), type: 'secret', help: t('crowdsec.password_help') },
  ]).catch(() => null); // without the right to change settings: no form
  const state0 = !st.configured ? notice('info', t('crowdsec.status.off'))
    : st.connected ? notice('success', t('crowdsec.status.ok', { machine: st.machine, count: String(st.blocked) }))
      : notice('warning', t('crowdsec.status.error', { error: st.error ?? '' }));
  const parts: (HTMLElement | null)[] = [pageHeader(t('crowdsec.title')), h('p', null, t('crowdsec.help')), state0,
    h('div', { class: 'ew-actions' }, button(t('crowdsec.refresh'), refresh, { variant: 'link' }))];
  if (st.connected) {
    const [decisions, allow, alerts] = await Promise.all([
      api.get<Decision[]>(`${BASE}/decisions`), api.get<Allowlist>(`${BASE}/allowlist`), api.get<Alert[]>(`${BASE}/alerts?limit=50`)]);
    parts.push(blockedSection(decisions, manage, refresh), allowSection(allow, manage, refresh), alertsSection(alerts));
  } else if (st.configured) {
    parts.push(allowSection(await api.get<Allowlist>(`${BASE}/allowlist`), manage, refresh));
  }
  if (form) parts.push(form.el);
  mount(el, ...parts);
}

function blockedSection(list: Decision[], manage: boolean, refresh: () => void): HTMLElement {
  const head = [t('crowdsec.col.address'), t('crowdsec.col.reason'), t('crowdsec.col.remaining'), t('crowdsec.col.origin')];
  if (manage) head.push(t('crowdsec.col.actions'));
  return h('section', { 'aria-labelledby': 'cs-blocked' },
    h('h2', { id: 'cs-blocked' }, t('crowdsec.blocked')),
    !list.length ? h('p', null, t('crowdsec.blocked_none')) : table(t('crowdsec.blocked'), head, list.map((d) => h('tr', null,
      h('td', null, h('code', null, d.value), d.simulated ? ` ${t('crowdsec.simulated')}` : null),
      h('td', null, d.scenario),
      h('td', null, d.remaining),
      h('td', null, d.origin),
      manage ? h('td', null, button(t('crowdsec.unblock'), async () => {
        if (!await confirmDialog(t('crowdsec.unblock_confirm', { address: d.value }), t('crowdsec.unblock'), true)) return;
        try {
          await api.del(`${BASE}/decisions/${d.id}`);
          toast(t('crowdsec.unblocked', { address: d.value }));
          refresh();
        } catch (e) { toast(errorText(e), 'error'); }
      }, { ariaLabel: t('crowdsec.unblock_label', { address: d.value }) })) : null))));
}

function allowSection(al: Allowlist, manage: boolean, refresh: () => void): HTMLElement {
  const editable = manage && al.exchange;
  const save = async (entries: Entry[]): Promise<void> => {
    await api.put(`${BASE}/allowlist`, { entries });
    toast(t('crowdsec.allow_saved'));
    refresh();
  };
  const head = [t('crowdsec.col.address'), t('crowdsec.col.note')];
  if (editable) head.push(t('crowdsec.col.actions'));
  const sync = al.error ? notice('warning', t('crowdsec.allowed_unknown', { error: al.error }))
    : !al.exchange ? notice('info', t('crowdsec.allowed_no_exchange'))
      : al.in_sync ? notice('success', t('crowdsec.allowed_sync')) : notice('warning', t('crowdsec.allowed_pending'));
  let addForm: HTMLElement | null = null;
  if (editable) {
    const value = textInput({ name: 'value', required: true, maxlength: 64, autocomplete: 'off' });
    const note = textInput({ name: 'note', maxlength: 200, autocomplete: 'off' });
    addForm = h('form', { class: 'ew-form', 'aria-labelledby': 'cs-allow-add' },
      h('h3', { id: 'cs-allow-add' }, t('crowdsec.allow_add')),
      field(t('crowdsec.allow_value'), value, { help: t('crowdsec.allow_value_help'), required: true }),
      field(t('crowdsec.allow_note'), note, { help: t('crowdsec.allow_note_help') }),
      h('div', { class: 'ew-actions' }, button(t('crowdsec.allow_add'), () => undefined, { type: 'submit', variant: 'primary' })));
    addForm.addEventListener('submit', (ev) => {
      ev.preventDefault();
      void save([...al.entries, { value: value.value.trim(), note: note.value.trim() }]).catch((e) => toast(errorText(e), 'error'));
    });
  }
  return h('section', { 'aria-labelledby': 'cs-allowed' },
    h('h2', { id: 'cs-allowed' }, t('crowdsec.allowed')),
    h('p', null, t('crowdsec.allowed_help')),
    sync,
    !al.entries.length ? h('p', null, t('crowdsec.allowed_none')) : table(t('crowdsec.allowed'), head, al.entries.map((e) => h('tr', null,
      h('td', null, h('code', null, e.value)),
      h('td', null, e.note),
      editable ? h('td', null, button(t('crowdsec.allow_remove'), () =>
        void save(al.entries.filter((x) => x.value !== e.value)).catch((err) => toast(errorText(err), 'error')),
      { ariaLabel: t('crowdsec.allow_remove_label', { address: e.value }) })) : null))),
    addForm);
}

function alertsSection(list: Alert[]): HTMLElement {
  return h('section', { 'aria-labelledby': 'cs-alerts' },
    h('h2', { id: 'cs-alerts' }, t('crowdsec.alerts')),
    !list.length ? h('p', null, t('crowdsec.alerts_none')) : table(t('crowdsec.alerts'),
      [t('crowdsec.col.time'), t('crowdsec.col.address'), t('crowdsec.col.country'), t('crowdsec.col.reason'), t('crowdsec.col.events')],
      list.map((a) => h('tr', null,
        h('td', null, a.created_at ? fmtDate(a.created_at, state.lang) : ''),
        h('td', null, h('code', null, a.source), a.simulated ? ` ${t('crowdsec.simulated')}` : null),
        h('td', null, a.country ?? ''),
        h('td', null, a.scenario || a.message),
        h('td', null, String(a.events))))));
}

export function register(): void {
  registerAdminPage({ id: 'crowdsec', area: 'security', permission: 'security.crowdsec.view', label: () => t('crowdsec.title'), order: 67, render: (el) => render(el).catch((e) => toast(errorText(e), 'error')) });
}
