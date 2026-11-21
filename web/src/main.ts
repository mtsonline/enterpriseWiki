// Application bootstrap.
import { dashboardView, offeredTilesForm, registerDashboardTiles } from './views/dashboard';
import { verifyEmailView } from './views/identity';
import { registerConfirmView, registerView } from './views/register';
import { resetConfirmView, resetView } from './views/passwordReset';
import './styles/base.css';
import './styles/code.css';
import './editor/editor.css';
import { mediaView } from './views/media';
import { signatureVerifyView } from './views/signatures';
import { officeView } from './components/office';
import { api, ApiError, setWaitNotice, setWaitPrompt } from './core/api';
import { errorText, pageHeader, waitDialog } from './components/ui';
import { toast } from './core/announce';
import { h, mount } from './core/dom';
import { loadMessages, t } from './core/i18n';
import { onBeforeRender, onNavigate, rerender, route, setMain, setNotFound, start } from './core/router';
import { rememberedLang, state, stored } from './core/state';
import type { Principal, Site } from './core/types';
import { categoryTree, ctx, currentSpace, setSpace, editToggle, helpToggle, languageSwitcher, searchBox, userMenu } from './components/widgets';
import { watchHelp } from './core/helptexts';
import { pluginModules } from './generated/plugins';
import { positionContainer, refreshPositions, registerAdminPage, registerPageAction, registerProfileSection, registerSlot, registerTemplate, previewTemplate, resolveTemplate, setShownTemplate, siteTemplate, triedTemplate } from './coreplugin';
import { tryBar } from './components/trytemplate';
import { instanceRightsButton } from './components/instanceRights';
import { instanceLabelsButton } from './components/instanceLabels';
import { placementEditor, typeLayouts } from './components/placement';
import { blockOptionsControl } from './components/blockoptions';
import { iconControl } from './components/iconpicker';
import { categoryControl, imageControl, pageControl, templateControl } from './components/macroparams';
import { registerMacroParamControl } from './editor/macros';
import { notificationBell } from './components/bell';
import { adminView } from './views/admin';
import { registerSourceControls } from './components/sources';
import { installMacroPager } from './core/pager';
import { startTooltips } from './components/tooltip';
import { categorySettingsView } from './views/categorySettings';
import { spaceKeyView, spacesView } from './views/spaces';
import { categoryView, editElementView, elementView, draftEditView, historyView, homeView, newElementView, siteHomeView, registerCategoryPageBlocks, registerElementBlocks } from './views/content';
import { loginView, notFoundView, profileView, searchView, trashView } from './views/misc';

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
  route('/dashboard', dashboardView);
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
  registerAdminPage({ id: 'placement', permission: 'core.settings.manage', order: 15, label: () => t('placement.admin'), render: async (panel) => { panel.append(pageHeader(t('placement.admin')), await placementEditor('site', ''), await typeLayouts(), await offeredTilesForm()); } });
  registerProfileSection({ id: 'core.placement', order: 60, render: async () => (state.site.personal_placement && state.user ? placementEditor('user', String(state.user.id)) : null) });
  // building blocks of the core: default positions; the administration,
  // categories and (if allowed) every person can place them elsewhere
  registerSlot({ id: 'core.search', position: 'header-tools', order: 10, label: () => t('block.core.search'), render: searchBox });
  registerSlot({ id: 'core.lang', position: 'header-tools', order: 20, label: () => t('block.core.lang'), render: languageSwitcher });
  registerSlot({ id: 'core.edit', position: 'header-tools', order: 30, label: () => t('block.core.edit'), render: editToggle });
  registerSlot({ id: 'core.help', position: 'header-tools', order: 25, label: () => t('block.core.help'), render: helpToggle });
  registerSlot({ id: 'core.bell', position: 'header-tools', order: 35, label: () => t('block.core.bell'), render: () => notificationBell() });
  registerSlot({ id: 'core.user', position: 'header-tools', order: 40, always: true, label: () => t('block.core.user'), render: userMenu });
  registerSlot({ id: 'core.tree', position: 'sidebar', order: 10, label: () => t('block.core.tree'), render: categoryTree });

  // Minimal fallback layout, used when no template plugin is active.
  registerTemplate({
    id: 'core.basic',
    positions: ['header-tools', 'sidebar', 'footer'],
    layout: (ctx) => {
      const main = h('main', { id: 'main', tabindex: '-1' });
      const root = h('div', { class: 'ew-basic' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        h('header', { class: 'ew-basic-header' }, h('a', { href: '/', class: 'ew-brand' }, state.site.name), ctx.position('header-tools')),
        h('div', { class: 'ew-basic-body' }, h('aside', { 'aria-label': t('nav.sidebar') }, ctx.position('sidebar')), main),
        h('footer', null, ctx.position('footer')));
      return { root, main };
    },
  });
}

// A failed request that no code handled is shown instead of failing silently.
// It is not suppressed: the browser tests still count it as an error.
window.addEventListener('unhandledrejection', (ev) => {
  if (ev.reason instanceof ApiError) toast(errorText(ev.reason), 'error');
});

async function boot(): Promise<void> {
  installMacroPager();
  startTooltips();
  const app = document.getElementById('app')!;
  try {
    const [site, me] = await Promise.all([api.get<Site>('site'), api.get<{ user: Principal | null; manages_categories?: boolean }>('auth/me')]);
    state.site = site;
    setWaitNotice(site.wait_notice_s);
    setWaitPrompt(waitDialog);
    state.user = me.user;
    state.managesCategories = !!me.manages_categories;
    state.lang = pickLanguage(site, me.user);
    state.editMode = !!me.user && stored('ewiki.edit') === '1';
    await loadMessages(state.lang);
  } catch (e) {
    mount(app, h('main', { class: 'ew-fatal' }, h('h1', null, 'Enterprise Wiki'), h('p', { role: 'alert' }, `The server is not reachable (${String(e)}). Please try again later.`)));
    return;
  }
  document.documentElement.dataset.site = state.site.name;
  // explanations are shown or hidden for the person, on every page
  watchHelp();
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
  let tpl = resolveTemplate(previewTemplate() || siteTemplate()) ?? resolveTemplate('core.basic')!;
  setShownTemplate(tpl.id);
  document.body.classList.add('ew', ...tpl.classNames);
  const build = () => {
    const layout = tpl.layout({ position: (name, tag) => positionContainer(name, tag) });
    mount(app, layout.root);
    trackStickyHeader(layout.root);
    return layout;
  };
  let layout = build();
  showPositionsMode();
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
  });
  start(layout.main);
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
