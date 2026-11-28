// Application bootstrap.
import { dashboardView, registerDashboardTiles } from './views/dashboard';
import { verifyEmailView } from './views/identity';
import { registerConfirmView, registerView } from './views/register';
import { resetConfirmView, resetView } from './views/passwordReset';
import './styles/base.css';
import './styles/code.css';
import './styles/math.css';
import './editor/editor.css';
import { mediaView } from './views/media';
import { tasksView } from './views/tasks';
import { notificationsView } from './views/notifications';
import { signatureVerifyView } from './views/signatures';
import { officeView } from './components/office';
import { api, ApiError, setWaitNotice, setWaitPrompt } from './core/api';
import { errorText, waitDialog } from './components/ui';
import { toast } from './core/announce';
import { h, mount } from './core/dom';
import { MAIN } from './core/templateSchema';
import { loadMessages, t } from './core/i18n';
import { onBeforeRender, onNavigate, rerender, route, setMain, setNotFound, start } from './core/router';
import { hasPerm, isAdmin, rememberedLang, state, stored } from './core/state';
import type { Principal, Site } from './core/types';
import { categoryTree, ctx, currentSpace, setSpace, editToggle, helpToggle, languageSwitcher, searchBox, userMenu } from './components/widgets';
import { watchHelp } from './core/helptexts';
import { watchDateInputs } from './components/datepicker';
import { readProgress } from './components/readprogress';
import { pluginModules } from './generated/plugins';
import { applyScheme } from './core/scheme';
import { schemeBlock } from './components/scheme';
import { brandHeader, printBrand } from './components/brand';
import { applyTemplateLook, hasTemplateSettings, loadTemplateSettings, positionContainer, refreshPositions, setPreviewTemplate, setTemplateSettingsDraft, templateSettingsOf, type TemplateSettingsState, registerPageAction, registerProfileSection, registerSlot, registerTemplate, previewTemplate, resolveTemplate, setShownTemplate, siteTemplate, triedTemplate } from './coreplugin';
import { tryBar } from './components/trytemplate';
import { closedPage, maintenanceBar } from './components/maintenance';
import { cookieBanner, cookieSettingsBlock, privacyGate } from './components/consent';
import { instanceRightsButton } from './components/instanceRights';
import { instanceLabelsButton } from './components/instanceLabels';
import { placementEditor } from './components/placement';
import { blockOptionsControl } from './components/blockoptions';
import { iconControl } from './components/iconpicker';
import { categoryControl, groupControl, imageControl, pageControl, templateControl, userControl } from './components/macroparams';
import { registerMacroParamControl } from './editor/macros';
import { notificationBell } from './components/bell';
import { showPopups } from './components/popups';
import { adminView } from './views/admin';
import { registerSourceControls } from './components/sources';
import { installMacroPager } from './core/pager';
import { startTooltips } from './components/tooltip';
import { categorySettingsView } from './views/categorySettings';
import { spaceKeyView, spacesView } from './views/spaces';
import { categoryView, editElementView, elementView, draftEditView, historyView, homeView, newElementView, siteHomeView, registerCategoryPageBlocks, registerElementBlocks } from './views/content';
import { loginView, notFoundView, profileView, searchView, trashView } from './views/misc';
import { personView } from './views/people';

function pickLanguage(site: Site, user: Principal | null): string {
  const candidates = [new URLSearchParams(location.search).get('lang'), rememberedLang(), user?.language, ...navigator.languages.map((l) => l.split('-')[0]), site.default_language];
  return candidates.find((l) => l && site.languages.includes(l)) ?? site.default_language;
}

