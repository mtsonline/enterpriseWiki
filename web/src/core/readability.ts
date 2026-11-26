// Long sentences and long words: the rules of internal/readability (the
// test of the interface texts) for the hint while writing. Both run the
// shared cases of internal/readability/testdata/cases.json; the regular
// expressions are those of the server, with the whitespace of Go (\s there
// is [\t\n\f\r ]) and its ASCII \w and \b. Positions refer to the text
// passed in (UTF-16 indices, as in the DOM and in ProseMirror).

/** The limits: most words of a sentence, most letters of a word. */
export interface Rules { sentenceWords: number; wordLetters: number }

export const DEFAULT_RULES: Rules = { sentenceWords: 20, wordLetters: 22 };

/** A long sentence or a long word: where it is and how long. */
export interface Finding {
  kind: 'sentence' | 'word';
  start: number;
  end: number;
  /** words of the sentence, letters of the word */
  count: number;
}

const RE_CODE = /`[^`]*`|\{[^}]*\}/g;
const RE_URL = /https?:\/\/[^\t\n\f\r )]*[^\t\n\f\r ).,;:]/g;
const RE_PATH = /[\w.-]*[\w-]\/[\w./-]*[\w-]|[\w-]+\.(sh|yaml|json|go|ts|md)\b/g;
const RE_SENTENCE = /[.!?;:][\t\n\f\r ]+|\n+/g;
const RE_WORD = /[\p{L}\p{N}]+/gu;
const RE_LETTERS = /\p{L}+/gu;

/** A masked text: s, and for each of its characters where it came from in the original. */
interface Masked { s: string; from: number[]; to: number[] }

function replaceMapped(m: Masked, re: RegExp): Masked {
  let s = '';
  const from: number[] = [], to: number[] = [];
  let last = 0;
  re.lastIndex = 0;
  for (let x = re.exec(m.s); x; x = re.exec(m.s)) {
    if (x[0] === '') { re.lastIndex++; continue; }
    for (let i = last; i < x.index; i++) { s += m.s[i]; from.push(m.from[i]); to.push(m.to[i]); }
    s += 'X';
    from.push(m.from[x.index]);
    to.push(m.to[x.index + x[0].length - 1]);
    last = x.index + x[0].length;
  }
  for (let i = last; i < m.s.length; i++) { s += m.s[i]; from.push(m.from[i]); to.push(m.to[i]); }
  return { s, from, to };
}

/** Code, placeholders, addresses and paths become one word ("X"). */
function mask(text: string): Masked {
  const from: number[] = [], to: number[] = [];
  for (let i = 0; i < text.length; i++) { from.push(i); to.push(i + 1); }
  let m: Masked = { s: text, from, to };
  for (const re of [RE_CODE, RE_URL, RE_PATH]) m = replaceMapped(m, re);
  return m;
}

/** The sentence of a masked range [a, b) as positions in the original (trimmed). */
function span(m: Masked, a: number, b: number): { start: number; end: number } | null {
  while (a < b && /[\t\n\f\r  ]/.test(m.s[a])) a++;
  while (b > a && /[\t\n\f\r  ]/.test(m.s[b - 1])) b--;
  return a < b ? { start: m.from[a], end: m.to[b - 1] } : null;
}

/** The sentences of a text: the masked text of each and where it is in the original. */
export function sentences(text: string): { text: string; start: number; end: number }[] {
  const m = mask(text);
  const out: { text: string; start: number; end: number }[] = [];
  let last = 0;
  const add = (a: number, b: number, end?: number) => {
    const part = m.s.slice(a, b);
    if (!part.trim()) return;
    const sp = span(m, a, b);
    if (sp) out.push({ text: part, start: sp.start, end: end ?? sp.end });
  };
  RE_SENTENCE.lastIndex = 0;
  for (let x = RE_SENTENCE.exec(m.s); x; x = RE_SENTENCE.exec(m.s)) {
    // the mark that ends the sentence belongs to it (shown with it)
    add(last, x.index, x[0][0] !== '\n' ? m.to[x.index] : undefined);
    last = x.index + x[0].length;
  }
  add(last, m.s.length);
  return out;
}

/** The words of a sentence (letters and digits). */
export function wordCount(s: string): number {
  return s.match(RE_WORD)?.length ?? 0;
}

/** The long words of a text: runs of letters longer than max. */
export function longWords(text: string, max: number): { word: string; start: number; end: number }[] {
  const m = mask(text);
  const out: { word: string; start: number; end: number }[] = [];
  RE_LETTERS.lastIndex = 0;
  for (let x = RE_LETTERS.exec(m.s); x; x = RE_LETTERS.exec(m.s)) {
    if ([...x[0]].length > max) out.push({ word: x[0], start: m.from[x.index], end: m.to[x.index + x[0].length - 1] });
  }
  return out;
}

/** The long sentences and long words of a text, in the order of the text. */
export function check(text: string, rules: Rules): Finding[] {
  const out: Finding[] = [];
  for (const s of sentences(text)) {
    const n = wordCount(s.text);
    if (n > rules.sentenceWords) out.push({ kind: 'sentence', start: s.start, end: s.end, count: n });
  }
  for (const w of longWords(text, rules.wordLetters)) out.push({ kind: 'word', start: w.start, end: w.end, count: [...w.word].length });
  return out.sort((a, b) => a.start - b.start || (a.kind === 'sentence' ? -1 : 1));
}

/** Whether a person sees the hint: the site decides whether it is offered, the person within that ("" follows the site). */
export function active(siteMode: string | undefined, pref: string | undefined): boolean {
  if (siteMode === 'on') return pref !== 'off';
  if (siteMode === 'offered') return pref === 'on';
  return false;
}

/**
 * The text of Markdown source that the hint checks: code blocks (fenced) and
 * the lines of blocks (::name{…}) are left out, everything else is kept at
 * its place (blanked), so positions stay those of the source.
 */
export function markdownProse(src: string): string {
  let fence = '';
  return src.split('\n').map((line) => {
    const f = /^\s{0,3}(`{3,}|~{3,})/.exec(line);
    if (fence) {
      if (f && f[1][0] === fence[0] && f[1].length >= fence.length && !line.slice(f[0].length).trim()) fence = '';
      return ' '.repeat(line.length);
    }
    if (f) { fence = f[1]; return ' '.repeat(line.length); }
    if (/^\s*:{2,}/.test(line)) return ' '.repeat(line.length);
    return line;
  }).join('\n');
}
