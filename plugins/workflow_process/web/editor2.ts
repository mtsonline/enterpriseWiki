// Editor parts of Workflow 2: triggers, the automatic steps notify, set,
// http, create, props and subflow, deadlines with escalation and notices.
import { api, button, categorySelect, checkbox, elementSelect, field, h, select, sourcesEditor, t, textInput, toast } from '@ewiki/sdk';
import type { Channel, Deadline, Field, Meta, Notice, NotifySpec, StepField, Trigger, TriggerStatus, TriggerType } from './types';
import type { EStep, Model } from './editor';

const BASE = 'x/workflow.process';
export const TRIGGER_TYPES: TriggerType[] = ['schedule', 'content', 'event', 'webhook'];
const CONTENT_EVENTS = ['create', 'update', 'delete', 'move', 'restore', 'properties'];
const TRIGGER_EVENTS = ['comment.create', 'form.submitted', 'workflow.completed', 'workflow.cancelled', 'event.registration', 'event.registration_cancelled'];
const RULE_OPS = ['equals', 'not_equals', 'contains', 'not_contains', 'empty', 'not_empty', 'gt', 'gte', 'lt', 'lte'];

/** Helpers of the editor (bound to its model and redraw). */
export interface Kit {
  text(get: () => string | undefined, set: (v: string) => void, opts?: { max?: number; type?: string; ph?: boolean; placeholder?: string }): HTMLInputElement;
  area(get: () => string | undefined, set: (v: string) => void, rows?: number, ph?: boolean): HTMLTextAreaElement;
  num(get: () => number | undefined, set: (v: number) => void): HTMLInputElement;
  stepSelect(get: () => number, set: (k: number) => void, self: EStep, allowNone?: boolean): HTMLSelectElement;
  placeholderHelp(): HTMLElement;
  redraw(): void;
  fields(): Field[];
  steps(): EStep[];
  meta(): Meta | null;
  elementId?: number;
}

const grid = (...c: (HTMLElement | null)[]) => h('div', { class: 'ewf-grid' }, ...c);

function sel<T extends string>(values: T[], value: string, label: (v: T) => string, onChange: (v: T) => void): HTMLSelectElement {
  const s = select(values.map((v) => ({ value: v, label: label(v) })), value);
  s.addEventListener('change', () => onChange(s.value as T));
  return s;
}

