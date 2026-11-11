// The parts of plugins in the editing of an element (see registerEditField).
import { h } from '../core/dom';
import { listEditFields, type EditFieldContext } from '../sdk';

export interface EditFields {
  el: HTMLElement;
  /** Values by field id (only fields that are shown). */
  values: () => Record<string, unknown>;
  /** The first problem (the field focuses itself) or null. */
  validate: () => string | null;
  /** Shows the values of a draft. */
  reload: (initial: Record<string, unknown> | null | undefined) => void;
}

export function editFields(ctx: Omit<EditFieldContext, 'initial'>, initial?: Record<string, unknown> | null): EditFields {
  const box = h('div', { class: 'ew-edit-fields' });
  let parts: { id: string; value: () => unknown; validate?: () => string | null }[] = [];
  let gen = 0;
  const load = async (init: Record<string, unknown> | null | undefined) => {
    const my = ++gen;
    const next: typeof parts = [];
    const els: HTMLElement[] = [];
    for (const f of listEditFields()) {
      try {
        const r = await f.render({ ...ctx, initial: init?.[f.id] });
        if (!r) continue;
        next.push({ id: f.id, value: r.value, validate: r.validate });
        els.push(r.el);
      } catch { /* a failing plugin does not block editing */ }
    }
    if (my !== gen) return;
    parts = next;
    box.replaceChildren(...els);
  };
  void load(initial);
  return {
    reload: (init) => void load(init),
    el: box,
    values: () => Object.fromEntries(parts.map((p) => [p.id, p.value()])),
    validate: () => {
      for (const p of parts) {
        const e = p.validate?.();
        if (e) return e;
      }
      return null;
    },
  };
}
