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
export { pluginSettingsForm, loadPluginSettings, savePluginSettings, MAPPING_FIELDS } from './components/settings';
export { renderAuthProviderPage } from './components/authadmin';
export { registerMFAMethodUI, mfaCodeForm, enrollFactor } from './core/mfa';
export type { MFAMethodUI, MFAStepContext, MFAEnrollContext } from './core/mfa';
export { webauthnSupported, webauthnCreate, webauthnGet, webauthnCancelled } from './core/webauthn';
export type { AuthProviderPage } from './components/authadmin';
export type { SettingField } from './components/settings';
export { t, pick, uiLang, pluralForm } from './core/i18n';
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
export { tagInput } from './components/tags';
export { pickMedium, uploadDialog, uploadMedium, mediaURL, mediaViewer, mediaThumb, mediaDetails, openMedium, isImage } from './components/media';
export { registerOfficeRenderer, officeButtons, officeURL, openOptions, officeAdminRender, officeReady } from './components/office';
export type { OfficeRenderer } from './components/office';
export { feedButton, feedURL, feedToken, advertiseFeed } from './components/feeds';
export type { WidgetFactory } from './core/widgets';
export type { MarkdownEditor, MarkdownEditorOptions, EditorMode } from './editor/editor';
export type { Peer } from './editor/collab';
export { formatAttrs, parseAttrs } from './editor/attrs';
export { macroDialog, loadMacros, registerMacroParamControl, getMacroParamControl } from './editor/macros';
export type { MacroParamContext, MacroParamControl } from './editor/macros';
export { currentCategory } from './components/widgets';
export { sourcesEditor, registerSourceKind } from './components/sources';
export type { SourceKind } from './components/sources';
export { subjectsEditor, userPicker, groupPicker } from './components/subjects';
export type { Subject } from './components/subjects';
export type { MacroChoice } from './editor/macros';

/** Loads the visual Markdown editor (a separate file, loaded on first use). */
export function loadMarkdownEditor(): Promise<typeof import('./editor/editor')> {
  return import('./editor/editor');
}

/** The editor of text pages for the signed-in user: own choice, else the site default. */
export function editorKind(): 'visual' | 'simple' {
  const own = state.user?.prefs?.editor;
  if (own === 'visual' || own === 'simple') return own;
  return state.site?.editor === 'simple' ? 'simple' : 'visual';
}

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
  /** Live editing: another editor saved revision rev (the base of the next save). */
  onRemoteSave?: (rev: number, by?: string) => void;
}

export interface EditorInstance {
  getBody(): unknown;
  /** Returns an error message if the content is invalid. */
  validate?(): string | null;
  /** The host saved revision rev (live editing tells the other editors). */
  saved?(rev: number): void;
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

export interface UserAdminField {
  id: string;
  /** A part of the dialog "edit user" (administrators); save runs after the core data was saved. */
  render: (userID: number) => { el: HTMLElement; save: () => Promise<void> };
}

const userAdminFields: UserAdminField[] = [];

/** Adds a part to the dialog "edit user" (e.g. allowed networks). */
export function registerUserAdminField(f: UserAdminField): void {
  userAdminFields.push(f);
}

export function listUserAdminFields(): UserAdminField[] {
  return userAdminFields;
}

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
  /** Rendered below the content of an element page (e.g. comments), or above it with placement "top" (e.g. page properties). */
  render: (el: import('./core/types').ElementView) => Node | null | Promise<Node | null>;
  placement?: 'top' | 'bottom';
}

const elementExtensions: ElementExtension[] = [];

