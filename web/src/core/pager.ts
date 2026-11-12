// Pages of macro lists (news, blog, FAQ … from sources): the links of the
// server-rendered page navigation (?mp=INDEX-PAGE) load only the one macro
// again, without reloading the page. Focus moves to the start of the list
// and the new page is announced.
import { api, qs } from './api';
import { announce, toast } from './announce';
import { hydrate } from './widgets';
import { state } from './state';
import { t } from './i18n';

let installed = false;

/** Handles clicks on page links of macro lists (once per document). */
export function installMacroPager(): void {
  if (installed) return;
  installed = true;
  document.addEventListener('click', (ev) => {
    const a = (ev.target as Element | null)?.closest?.<HTMLAnchorElement>('a[data-macro-page]');
    if (!a || ev.defaultPrevented || ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    const box = a.closest<HTMLElement>(`.ew-macro[data-macro-index="${CSS.escape(a.dataset.macroIndex ?? '')}"]`);
    const el = Number(a.dataset.element);
    const index = Number(a.dataset.macroIndex);
    const page = Number(a.dataset.macroPage);
    if (!box || !el || !index || !page) return;
    ev.preventDefault();
    ev.stopPropagation(); // not a navigation of the router
    void loadPage(box, el, index, page);
  }, true);
}

async function loadPage(box: HTMLElement, el: number, index: number, page: number): Promise<void> {
  box.setAttribute('aria-busy', 'true');
  try {
    const r = await api.get<{ html: string }>(`elements/${el}/macro/${index}${qs({ page, lang: state.lang })}`);
    const tpl = document.createElement('template');
    tpl.innerHTML = r.html; // server-rendered and sanitized like the element itself
    const next = tpl.content.firstElementChild as HTMLElement | null;
    if (!next) return;
    box.replaceWith(next);
    hydrate(next);
    const u = new URL(location.href);
    u.searchParams.set('mp', `${index}-${page}`);
    history.replaceState(history.state, '', u);
    next.tabIndex = -1;
    next.focus({ preventScroll: false });
    const cur = next.querySelector('.ew-pager .sr-only')?.textContent;
    announce(cur || t('pager.loaded', { page }));
  } catch (e) {
    box.removeAttribute('aria-busy');
    toast(e instanceof Error ? e.message : String(e), 'error');
  }
}
