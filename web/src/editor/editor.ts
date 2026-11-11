// The visual Markdown editor (ProseMirror). Saves GitHub Flavored Markdown
// with macro directives; a Markdown mode shows the source.
//
// Accessibility: the editing surface is a labelled multi-line textbox; the
// toolbar follows the WAI-ARIA toolbar pattern (one tab stop, arrow keys,
// Alt+F10 from the text); Tab only indents inside lists and tables, Escape
// followed by Tab always leaves the editor (no keyboard trap, WCAG 2.1.2).
import { baseKeymap, chainCommands, exitCode, joinBackward, lift, selectNodeBackward, setBlockType, toggleMark, wrapIn } from 'prosemirror-commands';
import { dropCursor } from 'prosemirror-dropcursor';
import { gapCursor } from 'prosemirror-gapcursor';
import { history, redo, undo } from 'prosemirror-history';
import { InputRule, inputRules, textblockTypeInputRule, undoInputRule, wrappingInputRule } from 'prosemirror-inputrules';
import { keymap } from 'prosemirror-keymap';
import { Fragment, Slice, type MarkType, type Node as PMNode, type NodeType, type ResolvedPos } from 'prosemirror-model';
import { liftListItem, sinkListItem, splitListItem, wrapInList } from 'prosemirror-schema-list';
import { EditorState, NodeSelection, Plugin, TextSelection, type Command, type Transaction } from 'prosemirror-state';
import { addColumnAfter, addColumnBefore, addRowAfter, addRowBefore, deleteColumn, deleteRow, deleteTable, goToNextCell, isInTable, tableEditing } from 'prosemirror-tables';
import { findWrapping } from 'prosemirror-transform';
import { EditorView, type NodeView } from 'prosemirror-view';
import { announce, toast } from '../core/announce';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { field, formDialog, select, textArea, textInput } from '../components/ui';
import { isImage, mediaURL, pickMedium } from '../components/media';
import { prosemirrorToYXmlFragment, redo as yRedo, undo as yUndo, yCursorPlugin, ySyncPlugin, yUndoPlugin, yUndoPluginKey } from 'y-prosemirror';
import { formatAttrs } from './attrs';
import { Collab, collabURL, colorOf, type Peer } from './collab';
import { label as macroLabel, loadMacros, macroDialog, type MacroChoice } from './macros';
import { parse, serialize } from './markdown';
import { schema } from './schema';

export type EditorMode = 'visual' | 'markdown';

export interface MarkdownEditorOptions {
  markdown: string;
  /** Content language (lang attribute, spell checking). */
  lang: string;
  /** Id of the element labelling the editor. */
  labelId: string;
  /** Initial mode (default: the user's last choice, else visual). */
  mode?: EditorMode;
  /** Called after every change of the content. */
  onChange?: () => void;
  /**
   * Live editing with the other editors of the element (the Markdown
   * passed in is used if this editor is the first one).
   */
  collab?: { elementId: number; userId: number; onSaved?: (rev: number, by?: Peer) => void };
}

export interface MarkdownEditor {
  getMarkdown(): string;
  setMarkdown(src: string): void;
  getMode(): EditorMode;
  setMode(m: EditorMode): void;
  focus(): void;
  destroy(): void;
  /** Tells the other live editors that revision rev was saved. */
  saved(rev: number): void;
  /** State of live editing: on, this editor joined first (leader), the others. */
  live(): { on: boolean; leader: boolean; peers: Peer[] };
  /** The ProseMirror view (tests). */
  view: EditorView;
}

const MODE_KEY = 'ewiki.editor.mode';
const nodes = schema.nodes;
const marks = schema.marks;

// ---------------------------------------------------------------- helpers

/** A form dialog that resolves with the value of ok(), or null if cancelled. */
function ask<T>(title: string, fields: HTMLElement[], submit: string, ok: () => T): Promise<T | null> {
  return new Promise((resolve) => {
    let result: T | null = null;
    formDialog(title, fields, submit, async () => { result = ok(); }, { onClose: () => resolve(result) });
  });
}

function markActive(state: EditorState, type: MarkType): boolean {
  const { from, $from, to, empty } = state.selection;
  if (empty) return !!type.isInSet(state.storedMarks || $from.marks());
  return state.doc.rangeHasMark(from, to, type);
}

function ancestor($pos: ResolvedPos, pred: (n: PMNode) => boolean): { node: PMNode; pos: number; depth: number } | null {
  for (let d = $pos.depth; d > 0; d--) {
    const n = $pos.node(d);
    if (pred(n)) return { node: n, pos: $pos.before(d), depth: d };
  }
  return null;
}

const isList = (n: PMNode) => n.type === nodes.bullet_list || n.type === nodes.ordered_list;

/** Toggles a list: wraps, unwraps, or changes the type of the list around the cursor. */
function toggleList(type: NodeType, task = false): Command {
  return (state, dispatch) => {
    const list = ancestor(state.selection.$from, isList);
    const isTask = !!list && list.node.firstChild?.attrs.checked !== null;
    if (list && list.node.type === type && isTask === task) return liftListItem(nodes.list_item)(state, dispatch);
    if (list) {
      if (dispatch) {
        const tr = state.tr.setNodeMarkup(list.pos, type);
        list.node.forEach((item, off) => {
          tr.setNodeMarkup(list.pos + 1 + off, undefined, { ...item.attrs, checked: task ? (item.attrs.checked ?? false) : null });
        });
        dispatch(tr);
      }
      return true;
    }
    if (!task) return wrapInList(type)(state, dispatch);
    return wrapInList(type)(state, dispatch && ((tr) => {
      const inner = ancestor(tr.selection.$from, isList);
      if (inner) inner.node.forEach((item, off) => { tr.setNodeMarkup(inner.pos + 1 + off, undefined, { ...item.attrs, checked: false }); });
      dispatch(tr);
    }));
  };
}

function toggleWrap(type: NodeType): Command {
  return (state, dispatch) => (ancestor(state.selection.$from, (n) => n.type === type) ? lift(state, dispatch) : wrapIn(type)(state, dispatch));
}

function toggleBlock(type: NodeType, attrs?: Record<string, unknown>): Command {
  return (state, dispatch) => {
    const p = state.selection.$from.parent;
    const same = p.type === type && (!attrs || Object.entries(attrs).every(([k, v]) => p.attrs[k] === v));
    return setBlockType(same ? nodes.paragraph : type, same ? undefined : attrs)(state, dispatch);
  };
}

