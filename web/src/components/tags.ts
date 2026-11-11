// Tag input: comma separated, with suggestions of existing tags (native
// <datalist>, so screen readers and keyboards work as usual).
import { api } from '../core/api';
import { h } from '../core/dom';
import { textInput } from './ui';

let list: HTMLDataListElement | null = null;

/** A text input for tags ("a, b, c") that suggests existing tags. */
export function tagInput(value: string): HTMLInputElement {
  const input = textInput({ value, autocomplete: 'off' });
  let timer = 0;
  let asked = '';
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = window.setTimeout(async () => {
      const parts = input.value.split(',');
      const last = parts.pop()!.trim().replace(/^#/, '').toLowerCase();
      const head = parts.map((p) => p.trim()).filter(Boolean);
      if (!list) {
        list = h('datalist', { id: 'ew-tag-suggestions' });
        document.body.append(list);
      }
      input.setAttribute('list', list.id);
      if (!last || last === asked) return;
      asked = last;
      let tags: { tag: string }[] = [];
      try { tags = await api.get<{ tag: string }[]>(`tags?prefix=${encodeURIComponent(last)}&limit=8`); } catch { /* no suggestions */ }
      list.replaceChildren(...tags.filter((x) => !head.includes(x.tag) && x.tag !== last)
        .map((x) => h('option', { value: [...head, x.tag].join(', ') + ', ' })));
    }, 200);
  });
  return input;
}
