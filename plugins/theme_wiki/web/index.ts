// theme.wiki: documentation layout with header, page tree sidebar,
// content area and footer.
//
// Positions (for menus, blocks and plugin widgets):
//   top-bar          full-width bar above the header (announcements)
//   header-nav       main navigation in the header
//   header-tools     search, language, edit mode, user menu
//   sidebar          sidebar above and with the page tree
//   sidebar-bottom   sidebar below the page tree
//   content-top      above the content of every page
//   content-bottom   below the content of every page
//   footer           footer
import { brandHeader, h, iconURL, MAIN, mediaMatches, poweredBy, registerTemplate, t, type SchemaArea, type SchemaRow, type TemplateSchema } from '@ewiki/coreplugin';
import './style.css';

export const POSITIONS = ['top-bar', 'header-nav', 'header-tools', 'sidebar', 'sidebar-bottom', 'content-top', 'content-bottom', 'footer'];

/** Compact: phones in both orientations and tablets. */
export const COMPACT = '(max-width: 1100px), (max-height: 520px)';

/** Where the positions are (the schematic in Administration → Templates):
 * compact, the main navigation and the sidebar are in the menu and the
 * tools behind their button; wide, the sidebar is a column beside the page. */
export const schema: TemplateSchema = (size) => {
  const compact = mediaMatches(COMPACT, size);
  const top: SchemaRow = { role: 'none', areas: [{ kind: 'area', positions: ['top-bar'] }] };
  const foot: SchemaRow = { role: 'contentinfo', areas: [{ kind: 'area', positions: ['footer'] }] };
  const page: SchemaArea = { kind: 'content', positions: ['content-top', MAIN, 'content-bottom'], grow: 3 };
  if (compact) {
    return [top,
      { role: 'banner', areas: [
        { kind: 'area', positions: ['header-nav', 'sidebar', 'sidebar-bottom'], behind: t('nav.menu') },
        { kind: 'brand', positions: [] },
        { kind: 'area', positions: ['header-tools'], behind: t('nav.tools_toggle') }] },
      { role: 'none', areas: [page] }, foot];
  }
  return [top,
    { role: 'banner', areas: [{ kind: 'brand', positions: [] }, { kind: 'area', positions: ['header-nav'], grow: 3 }, { kind: 'area', positions: ['header-tools'], grow: 2 }] },
    { role: 'none', areas: [{ kind: 'area', positions: ['sidebar', 'sidebar-bottom'] }, page] }, foot];
};