/** Adds a section below every element page. */
export function registerElementExtension(x: ElementExtension): void {
  elementExtensions.push(x);
  elementExtensions.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

/** Renders all element extensions into container. */
export async function renderElementExtensions(container: HTMLElement, el: import('./core/types').ElementView, top?: HTMLElement): Promise<void> {
  for (const x of elementExtensions) {
    try {
      const n = await x.render(el);
      const target = x.placement === 'top' && top ? top : container;
      if (n && target.isConnected) target.append(n);
    } catch (e) {
      console.error(`element extension ${x.id} failed`, e);
    }
  }
}

// ---------------------------------------------------------------- category extensions

export interface CategoryExtension {
  id: string;
  order?: number;
  /** Rendered below the content of a category page (e.g. reactions). */
  render: (c: import('./core/types').Category) => Node | null | Promise<Node | null>;
}

const categoryExtensions: CategoryExtension[] = [];

/** Adds a section below every category page. */
export function registerCategoryExtension(x: CategoryExtension): void {
  categoryExtensions.push(x);
  categoryExtensions.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

/** Renders all category extensions into container. */
export async function renderCategoryExtensions(container: HTMLElement, c: import('./core/types').Category): Promise<void> {
  for (const x of categoryExtensions) {
    try {
      const n = await x.render(c);
      if (n && container.isConnected) container.append(n);
    } catch (e) {
      console.error(`category extension ${x.id} failed`, e);
    }
  }
}

// ---------------------------------------------------------------- category settings tabs

/** A tab of the category settings (e.g. properties, appearance). visible
 * decides from the category (its permissions) whether the tab is shown. */
export interface CategorySettingsTab {
  id: string;
  order?: number;
  label: () => string;
  visible?: (c: import('./core/types').Category) => boolean;
  render: (panel: HTMLElement, c: import('./core/types').Category) => Promise<void> | void;
}

const categoryTabs: CategorySettingsTab[] = [];

export function registerCategorySettingsTab(x: CategorySettingsTab): void {
  categoryTabs.push(x);
  categoryTabs.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

export function listCategorySettingsTabs(): CategorySettingsTab[] {
  return categoryTabs;
}

// ---------------------------------------------------------------- profile sections

export interface ProfileSection {
  id: string;
  order?: number;
  /** A section of "My profile" (e.g. favorites); null hides it. */
  render: () => Node | null | Promise<Node | null>;
}

const profileSections: ProfileSection[] = [];

/** Adds a section to the profile page. */
export function registerProfileSection(x: ProfileSection): void {
  profileSections.push(x);
  profileSections.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

/** Renders the profile sections of plugins. */
export async function renderProfileSections(): Promise<Node[]> {
  const out: Node[] = [];
  for (const x of profileSections) {
    try {
      const n = await x.render();
      if (n) out.push(n);
    } catch (e) {
      console.error(`profile section ${x.id} failed`, e);
    }
  }
  return out;
}

// ---------------------------------------------------------------- menu styles (e.g. mega menu)

/** A menu item as given to menu styles (resolved for the viewer). */
export interface MenuItem {
  label: Record<string, string>;
  description?: Record<string, string>;
  href?: string;
  new_tab?: boolean;
  submenu?: boolean;
  children?: MenuItem[];
}

export interface MenuStyle {
  id: string;
  label: () => string;
  /** extra item fields the administration offers for this style */
  itemFields?: ('description')[];
  /** renders a menu (horizontal positions); link() renders an item link like the default menus */
  render: (menu: { id: number; name: string; items: MenuItem[] }, helpers: { label: (it: MenuItem) => string; link: (it: MenuItem) => HTMLElement }) => HTMLElement;
}

const menuStyles = new Map<string, MenuStyle>();

/** Registers a style for horizontal menus (plugins such as core.megamenu). */
export function registerMenuStyle(s: MenuStyle): void {
  menuStyles.set(s.id, s);
}

export function listMenuStyles(): MenuStyle[] {
  return [...menuStyles.values()];
}

// ---------------------------------------------------------------- reactions (provided by a plugin)

/** A plugin that shows reactions (e.g. core.reactions) on pages, categories and comments. */
export interface ReactionsProvider {
  /** loads the reactions of many targets at once (e.g. all comments of a page) */
  prefetch: (type: string, ids: number[]) => Promise<void>;
  /** the reaction bar of a target, or null */
  render: (type: string, id: number, title: string) => HTMLElement | null;
}

let reactionsProvider: ReactionsProvider | null = null;

export function registerReactions(p: ReactionsProvider): void {
  reactionsProvider = p;
}

/** Reaction bar of a target (null without a reactions plugin). */
export function reactionBar(type: string, id: number, title: string): HTMLElement | null {
  return reactionsProvider?.render(type, id, title) ?? null;
}

/** Loads the reactions of many targets before reactionBar is called for them. */
export async function prefetchReactions(type: string, ids: number[]): Promise<void> {
  if (reactionsProvider && ids.length) await reactionsProvider.prefetch(type, ids).catch(() => undefined);
}

// ---------------------------------------------------------------- macros (for editors)

export type { MacroParam, MacroInfo } from './core/types';

// ---------------------------------------------------------------- publish options

/** PublishOption adds a choice to the publish dialog (e.g. "show as news").
 * render returns null when the option does not apply; the value is sent to
 * the server as options[id] and applied by the plugin's content.PublishOption. */
export interface PublishOption {
  id: string;
  render: (ctx: { categoryId: number; type: string; elementId: number | null; lang: string }) => Promise<{ el: HTMLElement; value: () => unknown } | null>;
}

const publishOptions: PublishOption[] = [];

export function registerPublishOption(o: PublishOption): void {
  publishOptions.push(o);
}

export function listPublishOptions(): PublishOption[] {
  return publishOptions;
}

// ---------------------------------------------------------------- edit fields

/** EditField adds a part to the editing of elements of every type (e.g.
 * properties). Its value is sent as fields[id] when saving, publishing and
 * in drafts, and stored by the plugin's content.ElementField in the
 * transaction of the element. render returns null when it does not apply;
 * initial is the value of a draft (if any). */
export interface EditFieldContext { type: string; categoryId: number; elementId: number | null; lang: string; initial?: unknown }
export interface EditField {
  id: string;
  order?: number;
  render: (ctx: EditFieldContext) => Promise<{ el: HTMLElement; value: () => unknown; validate?: () => string | null } | null>;
}

const editFields: EditField[] = [];

export function registerEditField(f: EditField): void {
  editFields.push(f);
  editFields.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

export function listEditFields(): EditField[] {
  return editFields;
}

// ---------------------------------------------------------------- page actions

/** PageAction adds a button to the actions of element and category pages
 * (e.g. "Share"); render returns null when it does not apply. */
export interface PageAction {
  id: string;
  order?: number;
  render: (target: { type: 'element' | 'category'; id: number; title: string; categoryId: number }) => HTMLElement | null;
}

const pageActions: PageAction[] = [];

export function registerPageAction(a: PageAction): void {
  pageActions.push(a);
  pageActions.sort((x, y) => (x.order ?? 50) - (y.order ?? 50));
}

/** pageActionButtons renders the registered page actions for a target. */
export function pageActionButtons(target: { type: 'element' | 'category'; id: number; title: string; categoryId: number }): HTMLElement[] {
  const out: HTMLElement[] = [];
  for (const a of pageActions) {
    try {
      const el = a.render(target);
      if (el) out.push(el);
    } catch { /* a broken plugin must not break the page */ }
  }
  return out;
}
