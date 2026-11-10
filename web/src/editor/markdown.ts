// Markdown <-> document: GitHub Flavored Markdown (markdown-it) plus macro
// directives (::leaf{...}, :::container ... :::, :inline[label]{...}).
import markdownit, { type MarkdownIt, type StateBlock, type StateCore, type StateInline, type Token } from 'markdown-it';
import { DOMSerializer, type Node as PMNode } from 'prosemirror-model';
import { MarkdownParser, MarkdownSerializer, MarkdownSerializerState, defaultMarkdownSerializer } from 'prosemirror-markdown';
import { NAME, attrEnd, formatAttrs, parseAttrs } from './attrs';
import { schema } from './schema';

// ---------------------------------------------------------------- markdown-it plugin

function header(s: string): { name: string; params: Record<string, string>; rest: string } | null {
  const m = NAME.exec(s);
  if (!m) return null;
  let rest = s.slice(m[0].length);
  let params: Record<string, string> = {};
  if (rest.startsWith('{')) {
    const end = attrEnd(rest);
    if (end < 0) return null;
    const p = parseAttrs(rest.slice(1, end));
    if (!p) return null;
    params = p;
    rest = rest.slice(end + 1);
  }
  return { name: m[0], params, rest };
}

function directiveBlock(state: StateBlock, startLine: number, endLine: number, silent: boolean): boolean {
  const pos = state.bMarks[startLine] + state.tShift[startLine];
  const max = state.eMarks[startLine];
  if (state.sCount[startLine] - state.blkIndent >= 4) return false;
  const line = state.src.slice(pos, max).trimEnd();
  let colons = 0;
  while (line[colons] === ':') colons++;
  if (colons < 2) return false;
  const h = header(line.slice(colons).trimStart());
  if (!h || h.rest.trim() !== '') return false;
  if (silent) return true;
  if (colons === 2) {
    const t = state.push('directive_leaf', 'div', 0);
    t.meta = { name: h.name, params: h.params };
    t.map = [startLine, startLine + 1];
    state.line = startLine + 1;
    return true;
  }
  // container: find the closing fence with exactly the same number of colons
  let next = startLine + 1;
  let closed = false;
  for (; next < endLine; next++) {
    const s = state.src.slice(state.bMarks[next] + state.tShift[next], state.eMarks[next]).trimEnd();
    if (s.length === colons && /^:+$/.test(s) && state.sCount[next] - state.blkIndent < 4) {
      closed = true;
      break;
    }
  }
  const open = state.push('directive_open', 'div', 1);
  open.meta = { name: h.name, params: h.params };
  open.block = true;
  open.map = [startLine, next];
  const oldParent = state.parentType;
  const oldLineMax = state.lineMax;
  state.parentType = 'container' as typeof state.parentType;
  state.lineMax = next;
  state.md.block.tokenize(state, startLine + 1, next);
  state.parentType = oldParent;
  state.lineMax = oldLineMax;
  state.push('directive_close', 'div', -1).block = true;
  state.line = closed ? next + 1 : next;
  return true;
}

function directiveInline(state: StateInline, silent: boolean): boolean {
  const src = state.src;
  let pos = state.pos;
  if (src.charCodeAt(pos) !== 0x3a /* : */) return false;
  const prev = pos > 0 ? src[pos - 1] : ' ';
  if (/[\p{L}\p{N}_:/\\]/u.test(prev)) return false;
  const m = NAME.exec(src.slice(pos + 1));
  if (!m) return false;
  pos += 1 + m[0].length;
  let label = '';
  let hasLabel = false;
  if (src[pos] === '[') {
    let depth = 0;
    let end = -1;
    for (let i = pos; i < src.length; i++) {
      const c = src[i];
      if (c === '\\') { i++; continue; }
      if (c === '[') depth++;
      if (c === ']' && --depth === 0) { end = i; break; }
    }
    if (end < 0) return false;
    label = src.slice(pos + 1, end).replace(/\\\]/g, ']');
    hasLabel = true;
    pos = end + 1;
  }
  let params: Record<string, string> = {};
  if (src[pos] === '{') {
    const end = attrEnd(src.slice(pos));
    if (end < 0) return false;
    const p = parseAttrs(src.slice(pos + 1, pos + end));
    if (!p) return false;
    params = p;
    pos += end + 1;
  } else if (!hasLabel) {
    return false;
  }
  if (!silent) {
    const t = state.push('directive_inline', 'span', 0);
    t.meta = { name: m[0], params, label };
  }
  state.pos = pos;
  return true;
}

