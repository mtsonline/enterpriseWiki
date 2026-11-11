// theme.cms: website layout in the style of classic business/university
// themes: thin dark top bar, large header with logo and main navigation
// (transparent over a hero image on the start page, solid and smaller when
// scrolling), full-width hero, page title banners, light page background
// with white content area, dark footer with columns and a socket bar.
// Logo, colours and the start page hero are set under Administration →
// Website template (plugin settings, public via GET /api/v1/x/theme.cms/config).
import {
  api, h, mount, pick, pluginSettingsForm, registerAdminPage, registerSlot, registerTemplate, state, t, type Category, type SettingField,
} from '@ewiki/sdk';
import './style.css';

interface Config {
  logo?: string; logo_light?: string; logo_height?: number; primary_color?: string; accent_color?: string; header?: 'overlay' | 'solid';
  hero_image?: string; hero_title?: string; hero_subtitle?: string; hero_button_label?: string; hero_button_link?: string; hero_size?: 'medium' | 'large';
  banner?: boolean; banner_image?: string; copyright?: string; back_to_top?: boolean;
}

const BASE = '/api/v1/x/theme.cms';
let config: Promise<Config> | null = null;
const getConfig = (): Promise<Config> => (config ??= api.get<Config>(`${BASE}/config`).catch(() => ({})));
const img = (hash?: string) => (hash ? `/api/v1/files/${hash}` : '');
const isHome = () => location.pathname === '/';
const safeLink = (u?: string) => (u && (/^https?:\/\//i.test(u) || (u.startsWith('/') && !u.startsWith('//'))) ? u : '');

// Main navigation: home + top level categories (position "cms-nav", so it is
// refreshed after navigation, login and language changes like every position).
async function mainNav(): Promise<Node | null> {
  let cats: Category[] = [];
  try { cats = await api.get<Category[]>('categories?parent=0'); } catch { /* guests without access */ }
  const here = location.pathname;
  const href = (c: Category) => (c.short_url ? `/${c.short_url}` : `/c/${c.id}`);
  return h('ul', { class: 'ewcms-nav-list' },
    h('li', null, h('a', { href: '/', 'aria-current': here === '/' ? 'page' : undefined }, t('nav.home'))),
    ...cats.map((c) => h('li', null, h('a', { href: href(c), 'aria-current': here === href(c) || here.startsWith(`/c/${c.id}/`) ? 'page' : undefined },
      pick(c.titles, state.lang, state.site.default_language)))));
}

// Start page hero from the template settings.
async function hero(): Promise<Node | null> {
  if (!isHome()) return null;
  const c = await getConfig();
  if (!c.hero_image && !c.hero_title) return null;
  const link = safeLink(c.hero_button_link);
  return h('section', { class: ['ewcms-hero', `ewcms-hero--${c.hero_size === 'medium' ? 'medium' : 'large'}`, !c.hero_image && 'ewcms-hero--plain'],
    style: c.hero_image ? `--ewcms-hero-img:url("${img(c.hero_image)}")` : undefined, 'aria-label': c.hero_title || state.site.name },
  h('div', { class: 'ewcms-wrap ewcms-hero-inner' },
    c.hero_title ? h('p', { class: 'ewcms-hero-title' }, c.hero_title) : null,
    c.hero_subtitle ? h('p', { class: 'ewcms-hero-subtitle' }, c.hero_subtitle) : null,
    c.hero_button_label && link ? h('a', { href: link, class: 'ewcms-hero-button' }, c.hero_button_label) : null));
}

function settingsFields(): SettingField[] {
  return [
    { key: 'logo', label: t('cms.logo'), type: 'image', help: t('cms.logo_help') },
    { key: 'logo_light', label: t('cms.logo_light'), type: 'image', help: t('cms.logo_light_help') },
    { key: 'logo_height', label: t('cms.logo_height'), type: 'number', help: t('cms.logo_height_help') },
    { key: 'primary_color', label: t('cms.primary_color'), type: 'color', default: '#0a3760', help: t('cms.primary_color_help') },
    { key: 'header', label: t('cms.header'), type: 'select', options: [{ value: 'overlay', label: t('cms.header_overlay') }, { value: 'solid', label: t('cms.header_solid') }] },
    { key: 'hero_image', label: t('cms.hero_image'), type: 'image', help: t('cms.hero_image_help') },
    { key: 'hero_title', label: t('cms.hero_title') },
    { key: 'hero_subtitle', label: t('cms.hero_subtitle') },
    { key: 'hero_button_label', label: t('cms.hero_button_label') },
    { key: 'hero_button_link', label: t('cms.hero_button_link'), placeholder: '/studium, https://…' },
    { key: 'hero_size', label: t('cms.hero_size'), type: 'select', options: [{ value: 'large', label: t('cms.hero_large') }, { value: 'medium', label: t('cms.hero_medium') }] },
    { key: 'banner', label: t('cms.banner'), type: 'bool', help: t('cms.banner_help') },
    { key: 'banner_image', label: t('cms.banner_image'), type: 'image', help: t('cms.banner_image_help'), when: (v) => v.banner === true },
    { key: 'copyright', label: t('cms.copyright'), placeholder: '© Organisation', help: t('cms.copyright_help') },
    { key: 'back_to_top', label: t('cms.back_to_top'), type: 'bool' },
  ];
}

export function register(): void {
  registerSlot({ id: 'theme.cms.nav', position: 'cms-nav', order: 0, render: mainNav });
  registerSlot({ id: 'theme.cms.hero', position: 'hero', order: 0, render: hero });
  registerAdminPage({
    permission: 'core.settings.manage',
    id: 'website', label: () => t('cms.admin_title'), order: 30,
    render: async (el) => {
      const f = await pluginSettingsForm('theme.cms', settingsFields(), t('cms.admin_title'));
      el.replaceChildren(h('h1', null, t('cms.admin_title')), h('p', null, t('cms.admin_help')), f.el);
      f.el.addEventListener('submit', () => { config = null; setTimeout(() => location.reload(), 900); });
    },
  });
  registerTemplate({
    id: 'theme.cms',
    className: 'ewcms',
    // top-bar / header-tools: left and right of the dark bar above the header
    // cms-nav: automatic main navigation (top categories), header-nav: menus next to it
    // hero: full width below the header; content-top / content-bottom: around the page
    // sidebar: area below the content; footer-1 … footer-4: footer columns; footer: socket bar
    positions: ['top-bar', 'header-tools', 'cms-nav', 'header-nav', 'hero', 'content-top', 'content-bottom', 'sidebar', 'footer-1', 'footer-2', 'footer-3', 'footer-4', 'footer'],
    layout: (ctx) => {
      const main = h('main', { id: 'main', class: 'ewcms-main', tabindex: '-1' });
      const nav = h('nav', { id: 'ewcms-nav', class: 'ewcms-nav', 'aria-label': t('nav.main') }, ctx.position('cms-nav'), ctx.position('header-nav', 'div'));
      const toggle = h('button', { type: 'button', class: 'ewcms-burger', 'aria-expanded': 'false', 'aria-controls': 'ewcms-nav' },
        h('span', { class: 'ewcms-burger-lines', 'aria-hidden': 'true' }), h('span', { class: 'sr-only' }, t('nav.menu')));
      const setOpen = (open: boolean) => {
        toggle.setAttribute('aria-expanded', String(open));
        nav.classList.toggle('is-open', open);
        document.documentElement.classList.toggle('ewcms-nav-open', open);
      };
      toggle.addEventListener('click', () => setOpen(toggle.getAttribute('aria-expanded') !== 'true'));
      nav.addEventListener('click', (ev) => { if ((ev.target as Element).closest('a')) setOpen(false); });
      const brand = h('a', { href: '/', class: 'ewcms-brand' }, h('span', { class: 'ewcms-logo', 'aria-hidden': 'true' }), h('span', { class: 'ewcms-brand-name' }, state.site.name));
      const header = h('div', { class: 'ewcms-header' }, h('div', { class: 'ewcms-wrap ewcms-header-inner' }, brand, toggle, nav));
      // Escape closes the menu (from the button or from inside the menu)
      header.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && nav.classList.contains('is-open')) { setOpen(false); toggle.focus(); } });
      const heroPos = ctx.position('hero');
      // one banner landmark for the top bar (tools, language) and the header
      const head = h('header', { class: 'ewcms-head' },
        h('div', { class: 'ewcms-topbar' }, h('div', { class: 'ewcms-wrap ewcms-topbar-inner' }, ctx.position('top-bar'), ctx.position('header-tools'))),
        header);
      const year = new Date().getFullYear();
      const copyright = h('p', { class: 'ewcms-copyright' }, `© ${year} ${state.site.name}`);
      const top = h('a', { href: '#top', class: 'ewcms-totop', hidden: true }, h('span', { 'aria-hidden': 'true' }, '↑'), h('span', { class: 'sr-only' }, t('cms.back_to_top')));
      top.addEventListener('click', (ev) => { ev.preventDefault(); window.scrollTo({ top: 0, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' }); });
      const root = h('div', { class: 'ewcms-app', id: 'top' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        head,
        heroPos,
        h('div', { class: 'ewcms-body' },
          h('div', { class: 'ewcms-wrap' }, ctx.position('content-top')),
          main,
          h('div', { class: 'ewcms-wrap' }, ctx.position('content-bottom')),
          h('aside', { class: 'ewcms-aside', 'aria-label': t('nav.sidebar') }, h('div', { class: 'ewcms-wrap' }, ctx.position('sidebar')))),
        h('footer', { class: 'ewcms-footer' },
          h('div', { class: 'ewcms-wrap ewcms-footer-cols' }, ctx.position('footer-1'), ctx.position('footer-2'), ctx.position('footer-3'), ctx.position('footer-4')),
          h('div', { class: 'ewcms-socket' }, h('div', { class: 'ewcms-wrap ewcms-socket-inner' },
            copyright, ctx.position('footer'),
            h('p', { class: 'ewcms-powered' }, t('footer.powered', { version: state.site.version })))),
        ),
        top);

      // settings: colours, logo, banner, copyright
      void getConfig().then((c) => {
        if (c.primary_color) {
          root.style.setProperty('--ew-primary', c.primary_color);
          root.style.setProperty('--ew-primary-hover', `color-mix(in srgb, ${c.primary_color} 85%, #000)`);
          root.style.setProperty('--ew-link', c.primary_color);
        }
        if (c.logo) {
          const height = Math.min(Math.max(Number(c.logo_height) || 56, 24), 120);
          mount(brand,
            h('img', { class: 'ewcms-logo-img', src: img(c.logo), alt: state.site.name, style: `--ewcms-logo-h:${height}px` }),
            c.logo_light ? h('img', { class: 'ewcms-logo-img ewcms-logo-img--light', src: img(c.logo_light), alt: '', 'aria-hidden': 'true', style: `--ewcms-logo-h:${height}px` }) : null);
          root.classList.toggle('ewcms--light-logo', !!c.logo_light);
        }
        if (c.copyright) copyright.textContent = c.copyright.replace('{year}', String(year));
        root.classList.toggle('ewcms--solid', c.header === 'solid');
        if (c.banner) {
          root.classList.add('ewcms--banner');
          const b = c.banner_image || c.hero_image;
          if (b) root.style.setProperty('--ewcms-banner-img', `url("${img(b)}")`);
        }
        top.dataset.enabled = c.back_to_top === false ? '0' : '1';
      });

      // the area below the content is only shown when it has more than the wiki page tree
      const aside = root.querySelector<HTMLElement>('.ewcms-aside')!;
      const sidebarPos = aside.querySelector<HTMLElement>('.ew-pos--sidebar')!;
      const syncAside = () => {
        aside.hidden = !Array.from(sidebarPos.children).some((c) => !c.querySelector('.ew-tree') && !c.classList.contains('ew-tree'));
      };
      new MutationObserver(syncAside).observe(sidebarPos, { childList: true, subtree: true });
      syncAside();
      // header over the hero image while at the top, solid and smaller when scrolled
      const syncHero = () => root.classList.toggle('ewcms--has-hero', heroPos.childElementCount > 0);
      new MutationObserver(syncHero).observe(heroPos, { childList: true });
      syncHero();
      let ticking = false;
      const onScroll = () => {
        if (ticking) return;
        ticking = true;
        requestAnimationFrame(() => {
          ticking = false;
          const y = window.scrollY;
          root.classList.toggle('is-scrolled', y > 40);
          top.hidden = !(y > 600 && top.dataset.enabled !== '0');
        });
      };
      window.addEventListener('scroll', onScroll, { passive: true });
      onScroll();
      return { root, main };
    },
  });
}