function registerCore(): void {
  registerSourceControls();
  route('/', homeView);
  route('/start', siteHomeView);
  route('/c/:id', categoryView);
  route('/c/:id/new', newElementView);
  route('/c/:id/settings', categorySettingsView);
  route('/s/:key', spaceKeyView);
  route('/spaces', spacesView);
  route('/e/:id', elementView);
  route('/e/:id/edit', editElementView);
  route('/e/:id/history', historyView);
  route('/d/:id/edit', draftEditView);
  route('/search', searchView);
  route('/login', loginView);
  route('/verify-email', verifyEmailView);
  route('/register', registerView);
  route('/register/confirm', registerConfirmView);
  route('/password-reset', resetView);
  route('/password-reset/confirm', resetConfirmView);
  route('/profile', profileView);
  route('/people/:id', personView);
  route('/dashboard', dashboardView);
  route('/tasks', tasksView);
  route('/notifications', notificationsView);
  route('/trash', trashView);
  route('/signatures/verify', signatureVerifyView);
  route('/media', mediaView);
  route('/office/:id', officeView);
  route('/admin', adminView);
  route('/admin/:section', adminView);
  setNotFound(notFoundView);

  registerPageAction({ id: 'core.instance-rights', order: 90, label: () => t('instance.rights'), render: instanceRightsButton });
  registerPageAction({ id: 'core.instance-labels', order: 91, label: () => t('labels.button'), render: instanceLabelsButton });
  registerElementBlocks();
  registerCategoryPageBlocks();
  registerDashboardTiles();
  // the options of every block in the content (parameter "options" of a macro)
  registerMacroParamControl('blockoptions', blockOptionsControl);
  registerMacroParamControl('icon', iconControl);
  registerMacroParamControl('page', pageControl);
  registerMacroParamControl('category', categoryControl);
  registerMacroParamControl('template', templateControl);
  registerMacroParamControl('image', imageControl);
  registerMacroParamControl('user', userControl);
  registerMacroParamControl('group', groupControl);
  registerProfileSection({ id: 'core.placement', order: 60, render: async () => (state.site.personal_placement && state.user ? placementEditor('user', String(state.user.id)) : null) });
  // building blocks of the core: default positions; the administration,
  // categories and (if allowed) every person can place them elsewhere
  registerSlot({ id: 'core.search', position: 'header-tools', order: 10, label: () => t('block.core.search'), render: searchBox });
  registerSlot({ id: 'core.lang', position: 'header-tools', order: 20, label: () => t('block.core.lang'), render: languageSwitcher });
  registerSlot({ id: 'core.edit', position: 'header-tools', order: 30, label: () => t('block.core.edit'), render: editToggle });
  registerSlot({ id: 'core.help', position: 'header-tools', order: 25, label: () => t('block.core.help'), render: helpToggle });
  registerSlot({ id: 'core.bell', position: 'header-tools', order: 35, label: () => t('block.core.bell'), render: () => notificationBell() });
  registerSlot({ id: 'core.user', position: 'header-tools', order: 40, always: true, label: () => t('block.core.user'), render: userMenu });
  // reading progress: not placed by default (e.g. under the header in a template)
  // cookie settings: not placed by default (e.g. in the footer)
  registerSlot({ id: 'core.cookiesettings', position: '', order: 91, label: () => t('block.core.cookiesettings'), render: () => cookieSettingsBlock() });
  // light and dark: not placed by default (e.g. in the header tools of a template)
  registerSlot({ id: 'core.scheme', position: '', order: 38, label: () => t('block.core.scheme'), render: () => schemeBlock() });
  registerSlot({ id: 'core.readprogress', position: '', order: 90, label: () => t('block.core.readprogress'), render: readProgress });
  registerSlot({ id: 'core.tree', position: 'sidebar', order: 10, label: () => t('block.core.tree'), render: categoryTree });

  // Minimal fallback layout, used when no template plugin is active.
  registerTemplate({
    id: 'core.basic',
    positions: ['header-tools', 'sidebar', 'footer'],
    // the same on every size: the sidebar stays a column beside the page
    schema: () => [
      { role: 'banner', areas: [{ kind: 'brand', positions: [] }, { kind: 'area', positions: ['header-tools'], grow: 2 }] },
      { role: 'none', areas: [{ kind: 'area', positions: ['sidebar'] }, { kind: 'content', positions: [MAIN], grow: 3 }] },
      { role: 'contentinfo', areas: [{ kind: 'area', positions: ['footer'] }] },
    ],
    layout: (ctx) => {
      const main = h('main', { id: 'main', tabindex: '-1' });
      const root = h('div', { class: 'ew-basic' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        h('header', { class: 'ew-basic-header' }, h('a', { href: '/', class: 'ew-brand' }, ...brandHeader('ew-brand-logo', 40)), ctx.position('header-tools')),
        h('div', { class: 'ew-basic-body' }, h('aside', { 'aria-label': t('nav.sidebar') }, ctx.position('sidebar')), main),
        h('footer', null, ctx.position('footer')));
      return { root, main };
    },
  });
}

// A failed request that no code handled is shown instead of failing silently.
// It is not suppressed: the browser tests still count it as an error.
window.addEventListener('unhandledrejection', (ev) => {
  // the privacy policy is due (for example a new version while the page is open)
  if (ev.reason instanceof ApiError && ev.reason.code === 'consent_required') {
    void privacyGate().then(() => location.reload());
    return;
  }
  if (ev.reason instanceof ApiError) toast(errorText(ev.reason), 'error');
});

async function boot(): Promise<void> {
  installMacroPager();
  startTooltips();
  const app = document.getElementById('app')!;
  try {
    const [site, me] = await Promise.all([api.get<Site>('site'), api.get<{ user: Principal | null; manages_categories?: boolean }>('auth/me')]);
    state.site = site;
    // the scheme of the site template at once (privacy dialog, closed page);
    // the template shown applies it again below
    applyScheme(previewTemplate() || siteTemplate());
    setWaitNotice(site.wait_notice_s);
    setWaitPrompt(waitDialog);
    state.user = me.user;
    state.managesCategories = !!me.manages_categories;
    state.lang = pickLanguage(site, me.user);
    state.editMode = !!me.user && stored('ewiki.edit') === '1';
    await loadMessages(state.lang);
  } catch (e) {
    // light colours: the page the server marked dark must not stay dark around it
    document.documentElement.dataset.shown = 'light';
    mount(app, h('main', { class: 'ew-fatal' }, h('h1', null, 'Enterprise Wiki'), h('p', { role: 'alert' }, `The server is not reachable (${String(e)}). Please try again later.`)));
    return;
  }
  // closed for maintenance: only the page that says so (signing in stays possible)
  if (state.site.maintenance?.mode === 'closed' && !isAdmin() && !location.pathname.startsWith('/login')) {
    closedPage(app);
    return;
  }
  // the privacy policy first, where it is asked for (not on the sign-in page)
  if (!location.pathname.startsWith('/login')) await privacyGate();
  document.documentElement.dataset.site = state.site.name;
  // explanations are shown or hidden for the person, on every page
  watchHelp();
  watchDateInputs();
  registerCore();
  const active = new Set(state.site.plugins ?? []);
  // a plugin may need data before the page is built (templates of data):
  // its register() returns a promise, waited for at most a few seconds
  const waits: Promise<unknown>[] = [];
  for (const p of pluginModules) {
    if (!active.has(p.id)) continue;
    try {
      const r: unknown = p.mod.register?.();
      if (r instanceof Promise) waits.push(r.catch((e: unknown) => console.error(`plugin ${p.id} failed to register`, e)));
    } catch (e) {
      console.error(`plugin ${p.id} failed to register`, e);
    }
  }
  if (waits.length) await Promise.race([Promise.allSettled(waits), new Promise((res) => { setTimeout(res, 4000); })]);
  setupSettingsPreview();
  let tpl = resolveTemplate(previewTemplate() || siteTemplate()) ?? resolveTemplate('core.basic')!;
  // the settings of the template (logos, colours …) before the page is built
  await Promise.race([loadTemplateSettings(tpl.id), new Promise((res) => { setTimeout(res, 4000); })]);
  setShownTemplate(tpl.id);
  document.body.classList.add('ew', ...tpl.classNames);
  applyScheme(tpl.id);
  // each layout gets a signal that ends it when the next one replaces it
  let ended = new AbortController();
  const build = () => {
    applyTemplateLook(tpl.id);
    ended.abort();
    ended = new AbortController();
    const layout = tpl.layout({ position: (name, tag) => positionContainer(name, tag), settings: templateSettingsOf(tpl.id), signal: ended.signal });
    mount(app, layout.root, printBrand());
    trackStickyHeader(layout.root);
    return layout;
  };
  let layout = build();
  showPositionsMode();
  cookieBanner();
  const maint = maintenanceBar();
  if (maint) document.body.prepend(maint);
  // a template tried only in this browser: a bar says so and ends it
  if (!previewTemplate() && triedTemplate() && triedTemplate() !== state.site.template) document.body.prepend(tryBar(triedTemplate()));
  // Categories may have their own template: after the page is rendered, the
  // skeleton is rebuilt with it and the page content moved over (not re-rendered).
  const useTemplate = (id: string) => {
    const next = resolveTemplate(previewTemplate() || id) ?? resolveTemplate(siteTemplate());
    if (!next || next.id === tpl.id) return;
    document.body.classList.remove(...tpl.classNames);
    tpl = next;
    setShownTemplate(tpl.id);
    document.body.classList.add('ew', ...tpl.classNames);
    applyScheme(tpl.id);
    // the settings of another template come a moment later: built again then
    if (!hasTemplateSettings(tpl.id)) {
      const id = tpl.id;
      void loadTemplateSettings(id).then(() => { if (tpl.id === id) window.dispatchEvent(new Event('ewiki:relayout')); });
    }
    const old = layout.main;
    const focused = document.activeElement;
    const nodes = [...old.childNodes];
    layout = build();
    layout.main.append(...nodes);
    setMain(layout.main);
    if (focused instanceof HTMLElement && layout.main.contains(focused)) focused.focus({ preventScroll: true });
  };
  onBeforeRender(() => { ctx.template = ''; setSpace(null); });
  document.addEventListener('ewiki:template', () => { useTemplate(ctx.template || siteTemplate()); void refreshPositions(); });
  // accent color of the space (contrast checked on the server)
  const useAccent = () => {
    const a = currentSpace?.accent;
    const root = document.documentElement.style;
    if (a) {
      root.setProperty('--ew-primary', a);
      root.setProperty('--ew-link', a);
      root.setProperty('--ew-primary-hover', `color-mix(in srgb, ${a} 80%, black)`);
    } else {
      for (const p of ['--ew-primary', '--ew-link', '--ew-primary-hover']) root.removeProperty(p);
    }
  };
  onNavigate(() => {
    useAccent();
    useTemplate(ctx.template || siteTemplate());
    void refreshPositions();
    // pop-up notifications (at most once a minute)
    void showPopups();
  });
  start(layout.main);
  void showPopups();
  // Texts of the template itself (skip link, landmarks) follow the language.
  window.addEventListener('ewiki:relayout', () => {
    layout = build();
    setMain(layout.main);
    void rerender();
  });
}

let headerObserver: ResizeObserver | null = null;

/**
 * Keeps --ew-sticky-top at the height of a sticky or fixed page header, so that
 * focused controls scrolled into view are not hidden below it (WCAG 2.4.11,
 * via scroll-padding-top) and sticky toolbars sit right under it.
 */
function trackStickyHeader(root: HTMLElement): void {
  headerObserver?.disconnect();
  const header = root.querySelector<HTMLElement>('header');
  const set = () => {
    const pos = header ? getComputedStyle(header).position : '';
    const hgt = header && (pos === 'sticky' || pos === 'fixed') ? Math.ceil(header.getBoundingClientRect().height) : 0;
    document.documentElement.style.setProperty('--ew-sticky-top', `${hgt}px`);
  };
  set();
  if (header && 'ResizeObserver' in window) {
    headerObserver = new ResizeObserver(set);
    headerObserver.observe(header);
  }
}

/**
 * Position preview for administrators: /any/page?positions=1 outlines all
 * positions of the template with their names (until switched off).
 */
function showPositionsMode(): void {
  const q = new URLSearchParams(location.search).get('positions');
  try {
    if (q === '1') sessionStorage.setItem('ewiki.positions', '1');
    if (q === '0') sessionStorage.removeItem('ewiki.positions');
  } catch { /* storage unavailable */ }
  let on = false;
  try { on = sessionStorage.getItem('ewiki.positions') === '1'; } catch { on = q === '1'; }
  if (!on || !state.user?.is_admin) return;
  document.body.classList.add('ew-show-positions');
  const bar = h('div', { class: 'ew-positions-bar', role: 'region', 'aria-label': t('positions.preview') },
    h('span', null, t('positions.preview_help')), ' ',
    h('button', { type: 'button', class: 'ew-btn ew-btn--secondary', onclick: () => {
      try { sessionStorage.removeItem('ewiki.positions'); } catch { /* ignore */ }
      document.body.classList.remove('ew-show-positions');
      bar.remove();
    } }, t('positions.hide')));
  document.body.append(bar);
}

void boot();

/** The live preview of Administration → Templates → Settings: when this
 * window is its frame (same origin, a person who may change the settings),
 * the template of the preview is shown with the draft of its settings, and
 * every change of the draft builds the page again. Only this window sees
 * the draft. */
function setupSettingsPreview(): void {
  if (window.parent === window || !hasPerm('core.settings.manage')) return;
  let parent: SettingsPreviewParent;
  try {
    parent = window.parent as unknown as SettingsPreviewParent;
    if (parent.location.origin !== location.origin) return;
  } catch {
    return;
  }
  const draft = parent.__ewTplSettingsDraft;
  if (!draft || typeof draft.id !== 'string' || !resolveTemplate(draft.id, draft.id)) return;
  setPreviewTemplate(draft.id);
  setTemplateSettingsDraft(draft.id, draft.state);
  (window as unknown as SettingsPreviewWindow).__ewTplSettingsUpdate = (next) => {
    setTemplateSettingsDraft(draft.id, next);
    window.dispatchEvent(new Event('ewiki:relayout'));
  };
}

interface SettingsPreviewParent { location: Location; __ewTplSettingsDraft?: { id: string; state: TemplateSettingsState } }
interface SettingsPreviewWindow { __ewTplSettingsUpdate?: (next: TemplateSettingsState) => void }