// GFM task lists: "- [x] done" (list items whose paragraph starts with [ ] / [x])
function taskLists(state: StateCore): void {
  const toks = state.tokens;
  for (let i = 2; i < toks.length; i++) {
    const t = toks[i];
    if (t.type !== 'inline' || toks[i - 1].type !== 'paragraph_open' || toks[i - 2].type !== 'list_item_open') continue;
    const m = /^\[([ xX])\]\s+/.exec(t.content);
    if (!m) continue;
    toks[i - 2].attrSet('data-checked', m[1] === ' ' ? 'false' : 'true');
    t.content = t.content.slice(m[0].length);
    const first = t.children?.[0];
    if (first?.type === 'text') first.content = first.content.replace(/^\[([ xX])\]\s+/, '');
  }
}

export const md = markdownit('default', { html: true, linkify: false, breaks: false })
  .use((m: MarkdownIt) => {
    m.block.ruler.before('fence', 'directive', directiveBlock, { alt: ['paragraph', 'reference', 'blockquote', 'list'] });
    m.inline.ruler.before('emphasis', 'directive_inline', directiveInline);
    m.core.ruler.after('inline', 'task_lists', taskLists);
  });

// ---------------------------------------------------------------- parser

const cellAlign = (tok: Token) => {
  const s = String(tok.attrGet('style') ?? '');
  const m = /text-align:\s*(left|center|right)/.exec(s);
  return { align: m ? m[1] : null };
};

export const parser = new MarkdownParser(schema, md, {
  blockquote: { block: 'blockquote' },
  paragraph: { block: 'paragraph' },
  list_item: { block: 'list_item', getAttrs: (t) => ({ checked: t.attrGet('data-checked') === null ? null : t.attrGet('data-checked') === 'true' }) },
  bullet_list: { block: 'bullet_list', getAttrs: (_t, toks, i) => ({ tight: listIsTight(toks, i) }) },
  ordered_list: { block: 'ordered_list', getAttrs: (t, toks, i) => ({ order: Number(t.attrGet('start') ?? 1), tight: listIsTight(toks, i) }) },
  heading: { block: 'heading', getAttrs: (t) => ({ level: Number(t.tag.slice(1)) }) },
  code_block: { block: 'code_block', noCloseToken: true },
  fence: { block: 'code_block', getAttrs: (t) => ({ params: t.info || '' }), noCloseToken: true },
  hr: { node: 'horizontal_rule' },
  image: { node: 'image', getAttrs: (t) => ({ src: t.attrGet('src'), title: t.attrGet('title') || null, alt: (t.children?.[0]?.content) || '' }) },
  hardbreak: { node: 'hard_break' },
  em: { mark: 'em' },
  strong: { mark: 'strong' },
  s: { mark: 'strike' },
  link: { mark: 'link', getAttrs: (t) => ({ href: t.attrGet('href'), title: t.attrGet('title') || null }) },
  code_inline: { mark: 'code', noCloseToken: true },
  html_block: { node: 'html_block', getAttrs: (t) => ({ html: t.content.replace(/\n$/, '') }) },
  html_inline: { node: 'html_inline', getAttrs: (t) => ({ html: t.content }) },
  directive_leaf: { node: 'macro', getAttrs: (t) => ({ name: t.meta.name, params: t.meta.params, leaf: true }) },
  directive: { block: 'macro', getAttrs: (t) => ({ name: t.meta.name, params: t.meta.params, leaf: false }) },
  directive_inline: { node: 'macro_inline', getAttrs: (t) => ({ name: t.meta.name, params: t.meta.params, label: t.meta.label }) },
  table: { block: 'table' },
  thead: { ignore: true },
  tbody: { ignore: true },
  tr: { block: 'table_row' },
  th: { block: 'table_header', getAttrs: cellAlign },
  td: { block: 'table_cell', getAttrs: cellAlign },
});

function listIsTight(tokens: readonly Token[], i: number): boolean {
  while (++i < tokens.length) if (tokens[i].type !== 'list_item_open') return tokens[i].hidden;
  return false;
}

/** Parses Markdown into a document (never throws: falls back to text). */
export function parse(src: string): PMNode {
  try {
    return parser.parse(src) ?? schema.node('doc', null, [schema.node('paragraph')]);
  } catch {
    return schema.node('doc', null, [schema.node('code_block', null, src ? schema.text(src) : undefined)]);
  }
}

// ---------------------------------------------------------------- serializer

function macroDepth(n: PMNode): number {
  let d = 0;
  n.forEach((c) => { if (c.type.name === 'macro' && !c.attrs.leaf) d = Math.max(d, 1 + macroDepth(c)); else d = Math.max(d, macroDepth(c)); });
  return d;
}

