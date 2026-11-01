// theme.cms: website layout with a large header, main navigation built from
// the top level categories (with an off-canvas menu on small screens), full
// width content for layout builder sections and a footer with columns.
import { api, h, pick, registerSlot, registerTemplate, state, t, type Category } from '@ewiki/sdk';
import './style.css';

// Main navigation: home + top level categories (position "cms-nav", so it is
// refreshed after navigation, login and language changes like every position).
async function mainNav(): Promise<Node | null> {
  let cats: Category[] = [];
  try { cats = await api.get<Category[]>('categories?parent=0'); } catch { /* guests without access */ }
  const here = location.pathname;
  return h('ul', { class: 'ewcms-nav-list' },
    h('li', null, h('a', { href: '/', 'aria-current': here === '/' ? 'page' : undefined }, t('nav.home'))),
    ...cats.map((c) => h('li', null, h('a', { href: `/c/${c.id}`, 'aria-current': here === `/c/${c.id}` || here.startsWith(`/c/${c.id}/`) ? 'page' : undefined },
      pick(c.titles, state.lang, state.site.default_language)))));
}

export function register(): void {
  registerSlot({ id: 'theme.cms.nav', position: 'cms-nav', order: 0, render: mainNav });
  registerTemplate({
    id: 'theme.cms',
    className: 'ewcms',
    positions: ['cms-nav', 'header-nav', 'header-tools', 'sidebar', 'footer'],
    layout: (ctx) => {
      const main = h('main', { id: 'main', class: 'ewcms-main', tabindex: '-1' });
      const nav = h('nav', { id: 'ewcms-nav', class: 'ewcms-nav', 'aria-label': t('nav.main') }, ctx.position('cms-nav'), ctx.position('header-nav', 'div'));
      const toggle = h('button', { type: 'button', class: 'ewcms-burger ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': 'ewcms-nav' },
        h('span', { 'aria-hidden': 'true' }, '☰'), h('span', { class: 'sr-only' }, t('nav.menu')));
      const setOpen = (open: boolean) => { toggle.setAttribute('aria-expanded', String(open)); nav.classList.toggle('is-open', open); };
      toggle.addEventListener('click', () => setOpen(toggle.getAttribute('aria-expanded') !== 'true'));
      nav.addEventListener('click', (ev) => { if ((ev.target as Element).closest('a')) setOpen(false); });
      const header = h('header', { class: 'ewcms-header' }, h('div', { class: 'ewcms-wrap ewcms-header-inner' },
        h('a', { href: '/', class: 'ewcms-brand' }, h('span', { class: 'ewcms-logo', 'aria-hidden': 'true' }), h('span', null, state.site.name)),
        toggle, nav));
      // Escape closes the menu (from the button or from inside the menu)
      header.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && nav.classList.contains('is-open')) { setOpen(false); toggle.focus(); } });
      const root = h('div', { class: 'ewcms-app' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        h('div', { class: 'ewcms-topbar' }, h('div', { class: 'ewcms-wrap' }, ctx.position('header-tools'))),
        header,
        h('div', { class: 'ewcms-body' }, main,
          h('aside', { class: 'ewcms-aside', 'aria-label': t('nav.sidebar') }, h('div', { class: 'ewcms-wrap' }, ctx.position('sidebar')))),
        h('footer', { class: 'ewcms-footer' }, h('div', { class: 'ewcms-wrap' }, ctx.position('footer'),
          h('p', { class: 'ewcms-powered' }, t('footer.powered', { version: state.site.version })))));
      return { root, main };
    },
  });
}
