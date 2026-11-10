// Macro placeholders of the simple editor:
// <div data-macro="name" data-params="{...}">body</div>. The dialog is the
// one of the core (macroDialog from the SDK).
import type { MacroChoice } from '@ewiki/sdk';

/** Escapes a value for a double quoted HTML attribute. */
export function attr(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Builds the HTML of a macro placeholder. */
export function macroHTML(c: MacroChoice, body: string): string {
  return `<div data-macro="${attr(c.name)}" data-params="${attr(JSON.stringify(c.params))}">${c.hasBody ? body : ''}</div>`;
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