function inlineOf(state: MarkdownSerializerState, node: PMNode): string {
  // renders into a scratch buffer; the pending block separator is kept
  const s = state as unknown as { out: string; closed: unknown };
  const saved = s.out, closed = s.closed;
  s.out = '';
  s.closed = null;
  state.renderInline(node);
  const out = s.out;
  s.out = saved;
  s.closed = closed;
  return out;
}

const domSer = DOMSerializer.fromSchema(schema);

function tableHTML(node: PMNode): string {
  const div = document.createElement('div');
  div.append(domSer.serializeNode(node));
  return div.innerHTML;
}

export const serializer = new MarkdownSerializer({
  ...defaultMarkdownSerializer.nodes,
  text(state, node) {
    // a ":name[" or ":name{" in text must not become a macro
    const s = state as unknown as { esc: (str: string, start?: boolean) => string };
    const orig = s.esc;
    s.esc = (str: string, start?: boolean) => {
      let out = orig.call(state, str, start).replace(/(^|[^\p{L}\p{N}_:/\\]):(?=[a-z])/gu, '$1\\:');
      if (start) out = out.replace(/^(\s*):(?=:)/, '$1\\:');
      return out;
    };
    try {
      state.text(node.text ?? '', !(state as unknown as { inAutolink?: boolean }).inAutolink);
    } finally {
      s.esc = orig;
    }
  },
  bullet_list(state, node) {
    state.renderList(node, '  ', () => '- ');
  },
  list_item(state, node) {
    if (node.attrs.checked !== null) state.write(node.attrs.checked ? '[x] ' : '[ ] ');
    state.renderContent(node);
  },
  macro(state, node) {
    const { name, params, leaf } = node.attrs as { name: string; params: Record<string, string>; leaf: boolean };
    if (leaf) {
      state.write('::' + name + formatAttrs(params));
      state.closeBlock(node);
      return;
    }
    const fence = ':'.repeat(3 + macroDepth(node));
    state.write(fence + name + formatAttrs(params));
    state.ensureNewLine();
    if (node.childCount) state.renderContent(node);
    // the fence follows the content directly (no blank line)
    (state as unknown as { closed: unknown }).closed = null;
    state.ensureNewLine();
    state.write(fence);
    state.closeBlock(node);
  },
  macro_inline(state, node) {
    const { name, params, label } = node.attrs as { name: string; params: Record<string, string>; label: string };
    state.write(':' + name + '[' + label.replace(/\]/g, '\\]') + ']' + formatAttrs(params));
  },
  html_block(state, node) {
    state.write(node.attrs.html as string);
    state.closeBlock(node);
  },
  html_inline(state, node) {
    state.write(node.attrs.html as string);
  },
  table(state, node) {
    let merged = false;
    node.descendants((c) => { if ((c.attrs.colspan ?? 1) > 1 || (c.attrs.rowspan ?? 1) > 1) merged = true; return !merged; });
    if (merged) {
      // GFM tables cannot merge cells: keep the table as HTML
      state.write(tableHTML(node));
      state.closeBlock(node);
      return;
    }
    const rows: string[][] = [];
    let aligns: (string | null)[] = [];
    node.forEach((row, _o, ri) => {
      const cells: string[] = [];
      row.forEach((cell) => {
        if (ri === 0) aligns.push(cell.attrs.align as string | null);
        cells.push(inlineOf(state, cell).replace(/\n/g, '<br>').replace(/(^|[^\\])\|/g, '$1\\|').trim());
      });
      rows.push(cells);
    });
    const cols = Math.max(...rows.map((r) => r.length), 1);
    aligns = Array.from({ length: cols }, (_, i) => aligns[i] ?? null);
    const line = (r: string[]) => '| ' + Array.from({ length: cols }, (_, i) => r[i] ?? '').join(' | ') + ' |';
    state.write(line(rows[0] ?? []));
    state.ensureNewLine();
    state.write('|' + aligns.map((a) => (a === 'center' ? ' :---: |' : a === 'right' ? ' ---: |' : a === 'left' ? ' :--- |' : ' --- |')).join(''));
    for (const r of rows.slice(1)) {
      state.ensureNewLine();
      state.write(line(r));
    }
    state.closeBlock(node);
  },
  table_row() { /* written by table */ },
  table_cell() { /* written by table */ },
  table_header() { /* written by table */ },
}, {
  ...defaultMarkdownSerializer.marks,
  strike: { open: '~~', close: '~~', mixable: true, expelEnclosingWhitespace: true },
});

/** Serializes a document to Markdown. */
export function serialize(doc: PMNode): string {
  return serializer.serialize(doc, { tightLists: true }).replace(/\n{3,}/g, '\n\n').trim() + '\n';
}
