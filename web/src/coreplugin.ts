// Frontend interface of core plugins: plugins compiled into the wiki. It
// changes together with the core (no stability promise). Core plugins import
// from "@ewiki/coreplugin" and export a register() function from
// plugins/<dir>/web/index.ts.
//
//   import { registerEditor, h } from '@ewiki/coreplugin';
//   export function register() { registerEditor('my.type', (el, body) => ...); }
//
// See docs/en/developer/plugins.md.
import { state, onChange } from './core/state';
import { t } from './core/i18n';
import { api } from './core/api';
import { CATEGORY_POSITIONS, DASHBOARD_POSITIONS, ELEMENT_POSITIONS, EDITOR_POSITIONS, kindOf, resolvePlacements, type PagePosition, type PlacementLevel, type PositionKind } from './core/placement';
export { SCHEMA as BLOCK_OPTIONS_SCHEMA, optionDef, problems as optionProblems, maxColumns, type BlockOptions, type OptionDef, type OptionGroup } from './core/blockoptions';
export { CATEGORY_POSITIONS, DASHBOARD_POSITIONS, ELEMENT_POSITIONS, EDITOR_POSITIONS, kindOf, positionFits, resolvePlacements, type PagePosition, type Placement, type PlacementLevel, type PositionKind } from './core/placement';

import { h } from './core/dom';
import { optionsKey, shownNow, type BlockOptions } from './core/blockoptions';
import { styleFrame } from './core/optionframes';
export { tile } from './components/tile';
export { registerTaskSection, type TaskSection } from './views/tasks';
export { logout } from './components/widgets';
export { h, mount, append, trustedHTML, uid, fmtDate, srOnly } from './core/dom';
export type { Child } from './core/dom';
export { api, ApiError, qs, uploadFile, humanSize } from './core/api';
export { newest } from './core/newest';
export type { UploadedFile } from './core/api';
export { pluginSettingsForm, loadPluginSettings, savePluginSettings, MAPPING_FIELDS } from './components/settings';
export { renderAuthProviderPage } from './components/authadmin';
export { registerMFAMethodUI, mfaCodeForm, enrollFactor } from './core/mfa';
export type { MFAMethodUI, MFAStepContext, MFAEnrollContext } from './core/mfa';
export { webauthnSupported, webauthnCreate, webauthnGet, webauthnCancelled } from './core/webauthn';
export { signatureMethods, signChallenge, openSignature } from './components/signature';
export type { SignatureMethod, SignChallenge } from './components/signature';
export type { AuthProviderPage } from './components/authadmin';
export type { SettingField } from './components/settings';
export { t, pick, uiLang, pluralForm } from './core/i18n';
export { state, onChange, isAdmin, hasPerm } from './core/state';
export { toast, announce } from './core/announce';
export { consentGiven, registerConsentCategory, onConsentChange, type ConsentCategory } from './core/consent';
export { navigate, route, rerender } from './core/router';
export { button, field, textInput, textArea, select, checkbox, openDialog, confirmDialog, formDialog, langFields, errorText, loading, pageHeader } from './components/ui';
export type * from './core/types';
export { registerWidget, hydrate, claimWidgetControls, provideWidgetControls, type WidgetControl } from './core/widgets';
import { widgetControls } from './core/widgets';
export { instanceLabels } from './components/instanceLabels';
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
  register?: () => void | Promise<void>;
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

let shownTemplate = '';

/** Records the template the page is shown with (set by the shell). */
export function setShownTemplate(id: string): void {
  shownTemplate = id;
}

let previewTpl = '';
/** A template shown instead of the one that applies, only in this window (the
 * preview of the template designer). Changes nothing for anybody else. */
export function setPreviewTemplate(id: string): void { previewTpl = id; }
/** The template shown instead of the one that applies ('' = none). */
export function previewTemplate(): string { return previewTpl; }

const tryKey = () => `ewiki:try-template:${state.site?.site ?? ''}`;
/** A template someone tries only for themselves, in this browser, in place of
 * the template of the site (categories with a template of their own keep it).
 * Changes nothing for anybody else; '' = none or no longer there. */
export function triedTemplate(): string {
  let id = '';
  try { id = localStorage.getItem(tryKey()) ?? ''; } catch { /* storage unavailable: nothing tried */ }
  return id && templates.has(id) && resolveTemplate(id) ? id : '';
}
/** Tries a template in this browser ('' ends it). */
export function setTriedTemplate(id: string): void {
  try { if (id) localStorage.setItem(tryKey(), id); else localStorage.removeItem(tryKey()); } catch { /* not kept */ }
}
/** The template of the site as this browser shows it: the one tried, else the setting. */
export function siteTemplate(): string { return triedTemplate() || state.site.template; }

