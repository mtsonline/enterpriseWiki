// Field for the image of link previews (Open Graph): upload an image, pick
// one of the page's images or enter an address. Empty = automatic.
import { uploadFile } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, errorText, field, textInput } from './ui';

export interface PreviewImageInput {
  el: HTMLElement;
  value: () => string;
}

/** previewImageInput builds the field; candidates are images of the page to choose from. */
export function previewImageInput(current: string, opts: { label?: string; help?: string; candidates?: string[] } = {}): PreviewImageInput {
  const url = textInput({ value: current, placeholder: '/api/v1/files/…', autocomplete: 'off' });
  const listId = uid('imgs');
  const cands = [...new Set(opts.candidates ?? [])].slice(0, 20);
  if (cands.length) url.setAttribute('list', listId);
  const preview = h('div', { class: 'ew-image-preview', 'aria-live': 'polite' });
  const draw = () => {
    const v = url.value.trim();
    preview.replaceChildren(v ? h('img', { src: v, alt: t('preview_image.alt') }) : h('span', { class: 'ew-help' }, t('preview_image.auto')));
  };
  url.addEventListener('change', draw);
  draw();
  const fileId = uid('pimg');
  const file = h('input', { type: 'file', id: fileId, accept: 'image/png,image/jpeg,image/gif,image/webp', class: 'sr-only' }) as HTMLInputElement;
  file.addEventListener('change', async () => {
    const f = file.files?.[0];
    if (!f) return;
    try {
      url.value = `/api/v1/files/${(await uploadFile(f)).hash}`;
      draw();
      toast(t('settings.image_uploaded'));
    } catch (e) { toast(errorText(e), 'error'); }
  });
  const el = h('div', { class: 'ew-preview-image-field' },
    field(opts.label ?? t('preview_image.label'), url, { help: opts.help ?? t('preview_image.help') }),
    cands.length ? h('datalist', { id: listId }, ...cands.map((c) => h('option', { value: c }))) : null,
    preview,
    h('div', { class: 'ew-actions' },
      h('label', { for: fileId, class: 'ew-btn ew-btn--secondary' }, t('preview_image.upload')), file,
      button(t('settings.remove_image'), () => { url.value = ''; file.value = ''; draw(); }, { variant: 'link', ariaLabel: `${t('settings.remove_image')}: ${opts.label ?? t('preview_image.label')}` })));
  return { el, value: () => url.value.trim() };
}

/** imagesOf lists the addresses of the images in a piece of HTML. */
export function imagesOf(html: string): string[] {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  return [...doc.querySelectorAll('img[src]')].map((i) => i.getAttribute('src') ?? '').filter((s) => s.startsWith('/') && !s.startsWith('//') || /^https?:\/\//.test(s));
}