/** Splits a list item; new items of task lists are open tasks. */
const splitItem: Command = (state, dispatch) => {
  const item = ancestor(state.selection.$from, (n) => n.type === nodes.list_item);
  return splitListItem(nodes.list_item, item && item.node.attrs.checked !== null ? { checked: false } : undefined)(state, dispatch);
};

/** Enter in an empty list item ends the list. */
const liftEmptyItem: Command = (state, dispatch) => {
  const { $from, empty } = state.selection;
  if (!empty || $from.parent.content.size || !ancestor($from, (n) => n.type === nodes.list_item)) return false;
  return liftListItem(nodes.list_item)(state, dispatch);
};

/** Toggles "done" of the task item around the cursor. */
const toggleTask: Command = (state, dispatch) => {
  const item = ancestor(state.selection.$from, (n) => n.type === nodes.list_item && n.attrs.checked !== null);
  if (!item) return false;
  dispatch?.(state.tr.setNodeMarkup(item.pos, undefined, { ...item.node.attrs, checked: !item.node.attrs.checked }));
  return true;
};

/**
 * Inserts a block node at the selection; inside a table (cells hold text
 * only) it goes below the table instead of splitting it.
 */
function insertBlock(state: EditorState, node: PMNode): Transaction {
  const table = ancestor(state.selection.$from, (n) => n.type === nodes.table);
  if (!table) return state.tr.replaceSelectionWith(node);
  const after = table.pos + table.node.nodeSize;
  const tr = state.tr.insert(after, node);
  return tr.setSelection(TextSelection.near(tr.doc.resolve(after + node.nodeSize)));
}

const insertHr: Command = (state, dispatch) => {
  dispatch?.(insertBlock(state, nodes.horizontal_rule.create()).scrollIntoView());
  return true;
};

const insertTable: Command = (state, dispatch) => {
  if (isInTable(state)) return false;
  const cell = (type: NodeType) => type.createAndFill()!;
  const row = (type: NodeType) => nodes.table_row.create(null, [cell(type), cell(type), cell(type)]);
  const table = nodes.table.create(null, [row(nodes.table_header), row(nodes.table_cell), row(nodes.table_cell)]);
  if (dispatch) {
    const tr = state.tr.replaceSelectionWith(table);
    const at = inserted(tr, state, nodes.table);
    if (at >= 0) tr.setSelection(TextSelection.create(tr.doc, at + 3)); // table > row > cell
    dispatch(tr.scrollIntoView());
  }
  return true;
};

/** Position of the node of the given type that tr inserted at the selection of state (-1 if none). */
function inserted(tr: Transaction, state: EditorState, type: NodeType): number {
  const table = ancestor(state.selection.$from, (n) => n.type === nodes.table);
  const end = table ? table.pos + table.node.nodeSize : state.selection.to;
  const from = tr.mapping.map(state.selection.from, -1), to = tr.mapping.map(end, 1);
  let found = -1;
  tr.doc.nodesBetween(Math.max(0, from - 1), Math.min(tr.doc.content.size, to + 1), (n, pos) => {
    if (found < 0 && n.type === type) found = pos;
    return found < 0;
  });
  return found;
}

/**
 * Takes over a collapsed DOM selection the editor has not read yet (a key
 * pressed right after End/Home or a click: the selectionchange event comes
 * later, keyboard commands would act at the old position).
 */
function syncSelection(v: EditorView): void {
  if (v.composing) return;
  const sel = v.dom.ownerDocument.getSelection();
  if (!sel || !sel.isCollapsed || !sel.anchorNode || !v.dom.contains(sel.anchorNode)) return;
  let pos: number;
  try { pos = v.posAtDOM(sel.anchorNode, sel.anchorOffset); } catch { return; }
  const cur = v.state.selection;
  if (pos < 0 || pos > v.state.doc.content.size || (cur.empty && cur.from === pos) || cur instanceof NodeSelection) return;
  const $pos = v.state.doc.resolve(pos);
  if (!$pos.parent.inlineContent) return;
  v.dispatch(v.state.tr.setSelection(TextSelection.create(v.state.doc, pos)));
}

