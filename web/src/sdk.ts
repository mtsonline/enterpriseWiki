// Frontend plugin SDK. Plugins import from "@ewiki/sdk" and export a
// register() function from plugins/<dir>/web/index.ts.
//
//   import { registerEditor, h } from '@ewiki/sdk';
//   export function register() { registerEditor('my.type', (el, body) => ...); }
//
// See docs/developer/plugins.md.
import { state, onChange } from './core/state';
import { t } from './core/i18n';

export { h, mount, append, trustedHTML, uid, fmtDate, srOnly } from './core/dom';
export type { Child } from './core/dom';
export { api, ApiError, qs, uploadFile, humanSize } from './core/api';
export type { UploadedFile } from './core/api';
export { pluginSettingsForm, MAPPING_FIELDS } from './components/settings';
export type { SettingField } from './components/settings';
export { t, pick, uiLang } from './core/i18n';
export { state, onChange, isAdmin, hasPerm } from './core/state';
export { toast, announce } from './core/announce';
export { navigate, route, rerender } from './core/router';
export { button, field, textInput, textArea, select, checkbox, openDialog, confirmDialog, formDialog, langFields, errorText, loading, pageHeader } from './components/ui';
export type * from './core/types';
export { registerWidget, hydrate } from './core/widgets';
export type { TreeNode } from './components/widgets';
export { categorySelect, elementSelect, newCategory, newElement } from './components/pickers';
export type { CategorySelect, ElementSelect } from './components/pickers';
export { watchButton } from './components/watch';
export { feedButton, feedURL, feedToken, advertiseFeed } from './components/feeds';
export type { WidgetFactory } from './core/widgets';

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
export function resolveTemplate(id: string, fallback = 'theme.wiki'): ResolvedTemplate | null {
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

/** The template in use (with inherited positions). */
export function activeTemplate(): ResolvedTemplate | null {
  return resolveTemplate(state.site?.template ?? 'theme.wiki');
}

/** Human readable name of a template position (core locale "position.<name>"). */
export function positionLabel(name: string): string {
  const k = `position.${name}`;
  const v = t(k);
  return v === k ? name : v;
}

/**
 * Choices for "where should this appear": the positions of the active
 * template (plus the current value, marked, if the template does not offer it).
 */
export function positionOptions(current?: string, exclude: string[] = ['header-tools', 'cms-nav']): { value: string; label: string }[] {
  const list = (activeTemplate()?.positions ?? ['header-nav', 'sidebar', 'footer']).filter((p) => !exclude.includes(p));
  const opts = list.map((p) => ({ value: p, label: `${positionLabel(p)} (${p})` }));
  if (current && !list.includes(current)) opts.unshift({ value: current, label: t('positions.not_in_template', { name: current }) });
  return opts;
}

/** Human readable name of a template (locale "template.<id>"). */
export function templateLabel(id: string): string {
  const k = `template.${id}`;
  const v = t(k);
  return v === k ? id : v;
}

// ---------------------------------------------------------------- positions (slots)

export interface SlotDef {
  id: string;
  /** Position name, or "*" for every position (render receives the name). */
  position: string;
  order?: number;
  render: (position: string) => Node | null | Promise<Node | null>;
}

const slots: SlotDef[] = [];
const positionEls = new Map<string, Set<HTMLElement>>();

/** Registers a widget for a template position (e.g. "header", "sidebar", "footer"). */
export function registerSlot(def: SlotDef): void {
  slots.push(def);
  slots.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

const slotOf = new WeakMap<Node, string>();

/** Path of child indexes from root to node. */
function pathTo(root: Node, node: Node): number[] {
  const path: number[] = [];
  for (let n: Node | null = node; n && n !== root; n = n.parentNode) {
    if (!n.parentNode) return [];
    path.unshift(Array.prototype.indexOf.call(n.parentNode.childNodes, n));
  }
  return path;
}

async function fill(el: HTMLElement, name: string): Promise<void> {
  const nodes: Node[] = [];
  for (const s of slots.filter((x) => x.position === name || x.position === '*')) {
    try {
      const n = await s.render(name);
      if (n) {
        slotOf.set(n, s.id);
        nodes.push(n);
      }
    } catch (e) {
      console.error(`slot ${s.id} failed`, e);
    }
  }
  // Keep the keyboard focus on the same control when a position is
  // re-rendered (e.g. the language selector after switching the language).
  const active = document.activeElement;
  let restore: { slot: string; path: number[] } | null = null;
  if (active && el.contains(active)) {
    const top = Array.from(el.childNodes).find((c) => c.contains(active));
    const slot = top && slotOf.get(top);
    if (top && slot) restore = { slot, path: pathTo(top, active) };
  }
  el.replaceChildren(...nodes);
  if (restore) {
    let n: Node | undefined = nodes.find((x) => slotOf.get(x) === restore!.slot);
    for (const i of restore.path) n = n?.childNodes[i];
    if (n instanceof HTMLElement) n.focus();
  }
}

/** Creates a live position container. */
export function positionContainer(name: string, tag: keyof HTMLElementTagNameMap = 'div'): HTMLElement {
  const el = document.createElement(tag);
  el.className = `ew-pos ew-pos--${name}`;
  el.dataset.position = name;
  el.dataset.label = positionLabel(name);
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
  /** Id of the element being edited (undefined while creating). */
  elementId?: number;
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
  /** global permission needed for the page (declared in plugin.json); default: administrators only */
  permission?: string;
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

// ---------------------------------------------------------------- element extensions

export interface ElementExtension {
  id: string;
  order?: number;
  /** Rendered below the content of an element page (e.g. comments). */
  render: (el: import('./core/types').ElementView) => Node | null | Promise<Node | null>;
}

const elementExtensions: ElementExtension[] = [];

/** Adds a section below every element page. */
export function registerElementExtension(x: ElementExtension): void {
  elementExtensions.push(x);
  elementExtensions.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

/** Renders all element extensions into container. */
export async function renderElementExtensions(container: HTMLElement, el: import('./core/types').ElementView): Promise<void> {
  for (const x of elementExtensions) {
    try {
      const n = await x.render(el);
      if (n && container.isConnected) container.append(n);
    } catch (e) {
      console.error(`element extension ${x.id} failed`, e);
    }
  }
}

// ---------------------------------------------------------------- macros (for editors)

export interface MacroParam {
  name: string;
  labels: Record<string, string>;
  type: 'text' | 'textarea' | 'number' | 'select' | 'bool' | 'url';
  options?: string[];
  default?: string;
  required?: boolean;
}

export interface MacroInfo {
  name: string;
  plugin: string;
  labels: Record<string, string>;
  description: Record<string, string>;
  params: MacroParam[] | null;
  has_body: boolean;
}
