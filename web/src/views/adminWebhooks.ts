// Administration → Webhooks: notifications to Teams, Slack or JSON endpoints.
import { categorySelect } from '../components/pickers';
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, pageHeader, select, textInput } from '../components/ui';

interface Hook {
  id: number; name: string; url: string; format: 'json' | 'slack' | 'teams'; has_secret: boolean; events: string[]; category_id: number | null; enabled: boolean;
  last_status: number; last_error: string; last_at: string | null; pending: number; failed: number;
}
interface Delivery { id: number; event: string; status: 'pending' | 'delivered' | 'failed'; attempts: number; next_at: string; last_status: number; last_error: string; created_at: string; delivered_at: string | null }

const reload = () => navigate(location.pathname + location.search, { replace: true });

export async function webhooksAdmin(panel: HTMLElement): Promise<void> {
  const data = await api.get<{ webhooks: Hook[]; events: string[]; formats: string[] }>('admin/webhooks');
  const rows = data.webhooks.map((w) => h('tr', null,
    h('th', { scope: 'row' }, w.name, h('br'), h('small', { class: 'ew-muted' }, new URL(w.url).host)),
    h('td', null, t(`webhooks.format.${w.format}`)),
    h('td', null, w.events.length ? w.events.map((e) => t(`webhooks.ev.${e}`)).join(', ') : t('webhooks.events_help')),
    h('td', null, !w.enabled ? t('webhooks.disabled') : w.last_at ? `${w.last_status || '–'} · ${fmtDate(w.last_at, state.lang)}` : '–',
      w.last_error ? h('div', { class: 'ew-error-text' }, w.last_error) : null,
      w.pending || w.failed ? h('div', { class: 'ew-help' }, t('webhooks.pending_failed', { pending: w.pending, failed: w.failed })) : null),
    h('td', null, h('div', { class: 'ew-actions' },
      button(t('common.edit'), () => hookDialog(data.events, w), { variant: 'link', ariaLabel: `${t('common.edit')}: ${w.name}` }),
      button(t('webhooks.test'), async () => {
        try { await api.post(`admin/webhooks/${w.id}/test`); toast(t('webhooks.test_sent')); setTimeout(reload, 1500); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('webhooks.test')}: ${w.name}` }),
      button(t('webhooks.deliveries'), () => void deliveries(w), { variant: 'link', ariaLabel: `${t('webhooks.deliveries')}: ${w.name}` }),
      button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('webhooks.delete_confirm', { name: w.name }), t('common.delete'), true))) return;
        try { await api.del(`admin/webhooks/${w.id}`); toast(t('webhooks.deleted')); reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${w.name}` })))));
  mount(panel, pageHeader(t('admin.webhooks'), button(t('webhooks.new'), () => hookDialog(data.events, null), { variant: 'primary' })),
    h('p', null, t('webhooks.help')),
    rows.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('admin.webhooks')),
      h('thead', null, h('tr', null, ...[t('webhooks.name'), t('webhooks.format'), t('webhooks.events'), t('webhooks.last'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...rows)) : h('p', null, t('webhooks.none')));
}

async function hookDialog(events: string[], w: Hook | null): Promise<void> {
  const name = textInput({ value: w?.name ?? '', required: true, maxlength: 100 });
  const url = textInput({ value: w?.url ?? '', type: 'url', required: true, placeholder: 'https://…' });
  const format = select(['teams', 'slack', 'json'].map((f) => ({ value: f, label: t(`webhooks.format.${f}`) })), w?.format ?? 'teams');
  const boxes = events.map((e) => ({ e, c: checkbox(t(`webhooks.ev.${e}`), w?.events.includes(e) ?? false) }));
  const cat = await categorySelect({ selected: w?.category_id ?? 0, noneLabel: '–' });
  const enabled = checkbox(t('webhooks.enabled'), w?.enabled ?? true);
  const secret = select([
    ...(w?.has_secret ? [{ value: 'keep', label: t('webhooks.secret_keep') }] : [{ value: 'none', label: '–' }]),
    { value: 'generate', label: t('webhooks.secret_generate') },
    ...(w?.has_secret ? [{ value: 'remove', label: t('webhooks.secret_remove') }] : []),
  ], w?.has_secret ? 'keep' : 'none');
  const fSecret = field(t('webhooks.secret'), secret, { help: t('webhooks.secret_help') });
  const sync = () => { fSecret.hidden = format.value !== 'json'; };
  format.addEventListener('change', sync);
  formDialog(w ? t('webhooks.edit') : t('webhooks.new'), [
    field(t('webhooks.name'), name, { required: true }),
    field(t('webhooks.url'), url, { required: true }),
    field(t('webhooks.format'), format),
    fSecret,
    h('fieldset', { class: 'ew-checks' }, h('legend', null, t('webhooks.events')), h('p', { class: 'ew-help' }, t('webhooks.events_help')), ...boxes.map((b) => b.c.el)),
    cat.field(t('webhooks.category')),
    enabled.el,
  ], t('common.save'), async () => {
    const sv = secret.value;
    const body = {
      name: name.value, url: url.value.trim(), format: format.value, events: boxes.filter((b) => b.c.input.checked).map((b) => b.e),
      category_id: cat.value() || null, enabled: enabled.input.checked,
      secret: sv === 'generate' ? 'generate' : sv === 'remove' ? '' : undefined,
    };
    const r = w ? await api.put<{ secret?: string }>(`admin/webhooks/${w.id}`, body) : await api.post<{ secret?: string }>('admin/webhooks', body);
    toast(t('webhooks.saved'));
    if (r.secret) openDialog(t('webhooks.secret'), h('p', null, h('code', { class: 'ew-code' }, t('webhooks.secret_shown', { secret: r.secret }))), [], { onClose: reload });
    else reload();
  }, { wide: true });
  sync();
}

async function deliveries(w: Hook): Promise<void> {
  let list: Delivery[];
  try { list = (await api.get<{ deliveries: Delivery[] }>(`admin/webhooks/${w.id}${qs({ limit: 50 })}`)).deliveries; } catch (e) { toast(errorText(e), 'error'); return; }
  const body = list.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('webhooks.deliveries')),
    h('thead', null, h('tr', null, ...[t('webhooks.events'), t('webhooks.status'), t('webhooks.attempts'), t('webhooks.last'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...list.map((d) => h('tr', null,
      h('th', { scope: 'row' }, d.event === 'test' ? t('webhooks.event.test') : t(`webhooks.ev.${d.event}`), h('br'), h('small', null, fmtDate(d.created_at, state.lang))),
      h('td', null, t(`webhooks.st.${d.status}`), d.last_status ? ` (${d.last_status})` : '', d.last_error ? h('div', { class: 'ew-error-text' }, d.last_error) : null),
      h('td', null, String(d.attempts)),
      h('td', null, d.delivered_at ? fmtDate(d.delivered_at, state.lang) : d.status === 'pending' ? fmtDate(d.next_at, state.lang) : '–'),
      h('td', null, d.status !== 'pending' ? button(t('webhooks.retry'), async () => {
        try { await api.post(`admin/webhooks/${w.id}/deliveries/${d.id}/retry`); toast(t('webhooks.retried')); dlg.close(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('webhooks.retry')}: ${d.id}` }) : null)))))
    : h('p', null, t('webhooks.no_deliveries'));
  const dlg = openDialog(`${t('webhooks.deliveries')}: ${w.name}`, body, [], { wide: true });
}
