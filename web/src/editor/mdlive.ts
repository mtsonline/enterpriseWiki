// Live editing of the Markdown source: binds the textarea to a shared
// Y.Text and shows the cursors of the others over the textarea (a mirror
// element with the same text layout; the text itself stays in the textarea,
// so screen readers and the keyboard work as usual).
import type { Awareness } from 'y-protocols/awareness';
import * as Y from 'yjs';
import { h } from '../core/dom';
import { markerOf } from './collab';

type RelJSON = Record<string, unknown>;

/** Moves index i through a change (delta of a Y.Text event). */
function mapIndex(i: number, delta: { retain?: number; insert?: unknown; delete?: number }[]): number {
  let pos = 0;
  let out = i;
  for (const d of delta) {
    if (d.retain) pos += d.retain;
    else if (typeof d.insert === 'string') {
      if (pos < i) out += d.insert.length;
      pos += d.insert.length;
    } else if (d.delete) {
      if (pos < i) out -= Math.min(d.delete, i - pos);
    }
  }
  return Math.max(0, out);
}

export class TextareaBinding {
  private last: string;
  private readonly mirror: HTMLElement;
  private frame = 0;

  constructor(private ta: HTMLTextAreaElement, private text: Y.Text, private awareness: Awareness, private onChange: () => void) {
    this.mirror = h('div', { class: 'ewe-md-mirror', 'aria-hidden': 'true' });
    ta.before(this.mirror);
    this.last = text.toString();
    ta.value = this.last;
    ta.addEventListener('input', this.onInput);
    for (const ev of ['select', 'keyup', 'mouseup', 'focus']) ta.addEventListener(ev, this.onSelect);
    ta.addEventListener('scroll', this.schedule);
    text.observe(this.onText);
    awareness.on('change', this.schedule);
    window.addEventListener('resize', this.schedule);
    this.onSelect();
  }

  private onInput = () => {
    const v = this.ta.value.replace(/\r\n?/g, '\n');
    const o = this.last;
    let s = 0;
    while (s < o.length && s < v.length && o[s] === v[s]) s++;
    let e = 0;
    while (e < o.length - s && e < v.length - s && o[o.length - 1 - e] === v[v.length - 1 - e]) e++;
    this.text.doc!.transact(() => {
      if (o.length - s - e > 0) this.text.delete(s, o.length - s - e);
      if (v.length - s - e > 0) this.text.insert(s, v.slice(s, v.length - e));
    }, this);
    this.last = v;
    this.onSelect();
  };

  /** Programmatic changes of the textarea (e.g. inserting a macro). */
  changed(): void { this.onInput(); }

  private onText = (ev: Y.YTextEvent, tr: Y.Transaction) => {
    if (tr.origin === this) return;
    const focused = document.activeElement === this.ta;
    const a = mapIndex(this.ta.selectionStart, ev.delta), b = mapIndex(this.ta.selectionEnd, ev.delta);
    const top = this.ta.scrollTop;
    this.last = this.text.toString();
    this.ta.value = this.last;
    if (focused) this.ta.setSelectionRange(a, b);
    this.ta.scrollTop = top;
    this.onChange();
    this.schedule();
  };

  private onSelect = () => {
    const rel = (i: number): RelJSON => Y.relativePositionToJSON(Y.createRelativePositionFromTypeIndex(this.text, i)) as RelJSON;
    this.awareness.setLocalStateField('md', { a: rel(this.ta.selectionStart), h: rel(this.ta.selectionEnd) });
  };

  private schedule = () => {
    cancelAnimationFrame(this.frame);
    this.frame = requestAnimationFrame(() => this.render());
  };

  /** Draws the cursors of the others at their place in the text. */
  private render(): void {
    if (this.ta.offsetParent === null) return; // hidden
    const cs = getComputedStyle(this.ta);
    const m = this.mirror.style;
    for (const p of ['fontFamily', 'fontSize', 'fontWeight', 'lineHeight', 'letterSpacing', 'tabSize', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft',
      'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth', 'boxSizing', 'textIndent', 'wordSpacing'] as const) {
      (m as unknown as Record<string, string>)[p] = cs[p];
    }
    m.top = `${this.ta.offsetTop}px`;
    m.left = `${this.ta.offsetLeft}px`;
    m.width = `${this.ta.offsetWidth}px`;
    m.height = `${this.ta.offsetHeight}px`;
    // the scroll bar of the textarea narrows its text
    const scrollbar = this.ta.offsetWidth - this.ta.clientWidth - parseFloat(cs.borderLeftWidth) - parseFloat(cs.borderRightWidth);
    m.paddingRight = `${parseFloat(cs.paddingRight) + Math.max(0, scrollbar)}px`;

    const doc = this.text.doc!;
    const cursors: { at: number; id: number; name: string }[] = [];
    this.awareness.getStates().forEach((st, client) => {
      if (client === doc.clientID || !st.md || !st.user) return;
      const pos = Y.createAbsolutePositionFromRelativePosition(Y.createRelativePositionFromJSON((st.md as { h: RelJSON }).h), doc);
      if (pos && pos.type === this.text) cursors.push({ at: pos.index, id: (st.user as { id: number }).id, name: (st.user as { name: string }).name });
    });
    cursors.sort((x, y) => x.at - y.at);
    const text = this.last;
    const parts: (string | HTMLElement)[] = [];
    let i = 0;
    for (const c of cursors) {
      parts.push(text.slice(i, c.at));
      i = c.at;
      const mk = markerOf(c.id);
      const label = h('span', { class: 'ewe-cursor-label' }, `${mk.shape} ${c.name}`);
      label.style.backgroundColor = mk.color;
      const caret = h('span', { class: 'ewe-cursor ewe-md-caret' }, label);
      caret.style.borderColor = mk.color;
      parts.push(caret);
    }
    parts.push(text.slice(i) + '\n');
    this.mirror.replaceChildren(...parts);
    this.mirror.scrollTop = this.ta.scrollTop;
  }

  destroy(): void {
    this.ta.removeEventListener('input', this.onInput);
    for (const ev of ['select', 'keyup', 'mouseup', 'focus']) this.ta.removeEventListener(ev, this.onSelect);
    this.ta.removeEventListener('scroll', this.schedule);
    this.text.unobserve(this.onText);
    this.awareness.off('change', this.schedule);
    window.removeEventListener('resize', this.schedule);
    cancelAnimationFrame(this.frame);
    this.awareness.setLocalStateField('md', null);
    this.mirror.remove();
  }
}
