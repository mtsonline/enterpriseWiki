// theme.cms: website layout in the style of classic business/university
// themes: thin dark top bar, large header with logo and main navigation
// (transparent over a hero image on the start page, solid and smaller when
// scrolling), full-width hero, page title banners, light page background
// with white content area, dark footer with columns and a socket bar.
// Logo, colours and the start page hero are the settings of the template
// (declared in plugin.json, "template_settings"; Administration → Templates
// → Settings), checked by the server and handed to the layout.
import {
  api, brandHeader, h, iconURL, imageSetting, MAIN, mediaMatches, mount, pick, poweredBy, registerSlot, registerTemplate, shownTemplateSettings, state, t,
  type Category, type ImageSetting, type SchemaArea, type TemplateSchema, type TemplateSettingValues,
} from '@ewiki/coreplugin';
import './style.css';

/** The settings of the template, typed (the server checked them against plugin.json). */
interface Config {
  logo: ImageSetting | null; logo_light: ImageSetting | null; mark: ImageSetting | null; mark_on_dark: ImageSetting | null; header: string;
  hero_image: ImageSetting | null; hero_title: string; hero_subtitle: string; hero_button_label: string; hero_button_link: string; hero_size: string;
  banner: boolean; banner_image: ImageSetting | null; copyright: string; back_to_top: boolean;
}

function configOf(v: TemplateSettingValues): Config {
  const str = (k: string) => (typeof v[k] === 'string' ? v[k] as string : '');
  return {
    logo: imageSetting(v.logo), logo_light: imageSetting(v.logo_light), mark: imageSetting(v.mark), mark_on_dark: imageSetting(v.mark_on_dark), header: str('header'),
    hero_image: imageSetting(v.hero_image), hero_title: str('hero_title'), hero_subtitle: str('hero_subtitle'), hero_button_label: str('hero_button_label'),
    hero_button_link: str('hero_button_link'), hero_size: str('hero_size'), banner: v.banner === true, banner_image: imageSetting(v.banner_image),
    copyright: str('copyright'), back_to_top: v.back_to_top !== false,
  };
}

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
function hero(): Node | null {
  if (!isHome()) return null;
  const c = configOf(shownTemplateSettings());
  if (!c.hero_image && !c.hero_title) return null;
  const link = safeLink(c.hero_button_link);
  return h('section', { class: ['ewcms-hero', `ewcms-hero--${c.hero_size === 'medium' ? 'medium' : 'large'}`, !c.hero_image && 'ewcms-hero--plain'],
    style: c.hero_image ? `--ewcms-hero-img:url("${c.hero_image.src}")` : undefined, 'aria-label': c.hero_title || state.site.name },
  h('div', { class: 'ewcms-wrap ewcms-hero-inner' },
    c.hero_title ? h('p', { class: 'ewcms-hero-title' }, c.hero_title) : null,
    c.hero_subtitle ? h('p', { class: 'ewcms-hero-subtitle' }, c.hero_subtitle) : null,
    c.hero_button_label && link ? h('a', { href: link, class: 'ewcms-hero-button' }, c.hero_button_label) : null));
}

/** Compact: the navigation and the tools behind their buttons (style.css). */
const COMPACT = '(max-width: 1100px), (max-height: 520px)';

/** How many footer columns fit: 14rem each with 2rem between, in the width
 * of the page (78rem less 1.5rem padding on each side), as the CSS grid does. */
function footerColumns(width: number): number {
  const inner = Math.min(width, 78 * 16) - 2 * 24;
  return Math.max(1, Math.min(4, Math.floor((inner + 32) / (14 * 16 + 32))));
}

/** Where the positions are (the schematic in Administration → Templates). */
export const schema: TemplateSchema = (size) => {
  const compact = mediaMatches(COMPACT, size);
  const tools: SchemaArea = { kind: 'area', positions: ['header-tools'], behind: compact ? t('nav.tools_toggle') : undefined };
  const nav: SchemaArea = { kind: 'area', positions: ['cms-nav', 'header-nav'], grow: 3, behind: compact ? t('nav.menu') : undefined };
  return [
    { role: 'banner', areas: compact ? [{ kind: 'area', positions: ['top-bar'] }] : [{ kind: 'area', positions: ['top-bar'], grow: 3 }, tools] },
    { role: 'banner', areas: compact ? [{ kind: 'brand', positions: [] }, tools, nav] : [{ kind: 'brand', positions: [] }, nav] },
    { role: 'none', areas: [{ kind: 'area', positions: ['hero'] }] },
    { role: 'none', areas: [{ kind: 'content', positions: ['content-top', MAIN, 'content-bottom'] }] },
    { role: 'none', areas: [{ kind: 'area', positions: ['sidebar'] }] },
    { role: 'contentinfo', columns: footerColumns(size.width), areas: ['footer-1', 'footer-2', 'footer-3', 'footer-4'].map((p) => ({ kind: 'area' as const, positions: [p] })) },
    { role: 'contentinfo', areas: [{ kind: 'area', positions: ['footer'] }] },
  ];
};

