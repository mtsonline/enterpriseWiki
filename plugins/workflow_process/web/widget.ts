// Interactive part of a workflow page: start a case, list cases, act on them.
import { api, button, checkbox, errorText, field, fmtDate, formDialog, h, humanSize, mount, openDialog, select, state, t, textArea, textInput, toast, uploadFile } from '@ewiki/sdk';
import type { Action, Field, Instance } from './types';

export const BASE = 'x/workflow.process';

interface ListResponse { instances: Instance[]; can_start: boolean; fields: Field[]; lang: string; first_step: string; manage: boolean }

export function statusBadge(x: Instance): HTMLElement {
  if (x.status === 'open') return h('span', { class: 'ew-badge ew-badge--info' }, x.step_name || x.step);
  if (x.status === 'cancelled') return h('span', { class: 'ew-badge' }, t('workflow.status.cancelled'));
  const cls = x.outcome === 'success' ? 'ew-badge--ok' : x.outcome === 'failure' ? 'ew-badge--warn' : '';
  return h('span', { class: ['ew-badge', cls] }, x.step_name || t('workflow.status.done'));
}

function valueOf(x: Instance, f: Field): Node | string {
  const v = x.data[f.id];
  if (v === undefined || v === null || v === '') return '–';
  if (f.type === 'checkbox') return v ? t('common.yes') : t('common.no');
  if (f.type === 'file' && typeof v === 'object') {
    const file = v as { name: string; size: number };
    return h('a', { href: `/api/v1/${BASE}/instances/${x.id}/files/${encodeURIComponent(f.id)}`, download: file.name }, `${file.name} (${humanSize(file.size)})`);
  }
  if (f.type === 'textarea') return h('span', { class: 'ewf-pre' }, String(v));
  return String(v);
}

/** Opens a case in a dialog. onChange is called after an action. */
export async function openCase(id: number, onChange?: () => void): Promise<void> {
  const x = await api.get<Instance>(`${BASE}/instances/${id}`);
  const stepName = (sid: string | null) => (sid ? x.definition.steps.find((s) => s.id === sid)?.name ?? sid : '');
  const act = (a: Action | null) => {
    const note = textArea({ rows: 4 });
    const needNote = a ? a.note : 'optional';
    const title = a ? a.label : t('workflow.cancel_case');
    const run = async () => {
      if (a) await api.post(`${BASE}/instances/${x.id}/actions`, { action: a.id, note: note.value });
      else await api.post(`${BASE}/instances/${x.id}/cancel`, { note: note.value });
      toast(t('workflow.done_msg', { action: title }));
      dlg.close();
      onChange?.();
    };
    formDialog(`${title}: ${x.title}`, [
      h('p', null, a ? t('workflow.action_confirm', { action: a.label, step: stepName(a.next) }) : t('workflow.cancel_confirm')),
      needNote ? field(needNote === 'required' ? t('workflow.note_required') : t('workflow.note_optional'), note, { required: needNote === 'required' }) : null,
    ], title, async () => {
      if (needNote === 'required' && !note.value.trim()) throw new Error(t('workflow.note_missing'));
      await run();
    });
  };
  const dlg = openDialog(x.title, h('div', { class: 'ewf-case' },
    h('p', null, t('workflow.case_meta', { id: x.id, user: x.started_by || '–', date: fmtDate(x.started_at, state.lang) }), ' ', statusBadge(x)),
    h('h3', null, t('workflow.data')),
    h('dl', { class: 'ewf-data' }, ...x.definition.fields.flatMap((f) => [h('dt', null, f.label), h('dd', null, valueOf(x, f))])),
    h('h3', null, t('workflow.history')),
    h('ol', { class: 'ewf-history' }, ...(x.events ?? []).map((e) => h('li', null,
      h('time', { datetime: e.at }, fmtDate(e.at, state.lang)), ' – ', h('strong', null, e.actor), ': ',
      e.action === 'start' ? t('workflow.ev.start', { step: stepName(e.to_step) })
        : e.action === 'cancel' ? t('workflow.ev.cancel')
          : t('workflow.ev.action', { action: e.action_label || e.action, step: stepName(e.to_step) }),
      e.note ? h('blockquote', { class: 'ewf-note' }, e.note) : null))),
  ), [
    ...x.actions.map((a) => button(a.label, () => act(a), { variant: 'primary' })),
    x.can_cancel ? button(t('workflow.cancel_case'), () => act(null), { variant: 'danger' }) : null,
  ], { wide: true });
}

