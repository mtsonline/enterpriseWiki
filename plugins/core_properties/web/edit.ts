// The properties in the editing of an element (every type) and the quick
// editing on the page: inputs for the definitions, free properties.
import { button, field, formDialog, h, select, t, textInput, uid } from '@ewiki/sdk';
import { loadTypes, typeLabel, type Props, type Shown } from './model';
import { valueInput, type ValueInput } from './inputs';

export interface PropsInput { values: Record<string, unknown>; free: Record<string, { type: string; label: string }> }

const keyOf = (label: string): string => {
  const k = label.toLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '').replace(/ß/g, 'ss').replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 60);
  return /^[a-z]/.test(k) ? k : `p_${k}`.slice(0, 63);
};

/** The form of the properties; value() returns what changed for the API. */
export async function propertiesForm(data: Props, opts: { compact?: boolean } = {}): Promise<{ el: HTMLElement; value: () => PropsInput; validate: () => string | null; empty: boolean }> {
  const inputs = new Map<string, { item: Shown; input: ValueInput; initial: string }>();
  const removed = new Set<string>();
  const added: Record<string, { type: string; label: string }> = {};
  const groups = new Map<string, HTMLElement>();
  const body = h('div', { class: 'ewprop-form' });
  const freeList = h('div', { class: 'ewprop-free' });
  const groupBox = (name: string): HTMLElement => {
    if (!name) return body;
    let g = groups.get(name);
    if (!g) {
      g = h('fieldset', { class: 'ew-fieldset ewprop-group' }, h('legend', null, name));
      groups.set(name, g);
      body.append(g);
    }
    return g;
  };
  const addInput = async (item: Shown, target: HTMLElement) => {
    const computed = item.type === 'autonumber' || item.type === 'formula';
    const input = await valueInput({ key: item.key, label: item.label, type: item.type, options: item.options, required: item.required, help: item.help, readonly: !item.can_edit || computed }, item.value, data.refs);
    inputs.set(item.key, { item, input, initial: JSON.stringify(item.value ?? null) });
    if (item.def_id === null && item.can_edit) {
      // free properties can be removed
      const row = h('div', { class: 'ewprop-free-row' }, input.el,
        button(t('props.remove_free'), () => { removed.add(item.key); inputs.delete(item.key); delete added[item.key]; row.remove(); },
          { variant: 'subtle', ariaLabel: t('props.remove_named', { name: item.label }) }));
      target.append(row);
    } else {
      target.append(input.el);
    }
  };
  for (const item of data.items) {
    if (item.def_id === null) continue;
    if (!item.can_edit && (item.value === null || item.value === undefined)) continue; // nothing to show or change
    await addInput(item, groupBox(item.group ?? ''));
  }
  for (const item of data.items) if (item.def_id === null) await addInput(item, freeList);
  const addFree = data.can_add_free ? button(t('props.add_free'), async () => {
    const { types } = await loadTypes();
    const name = textInput({ required: true, maxlength: 80 });
    const type = select(types.filter((x) => !x.computed && !['category', 'media'].includes(x.id)).map((x) => ({ value: x.id, label: typeLabel(x.id) })), 'text');
    formDialog(t('props.add_free_title'), [field(t('props.name'), name, { required: true, help: t('props.name_help') }), field(t('props.type_label'), type)], t('props.add_free'), async () => {
      const label = name.value.trim();
      if (!label) throw new Error(t('props.required', { name: t('props.name') }));
      const key = keyOf(label);
      if (inputs.has(key) || data.items.some((x) => x.key === key && !removed.has(key))) throw new Error(t('props.exists', { name: label }));
      removed.delete(key);
      added[key] = { type: type.value, label };
      await addInput({ key, label, type: type.value, def_id: null, value: null, show_on_page: true, can_edit: true }, freeList);
      queueMicrotask(() => (freeList.lastElementChild?.querySelector('input, select, textarea') as HTMLElement | null)?.focus());
    });
  }, { variant: 'secondary' }) : null;
  const empty = !inputs.size && !addFree;
  const headId = uid('ewprop-h');
  const el = h('section', { class: 'ewprop-edit', 'aria-labelledby': headId },
    h('h2', { id: headId, class: opts.compact ? 'sr-only' : 'ewprop-edit-title' }, t('props.title')),
    body, freeList, addFree);
  return {
    el, empty,
    value: () => {
      const values: Record<string, unknown> = {};
      for (const [k, x] of inputs) {
        if (!x.item.can_edit || x.item.type === 'autonumber' || x.item.type === 'formula') continue;
        const v = x.input.get();
        if (JSON.stringify(v ?? null) !== x.initial || added[k]) values[k] = v;
      }
      for (const k of removed) if (data.items.some((x) => x.key === k)) values[k] = null;
      const free: PropsInput['free'] = {};
      for (const [k, m] of Object.entries(added)) if (k in values) free[k] = m;
      return { values, free };
    },
    validate: () => {
      for (const [, x] of inputs) {
        if (!x.item.can_edit) continue;
        const e = x.input.validate();
        if (e) return e;
      }
      return null;
    },
  };
}
