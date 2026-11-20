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
import { h, registerTemplate, state, t } from '@ewiki/coreplugin';
import './style.css';

export const POSITIONS = ['top-bar', 'header-nav', 'header-tools', 'sidebar', 'sidebar-bottom', 'content-top', 'content-bottom', 'footer'];

export function register(): void {
  registerTemplate({
    id: 'theme.wiki',
    className: 'ewc',
    positions: POSITIONS,
    layout: (ctx) => {
      const main = h('main', { id: 'main', class: 'ewc-main', tabindex: '-1' });
      const sidebar = h('aside', { id: 'ewc-sidebar', class: 'ewc-sidebar', 'aria-label': t('nav.sidebar') }, ctx.position('sidebar'), ctx.position('sidebar-bottom'));
      const toggle = h('button', { type: 'button', class: 'ewc-sidebar-toggle ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': 'ewc-sidebar ewc-header-panel' },
        h('span', { 'aria-hidden': 'true' }, '☰'), h('span', { class: 'sr-only' }, t('nav.sidebar_toggle')));
      // small screens: one menu button opens the navigation, the tools
      // (search, language, edit, user) and the sidebar together (closed at
      // first, so the page starts with its content); large screens: the
      // sidebar can be hidden for more reading width (remembered)
      const small = () => window.matchMedia?.('(max-width: 900px)').matches ?? false;
      let hidden = false;
      try { hidden = localStorage.getItem('ewc-sidebar') === 'hidden'; } catch { /* storage not available */ }
      const apply = () => {
        const open = small() ? sidebar.classList.contains('is-open') : !hidden;
        toggle.setAttribute('aria-expanded', String(open));
        panel.classList.toggle('is-open', small() && open);
        shell.classList.toggle('ewc-shell--collapsed', !small() && hidden);
      };
      toggle.addEventListener('click', () => {
        if (small()) {
          sidebar.classList.toggle('is-open');
        } else {
          hidden = !hidden;
          try { localStorage.setItem('ewc-sidebar', hidden ? 'hidden' : 'shown'); } catch { /* ignore */ }
        }
        apply();
      });
      window.matchMedia?.('(max-width: 900px)').addEventListener?.('change', apply);
      const panel = h('div', { id: 'ewc-header-panel', class: 'ewc-header-panel' }, ctx.position('header-nav', 'div'), ctx.position('header-tools'));
      // Escape closes the open menu on small screens; the focus goes back to the button
      const escape = (e: KeyboardEvent) => {
        if (e.key !== 'Escape' || e.defaultPrevented || !small() || !sidebar.classList.contains('is-open')) return;
        sidebar.classList.remove('is-open');
        apply();
        toggle.focus();
      };
      for (const el of [panel, sidebar, toggle]) el.addEventListener('keydown', escape);
      // following a link of the menu closes it (the new page starts with its content)
      const closeOnLink = (e: Event) => {
        if (small() && (e.target as Element | null)?.closest?.('a[href]')) { sidebar.classList.remove('is-open'); apply(); }
      };
      panel.addEventListener('click', closeOnLink);
      sidebar.addEventListener('click', closeOnLink);
      const shell = h('div', { class: 'ewc-shell' }, sidebar,
        h('div', { class: 'ewc-content' }, ctx.position('content-top'), main, ctx.position('content-bottom')));
      apply();
      const root = h('div', { class: 'ewc-app' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        ctx.position('top-bar'),
        h('header', { class: 'ewc-header' },
          toggle,
          h('a', { href: '/', class: 'ewc-brand' },
            h('span', { class: 'ewc-logo', 'aria-hidden': 'true' }),
            h('span', null, state.site.name)),
          panel),
        shell,
        h('footer', { class: 'ewc-footer' }, ctx.position('footer'),
          h('p', { class: 'ewc-powered' }, t('footer.powered', { version: state.site.version }))));
      return { root, main };
    },
  });
}