/** Builds the start form of a workflow. */
function startDialog(element: number, data: ListResponse, done: (id: number) => void): void {
  const getters: { f: Field; get: () => Promise<unknown> }[] = [];
  const controls = data.fields.map((f) => {
    const opts = { required: f.required, help: f.help };
    switch (f.type) {
      case 'textarea': { const c = textArea({ rows: 5, lang: data.lang }); getters.push({ f, get: async () => c.value }); return field(f.label, c, opts); }
      case 'select': { const c = select([{ value: '', label: t('workflow.choose') }, ...(f.options ?? []).map((o) => ({ value: o, label: o }))], ''); getters.push({ f, get: async () => c.value }); return field(f.label, c, opts); }
      case 'checkbox': { const c = checkbox(f.label, false, { help: f.help }); if (f.required) c.input.required = true; getters.push({ f, get: async () => c.input.checked }); return c.el; }
      case 'file': {
        const c = h('input', { type: 'file', class: 'ew-input' });
        getters.push({ f, get: async () => { const file = c.files?.[0]; if (!file) return null; const up = await uploadFile(file); return { hash: up.hash, name: up.name, mime: up.mime, size: up.size }; } });
        return field(f.label, c, opts);
      }
      default: {
        const type = f.type === 'email' ? 'email' : f.type === 'date' ? 'date' : f.type === 'number' ? 'number' : 'text';
        const c = textInput({ type, required: f.required, autocomplete: f.type === 'email' ? 'email' : undefined, lang: data.lang });
        getters.push({ f, get: async () => c.value });
        return field(f.label, c, opts);
      }
    }
  });
  formDialog(t('workflow.start'), [h('p', { class: 'ew-help' }, t('workflow.start_help', { step: data.first_step })), ...controls], t('workflow.submit'), async () => {
    for (const g of getters) {
      if (!g.f.required) continue;
      const el = controls[getters.indexOf(g)].querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select');
      const empty = g.f.type === 'checkbox' ? !(el as HTMLInputElement).checked : g.f.type === 'file' ? !(el as HTMLInputElement).files?.length : !el?.value.trim();
      if (empty) { el?.focus(); throw new Error(t('workflow.field_missing', { field: g.f.label })); }
    }
    const values: Record<string, unknown> = {};
    for (const g of getters) {
      const v = await g.get();
      if (v !== null && v !== '') values[g.f.id] = v;
    }
    const x = await api.post<Instance>(`${BASE}/elements/${element}/instances`, { data: values, lang: data.lang });
    toast(t('workflow.started'));
    done(x.id);
  }, { wide: true });
}

/** Widget: data-ew-widget="workflow" data-element="ID". */
export async function workflowWidget(el: HTMLElement): Promise<void> {
  const element = Number(el.dataset.element);
  let filter = 'open';
  const draw = async (openId?: number) => {
    let data: ListResponse;
    try {
      data = await api.get<ListResponse>(`${BASE}/elements/${element}/instances?status=${filter}&lang=${state.lang}`);
    } catch (e) {
      mount(el, h('p', { class: 'ew-field-error' }, errorText(e)));
      return;
    }
    const filterSel = select([{ value: 'open', label: t('workflow.filter.open') }, { value: '', label: t('workflow.filter.all') }], filter);
    filterSel.addEventListener('change', () => { filter = filterSel.value; void draw(); });
    const rows = data.instances.map((x) => h('tr', null,
      h('th', { scope: 'row' }, button(x.title, () => void openCase(x.id, () => void draw()).catch((e) => toast(errorText(e), 'error')), { variant: 'link' })),
      h('td', null, statusBadge(x)), h('td', null, x.started_by || '–'), h('td', null, fmtDate(x.updated_at, state.lang)),
      h('td', null, x.actions.length ? h('span', { class: 'ew-badge ew-badge--warn' }, t('workflow.your_turn')) : '')));
    mount(el, h('section', { class: 'ewf-cases', 'aria-labelledby': `ewf-h-${element}` },
      h('div', { class: 'ew-page-header' }, h('h2', { id: `ewf-h-${element}` }, t('workflow.cases')),
        data.can_start ? button(t('workflow.start'), () => startDialog(element, data, (id) => void draw(id)), { variant: 'primary' })
          : h('a', { href: `/login?next=${encodeURIComponent(location.pathname)}` }, t('workflow.login'))),
      state.user ? field(t('workflow.filter'), filterSel, { inline: true }) : null,
      state.user ? (rows.length
        ? h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('workflow.cases')),
          h('thead', null, h('tr', null, ...[t('workflow.case'), t('common.status'), t('workflow.started_by'), t('workflow.updated'), ''].map((x) => h('th', { scope: 'col' }, x)))),
          h('tbody', null, ...rows)))
        : h('p', { class: 'ew-muted' }, t('workflow.no_cases'))) : null));
    if (openId) await openCase(openId, () => void draw()).catch(() => undefined);
  };
  await draw();
}
