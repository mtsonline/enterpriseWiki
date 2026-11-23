// Styles of the standard macros (rendered on the server) and the lightbox
// of the image gallery.
import './style.css';
import { api, button, errorText, h, openDialog, qs, registerWidget, rerender, state, t, textInput, toast, uid, uploadDialog } from '@ewiki/coreplugin';

function lightbox(links: HTMLAnchorElement[], start: number): void {
  let i = start;
  const img = h('img', { class: 'ew-lightbox-img', alt: '' }) as HTMLImageElement;
  const caption = h('p', { class: 'ew-lightbox-caption', 'aria-live': 'polite' });
  const show = () => {
    const a = links[i];
    const thumb = a.querySelector('img');
    img.src = a.href;
    img.alt = thumb?.alt ?? '';
    caption.textContent = `${a.dataset.title || thumb?.alt || ''} (${i + 1}/${links.length})`;
    const prot = a.closest('.ew-protected') !== null;
    img.classList.toggle('ew-protected', prot);
    img.draggable = !prot;
  };
  const prev = button(t('gallery.prev'), () => { i = (i + links.length - 1) % links.length; show(); }, { variant: 'subtle' });
  const next = button(t('gallery.next'), () => { i = (i + 1) % links.length; show(); }, { variant: 'subtle' });
  const dlg = openDialog(t('gallery.title'), h('div', { class: 'ew-lightbox' }, img, caption), links.length > 1 ? [prev, next] : [], { wide: true });
  img.addEventListener('contextmenu', (ev) => { if (img.classList.contains('ew-protected')) ev.preventDefault(); });
  dlg.dialog.addEventListener('keydown', (ev) => {
    if (ev.key === 'ArrowLeft') { prev.click(); ev.preventDefault(); }
    if (ev.key === 'ArrowRight') { next.click(); ev.preventDefault(); }
  });
  show();
}

/** Filter of the page tree: shows the entries whose text contains the
 * words, with the categories above them (opened). Without script the tree
 * is complete and works on its own. */
function pageTreeFilter(box: HTMLElement): void {
  const nav = box.closest('.ew-pagetree');
  if (!nav) return;
  const input = textInput({ type: 'search' });
  const id = uid('ptf');
  input.id = id;
  const status = h('p', { class: 'sr-only', role: 'status' });
  box.append(h('label', { for: id }, t('pagetree.filter')), input, status);
  const items = Array.from(nav.querySelectorAll<HTMLLIElement>('li'));
  const details = Array.from(nav.querySelectorAll<HTMLDetailsElement>('details'));
  const wasOpen = details.map((d) => d.open);
  let timer = 0;
  const run = () => {
    const words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) {
      items.forEach((li) => { li.hidden = false; });
      details.forEach((d, i) => { d.open = wasOpen[i]; });
      status.textContent = '';
      return;
    }
    let found = 0;
    // from the bottom up: a category stays when it or something below matches
    for (const li of [...items].reverse()) {
      const own = (li.querySelector(':scope > a')?.textContent ?? '').toLowerCase();
      const hit = words.every((w) => own.includes(w));
      const below = Array.from(li.querySelectorAll<HTMLLIElement>(':scope > details > ul > li')).some((c) => !c.hidden);
      li.hidden = !hit && !below;
      if (hit) found++;
      const d = li.querySelector<HTMLDetailsElement>(':scope > details');
      if (d) d.open = below;
    }
    status.textContent = t('pagetree.found', { count: found });
  };
  input.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(run, 150); });
}

/** The first results of a search box while typing: a list of links under
 * the field and the number in a status (read by screen readers); the
 * search page shows all of them. */
function searchLive(form: HTMLElement): void {
  const input = form.querySelector<HTMLInputElement>('input[name="q"]');
  if (!input) return;
  const cat = form.querySelector<HTMLInputElement>('input[name="category"]')?.value;
  const status = h('p', { class: 'sr-only', role: 'status' });
  const list = h('ul', { class: 'ew-searchbox-results' });
  const more = h('p', { class: 'ew-searchbox-more' });
  const box = h('div', { class: 'ew-searchbox-live', hidden: true }, list, more);
  form.append(status, box);
  let timer = 0;
  let seq = 0;
  const run = async () => {
    const q = input.value.trim();
    const my = ++seq;
    if (q.length < 2) { box.hidden = true; status.textContent = ''; return; }
    let res: { elements: { element_id: number; title: string }[] };
    try { res = await api.get(`search${qs({ q, category: cat || undefined, lang: state.lang, limit: 6 })}`); } catch { return; }
    if (my !== seq) return; // an older answer
    const items = res.elements.slice(0, 5);
    list.replaceChildren(...items.map((x) => h('li', null, h('a', { href: `/e/${x.element_id}` }, x.title))));
    more.replaceChildren(h('a', { href: `/search${qs({ q, category: cat || undefined })}` }, t('search.all_results')));
    status.textContent = items.length ? t('search.live_count', { count: res.elements.length > 5 ? '5+' : String(items.length) }) : t('search.live_none');
    box.hidden = false;
  };
  input.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => void run(), 300); });
  // Escape closes the list and keeps the text
  form.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && !box.hidden) { box.hidden = true; input.focus(); } });
}

