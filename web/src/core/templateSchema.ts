// The schematic of a template: where its positions are on a desktop, a
// tablet and a phone held upright or sideways (Administration → Templates).
// Every template describes its own rows for a size of window; the schematic
// draws them as boxes. The breakpoints are read from the same media queries
// the template uses (mediaMatches), so schematic and page agree; a browser
// test compares them for every template shipped.

/** The devices of the schematic, with the size of their window in CSS pixels. */
export const DEVICES = [
  { id: 'desktop', width: 1280, height: 800 },
  { id: 'tablet', width: 820, height: 1180 },
  { id: 'phone', width: 390, height: 844 },
  { id: 'phone-landscape', width: 844, height: 390 },
] as const;
export type Device = (typeof DEVICES)[number]['id'];
export interface WindowSize { width: number; height: number }

/** The place of the page itself (the content) among the positions of an area. */
export const MAIN = '@main';

/** One area of a row: the positions in it (MAIN for the content of the
 * page), how wide it is next to the others and, on this size, whether it is
 * closed behind a button (then the visible name of that button). */
export interface SchemaArea {
  kind: 'content' | 'brand' | 'area';
  positions: string[];
  /** share of the width of the row (default 1) */
  grow?: number;
  /** closed behind a button with this name (menu, tools …) */
  behind?: string;
  /** not shown at all on this size */
  hidden?: boolean;
}

/** One row of the schematic: its areas side by side, or in columns that wrap. */
export interface SchemaRow {
  role: 'banner' | 'contentinfo' | 'none';
  areas: SchemaArea[];
  /** how many areas fit next to each other (default: all; 1: one below the other) */
  columns?: number;
}

/** A template's description of its rows for a size of window. */
export type TemplateSchema = (size: WindowSize) => SchemaRow[];

/** The size of the window of a device. */
export function deviceSize(d: Device): WindowSize {
  const x = DEVICES.find((v) => v.id === d) ?? DEVICES[0];
  return { width: x.width, height: x.height };
}

const REM = 16;

/** Whether a media query applies to a window of this size. Understands what
 * templates use: lists with commas, "and", "or", "not", parentheses, and the
 * features width and height (min-/max-, in px, rem or em) and orientation.
 * Anything else does not apply (false), so a mistake shows as a difference
 * in the browser test instead of passing unnoticed. */
export function mediaMatches(query: string, size: WindowSize): boolean {
  return query.split(',').some((part) => {
    const p = new Parser(part.trim(), size);
    const ok = p.condition();
    return p.done() ? ok : false;
  });
}

class Parser {
  private i = 0;
  private readonly tokens: string[];
  constructor(text: string, private readonly size: WindowSize) {
    this.tokens = text.toLowerCase().match(/\(|\)|[^\s():]+(?:\s*:\s*[^\s()]+)?/g) ?? [];
  }
  done(): boolean { return this.i === this.tokens.length; }
  private peek(): string | undefined { return this.tokens[this.i]; }
  /** condition := ["not"] term (("and" | "or") term)* */
  condition(): boolean {
    let not = false;
    if (this.peek() === 'not') { this.i++; not = true; }
    if (this.peek() === 'only') this.i++;
    let v = this.term();
    while (this.peek() === 'and' || this.peek() === 'or') {
      const op = this.tokens[this.i++];
      const w = this.term();
      v = op === 'and' ? v && w : v || w;
    }
    return not ? !v : v;
  }
  /** term := "screen" | "all" | "(" feature ")" | "(" condition ")" */
  private term(): boolean {
    const tk = this.tokens[this.i++];
    if (tk === 'screen' || tk === 'all') return true;
    if (tk !== '(') return false;
    const inner = this.peek() ?? '';
    let v: boolean;
    if (inner === '(' || inner === 'not') v = this.condition();
    else { this.i++; v = this.feature(inner); }
    if (this.tokens[this.i++] !== ')') return false;
    return v;
  }
  private feature(text: string): boolean {
    const m = /^(min-|max-)?(width|height|orientation)\s*:\s*([0-9.]+)?(px|rem|em)?([a-z]+)?$/.exec(text);
    if (!m) return false;
    const [, bound, name, num, unit, word] = m;
    if (name === 'orientation') {
      const portrait = this.size.height >= this.size.width;
      return word === 'portrait' ? portrait : word === 'landscape' ? !portrait : false;
    }
    if (num === undefined) return false;
    const px = Number(num) * (unit === 'rem' || unit === 'em' ? REM : 1);
    const have = name === 'width' ? this.size.width : this.size.height;
    return bound === 'min-' ? have >= px : bound === 'max-' ? have <= px : have === px;
  }
}

/** The kinds of pages shown in the content of a template (the views of the
 * schematic of the placements). */
export type PageView = 'site' | 'category' | 'element' | 'editor' | 'dashboard';

/** The rows of a page inside the content of the template, for a size of
 * window: the positions of a category page, a page, the editor or the
 * dashboard. The columns stack where the core CSS stacks them (base.css). */
export function pageSchema(view: PageView, size: WindowSize): SchemaRow[] {
  const row = (...positions: string[]): SchemaRow => ({ role: 'none', areas: [{ kind: 'area', positions }] });
  const beside = (main: SchemaArea, aside: string, stackAt: string): SchemaRow =>
    ({ role: 'none', columns: mediaMatches(stackAt, size) ? 1 : undefined, areas: [main, { kind: 'area', positions: [aside] }] });
  switch (view) {
    case 'category':
    case 'element':
      return [row(`${view}-header`), row(`${view}-toolbar`),
        beside({ kind: 'content', positions: [`${view}-content`], grow: 3 }, `${view}-aside`, '(max-width: 48rem)'), row(`${view}-footer`)];
    case 'editor':
      return [row('editor-header'), row('editor-toolbar'), beside({ kind: 'content', positions: [MAIN], grow: 3 }, 'editor-aside', '(max-width: 56rem)'),
        row('editor-footer'), row('editor-statusbar')];
    case 'dashboard':
      return [row('dashboard-wide'), { role: 'none', columns: mediaMatches('(max-width: 60rem)', size) ? 1 : 3,
        areas: ['dashboard-1', 'dashboard-2', 'dashboard-3'].map((p) => ({ kind: 'area' as const, positions: [p] })) }];
    default:
      return [];
  }
}
