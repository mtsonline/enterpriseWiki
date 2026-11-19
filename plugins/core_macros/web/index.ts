// Styles of the standard macros (rendered on the server) and the lightbox
// of the image gallery.
import './style.css';
import { button, h, openDialog, registerWidget, t, textInput, uid } from '@ewiki/coreplugin';

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

export function register(): void {
  registerWidget('core.macros.pagetree', pageTreeFilter);
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
