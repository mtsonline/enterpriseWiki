// core.layout: the styles of the layout blocks, and the tabs: the server
// renders one section per part (readable without script); here they become
// a tablist after the WAI-ARIA pattern (arrow keys, Home, End).
import { h, registerWidget, t, uid } from '@ewiki/coreplugin';
import './style.css';

const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** A counter counts up once when it comes into view (1.2 s, never with
 * reduced motion). Screen readers keep the whole number: only a copy hidden
 * from them moves. */
function counterWidget(root: HTMLElement): void {
  const num = root.querySelector<HTMLElement>('.ewl-counter-num');
  const to = Number(root.dataset.to);
  const dec = Number(root.dataset.decimals ?? '0');
  if (!num || !Number.isFinite(to) || reducedMotion() || !('IntersectionObserver' in window)) return;
  const final = num.textContent ?? '';
  const lang = document.documentElement.lang || 'en';
  const fmt = new Intl.NumberFormat(lang, { minimumFractionDigits: dec, maximumFractionDigits: dec });
  const value = root.querySelector<HTMLElement>('.ewl-counter-value')!;
  const shown = h('span', { class: 'ewl-counter-shown', 'aria-hidden': 'true' }, value.textContent ?? '');
  const sr = h('span', { class: 'sr-only' }, value.textContent ?? '');
  const io = new IntersectionObserver((entries) => {
    if (!entries.some((e) => e.isIntersecting)) return;
    io.disconnect();
    const pre = root.querySelector('.ewl-counter-pre')?.textContent ?? '';
    const suf = root.querySelector('.ewl-counter-suf')?.textContent ?? '';
    value.replaceChildren(sr, shown);
    const start = performance.now();
    const step = (now: number) => {
      const k = Math.min(1, (now - start) / 1200);
      const eased = 1 - (1 - k) ** 3;
      shown.textContent = k < 1 ? `${pre}${fmt.format(to * eased).replace('-', '−')}${suf}` : `${pre}${final}${suf}`;
      if (k < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  }, { threshold: 0.5 });
  io.observe(root);
}

/** A dialog opened by a button: the content to open in place (without
 * script) becomes a button and a modal dialog (Escape closes, the focus
 * returns to the button, the rest of the page is inert). */
function dialogWidget(root: HTMLElement): void {
  const summary = root.querySelector<HTMLElement>(':scope > summary');
  const content = root.querySelector<HTMLElement>(':scope > .ewl-dialog-content');
  if (!summary || !content || typeof HTMLDialogElement === 'undefined') return;
  const titleEl = content.querySelector<HTMLElement>(':scope > .ewl-dialog-title');
  const titleId = uid('ewl-dlg');
  const heading = h('h2', { id: titleId, class: 'ewl-dialog-heading' }, titleEl?.textContent ?? '');
  titleEl?.remove();
  const close = h('button', { type: 'button', class: 'ew-btn ew-btn--secondary' }, t('common.close'));
  const dlg = h('dialog', { class: 'ew-dialog ewl-dialog-box', 'aria-labelledby': titleId },
    heading, content, h('div', { class: 'ew-dialog-actions' }, close)) as HTMLDialogElement;
  const open = h('button', { type: 'button', class: summary.className, 'aria-haspopup': 'dialog' }, ...[...summary.childNodes]) as HTMLButtonElement;
  close.addEventListener('click', () => dlg.close());
  // a click on the backdrop closes it too
  dlg.addEventListener('click', (ev) => { if (ev.target === dlg) dlg.close(); });
  // back to the button, unless the focus has already gone elsewhere (never
  // take it away from where the person is now)
  dlg.addEventListener('close', () => {
    const at = document.activeElement;
    if (!at || at === document.body || dlg.contains(at)) open.focus();
  });
  open.addEventListener('click', () => dlg.showModal());
  const wrap = h('div', { class: 'ewl-dialog-ready' }, open, dlg);
  root.replaceWith(wrap);
}

/** A tooltip: the word becomes a button; the explanation shows on pointing,
 * focus and tapping, stays while the pointer is on it and hides with
 * Escape (WCAG 1.4.13). */
function tipWidget(root: HTMLElement): void {
  const term = root.querySelector<HTMLElement>(':scope > .ewl-tip-term');
  const text = root.querySelector<HTMLElement>(':scope > .ewl-tip-text');
  if (!term || !text) return;
  const id = uid('ewl-tip');
  const tip = h('span', { class: 'ewl-tip-bubble', role: 'tooltip', id, hidden: true }, (text.textContent ?? '').trim().replace(/^\(|\)$/g, ''));
  const btn = h('button', { type: 'button', class: 'ewl-tip-button', 'aria-describedby': id }, term.textContent ?? '') as HTMLButtonElement;
  let pinned = false;
  let timer = 0;
  const show = () => { window.clearTimeout(timer); delete tip.dataset.dismissed; tip.hidden = false; };
  // it stays while it is pinned by a tap or the word has the keyboard focus
  // (only Escape or leaving the word with the focus hides it then)
  const hide = (force = false) => {
    if ((pinned || document.activeElement === btn) && !force) return;
    window.clearTimeout(timer);
    timer = window.setTimeout(() => { tip.hidden = true; pinned = false; }, force ? 0 : 200);
  };
  // shown with the keyboard focus also by CSS (:focus-visible), whatever the
  // order of events; Escape dismisses it until the focus leaves
  btn.addEventListener('focus', show);
  btn.addEventListener('blur', () => { delete tip.dataset.dismissed; hide(true); });
  btn.addEventListener('click', () => { pinned = !pinned || tip.hidden === true; if (pinned) show(); else hide(true); });
  root.addEventListener('mouseenter', show);
  root.addEventListener('mouseleave', () => hide());
  const dismiss = () => { tip.dataset.dismissed = ''; hide(true); };
  const shownNow = () => tip.hidden !== true || (btn.matches(':focus-visible') && tip.dataset.dismissed === undefined);
  root.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && shownNow()) { ev.stopPropagation(); dismiss(); } });
  document.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && shownNow()) dismiss(); });
  root.replaceChildren(btn, tip);
  root.classList.add('ewl-tip--ready');
}