/** Markdown-looking plain text (pasted text is parsed as Markdown then). */
const MD_HINT = /(^|\n)(#{1,6} |[-*+] |\d+[.)] |> |```|~~~|\|.*\||:{2,}[a-z])|\*\*\S|__\S|\[[^\]\n]+\]\([^)\n]+\)|~~\S/;

/** Attributes of an inline macro: the parameter text becomes the label. */
function inlineAttrs(c: MacroChoice, inline: boolean): { params: Record<string, string>; label?: string } {
  if (!inline) return { params: c.params };
  const { text = '', ...params } = c.params;
  return { params, label: text };
}

// ---------------------------------------------------------------- node views

type GetPos = () => number | undefined;

interface Host {
  view: () => EditorView;
  edit: (pos: number) => void;
}

/** Stops the editor from handling events of controls inside node views. */
const isControl = (e: Event) => e.target instanceof Element && !!e.target.closest('.ewe-ctl');

function ctlButton(text: string, aria: string, onClick: () => void): HTMLButtonElement {
  const b = h('button', { type: 'button', class: 'ewe-ctl ewe-ctl-btn', 'aria-label': aria, title: aria }, text);
  b.addEventListener('mousedown', (e) => e.preventDefault());
  b.addEventListener('click', (e) => { e.preventDefault(); onClick(); });
  return b;
}

const macroNames = new Map<string, string>();
void loadMacros().then((ms) => { for (const m of ms) macroNames.set(m.name, macroLabel(m.labels) || m.name); }).catch(() => undefined);
const macroName = (name: string) => macroNames.get(name) ?? name;

function paramSummary(params: Record<string, string>): string {
  const s = Object.entries(params).map(([k, v]) => `${k}=${v}`).join(', ');
  return s.length > 80 ? s.slice(0, 79) + '…' : s;
}

class MacroView implements NodeView {
  dom: HTMLElement;
  contentDOM?: HTMLElement;
  private title: HTMLElement;
  private info: HTMLElement;
  constructor(private node: PMNode, host: Host, private getPos: GetPos) {
    this.title = h('span', { class: 'ewe-macro-name' });
    this.info = h('span', { class: 'ewe-macro-params' });
    const head = h('div', { class: 'ewe-macro-head ewe-ctl', contenteditable: 'false' },
      h('span', { 'aria-hidden': 'true' }, '⚙ '), this.title, ' ', this.info,
      ctlButton('✎', t('editor.macro_edit'), () => this.at((p) => host.edit(p))),
      ctlButton('×', t('editor.macro_remove'), () => this.at((p) => {
        const v = host.view();
        v.dispatch(v.state.tr.delete(p, p + this.node.nodeSize));
        v.focus();
        announce(t('editor.removed'));
      })));
    this.dom = h('div', { class: ['ewe-macro', node.attrs.leaf && 'ewe-macro--leaf'], role: 'group' }, head);
    if (!node.attrs.leaf) {
      this.contentDOM = h('div', { class: 'ewe-macro-body' });
      this.dom.append(this.contentDOM);
    }
    this.render();
  }
  private at(fn: (pos: number) => void) {
    const p = this.getPos();
    if (p !== undefined) fn(p);
  }
  private render() {
    const name = macroName(this.node.attrs.name as string);
    this.title.textContent = name;
    this.info.textContent = paramSummary(this.node.attrs.params as Record<string, string>);
    this.dom.setAttribute('aria-label', t('editor.macro_named', { name }));
    this.dom.dataset.macro = this.node.attrs.name as string;
  }
  update(node: PMNode): boolean {
    if (node.type !== this.node.type || node.attrs.leaf !== this.node.attrs.leaf) return false;
    this.node = node;
    this.render();
    return true;
  }
  stopEvent(e: Event): boolean { return isControl(e); }
  ignoreMutation(m: ViewMutationRecord): boolean {
    return m.type !== 'selection' && !(this.contentDOM && this.contentDOM.contains(m.target));
  }
}

type ViewMutationRecord = MutationRecord | { type: 'selection'; target: Node };

/** Inline atoms (inline macros, inline HTML) and HTML blocks: a chip with an edit button. */
class ChipView implements NodeView {
  dom: HTMLElement;
  private text: HTMLElement;
  constructor(private node: PMNode, host: Host, private getPos: GetPos) {
    const block = node.type === nodes.html_block;
    this.text = h(block ? 'pre' : 'span', { class: 'ewe-chip-text' });
    this.dom = h(block ? 'div' : 'span', { class: ['ewe-chip', block && 'ewe-chip--block', `ewe-${node.type.name}`], contenteditable: 'false' },
      this.text, ctlButton('✎', '', () => { const p = this.getPos(); if (p !== undefined) host.edit(p); }));
    this.render();
  }
  private render() {
    const a = this.node.attrs;
    let txt: string, aria: string;
    if (this.node.type === nodes.macro_inline) {
      const name = macroName(a.name as string);
      txt = `⚙ ${name}${a.label ? `: ${a.label as string}` : ''}`;
      aria = t('editor.macro_edit_named', { name });
      this.dom.dataset.macro = a.name as string;
    } else {
      txt = a.html as string;
      aria = t('editor.html_edit');
      this.dom.setAttribute('aria-label', t('editor.html'));
    }
    this.text.textContent = txt;
    const btn = this.dom.querySelector('button')!;
    btn.setAttribute('aria-label', aria);
    btn.title = aria;
  }
  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false;
    this.node = node;
    this.render();
    return true;
  }
  stopEvent(e: Event): boolean { return isControl(e); }
  ignoreMutation(): boolean { return true; }
}

/** List items with a checkbox (task lists). */
class TaskItemView implements NodeView {
  dom: HTMLElement;
  contentDOM: HTMLElement;
  private box: HTMLInputElement;
  constructor(private node: PMNode, view: EditorView, getPos: GetPos) {
    this.box = h('input', { type: 'checkbox', class: 'ewe-ctl', 'aria-label': t('editor.task_done') });
    this.box.addEventListener('change', () => {
      const p = getPos();
      if (p === undefined) return;
      view.dispatch(view.state.tr.setNodeMarkup(p, undefined, { ...this.node.attrs, checked: this.box.checked }));
    });
    this.contentDOM = h('div', { class: 'ewe-task-body' });
    this.dom = h('li', { class: 'ewe-task' }, h('span', { class: 'ewe-task-box', contenteditable: 'false' }, this.box), this.contentDOM);
    this.render();
  }
  private render() {
    this.box.checked = !!this.node.attrs.checked;
    this.dom.dataset.checked = String(!!this.node.attrs.checked);
  }
  update(node: PMNode): boolean {
    if (node.type !== this.node.type || node.attrs.checked === null) return false;
    this.node = node;
    this.render();
    return true;
  }
  stopEvent(e: Event): boolean { return isControl(e); }
  ignoreMutation(m: ViewMutationRecord): boolean { return m.type !== 'selection' && !this.contentDOM.contains(m.target); }
}

// ---------------------------------------------------------------- toolbar

interface Tool {
  id: string;
  icon: string;
  key: string;
  /** label (instead of the translation of key) */
  label?: string;
  shortcut?: string;
  run: (v: EditorView) => void;
  active?: (s: EditorState) => boolean;
  enabled?: (s: EditorState) => boolean;
  /** Shown only while this holds (table tools). */
  when?: (s: EditorState) => boolean;
  /** Available in Markdown mode, too. */
  markdown?: boolean;
}

const mod = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl';
const cmdTool = (c: Command) => (v: EditorView) => { c(v.state, v.dispatch, v); v.focus(); };

// ---------------------------------------------------------------- editor

