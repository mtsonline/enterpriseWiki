// The document model of the editor: GitHub Flavored Markdown plus macro
// directives and raw HTML (kept as it is).
import { Schema, type NodeSpec, type MarkSpec } from 'prosemirror-model';
import { tableNodes } from 'prosemirror-tables';

const cellAttrs = { align: { default: null as string | null } };

const nodes: Record<string, NodeSpec> = {
  doc: { content: 'block+' },
  paragraph: { group: 'block', content: 'inline*', parseDOM: [{ tag: 'p' }], toDOM: () => ['p', 0] },
  heading: {
    group: 'block', content: 'inline*', attrs: { level: { default: 2 } }, defining: true,
    parseDOM: [1, 2, 3, 4, 5, 6].map((l) => ({ tag: `h${l}`, attrs: { level: l } })),
    toDOM: (n) => [`h${n.attrs.level}`, 0],
  },
  blockquote: { group: 'block', content: 'block+', defining: true, parseDOM: [{ tag: 'blockquote' }], toDOM: () => ['blockquote', 0] },
  horizontal_rule: { group: 'block', parseDOM: [{ tag: 'hr' }], toDOM: () => ['hr'] },
  code_block: {
    group: 'block', content: 'text*', marks: '', code: true, defining: true, attrs: { params: { default: '' } },
    parseDOM: [{ tag: 'pre', preserveWhitespace: 'full' }],
    toDOM: (n) => ['pre', { 'data-lang': n.attrs.params || null }, ['code', 0]],
  },
  bullet_list: { group: 'block', content: 'list_item+', attrs: { tight: { default: true } }, parseDOM: [{ tag: 'ul' }], toDOM: () => ['ul', 0] },
  ordered_list: {
    group: 'block', content: 'list_item+', attrs: { order: { default: 1 }, tight: { default: true } },
    parseDOM: [{ tag: 'ol', getAttrs: (d) => ({ order: Number((d as HTMLElement).getAttribute('start') ?? 1) }) }],
    toDOM: (n) => ['ol', n.attrs.order === 1 ? {} : { start: n.attrs.order }, 0],
  },
  list_item: {
    content: 'paragraph block*', defining: true, attrs: { checked: { default: null as boolean | null } },
    parseDOM: [{ tag: 'li' }],
    toDOM: (n) => ['li', n.attrs.checked === null ? {} : { class: 'ewe-task', 'data-checked': String(n.attrs.checked) }, 0],
  },
  // raw HTML that Markdown cannot express (merged cells, figures ...)
  html_block: {
    group: 'block', atom: true, attrs: { html: { default: '' } }, selectable: true,
    toDOM: (n) => ['pre', { class: 'ewe-html', 'data-html': 'block' }, n.attrs.html],
  },
  // macros: a leaf (::name{...}) or a container with content (:::name{...})
  macro: {
    group: 'block', content: 'block*', defining: true, isolating: true,
    attrs: { name: { default: '' }, params: { default: {} as Record<string, string> }, leaf: { default: false } },
    toDOM: (n) => ['div', { class: 'ewe-macro', 'data-macro': n.attrs.name }, 0],
  },
  text: { group: 'inline' },
  image: {
    inline: true, group: 'inline', draggable: true,
    attrs: { src: {}, alt: { default: '' }, title: { default: null as string | null } },
    parseDOM: [{ tag: 'img[src]', getAttrs: (d) => ({ src: (d as HTMLElement).getAttribute('src'), alt: (d as HTMLElement).getAttribute('alt') ?? '', title: (d as HTMLElement).getAttribute('title') }) }],
    toDOM: (n) => ['img', { src: n.attrs.src, alt: n.attrs.alt, title: n.attrs.title }],
  },
  hard_break: { inline: true, group: 'inline', selectable: false, parseDOM: [{ tag: 'br' }], toDOM: () => ['br'] },
  html_inline: {
    inline: true, group: 'inline', atom: true, attrs: { html: { default: '' } },
    toDOM: (n) => ['code', { class: 'ewe-html', 'data-html': 'inline' }, n.attrs.html],
  },
  macro_inline: {
    inline: true, group: 'inline', atom: true,
    attrs: { name: { default: '' }, params: { default: {} as Record<string, string> }, label: { default: '' } },
    toDOM: (n) => ['span', { class: 'ewe-macro-inline', 'data-macro': n.attrs.name }, `${n.attrs.name}: ${n.attrs.label}`],
  },
  ...tableNodes({
    tableGroup: 'block', cellContent: 'inline*',
    cellAttributes: { align: { default: cellAttrs.align.default, getFromDOM: (d) => (d as HTMLElement).style.textAlign || null, setDOMAttr: (v, a) => { if (v) a.style = `text-align: ${v}`; } } },
  }),
};

const marks: Record<string, MarkSpec> = {
  em: { parseDOM: [{ tag: 'i' }, { tag: 'em' }], toDOM: () => ['em', 0] },
  strong: { parseDOM: [{ tag: 'strong' }, { tag: 'b' }], toDOM: () => ['strong', 0] },
  strike: { parseDOM: [{ tag: 's' }, { tag: 'del' }], toDOM: () => ['s', 0] },
  link: {
    attrs: { href: {}, title: { default: null as string | null } }, inclusive: false,
    parseDOM: [{ tag: 'a[href]', getAttrs: (d) => ({ href: (d as HTMLElement).getAttribute('href'), title: (d as HTMLElement).getAttribute('title') }) }],
    toDOM: (m) => ['a', { href: m.attrs.href, title: m.attrs.title, rel: 'noopener noreferrer' }, 0],
  },
  code: { parseDOM: [{ tag: 'code' }], toDOM: () => ['code', 0], excludes: '_' },
};

export const schema = new Schema({ nodes, marks });