/** The template in use on this page (with inherited positions): the one of the category, else the site template. */
export function activeTemplate(): ResolvedTemplate | null {
  return resolveTemplate(shownTemplate || (state.site?.template ?? 'theme.wiki'));
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
  if (v !== k) return v;
  // templates offered by plugins (templates of data) bring their names
  const x = state.site?.templates?.find((y) => y.id === id) as { name?: string; names?: Record<string, string> } | undefined;
  return x?.names?.[state.lang] || x?.name || id;
}

// ---------------------------------------------------------------- positions (slots)


/** The page a block of an element page or of the editing page is rendered
 * for: the element (if there is one) and the parts of the editing page
 * (editor blocks; each part exists once and keeps its state). */
export interface PageContext { target?: PageTarget; view?: import('./core/types').ElementView; parts?: Map<string, Node | null>; /** a category page: its data (views/category.ts) */ category?: unknown; /** the path (breadcrumbs) of the page, a new node each call */ path?: () => HTMLElement }

/** The context of an element page. */
export interface ElementContext extends PageContext { target: PageTarget; view: import('./core/types').ElementView }

export interface SlotDef {
  id: string;
  /** The kind of positions the block fits (default "site"). Element blocks
   * render only on element pages and receive the element (ctx). */
  kind?: PositionKind;
  /** Default position, or "*" for every position (render receives the name;
   * such slots are not moved). */
  position: string;
  order?: number;
  /** Name of the building block in the placement administration. */
  label?: () => string;
  /** The block is never lost: placed nowhere (of its kind), it goes to the
   * first position (the user menu, the management of an element page). */
  always?: boolean;
  /** The block exists once per page (the content of a page, a featured
   * page): only its first placement applies. */
  single?: boolean;
  /** Blocks that stand in for this one: placed on the page (at a position of
   * its kind), this block is left out (the title bar holds the title and
   * the path, so they are not shown twice). */
  replacedBy?: string[];
  /** Instances of the block (menus, carousels, content blocks …): each is a
   * block of its own ("<id>/<key>"; key "" is the block itself) with its
   * own default position (empty: not placed) that the placement levels can
   * change, hide or repeat. */
  instances?: () => Promise<SlotInstance[]>;
  /** Renders the block (an instance: its key) at a position. */
  render: (position: string, key?: string, ctx?: PageContext) => Node | null | Promise<Node | null>;
}

/** An instance of a block (see SlotDef.instances). */
export interface SlotInstance { key: string; label: string; position: string; order?: number }

/** A block that can be placed: a slot or one instance of it. */
export interface PlaceableBlock { id: string; label: string; position: string; order: number; slot: SlotDef; key?: string; kind: PositionKind; always?: boolean }

// ---------------------------------------------------------------- placement


let placementCategory = 0;
let placementElement = 0;
let placementCache: { key: string; levels: Promise<PlacementLevel[]> } | null = null;

/** Sets the category (and element) of the page shown: their placements
 * apply. */
export function setPlacementContext(categoryId: number, elementId = 0): void {
  if (placementCategory === categoryId && placementElement === elementId) return;
  placementCategory = categoryId;
  placementElement = elementId;
  // the positions may already be filled for the previous page: fill them
  // again with the placements of this category (once per change; new
  // positions wait for it)
  refreshPending = true;
  queueMicrotask(() => void refreshPositions());
}

/** Drops the cached placements and instance lists (after they were changed). */
export function invalidatePlacements(): void {
  placementCache = null;
  blockCache = null;
}

let blockCache: Promise<PlaceableBlock[]> | null = null;

/** The blocks that can be placed: every slot except "*", and the instances
 * of slots with instances (loaded again with every refresh of the
 * positions; plugins cache their own lists). */
export function placeableBlocks(): Promise<PlaceableBlock[]> {
  if (!blockCache) {
    const p = (async () => {
      const out: PlaceableBlock[] = [];
      for (const s of slots) {
        if (s.instances) {
          let list: SlotInstance[] = [];
          try { list = await s.instances(); } catch (e) { console.error(`instances of ${s.id} failed`, e); }
          for (const i of list) out.push({ id: i.key ? `${s.id}/${i.key}` : s.id, label: i.label, position: i.position, order: i.order ?? s.order ?? 50, slot: s, key: i.key, kind: s.kind ?? 'site' });
        } else if (s.position !== '*') {
          out.push({ id: s.id, label: s.label?.() ?? s.id, position: s.position, order: s.order ?? 50, slot: s, kind: s.kind ?? 'site', always: s.always });
        }
      }
      return out;
    })();
    blockCache = p;
  }
  return blockCache;
}

