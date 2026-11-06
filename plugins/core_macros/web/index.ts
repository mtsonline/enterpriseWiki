// Styles of the standard macros (rendered on the server) and the lightbox
// of the image gallery.
import './style.css';
import { button, h, openDialog, t } from '@ewiki/sdk';

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

export function register(): void {
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
