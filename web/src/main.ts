// Application bootstrap.
import './styles/base.css';
import { api } from './core/api';
import { h, mount } from './core/dom';
import { loadMessages, t } from './core/i18n';
import { onNavigate, rerender, route, setMain, setNotFound, start } from './core/router';
import { rememberedLang, state, stored } from './core/state';
import type { Principal, Site } from './core/types';
import { categoryTree, editToggle, languageSwitcher, searchBox, userMenu } from './components/widgets';
import { pluginModules } from './generated/plugins';
import { positionContainer, refreshPositions, registerSlot, registerTemplate, resolveTemplate } from './sdk';
import { adminView } from './views/admin';
import { categorySettingsView } from './views/categorySettings';
import { categoryView, editElementView, elementView, historyView, homeView, newElementView } from './views/content';
import { loginView, notFoundView, profileView, searchView, trashView } from './views/misc';

function pickLanguage(site: Site, user: Principal | null): string {
  const candidates = [new URLSearchParams(location.search).get('lang'), rememberedLang(), user?.language, ...navigator.languages.map((l) => l.split('-')[0]), site.default_language];
  return candidates.find((l) => l && site.languages.includes(l)) ?? site.default_language;
}

function registerCore(): void {
  route('/', homeView);
  route('/c/:id', categoryView);
  route('/c/:id/new', newElementView);
  route('/c/:id/settings', categorySettingsView);
  route('/e/:id', elementView);
  route('/e/:id/edit', editElementView);
  route('/e/:id/history', historyView);
  route('/search', searchView);
  route('/login', loginView);
  route('/profile', profileView);
  route('/trash', trashView);
  route('/admin', adminView);
  route('/admin/:section', adminView);
  setNotFound(notFoundView);

  registerSlot({ id: 'core.search', position: 'header-tools', order: 10, render: searchBox });
  registerSlot({ id: 'core.lang', position: 'header-tools', order: 20, render: languageSwitcher });
  registerSlot({ id: 'core.edit', position: 'header-tools', order: 30, render: editToggle });
  registerSlot({ id: 'core.user', position: 'header-tools', order: 40, render: userMenu });
  registerSlot({ id: 'core.tree', position: 'sidebar', order: 10, render: categoryTree });

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

async function boot(): Promise<void> {
  const app = document.getElementById('app')!;
  try {
    const [site, me] = await Promise.all([api.get<Site>('site'), api.get<{ user: Principal | null }>('auth/me')]);
    state.site = site;
    state.user = me.user;
    state.lang = pickLanguage(site, me.user);
    state.editMode = !!me.user && stored('ewiki.edit') === '1';
    await loadMessages(state.lang);
  } catch (e) {
    mount(app, h('main', { class: 'ew-fatal' }, h('h1', null, 'Enterprise Wiki'), h('p', { role: 'alert' }, `The server is not reachable (${String(e)}). Please try again later.`)));
    return;
  }
  document.documentElement.dataset.site = state.site.name;
  registerCore();
  const active = new Set(state.site.plugins ?? []);
  for (const p of pluginModules) {
    if (!active.has(p.id)) continue;
    try {
      p.mod.register?.();
    } catch (e) {
      console.error(`plugin ${p.id} failed to register`, e);
    }
  }
  const tpl = resolveTemplate(state.site.template) ?? resolveTemplate('core.basic')!;
  document.body.classList.add('ew', ...tpl.classNames);
  const build = () => {
    const layout = tpl.layout({ position: (name, tag) => positionContainer(name, tag) });
    mount(app, layout.root);
    return layout;
  };
  const layout = build();
  showPositionsMode();
  onNavigate(() => void refreshPositions());
  start(layout.main);
  // Texts of the template itself (skip link, landmarks) follow the language.
  window.addEventListener('ewiki:relayout', () => {
    setMain(build().main);
    void rerender();
  });
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