/** Creates the editor inside container. */
export function createMarkdownEditor(container: HTMLElement, opts: MarkdownEditorOptions): MarkdownEditor {
  let mode: EditorMode = opts.mode ?? (() => { try { return localStorage.getItem(MODE_KEY) === 'markdown' ? 'markdown' : 'visual'; } catch { return 'visual'; } })();
  let escaped = false;
  const helpId = uid('ewe-help');

  // ------------------------------------------------ dialogs (need the view)

  const linkDialog = async (v: EditorView) => {
    const { state } = v;
    const { from, to, empty, $from } = state.selection;
    let range = { from, to };
    let href = '';
    const link = marks.link.isInSet($from.marks()) ?? (empty ? null : state.doc.rangeHasMark(from, to, marks.link) ? marks.link.create({ href: '' }) : null);
    if (link && empty) {
      // the whole link around the cursor
      const parent = $from.parent, start = $from.start();
      let s = $from.parentOffset, e = $from.parentOffset;
      parent.forEach((child, off) => {
        if (off <= $from.parentOffset && off + child.nodeSize >= $from.parentOffset && link.isInSet(child.marks)) { s = Math.min(s, off); e = Math.max(e, off + child.nodeSize); }
      });
      range = { from: start + s, to: start + e };
    }
    if (link) href = (link.attrs.href as string) || (state.doc.nodeAt(from)?.marks.find((m) => m.type === marks.link)?.attrs.href as string) || '';
    const url = textInput({ value: href, type: 'url', placeholder: 'https://' });
    const text = textInput({ value: '' });
    const fields = [field(t('editor.link_url'), url, { help: t('editor.link_help') })];
    if (range.from === range.to) fields.push(field(t('editor.link_text'), text));
    const res = await ask(t('editor.link'), fields, t('common.apply'), () => {
      const v2 = url.value.trim();
      if (v2 && !/^(https?:\/\/|mailto:|\/|#)/i.test(v2)) throw new Error(t('editor.link_invalid'));
      return { href: v2, text: text.value.trim() };
    });
    if (!res) return v.focus();
    let tr = v.state.tr;
    if (!res.href) tr = tr.removeMark(range.from, range.to, marks.link);
    else if (range.from === range.to) {
      const node = schema.text(res.text || res.href, [marks.link.create({ href: res.href })]);
      tr = tr.replaceSelectionWith(node, false);
    } else tr = tr.removeMark(range.from, range.to, marks.link).addMark(range.from, range.to, marks.link.create({ href: res.href }));
    v.dispatch(tr.scrollIntoView());
    v.focus();
  };

  const imageDialog = async (v: EditorView, pos?: number) => {
    const existing = pos === undefined ? null : v.state.doc.nodeAt(pos);
    let src = existing?.attrs.src as string | undefined;
    let alt0 = (existing?.attrs.alt as string | undefined) ?? '';
    if (!existing) {
      const m = await pickMedium();
      if (!m) return v.focus();
      if (!isImage(m)) {
        const name = m.title || m.name;
        v.dispatch(v.state.tr.replaceSelectionWith(schema.text(name, [marks.link.create({ href: `/api/v1/media/${m.id}/view` })]), false));
        return v.focus();
      }
      src = mediaURL(m);
      alt0 = m.alt;
    }
    const alt = textInput({ maxlength: 300, value: alt0 });
    const res = await ask(t('editor.image'), [field(t('editor.image_alt'), alt, { required: true, help: t('editor.image_alt_help') })], t('common.apply'), () => alt.value.trim());
    if (res === null) return v.focus();
    const tr = existing && pos !== undefined
      ? v.state.tr.setNodeMarkup(pos, undefined, { ...existing.attrs, alt: res })
      : v.state.tr.replaceSelectionWith(nodes.image.create({ src, alt: res }));
    v.dispatch(tr.scrollIntoView());
    v.focus();
  };

  const htmlDialog = async (v: EditorView, pos: number) => {
    const node = v.state.doc.nodeAt(pos);
    if (!node) return;
    const ta = textArea({ value: node.attrs.html as string, rows: 10 });
    ta.classList.add('ewe-mono');
    const res = await ask(t('editor.html_edit'), [h('p', { class: 'ew-help' }, t('editor.html_help')), field(t('editor.html'), ta)], t('common.apply'), () => ta.value);
    if (res === null) return v.focus();
    const tr = res.trim() === '' ? v.state.tr.delete(pos, pos + node.nodeSize) : v.state.tr.setNodeMarkup(pos, undefined, { html: res });
    v.dispatch(tr);
    v.focus();
  };

  const macroEdit = async (v: EditorView, pos: number) => {
    const node = v.state.doc.nodeAt(pos);
    if (!node) return;
    const inline = node.type === nodes.macro_inline;
    const params = node.attrs.params as Record<string, string>;
    try {
      const c = await macroDialog({ name: node.attrs.name as string, params: inline ? { ...params, text: node.attrs.label as string } : params });
      if (c) {
        const cur = v.state.doc.nodeAt(pos);
        if (cur && cur.type === node.type) v.dispatch(v.state.tr.setNodeMarkup(pos, undefined, { ...cur.attrs, ...inlineAttrs(c, inline) }));
      }
    } catch (e) {
      toast(String((e as Error).message ?? e), 'error');
    }
    v.focus();
  };

  /** Inserts a macro (wrapping the selected blocks for macros with a body). */
  const macroInsert = async (v: EditorView, only?: string) => {
    let c;
    const selected = mode === 'markdown'
      ? textarea.value.slice(textarea.selectionStart, textarea.selectionEnd)
      : v.state.doc.textBetween(v.state.selection.from, v.state.selection.to, ' ');
    try {
      c = await macroDialog(null, only, selected.trim() && !selected.includes('\n') ? { text: selected.trim() } : {});
    } catch (e) {
      toast(String((e as Error).message ?? e), 'error');
      return;
    }
    if (!c) return mode === 'visual' ? v.focus() : textarea.focus();
    if (mode === 'markdown') {
      const start = textarea.selectionStart, end = textarea.selectionEnd;
      const inner = textarea.value.slice(start, end).trim() || t('editor.macro_body');
      const head = c.name + formatAttrs(c.params);
      let snippet = c.hasBody ? `\n:::${head}\n${inner}\n:::\n` : `\n::${head}\n`;
      if (c.inline) {
        const { label, params } = inlineAttrs(c, true);
        snippet = `:${c.name}[${(label as string).replace(/\]/g, '\\]')}]${formatAttrs(params as Record<string, string>)}`;
      }
      textarea.setRangeText(snippet, start, end, 'end');
      textarea.focus();
      opts.onChange?.();
      return;
    }
    const { state } = v;
    if (c.inline && state.selection.$from.parent.inlineContent) {
      v.dispatch(state.tr.replaceSelectionWith(nodes.macro_inline.create({ name: c.name, ...inlineAttrs(c, true) }), false).scrollIntoView());
      v.focus();
      return;
    }
    const attrs = { name: c.name, params: c.params, leaf: !c.hasBody };
    let tr: Transaction;
    const range = c.hasBody && !state.selection.empty ? state.selection.$from.blockRange(state.selection.$to) : null;
    const wrapping = range && findWrapping(range, nodes.macro, attrs);
    if (range && wrapping) {
      tr = state.tr.wrap(range, wrapping);
    } else {
      const node = c.hasBody ? nodes.macro.create(attrs, nodes.paragraph.create(null, schema.text(t('editor.macro_body')))) : nodes.macro.create(attrs);
      tr = insertBlock(state, node);
      // select the placeholder text of the body (or put the cursor after a leaf macro)
      const at = inserted(tr, state, nodes.macro);
      if (at >= 0 && c.hasBody) tr.setSelection(TextSelection.create(tr.doc, at + 2, at + 2 + t('editor.macro_body').length));
      else if (at >= 0) tr.setSelection(TextSelection.near(tr.doc.resolve(at + node.nodeSize)));
    }
    v.dispatch(tr.scrollIntoView());
    v.focus();
  };

  /** Opens the edit dialog of the node at pos (macro, image, HTML). */
  const editAt = (v: EditorView, pos: number): boolean => {
    const n = v.state.doc.nodeAt(pos);
    if (!n) return false;
    if (n.type === nodes.macro || n.type === nodes.macro_inline) void macroEdit(v, pos);
    else if (n.type === nodes.image) void imageDialog(v, pos);
    else if (n.type === nodes.html_block || n.type === nodes.html_inline) void htmlDialog(v, pos);
    else return false;
    return true;
  };

  /** Enter on a selected macro/image/HTML node opens its dialog. */
  const editSelected: Command = (state, _d, v) => {
    const sel = state.selection;
    if (!(sel instanceof NodeSelection) || !v) return false;
    if (sel.node.type === nodes.macro && !sel.node.attrs.leaf) return false;
    return editAt(v, sel.from);
  };

  // ------------------------------------------------ live editing (undo is per user then)
  let live: Collab | null = null;
  let liveOn = false;
  const doUndo: Command = (s, d) => (liveOn ? yUndo(s) : undo(s, d));
  const doRedo: Command = (s, d) => (liveOn ? yRedo(s) : redo(s, d));
  const canUndo = (s: EditorState) => (liveOn ? (yUndoPluginKey.getState(s)?.undoManager?.undoStack.length ?? 0) > 0 : undo(s));
  const canRedo = (s: EditorState) => (liveOn ? (yUndoPluginKey.getState(s)?.undoManager?.redoStack.length ?? 0) > 0 : redo(s));

  // ------------------------------------------------ toolbar definition

  const tools: (Tool | '|')[] = [
    { id: 'strong', icon: 'B', key: 'editor.bold', shortcut: `${mod}+B`, run: cmdTool(toggleMark(marks.strong)), active: (s) => markActive(s, marks.strong) },
    { id: 'em', icon: 'I', key: 'editor.italic', shortcut: `${mod}+I`, run: cmdTool(toggleMark(marks.em)), active: (s) => markActive(s, marks.em) },
    { id: 'strike', icon: 'S', key: 'editor.strike', shortcut: `${mod}+Shift+X`, run: cmdTool(toggleMark(marks.strike)), active: (s) => markActive(s, marks.strike) },
    { id: 'code', icon: '`', key: 'editor.code', shortcut: `${mod}+E`, run: cmdTool(toggleMark(marks.code)), active: (s) => markActive(s, marks.code) },
    { id: 'link', icon: '🔗', key: 'editor.link', shortcut: `${mod}+K`, run: (v) => void linkDialog(v), active: (s) => markActive(s, marks.link) },
    '|',
    { id: 'bullet', icon: '•', key: 'editor.bullet', shortcut: `${mod}+Shift+8`, run: cmdTool(toggleList(nodes.bullet_list)), active: (s) => { const l = ancestor(s.selection.$from, isList); return !!l && l.node.type === nodes.bullet_list && l.node.firstChild?.attrs.checked === null; } },
    { id: 'ordered', icon: '1.', key: 'editor.ordered', shortcut: `${mod}+Shift+7`, run: cmdTool(toggleList(nodes.ordered_list)), active: (s) => ancestor(s.selection.$from, isList)?.node.type === nodes.ordered_list },
    { id: 'task', icon: '☑', key: 'editor.tasks', shortcut: `${mod}+Shift+9`, run: cmdTool(toggleList(nodes.bullet_list, true)), active: (s) => { const l = ancestor(s.selection.$from, isList); return !!l && l.node.firstChild?.attrs.checked !== null; } },
    { id: 'quote', icon: '❝', key: 'editor.quote', shortcut: `${mod}+Shift+B`, run: cmdTool(toggleWrap(nodes.blockquote)), active: (s) => !!ancestor(s.selection.$from, (n) => n.type === nodes.blockquote) },
    '|',
    { id: 'table', icon: '▦', key: 'editor.table', run: cmdTool(insertTable), enabled: (s) => insertTable(s) },
    { id: 'image', icon: '🖼', key: 'editor.image', run: (v) => void imageDialog(v) },
    { id: 'hr', icon: '—', key: 'editor.hr', run: cmdTool(insertHr) },
    { id: 'macro', icon: '⚙', key: 'editor.macro_insert', run: (v) => void macroInsert(v), markdown: true },
    '|',
    { id: 'row_before', icon: '⤒', key: 'editor.row_before', run: cmdTool(addRowBefore), when: isInTable },
    { id: 'row_after', icon: '⤓', key: 'editor.row_after', run: cmdTool(addRowAfter), when: isInTable },
    { id: 'col_before', icon: '⇤', key: 'editor.col_before', run: cmdTool(addColumnBefore), when: isInTable },
    { id: 'col_after', icon: '⇥', key: 'editor.col_after', run: cmdTool(addColumnAfter), when: isInTable },
    { id: 'row_delete', icon: '⊟', key: 'editor.row_delete', run: cmdTool(deleteRow), when: isInTable },
    { id: 'col_delete', icon: '⊠', key: 'editor.col_delete', run: cmdTool(deleteColumn), when: isInTable },
    { id: 'table_delete', icon: '✖', key: 'editor.table_delete', run: cmdTool(deleteTable), when: isInTable },
    '|',
    { id: 'undo', icon: '↶', key: 'editor.undo', shortcut: `${mod}+Z`, run: cmdTool(doUndo), enabled: canUndo },
    { id: 'redo', icon: '↷', key: 'editor.redo', shortcut: `${mod}+Shift+Z`, run: cmdTool(doRedo), enabled: canRedo },
  ];

  // block type select (paragraph, headings, code block)
  const blockSel = select([
    { value: '', label: '–' },
    { value: 'p', label: t('editor.paragraph') },
    ...[1, 2, 3, 4].map((l) => ({ value: `h${l}`, label: t('editor.heading', { level: l }) })),
    { value: 'code', label: t('editor.code_block') },
  ]);
  blockSel.classList.add('ewe-block');
  blockSel.setAttribute('aria-label', t('editor.block_type'));
  (blockSel.options[0] as HTMLOptionElement).disabled = true;
  blockSel.addEventListener('change', () => {
    const val = blockSel.value;
    const c = val === 'p' ? setBlockType(nodes.paragraph) : val === 'code' ? setBlockType(nodes.code_block) : setBlockType(nodes.heading, { level: Number(val.slice(1)) });
    c(view.state, view.dispatch, view);
    view.focus();
  });

  const modeBtn = h('button', { type: 'button', class: 'ewe-tool ewe-mode', 'aria-pressed': 'false' }, t('editor.markdown_mode'));
  modeBtn.addEventListener('click', () => setMode(mode === 'visual' ? 'markdown' : 'visual'));

  const toolbar = h('div', { class: 'ewe-toolbar', role: 'toolbar', 'aria-label': t('editor.toolbar') });
  const buttons: { tool: Tool; btn: HTMLButtonElement }[] = [];
  toolbar.append(blockSel);
  function makeButton(tool: Tool): HTMLButtonElement {
    const text = tool.label ?? t(tool.key);
    // WCAG 2.5.3 (label in name): visible letters must be part of the accessible name
    const name = /[A-Za-z0-9]/.test(tool.icon) && (tool.icon.length > 1 || !text.toLowerCase().includes(tool.icon.toLowerCase())) ? `${tool.icon} ${text}` : text;
    const tip = tool.shortcut ? `${name} (${tool.shortcut})` : name;
    const btn = h('button', { type: 'button', class: 'ewe-tool', 'aria-label': name, title: tip, 'aria-keyshortcuts': tool.shortcut ? tool.shortcut.replace('⌘', 'Meta').replace('Ctrl', 'Control') : undefined, 'aria-pressed': tool.active ? 'false' : undefined },
      h('span', { 'aria-hidden': 'true' }, tool.icon));
    btn.addEventListener('mousedown', (e) => e.preventDefault()); // keep the selection
    btn.addEventListener('click', () => tool.run(view));
    buttons.push({ tool, btn });
    return btn;
  }
  for (const tool of tools) {
    if (tool === '|') { toolbar.append(h('span', { class: 'ewe-sep', 'aria-hidden': 'true' })); continue; }
    toolbar.append(makeButton(tool));
  }
  toolbar.append(h('span', { class: 'ewe-spacer' }), modeBtn);

  // own buttons of macros (plugins declare them), after the macro button
  void loadMacros().then((ms) => {
    let after = buttons.find((b) => b.tool.id === 'macro')?.btn;
    for (const m of ms.filter((x) => x.toolbar)) {
      const btn = makeButton({ id: `macro:${m.name}`, icon: m.toolbar!, key: '', label: macroLabel(m.labels) || m.name, run: (v) => void macroInsert(v, m.name), markdown: true });
      after?.after(btn);
      after = btn;
    }
    updateToolbar(view.state);
  }).catch(() => undefined);

  // roving tab index: one tab stop, arrow keys move inside the toolbar
  const controls = () => Array.from(toolbar.querySelectorAll<HTMLElement>('button, select')).filter((b) => !b.hidden && !(b as HTMLButtonElement).disabled);
  let current: HTMLElement = blockSel;
  const rove = () => {
    const list = controls();
    if (!list.includes(current)) current = list[0];
    for (const c of list) c.tabIndex = c === current ? 0 : -1;
  };
  toolbar.addEventListener('focusin', (e) => { if (e.target instanceof HTMLElement && e.target !== toolbar) { current = e.target; rove(); } });
  toolbar.addEventListener('keydown', (e) => {
    const list = controls();
    const i = list.indexOf(document.activeElement as HTMLElement);
    let next = -1;
    if (e.key === 'ArrowRight') next = (i + 1) % list.length;
    else if (e.key === 'ArrowLeft') next = (i - 1 + list.length) % list.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = list.length - 1;
    else if (e.key === 'Escape') { e.preventDefault(); focus(); return; }
    if (next < 0 || (document.activeElement instanceof HTMLSelectElement && (e.key === 'Home' || e.key === 'End'))) return;
    e.preventDefault();
    list[next].focus();
  });

  const updateToolbar = (s: EditorState) => {
    const md = mode === 'markdown';
    for (const { tool, btn } of buttons) {
      btn.hidden = (!!tool.when && (md || !tool.when(s)));
      btn.disabled = md ? !tool.markdown : (tool.enabled ? !tool.enabled(s) : false);
      if (tool.active) btn.setAttribute('aria-pressed', String(!md && tool.active(s)));
    }
    for (const sep of Array.from(toolbar.querySelectorAll<HTMLElement>('.ewe-sep'))) {
      // hide separators next to hidden groups
      let prev = sep.previousElementSibling as HTMLElement | null;
      while (prev && prev.hidden) prev = prev.previousElementSibling as HTMLElement | null;
      sep.hidden = !prev || prev.classList.contains('ewe-sep');
    }
    blockSel.disabled = md;
    modeBtn.disabled = liveOn && livePeers.length > 0;
    modeBtn.title = modeBtn.disabled ? t('editor.markdown_shared') : '';
    const p = s.selection.$from.parent;
    blockSel.value = p.type === nodes.paragraph ? 'p' : p.type === nodes.heading && (p.attrs.level as number) <= 4 ? `h${p.attrs.level as number}` : p.type === nodes.code_block ? 'code' : '';
    rove();
  };

  // ------------------------------------------------ keymap and plugins

  const tab = (c: Command): Command => (s, d, v) => !escaped && c(s, d, v);
  const keys: Record<string, Command> = {
    'Mod-z': doUndo, 'Shift-Mod-z': doRedo, 'Mod-y': doRedo, Backspace: chainCommands(undoInputRule, joinBackward, selectNodeBackward),
    'Mod-b': toggleMark(marks.strong), 'Mod-i': toggleMark(marks.em), 'Mod-Shift-x': toggleMark(marks.strike), 'Mod-e': toggleMark(marks.code),
    'Mod-k': (_s, _d, v) => { if (v) void linkDialog(v); return true; },
    'Mod-Shift-8': toggleList(nodes.bullet_list), 'Mod-Shift-7': toggleList(nodes.ordered_list), 'Mod-Shift-9': toggleList(nodes.bullet_list, true),
    'Mod-Shift-b': toggleWrap(nodes.blockquote),
    'Mod-Alt-0': setBlockType(nodes.paragraph), 'Mod-Alt-1': toggleBlock(nodes.heading, { level: 1 }), 'Mod-Alt-2': toggleBlock(nodes.heading, { level: 2 }),
    'Mod-Alt-3': toggleBlock(nodes.heading, { level: 3 }), 'Mod-Alt-4': toggleBlock(nodes.heading, { level: 4 }), 'Mod-Alt-c': toggleBlock(nodes.code_block),
    'Mod-Enter': chainCommands(toggleTask, exitCode),
    'Shift-Enter': chainCommands(exitCode, (s, d) => { d?.(s.tr.replaceSelectionWith(nodes.hard_break.create()).scrollIntoView()); return true; }),
    Enter: chainCommands(editSelected, splitItem, liftEmptyItem),
    Tab: tab(chainCommands(goToNextCell(1), sinkListItem(nodes.list_item))),
    'Shift-Tab': tab(chainCommands(goToNextCell(-1), liftListItem(nodes.list_item))),
    'Alt-F10': () => { (controls().find((c) => c.tabIndex === 0) ?? controls()[0])?.focus(); return true; },
  };

  const rules = inputRules({ rules: [
    textblockTypeInputRule(/^(#{1,6})\s$/, nodes.heading, (m) => ({ level: m[1].length })),
    wrappingInputRule(/^\s*>\s$/, nodes.blockquote),
    wrappingInputRule(/^\s*([-+*])\s$/, nodes.bullet_list),
    wrappingInputRule(/^(\d+)\.\s$/, nodes.ordered_list, (m) => ({ order: Number(m[1]) }), (m, n) => n.childCount + (n.attrs.order as number) === Number(m[1])),
    textblockTypeInputRule(/^```([\w-]*)\s$/, nodes.code_block, (m) => ({ params: m[1] })),
    new InputRule(/^---$/, (state, _m, start, end) => {
      const $s = state.doc.resolve(start);
      if ($s.parent.type !== nodes.paragraph || $s.parent.textContent !== '--') return null;
      return state.tr.replaceRangeWith($s.before(), $s.after(), nodes.horizontal_rule.create());
    }),
    // "[ ] " / "[x] " at the start of a list item makes it a task
    new InputRule(/^\[([ xX])\]\s$/, (state, m, start, end) => {
      const item = ancestor(state.doc.resolve(start), (n) => n.type === nodes.list_item);
      if (!item) return null;
      return state.tr.delete(start, end).setNodeMarkup(item.pos, undefined, { ...item.node.attrs, checked: m[1] !== ' ' });
    }),
    markRule(/(\*\*)([^*\s](?:[^*]*[^*\s])?)(\*\*)$/, marks.strong),
    markRule(/(?<![*\w])(\*)([^*\s](?:[^*]*[^*\s])?)(\*)$/, marks.em),
    markRule(/(?<![_\w])(_)([^_\s](?:[^_]*[^_\s])?)(_)$/, marks.em),
    markRule(/(~~)([^~\s](?:[^~]*[^~\s])?)(~~)$/, marks.strike),
    markRule(/(`)([^`]+)(`)$/, marks.code),
  ] });

  const escapePlugin = new Plugin({
    props: {
      handleKeyDown(v, e) {
        if (e.key === 'Escape') escaped = true;
        else if (e.key !== 'Tab' && e.key !== 'Shift') escaped = false;
        syncSelection(v);
        return false;
      },
    },
  });

  const host: Host = { view: () => view, edit: (pos) => { editAt(view, pos); } };

  const plugins = [
    escapePlugin, rules, keymap(keys), keymap(baseKeymap), history(), dropCursor(), gapCursor(), tableEditing(),
  ];
  const livePlugins = (c: Collab) => [
    escapePlugin, rules, keymap(keys), keymap(baseKeymap),
    ySyncPlugin(c.fragment), yCursorPlugin(c.awareness, { cursorBuilder, selectionBuilder }), yUndoPlugin(),
    dropCursor(), gapCursor(), tableEditing(),
  ];

  const docOf = (src: string) => {
    const d = parse(src);
    return d.content.size ? d : schema.node('doc', null, [nodes.paragraph.create()]);
  };

  // ------------------------------------------------ DOM

  const surface = h('div', { class: 'ewe-surface' });
  const textarea = h('textarea', { class: 'ew-input ewe-markdown', rows: 20, 'aria-labelledby': opts.labelId, 'aria-describedby': helpId, lang: opts.lang, spellcheck: 'true', hidden: true });
  textarea.addEventListener('input', () => opts.onChange?.());
  const help = h('p', { class: 'ew-help ewe-help', id: helpId });
  const liveBox = h('p', { class: 'ewe-live', hidden: true });
  const root = h('div', { class: 'ewe-editor' }, toolbar, liveBox, surface, textarea, help);
  container.append(root);

  const view: EditorView = new EditorView(surface, {
    state: EditorState.create({ doc: docOf(opts.markdown), plugins }),
    attributes: {
      class: 'ew-content ewe-prose', role: 'textbox', 'aria-multiline': 'true', 'aria-labelledby': opts.labelId,
      'aria-describedby': helpId, lang: opts.lang, spellcheck: 'true',
    },
    nodeViews: {
      macro: (n, _v, g) => new MacroView(n, host, g),
      macro_inline: (n, _v, g) => new ChipView(n, host, g),
      html_block: (n, _v, g) => new ChipView(n, host, g),
      html_inline: (n, _v, g) => new ChipView(n, host, g),
      list_item: (n, v, g) => (n.attrs.checked === null ? defaultItem() : new TaskItemView(n, v, g)),
    },
    handleDoubleClickOn: (v, pos, node, nodePos, _e, direct) => direct && editAt(v, nodePos),
    clipboardTextParser: (text, $ctx) => {
      if ($ctx.parent.type.spec.code || !MD_HINT.test(text)) {
        // plain text: one paragraph per line block
        const paras = text.replace(/\r\n?/g, '\n').split(/\n{2,}/).map((p) => nodes.paragraph.create(null, p ? schema.text(p) : undefined));
        if ($ctx.parent.type.spec.code) return new Slice(Fragment.from(schema.text(text)), 0, 0);
        return Slice.maxOpen(Fragment.from(paras));
      }
      return Slice.maxOpen(parse(text).content);
    },
    dispatchTransaction(tr) {
      const s = view.state.apply(tr);
      view.updateState(s);
      updateToolbar(s);
      if (tr.docChanged) opts.onChange?.();
    },
  });

  function defaultItem(): NodeView {
    const dom = document.createElement('li');
    return { dom, contentDOM: dom, update: (m: PMNode) => m.type === nodes.list_item && m.attrs.checked === null };
  }

  // ------------------------------------------------ live editing

  let livePeers: Peer[] = [];
  function showLive() {
    if (!liveOn) return;
    liveBox.hidden = false;
    liveBox.replaceChildren(h('span', { class: 'ewe-live-dot', 'aria-hidden': 'true' }),
      livePeers.length ? t('editor.live_with', { names: livePeers.map((p) => p.name).join(', ') }) : t('editor.live_alone'));
  }
  if (opts.collab) {
    const c = new Collab(collabURL(opts.collab.elementId, opts.lang), {
      onPeers: (peers) => {
        const before = new Set(livePeers.map((p) => p.id));
        const now = new Set(peers.map((p) => p.id));
        for (const p of peers) if (!before.has(p.id)) announce(t('editor.live_joined', { name: p.name }));
        for (const p of livePeers) if (!now.has(p.id)) announce(t('editor.live_left', { name: p.name }));
        livePeers = peers;
        showLive();
        // the Markdown source is not shared: back to the visual editor
        if (peers.length && mode === 'markdown') setMode('visual');
        updateToolbar(view.state);
      },
      onSaved: (rev, by) => opts.collab?.onSaved?.(rev, by),
      onClosed: () => {
        // keep editing alone with the current content
        liveOn = false;
        livePeers = [];
        live = null;
        liveBox.textContent = t('editor.live_lost');
        liveBox.hidden = false;
        view.updateState(EditorState.create({ doc: view.state.doc, plugins }));
        updateToolbar(view.state);
      },
    });
    live = c;
    liveBox.textContent = t('editor.live_connecting');
    liveBox.hidden = false;
    // while live editing connects, typing goes into the local document (it
    // becomes the shared one if this editor is the first)
    const initial = view.state.doc;
    c.ready.then((start) => {
      const first = start === 'first' || c.fragment.length === 0;
      if (first) prosemirrorToYXmlFragment(view.state.doc, c.fragment);
      else if (!view.state.doc.eq(initial)) toast(t('editor.live_replaced'), 'error');
      liveOn = true;
      view.updateState(EditorState.create({ schema, plugins: livePlugins(c) }));
      updateToolbar(view.state);
      // the shared document is drawn by the sync plugin after this tick
      window.setTimeout(() => { showLive(); updateToolbar(view.state); }, 50);
    }).catch(() => {
      // no live editing (not reachable, too many editors): edit alone
      live = null;
      liveBox.textContent = t('editor.live_unavailable');
    });
  }

  // ------------------------------------------------ modes

  function setMode(m: EditorMode) {
    if (m === mode) return;
    if (m === 'markdown' && liveOn && livePeers.length) return; // shared: visual only
    if (m === 'markdown') {
      textarea.value = serialize(view.state.doc);
    } else {
      const doc = docOf(textarea.value);
      view.dispatch(view.state.tr.replaceWith(0, view.state.doc.content.size, doc.content));
    }
    mode = m;
    try { localStorage.setItem(MODE_KEY, m); } catch { /* storage unavailable */ }
    surface.hidden = m === 'markdown';
    textarea.hidden = m !== 'markdown';
    modeBtn.setAttribute('aria-pressed', String(m === 'markdown'));
    help.textContent = m === 'markdown' ? t('editor.help_markdown') : t('editor.help_visual');
    updateToolbar(view.state);
    announce(m === 'markdown' ? t('editor.mode_markdown') : t('editor.mode_visual'));
    focus();
  }

  function focus() {
    if (mode === 'markdown') textarea.focus();
    else view.focus();
  }

  // initial mode without announcement
  const initial = mode;
  mode = 'visual';
  help.textContent = t('editor.help_visual');
  updateToolbar(view.state);
  if (initial === 'markdown') {
    textarea.value = serialize(view.state.doc);
    mode = 'markdown';
    surface.hidden = true;
    textarea.hidden = false;
    modeBtn.setAttribute('aria-pressed', 'true');
    help.textContent = t('editor.help_markdown');
    updateToolbar(view.state);
  }

  return {
    view,
    getMarkdown: () => (mode === 'markdown' ? textarea.value.replace(/\r\n?/g, '\n') : serialize(view.state.doc)),
    setMarkdown: (src: string) => {
      if (mode === 'markdown') textarea.value = src;
      else view.dispatch(view.state.tr.replaceWith(0, view.state.doc.content.size, docOf(src).content));
    },
    getMode: () => mode,
    setMode,
    focus,
    saved: (rev: number) => { if (liveOn && opts.collab) live?.saved(rev, opts.collab.userId); },
    live: () => ({ on: liveOn, leader: liveOn && !!live?.leader, peers: livePeers }),
    // the view first: its plugins stop listening to the shared document
    destroy: () => { view.destroy(); live?.destroy(); root.remove(); },
  };
}

/** The cursor of another editor: a colored line with the name. */
function cursorBuilder(user: { id?: number; name?: string }): HTMLElement {
  const color = colorOf(user.id ?? 0);
  const label = h('span', { class: 'ewe-cursor-label' }, user.name ?? '');
  label.style.backgroundColor = color;
  const el = h('span', { class: 'ewe-cursor', 'aria-hidden': 'true' }, '\u2060', label, '\u2060');
  el.style.borderColor = color;
  return el;
}

function selectionBuilder(user: { id?: number }): { style: string; class: string } {
  return { style: `background-color: ${colorOf(user.id ?? 0)}33`, class: 'ewe-remote-selection' };
}

/**
 * An input rule that applies a mark: re must match (open)(inner)(close) at
 * the end; the last character of close is the one being typed.
 */
function markRule(re: RegExp, type: MarkType): InputRule {
  return new InputRule(re, (state, m, start, end) => {
    const [, open, inner] = m;
    const $s = state.doc.resolve(start);
    if ($s.parent.type.spec.code || (type !== marks.code && state.doc.rangeHasMark(start, end, marks.code))) return null;
    const innerStart = start + open.length, innerEnd = innerStart + inner.length;
    const tr = state.tr.delete(innerEnd, end).delete(start, innerStart);
    return tr.addMark(start, start + inner.length, type.create()).removeStoredMark(type);
  });
}
