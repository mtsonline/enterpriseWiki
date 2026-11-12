// Structured editor for workflow definitions: start form, steps of all
// types (task, approval, condition, e-mail, webhook, waiting time, end).
import { api, button, checkbox, confirmDialog, field, h, select, t, textArea, textInput, type EditorContext, type EditorInstance } from '@ewiki/sdk';
import { TEMPLATES } from './templates';
import type { Action, Definition, Field, Meta, Rule, Step, StepType } from './types';

// Internal model: steps reference each other by a stable key, so renaming a
// step id does not break the "next step" choices.
interface EAction extends Omit<Action, 'next'> { next: number }
interface EStep extends Omit<Step, 'actions' | 'next' | 'next_else' | 'type'> { key: number; type: StepType; actions: EAction[]; next: number; nextElse: number }
interface Model { description: string; fields: Field[]; steps: EStep[] }

let seq = 0;
const BASE = 'x/workflow.process';
const STEP_TYPES: StepType[] = ['task', 'approval', 'condition', 'email', 'webhook', 'delay', 'end'];
const RULE_OPS = ['equals', 'not_equals', 'contains', 'not_contains', 'empty', 'not_empty', 'gt', 'gte', 'lt', 'lte'];
const FIELD_TYPES = ['text', 'textarea', 'email', 'date', 'number', 'select', 'checkbox', 'file'];
const PLACEHOLDERS = ['case.title', 'case.id', 'case.url', 'case.step', 'workflow.title', 'starter.name', 'starter.email', 'actor.name', 'actor.email',
  'last.action', 'last.note', 'site.name', 'date'];

function typeOf(s: Step): StepType {
  return s.type || (s.actions?.length ? 'task' : 'end');
}

function toModel(d: Definition): Model {
  const keys = new Map<string, number>();
  const steps: EStep[] = d.steps.map((s) => {
    const key = ++seq;
    keys.set(s.id, key);
    return { ...s, key, type: typeOf(s), actions: [], next: 0, nextElse: 0, rules: (s.rules ?? []).map((r) => ({ ...r })),
      email: s.email ? { ...s.email } : undefined, webhook: s.webhook ? { ...s.webhook } : undefined, delay: s.delay ? { ...s.delay } : undefined,
      assignee: s.assignee?.type ? { ...s.assignee } : { type: 'group', value: '' } };
  });
  d.steps.forEach((s, i) => {
    steps[i].actions = (s.actions ?? []).map((a) => ({ ...a, next: keys.get(a.next) ?? 0 }));
    steps[i].next = keys.get(s.next ?? '') ?? 0;
    steps[i].nextElse = keys.get(s.next_else ?? '') ?? 0;
  });
  return { description: d.description ?? '', fields: (d.fields ?? []).map((f) => ({ ...f, options: f.options ? [...f.options] : [] })), steps };
}

export function slug(s: string, taken: Set<string>, fallback: string): string {
  let base = s.toLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '').replace(/ß/g, 'ss')
    .replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 30);
  if (!/^[a-z0-9]/.test(base)) base = fallback;
  let id = base, n = 2;
  while (taken.has(id)) id = `${base}_${n++}`;
  taken.add(id);
  return id;
}

function uniqueId(raw: string, label: string, taken: Set<string>, fallback: string): string {
  const id = raw.trim();
  if (id && !taken.has(id)) { taken.add(id); return id; }
  return slug(label, taken, fallback);
}

function toDefinition(m: Model): Definition {
  const fieldIds = new Set<string>();
  const fields = m.fields.map((f, i) => ({ ...f, id: uniqueId(f.id, f.label, fieldIds, `field${i + 1}`),
    options: f.type === 'select' ? (f.options ?? []).map((o) => o.trim()).filter(Boolean) : undefined }));
  const stepIds = new Set<string>();
  const ids = new Map<number, string>();
  m.steps.forEach((s, i) => ids.set(s.key, uniqueId(s.id, s.name, stepIds, `step${i + 1}`)));
  const ref = (k: number) => ids.get(k) ?? '';
  const steps: Step[] = m.steps.map((s) => {
    const base: Step = { id: ids.get(s.key)!, name: s.name, type: s.type, description: s.description, assignee: { type: '', value: '' }, actions: [] };
    switch (s.type) {
      case 'end': return { ...base, outcome: s.outcome || 'neutral' };
      case 'task': {
        const aIds = new Set<string>();
        return { ...base, assignee: s.assignee, due_days: s.due_days || undefined,
          actions: s.actions.map((a, j) => ({ id: uniqueId(a.id, a.label, aIds, `action${j + 1}`), label: a.label, next: ref(a.next), note: a.note || undefined })) };
      }
      case 'approval': return { ...base, assignee: s.assignee, due_days: s.due_days || undefined, required: s.required || 0,
        approve_label: s.approve_label || undefined, reject_label: s.reject_label || undefined, next: ref(s.next), next_else: ref(s.nextElse) };
      case 'condition': return { ...base, match: s.match || 'all', rules: s.rules ?? [], next: ref(s.next), next_else: ref(s.nextElse) };
      case 'email': return { ...base, email: s.email, next: ref(s.next) };
      case 'webhook': return { ...base, webhook: s.webhook, next: ref(s.next), next_else: ref(s.nextElse) || undefined };
      case 'delay': return { ...base, delay: s.delay, next: ref(s.next) };
    }
    return base;
  });
  return { description: m.description, fields, steps };
}

