// Macro dialog of the editor: choose a macro of an active plugin and fill in
// its parameters (GET /api/v1/macros).
import { api } from '../core/api';
import { h } from '../core/dom';
import { t, uiLang } from '../core/i18n';
import type { MacroInfo, MacroParam } from '../core/types';
import { checkbox, field, formDialog, select, textArea, textInput } from '../components/ui';
import { currentCategory } from '../components/widgets';

let cache: Promise<MacroInfo[]> | null = null;

/** Lists the macros of active plugins (cached per page load). */
export function loadMacros(): Promise<MacroInfo[]> {
  cache ??= api.get<MacroInfo[]>('macros').catch((e: unknown) => { cache = null; throw e; });
  return cache;
}

/** The label of a localized map in the UI language. */
export const label = (m: Record<string, string> | undefined): string => (m ? m[uiLang()] || m.en || Object.values(m)[0] || '' : '');

export interface MacroChoice { name: string; params: Record<string, string>; hasBody: boolean; inline: boolean }

/** Context of a custom parameter control: the other parameters of the dialog. */
export interface MacroParamContext {
  label: string;
  required: boolean;
  /** current values of all parameters (also of other controls) */
  params: () => Record<string, string>;
  /** the category of the page being edited (0: unknown) */
  categoryId: number;
}

export type MacroParamControl = (value: string, ctx: MacroParamContext) => { el: HTMLElement; get: () => string };

const customControls = new Map<string, MacroParamControl>();

/** Registers a control for macro parameters of type custom:<name>. */
export function registerMacroParamControl(name: string, f: MacroParamControl): void {
  customControls.set(name, f);
}

/** A registered control (e.g. for editors of element types with the same settings). */
export function getMacroParamControl(name: string): MacroParamControl | undefined {
  return customControls.get(name);
}

function paramControl(p: MacroParam, value: string, ctx: Omit<MacroParamContext, 'label' | 'required'>): { el: HTMLElement; get: () => string } {
  const name = label(p.labels) || p.name;
  if (p.type.startsWith('custom:')) {
    const f = customControls.get(p.type.slice(7));
    if (f) return f(value, { ...ctx, label: name, required: !!p.required });
  }
  switch (p.type) {
    case 'select': {
      const s = select((p.options ?? []).map((o) => ({ value: o, label: label(p.option_labels?.[o]) || o })), value || p.default);
      return { el: field(name, s, { required: p.required }), get: () => s.value };
    }
    case 'bool': {
      const c = checkbox(name, value === 'true');
      return { el: c.el, get: () => String(c.input.checked) };
    }
    case 'textarea': {
      const ta = textArea({ value, rows: 4 });
      ta.classList.add('ewe-mono');
      return { el: field(name, ta, { required: p.required }), get: () => ta.value };
    }
    default: {
      const i = textInput({ value, type: p.type === 'number' ? 'number' : 'text', required: p.required, placeholder: p.type === 'url' ? 'https://' : undefined });
      return { el: field(name, i, { required: p.required }), get: () => i.value };
    }
  }
}

/**
 * Opens the macro dialog. existing pre-fills it for editing; only (pre-)
 * selects a macro for new insertions (toolbar buttons of macro plugins);
 * preset pre-fills parameters of a new macro (e.g. the selected text).
 * Resolves with the choice, or null if the dialog was cancelled.
 */
export async function macroDialog(existing: { name: string; params: Record<string, string> } | null, only?: string, preset: Record<string, string> = {}): Promise<MacroChoice | null> {
  const macros = (await loadMacros()).slice().sort((a, b) => label(a.labels).localeCompare(label(b.labels)));
  if (!macros.length) throw new Error(t('editor.macro_none'));
  const initial = existing?.name ?? only ?? macros[0].name;
  if (!macros.some((m) => m.name === initial)) throw new Error(t('editor.macro_unknown', { name: initial }));
  const pickSel = select(macros.map((m) => ({ value: m.name, label: label(m.labels) || m.name })), initial);
  if (existing) pickSel.disabled = true;
  const desc = h('p', { class: 'ew-help', 'aria-live': 'polite' });
  const paramBox = h('div', { class: 'ewe-macro-params' });
  let getters: { name: string; get: () => string; required?: boolean; label: string }[] = [];
  const show = () => {
    const m = macros.find((x) => x.name === pickSel.value);
    if (!m) return;
    desc.textContent = label(m.description);
    getters = [];
    const all = () => Object.fromEntries(getters.map((g) => [g.name, g.get()]));
    paramBox.replaceChildren(...(m.params ?? []).map((p) => {
      const c = paramControl(p, existing?.name === m.name ? (existing.params[p.name] ?? '') : (preset[p.name] ?? p.default ?? ''), { params: all, categoryId: currentCategory() });
      getters.push({ name: p.name, get: c.get, required: p.required, label: label(p.labels) || p.name });
      return c.el;
    }));
    if (!(m.params ?? []).length) paramBox.append(h('p', { class: 'ew-muted' }, t('editor.macro_no_params')));
  };
  pickSel.addEventListener('change', show);
  show();
  return new Promise((resolve) => {
    let result: MacroChoice | null = null;
    formDialog(existing ? t('editor.macro_edit') : t('editor.macro_insert'),
      [field(t('editor.macro'), pickSel), desc, paramBox], existing ? t('common.apply') : t('editor.macro_insert'), async () => {
        const m = macros.find((x) => x.name === pickSel.value)!;
        const params: Record<string, string> = {};
        for (const g of getters) {
          const v = g.get().trim();
          if (g.required && !v) throw new Error(t('editor.macro_required', { field: g.label }));
          if (v) params[g.name] = v;
        }
        result = { name: m.name, params, hasBody: m.has_body, inline: !!m.inline };
      }, { wide: true, onClose: () => resolve(result) });
  });
}
