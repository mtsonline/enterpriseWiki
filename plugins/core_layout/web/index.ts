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

/** A button that pauses and resumes motion (WCAG 2.2.2); its name says what
 * it does now. */
function pauseButton(onChange: (paused: boolean) => void): HTMLButtonElement {
  const btn = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ewl-pause', 'aria-pressed': 'false' }) as HTMLButtonElement;
  const show = (paused: boolean) => {
    btn.setAttribute('aria-pressed', String(paused));
    btn.replaceChildren(h('span', { 'aria-hidden': 'true' }, paused ? '▶ ' : '❚❚ '), paused ? t('layout.resume') : t('layout.pause'));
  };
  btn.addEventListener('click', () => { const p = btn.getAttribute('aria-pressed') !== 'true'; show(p); onChange(p); });
  show(false);
  return btn;
}

/** Motion stops while the pointer or the focus is in root. */
function heldWhileUsed(root: HTMLElement): () => boolean {
  let held = false;
  const on = () => { held = true; };
  const off = () => { held = root.matches(':hover') || root.contains(document.activeElement); };
  root.addEventListener('mouseenter', on);
  root.addEventListener('mouseleave', () => { held = root.contains(document.activeElement); });
  root.addEventListener('focusin', on);
  root.addEventListener('focusout', () => setTimeout(off, 0));
  return () => held;
}

/** A heading with changing words: one word shows at a time (every 2.5 s);
 * screen readers get the whole text with all words; a pause button; nothing
 * moves with reduced motion. */
function rotatingWidget(root: HTMLElement): void {
  const heading = root.querySelector<HTMLElement>('.ewl-rot-heading');
  const words = [...root.querySelectorAll<HTMLElement>('.ewl-rot-word')];
  if (!heading || words.length < 2 || reducedMotion()) return;
  const whole = heading.textContent ?? '';
  const visual = h('span', { 'aria-hidden': 'true', class: 'ewl-rot-visual' }, ...[...heading.childNodes]);
  heading.replaceChildren(h('span', { class: 'sr-only' }, whole), visual);
  root.classList.add('ewl-rot--ready');
  const typing = root.classList.contains('ewl-rot--type');
  const texts = words.map((w) => w.textContent ?? '');
  let at = 0;
  let paused = false;
  const held = heldWhileUsed(root);
  const show = (i: number) => {
    words.forEach((w, j) => w.classList.toggle('is-active', i === j));
    if (typing) {
      const w = words[i];
      let n = 0;
      w.textContent = '';
      const step = () => { n++; w.textContent = texts[i].slice(0, n); if (n < texts[i].length) setTimeout(step, 70); };
      step();
    }
  };
  show(0);
  window.setInterval(() => {
    if (paused || held() || !root.isConnected) return;
    at = (at + 1) % words.length;
    show(at);
  }, 2500);
  root.append(pauseButton((p) => { paused = p; }));
}

/** A ticker: the list runs through the band (a copy hidden from screen
 * readers closes the loop); a pause button; it stops while used; nothing
 * moves with reduced motion. */
function tickerWidget(root: HTMLElement): void {
  const list = root.querySelector<HTMLElement>(':scope > .ewl-ticker-list');
  if (!list || reducedMotion()) return;
  const copy = list.cloneNode(true) as HTMLElement;
  copy.setAttribute('aria-hidden', 'true');
  copy.removeAttribute('id');
  for (const el of copy.querySelectorAll<HTMLElement>('a, button, input, select, textarea, [tabindex]')) el.tabIndex = -1;
  const track = h('div', { class: 'ewl-ticker-track' }, list, copy);
  const band = h('div', { class: 'ewl-ticker-band' }, track);
  root.replaceChildren(pauseButton((p) => root.classList.toggle('ewl-ticker--paused', p)), band);
  root.classList.add('ewl-ticker--run');
}

/** A slider after the carousel pattern of WAI-ARIA: one part at a time,
 * previous and next, a button per part; optionally it changes by itself
 * (every 7 s, never with reduced motion) with a pause button, and stops
 * while used. */