/** Converts a step to another type, keeping what fits. */
function retype(s: EStep, type: StepType): void {
  s.type = type;
  if (type === 'task' && !s.actions.length) s.actions = [{ id: '', label: '', next: 0 }];
  if ((type === 'task' || type === 'approval') && !s.assignee.type) s.assignee = { type: 'group', value: '' };
  if (type === 'condition' && !s.rules?.length) s.rules = [{ field: '', op: 'equals', value: '' }];
  if (type === 'email' && !s.email) s.email = { to: '{{starter.email}}', subject: '{{case.title}}', body: '' };
  if (type === 'webhook' && !s.webhook) s.webhook = { url: '', method: 'POST' };
  if (type === 'delay' && !s.delay) s.delay = { days: 1 };
  if (type === 'end' && !s.outcome) s.outcome = 'success';
}

export function createWorkflowEditor(container: HTMLElement, body: unknown, ctx: EditorContext): EditorInstance {
  let m = toModel((body ?? { description: '', fields: [], steps: [] }) as Definition);
  let meta: Meta | null = null;
  const root = h('div', { class: 'ewf-editor' });
  container.append(root);
  let dirty = false; // ask before a template replaces user input
  root.addEventListener('input', (ev) => { if (!(ev.target as Element).closest('.ewf-template')) dirty = true; });
  // the last focused text field receives placeholders
  let lastText: HTMLInputElement | HTMLTextAreaElement | null = null;
  root.addEventListener('focusin', (ev) => {
    const el = ev.target as HTMLElement;
    if (el.matches('.ewf-ph-target')) lastText = el as HTMLInputElement;
  });
  void api.get<Meta>(`${BASE}/meta`).then((x) => { meta = x; draw(); }).catch(() => undefined);

  const moveBtns = <T>(list: T[], i: number, what: string, refocus: (i: number) => void) => [
    button(h('span', { 'aria-hidden': 'true' }, '↑'), () => { [list[i - 1], list[i]] = [list[i], list[i - 1]]; draw(); refocus(i - 1); },
      { variant: 'subtle', ariaLabel: t('workflow.move_up', { what }), disabled: i === 0 }),
    button(h('span', { 'aria-hidden': 'true' }, '↓'), () => { [list[i + 1], list[i]] = [list[i], list[i + 1]]; draw(); refocus(i + 1); },
      { variant: 'subtle', ariaLabel: t('workflow.move_down', { what }), disabled: i === list.length - 1 }),
    button(t('common.delete'), () => { list.splice(i, 1); draw(); refocus(Math.min(i, list.length - 1)); }, { variant: 'danger', ariaLabel: `${t('common.delete')}: ${what}` }),
  ];
  const focusIn = (sel: string) => (i: number) => root.querySelectorAll<HTMLElement>(sel)[i]?.querySelector<HTMLElement>('input, select, textarea')?.focus();

  // inputs bound to model properties
  const text = (get: () => string | undefined, set: (v: string) => void, opts: { max?: number; type?: string; ph?: boolean; placeholder?: string } = {}) => {
    const i = textInput({ value: get() ?? '', maxlength: opts.max ?? 200, type: opts.type, placeholder: opts.placeholder, lang: opts.type ? undefined : ctx.lang });
    if (opts.ph) i.classList.add('ewf-ph-target');
    i.addEventListener('input', () => set(i.value));
    return i;
  };
  const area = (get: () => string | undefined, set: (v: string) => void, rows = 4, ph = false) => {
    const a = textArea({ value: get() ?? '', rows, lang: ctx.lang });
    if (ph) a.classList.add('ewf-ph-target');
    a.addEventListener('input', () => set(a.value));
    return a;
  };
  const num = (get: () => number | undefined, set: (v: number) => void) => {
    const i = textInput({ value: String(get() ?? 0), type: 'number' });
    i.min = '0';
    i.addEventListener('input', () => set(Math.max(0, Number(i.value) || 0)));
    return i;
  };
  const stepSelect = (get: () => number, set: (k: number) => void, self: EStep, allowNone = false) => {
    const sel = select([{ value: '0', label: allowNone ? t('workflow.none') : t('workflow.choose') },
      ...m.steps.filter((x) => x !== self).map((x) => ({ value: String(x.key), label: x.name || x.id || '?' }))], String(get()));
    sel.addEventListener('change', () => set(Number(sel.value)));
    return sel;
  };

  const fieldRow = (f: Field, i: number): HTMLElement => {
    const legend = h('legend', null, f.label || t('workflow.field_n', { n: i + 1 }));
    const label = text(() => f.label, (v) => { f.label = v; legend.textContent = v || t('workflow.field_n', { n: i + 1 }); });
    const id = text(() => f.id, (v) => { f.id = v; }, { max: 40 });
    const type = select(FIELD_TYPES.map((x) => ({ value: x, label: t(`workflow.type.${x}`) })), f.type || 'text');
    type.addEventListener('change', () => { f.type = type.value; draw(); focusIn('.ewf-field-row')(i); });
    const req = checkbox(t('workflow.required'), !!f.required);
    req.input.addEventListener('change', () => { f.required = req.input.checked; });
    const opts = area(() => (f.options ?? []).join('\n'), (v) => { f.options = v.split('\n'); }, 3);
    return h('fieldset', { class: 'ewf-row ewf-field-row' }, legend,
      h('div', { class: 'ewf-grid' }, field(t('workflow.label'), label, { required: true }), field(t('workflow.type'), type),
        field(t('workflow.id'), id, { help: t('workflow.id_help') }), field(t('workflow.help_text'), text(() => f.help, (v) => { f.help = v; }, { max: 500 }))),
      f.type === 'select' ? field(t('workflow.options'), opts, { help: t('workflow.options_help'), required: true }) : null,
      req.el, h('div', { class: 'ew-actions' }, ...moveBtns(m.fields, i, f.label || t('workflow.field_n', { n: i + 1 }), focusIn('.ewf-field-row'))));
  };

  const actionRow = (s: EStep, a: EAction, j: number): HTMLElement => {
    const note = select([{ value: '', label: t('workflow.note.none') }, { value: 'optional', label: t('workflow.note.optional') }, { value: 'required', label: t('workflow.note.required') }], a.note ?? '');
    note.addEventListener('change', () => { a.note = note.value as EAction['note']; });
    return h('div', { class: 'ewf-action-row', role: 'group', 'aria-label': t('workflow.action_n', { n: j + 1 }) },
      field(t('workflow.action_label'), text(() => a.label, (v) => { a.label = v; }, { max: 100 }), { required: true }),
      field(t('workflow.next'), stepSelect(() => a.next, (k) => { a.next = k; }, s), { required: true }), field(t('workflow.note'), note),
      button(t('common.delete'), () => { s.actions.splice(j, 1); draw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${a.label || t('workflow.action_n', { n: j + 1 })}` }));
  };

  const assigneeFields = (s: EStep) => {
    const at = select(['starter', 'user', 'group', 'role'].map((x) => ({ value: x, label: t(`workflow.assignee.${x}`) })), s.assignee.type || 'group');
    const av = textInput({ value: s.assignee.value, maxlength: 200, placeholder: t('workflow.assignee_value_ph') });
    at.addEventListener('change', () => { s.assignee = { type: at.value as EStep['assignee']['type'], value: av.value }; av.disabled = at.value === 'starter'; });
    av.addEventListener('input', () => { s.assignee = { type: at.value as EStep['assignee']['type'], value: av.value }; });
    av.disabled = at.value === 'starter';
    return h('div', { class: 'ewf-grid' }, field(t('workflow.assignee'), at), field(t('workflow.assignee_value'), av, { help: t('workflow.assignee_help') }),
      field(t('workflow.due_days'), num(() => s.due_days, (v) => { s.due_days = v; }), { help: t('workflow.due_days_help') }));
  };

  const placeholderHelp = () => h('details', { class: 'ewf-ph' }, h('summary', null, t('workflow.placeholders')),
    h('p', { class: 'ew-help' }, t('workflow.placeholders_help')),
    h('ul', { class: 'ewf-ph-list' }, ...[...PLACEHOLDERS, ...m.fields.filter((f) => f.id || f.label).map((f) => `field.${f.id || slug(f.label, new Set(), 'field')}`)].map((p) =>
      h('li', null, button(`{{${p}}}`, () => {
        const target = lastText;
        if (!target) return;
        const pos = target.selectionStart ?? target.value.length;
        target.setRangeText(`{{${p}}}`, pos, target.selectionEnd ?? pos, 'end');
        target.dispatchEvent(new Event('input', { bubbles: true }));
        target.focus();
      }, { variant: 'link', ariaLabel: t('workflow.insert_placeholder', { p }) })))));

  const ruleRow = (s: EStep, r: Rule, j: number): HTMLElement => {
    const fieldSel = select([{ value: '', label: t('workflow.choose') }, { value: 'case.title', label: t('workflow.case_title') },
      ...m.fields.filter((f) => f.id).map((f) => ({ value: f.id, label: f.label || f.id }))], r.field);
    fieldSel.addEventListener('change', () => { r.field = fieldSel.value; });
    const op = select(RULE_OPS.map((x) => ({ value: x, label: t(`workflow.op.${x}`) })), r.op || 'equals');
    const value = text(() => r.value, (v) => { r.value = v; }, { max: 500 });
    const hideValue = () => { value.closest('.ew-field')?.toggleAttribute('hidden', op.value === 'empty' || op.value === 'not_empty'); };
    op.addEventListener('change', () => { r.op = op.value; hideValue(); });
    const row = h('div', { class: 'ewf-action-row', role: 'group', 'aria-label': t('workflow.rule_n', { n: j + 1 }) },
      field(t('workflow.rule_field'), fieldSel, { required: true }), field(t('workflow.rule_op'), op), field(t('workflow.rule_value'), value),
      button(t('common.delete'), () => { s.rules!.splice(j, 1); draw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${t('workflow.rule_n', { n: j + 1 })}` }));
    queueMicrotask(hideValue);
    return row;
  };

  const typeFields = (s: EStep, i: number): (HTMLElement | null)[] => {
    switch (s.type) {
      case 'end': {
        const outcome = select(['success', 'failure', 'neutral'].map((x) => ({ value: x, label: t(`workflow.outcome.${x}`) })), s.outcome || 'neutral');
        outcome.addEventListener('change', () => { s.outcome = outcome.value; });
        return [h('p', { class: 'ew-help' }, t('workflow.final_help')), field(t('workflow.outcome'), outcome)];
      }
      case 'task':
        return [assigneeFields(s), h('h4', null, t('workflow.actions')), ...s.actions.map((a, j) => actionRow(s, a, j)),
          button(t('workflow.add_action'), () => {
            s.actions.push({ id: '', label: '', next: 0 }); draw();
            root.querySelectorAll<HTMLElement>('.ewf-step-row')[i]?.querySelectorAll<HTMLInputElement>('.ewf-action-row input')[s.actions.length - 1]?.focus();
          })];
      case 'approval':
        return [h('p', { class: 'ew-help' }, t('workflow.approval_help')), assigneeFields(s),
          h('div', { class: 'ewf-grid' },
            field(t('workflow.required_approvals'), num(() => s.required, (v) => { s.required = v; }), { help: t('workflow.required_approvals_help') }),
            field(t('workflow.approve_label'), text(() => s.approve_label, (v) => { s.approve_label = v; }, { max: 100, placeholder: t('workflow.approve') })),
            field(t('workflow.reject_label'), text(() => s.reject_label, (v) => { s.reject_label = v; }, { max: 100, placeholder: t('workflow.reject') }))),
          h('div', { class: 'ewf-grid' },
            field(t('workflow.next_approved'), stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true }),
            field(t('workflow.next_rejected'), stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s), { required: true }))];
      case 'condition': {
        const match = select([{ value: 'all', label: t('workflow.match.all') }, { value: 'any', label: t('workflow.match.any') }], s.match || 'all');
        match.addEventListener('change', () => { s.match = match.value as 'all' | 'any'; });
        s.rules ??= [];
        return [h('p', { class: 'ew-help' }, t('workflow.condition_help')), field(t('workflow.match'), match),
          ...s.rules.map((r, j) => ruleRow(s, r, j)),
          button(t('workflow.add_rule'), () => { s.rules!.push({ field: '', op: 'equals', value: '' }); draw(); }),
          h('div', { class: 'ewf-grid' },
            field(t('workflow.next_true'), stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true }),
            field(t('workflow.next_false'), stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s), { required: true }))];
      }
      case 'email': {
        const e = (s.email ??= { to: '', subject: '', body: '' });
        return [h('p', { class: 'ew-help' }, t('workflow.email_help')),
          meta && !meta.mail_configured ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('workflow.mail_not_configured')) : null,
          h('div', { class: 'ewf-grid' },
            field(t('workflow.email_to'), text(() => e.to, (v) => { e.to = v; }, { max: 1000, ph: true }), { required: true, help: t('workflow.email_to_help') }),
            field(t('workflow.email_cc'), text(() => e.cc, (v) => { e.cc = v; }, { max: 1000, ph: true })),
            field(t('workflow.email_from'), text(() => e.from, (v) => { e.from = v; }, { max: 1000, ph: true }), { help: t('workflow.email_from_help') }),
            field(t('workflow.email_reply_to'), text(() => e.reply_to, (v) => { e.reply_to = v; }, { max: 1000, ph: true }))),
          field(t('workflow.email_subject'), text(() => e.subject, (v) => { e.subject = v; }, { max: 250, ph: true }), { required: true }),
          field(t('workflow.email_body'), area(() => e.body, (v) => { e.body = v; }, 8, true), { required: true }),
          placeholderHelp(),
          field(t('workflow.next'), stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true })];
      }
      case 'webhook': {
        const w = (s.webhook ??= { url: '', method: 'POST' });
        const method = select([{ value: 'POST', label: 'POST' }, { value: 'PUT', label: 'PUT' }], w.method || 'POST');
        method.addEventListener('change', () => { w.method = method.value; });
        return [h('p', { class: 'ew-help' }, t('workflow.webhook_help')),
          meta && !meta.http_allow ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('workflow.webhook_no_allow')) : null,
          h('div', { class: 'ewf-grid' },
            field(t('workflow.webhook_url'), text(() => w.url, (v) => { w.url = v; }, { max: 1000, type: 'url', placeholder: 'https://' }), { required: true }),
            field(t('workflow.webhook_method'), method),
            field(t('workflow.webhook_secret'), text(() => w.secret, (v) => { w.secret = v; }, { max: 200 }), { help: t('workflow.webhook_secret_help') })),
          h('div', { class: 'ewf-grid' },
            field(t('workflow.next'), stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true }),
            field(t('workflow.next_error'), stepSelect(() => s.nextElse, (k) => { s.nextElse = k; }, s, true), { help: t('workflow.next_error_help') }))];
      }
      case 'delay': {
        const d = (s.delay ??= { days: 1 });
        const until = select([{ value: '', label: t('workflow.delay_now') }, ...m.fields.filter((f) => f.type === 'date' && f.id).map((f) => ({ value: f.id, label: f.label }))], d.until ?? '');
        until.addEventListener('change', () => { d.until = until.value || undefined; });
        return [h('p', { class: 'ew-help' }, t('workflow.delay_help')),
          h('div', { class: 'ewf-grid' }, field(t('workflow.delay_from'), until),
            field(t('workflow.delay_days'), num(() => d.days, (v) => { d.days = v; })),
            field(t('workflow.delay_hours'), num(() => d.hours, (v) => { d.hours = v; }))),
          field(t('workflow.next'), stepSelect(() => s.next, (k) => { s.next = k; }, s), { required: true })];
      }
    }
    return [];
  };

  const stepRow = (s: EStep, i: number): HTMLElement => {
    const legend = h('legend', null, `${i + 1}. ${s.name} – ${t(`workflow.steptype.${s.type}`)}`);
    const name = text(() => s.name, (v) => { s.name = v; legend.textContent = `${i + 1}. ${v} – ${t(`workflow.steptype.${s.type}`)}`; });
    name.addEventListener('change', () => draw()); // refresh "next step" lists
    const type = select(STEP_TYPES.map((x) => ({ value: x, label: t(`workflow.steptype.${x}`) })), s.type);
    type.addEventListener('change', () => { retype(s, type.value as StepType); draw(); root.querySelectorAll<HTMLSelectElement>('.ewf-step-type')[i]?.focus(); });
    type.classList.add('ewf-step-type');
    return h('fieldset', { class: ['ewf-row', 'ewf-step-row', `ewf-step-row--${s.type}`] }, legend,
      h('div', { class: 'ewf-grid' }, field(t('workflow.step_name'), name, { required: true }), field(t('workflow.step_type'), type),
        field(t('workflow.id'), text(() => s.id, (v) => { s.id = v; }, { max: 40 }), { help: t('workflow.id_help') })),
      field(t('workflow.step_desc'), text(() => s.description, (v) => { s.description = v; }, { max: 2000 })),
      ...typeFields(s, i),
      h('div', { class: 'ew-actions' }, ...moveBtns(m.steps, i, s.name || t('workflow.step_n', { n: i + 1 }), focusIn('.ewf-step-row'))));
  };

  const draw = () => {
    const desc = area(() => m.description, (v) => { m.description = v; }, 4);
    desc.setAttribute('aria-labelledby', ctx.labelId);
    const tpl = select([{ value: '', label: t('workflow.template_choose') }, ...TEMPLATES.map((x) => ({ value: x.id, label: t(`workflow.template.${x.id}`) }))], '');
    const newType = select(STEP_TYPES.map((x) => ({ value: x, label: t(`workflow.steptype.${x}`) })), 'task');
    root.replaceChildren(
      h('div', { class: 'ewf-template' }, field(t('workflow.template'), tpl, { inline: true }),
        button(t('workflow.template_apply'), async () => {
          const tp = TEMPLATES.find((x) => x.id === tpl.value);
          if (!tp) return;
          if (dirty && !(await confirmDialog(t('workflow.template_confirm'), t('workflow.template_apply')))) return;
          m = toModel(tp.def(ctx.lang));
          dirty = false;
          draw();
          root.querySelector<HTMLElement>('textarea')?.focus();
        })),
      field(t('workflow.description'), desc, { help: t('workflow.description_help') }),
      h('section', { class: 'ewf-ed-section', 'aria-labelledby': 'ewf-fields-h' }, h('h3', { id: 'ewf-fields-h' }, t('workflow.fields')), h('p', { class: 'ew-help' }, t('workflow.fields_help')),
        ...m.fields.map(fieldRow),
        button(t('workflow.add_field'), () => { m.fields.push({ id: '', label: '', type: 'text' }); draw(); focusIn('.ewf-field-row')(m.fields.length - 1); })),
      h('section', { class: 'ewf-ed-section', 'aria-labelledby': 'ewf-steps-h' }, h('h3', { id: 'ewf-steps-h' }, t('workflow.steps')), h('p', { class: 'ew-help' }, t('workflow.steps_help')),
        ...m.steps.map(stepRow),
        h('div', { class: 'ewf-template ewf-add-step' }, field(t('workflow.new_step_type'), newType, { inline: true }),
          button(t('workflow.add_step'), () => {
            const s: EStep = { key: ++seq, id: '', name: '', type: 'task', assignee: { type: 'group', value: '' }, actions: [], next: 0, nextElse: 0 };
            retype(s, newType.value as StepType);
            m.steps.push(s);
            draw();
            focusIn('.ewf-step-row')(m.steps.length - 1);
          }))),
    );
  };
  draw();

  return {
    getBody: () => toDefinition(m),
    validate: () => {
      if (!m.steps.length) return t('workflow.err_steps');
      for (const f of m.fields) if (!f.label.trim()) return t('workflow.err_field_label');
      for (const s of m.steps) {
        if (!s.name.trim()) return t('workflow.err_step_name');
        if ((s.type === 'task' || s.type === 'approval') && s.assignee.type !== 'starter' && !s.assignee.value.trim()) return t('workflow.err_assignee', { step: s.name });
        if (s.type === 'task') {
          if (!s.actions.length) return t('workflow.err_action', { step: s.name });
          for (const a of s.actions) if (!a.label.trim() || !a.next) return t('workflow.err_action', { step: s.name });
        }
        if (['approval', 'condition'].includes(s.type) && (!s.next || !s.nextElse)) return t('workflow.err_next', { step: s.name });
        if (['email', 'webhook', 'delay'].includes(s.type) && !s.next) return t('workflow.err_next', { step: s.name });
        if (s.type === 'condition' && (!s.rules?.length || s.rules.some((r) => !r.field))) return t('workflow.err_rules', { step: s.name });
        if (s.type === 'email' && (!s.email?.to.trim() || !s.email.subject.trim() || !s.email.body.trim())) return t('workflow.err_email', { step: s.name });
        if (s.type === 'webhook' && !/^https?:\/\//.test(s.webhook?.url ?? '')) return t('workflow.err_webhook', { step: s.name });
      }
      return null;
    },
  };
}
