// Minimal History API router with focus management for screen readers.
import { announce } from './announce';
import { mount } from './dom';

export interface RouteCtx {
  params: Record<string, string>;
  query: URLSearchParams;
  path: string;
  main: HTMLElement;
}

export type View = (ctx: RouteCtx) => Promise<void> | void;

interface Route {
  re: RegExp;
  keys: string[];
  view: View;
}

const routes: Route[] = [];
let mainEl: HTMLElement | null = null;
let notFound: View | null = null;
let afterNavigate: (() => void) | null = null;
let beforeRender: (() => void) | null = null;
let renderSeq = 0;
let first = true;

/** Registers a route; ":name" segments become params. Later routes win. */
export function route(pattern: string, view: View): void {
  const keys: string[] = [];
  const re = new RegExp('^' + pattern.replace(/\/:([a-zA-Z_]+)/g, (_, k) => { keys.push(k); return '/([^/]+)'; }) + '/?$');
  routes.unshift({ re, keys, view });
}

export function setNotFound(v: View): void { notFound = v; }
export function onNavigate(fn: () => void): void { afterNavigate = fn; }

/**
 * Navigates to url (same origin). A page with unsaved changes asks first
 * (guardLeave); force: the page saved or dropped its changes itself.
 */
export function navigate(url: string, opts: { replace?: boolean; force?: boolean } = {}): void {
  const g = leave;
  if (!opts.force && g && safe(g.dirty)) {
    void g.ask().then((ok) => {
      if (!ok) return;
      if (leave === g) leave = null;
      go(url, opts);
    });
    return;
  }
  go(url, opts);
}

function go(url: string, opts: { replace?: boolean }): void {
  if (opts.replace) history.replaceState(null, '', url);
  else history.pushState(null, '', url);
  void render();
}

/** Re-renders the current route (e.g. after a language or login change). */
export function rerender(): Promise<void> {
  return render(true);
}

// the page asks before it is left (unsaved changes): links, back and forward
let leave: { dirty: () => boolean; ask: () => Promise<boolean> } | null = null;
let shownURL = '';

/**
 * Guards the current page: while dirty() is true, following a link of the
 * wiki or going back asks with ask() (true: leave), and closing or
 * reloading the tab asks with the browser's own question. Ends when the
 * next page is rendered.
 */
export function guardLeave(dirty: () => boolean, ask: () => Promise<boolean>): void {
  const g = { dirty, ask };
  leave = g;
  const unload = (e: BeforeUnloadEvent) => {
    if (leave !== g || !safe(dirty)) return;
    e.preventDefault();
    e.returnValue = ''; // older browsers need it to ask
  };
  window.addEventListener('beforeunload', unload);
  onLeave(() => {
    if (leave === g) leave = null;
    window.removeEventListener('beforeunload', unload);
  });
}

const safe = (f: () => boolean) => { try { return f(); } catch { return true; } };

/** Starts the router; main is the element views render into. */
export function start(main: HTMLElement): void {
  mainEl = main;
  window.addEventListener('popstate', () => {
    const g = leave;
    const to = location.pathname + location.search + location.hash;
    // a jump inside the guarded page (#heading, skip link) keeps it as it is
    if (g && to.split('#')[0] === shownURL.split('#')[0]) { shownURL = to; return; }
    if (g && safe(g.dirty)) {
      // back or forward: stay on the page until the user decided
      history.pushState(null, '', shownURL);
      void g.ask().then((ok) => { if (ok) { leave = null; go(to, {}); } });
      return;
    }
    void render();
  });
  document.addEventListener('click', (ev) => {
    if (ev.defaultPrevented || ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    const a = (ev.target as Element | null)?.closest?.('a');
    if (!a || a.target || a.hasAttribute('download') || a.getAttribute('rel')?.includes('external')) return;
    const href = a.getAttribute('href');
    if (!href || href.startsWith('#') || href.startsWith('mailto:')) return;
    const u = new URL(a.href, location.href);
    if (u.origin !== location.origin || u.pathname.startsWith('/api/')) return;
    ev.preventDefault();
    navigate(u.pathname + u.search + u.hash);
  });
  void render();
}

export function setMain(main: HTMLElement): void { mainEl = main; }

/** Runs before a page is rendered (e.g. to reset page context). */
export function onBeforeRender(fn: () => void): void { beforeRender = fn; }

let leaving: (() => void)[] = [];

/**
 * Registers clean-up work of the current page (e.g. cancelling a pending
 * passkey request); it runs once, before the next page is rendered.
 */
export function onLeave(fn: () => void): void { leaving.push(fn); }

async function render(keepFocus = false): Promise<void> {
  if (!mainEl) return;
  const seq = ++renderSeq;
  // feed links (<link rel="alternate">) belong to the page that added them
  document.querySelectorAll('link[data-ew-feed]').forEach((l) => l.remove());
  const path = location.pathname;
  shownURL = location.pathname + location.search + location.hash;
  const query = new URLSearchParams(location.search);
  let view: View | null = notFound;
  let params: Record<string, string> = {};
  for (const r of routes) {
    const m = r.re.exec(path);
    if (m) {
      view = r.view;
      params = Object.fromEntries(r.keys.map((k, i) => [k, decodeURIComponent(m[i + 1])]));
      break;
    }
  }
  const done = leaving;
  leaving = [];
  for (const fn of done) {
    try { fn(); } catch { /* clean-up must not stop the next page */ }
  }
  beforeRender?.();
  const target = document.createElement('div');
  target.className = 'ew-view';
  mainEl.setAttribute('aria-busy', 'true');
  try {
    if (view) await view({ params, query, path, main: target });
  } finally {
    if (seq === renderSeq) mainEl.removeAttribute('aria-busy');
  }
  if (seq !== renderSeq) return; // a newer navigation started
  mount(mainEl, target);
  afterNavigate?.();
  const h1 = mainEl.querySelector('h1');
  const title = h1?.textContent?.trim() ?? '';
  document.title = title ? `${title} – ${document.documentElement.dataset.site ?? ''}` : (document.documentElement.dataset.site ?? '');
  if (!first && !keepFocus) {
    if (h1) {
      h1.setAttribute('tabindex', '-1');
      h1.focus();
    }
    announce(title);
  }
  if (!keepFocus && !location.hash) window.scrollTo(0, 0);
  first = false;
}
