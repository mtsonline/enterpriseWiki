// Macro dialog of the core.html editor: insert or edit
// <div data-macro="name" data-params="{...}">body</div>.
import { api, checkbox, field, formDialog, h, select, t, textArea, textInput, uiLang, type MacroInfo, type MacroParam } from '@ewiki/sdk';

let cache: Promise<MacroInfo[]> | null = null;

/** Lists the macros of active plugins (cached per page load). */
export function loadMacros(): Promise<MacroInfo[]> {
  cache ??= api.get<MacroInfo[]>('macros').catch((e) => { cache = null; throw e; });
  return cache;
}

const label = (m: Record<string, string>): string => m[uiLang()] || m.en || Object.values(m)[0] || '';

export interface MacroChoice { name: string; params: Record<string, string>; hasBody: boolean }

/** Escapes a value for a double quoted HTML attribute. */
export function attr(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Builds the HTML of a macro placeholder. */
export function macroHTML(c: MacroChoice, body: string): string {
  return `<div data-macro="${attr(c.name)}" data-params="${attr(JSON.stringify(c.params))}">${c.hasBody ? body : ''}</div>`;
}

function paramControl(p: MacroParam, value: string): { el: HTMLElement; get: () => string } {
  const name = label(p.labels) || p.name;
  switch (p.type) {
    case 'select': {
      const s = select((p.options ?? []).map((o) => ({ value: o, label: o })), value || p.default);
      return { el: field(name, s, { required: p.required }), get: () => s.value };
    }
    case 'bool': {
      const c = checkbox(name, value === 'true');
      return { el: c.el, get: () => String(c.input.checked) };
    }
    case 'textarea': {
      const ta = textArea({ value, rows: 4 });
      ta.classList.add('ewh-mono');
      return { el: field(name, ta, { required: p.required }), get: () => ta.value };
    }
    default: {
      const i = textInput({ value, type: p.type === 'number' ? 'number' : p.type === 'url' ? 'text' : 'text', required: p.required, placeholder: p.type === 'url' ? 'https://' : undefined });
      return { el: field(name, i, { required: p.required }), get: () => i.value };
    }
  }
}

/** Opens the macro dialog. existing pre-fills it for editing. */
export async function macroDialog(existing: { name: string; params: Record<string, string> } | null, onDone: (c: MacroChoice) => void): Promise<void> {
  const macros = (await loadMacros()).slice().sort((a, b) => label(a.labels).localeCompare(label(b.labels)));
  if (!macros.length) throw new Error(t('core.html.macro_none'));
  const pickSel = select(macros.map((m) => ({ value: m.name, label: label(m.labels) })), existing?.name ?? macros[0].name);
  if (existing) pickSel.disabled = true;
  const desc = h('p', { class: 'ew-help', 'aria-live': 'polite' });
  const paramBox = h('div', { class: 'ewh-macro-params' });
  let getters: { name: string; get: () => string; required?: boolean; label: string }[] = [];
  const show = () => {
    const m = macros.find((x) => x.name === pickSel.value);
    if (!m) return;
    desc.textContent = label(m.description);
    getters = [];
    paramBox.replaceChildren(...(m.params ?? []).map((p) => {
      const c = paramControl(p, existing?.name === m.name ? (existing.params[p.name] ?? '') : (p.default ?? ''));
      getters.push({ name: p.name, get: c.get, required: p.required, label: label(p.labels) || p.name });
      return c.el;
    }));
    if (!(m.params ?? []).length) paramBox.append(h('p', { class: 'ew-muted' }, t('core.html.macro_no_params')));
  };
  pickSel.addEventListener('change', show);
  show();
  formDialog(existing ? t('core.html.macro_edit') : t('core.html.macro_insert'),
    [field(t('core.html.macro'), pickSel), desc, paramBox], existing ? t('common.apply') : t('core.html.macro_insert'), async () => {
      const m = macros.find((x) => x.name === pickSel.value)!;
      const params: Record<string, string> = {};
      for (const g of getters) {
        const v = g.get().trim();
        if (g.required && !v) throw new Error(t('core.html.macro_required', { field: g.label }));
        if (v) params[g.name] = v;
      }
      onDone({ name: m.name, params, hasBody: m.has_body });
    }, { wide: true });
}

/** Reads the macro placeholder around node (WYSIWYG editing). */
export function macroAt(node: Node | null, root: HTMLElement): HTMLElement | null {
  const el = (node instanceof Element ? node : node?.parentElement)?.closest<HTMLElement>('[data-macro]') ?? null;
  return el && root.contains(el) ? el : null;
}

export function parseParams(el: HTMLElement): Record<string, string> {
  try {
    const v = JSON.parse(el.dataset.params || '{}') as Record<string, unknown>;
    const out: Record<string, string> = {};
    for (const [k, x] of Object.entries(v)) out[k] = String(x);
    return out;
  } catch {
    return {};
  }
}
