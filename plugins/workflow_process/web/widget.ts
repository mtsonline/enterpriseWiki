// Interactive part of a workflow page: start a case, list cases, act on them.
import { api, button, checkbox, errorText, field, fmtDate, formDialog, h, humanSize, mount, openDialog, select, state, t, textArea, textInput, toast, uploadFile } from '@ewiki/sdk';
import type { Action, Event, Field, Instance } from './types';

export const BASE = 'x/workflow.process';

interface ListResponse { instances: Instance[]; can_start: boolean; fields: Field[]; lang: string; first_step: string; manage: boolean }

/** Label of an action (approval and system actions have default labels). */
export function actionLabel(a: Action): string {
  if (a.label) return a.label;
  const k: Record<string, string> = { approve: 'workflow.approve', reject: 'workflow.reject', _retry: 'workflow.retry', _skip: 'workflow.skip',
    _claim: 'workflow.handover.claim', _release: 'workflow.handover.release', _delegate: 'workflow.handover.delegate', _ask: 'workflow.handover.ask', _answer: 'workflow.handover.answer' };
  return k[a.id] ? t(k[a.id]) : a.id;
}

export function statusBadge(x: Instance): HTMLElement {
  if (x.status === 'open') {
    return h('span', { class: ['ew-badge', x.last_error ? 'ew-badge--warn' : 'ew-badge--info'] }, x.step_name || x.step, x.progress ? ` (${x.progress})` : '');
  }
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

function eventText(e: Event, stepName: (id: string | null) => string): string {
  const step = stepName(e.to_step);
  switch (e.action) {
    case 'start': return e.action_label.startsWith('trigger:') ? t('workflow.ev.start_trigger', { step, trigger: e.action_label.slice(8) }) : t('workflow.ev.start', { step });
    case 'notify': return t('workflow.ev.notify', { subject: e.action_label, step });
    case 'set': return t('workflow.ev.set', { step });
    case 'http': return t('workflow.ev.http', { status: e.action_label, step });
    case 'create': return t('workflow.ev.create', { title: e.action_label, step });
    case 'props': return t('workflow.ev.props', { step });
    case 'subflow': case 'subflow_started': return t('workflow.ev.subflow_started');
    case 'subflow_done': return t('workflow.ev.subflow_done', { step });
    case 'http_failed': case 'create_failed': case 'props_failed': case 'subflow_failed': return t('workflow.ev.step_failed');
    case 'warning': return t('workflow.ev.warning');
    case 'escalate': return t(`workflow.ev.escalate_${['notify', 'reassign', 'cancel'].includes(e.action_label) ? e.action_label : 'action'}`, { step });
    case 'cancel': return t('workflow.ev.cancel');
    case 'approve': return e.from_step === e.to_step ? t('workflow.ev.approve') : t('workflow.ev.action', { action: e.action_label || t('workflow.approve'), step });
    case 'reject': return t('workflow.ev.action', { action: e.action_label || t('workflow.reject'), step });
    case 'condition': return t(e.action_label === 'true' ? 'workflow.ev.condition_true' : 'workflow.ev.condition_false', { step });
    case 'email': return t('workflow.ev.email', { subject: e.action_label, step });
    case 'webhook': return t('workflow.ev.webhook', { step });
    case 'webhook_failed': return t('workflow.ev.webhook_failed');
    case 'timer': return t('workflow.ev.timer', { step });
    case 'retry': return t('workflow.ev.retry');
    case '_skip': return t('workflow.ev.action', { action: t('workflow.skip'), step });
    case '_claim': return t('workflow.ev.claim');
    case '_release': return t('workflow.ev.release');
    case '_delegate': return t('workflow.ev.delegate', { to: e.action_label });
    case '_ask': return t('workflow.ev.ask');
    case '_answer': return t('workflow.ev.answer');
    case 'parallel_started': return t('workflow.ev.parallel_started', { n: e.action_label });
    case 'parallel_done': return t('workflow.ev.parallel_done', { n: e.action_label, step });
    case 'parallel_failed': return t('workflow.ev.parallel_failed', { n: e.action_label, step });
    case 'branch_start': return e.action_label ? t('workflow.ev.branch_start_item', { item: e.action_label, step }) : t('workflow.ev.start', { step });
    case 'branch_done': return t('workflow.ev.branch_done');
    case 'branch_cancelled': return t('workflow.ev.branch_cancelled');
  }
  return t('workflow.ev.action', { action: e.action_label || e.action, step });
}

/** Opens a case in a dialog. onChange is called after an action. */
export async function openCase(id: number, onChange?: () => void): Promise<void> {
  const x = await api.get<Instance>(`${BASE}/instances/${id}`);
  const stepName = (sid: string | null) => (sid ? x.definition.steps.find((s) => s.id === sid)?.name ?? sid : '');
  const act = (a: Action | null) => {
    const note = textArea({ rows: 4 });
    const needNote = a ? a.note : 'optional';
    const title = a ? actionLabel(a) : t('workflow.cancel_case');
    const handover = !!a && a.id.startsWith('_') && a.id !== '_skip';
    // a decision brings the fields of the form of the step
    const decision = !!a && !a.id.startsWith('_') && a.id !== 'reject';
    const inputs = decision ? (x.form ?? []).filter((sf) => sf.mode === 'edit' || sf.mode === 'required').flatMap((sf) => {
      const f = x.definition.fields.find((ff) => ff.id === sf.field);
      return f ? [fieldControl({ ...f, required: sf.mode === 'required' }, x.lang, x.data[f.id])] : [];
    }) : [];
    const to = a?.id === '_delegate' ? textInput({ required: true, autocomplete: 'off', placeholder: t('workflow.handover.to_ph') }) : null;
    const run = async () => {
      const data: Record<string, unknown> = {};
      for (const c of inputs) {
        if (c.f.required && c.empty()) { c.focus(); throw new Error(t('workflow.field_missing', { field: c.f.label })); }
        const v = await c.get();
        if (v !== null && v !== '') data[c.f.id] = v;
      }
      if (to && !to.value.trim()) { to.focus(); throw new Error(t('workflow.handover.to_missing')); }
      if (a) await api.post(`${BASE}/instances/${x.id}/actions`, { action: a.id, note: note.value, to: to?.value.trim(), data });
      else await api.post(`${BASE}/instances/${x.id}/cancel`, { note: note.value });
      toast(t('workflow.done_msg', { action: title }));
      dlg.close();
      onChange?.();
    };
    formDialog(`${title}: ${x.title}`, [
      h('p', null, a ? (a.id === '_retry' ? t('workflow.retry_confirm') : handover ? t(`workflow.handover.${a.id.slice(1)}_help`)
        : t('workflow.action_confirm', { action: actionLabel(a), step: stepName(a.next) })) : t('workflow.cancel_confirm')),
      ...inputs.map((c) => c.el),
      to ? field(t('workflow.handover.to'), to, { required: true, help: t('workflow.handover.to_help') }) : null,
      needNote ? field(needNote === 'required' ? t('workflow.note_required') : t('workflow.note_optional'), note, { required: needNote === 'required' }) : null,
    ], title, async () => {
      if (needNote === 'required' && !note.value.trim()) throw new Error(t('workflow.note_missing'));
      await run();
    });
  };
  const dlg = openDialog(x.title, h('div', { class: 'ewf-case' },
    h('p', null, t('workflow.case_meta', { id: x.id, user: x.started_by || '–', date: fmtDate(x.started_at, state.lang) }), ' ', statusBadge(x)),
    x.progress ? h('p', null, t('workflow.progress', { progress: x.progress })) : null,
    x.status === 'open' && x.wake_at && !x.actions.length ? h('p', null, t('workflow.waiting_until', { date: fmtDate(x.wake_at, state.lang) })) : null,
    x.last_error ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('workflow.last_error', { error: x.last_error })) : null,
    x.handover ? h('p', null, t(`workflow.handover.state_${x.handover}`)) : null,
    x.branches?.length ? h('section', { 'aria-labelledby': `ewf-br-${x.id}` }, h('h3', { id: `ewf-br-${x.id}` }, t('workflow.branches')),
      h('ul', { class: 'ewf-branches' }, ...x.branches.map((b) => h('li', null, b.title, ' ',
        h('span', { class: ['ew-badge', b.status === 'done' ? (b.outcome === 'failure' ? 'ew-badge--warn' : 'ew-badge--ok') : b.status === 'open' ? 'ew-badge--info' : ''] },
          b.status === 'open' ? b.step_name : t(`workflow.status.${b.status}`)))))) : null,
    h('h3', null, t('workflow.data')),
    x.status === 'open' && x.due_at ? h('p', null, t(x.escalated ? 'workflow.due_escalated' : 'workflow.due_at', { date: fmtDate(x.due_at, state.lang) })) : null,
    h('dl', { class: 'ewf-data' }, ...x.definition.fields.filter((f) => f.id in x.data || !f.later).flatMap((f) => [h('dt', null, f.label), h('dd', null, valueOf(x, f))]),
      // values set by automatic steps
      ...Object.keys(x.data).filter((k) => !x.definition.fields.some((f) => f.id === k)).flatMap((k) => [h('dt', null, k), h('dd', null, String(x.data[k] ?? ''))])),
    h('h3', null, t('workflow.history')),
    h('ol', { class: 'ewf-history' }, ...(x.events ?? []).map((e) => h('li', null,
      h('time', { datetime: e.at }, fmtDate(e.at, state.lang)), ' – ', h('strong', null, e.actor === 'system' ? t('workflow.system') : e.actor), ': ',
      eventText(e, stepName),
      e.note ? h('blockquote', { class: 'ewf-note' }, e.note) : null))),
  ), [
    ...x.actions.map((a) => button(actionLabel(a), () => act(a), { variant: a.id === 'reject' ? 'danger' : 'primary' })),
    x.can_cancel ? button(t('workflow.cancel_case'), () => act(null), { variant: 'danger' }) : null,
  ], { wide: true });
}

