// The hint on long sentences and long words while writing (site setting
// ui.readability, personal choice in the profile). It never blocks saving
// and never changes the text: the visual editor marks with decorations of
// ProseMirror, the simple editor with highlights of the browser (CSS Custom
// Highlight API), the Markdown source only lists the places.
//
// For screen readers it stays quiet: a summary ("3 long sentences") that
// the editor names as its description (read when it gets the focus, not
// announced while typing), and the list of places on demand. The line has
// the same height in every state and the list a fixed height, so nothing
// below moves while it changes.
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { active, check, markdownProse, type Finding, type Rules } from '../core/readability';

/** One place in the list: what it is, the start of its text and how to go there. */
export interface HintItem { kind: Finding['kind']; count: number; excerpt: string; select: () => void }

/** The limits if the hint is on for the signed-in person (else null). */
export function readabilityRules(): Rules | null {
  const r = state.site?.readability;
  if (!r || !state.user || !active(r.mode, state.user.prefs?.readability)) return null;
  return { sentenceWords: r.sentence_words || 20, wordLetters: r.word_letters || 22 };
}

/** How many characters of a text the hint checks (limit core.ui.readability_kb). */
export function readabilityMaxChars(): number {
  return Math.max(16, state.site?.readability?.max_kb || 512) * 1024;
}

/** The start of a text for the list (one line, at most about 60 characters). */
export function excerpt(text: string): string {
  const s = text.replace(/\s+/g, ' ').trim();
  return s.length > 60 ? `${s.slice(0, 57).trimEnd()} …` : s;
}

/** Waits until typing pauses. */
export function debounce(fn: () => void, ms = 400): { run: () => void; cancel: () => void } {
  let timer = 0;
  return {
    run: () => { window.clearTimeout(timer); timer = window.setTimeout(fn, ms); },
    cancel: () => window.clearTimeout(timer),
  };
}

/** The line with the summary and the list of places on demand. */
export class ReadabilityHint {
  readonly el: HTMLElement;
  /** id of the summary: the editor names it as its description */
  readonly summaryId = uid('ew-rh');
  private readonly summary: HTMLElement;
  private readonly toggle: HTMLButtonElement;
  private readonly box: HTMLElement;
  private readonly list: HTMLUListElement;
  private items: HintItem[] = [];
  private text = '';
  private open = false;

  constructor() {
    const listId = uid('ew-rh-list');
    this.summary = h('span', { class: 'ew-rh-summary', id: this.summaryId });
    this.toggle = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-rh-toggle', 'aria-expanded': 'false', 'aria-controls': listId }, t('readability.places'));
    this.toggle.addEventListener('click', () => this.setOpen(!this.open));
    this.list = h('ul', { class: 'ew-rh-items' });
    this.box = h('div', { class: 'ew-rh-list', id: listId, role: 'region', 'aria-label': t('readability.places_list'), tabindex: '0', hidden: true }, this.list);
    this.el = h('div', { class: 'ew-rh' }, h('p', { class: 'ew-rh-line' }, this.summary, this.toggle), this.box);
    this.update([], false);
  }

  private setOpen(open: boolean): void {
    this.open = open;
    this.box.hidden = !open;
    this.toggle.setAttribute('aria-expanded', String(open));
    if (open) this.fill();
  }

  /** Shows the places found (truncated: only the start of the text was checked). */
  update(items: HintItem[], truncated: boolean): void {
    const sentences = items.filter((x) => x.kind === 'sentence').length;
    const words = items.length - sentences;
    const parts: string[] = [];
    if (sentences) parts.push(t('readability.sentences', { count: sentences }));
    if (words) parts.push(t('readability.words', { count: words }));
    let text = parts.length ? t('readability.found', { what: parts.join(', ') }) : t('readability.none');
    if (truncated) text += ` ${t('readability.truncated')}`;
    this.items = items;
    if (text !== this.text) {
      this.text = text;
      this.summary.textContent = text;
    }
    if (this.open) this.fill();
  }

  private fill(): void {
    // the focus stays on the same place of the list when it is renewed
    const focused = this.list.contains(document.activeElement) ? [...this.list.querySelectorAll('button')].indexOf(document.activeElement as HTMLButtonElement) : -1;
    this.list.replaceChildren(...(this.items.length ? this.items.map((it) => {
      const b = h('button', { type: 'button', class: 'ew-rh-item' },
        t(it.kind === 'sentence' ? 'readability.item_sentence' : 'readability.item_word', { count: it.count, text: it.excerpt }));
      b.addEventListener('click', () => it.select());
      return h('li', null, b);
    }) : [h('li', { class: 'ew-help' }, t('readability.none'))]));
    if (focused >= 0) (this.list.querySelectorAll('button')[Math.min(focused, this.items.length - 1)] as HTMLButtonElement | undefined)?.focus();
  }
}

