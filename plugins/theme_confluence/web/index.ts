// theme.confluence: documentation layout with header, page tree sidebar,
// content area and footer. Positions: header-nav, header-tools, sidebar, footer.
import { h, registerTemplate, state, t } from '@ewiki/sdk';
import './style.css';

export function register(): void {
  registerTemplate({
    id: 'theme.confluence',
    className: 'ewc',
    positions: ['header-nav', 'header-tools', 'sidebar', 'footer'],
    layout: (ctx) => {
      const main = h('main', { id: 'main', class: 'ewc-main', tabindex: '-1' });
      const sidebar = h('aside', { id: 'ewc-sidebar', class: 'ewc-sidebar', 'aria-label': t('nav.sidebar') }, ctx.position('sidebar'));
      const toggle = h('button', { type: 'button', class: 'ewc-sidebar-toggle ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': 'ewc-sidebar' },
        h('span', { 'aria-hidden': 'true' }, '☰'), h('span', { class: 'sr-only' }, t('nav.sidebar_toggle')));
      toggle.addEventListener('click', () => {
        const open = toggle.getAttribute('aria-expanded') !== 'true';
        toggle.setAttribute('aria-expanded', String(open));
        sidebar.classList.toggle('is-open', open);
      });
      const root = h('div', { class: 'ewc-app' },
        h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip')),
        h('header', { class: 'ewc-header' },
          toggle,
          h('a', { href: '/', class: 'ewc-brand' },
            h('span', { class: 'ewc-logo', 'aria-hidden': 'true' }),
            h('span', null, state.site.name)),
          ctx.position('header-nav', 'div'),
          ctx.position('header-tools')),
        h('div', { class: 'ewc-shell' }, sidebar, main),
        h('footer', { class: 'ewc-footer' }, ctx.position('footer'),
          h('p', { class: 'ewc-powered' }, t('footer.powered', { version: state.site.version }))));
      return { root, main };
    },
  });
}
