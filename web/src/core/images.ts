// Pictures in content that are shown smaller than they are open large on a
// click: they become links to the picture (the lightbox of the standard
// macros opens them; without it the link opens the picture). The name of
// the link is the alternative text, so screen readers say what opens.
import { t } from './i18n';

const OWN = /^\/api\/v1\/(media|files)\//;

/** Turns pictures of the wiki in content that are scaled down into links that open them large (each once). */
export function hydrateImages(root: ParentNode): void {
  root.querySelectorAll<HTMLImageElement>('.ew-content img').forEach((img) => {
    if (img.dataset.ewImg || img.closest('a, button, .ew-gallery, .ProseMirror, .ewe-prose, [data-ew-no-zoom]')) return;
    img.dataset.ewImg = '1';
    const src = img.getAttribute('src') ?? '';
    if (!OWN.test(src)) return; // only pictures of the wiki
    const wrap = () => {
      // only when it is shown smaller than it is
      if (!img.isConnected || img.naturalWidth <= img.clientWidth + 1 || img.closest('a')) return;
      const a = document.createElement('a');
      a.href = src.replace(/([?&])w=\d+&?/, '$1').replace(/[?&]$/, '');
      a.dataset.ewLightbox = '';
      a.dataset.title = img.alt;
      a.className = 'ew-zoom';
      // a picture without alternative text still gets a name for its link
      if (!img.alt.trim()) a.setAttribute('aria-label', t('image.open_large'));
      img.replaceWith(a);
      a.append(img);
    };
    if (img.complete && img.naturalWidth) wrap();
    else img.addEventListener('load', wrap, { once: true });
  });
}
