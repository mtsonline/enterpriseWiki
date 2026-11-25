// The preview of the template designer, inside its frame (or a window it opened): the wiki itself
// (same origin, same person, same rights), shown with the draft of the
// designer instead of the template that applies. The draft is read from the
// window of the designer, which must be of the same origin; it is only shown
// here and never stored. The engine writes CSS only from values of a safe
// form (cssSafe), so a draft that is not checked yet cannot inject anything.
import { applyScheme, h, hasPerm, registerTemplate, setPreviewTemplate, setTemplateScheme } from '@ewiki/coreplugin';
import { buildLayout, classOf, cssSafe, designCSS, type Design } from './engine';
import type { Measure, PreviewWindow } from './designer';

export const PREVIEW_ID = 'core.template/preview';

/** The draft of the designer that opened this window as its preview, if any. */
function draftOfParent(): Design | null {
  if (new URLSearchParams(location.search).get('ewd-preview') !== '1') return null;
  // the frame in the designer, or a window it opened ("preview in its own window")
  const designerWindow = window.parent !== window ? window.parent : (window.opener as Window | null);
  if (!designerWindow) return null;
  try {
    // throws for a window of another origin
    if (designerWindow.location.origin !== location.origin) return null;
    return designerWindow.__ewdDraft ?? null;
  } catch {
    return null;
  }
}

/** The scheme the designer shows in its preview: dark only when the draft
 * has dark colors; the choice of the person does not count here. */
function previewScheme(d: Design): 'light' | 'dark' | '' {
  if (!d.tokens.dark) return '';
  try {
    const w = window.parent !== window ? window.parent : (window.opener as Window | null);
    return w?.location.origin === location.origin && w.__ewdPreviewScheme === 'dark' ? 'dark' : 'light';
  } catch {
    return 'light';
  }
}

/** Sets up the preview when this window is the frame of the designer. */
export function setupPreview(): void {
  const first = draftOfParent();
  if (!first || !hasPerm('core.template.design') || !cssSafe(first)) return;
  let current = first;
  const cls = classOf(PREVIEW_ID);
  const style = h('style', { id: 'ewd-style-preview' });
  style.textContent = designCSS(current, cls);
  document.head.append(style);
  const positions = () => current.rows.flatMap((r) => r.blocks.flatMap((b) => [...b.positions, ...(b.after ?? [])]));
  registerTemplate({ id: PREVIEW_ID, className: cls, get positions() { return positions(); }, layout: (ctx) => buildLayout(current, ctx) });
  // light or dark as the designer shows it (not the choice of the person)
  delete document.documentElement.dataset.schemeChoice;
  setTemplateScheme(PREVIEW_ID, previewScheme(current));
  setPreviewTemplate(PREVIEW_ID);
  document.documentElement.classList.add('ewd-in-preview');
  const w = window as PreviewWindow;
  w.__ewdPreviewUpdate = (d: Design) => {
    if (!cssSafe(d)) return;
    current = d;
    style.textContent = designCSS(current, cls);
    setTemplateScheme(PREVIEW_ID, previewScheme(current));
    applyScheme(PREVIEW_ID);
    window.dispatchEvent(new Event('ewiki:relayout'));
  };
  w.__ewdMeasure = () => measure();
  w.__ewdHeader = () => {
    // the closed height of a sticky header (open drawers do not count)
    const hd = document.querySelector<HTMLElement>('header.ewd-landmark.ewd-sticky');
    if (!hd) return null;
    const closed = parseFloat(hd.style.getPropertyValue('--ewd-header-closed'));
    return { height: Number.isFinite(closed) ? closed : hd.getBoundingClientRect().height, window: window.innerHeight };
  };
}

/** What the designer shows as hints: per block whether its content has
 * room, and for menus how many entries fit on the first line. */
function measure(): Measure[] {
  const out: Measure[] = [];
  for (const el of document.querySelectorAll<HTMLElement>('.ewd-block[data-block]')) {
    const shown = el.offsetParent !== null || getComputedStyle(el).position === 'fixed';
    const items = [...el.querySelectorAll<HTMLElement>('.ew-pos a, .ew-pos button')].filter((x) => x.offsetParent !== null && !x.closest('[hidden], [role="menu"], .ewm-sub'));
    const top = items.length ? items[0].getBoundingClientRect().top : 0;
    const firstLine = items.filter((x) => Math.abs(x.getBoundingClientRect().top - top) < 4).length;
    out.push({ id: el.dataset.block ?? '', shown, overflow: el.scrollWidth > el.clientWidth + 1, need: el.scrollWidth, have: el.clientWidth, items: items.length, firstLine });
  }
  return out;
}