export function register(): void {
  registerTemplate({
    id: 'theme.wiki',
    className: 'ewc',
    positions: POSITIONS,
    schema,
    layout: (ctx) => {
      const main = h('main', { id: 'main', class: 'ewc-main', tabindex: '-1' });
      const sidebar = h('aside', { id: 'ewc-sidebar', class: 'ewc-sidebar', 'aria-label': t('nav.sidebar') }, ctx.position('sidebar'), ctx.position('sidebar-bottom'));
      const nav = h('div', { class: 'ewc-nav' }, ctx.position('header-nav', 'div'));
      const tools = h('div', { id: 'ewc-tools', class: 'ewc-tools' }, ctx.position('header-tools'));
      const toggle = h('button', { type: 'button', class: 'ewc-sidebar-toggle ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': 'ewc-sidebar' },
        h('span', { 'aria-hidden': 'true' }, '☰'), h('span', { class: 'sr-only' }, t('nav.sidebar_toggle')));
      const toolsToggle = h('button', { type: 'button', class: 'ewc-tools-toggle ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': 'ewc-tools' },
        h('span', { class: 'ew-icon', 'aria-hidden': 'true', style: `--ew-icon: url("${iconURL('search')}")` }), h('span', { class: 'sr-only' }, t('nav.tools_toggle')));
      const close = h('button', { type: 'button', class: 'ewc-drawer-close ew-btn ew-btn--subtle' },
        h('span', { 'aria-hidden': 'true' }, '×'), h('span', { class: 'sr-only' }, t('nav.menu_close')));
      // Compact (phones in both orientations, tablets): the page starts with
      // its content. The menu (main navigation and sidebar) and the tools
      // (search, language, edit, account) are closed behind two buttons. The
      // menu opens below the bar across the whole width in portrait and as a
      // drawer of the full height in landscape (CSS). Wide screens: the
      // sidebar can be hidden for more reading width (remembered).
      const compact = () => window.matchMedia?.(COMPACT).matches ?? false;
      let hidden = false;
      try { hidden = localStorage.getItem('ewc-sidebar') === 'hidden'; } catch { /* storage not available */ }
      const menuOpen = () => sidebar.classList.contains('is-open');
      const apply = () => {
        const c = compact();
        root.classList.toggle('ewc-app--compact', c);
        // the main navigation belongs to the menu when compact, to the bar otherwise
        if (c && nav.parentElement !== sidebar) sidebar.prepend(close, nav);
        if (!c && nav.parentElement !== header) { close.remove(); header.insertBefore(nav, tools); }
        if (!c) { sidebar.classList.remove('is-open'); tools.classList.remove('is-open'); }
        toggle.setAttribute('aria-expanded', String(c ? menuOpen() : !hidden));
        toolsToggle.setAttribute('aria-expanded', String(tools.classList.contains('is-open')));
        shell.classList.toggle('ewc-shell--collapsed', !c && hidden);
        document.documentElement.classList.toggle('ewc-menu-open', c && menuOpen());
        // the menu in portrait starts right below the bar
        if (c && menuOpen()) root.style.setProperty('--ewc-bar-h', `${Math.round(header.getBoundingClientRect().bottom)}px`);
      };
      const setMenu = (open: boolean) => {
        sidebar.classList.toggle('is-open', open);
        if (open) tools.classList.remove('is-open');
        apply();
        if (open) (sidebar.querySelector<HTMLElement>('a[href], button:not(.ewc-drawer-close)') ?? close).focus();
      };
      toggle.addEventListener('click', () => {
        if (compact()) {
          setMenu(!menuOpen());
        } else {
          hidden = !hidden;
          try { localStorage.setItem('ewc-sidebar', hidden ? 'hidden' : 'shown'); } catch { /* ignore */ }
          apply();
        }
      });
      toolsToggle.addEventListener('click', () => {
        const open = !tools.classList.contains('is-open');
        tools.classList.toggle('is-open', open);
        if (open) sidebar.classList.remove('is-open');
        apply();
        if (open) tools.querySelector<HTMLElement>('input, select, button, a[href]')?.focus();
      });
      close.addEventListener('click', () => { setMenu(false); toggle.focus(); });
      window.matchMedia?.(COMPACT).addEventListener?.('change', apply);
      // Escape closes what is open; the focus goes back to its button
      const escape = (e: KeyboardEvent) => {
        if (e.key !== 'Escape' || e.defaultPrevented || !compact()) return;
        if (menuOpen()) { setMenu(false); toggle.focus(); } else if (tools.classList.contains('is-open')) { tools.classList.remove('is-open'); apply(); toolsToggle.focus(); }
      };
      for (const el of [sidebar, tools, toggle, toolsToggle]) el.addEventListener('keydown', escape);
      // a click beside the open menu closes it, so does following one of its links
      document.addEventListener('click', (e) => {
        if (!compact() || !menuOpen()) return;
        const target = e.target as Element | null;
        if (target?.closest?.('a[href]') && sidebar.contains(target)) { setMenu(false); return; }
        if (!sidebar.contains(target) && !toggle.contains(target)) setMenu(false);
      });
      const shell = h('div', { class: 'ewc-shell' }, sidebar,
        h('div', { class: 'ewc-content' }, ctx.position('content-top'), main, ctx.position('content-bottom')));
      const header = h('header', { class: 'ewc-header' },
        toggle,
        h('a', { href: '/', class: 'ewc-brand' }, ...brandHeader('ewc-logo', 44)),
        nav, toolsToggle, tools);
      const root = h('div', { class: 'ewc-app' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        ctx.position('top-bar'),
        header,
        shell,
        h('footer', { class: 'ewc-footer' }, ctx.position('footer'),
          poweredBy('ewc-powered')));
      apply();
      return { root, main };
    },
  });
}