/** Rows of key → value (data of triggers and sub-workflows, values of properties, answer of HTTP). */
function mapRows(kit: Kit, obj: Record<string, string>, keyLabel: string, valueLabel: string, opts: { keys?: { value: string; label: string }[]; ph?: boolean } = {}): HTMLElement {
  const box = h('div', { class: 'ewf-map', role: 'group', 'aria-label': `${keyLabel} → ${valueLabel}` });
  const draw = () => {
    const rows = Object.entries(obj).map(([k, v], j) => {
      let key = k;
      const keyCtl = opts.keys
        ? select([{ value: '', label: t('workflow.choose') }, ...opts.keys], k)
        : textInput({ value: k, maxlength: 64 });
      const commit = (nk: string) => {
        const val = obj[key];
        delete obj[key];
        if (nk) obj[nk] = val ?? '';
        key = nk;
      };
      keyCtl.addEventListener('change', () => { commit((keyCtl as HTMLInputElement).value.trim()); draw(); });
      const valCtl = kit.text(() => v, (nv) => { if (key) obj[key] = nv; }, { max: 5000, ph: opts.ph });
      return h('div', { class: 'ewf-action-row', role: 'group', 'aria-label': t('workflow.row_n', { n: j + 1 }) },
        field(keyLabel, keyCtl), field(valueLabel, valCtl),
        button(t('common.delete'), () => { delete obj[key]; draw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${key || t('workflow.row_n', { n: j + 1 })}` }));
    });
    box.replaceChildren(...rows, button(t('workflow.add_row'), () => {
      let n = 1;
      while (`new${n}` in obj) n++;
      obj[opts.keys ? '' : `new${n}`] = '';
      draw();
      box.querySelectorAll<HTMLElement>('.ewf-action-row')[Object.keys(obj).length - 1]?.querySelector<HTMLElement>('input, select')?.focus();
    }, { variant: 'subtle' }));
  };
  draw();
  return box;
}

const fieldOptions = (kit: Kit, dateOnly = false) => kit.fields().filter((f) => f.id && (!dateOnly || f.type === 'date')).map((f) => ({ value: f.id, label: f.label || f.id }));

/** Recipients, subject, text and channel of a message. */
function messageFields(kit: Kit, n: NotifySpec): HTMLElement[] {
  return [
    grid(field(t('workflow.notify_to'), kit.text(() => n.to, (v) => { n.to = v; }, { max: 1000, ph: true }), { required: true, help: t('workflow.notify_to_help') }),
      field(t('workflow.notify_channel'), sel<Channel>(['both', 'inbox', 'email'], n.channel ?? 'both', (c) => t(`workflow.channel.${c}`), (c) => { n.channel = c; }))),
    field(t('workflow.email_subject'), kit.text(() => n.subject, (v) => { n.subject = v; }, { max: 250, ph: true }), { required: true }),
    field(t('workflow.email_body'), kit.area(() => n.body, (v) => { n.body = v; }, 4, true)),
  ];
}

const runAs = (s: EStep) => field(t('workflow.run_as'), sel(['definer', 'starter'], s.run_as ?? 'definer', (v) => t(`workflow.run_as.${v}`), (v) => { s.run_as = v; }),
  { help: t('workflow.run_as_help') });

/** Fields of the steps of Workflow 2. */
export function extraStepFields(s: EStep, kit: Kit): (HTMLElement | null)[] {
  const next = () => field(t('workflow.next'), kit.stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true });
  switch (s.type) {
    case 'notify': {
      const n = (s.notify ??= { to: 'starter', subject: '{{case.title}}', channel: 'both' });
      return [h('p', { class: 'ew-help' }, t('workflow.notify_help')), ...messageFields(kit, n), kit.placeholderHelp(), next()];
    }
    case 'set': {
      const list = (s.set ??= [{ field: '', value: '' }]);
      const rows = list.map((a, j) => {
        const mode = sel(['value', 'formula'], a.formula ? 'formula' : 'value', (v) => t(`workflow.set_mode.${v}`), (v) => {
          if (v === 'formula') { a.formula = a.value ?? ''; a.value = undefined; } else { a.value = a.formula ?? ''; a.formula = undefined; }
          kit.redraw();
        });
        const val = a.formula !== undefined
          ? field(t('workflow.set_formula'), kit.text(() => a.formula, (v) => { a.formula = v; }, { max: 1000 }), { help: t('workflow.set_formula_help') })
          : field(t('workflow.set_value'), kit.text(() => a.value, (v) => { a.value = v; }, { max: 5000, ph: true }));
        return h('div', { class: 'ewf-action-row', role: 'group', 'aria-label': t('workflow.row_n', { n: j + 1 }) },
          field(t('workflow.set_field'), kit.text(() => a.field, (v) => { a.field = v.trim(); }, { max: 40 }), { required: true, help: t('workflow.set_field_help') }),
          field(t('workflow.set_how'), mode), val,
          button(t('common.delete'), () => { list.splice(j, 1); kit.redraw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${a.field || t('workflow.row_n', { n: j + 1 })}` }));
      });
      return [h('p', { class: 'ew-help' }, t('workflow.set_help')), ...rows,
        button(t('workflow.add_row'), () => { list.push({ field: '', value: '' }); kit.redraw(); }, { variant: 'subtle' }), kit.placeholderHelp(), next()];
    }
    case 'http': {
      const x = (s.http ??= { url: '', method: 'GET', map: {} });
      x.map ??= {};
      const meta = kit.meta();
      return [h('p', { class: 'ew-help' }, t('workflow.http_help')),
        meta && !meta.http_allow ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('workflow.webhook_no_allow')) : null,
        grid(field(t('workflow.webhook_url'), kit.text(() => x.url, (v) => { x.url = v; }, { max: 2000, ph: true, placeholder: 'https://' }), { required: true, help: t('workflow.http_url_help') }),
          field(t('workflow.webhook_method'), sel(['GET', 'POST', 'PUT', 'PATCH', 'DELETE'], x.method ?? 'GET', (v) => v, (v) => { x.method = v; kit.redraw(); }))),
        x.method && x.method !== 'GET' && x.method !== 'DELETE' ? field(t('workflow.http_body'), kit.area(() => x.body, (v) => { x.body = v; }, 4, true), { help: t('workflow.http_body_help') }) : null,
        h('h3', null, t('workflow.http_map')), h('p', { class: 'ew-help' }, t('workflow.http_map_help')),
        mapRows(kit, x.map, t('workflow.set_field'), t('workflow.http_path')),
        runAs(s), kit.placeholderHelp(),
        grid(next(), field(t('workflow.next_error'), kit.stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s, true), { help: t('workflow.next_error_help') }))];
    }
    case 'create': {
      const c = (s.create ??= { category: 0, title: '{{case.title}}', body: '', store_id: 'page' });
      const catBox = h('div');
      void categorySelect({ selected: c.category || 0, perm: 'create', noneLabel: t('workflow.choose'), onChange: (id) => { c.category = id; } })
        .then((cs) => catBox.append(cs.field(t('workflow.create_category'), { required: true })));
      return [h('p', { class: 'ew-help' }, t('workflow.create_help')), catBox,
        field(t('workflow.create_title'), kit.text(() => c.title, (v) => { c.title = v; }, { max: 500, ph: true }), { required: true }),
        field(t('workflow.create_body'), kit.area(() => c.body, (v) => { c.body = v; }, 6, true), { help: t('workflow.create_body_help') }),
        grid(field(t('workflow.create_tags'), kit.text(() => c.tags, (v) => { c.tags = v; }, { max: 1000, ph: true })),
          field(t('workflow.create_store_id'), kit.text(() => c.store_id, (v) => { c.store_id = v.trim() || undefined; }, { max: 40 }), { help: t('workflow.create_store_help') }),
          field(t('workflow.create_store_url'), kit.text(() => c.store_url, (v) => { c.store_url = v.trim() || undefined; }, { max: 40 }))),
        runAs(s), kit.placeholderHelp(), next()];
    }
    case 'props': {
      const p = (s.props ??= { element: '', values: {} });
      return [h('p', { class: 'ew-help' }, t('workflow.props_help')),
        field(t('workflow.props_element'), kit.text(() => p.element, (v) => { p.element = v; }, { max: 200, ph: true, placeholder: '{{field.page}}' }), { required: true, help: t('workflow.props_element_help') }),
        h('h3', null, t('workflow.props_values')),
        mapRows(kit, p.values, t('workflow.props_key'), t('workflow.set_value'), { ph: true }),
        runAs(s), kit.placeholderHelp(), next()];
    }
    case 'parallel': {
      const p = (s.parallel ??= { branches: [] });
      s.branchKeys ??= [];
      const others = kit.steps().filter((x) => x !== s && x.key !== s.next);
      const forEach = select([{ value: '', label: t('workflow.parallel_fixed') }, ...fieldOptions(kit)], p.for_each ?? '');
      forEach.addEventListener('change', () => { p.for_each = forEach.value || undefined; kit.redraw(); });
      const branches = h('fieldset', { class: 'ewf-checks' }, h('legend', null, p.for_each ? t('workflow.parallel_branch_one') : t('workflow.parallel_branches')),
        ...others.map((x) => {
          const cb = checkbox(x.name || x.id || '?', s.branchKeys!.includes(x.key));
          cb.input.addEventListener('change', () => {
            s.branchKeys = cb.input.checked ? [...new Set([...(p.for_each ? [] : s.branchKeys!), x.key])] : s.branchKeys!.filter((k) => k !== x.key);
            if (p.for_each) kit.redraw();
          });
          return cb.el;
        }));
      return [h('p', { class: 'ew-help' }, t('workflow.parallel_help')),
        field(t('workflow.parallel_for_each'), forEach, { help: t('workflow.parallel_for_each_help') }), branches,
        field(t('workflow.parallel_require'), kit.num(() => p.require, (v) => { p.require = v || undefined; }), { help: t('workflow.parallel_require_help') }),
        grid(field(t('workflow.parallel_join'), kit.stepSelect(() => s.next, (k) => { s.next = k; kit.redraw(); }, s), { required: true, help: t('workflow.parallel_join_help') }),
          field(t('workflow.parallel_failed'), kit.stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s, true)))];
    }
    case 'subflow': {
      const f = (s.subflow ??= { workflow: 0, wait: true, data: {}, return: {} });
      f.data ??= {};
      f.return ??= {};
      const wfBox = h('div');
      void elementSelect({ selected: f.workflow || 0, type: 'workflow.process', onChange: (id) => { f.workflow = id; } })
        .then((es) => wfBox.append(field(t('workflow.subflow_workflow'), es.el)));
      const wait = checkbox(t('workflow.subflow_wait'), !!f.wait, { help: t('workflow.subflow_wait_help') });
      wait.input.addEventListener('change', () => { f.wait = wait.input.checked; kit.redraw(); });
      return [h('p', { class: 'ew-help' }, t('workflow.subflow_help')), wfBox,
        field(t('workflow.subflow_title'), kit.text(() => f.title, (v) => { f.title = v; }, { max: 200, ph: true })),
        h('h3', null, t('workflow.subflow_data')), mapRows(kit, f.data, t('workflow.subflow_their_field'), t('workflow.set_value'), { ph: true }),
        wait.el,
        f.wait ? h('h3', null, t('workflow.subflow_return')) : null,
        f.wait ? mapRows(kit, f.return, t('workflow.set_field'), t('workflow.subflow_their_field')) : null,
        runAs(s), kit.placeholderHelp(),
        grid(next(), f.wait ? field(t('workflow.subflow_next_failed'), kit.stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s, true)) : null)];
    }
  }
  return [];
}