export function register(): void {
  registerSlot({ id: 'theme.cms.nav', position: 'cms-nav', order: 0, label: () => t('theme.cms.block_nav'), render: mainNav });
  registerSlot({ id: 'theme.cms.hero', position: 'hero', order: 0, label: () => t('theme.cms.block_hero'), render: hero });
  registerTemplate({
    id: 'theme.cms',
    className: 'ewcms',
    // top-bar / header-tools: left and right of the dark bar above the header
    // cms-nav: automatic main navigation (top categories), header-nav: menus next to it
    // hero: full width below the header; content-top / content-bottom: around the page
    // sidebar: area below the content; footer-1 … footer-4: footer columns; footer: socket bar
    schema,
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
        // portrait: the menu starts right below the header
        if (open) document.documentElement.style.setProperty('--ewcms-head-h', `${Math.round(header.getBoundingClientRect().bottom)}px`);
        // one thing open at a time: the menu closes the tools
        if (open) { tools.classList.remove('is-open'); toolsToggle.setAttribute('aria-expanded', 'false'); }
      };
      toggle.addEventListener('click', () => setOpen(toggle.getAttribute('aria-expanded') !== 'true'));
      nav.addEventListener('click', (ev) => { if ((ev.target as Element).closest('a')) setOpen(false); });
      const c = configOf(ctx.settings);
      // without an own logo: the logo of Enterprise Wiki (with the marks of the settings)
      const brand = h('a', { href: '/', class: 'ewcms-brand' }, ...brandHeader('ewcms-logo', 60, { mark: c.mark, markOnDark: c.mark_on_dark }));
      // compact screens (CSS: the same query as the wiki template): search,
      // language, edit and account stay closed behind one button, so the
      // page starts with its content
      const tools = h('div', { id: 'ewcms-tools', class: 'ewcms-tools' }, ctx.position('header-tools'));
      const toolsToggle = h('button', { type: 'button', class: 'ewcms-tools-toggle', 'aria-expanded': 'false', 'aria-controls': 'ewcms-tools' },
        h('span', { class: 'ew-icon', 'aria-hidden': 'true', style: `--ew-icon: url("${iconURL('search')}")` }), h('span', { class: 'sr-only' }, t('nav.tools_toggle')));
      const setTools = (open: boolean) => {
        tools.classList.toggle('is-open', open);
        toolsToggle.setAttribute('aria-expanded', String(open));
        if (open) { setOpen(false); tools.querySelector<HTMLElement>('input, select, button, a[href]')?.focus(); }
      };
      toolsToggle.addEventListener('click', () => setTools(!tools.classList.contains('is-open')));
      tools.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && tools.classList.contains('is-open')) { setTools(false); toolsToggle.focus(); } });
      const header = h('div', { class: 'ewcms-header' }, h('div', { class: 'ewcms-wrap ewcms-header-inner' }, brand, toolsToggle, toggle, nav));
      // Escape closes the menu (from the button or from inside the menu)
      header.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && nav.classList.contains('is-open')) { setOpen(false); toggle.focus(); } });
      const heroPos = ctx.position('hero');
      // one banner landmark for the top bar (tools, language) and the header
      const head = h('header', { class: 'ewcms-head' },
        h('div', { class: 'ewcms-topbar' }, h('div', { class: 'ewcms-wrap ewcms-topbar-inner' }, ctx.position('top-bar'), tools)),
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
            poweredBy('ewcms-powered'))),
        ),
        top);

      // settings: logo, banner, copyright (the colours, fonts and the height
      // of the logo are CSS variables the core sets from the settings)
      if (c.logo) {
        // the light logo is shown over the start image and in the dark scheme
        // (style.css); on narrow screens the marks, if set. Width and height
        // reserve the room of each image before it loads.
        const logoImg = (x: ImageSetting, mark: ImageSetting | null, light: boolean) => {
          const img = h('img', { class: ['ewcms-logo-img', light && 'ewcms-logo-img--light'], src: x.src, width: x.width, height: x.height,
            alt: light ? '' : state.site.name, 'aria-hidden': light ? 'true' : undefined });
          return mark ? h('picture', null, h('source', { media: '(max-width: 600px)', srcset: mark.src, width: mark.width, height: mark.height }), img) : img;
        };
        mount(brand, logoImg(c.logo, c.mark, false), c.logo_light ? logoImg(c.logo_light, c.mark_on_dark ?? c.mark, true) : null);
        root.classList.toggle('ewcms--light-logo', !!c.logo_light);
      }
      if (c.copyright) copyright.textContent = c.copyright.replace('{year}', String(year));
      root.classList.toggle('ewcms--solid', c.header === 'solid');
      if (c.banner) {
        root.classList.add('ewcms--banner');
        const b = c.banner_image ?? c.hero_image;
        if (b) root.style.setProperty('--ewcms-banner-img', `url("${b.src}")`);
      }
      top.dataset.enabled = c.back_to_top ? '1' : '0';

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