/** Filter of a news list by tag: one button per tag of the items shown
 * (pressed: only items with this tag); "All" shows everything again. */
function newsFilter(box: HTMLElement): void {
  const list = box.nextElementSibling;
  if (!list?.classList.contains('ew-news')) return;
  const items = Array.from(list.querySelectorAll<HTMLLIElement>(':scope > li'));
  const tags = [...new Set(items.flatMap((li) => (li.dataset.tags ?? '').split(',').filter(Boolean)))].sort().slice(0, 20);
  if (!tags.length) return;
  const status = h('p', { class: 'sr-only', role: 'status' });
  let current = '';
  const buttons: HTMLButtonElement[] = [];
  const apply = () => {
    let n = 0;
    for (const li of items) {
      li.hidden = !!current && !(li.dataset.tags ?? '').split(',').includes(current);
      if (!li.hidden) n++;
    }
    for (const b of buttons) b.setAttribute('aria-pressed', String(b.dataset.tag === current));
    status.textContent = t('news.filter_count', { count: n });
  };
  for (const tag of ['', ...tags]) {
    const b = button(tag ? `#${tag}` : t('news.filter_all'), () => { current = tag; apply(); }, { variant: 'subtle' });
    b.dataset.tag = tag;
    buttons.push(b);
  }
  box.append(h('div', { role: 'group', 'aria-label': t('news.filter') }, ...buttons), status);
  for (const b of buttons) b.setAttribute('aria-pressed', String(b.dataset.tag === ''));
}

/** The report of tasks: ticking a task saves its page (the server checks
 * who may). After a change the page has a new revision: the other tasks of
 * the same page tick from it. */
function taskTicks(box: HTMLElement): void {
  const status = box.querySelector<HTMLElement>('[role="status"]');
  box.addEventListener('change', async (ev) => {
    const cb = ev.target as HTMLInputElement;
    if (!cb.matches?.('input.ew-task-tick')) return;
    const { element, lang, rev, item } = cb.dataset;
    const done = cb.checked;
    cb.disabled = true;
    try {
      const r = await api.post<{ rev: number; state: string }>(`x/core.html/tasks/${Number(element)}/${Number(item)}`, { lang, rev: Number(rev), done });
      for (const o of box.querySelectorAll<HTMLInputElement>('input.ew-task-tick')) {
        if (o.dataset.element === element && o.dataset.lang === lang) o.dataset.rev = String(r.rev);
      }
      if (status) status.textContent = t(done ? 'tasks.ticked' : 'tasks.reopened');
    } catch (e) {
      cb.checked = !done;
      toast(errorText(e), 'error');
    } finally {
      cb.disabled = false;
    }
  });
}

export function register(): void {
  registerWidget('core.macros.pagetree', pageTreeFilter);
  registerWidget('core.macros.searchlive', searchLive);
  registerWidget('core.macros.newsfilter', newsFilter);
  registerWidget('core.macros.tasks', taskTicks);
  // the attachments list: upload a file into its category (the server shows
  // the button only to people who may add files there and checks it again)
  registerWidget('core.macros.upload', (box) => {
    const cat = Number(box.dataset.category) || 0;
    box.append(button(t('attachments.upload'), async () => {
      if (await uploadDialog({ categoryId: cat })) rerender();
    }, { variant: 'secondary' }));
  });
  document.addEventListener('click', (ev) => {
    const a = (ev.target as Element | null)?.closest?.('a[data-ew-lightbox]') as HTMLAnchorElement | null;
    if (!a || ev.ctrlKey || ev.metaKey || ev.shiftKey) return;
    const list = a.closest('.ew-gallery');
    const links = list ? Array.from(list.querySelectorAll<HTMLAnchorElement>('a[data-ew-lightbox]')) : [a];
    ev.preventDefault();
    lightbox(links, Math.max(0, links.indexOf(a)));
  });
  document.addEventListener('contextmenu', (ev) => {
    if ((ev.target as Element | null)?.closest?.('.ew-protected')) ev.preventDefault();
  });
}
