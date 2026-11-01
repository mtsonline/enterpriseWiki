// Structured editor for workflow definitions (fields, steps, actions).
import { button, checkbox, confirmDialog, field, h, select, t, textArea, textInput, type EditorContext, type EditorInstance } from '@ewiki/sdk';
import { TEMPLATES } from './templates';
import type { Action, Definition, Field, Step } from './types';

// Internal model: steps and actions reference each other by a stable key, so
// renaming a step id does not break the "next step" choices.
interface EAction extends Omit<Action, 'next'> { next: number }
interface EStep extends Omit<Step, 'actions'> { key: number; actions: EAction[] }
interface Model { description: string; fields: Field[]; steps: EStep[] }

let seq = 0;

function toModel(d: Definition): Model {
  const keys = new Map<string, number>();
  const steps = d.steps.map((s) => { const key = ++seq; keys.set(s.id, key); return { ...s, key, actions: [] as EAction[] }; });
  d.steps.forEach((s, i) => { steps[i].actions = (s.actions ?? []).map((a) => ({ ...a, next: keys.get(a.next) ?? 0 })); });
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

function toDefinition(m: Model): Definition {
  const fieldIds = new Set<string>();
  const fields = m.fields.map((f, i) => ({ ...f, id: f.id.trim() && !fieldIds.has(f.id.trim()) ? (fieldIds.add(f.id.trim()), f.id.trim()) : slug(f.label, fieldIds, `field${i + 1}`),
    options: f.type === 'select' ? (f.options ?? []).map((o) => o.trim()).filter(Boolean) : undefined }));
  const stepIds = new Set<string>();
  const ids = new Map<number, string>();
  for (const [i, s] of m.steps.entries()) {
    const id = s.id.trim() && !stepIds.has(s.id.trim()) ? (stepIds.add(s.id.trim()), s.id.trim()) : slug(s.name, stepIds, `step${i + 1}`);
    ids.set(s.key, id);
  }
  const steps: Step[] = m.steps.map((s) => {
    const aIds = new Set<string>();
    return {
      id: ids.get(s.key)!, name: s.name, description: s.description, assignee: s.assignee, outcome: s.actions.length ? undefined : (s.outcome || 'neutral'),
      actions: s.actions.map((a, j) => ({ id: a.id.trim() && !aIds.has(a.id.trim()) ? (aIds.add(a.id.trim()), a.id.trim()) : slug(a.label, aIds, `action${j + 1}`),
        label: a.label, next: ids.get(a.next) ?? '', note: a.note || undefined })),
    };
  });
  return { description: m.description, fields, steps };
}

const FIELD_TYPES = ['text', 'textarea', 'email', 'date', 'number', 'select', 'checkbox', 'file'];

export function createWorkflowEditor(container: HTMLElement, body: unknown, ctx: EditorContext): EditorInstance {
  let m = toModel((body ?? { description: '', fields: [], steps: [] }) as Definition);
  const root = h('div', { class: 'ewf-editor' });
  container.append(root);
  let dirty = false; // ask before a template replaces user input
  root.addEventListener('input', (ev) => { if (!(ev.target as Element).closest('.ewf-template')) dirty = true; });

  const moveBtns = <T>(list: T[], i: number, what: string, refocus: (i: number) => void) => [
    button(h('span', { 'aria-hidden': 'true' }, '↑'), () => { [list[i - 1], list[i]] = [list[i], list[i - 1]]; draw(); refocus(i - 1); },
      { variant: 'subtle', ariaLabel: t('workflow.move_up', { what }), disabled: i === 0 }),
    button(h('span', { 'aria-hidden': 'true' }, '↓'), () => { [list[i + 1], list[i]] = [list[i], list[i + 1]]; draw(); refocus(i + 1); },
      { variant: 'subtle', ariaLabel: t('workflow.move_down', { what }), disabled: i === list.length - 1 }),
    button(t('common.delete'), () => { list.splice(i, 1); draw(); refocus(Math.min(i, list.length - 1)); }, { variant: 'danger', ariaLabel: `${t('common.delete')}: ${what}` }),
  ];
  const focusIn = (sel: string) => (i: number) => root.querySelectorAll<HTMLElement>(sel)[i]?.querySelector<HTMLElement>('input, select, textarea')?.focus();

  const fieldRow = (f: Field, i: number): HTMLElement => {
    const label = textInput({ value: f.label, maxlength: 200, lang: ctx.lang });
    label.addEventListener('input', () => { f.label = label.value; legend.textContent = label.value || t('workflow.field_n', { n: i + 1 }); });
    const id = textInput({ value: f.id, maxlength: 40 });
    id.addEventListener('input', () => { f.id = id.value; });
    const type = select(FIELD_TYPES.map((x) => ({ value: x, label: t(`workflow.type.${x}`) })), f.type || 'text');
    type.addEventListener('change', () => { f.type = type.value; draw(); focusIn('.ewf-field-row')(i); });
    const req = checkbox(t('workflow.required'), !!f.required);
    req.input.addEventListener('change', () => { f.required = req.input.checked; });
    const help = textInput({ value: f.help ?? '', maxlength: 500, lang: ctx.lang });
    help.addEventListener('input', () => { f.help = help.value; });
    const opts = textArea({ value: (f.options ?? []).join('\n'), rows: 3, lang: ctx.lang });
    opts.addEventListener('input', () => { f.options = opts.value.split('\n'); });
    const legend = h('legend', null, f.label || t('workflow.field_n', { n: i + 1 }));
    return h('fieldset', { class: 'ewf-row ewf-field-row' }, legend,
      h('div', { class: 'ewf-grid' }, field(t('workflow.label'), label, { required: true }), field(t('workflow.type'), type),
        field(t('workflow.id'), id, { help: t('workflow.id_help') }), field(t('workflow.help_text'), help)),
      f.type === 'select' ? field(t('workflow.options'), opts, { help: t('workflow.options_help'), required: true }) : null,
      req.el, h('div', { class: 'ew-actions' }, ...moveBtns(m.fields, i, f.label || t('workflow.field_n', { n: i + 1 }), focusIn('.ewf-field-row'))));
  };

  const actionRow = (s: EStep, a: EAction, j: number): HTMLElement => {
    const label = textInput({ value: a.label, maxlength: 100, lang: ctx.lang });
    label.addEventListener('input', () => { a.label = label.value; });
    const next = select([{ value: '0', label: t('workflow.choose') }, ...m.steps.map((x) => ({ value: String(x.key), label: x.name || x.id || '?' }))], String(a.next));
    next.addEventListener('change', () => { a.next = Number(next.value); });
    const note = select([{ value: '', label: t('workflow.note.none') }, { value: 'optional', label: t('workflow.note.optional') }, { value: 'required', label: t('workflow.note.required') }], a.note ?? '');
    note.addEventListener('change', () => { a.note = note.value as EAction['note']; });
    return h('div', { class: 'ewf-action-row', role: 'group', 'aria-label': t('workflow.action_n', { n: j + 1 }) },
      field(t('workflow.action_label'), label, { required: true }), field(t('workflow.next'), next, { required: true }), field(t('workflow.note'), note),
      button(t('common.delete'), () => { s.actions.splice(j, 1); draw(); }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${a.label || t('workflow.action_n', { n: j + 1 })}` }));
  };

  const stepRow = (s: EStep, i: number): HTMLElement => {
    const name = textInput({ value: s.name, maxlength: 200, lang: ctx.lang });
    name.addEventListener('input', () => { s.name = name.value; legend.textContent = `${i + 1}. ${name.value}`; });
    name.addEventListener('change', () => draw()); // refresh "next step" lists
    const id = textInput({ value: s.id, maxlength: 40 });
    id.addEventListener('input', () => { s.id = id.value; });
    const desc = textInput({ value: s.description ?? '', maxlength: 2000, lang: ctx.lang });
    desc.addEventListener('input', () => { s.description = desc.value; });
    const final = s.actions.length === 0;
    const legend = h('legend', null, `${i + 1}. ${s.name}`);
    const parts: (HTMLElement | null)[] = [h('div', { class: 'ewf-grid' }, field(t('workflow.step_name'), name, { required: true }), field(t('workflow.id'), id, { help: t('workflow.id_help') })),
      field(t('workflow.step_desc'), desc)];
    if (final) {
      const outcome = select(['success', 'failure', 'neutral'].map((x) => ({ value: x, label: t(`workflow.outcome.${x}`) })), s.outcome || 'neutral');
      outcome.addEventListener('change', () => { s.outcome = outcome.value; });
      parts.push(h('p', { class: 'ew-help' }, t('workflow.final_help')), field(t('workflow.outcome'), outcome));
    } else {
      const at = select(['starter', 'user', 'group', 'role'].map((x) => ({ value: x, label: t(`workflow.assignee.${x}`) })), s.assignee?.type || 'group');
      const av = textInput({ value: s.assignee?.value ?? '', maxlength: 200 });
      at.addEventListener('change', () => { s.assignee = { type: at.value as Step['assignee']['type'], value: av.value }; av.disabled = at.value === 'starter'; });
      av.addEventListener('input', () => { s.assignee = { type: at.value as Step['assignee']['type'], value: av.value }; });
      av.disabled = at.value === 'starter';
      parts.push(h('div', { class: 'ewf-grid' }, field(t('workflow.assignee'), at), field(t('workflow.assignee_value'), av, { help: t('workflow.assignee_help') })),
        h('h4', null, t('workflow.actions')), ...s.actions.map((a, j) => actionRow(s, a, j)));
    }
    parts.push(h('div', { class: 'ew-actions' },
      button(t('workflow.add_action'), () => {
        s.assignee ??= { type: 'group', value: '' };
        if (!s.assignee.type) s.assignee = { type: 'group', value: '' };
        s.actions.push({ id: '', label: '', next: 0 }); draw();
        root.querySelectorAll<HTMLElement>('.ewf-step-row')[i]?.querySelectorAll<HTMLInputElement>('.ewf-action-row input')[s.actions.length - 1]?.focus();
      }),
      ...moveBtns(m.steps, i, s.name || t('workflow.step_n', { n: i + 1 }), focusIn('.ewf-step-row'))));
    return h('fieldset', { class: ['ewf-row', 'ewf-step-row', final && 'ewf-step-final'] }, legend, ...parts);
  };

  const draw = () => {
    const desc = textArea({ value: m.description, rows: 4, lang: ctx.lang });
    desc.setAttribute('aria-labelledby', ctx.labelId);
    desc.addEventListener('input', () => { m.description = desc.value; });
    const tpl = select([{ value: '', label: t('workflow.template_choose') }, ...TEMPLATES.map((x) => ({ value: x.id, label: t(`workflow.template.${x.id}`) }))], '');
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
        h('div', { class: 'ew-actions' },
          button(t('workflow.add_step'), () => { m.steps.splice(Math.max(0, m.steps.findIndex((s) => !s.actions.length)) || m.steps.length, 0, { key: ++seq, id: '', name: '', assignee: { type: 'group', value: '' }, actions: [{ id: '', label: '', next: 0 }] }); draw(); }),
          button(t('workflow.add_final'), () => { m.steps.push({ key: ++seq, id: '', name: '', assignee: { type: 'starter', value: '' }, actions: [], outcome: 'success' }); draw(); focusIn('.ewf-step-row')(m.steps.length - 1); }))),
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
        if (s.actions.length && s.assignee.type !== 'starter' && !s.assignee.value.trim()) return t('workflow.err_assignee', { step: s.name });
        for (const a of s.actions) if (!a.label.trim() || !a.next) return t('workflow.err_action', { step: s.name });
      }
      return null;
    },
  };
}
