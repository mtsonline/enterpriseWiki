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

/** Navigates to url (same origin). */
export function navigate(url: string, opts: { replace?: boolean } = {}): void {
  if (opts.replace) history.replaceState(null, '', url);
  else history.pushState(null, '', url);
  void render();
}

/** Re-renders the current route (e.g. after a language or login change). */
export function rerender(): Promise<void> {
  return render(true);
}

/** Starts the router; main is the element views render into. */
export function start(main: HTMLElement): void {
  mainEl = main;
  window.addEventListener('popstate', () => void render());
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

async function render(keepFocus = false): Promise<void> {
  if (!mainEl) return;
  const seq = ++renderSeq;
  // feed links (<link rel="alternate">) belong to the page that added them
  document.querySelectorAll('link[data-ew-feed]').forEach((l) => l.remove());
  const path = location.pathname;
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