function sliderWidget(root: HTMLElement): void {
  const box = root.querySelector<HTMLElement>(':scope > .ewl-slides');
  const slides = box ? [...box.querySelectorAll<HTMLElement>(':scope > .ewl-slide')] : [];
  if (!box || slides.length < 2) return;
  box.id ||= uid('ewl-slides');
  let at = 0;
  const auto = root.dataset.auto === 'true' && !reducedMotion();
  let paused = !auto;
  const dots = slides.map((_, i) => {
    const b = h('button', { type: 'button', class: 'ewl-slider-dot', 'aria-controls': box.id, 'aria-label': t('layout.slide_n', { n: String(i + 1), of: String(slides.length) }) }) as HTMLButtonElement;
    b.addEventListener('click', () => go(i));
    return b;
  });
  const go = (i: number) => {
    at = (i + slides.length) % slides.length;
    slides.forEach((s, j) => { s.hidden = j !== at; });
    dots.forEach((d, j) => { if (j === at) d.setAttribute('aria-current', 'true'); else d.removeAttribute('aria-current'); });
  };
  const prev = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle', 'aria-controls': box.id }, h('span', { 'aria-hidden': 'true' }, '‹ '), t('layout.previous')) as HTMLButtonElement;
  const next = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle', 'aria-controls': box.id }, t('layout.next'), h('span', { 'aria-hidden': 'true' }, ' ›')) as HTMLButtonElement;
  prev.addEventListener('click', () => go(at - 1));
  next.addEventListener('click', () => go(at + 1));
  // changes are announced only while it does not change by itself
  const live = () => box.setAttribute('aria-live', paused ? 'polite' : 'off');
  const controls = h('div', { class: 'ewl-slider-controls' });
  if (auto) {
    controls.append(pauseButton((p) => { paused = p; live(); }));
    const held = heldWhileUsed(root);
    window.setInterval(() => { if (!paused && !held() && root.isConnected) go(at + 1); }, 7000);
  }
  controls.append(prev, h('div', { class: 'ewl-slider-dots' }, ...dots), next);
  root.prepend(controls);
  root.classList.add('ewl-slider--ready');
  live();
  go(0);
}

/** An external video or embed (two clicks): the server writes a box that
 * names the service; nothing is loaded from it before the reader presses
 * the button. "Always load from …" is remembered in this browser only and
 * can be taken back next to the frame. */
const EMBED_KEY = 'ewiki.embed.allow';
function embedHosts(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(EMBED_KEY) || '[]') as unknown;
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}
function setEmbedHost(host: string, on: boolean): void {
  try {
    const hosts = embedHosts().filter((x) => x !== host);
    if (on) hosts.push(host);
    localStorage.setItem(EMBED_KEY, JSON.stringify(hosts.slice(-50)));
  } catch {
    // storage blocked: the choice holds for this page only
  }
}
function embedWidget(root: HTMLElement): void {
  const src = root.dataset.src ?? '';
  const host = root.dataset.host ?? '';
  const title = root.dataset.title ?? '';
  let url: URL;
  try {
    url = new URL(src);
  } catch {
    return;
  }
  if (url.protocol !== 'https:' || !host) return;
  const box = root.querySelector<HTMLElement>('.ewl-embed-box');
  if (!box) return;
  const load = (focus: boolean): void => {
    const frame = h('iframe', {
      class: 'ewl-embed-frame', src: url.href, title, loading: 'lazy', referrerpolicy: 'strict-origin-when-cross-origin',
      allow: 'fullscreen; picture-in-picture; encrypted-media', allowfullscreen: '',
      sandbox: 'allow-scripts allow-same-origin allow-presentation allow-popups allow-popups-to-escape-sandbox',
    }) as HTMLIFrameElement;
    const wrap = h('div', { class: 'ewl-embed-media' }, frame);
    box.replaceWith(wrap);
    if (embedHosts().includes(host)) {
      const stop = h('button', { type: 'button', class: 'ewl-embed-forget' }, t('layout.embed_forget', { host })) as HTMLButtonElement;
      stop.addEventListener('click', () => {
        setEmbedHost(host, false);
        stop.replaceWith(h('p', { class: 'ewl-embed-forgotten', role: 'status' }, t('layout.embed_forgotten', { host })));
      });
      wrap.after(stop);
    }
    if (focus) frame.focus();
  };
  if (embedHosts().includes(host)) {
    load(false);
    return;
  }
  const remember = h('input', { type: 'checkbox' }) as HTMLInputElement;
  const btn = h('button', { type: 'button', class: 'ewl-cta ewl-cta--primary ewl-embed-load' }, t('layout.embed_load')) as HTMLButtonElement;
  btn.addEventListener('click', () => {
    if (remember.checked) setEmbedHost(host, true);
    load(true);
  });
  const link = box.querySelector('.ewl-embed-link');
  const actions = h('p', { class: 'ewl-embed-actions' }, btn, h('label', { class: 'ewl-embed-remember' }, remember, ' ', t('layout.embed_remember', { host })));
  if (link) link.before(actions);
  else box.append(actions);
}

export function register(): void {
  registerWidget('ewl.rotating', rotatingWidget);
  registerWidget('ewl.ticker', tickerWidget);
  registerWidget('ewl.slider', sliderWidget);
  registerWidget('ewl.counter', counterWidget);
  registerWidget('ewl.embed', embedWidget);
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
