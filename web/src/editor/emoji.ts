// Emoji in the editor: a dialog with a search in the language of the
// interface (names and keywords from Unicode CLDR, via emojibase-data) and
// the groups of Unicode. The list is a listbox: the search field keeps the
// focus, the arrow keys move through the emoji, Enter inserts. The data is
// loaded only when the dialog opens (assets/emoji/<version>/<lang>.json).
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { formDialog } from '../components/ui';
import { EMOJI_BASE } from '../generated/emoji';

/** One emoji: character, name, keywords, group. */
type Emoji = [string, string, string, number];
interface EmojiData { groups: string[]; emoji: Emoji[] }

const cache = new Map<string, Promise<EmojiData>>();
function load(lang: string): Promise<EmojiData> {
  const l = lang.startsWith('de') ? 'de' : 'en';
  let p = cache.get(l);
  if (!p) {
    p = fetch(`${EMOJI_BASE}/${l}.json`).then((r) => {
      if (!r.ok) throw new Error(String(r.status));
      return r.json() as Promise<EmojiData>;
    });
    p.catch(() => cache.delete(l));
    cache.set(l, p);
  }
  return p;
}

/** Folds for the search: lower case, without accents (ä → a, ß → ss). */
export const fold = (s: string): string => s.toLocaleLowerCase().normalize('NFD').replace(/\p{M}/gu, '').replace(/ß/g, 'ss');

/** The emoji matching query (every word in the name or the keywords), at most max. */
export function searchEmoji(list: Emoji[], query: string, max = 200): Emoji[] {
  const words = fold(query).split(/\s+/).filter(Boolean);
  if (!words.length) return [];
  const out: Emoji[] = [];
  for (const e of list) {
    const hay = fold(`${e[1]} ${e[2]}`);
    if (words.every((w) => hay.includes(w))) {
      out.push(e);
      if (out.length >= max) break;
    }
  }
  return out;
}

let seq = 0;

/** Opens the emoji dialog; resolves with the chosen emoji or null. */
export async function emojiDialog(): Promise<string | null> {
  const data = await load(state.lang);
  const id = `ewe-emoji-${++seq}`;
  const search = h('input', {
    class: 'ew-input', type: 'search', role: 'combobox', 'aria-expanded': 'true', 'aria-controls': `${id}-list`,
    'aria-autocomplete': 'list', autocomplete: 'off', id: `${id}-q`,
  }) as HTMLInputElement;
  const groupSel = h('select', { class: 'ew-input', id: `${id}-g` },
    ...data.groups.map((g, i) => h('option', { value: String(i) }, g))) as HTMLSelectElement;
  const list = h('div', { class: 'ewe-emoji-list', role: 'listbox', id: `${id}-list`, 'aria-label': t('editor.emoji_list') });
  const status = h('p', { class: 'sr-only', role: 'status' });
  let shown: Emoji[] = [];
  let active = -1;
  let chosen: string | null = null;
  let close: (() => void) | null = null;

  const setActive = (i: number) => {
    const opts = list.children;
    if (!opts.length) return;
    active = Math.max(0, Math.min(opts.length - 1, i));
    for (let k = 0; k < opts.length; k++) opts[k].setAttribute('aria-selected', k === active ? 'true' : 'false');
    const el = opts[active] as HTMLElement;
    search.setAttribute('aria-activedescendant', el.id);
    el.scrollIntoView({ block: 'nearest' });
  };
  const choose = (e: Emoji) => { chosen = e[0]; close?.(); };
  const draw = () => {
    const q = search.value.trim();
    shown = q ? searchEmoji(data.emoji, q) : data.emoji.filter((e) => e[3] === Number(groupSel.value));
    groupSel.disabled = !!q;
    list.replaceChildren(...shown.map((e, i) => {
      const o = h('div', { class: 'ewe-emoji', role: 'option', id: `${id}-o${i}`, 'aria-selected': 'false', title: e[1] },
        h('span', { 'aria-hidden': 'true' }, e[0]), h('span', { class: 'sr-only' }, e[1]));
      o.addEventListener('click', () => choose(e));
      return o;
    }));
    active = -1;
    search.removeAttribute('aria-activedescendant');
    if (shown.length) setActive(0);
    status.textContent = q ? t('editor.emoji_found', { n: shown.length }) : '';
  };
  let timer = 0;
  search.addEventListener('input', () => { window.clearTimeout(timer); timer = window.setTimeout(draw, 120); });
  groupSel.addEventListener('change', draw);
  search.addEventListener('keydown', (ev) => {
    const cols = Math.max(1, Math.round(list.clientWidth / 44));
    const step: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: cols, ArrowUp: -cols };
    if (ev.key in step && shown.length) {
      ev.preventDefault();
      setActive(active + step[ev.key]);
    } else if (ev.key === 'Enter') {
      ev.preventDefault();
      if (active >= 0 && shown[active]) choose(shown[active]);
    }
  });
  draw();
  return new Promise((resolve) => {
    const handle = formDialog(t('editor.emoji'), [
      h('div', { class: 'ew-field' }, h('label', { for: search.id }, t('editor.emoji_search')), search, h('p', { class: 'ew-help' }, t('editor.emoji_help'))),
      h('div', { class: 'ew-field' }, h('label', { for: groupSel.id }, t('editor.emoji_group')), groupSel),
      list, status,
    ], t('editor.emoji_insert'), async () => {
      if (!chosen && active >= 0 && shown[active]) chosen = shown[active][0];
      if (!chosen) throw new Error(t('editor.emoji_none'));
    }, { wide: true, onClose: () => resolve(chosen) });
    close = () => handle.close();
    search.focus();
  });
}