// ---------------------------------------------------------------- textarea (Markdown source)

/** The places of Markdown source in a textarea (selected when chosen). */
export function textareaItems(ta: HTMLTextAreaElement, rules: Rules, max: number): { items: HintItem[]; truncated: boolean } {
  const src = ta.value;
  const truncated = src.length > max;
  const text = markdownProse(truncated ? src.slice(0, max) : src);
  const items = check(text, rules).map((f) => ({
    kind: f.kind, count: f.count, excerpt: excerpt(text.slice(f.start, f.end)),
    select: () => { ta.focus(); ta.setSelectionRange(f.start, f.end); },
  }));
  return { items, truncated };
}

// ---------------------------------------------------------------- HTML (contenteditable)

const BLOCKS = 'p, h1, h2, h3, h4, h5, h6, li, td, th, dt, dd, caption, figcaption, blockquote, div';

/** The highlights of the browser, if it has them (the marks are only drawn, the DOM stays). */
interface HighlightRegistry { set(name: string, h: unknown): void; delete(name: string): void }
const highlights = (): { reg: HighlightRegistry; make: (ranges: Range[]) => unknown } | null => {
  const reg = (globalThis as { CSS?: { highlights?: HighlightRegistry } }).CSS?.highlights;
  const H = (globalThis as { Highlight?: new (...r: Range[]) => unknown }).Highlight;
  return reg && H ? { reg, make: (ranges) => new H(...ranges) } : null;
};

/** Marks the places in an editable HTML surface (the text stays as it is). */
export function markRanges(sentences: Range[], words: Range[]): void {
  const hl = highlights();
  if (!hl) return;
  hl.reg.set('ew-rh-sentence', hl.make(sentences));
  hl.reg.set('ew-rh-word', hl.make(words));
}

/** Removes the marks of markRanges. */
export function clearRanges(): void {
  const hl = highlights();
  hl?.reg.delete('ew-rh-sentence');
  hl?.reg.delete('ew-rh-word');
}

/**
 * The places of an editable HTML surface: the text of each block, code
 * (inline) as one word, code blocks left out. Returns the ranges to mark.
 */
export function domItems(root: HTMLElement, rules: Rules, max: number): { items: HintItem[]; sentences: Range[]; words: Range[]; truncated: boolean } {
  type Ref = { node: Text; offset: number };
  const groups: { block: Element; text: string; refs: (Ref | null)[] }[] = [];
  let budget = max;
  let truncated = false;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (n instanceof Element) {
      if (n.tagName === 'BR') {
        const g = groups[groups.length - 1];
        if (g && n.parentElement && g.block.contains(n)) { g.text += '\n'; g.refs.push(null); }
      }
      continue;
    }
    const node = n as Text;
    const parent = node.parentElement;
    if (!parent || parent.closest('pre')) continue;
    const block = parent.closest(BLOCKS) ?? root;
    let g = groups[groups.length - 1];
    if (!g || g.block !== block) groups.push(g = { block, text: '', refs: [] });
    const code = !!parent.closest('code');
    const s = node.data;
    if (budget <= 0) { truncated = truncated || !!s.trim(); continue; }
    const take = Math.min(s.length, budget);
    budget -= take;
    if (take < s.length) truncated = true;
    if (code) { g.text += '`'; g.refs.push({ node, offset: 0 }); }
    for (let i = 0; i < take; i++) {
      g.text += code && s[i] === '`' ? "'" : s[i];
      g.refs.push({ node, offset: i });
    }
    if (code) { g.text += '`'; g.refs.push({ node, offset: Math.max(0, take - 1) }); }
  }
  const items: HintItem[] = [];
  const sentences: Range[] = [];
  const words: Range[] = [];
  for (const g of groups) {
    for (const f of check(g.text, rules)) {
      const a = g.refs.slice(f.start, f.end).find((r) => r) ?? null;
      const b = g.refs.slice(f.start, f.end).reverse().find((r) => r) ?? null;
      if (!a || !b) continue;
      const range = document.createRange();
      range.setStart(a.node, a.offset);
      range.setEnd(b.node, b.offset + 1);
      (f.kind === 'sentence' ? sentences : words).push(range);
      items.push({
        kind: f.kind, count: f.count, excerpt: excerpt(g.text.slice(f.start, f.end)),
        select: () => {
          if (!a.node.isConnected || !b.node.isConnected) return;
          const sel = document.getSelection();
          root.focus();
          sel?.removeAllRanges();
          sel?.addRange(range.cloneRange());
          (a.node.parentElement ?? root).scrollIntoView({ block: 'nearest' });
        },
      });
    }
  }
  return { items, sentences, words, truncated };
}

