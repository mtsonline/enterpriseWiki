// Mega menu style for core.menu: top entries are disclosure buttons that open
// a wide panel; the entries below are shown in columns (with headings and
// descriptions). One panel is open at a time; Escape closes it and returns
// focus, leaving the menu with Tab or a click outside closes it; on devices
// with a mouse panels open on hover after a short delay.
import { h, pick, registerMenuStyle, state, t, uid, type MenuItem } from '@ewiki/sdk';
import './style.css';

const desc = (it: MenuItem) => (it.description ? pick(it.description, state.lang, state.site.default_language) : '');

function render(menu: { id: number; name: string; items: MenuItem[] }, x: { label: (it: MenuItem) => string; link: (it: MenuItem) => HTMLElement }): HTMLElement {
  const bar = h('ul', { class: 'ewmm-bar' });
  const closers: (() => void)[] = [];
  const closeAll = (except?: () => void) => { for (const c of closers) if (c !== except) c(); };
  for (const top of menu.items) {
    const kids = top.children ?? [];
    const li = h('li', { class: 'ewmm-top' });
    if (!kids.length) {
      li.append(top.href ? x.link(top) : h('span', { class: 'ewmm-label' }, x.label(top)));
      bar.append(li);
      continue;
    }
    const id = uid('ewmm');
    const btn = h('button', { type: 'button', class: 'ewmm-btn', 'aria-expanded': 'false', 'aria-controls': id },
      x.label(top), h('span', { class: 'ewmm-caret', 'aria-hidden': 'true' })) as HTMLButtonElement;
    const cols = h('div', { class: 'ewmm-cols' }, ...kids.map((col) => {
      const sub = col.children ?? [];
      return h('section', { class: 'ewmm-col' },
        h('h3', { class: 'ewmm-col-title' }, col.href ? x.link(col) : x.label(col)),
        desc(col) ? h('p', { class: 'ewmm-desc' }, desc(col)) : null,
        sub.length ? h('ul', { class: 'ewmm-links' }, ...sub.map((it) => h('li', null,
          it.href ? x.link(it) : h('span', { class: 'ewmm-label' }, x.label(it)),
          desc(it) ? h('span', { class: 'ewmm-desc' }, desc(it)) : null))) : null);
    }));
    const panel = h('div', { class: 'ewmm-panel', id, hidden: true },
      top.href || desc(top) ? h('div', { class: 'ewmm-intro' },
        top.href ? h('a', { href: top.href, class: 'ewmm-overview' }, t('core.megamenu.overview', { title: x.label(top) })) : null,
        desc(top) ? h('p', { class: 'ewmm-desc' }, desc(top)) : null) : null,
      cols);
    const set = (open: boolean) => {
      panel.hidden = !open;
      btn.setAttribute('aria-expanded', String(open));
      li.classList.toggle('is-open', open);
      if (open) closeAll(close);
    };
    const close = () => set(false);
    closers.push(close);
    btn.addEventListener('click', () => set(panel.hidden === true));
    li.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape' && !panel.hidden) { ev.stopPropagation(); set(false); btn.focus(); }
    });
    li.addEventListener('focusout', (ev) => { if (!li.contains(ev.relatedTarget as Node)) set(false); });
    if (window.matchMedia?.('(hover: hover)').matches) {
      let timer = 0;
      li.addEventListener('mouseenter', () => { window.clearTimeout(timer); timer = window.setTimeout(() => set(true), 150); });
      li.addEventListener('mouseleave', () => { window.clearTimeout(timer); timer = window.setTimeout(() => set(false), 300); });
    }
    li.append(btn, panel);
    bar.append(li);
  }
  document.addEventListener('click', (ev) => { if (!bar.contains(ev.target as Node)) closeAll(); });
  return h('div', { class: 'ewmm' }, bar);
}

export function register(): void {
  registerMenuStyle({ id: 'mega', label: () => t('core.megamenu.style'), itemFields: ['description'], render });
}
