// Input for short URLs (/kalender) with the site address as visible prefix.
import { t } from '../core/i18n';
import { checkbox, textInput } from './ui';

/** The option "only this short URL" (readable paths /wiki/… switched off for the target). */
export function shortOnlyBox(checked: boolean): { el: HTMLElement; input: HTMLInputElement } {
  return checkbox(t('shorturl.only'), checked, { help: t('shorturl.only_help') });
}

/** Returns the input for a short URL (normalized when leaving the field). */
export function shortURLInput(value: string): HTMLInputElement {
  const input = textInput({ value: value ?? '', placeholder: 'kalender', maxlength: 120 });
  input.classList.add('ew-shorturl-input');
  input.addEventListener('blur', () => {
    // normalize like the server: lower case, spaces to dashes, no leading slash
    input.value = input.value.trim().replace(/^\/+|\/+$/g, '').toLowerCase().replace(/\s+/g, '-');
  });
  return input;
}

/** A link to an element or category: its short URL or /e/ID, /c/ID. */
export function hrefOf(kind: 'c' | 'e', id: number, short?: string): string {
  return short ? `/${short}` : `/${kind}/${id}`;
}
