// Light and dark. A person chooses "system" (as the device is set), "light"
// or "dark"; without a choice the template decides (its color_scheme). A
// template without color_scheme has light colours only, and the choice does
// not apply to it. The server keeps the choice (personal settings when signed
// in, a cookie for guests) and marks the page it sends with it
// (<html data-scheme data-scheme-choice>), so a dark page does not flash light.
import { api } from './api';
import { state } from './state';

export type Scheme = 'system' | 'light' | 'dark';
export const SCHEMES: readonly Scheme[] = ['system', 'light', 'dark'];

const isScheme = (v: unknown): v is Scheme => typeof v === 'string' && (SCHEMES as readonly string[]).includes(v);

/** schemes of templates that exist only in this page (the preview of the designer) */
const overrides = new Map<string, Scheme | ''>();

/** Sets the default scheme of a template that exists only in this page (the
 * draft in the preview of the designer); the server does not know it. */
export function setTemplateScheme(id: string, scheme: Scheme | ''): void {
  overrides.set(id, scheme);
}

/** the default scheme of the template shown ('' = light colours only) */
let templateDefault: Scheme | '' = '';
let media: MediaQueryList | null = null;
const listeners = new Set<() => void>();

/** The scheme the person (or this browser) chose ('' = the default of the template). */
export function schemeChoice(): Scheme | '' {
  const v = document.documentElement.dataset.schemeChoice;
  return isScheme(v) ? v : '';
}

/** Whether the template shown follows light and dark (else it is light only). */
export function schemeOffered(): boolean {
  return templateDefault !== '';
}

/** The scheme that applies: the choice, else the default of the template. */
export function schemeSetting(): Scheme | '' {
  return templateDefault ? schemeChoice() || templateDefault : '';
}

/** The scheme on screen. */
export function shownScheme(): 'light' | 'dark' {
  const s = schemeSetting();
  if (s === 'system') return media?.matches ? 'dark' : 'light';
  return s === 'dark' ? 'dark' : 'light';
}

/** The default scheme of a template ('' = light only). Only the templates the
 * server names (plugin manifests) and the basic template of the core follow
 * light and dark. */
export function templateScheme(id: string): Scheme | '' {
  if (id === 'core.basic') return 'system';
  if (overrides.has(id)) return overrides.get(id)!;
  const v = state.site?.templates?.find((x) => x.id === id)?.color_scheme;
  return isScheme(v) ? v : '';
}

function render(): void {
  const root = document.documentElement;
  const s = schemeSetting();
  if (s) root.dataset.scheme = s;
  else delete root.dataset.scheme;
  const shown = shownScheme();
  root.dataset.shown = shown;
  document.body.classList.toggle('ewc-dark', shown === 'dark');
  for (const fn of listeners) fn();
}

/** Applies the scheme for the template shown (called whenever it changes). */
export function applyScheme(templateId: string): void {
  templateDefault = templateScheme(templateId);
  if (!media && typeof matchMedia === 'function') {
    media = matchMedia('(prefers-color-scheme: dark)');
    media.addEventListener('change', render);
  }
  render();
}

/** Runs fn whenever the scheme changes (the choice, the template or the device). */
export function onSchemeChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Chooses a scheme ('' = the default of the template); kept by the server. */
export async function chooseScheme(v: Scheme | ''): Promise<void> {
  await api.put('scheme', { scheme: v });
  const root = document.documentElement;
  if (v) root.dataset.schemeChoice = v;
  else delete root.dataset.schemeChoice;
  if (state.user) {
    const prefs = { ...(state.user.prefs ?? {}) };
    if (v) prefs.scheme = v;
    else delete prefs.scheme;
    state.user.prefs = prefs;
  }
  render();
}
