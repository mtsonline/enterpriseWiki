// Frontend plugin SDK. Plugins import from "@ewiki/sdk" and export a
// register() function from plugins/<dir>/web/index.ts.
//
//   import { registerEditor, h } from '@ewiki/sdk';
//   export function register() { registerEditor('my.type', (el, body) => ...); }
//
// See docs/developer/plugins.md.
import { state, onChange } from './core/state';

export { h, mount, append, trustedHTML, uid, fmtDate, srOnly } from './core/dom';
export type { Child } from './core/dom';
export { api, ApiError, qs } from './core/api';
export { t, pick, uiLang } from './core/i18n';
export { state, onChange, isAdmin } from './core/state';
export { toast, announce } from './core/announce';
export { navigate, route, rerender } from './core/router';
export { button, field, textInput, textArea, select, checkbox, openDialog, confirmDialog, formDialog, langFields, errorText, loading, pageHeader } from './components/ui';
export type * from './core/types';

// ---------------------------------------------------------------- plugin modules

export interface PluginModule {
  register?: () => void;
}

export interface LoadedPlugin {
  id: string;
  mod: PluginModule;
}

export function definePluginModule(id: string, mod: PluginModule): LoadedPlugin {
  return { id, mod };
}

// ---------------------------------------------------------------- templates

export interface LayoutContext {
  /** Returns a live container for a position (re-rendered on changes). */
  position(name: string, tag?: keyof HTMLElementTagNameMap): HTMLElement;
}

export interface Layout {
  root: HTMLElement;
  main: HTMLElement;
}

export interface TemplateDef {
  id: string;
  /** Base template id: everything not defined here is inherited. */
  extends?: string;
  /** CSS class added to <body> (all classes of the inheritance chain are added). */
  className?: string;
  /** Positions offered to plugins (menus, widgets). */
  positions?: string[];
  /** Builds the page skeleton. Inherited from the base template if omitted. */
  layout?: (ctx: LayoutContext) => Layout;
}

const templates = new Map<string, TemplateDef>();

export function registerTemplate(def: TemplateDef): void {
  templates.set(def.id, def);
}

export interface ResolvedTemplate {
  id: string;
  classNames: string[];
  positions: string[];
  layout: (ctx: LayoutContext) => Layout;
}

/** Resolves a template with its inheritance chain. */
export function resolveTemplate(id: string, fallback = 'theme.confluence'): ResolvedTemplate | null {
  const chain: TemplateDef[] = [];
  let cur = templates.get(id) ?? templates.get(fallback);
  const seen = new Set<string>();
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    chain.push(cur);
    cur = cur.extends ? templates.get(cur.extends) : undefined;
  }
  const layout = chain.find((d) => d.layout)?.layout;
  if (!layout) return null;
  return {
    id: chain[0].id,
    classNames: chain.map((d) => d.className).filter((c): c is string => !!c).reverse(),
    positions: [...new Set(chain.flatMap((d) => d.positions ?? []))],
    layout,
  };
}

export function listTemplates(): TemplateDef[] {
  return [...templates.values()];
}

// ---------------------------------------------------------------- positions (slots)

export interface SlotDef {
  id: string;
  position: string;
  order?: number;
  render: () => Node | null | Promise<Node | null>;
}

const slots: SlotDef[] = [];
const positionEls = new Map<string, Set<HTMLElement>>();

/** Registers a widget for a template position (e.g. "header", "sidebar", "footer"). */
export function registerSlot(def: SlotDef): void {
  slots.push(def);
  slots.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

async function fill(el: HTMLElement, name: string): Promise<void> {
  const nodes: Node[] = [];
  for (const s of slots.filter((x) => x.position === name)) {
    try {
      const n = await s.render();
      if (n) nodes.push(n);
    } catch (e) {
      console.error(`slot ${s.id} failed`, e);
    }
  }
  el.replaceChildren(...nodes);
}

/** Creates a live position container. */
export function positionContainer(name: string, tag: keyof HTMLElementTagNameMap = 'div'): HTMLElement {
  const el = document.createElement(tag);
  el.className = `ew-pos ew-pos--${name}`;
  el.dataset.position = name;
  if (!positionEls.has(name)) positionEls.set(name, new Set());
  positionEls.get(name)!.add(el);
  void fill(el, name);
  return el;
}

/** Re-renders all positions (after navigation, login, language change). */
export function refreshPositions(): Promise<void> {
  const jobs: Promise<void>[] = [];
  for (const [name, els] of positionEls) {
    for (const el of els) {
      if (!el.isConnected) els.delete(el);
      else jobs.push(fill(el, name));
    }
  }
  return Promise.all(jobs).then(() => undefined);
}

onChange(() => void refreshPositions());

// ---------------------------------------------------------------- element editors

export interface EditorContext {
  lang: string;
  type: string;
  /** Label id to reference from the editing surface (aria-labelledby). */
  labelId: string;
}

export interface EditorInstance {
  getBody(): unknown;
  /** Returns an error message if the content is invalid. */
  validate?(): string | null;
  destroy?(): void;
}

export type EditorFactory = (container: HTMLElement, body: unknown, ctx: EditorContext) => EditorInstance;

const editors = new Map<string, EditorFactory>();

/** Registers the editor of an element type. */
export function registerEditor(type: string, factory: EditorFactory): void {
  editors.set(type, factory);
}

/** Returns the editor of a type (plugins can wrap the editor of a base type). */
export function getEditor(type: string): EditorFactory | undefined {
  return editors.get(type);
}

// ---------------------------------------------------------------- admin pages

export interface AdminPage {
  id: string;
  label: () => string;
  order?: number;
  render: (container: HTMLElement) => Promise<void> | void;
}

const adminPages: AdminPage[] = [];

/** Adds a page to the administration area (/admin/<id>). */
export function registerAdminPage(p: AdminPage): void {
  adminPages.push(p);
  adminPages.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

export function listAdminPages(): AdminPage[] {
  return adminPages;
}

/** Element type label in the current language. */
export function typeLabel(id: string): string {
  const ti = state.site?.element_types.find((x) => x.id === id);
  return ti ? (ti.labels[state.lang] || ti.labels.en || id) : id;
}
