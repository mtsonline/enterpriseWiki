// The hint on long sentences and words in the visual editor: marks as
// decorations of ProseMirror (drawn over the text, never part of the
// document or the saved Markdown) and the places for the list of the hint.
import { Plugin, PluginKey, TextSelection, type EditorState } from 'prosemirror-state';
import { Decoration, DecorationSet, type EditorView } from 'prosemirror-view';
import type { Node as PMNode } from 'prosemirror-model';
import { check, type Rules } from '../core/readability';
import { debounce, excerpt, type HintItem, type ReadabilityHint } from '../components/readability';

const key = new PluginKey<DecorationSet>('ewiki-readability');

interface Found { from: number; to: number; kind: HintItem['kind']; count: number; text: string }

/** The long sentences and words of a document (code blocks and inline code left out). */
export function findInDoc(doc: PMNode, rules: Rules, max: number): { found: Found[]; truncated: boolean } {
  const found: Found[] = [];
  let budget = max;
  let truncated = false;
  doc.descendants((node, pos) => {
    if (node.type.spec.code || node.isAtom) return false;
    if (!node.isTextblock) return true;
    if (budget <= 0) { truncated = truncated || node.textContent.trim() !== ''; return false; }
    // the text of the block and the place in the document of each character
    let text = '';
    const at: number[] = [];
    node.forEach((child, offset) => {
      const p = pos + 1 + offset;
      if (child.isText) {
        const s = child.text ?? '';
        const code = child.marks.some((m) => m.type.spec.code || m.type.name === 'code');
        if (code) { text += '`'; at.push(p); }
        for (let i = 0; i < s.length; i++) { text += code && s[i] === '`' ? "'" : s[i]; at.push(p + i); }
        if (code) { text += '`'; at.push(p + Math.max(0, s.length - 1)); }
      } else if (child.type.name === 'hard_break') {
        text += '\n'; at.push(p);
      } else {
        text += ' X '; at.push(p, p, p); // an image or an inline block: one word
      }
    });
    if (text.length > budget) { text = text.slice(0, budget); truncated = true; }
    budget -= text.length;
    for (const f of check(text, rules)) {
      found.push({ from: at[f.start], to: at[f.end - 1] + 1, kind: f.kind, count: f.count, text: text.slice(f.start, f.end) });
    }
    return false;
  });
  return { found, truncated };
}

function decorations(doc: PMNode, found: Found[]): DecorationSet {
  return DecorationSet.create(doc, found.map((f) => Decoration.inline(f.from, f.to, { class: f.kind === 'sentence' ? 'ewe-rh-sentence' : 'ewe-rh-word' })));
}

/**
 * The plugin: marks the places when the editor starts and after every
 * pause in typing; passes them to the hint (shown) while show() is true.
 */
export function readabilityPlugin(rules: Rules, max: number, hint: ReadabilityHint, show: () => boolean): Plugin {
  let view: EditorView | null = null;
  const items = (found: Found[]): HintItem[] => found.map((f) => ({
    kind: f.kind, count: f.count, excerpt: excerpt(f.text),
    select: () => {
      const v = view;
      if (!v) return;
      const size = v.state.doc.content.size;
      const from = Math.min(f.from, size), to = Math.min(f.to, size);
      try {
        v.dispatch(v.state.tr.setSelection(TextSelection.create(v.state.doc, from, to)).scrollIntoView());
      } catch { /* the place changed meanwhile */ }
      v.focus();
    },
  }));
  const compute = (st: EditorState): DecorationSet => {
    const r = findInDoc(st.doc, rules, max);
    if (show()) hint.update(items(r.found), r.truncated);
    return decorations(st.doc, r.found);
  };
  const later = debounce(() => {
    const v = view;
    if (!v || v.isDestroyed) return;
    v.dispatch(v.state.tr.setMeta(key, compute(v.state)).setMeta('addToHistory', false));
  });
  return new Plugin({
    key,
    state: {
      init: (_cfg, st) => compute(st),
      apply: (tr, old) => tr.getMeta(key) ?? (tr.docChanged ? old.map(tr.mapping, tr.doc) : old),
    },
    props: { decorations: (st) => key.getState(st) },
    view: (v) => {
      view = v;
      // other plugins may replace the document while their views start
      // (the shared document of live editing): check again after them
      later.run();
      return {
        update: (v2, prev) => { view = v2; if (v2.state.doc !== prev.doc) later.run(); },
        destroy: () => { later.cancel(); },
      };
    },
  });
}

/** Renews the marks and the list now (after a switch of the mode). */
export function refreshReadability(v: EditorView): void {
  const p = key.get(v.state);
  const spec = p?.spec as { state?: { init: (c: unknown, s: EditorState) => DecorationSet } } | undefined;
  if (spec?.state) v.dispatch(v.state.tr.setMeta(key, spec.state.init({}, v.state)));
}