export function register(): void {
  registerWidget('ewl.counter', counterWidget);
  registerWidget('ewl.dialog', dialogWidget);
  registerWidget('ewl.tip', tipWidget);
  registerWidget('ewl.tabs', (root) => {
    const parts = [...root.querySelectorAll<HTMLElement>(':scope > .ewl-tab')];
    if (parts.length < 1) return;
    const list = h('div', { role: 'tablist', class: 'ewl-tablist', 'aria-label': root.dataset.label || undefined });
    const tabs = parts.map((part, i) => {
      const name = part.querySelector<HTMLElement>(':scope > .ewl-tab-name')?.textContent ?? String(i + 1);
      const tabId = uid('ewl-tab');
      part.setAttribute('role', 'tabpanel');
      part.setAttribute('aria-labelledby', tabId);
      part.tabIndex = 0;
      const tab = h('button', { type: 'button', role: 'tab', id: tabId, 'aria-controls': part.id, 'aria-selected': 'false', tabindex: '-1' }, name) as HTMLButtonElement;
      tab.addEventListener('click', () => select(i, false));
      return tab;
    });
    const select = (i: number, focus: boolean) => {
      tabs.forEach((t, j) => {
        t.setAttribute('aria-selected', String(i === j));
        t.tabIndex = i === j ? 0 : -1;
        parts[j].hidden = i !== j;
      });
      if (focus) tabs[i].focus();
    };
    list.addEventListener('keydown', (ev) => {
      const at = tabs.indexOf(document.activeElement as HTMLButtonElement);
      if (at < 0) return;
      const to = ev.key === 'ArrowRight' ? (at + 1) % tabs.length : ev.key === 'ArrowLeft' ? (at - 1 + tabs.length) % tabs.length
        : ev.key === 'Home' ? 0 : ev.key === 'End' ? tabs.length - 1 : -1;
      if (to < 0) return;
      ev.preventDefault();
      select(to, true);
    });
    list.append(...tabs);
    root.prepend(list);
    root.classList.add('ewl-tabs--ready');
    // a link to one part (#ewl-tab-…) opens its tab
    const hash = location.hash.slice(1);
    const start = Math.max(0, parts.findIndex((p) => p.id === hash));
    select(start, false);
  });
}