/** Deadline, warnings and escalation of a task or approval. */
export function deadlineFields(s: EStep, kit: Kit): HTMLElement {
  const on = !!s.deadline;
  const box = h('fieldset', { class: 'ewf-row ewf-sub' }, h('legend', null, t('workflow.deadline')));
  const toggle = checkbox(t('workflow.deadline_on'), on);
  toggle.input.addEventListener('change', () => { s.deadline = toggle.input.checked ? { days: 3, warn: [24] } : undefined; kit.redraw(); });
  box.append(toggle.el);
  if (!s.deadline) return box;
  const d: Deadline = s.deadline;
  const from = select([{ value: '', label: t('workflow.deadline_from_now') }, ...fieldOptions(kit, true)], d.field ?? '');
  from.addEventListener('change', () => { d.field = from.value || undefined; });
  const work = checkbox(t('workflow.deadline_workdays'), !!d.workdays, { help: t('workflow.deadline_workdays_help') });
  work.input.addEventListener('change', () => { d.workdays = work.input.checked; });
  const warn = kit.text(() => (d.warn ?? []).join(', '), (v) => { d.warn = v.split(/[ ,;]+/).map(Number).filter((n) => n > 0); }, { max: 100 });
  const e = (d.escalate ??= { action: '' });
  const actions = [...(s.type === 'approval' ? [{ value: 'approve', label: t('workflow.approve') }, { value: 'reject', label: t('workflow.reject') }] : []),
    ...s.actions.map((a) => ({ value: a.id || a.label, label: a.label || a.id }))];
  const action = sel(['', 'notify', 'reassign', 'action', 'cancel'], e.action, (v) => t(`workflow.escalate.${v || 'none'}`), (v) => { e.action = v; kit.redraw(); });
  const detail: (HTMLElement | null)[] = [];
  if (e.action === 'reassign') {
    const as = (e.assignee ??= { type: 'group', value: '' });
    detail.push(grid(field(t('workflow.assignee'), sel(['starter', 'user', 'group', 'role'], as.type || 'group', (v) => t(`workflow.assignee.${v}`), (v) => { as.type = v; })),
      field(t('workflow.assignee_value'), kit.text(() => as.value, (v) => { as.value = v; }, { max: 200 }), { help: t('workflow.assignee_help') })));
  }
  if (e.action === 'action') {
    const a = select([{ value: '', label: t('workflow.choose') }, ...actions], e.action_id ?? '');
    a.addEventListener('change', () => { e.action_id = a.value; });
    detail.push(field(t('workflow.escalate_action'), a, { required: true }));
  }
  if (e.action) {
    const msg = (e.notify ??= { to: '', subject: '', channel: 'both' });
    detail.push(h('p', { class: 'ew-help' }, e.action === 'notify' ? t('workflow.escalate_notify_help') : t('workflow.escalate_extra_help')), ...messageFields(kit, msg));
  }
  box.append(
    grid(field(t('workflow.deadline_from'), from), field(t('workflow.delay_days'), kit.num(() => d.days, (v) => { d.days = v; })),
      field(t('workflow.delay_hours'), kit.num(() => d.hours, (v) => { d.hours = v; }))),
    work.el,
    field(t('workflow.deadline_warn'), warn, { help: t('workflow.deadline_warn_help') }),
    field(t('workflow.escalate'), action, { help: t('workflow.escalate_help') }), ...detail.filter((x): x is HTMLElement => !!x));
  return box;
}