/** The placement levels of the page shown (cached). */
export function currentPlacementLevels(): Promise<PlacementLevel[]> {
  return placementLevels();
}

function placementLevels(): Promise<PlacementLevel[]> {
  const tpl = activeTemplate()?.id ?? '';
  const key = `${tpl}|${placementCategory}|${placementElement}|${state.user?.id ?? 0}`;
  if (placementCache?.key !== key) {
    const q = new URLSearchParams({ template: tpl });
    if (placementCategory) q.set('category', String(placementCategory));
    if (placementElement) q.set('element', String(placementElement));
    placementCache = { key, levels: api.get<PlacementLevel[]>(`placements?${q}`).catch(() => []) };
  }
  return placementCache.levels;
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

const elementCtx = new WeakMap<HTMLElement, { ctx: PageContext; filled?: () => void }>();

// ------------------------------------------------------------ options of placed blocks

const optionFrames = new WeakMap<Node, { key: string; frame: HTMLElement }>();

/** A placed block with options: a frame with the look of its options (see
 * optionframes.ts). The same node keeps its frame (nothing is moved out of
 * the page that need not be). */
function withOptions(n: Node, o: BlockOptions): Node {
  const key = optionsKey(o);
  const have = optionFrames.get(n);
  if (have && have.key === key) return have.frame;
  const body = h('div', { class: 'ew-bo-body' });
  const frame = h('div', { class: 'ew-bo' }, body);
  if (!styleFrame(frame, o)) return n; // options of an invalid form are ignored
  body.append(n);
  optionFrames.set(n, { key, frame });
  return frame;
}

const fillGen = new WeakMap<HTMLElement, number>();
let refreshPending = false;

/** The positions of a kind on the page shown. */
function availOf(kind: PositionKind): readonly string[] {
  return kind === 'element' ? ELEMENT_POSITIONS : kind === 'category' ? CATEGORY_POSITIONS : kind === 'editor' ? EDITOR_POSITIONS
    : kind === 'dashboard' ? DASHBOARD_POSITIONS : (activeTemplate()?.positions ?? []);
}

async function fill(el: HTMLElement, name: string): Promise<void> {
  // only the latest filling of a container applies (an older one may finish later)
  const gen = (fillGen.get(el) ?? 0) + 1;
  fillGen.set(el, gen);
  const nodes: Node[] = [];
  const blocks = await placeableBlocks();
  const placed = resolvePlacements(await placementLevels(), blocks, { personal: !!state.site.personal_placement });
  const tplPositions = activeTemplate()?.positions ?? [];
  const ec = elementCtx.get(el);
  const kind = kindOf(name);
  if (kind !== 'site' && !ec) return; // positions of a page belong to that page
  const here: { s: SlotDef; id: string; key?: string; sort: number; options?: BlockOptions }[] = [];
  if (kind === 'site') for (const s of slots) if (s.position === '*' && !s.instances) here.push({ s, id: s.id, sort: s.order ?? 50 });
  for (const b of blocks) {
    if (b.kind !== kind) continue;
    let ps = placed.get(b.id) ?? [];
    // blocks that must stay reachable (the user menu, the management of a
    // page …) never get lost: if one is nowhere in this template (or page),
    // it goes to the first position of the template (on a page: its default)
    if (b.always) {
      const avail = availOf(kind);
      if (!ps.some((p) => avail.includes(p.position))) ps = [{ position: kind === 'site' ? (tplPositions[0] ?? b.position) : b.position, sort: b.order }];
    }
    // a part of the editing page exists once (it keeps what was typed), so
    // does the content of a page: only the first placement
    if (kind === 'editor' || b.slot.single) ps = ps.slice(0, 1);
    // tiles the administration does not offer are on no dashboard
    if (kind === 'dashboard' && !tileOffered(b.id)) continue;
    // a block that stands in for this one is placed on the page: leave it out
    if (b.slot.replacedBy?.some((r) => (placed.get(r) ?? []).some((p) => availOf(kind).includes(p.position)))) continue;
    for (const p of ps) {
      if (p.position !== name) continue;
      // options may show a block only to some, at some times, in some languages (display only)
      if (!shownNow(p.options, Date.now(), !!state.user, state.lang)) continue;
      here.push({ s: b.slot, id: b.id, key: b.key, sort: p.sort, options: p.options });
    }
  }
  here.sort((a, b) => a.sort - b.sort);
  // render all at once (each block loads its own data)
  const rendered = await Promise.all(here.map(async ({ s, id, key, options }) => {
    try {
      const n0 = await s.render(name, key, ec?.ctx);
      const n = n0 && options && Object.keys(options).length ? withOptions(n0, options) : n0;
      // a tile of the dashboard: a frame (moving it with the mouse takes the frame)
      if (n && kind === 'dashboard') return { id, n: h('div', { class: 'ew-tile', 'data-block': id }, n) };
      return { id, n };
    } catch (e) {
      console.error(`slot ${id} failed`, e);
      return { id, n: null };
    }
  }));
  for (const { id, n } of rendered) {
    if (n) {
      slotOf.set(n, id);
      nodes.push(n);
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
  if (fillGen.get(el) !== gen || (!el.isConnected && ec)) return; // newer filling, or the page was left
  reconcile(el, nodes);
  if (restore) {
    let n: Node | undefined = nodes.find((x) => slotOf.get(x) === restore!.slot);
    for (const i of restore.path) n = n?.childNodes[i];
    if (n instanceof HTMLElement) n.focus();
  }
  ec?.filled?.();
}

/** Puts nodes as the children of el, moving only what is not in place: a
 * node that stays (the content of a page with its editor) is not taken out
 * of the page. */
function reconcile(el: HTMLElement, nodes: Node[]): void {
  const keep = new Set(nodes);
  for (const c of Array.from(el.childNodes)) if (!keep.has(c)) c.remove();
  nodes.forEach((n, i) => {
    const at = el.childNodes[i];
    if (at !== n) el.insertBefore(n, at ?? null);
  });
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

/** Whether the administration offers a tile for the own dashboard
 * (ui.dashboard_tiles; empty = all). */
export function tileOffered(id: string): boolean {
  const offered = state.site.dashboard_tiles ?? [];
  return offered.length === 0 || offered.includes(id);
}

/** Creates a live position of an element page (element-header …) or of the
 * editing page (editor-header …) for the page shown; filled is called after
 * every filling. */
export function pagePositionContainer(name: PagePosition, ctx: PageContext, filled?: () => void): HTMLElement {
  const el = document.createElement('div');
  el.className = `ew-pos ew-pos--${name}`;
  el.dataset.position = name;
  el.dataset.label = positionLabel(name);
  // a group of buttons, a side column: named only when there is something in it
  const toolbar = name === 'element-toolbar' || name === 'category-toolbar';
  const role = toolbar ? 'group' : name === 'element-aside' || name === 'category-aside' || name === 'editor-aside' ? 'complementary' : '';
  const label = toolbar ? t('common.actions') : positionLabel(name);
  elementCtx.set(el, {
    ctx, filled: () => {
      if (role && el.childNodes.length) {
        el.setAttribute('role', role);
        el.setAttribute('aria-label', label);
      } else {
        el.removeAttribute('role');
        el.removeAttribute('aria-label');
      }
      filled?.();
    },
  });
  if (!positionEls.has(name)) positionEls.set(name, new Set());
  positionEls.get(name)!.add(el);
  if (!refreshPending) void fill(el, name);
  return el;
}

/** Re-renders all positions (after navigation, login, language change). */
export function refreshPositions(): Promise<void> {
  refreshPending = false;
  blockCache = null; // instances may have changed (a new menu …)
  const jobs: Promise<void>[] = [];
  for (const [name, els] of positionEls) {
    for (const el of els) {
      // a new container not yet in the page is filled too (it waited for this)
      if (!el.isConnected && fillGen.has(el)) els.delete(el);
      else jobs.push(fill(el, name));
    }
  }
  return Promise.all(jobs).then(() => undefined);
}

onChange(() => void refreshPositions());

// ---------------------------------------------------------------- element editors

export interface EditorContext {
  /** Where the editor puts its formatting toolbar (a block of the editing
   * page that can be moved or hidden); without it the toolbar stays above
   * the editing surface. */
  toolbarHost?: HTMLElement;
  /** Buttons of the formatting toolbar to show (names as in
   * content.EditorTools; undefined or ['*'] = all). Hiding a button does
   * not forbid the formatting (shortcuts and Markdown keep working). */
  tools?: string[];
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

/** Whether the toolbar shows button tool (see EditorContext.tools). */
export function toolShown(tools: string[] | undefined, tool: string): boolean {
  return !tools || tools.includes('*') || tools.includes(tool);
}

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
  /** Name in the placement administration. */
  label?: () => string;
  /** Rendered below the content of an element page (e.g. comments), or above it with placement "top" (e.g. page properties). */
  render: (el: import('./core/types').ElementView) => Node | null | Promise<Node | null>;
  placement?: 'top' | 'bottom';
}

const elementExtensions: ElementExtension[] = [];

/** Registers a control of a widget (search, "new post" …) as an element
 * block of the pages of its element type: on its own page the widget hands
 * the control over (provideWidgetControls under key); there it can be
 * moved, hidden or repeated like any block. */
export function registerWidgetControl(c: { id: string; type: string; key: string; label: () => string; position: (typeof ELEMENT_POSITIONS)[number]; order?: number }): void {
  registerSlot({ id: c.id, kind: 'element', position: c.position, order: c.order ?? 50, label: c.label,
    render: async (_p, _k, ctx) => {
      if (!ctx?.view || ctx.view.type !== c.type) return null;
      return (await widgetControls(ctx.view.id))[c.key]?.({ title: ctx.view.title }) ?? null;
    } });
}

/** Adds a section to every element page: an element block (id of the
 * extension) at element-footer, or element-header with placement "top". */
export function registerElementExtension(x: ElementExtension): void {
  elementExtensions.push(x);
  elementExtensions.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
  registerSlot({ id: x.id, kind: 'element', position: x.placement === 'top' ? 'element-header' : 'element-footer', order: 100 + (x.order ?? 50), label: x.label ?? (() => x.id), render: (_p, _k, ctx) => (ctx?.view ? x.render(ctx.view) : null) });
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

/** PageTarget is the page an action is for; elementType only for elements. */
export interface PageTarget { type: 'element' | 'category'; id: number; title: string; categoryId: number; elementType?: string }

/** PageAction adds a button to the actions of element and category pages
 * (e.g. "Share"); render returns null when it does not apply. */
export interface PageAction {
  id: string;
  order?: number;
  /** Name in the placement administration. */
  label?: () => string;
  render: (target: PageTarget) => HTMLElement | null;
}

const pageActions: PageAction[] = [];

/** Registers a page action. On element pages it is an element block (id of
 * the action, default position element-toolbar) that can be moved, hidden
 * or repeated; on category pages it is the category block
 * core.category.action/<id with "_" for ".">. */
export function registerPageAction(a: PageAction): void {
  pageActions.push(a);
  pageActions.sort((x, y) => (x.order ?? 50) - (y.order ?? 50));
  registerSlot({ id: a.id, kind: 'element', position: 'element-toolbar', order: 100 + (a.order ?? 50), label: a.label ?? (() => a.id), render: (_p, _k, ctx) => (ctx?.target ? a.render(ctx.target) : null) });
}

/** Further columns for the table of a category overview (the view
 * "table"), e.g. the properties of the pages. A provider offers its keys
 * and gives the values of the pages shown, with the rights of the reader;
 * what the reader may not see stays empty. */
export interface OverviewColumns {
  id: string;
  /** keys to choose in the display settings of a category */
  options(categoryId: number): Promise<{ key: string; label: string }[]>;
  /** the labels of the keys and their values (as text) for these pages */
  values(ids: number[], keys: string[], lang: string): Promise<{ columns: { key: string; label: string }[]; values: Map<number, Record<string, string>> }>;
}
const overviewColumns: OverviewColumns[] = [];

/** registerOverviewColumns adds columns for the table of category overviews. */
export function registerOverviewColumns(p: OverviewColumns): void {
  overviewColumns.push(p);
}

/** listOverviewColumns returns the providers of table columns. */
export function listOverviewColumns(): readonly OverviewColumns[] {
  return overviewColumns;
}

/** pageActionList returns the registered page actions (in order). */
export function pageActionList(): readonly PageAction[] {
  return pageActions;
}

/** pageActionButtons renders the registered page actions for a target. */
export function pageActionButtons(target: PageTarget): HTMLElement[] {
  const out: HTMLElement[] = [];
  for (const a of pageActions) {
    try {
      const el = a.render(target);
      if (el) out.push(el);
    } catch { /* a broken plugin must not break the page */ }
  }
  return out;
}