interface Control { f: Field; el: HTMLElement; get: () => Promise<unknown>; empty: () => boolean; focus: () => void }

/** An input for a field of the case (start form and forms of steps). */
function fieldControl(f: Field, lang: string, value?: unknown): Control {
  const opts = { required: f.required, help: f.help };
  const str = value === undefined || value === null ? '' : String(value);
  switch (f.type) {
    case 'textarea': { const c = textArea({ rows: 5, lang, value: str }); return { f, el: field(f.label, c, opts), get: async () => c.value, empty: () => !c.value.trim(), focus: () => c.focus() }; }
    case 'select': {
      const c = select([{ value: '', label: t('workflow.choose') }, ...(f.options ?? []).map((o) => ({ value: o, label: o }))], str);
      return { f, el: field(f.label, c, opts), get: async () => c.value, empty: () => !c.value, focus: () => c.focus() };
    }
    case 'checkbox': {
      const c = checkbox(f.label, value === true, { help: f.help });
      if (f.required) c.input.required = true;
      return { f, el: c.el, get: async () => c.input.checked, empty: () => !c.input.checked, focus: () => c.input.focus() };
    }
    case 'file': {
      const c = h('input', { type: 'file', class: 'ew-input' });
      return { f, el: field(f.label, c, opts), empty: () => !c.files?.length, focus: () => c.focus(),
        get: async () => { const file = c.files?.[0]; if (!file) return null; const up = await uploadFile(file); return { hash: up.hash, name: up.name, mime: up.mime, size: up.size }; } };
    }
    default: {
      const type = f.type === 'email' ? 'email' : f.type === 'date' ? 'date' : f.type === 'number' ? 'number' : 'text';
      const c = textInput({ type, required: f.required, autocomplete: f.type === 'email' ? 'email' : undefined, lang, value: str });
      return { f, el: field(f.label, c, opts), get: async () => c.value, empty: () => !c.value.trim(), focus: () => c.focus() };
    }
  }
}

/** Builds the start form of a workflow. */
function startDialog(element: number, data: ListResponse, done: (id: number) => void): void {
  const controls = data.fields.map((f) => fieldControl(f, data.lang));
  formDialog(t('workflow.start'), [h('p', { class: 'ew-help' }, t('workflow.start_help', { step: data.first_step })), ...controls.map((c) => c.el)], t('workflow.submit'), async () => {
    for (const c of controls) {
      if (c.f.required && c.empty()) { c.focus(); throw new Error(t('workflow.field_missing', { field: c.f.label })); }
    }
    const values: Record<string, unknown> = {};
    for (const c of controls) {
      const v = await c.get();
      if (v !== null && v !== '') values[c.f.id] = v;
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
  const wanted = Number(new URLSearchParams(location.search).get('case'));
  await draw(wanted > 0 ? wanted : undefined);
}