/** Fields of the case in the form of a task or approval (field rights). */
export function stepFormFields(s: EStep, kit: Kit): HTMLElement {
  const list: StepField[] = (s.form ??= []);
  const fields = kit.fields().filter((f) => f.id);
  const mode = (f: Field) => list.find((x) => x.field === f.id)?.mode ?? '';
  const modes = (f: Field) => [{ value: '', label: t('workflow.form_mode.default') },
    ...(f.type === 'file' ? [] : [{ value: 'edit', label: t('workflow.form_mode.edit') }, { value: 'required', label: t('workflow.form_mode.required') }]),
    { value: 'read', label: t('workflow.form_mode.read') }, { value: 'hidden', label: t('workflow.form_mode.hidden') }];
  return h('details', { class: 'ewf-row ewf-sub', open: list.length > 0 },
    h('summary', null, t('workflow.step_form', { n: list.length })),
    h('p', { class: 'ew-help' }, t('workflow.step_form_help')),
    fields.length ? h('div', { class: 'ewf-grid' }, ...fields.map((f) => {
      const sel = select(modes(f), mode(f));
      sel.addEventListener('change', () => {
        const i = list.findIndex((x) => x.field === f.id);
        if (i >= 0) list.splice(i, 1);
        if (sel.value) list.push({ field: f.id, mode: sel.value as StepField['mode'] });
      });
      return field(f.label || f.id, sel);
    })) : h('p', { class: 'ew-muted' }, t('workflow.step_form_none')));
}

/** Messages when a case enters or leaves the step. */
export function noticeFields(s: EStep, kit: Kit): HTMLElement {
  const list: Notice[] = (s.notices ??= []);
  return h('details', { class: 'ewf-row ewf-sub', open: list.length > 0 },
    h('summary', null, t('workflow.notices', { n: list.length })),
    h('p', { class: 'ew-help' }, t('workflow.notices_help')),
    ...list.map((n, j) => h('fieldset', { class: 'ewf-row' }, h('legend', null, t('workflow.notice_n', { n: j + 1 })),
      field(t('workflow.notice_on'), sel(['enter', 'leave'], n.on, (v) => t(`workflow.notice_on.${v}`), (v) => { n.on = v; })),
      ...messageFields(kit, n),
      button(t('common.delete'), () => { list.splice(j, 1); kit.redraw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${t('workflow.notice_n', { n: j + 1 })}` }))),
    button(t('workflow.add_notice'), () => { list.push({ on: 'enter', to: 'assignees', subject: '{{case.title}}', channel: 'both' }); kit.redraw(); }, { variant: 'subtle' }));
}

/** The section "Start": start form or triggers only, the triggers. */
export function triggersSection(m: Model, kit: Kit): HTMLElement {
  const list: Trigger[] = (m.triggers ??= []);
  const status = h('div', { class: 'ewf-trigger-status', 'aria-live': 'polite' });
  const statusOf = new Map<string, HTMLElement>();
  if (kit.elementId) {
    void api.get<{ triggers: TriggerStatus[] }>(`${BASE}/elements/${kit.elementId}/triggers`).then((r) => {
      for (const st of r.triggers) {
        const box = statusOf.get(st.id);
        if (!box) continue;
        const parts = [st.active ? t('workflow.trigger_active') : t('workflow.trigger_inactive'), t('workflow.trigger_starts', { n: st.starts_24h })];
        if (st.next) parts.push(t('workflow.trigger_next', { at: new Date(st.next).toLocaleString() }));
        box.replaceChildren(h('p', { class: 'ew-help' }, parts.join(' · ')),
          st.last_error ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('workflow.trigger_error', { error: st.last_error })) : '',
          st.type === 'webhook' ? hookBox(st) : '');
      }
    }).catch(() => undefined);
  }
  const ro = (value: string) => { const i = textInput({ value }); i.readOnly = true; return i; };
  const hookBox = (st: TriggerStatus) => {
    const out = h('div');
    const show = (secret?: string) => out.replaceChildren(
      field(t('workflow.hook_url'), ro(st.url ?? '')),
      secret ? field(t('workflow.hook_secret'), ro(secret), { help: t('workflow.hook_secret_once') })
        : h('p', { class: 'ew-help' }, st.has_secret ? t('workflow.hook_has_secret') : t('workflow.hook_no_secret')),
      h('p', { class: 'ew-help' }, t('workflow.hook_sign_help')),
      button(st.has_secret ? t('workflow.hook_new_secret') : t('workflow.hook_create_secret'), async () => {
        try {
          const r = await api.post<{ secret: string }>(`${BASE}/elements/${kit.elementId}/triggers/${st.id}/secret`, {});
          st.has_secret = true;
          show(r.secret);
          out.querySelectorAll<HTMLInputElement>('input')[1]?.focus();
        } catch (err) { toast((err as Error).message, 'error'); }
      }, { variant: 'subtle' }));
    show();
    return out;
  };
  const manual = checkbox(t('workflow.no_manual'), !!m.no_manual, { help: t('workflow.no_manual_help') });
  manual.input.addEventListener('change', () => { m.no_manual = manual.input.checked; });
  const rows = list.map((tr, i) => {
    const legend = h('legend', null, `${tr.name || t(`workflow.trigger.${tr.type}`)} – ${t(`workflow.trigger.${tr.type}`)}`);
    const box = h('fieldset', { class: 'ewf-row ewf-trigger-row' }, legend);
    const off = checkbox(t('workflow.trigger_off'), !!tr.off);
    off.input.addEventListener('change', () => { tr.off = off.input.checked; });
    box.append(grid(
      field(t('workflow.trigger_name'), kit.text(() => tr.name, (v) => { tr.name = v; }, { max: 200 })),
      field(t('workflow.trigger_type'), sel(TRIGGER_TYPES, tr.type, (v) => t(`workflow.trigger.${v}`), (v) => { tr.type = v; if (v !== 'content' && v !== 'event') tr.run_as = 'definer'; kit.redraw(); })),
      field(t('workflow.id'), kit.text(() => tr.id, (v) => { tr.id = v.trim(); }, { max: 40 }), { help: t('workflow.id_help') })), off.el);
    switch (tr.type) {
      case 'schedule':
        box.append(field(t('workflow.trigger_cron'), kit.text(() => tr.cron, (v) => { tr.cron = v; }, { max: 100, placeholder: '0 7 * * 1-5' }), { required: true, help: t('workflow.trigger_cron_help') }));
        break;
      case 'content': {
        const src = sourcesEditor(t('workflow.trigger_sources'), tr.sources ?? '', { help: t('workflow.trigger_sources_help') });
        // read when saving (the sources editor keeps its own state)
        Object.defineProperty(tr, '_src', { value: src.get, enumerable: false, configurable: true, writable: true });
        tr.events ??= ['create'];
        const evs = h('fieldset', { class: 'ewf-checks' }, h('legend', null, t('workflow.trigger_events')),
          ...CONTENT_EVENTS.map((ev) => {
            const cb = checkbox(t(`workflow.content_event.${ev}`), tr.events!.includes(ev));
            cb.input.addEventListener('change', () => { tr.events = cb.input.checked ? [...new Set([...tr.events!, ev])] : tr.events!.filter((x) => x !== ev); });
            return cb.el;
          }));
        const prop = (tr.prop ??= { field: '', op: 'equals', value: '' });
        box.append(src.el, evs,
          h('p', { class: 'ew-help' }, t('workflow.trigger_prop_help')),
          grid(field(t('workflow.trigger_prop_key'), kit.text(() => prop.field, (v) => { prop.field = v.trim(); }, { max: 64 })),
            field(t('workflow.rule_op'), sel(RULE_OPS, prop.op || 'equals', (v) => t(`workflow.op.${v}`), (v) => { prop.op = v; })),
            field(t('workflow.rule_value'), kit.text(() => prop.value, (v) => { prop.value = v; }, { max: 500 }))));
        break;
      }
      case 'event': {
        const ev = sel(TRIGGER_EVENTS, tr.event ?? 'comment.create', (v) => t(`workflow.trigger_event.${v}`), (v) => { tr.event = v; kit.redraw(); });
        tr.event ??= 'comment.create';
        const catBox = h('div');
        void categorySelect({ selected: tr.category || 0, perm: 'view', noneLabel: t('workflow.trigger_everywhere'), onChange: (id) => { tr.category = id || undefined; } })
          .then((cs) => catBox.append(cs.field(t('workflow.trigger_category'), { help: t('workflow.trigger_category_help') })));
        box.append(field(t('workflow.trigger_event'), ev), catBox);
        if (tr.event.startsWith('workflow.')) {
          const wfBox = h('div');
          void elementSelect({ selected: tr.workflow || 0, type: 'workflow.process', onChange: (id) => { tr.workflow = id; } })
            .then((es) => wfBox.append(field(t('workflow.trigger_workflow'), es.el)));
          box.append(wfBox);
          if (tr.event === 'workflow.completed') {
            box.append(field(t('workflow.outcome'), sel(['', 'success', 'failure', 'neutral'], tr.outcome ?? '', (v) => (v ? t(`workflow.outcome.${v}`) : t('workflow.trigger_any_outcome')), (v) => { tr.outcome = v || undefined; })));
          }
        }
        break;
      }
      case 'webhook':
        box.append(field(t('workflow.trigger_allow_from'), kit.area(() => (tr.allow_from ?? []).join('\n'), (v) => { tr.allow_from = v.split(/[\n,;]+/).map((x) => x.trim()).filter(Boolean); }, 3),
          { help: t('workflow.trigger_allow_from_help') }));
        if (!kit.elementId) box.append(h('p', { class: 'ew-help' }, t('workflow.hook_save_first')));
        break;
    }
    if (tr.type === 'content' || tr.type === 'event') {
      box.append(field(t('workflow.run_as'), sel(['definer', 'actor'], tr.run_as ?? 'definer', (v) => t(`workflow.trigger_run_as.${v}`), (v) => { tr.run_as = v; }),
        { help: t('workflow.trigger_run_as_help') }));
    } else {
      box.append(h('p', { class: 'ew-help' }, t('workflow.trigger_definer_help')));
    }
    tr.data ??= {};
    box.append(field(t('workflow.trigger_title'), kit.text(() => tr.title, (v) => { tr.title = v; }, { max: 500, ph: true }), { help: t('workflow.trigger_title_help') }),
      h('h3', null, t('workflow.trigger_data')),
      mapRows(kit, tr.data, t('workflow.field'), t('workflow.set_value'), { keys: fieldOptions(kit), ph: true }));
    const st = h('div', { class: 'ewf-trigger-st' });
    statusOf.set(tr.id, st);
    box.append(st, h('div', { class: 'ew-actions' }, button(t('common.delete'), () => { list.splice(i, 1); kit.redraw(); },
      { variant: 'danger', ariaLabel: `${t('common.delete')}: ${tr.name || t(`workflow.trigger.${tr.type}`)}` })));
    return box;
  });
  const newType = select(TRIGGER_TYPES.map((x) => ({ value: x, label: t(`workflow.trigger.${x}`) })), 'schedule');
  return h('section', { class: 'ewf-ed-section', 'aria-labelledby': 'ewf-triggers-h' }, h('h2', { id: 'ewf-triggers-h' }, t('workflow.triggers')),
    h('p', { class: 'ew-help' }, t('workflow.triggers_help')), manual.el, ...rows, status,
    h('div', { class: 'ewf-add-trigger' }, field(t('workflow.trigger_new_type'), newType, { inline: true }),
      button(t('workflow.add_trigger'), () => {
        const type = newType.value as TriggerType;
        const taken = new Set(list.map((x) => x.id));
        let n = 1;
        while (taken.has(`${type}${n}`)) n++;
        list.push({ id: `${type}${n}`, type, cron: type === 'schedule' ? '0 7 * * 1-5' : undefined, events: type === 'content' ? ['create'] : undefined,
          event: type === 'event' ? 'comment.create' : undefined, data: {} });
        kit.redraw();
        document.querySelectorAll<HTMLElement>('.ewf-trigger-row')[list.length - 1]?.querySelector<HTMLElement>('input, select')?.focus();
      })));
}

/** Errors of the parts of Workflow 2 (shown before saving). */
export function validateExtra(m: Model): string | null {
  for (const tr of m.triggers ?? []) {
    if (tr.type === 'schedule' && !tr.cron?.trim()) return t('workflow.err_cron', { name: tr.name || tr.id });
    const src = (tr as Trigger & { _src?: () => string })._src?.() ?? tr.sources;
    if (tr.type === 'content' && !src?.trim()) return t('workflow.err_sources', { name: tr.name || tr.id });
    if (tr.type === 'event' && tr.event?.startsWith('workflow.') && !tr.workflow) return t('workflow.err_trigger_workflow', { name: tr.name || tr.id });
  }
  for (const s of m.steps) {
    if (s.type === 'notify' && (!s.notify?.to.trim() || !s.notify.subject.trim())) return t('workflow.err_notify', { step: s.name });
    if (s.type === 'set' && (!s.set?.length || s.set.some((a) => !a.field))) return t('workflow.err_set', { step: s.name });
    if (s.type === 'http' && !/^https?:\/\//.test(s.http?.url ?? '')) return t('workflow.err_webhook', { step: s.name });
    if (s.type === 'create' && (!s.create?.category || !s.create.title.trim())) return t('workflow.err_create', { step: s.name });
    if (s.type === 'props' && (!s.props?.element.trim() || !Object.keys(s.props.values).length)) return t('workflow.err_props', { step: s.name });
    if (s.type === 'subflow' && !s.subflow?.workflow) return t('workflow.err_subflow', { step: s.name });
    if (s.type === 'parallel' && (!s.branchKeys?.length || !s.next)) return t('workflow.err_parallel', { step: s.name });
    if (['notify', 'set', 'http', 'create', 'props', 'subflow', 'parallel'].includes(s.type) && !s.next) return t('workflow.err_next', { step: s.name });
  }
  return null;
}

/** Cleans the parts of Workflow 2 for saving. */
export function cleanTrigger(tr: Trigger): Trigger {
  const get = (tr as Trigger & { _src?: () => string })._src;
  const x: Trigger = { ...tr };
  if (get && x.type === 'content') x.sources = get();
  if (x.prop && !x.prop.field) delete x.prop;
  if (x.data) for (const [k, v] of Object.entries(x.data)) if (!k || !v.trim()) delete x.data[k];
  return x;
}

/** Options of the whole process: decision links in e-mails, retention of cases. */
export function optionsSection(m: Model): HTMLElement {
  const links = checkbox(t('workflow.links'), !!m.links, { help: t('workflow.links_help') });
  links.input.addEventListener('change', () => { m.links = links.input.checked; validity.hidden = !m.links; });
  // validity of the links: hours or days (at most 90 days; never past the deadline of the step)
  const inDays = !m.link_hours || m.link_hours % 24 === 0;
  const amount = textInput({ type: 'number', value: String(m.link_hours ? (inDays ? m.link_hours / 24 : m.link_hours) : 14) });
  amount.min = '1';
  const unit = select([{ value: 'd', label: t('workflow.links_unit.days') }, { value: 'h', label: t('workflow.links_unit.hours') }], inDays ? 'd' : 'h');
  const setValid = () => {
    const n = Math.round(Number(amount.value));
    amount.max = unit.value === 'd' ? '90' : '2160';
    m.link_hours = n > 0 ? Math.min(n * (unit.value === 'd' ? 24 : 1), 2160) : undefined;
  };
  amount.addEventListener('input', setValid);
  unit.addEventListener('change', setValid);
  setValid();
  const validity = h('div', { class: 'ewf-grid', hidden: !m.links },
    field(t('workflow.links_valid'), amount, { help: t('workflow.links_valid_help') }), field(t('workflow.links_unit'), unit));
  const days = textInput({ type: 'number', value: m.retention?.days ? String(m.retention.days) : '' });
  days.min = '1'; days.max = '36500';
  const set = () => {
    const n = Number(days.value);
    m.retention = n > 0 ? { days: Math.round(n), mode: (mode.value as 'delete' | 'anonymize') } : undefined;
  };
  const mode = select([{ value: 'anonymize', label: t('workflow.retention.anonymize') }, { value: 'delete', label: t('workflow.retention.delete') }],
    m.retention?.mode ?? 'anonymize');
  mode.addEventListener('change', set);
  days.addEventListener('input', set);
  return h('section', { class: 'ewf-ed-section', 'aria-labelledby': 'ewf-opt-h' }, h('h2', { id: 'ewf-opt-h' }, t('workflow.process_options')),
    links.el, validity,
    h('div', { class: 'ewf-grid' },
      field(t('workflow.retention.days'), days, { help: t('workflow.retention.help') }),
      field(t('workflow.retention.mode'), mode)));
}
