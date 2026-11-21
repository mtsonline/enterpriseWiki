// The visual designer of templates of data. One draft is changed in two ways
// that always show the same: the visual view (structure, properties, design,
// preview) and the text view (JSON). The preview is the wiki itself in a
// frame of the same origin, built by the same engine with the draft, so what
// is designed is what is shown (1:1). Every change is checked by the server
// while designing (POST /check), with the same limits the engine uses.
import {
  api, button, checkbox, confirmDialog, errorText, field, fmtDate, h, langFields, placeableBlocks, positionLabel, select, state, t, textArea, textInput, toast, uiLang, uid,
} from '@ewiki/coreplugin';
import type { Block, Design, Row, View, ViewName } from './engine';
import { LIMITS, VIEWS } from './engine';
import { checkPairs } from './contrast';
import { cssProblems } from './room';
import { cssSafe, needRoom, positionsRoom, room, viewOf } from './room';

const BASE = 'x/core.template';
/** The width of the frame of each view: a common window of that kind. */
const FRAME_PX: Record<ViewName, number> = { desktop: 1280, tablet: 768, mobile: 375 };
const FRAME_HEIGHT = 760;
const HISTORY_MAX = 100;


/** The kind of an area in its name (an area with positions is just an area). */
const kindName = (kind: string): string => (kind === 'area' ? t('template.d_name_area') : t(`template.d_kind_${kind}`));
export interface Tpl {
  id: string; key: string; names: Record<string, string>; design?: Design; version: number; read_only?: boolean;
  may?: { edit: boolean; manage: boolean };
}

/** What the frame of the preview offers (see preview.ts). */
export interface PreviewWindow extends Window {
  __ewdPreviewUpdate?: (d: Design) => void;
  __ewdMeasure?: () => Measure[];
}
/** What the preview measured of one block in the view shown. */
export interface Measure { id: string; shown: boolean; overflow: boolean; need: number; have: number; items: number; firstLine: number }

declare global {
  interface Window { __ewdDraft?: Design }
}

const pick = (m: Record<string, string> | undefined) => (m ? m[state.lang] || m[state.site.default_language] || Object.values(m)[0] || '' : '');
const clone = (d: Design): Design => structuredClone(d);
const fmt = (n: number) => (Math.round(n * 10) / 10).toLocaleString(uiLang());

/** A group of toggle buttons (aria-pressed): one of them is on. */
function toggleGroup(label: string, items: { value: string; label: string }[], value: string, onPick: (v: string) => void): HTMLElement {
  return h('div', { role: 'group', 'aria-label': label, class: 'ewd-toggle' },
    ...items.map((it) => button(it.label, () => onPick(it.value), { variant: it.value === value ? 'primary' : 'subtle', pressed: it.value === value })));
}

/** A new id that is not used yet in the design. */
function freeId(d: Design, base: string): string {
  const used = new Set(d.rows.flatMap((r) => [r.id, ...r.blocks.map((b) => b.id)]));
  for (let i = 1; ; i++) {
    const id = i === 1 ? base : `${base}-${i}`;
    if (!used.has(id)) return id;
  }
}

function freePosition(d: Design, base: string): string {
  const used = new Set(d.rows.flatMap((r) => r.blocks.flatMap((b) => [...b.positions, ...(b.after ?? [])])));
  for (let i = 1; ; i++) {
    const id = `${base}-${i}`;
    if (!used.has(id)) return id;
  }
}

/** Whether a parsed text has the shape the visual view can show (the server
 * checks all the rest). */
function shapeOK(d: unknown): d is Design {
  const x = d as Design;
  return !!x && typeof x === 'object' && !!x.tokens && typeof x.tokens === 'object' && !!x.options && typeof x.options === 'object'
    && Array.isArray(x.rows) && x.rows.every((r) => r && typeof r === 'object' && Array.isArray(r.blocks) && r.blocks.every((b) => b && typeof b === 'object' && Array.isArray(b.positions)));
}

export async function designer(panel: HTMLElement, key: string, back: () => void, saved: (t: Tpl) => void): Promise<void> {
  const [{ template: tpl, used_by: used }, ownFonts] = await Promise.all([
    api.get<{ template: Tpl; used_by: string[] }>(`${BASE}/templates/${key}`),
    api.get<{ token: string; name: string }[]>(`${BASE}/fonts`),
  ]);
  let version = tpl.version;
  let draft = clone(tpl.design!);
  let savedJSON = JSON.stringify(draft);
  const history: string[] = [savedJSON];
  let at = 0;
  let mode: 'visual' | 'text' = 'visual';
  let view: ViewName = 'desktop';
  let sel: { row: number; block: number | null } = { row: 0, block: null };
  let previewPath = '/';
  let measures: Measure[] = [];

  const names = langFields(t('template.name'), tpl.names, { required: true });
  const problems = h('div', { class: 'ewd-problems', role: 'status', 'aria-live': 'polite' });
  const visual = h('div', { class: 'ewd-visual' });
  const text = textArea({ value: JSON.stringify(draft, null, 2), rows: 28 });
  text.classList.add('ewd-json');
  text.spellcheck = false;
  const textBox = h('div', { class: 'ewd-textview', hidden: true }, field(t('template.design_text'), text, { help: t('template.design_text_help') }));
  const undoBtn = button(t('template.d_undo'), () => undo(), { variant: 'subtle', ariaLabel: t('template.d_undo_long') });
  const redoBtn = button(t('template.d_redo'), () => redo(), { variant: 'subtle', ariaLabel: t('template.d_redo_long') });
  const modeBox = h('div');
  const previewBox = h('section', { class: 'ewd-preview', 'aria-labelledby': 'ewd-preview-h' });
  const hintsBox = h('div', { class: 'ewd-hints', role: 'status', 'aria-live': 'polite' });
  let frame: HTMLIFrameElement | null = null;
  let popup: Window | null = null;

  // ------------------------------------------------------------ changes
  let checkTimer = 0;
  const schedule = () => { clearTimeout(checkTimer); checkTimer = window.setTimeout(() => void check(), 400); };
  const syncButtons = () => {
    undoBtn.disabled = at === 0;
    redoBtn.disabled = at === history.length - 1;
  };
  /** Takes a new draft: history, text, check, preview, visual view. */
  const commit = (next: Design, opts: { fromText?: boolean; keep?: boolean } = {}) => {
    draft = next;
    const json = JSON.stringify(draft);
    if (!opts.keep && json !== history[at]) {
      history.splice(at + 1);
      history.push(json);
      if (history.length > HISTORY_MAX) history.shift();
      at = history.length - 1;
    }
    if (!opts.fromText) text.value = JSON.stringify(draft, null, 2);
    syncButtons();
    schedule();
    updatePreview();
    renderVisual();
  };
  const change = (f: (d: Design) => void) => {
    const d = clone(draft);
    f(d);
    commit(d);
  };
  const undo = () => {
    if (at === 0) return;
    at--;
    commit(JSON.parse(history[at]) as Design, { keep: true });
  };
  const redo = () => {
    if (at >= history.length - 1) return;
    at++;
    commit(JSON.parse(history[at]) as Design, { keep: true });
  };

  // ------------------------------------------------------------ check (server)
  const check = async (): Promise<boolean> => {
    let design: unknown = draft;
    if (mode === 'text') {
      try { design = JSON.parse(text.value); } catch (e) {
        problems.replaceChildren(h('p', { class: 'ew-form-alert' }, t('template.not_json_detail', { error: String((e as Error).message) })));
        return false;
      }
    }
    const r = await api.post<{ problems: { where: string; text: string }[] }>(`${BASE}/check`, { design });
    problems.replaceChildren(r.problems.length
      ? h('div', { class: 'ew-form-alert' }, h('p', null, t('template.problems', { n: r.problems.length })),
        h('ul', null, ...r.problems.map((p) => h('li', null, h('strong', null, p.where), ': ', p.text))))
      : h('p', { class: 'ew-ok' }, t('template.no_problems')));
    return r.problems.length === 0;
  };
  let textTimer = 0;
  text.addEventListener('input', () => {
    clearTimeout(textTimer);
    textTimer = window.setTimeout(() => {
      let d: unknown;
      try { d = JSON.parse(text.value); } catch { void check(); return; }
      if (shapeOK(d)) commit(d, { fromText: true });
      else void check();
    }, 500);
  });

  // ------------------------------------------------------------ preview
  const updatePreview = () => {
    window.__ewdDraft = draft;
    const w = frame?.contentWindow as PreviewWindow | null | undefined;
    if (!cssSafe(draft)) {
      hintsBox.replaceChildren(h('p', { class: 'ew-help' }, t('template.d_preview_wait')));
      return;
    }
    if (w?.__ewdPreviewUpdate) {
      w.__ewdPreviewUpdate(draft);
      measureSoon();
    }
    if (popup && !popup.closed) (popup as PreviewWindow).__ewdPreviewUpdate?.(draft);
  };
  let measureTimer = 0;
  const measureSoon = () => {
    clearTimeout(measureTimer);
    measureTimer = window.setTimeout(() => {
      const w = frame?.contentWindow as PreviewWindow | null | undefined;
      measures = w?.__ewdMeasure?.() ?? [];
      renderHints();
    }, 350);
  };
  const renderHints = () => {
    const out: HTMLElement[] = [];
    for (const r of draft.rows) {
      for (const b of r.blocks) {
        const m = measures.find((x) => x.id === b.id);
        if (!m || !m.shown) continue;
        const name = pick(b.label) || b.id;
        if (b.role === 'navigation' && m.items > 0 && m.firstLine < m.items) {
          out.push(h('li', null, t('template.d_hint_menu', { name, fit: m.firstLine, all: m.items, more: m.items - m.firstLine, view: t(`template.view_${view}`) })));
        } else if (b.role === 'navigation' && m.items > 0) {
          out.push(h('li', null, t('template.d_hint_menu_ok', { name, all: m.items, view: t(`template.view_${view}`) })));
        }
        if (m.overflow) out.push(h('li', null, t('template.d_hint_overflow', { name, need: Math.round(m.need), have: Math.round(m.have), view: t(`template.view_${view}`) })));
      }
    }
    hintsBox.replaceChildren(h('h3', null, t('template.d_hints', { view: t(`template.view_${view}`) })),
      out.length ? h('ul', null, ...out) : h('p', { class: 'ew-help' }, t('template.d_hints_none')));
  };
  const renderPreview = () => {
    const wrap = h('div', { class: 'ewd-frame-wrap' });
    frame = h('iframe', {
      class: 'ewd-frame', title: t('template.d_preview_frame', { view: t(`template.view_${view}`) }),
      src: `${previewPath}${previewPath.includes('?') ? '&' : '?'}ewd-preview=1`, width: String(FRAME_PX[view]), height: String(FRAME_HEIGHT),
    }) as HTMLIFrameElement;
    frame.addEventListener('load', () => { updatePreview(); measureSoon(); });
    wrap.append(frame);
    const fit = () => {
      const s = Math.min(1, wrap.clientWidth / FRAME_PX[view]);
      frame!.style.transform = `scale(${s})`;
      wrap.style.height = `${Math.ceil(FRAME_HEIGHT * s)}px`;
    };
    new ResizeObserver(fit).observe(wrap);
    const path = textInput({ value: previewPath });
    path.addEventListener('change', () => {
      const v = path.value.trim();
      // only pages of this wiki (a path, never another site)
      previewPath = /^\/(?!\/)[^\s\\]*$/.test(v) ? v : '/';
      path.value = previewPath;
      renderPreview();
    });
    previewBox.replaceChildren(
      h('h2', { id: 'ewd-preview-h' }, t('template.d_preview')),
      h('p', { class: 'ew-help' }, t('template.d_preview_help')),
      toggleGroup(t('template.d_view'), VIEWS.map((v) => ({ value: v, label: `${t(`template.view_${v}`)} (${FRAME_PX[v]} px)` })), view, (v) => { view = v as ViewName; renderPreview(); renderVisual(); }),
      field(t('template.d_preview_page'), path, { help: t('template.d_preview_page_help') }),
      h('p', null, button(t('template.d_preview_window'), () => {
        // same origin, so the window reads the draft from here (and follows changes)
        const w = window.open(`${previewPath}${previewPath.includes('?') ? '&' : '?'}ewd-preview=1`, 'ewd-preview');
        if (w) { popup = w; w.addEventListener('load', () => updatePreview()); }
      }, { variant: 'subtle' })),
      wrap, hintsBox);
  };

  // ------------------------------------------------------------ visual view
  const ids = new Map<string, string>();
  /** A stable id for a control, so the focus stays where it was after a change. */
  const cid = (k: string) => {
    if (!ids.has(k)) ids.set(k, uid('ewd'));
    return ids.get(k)!;
  };
  const renderVisual = () => {
    const focused = document.activeElement?.id;
    visual.replaceChildren(structure(), properties(), design());
    if (focused) {
      const el = document.getElementById(focused);
      if (el && visual.contains(el)) el.focus({ preventScroll: true });
    }
  };

  /** How a row fits in each view, in words. */
  const roomLine = (r: Row): HTMLElement => h('ul', { class: 'ewd-room' }, ...VIEWS.map((v) => {
    const st = viewOf(draft, r, v);
    const need = needRoom(r, v, v === 'desktop' ? undefined : st);
    const have = room(draft, r, v);
    let msg: string;
    let bad = false;
    if (v !== 'desktop' && st.stack) msg = t('template.d_room_stack', { view: t(`template.view_${v}`) });
    else if (need > have) { msg = t('template.d_room_bad', { view: t(`template.view_${v}`), need: fmt(need), have: fmt(have) }); bad = true; }
    else msg = t('template.d_room_ok', { view: t(`template.view_${v}`), need: fmt(need), have: fmt(have) });
    if (v !== 'desktop' && st.drawer.length) msg += ` ${t('template.d_room_drawer', { list: st.drawer.join(', ') })}`;
    return h('li', { class: bad ? 'ewd-room--bad' : undefined }, msg);
  }));

  const structure = (): HTMLElement => {
    const rows = draft.rows.map((r, ri) => {
      const rowName = t('template.d_row', { n: ri + 1, id: r.id });
      const rowSel = sel.row === ri && sel.block === null;
      return h('li', { class: ['ewd-s-row', rowSel && 'ewd-selected'] },
        h('div', { class: 'ewd-s-head' },
          button(rowName, () => { sel = { row: ri, block: null }; renderVisual(); }, { variant: rowSel ? 'primary' : 'subtle', pressed: rowSel, id: cid(`row-${ri}`) }),
          h('span', { class: 'ew-badge' }, t(`template.d_rowrole_${r.role}`)),
          h('span', { class: 'ewd-s-tools' },
            button('↑', () => change((d) => { [d.rows[ri - 1], d.rows[ri]] = [d.rows[ri], d.rows[ri - 1]]; sel = { row: ri - 1, block: null }; }), { variant: 'subtle', ariaLabel: t('template.d_row_up', { name: rowName }), disabled: ri === 0, id: cid(`row-${ri}-up`) }),
            button('↓', () => change((d) => { [d.rows[ri + 1], d.rows[ri]] = [d.rows[ri], d.rows[ri + 1]]; sel = { row: ri + 1, block: null }; }), { variant: 'subtle', ariaLabel: t('template.d_row_down', { name: rowName }), disabled: ri === draft.rows.length - 1, id: cid(`row-${ri}-down`) }),
            button(t('template.d_add_block'), () => change((d) => {
              const id = freeId(d, 'block');
              d.rows[ri].blocks.push({ id, kind: 'area', role: 'none', width: 0, positions: [freePosition(d, id)], layout: 'row', align: 'start' });
              sel = { row: ri, block: d.rows[ri].blocks.length - 1 };
            }), { variant: 'subtle', ariaLabel: t('template.d_add_block_to', { name: rowName }), id: cid(`row-${ri}-add`), disabled: r.blocks.length >= LIMITS.max_blocks_per_row }),
            button(t('common.delete'), () => void (async () => {
              if (r.blocks.some((b) => b.kind === 'content')) { toast(t('template.d_keep_content'), 'error'); return; }
              if (!(await confirmDialog(t('template.d_delete_row', { name: rowName }), t('common.delete'), true))) return;
              change((d) => { d.rows.splice(ri, 1); sel = { row: Math.max(0, ri - 1), block: null }; });
            })(), { variant: 'subtle', ariaLabel: `${t('common.delete')}: ${rowName}`, id: cid(`row-${ri}-del`) }))),
        h('ul', { class: 'ewd-s-blocks' }, ...r.blocks.map((b, bi) => {
          const name = `${pick(b.label) || b.id} (${kindName(b.kind)})`;
          const on = sel.row === ri && sel.block === bi;
          return h('li', { class: ['ewd-s-block', on && 'ewd-selected'] },
            button(name, () => { sel = { row: ri, block: bi }; renderVisual(); }, { variant: on ? 'primary' : 'subtle', pressed: on, id: cid(`b-${ri}-${bi}`) }),
            button('←', () => change((d) => { const bs = d.rows[ri].blocks; [bs[bi - 1], bs[bi]] = [bs[bi], bs[bi - 1]]; sel = { row: ri, block: bi - 1 }; }), { variant: 'subtle', ariaLabel: t('template.d_block_left', { name }), disabled: bi === 0, id: cid(`b-${ri}-${bi}-l`) }),
            button('→', () => change((d) => { const bs = d.rows[ri].blocks; [bs[bi + 1], bs[bi]] = [bs[bi], bs[bi + 1]]; sel = { row: ri, block: bi + 1 }; }), { variant: 'subtle', ariaLabel: t('template.d_block_right', { name }), disabled: bi === r.blocks.length - 1, id: cid(`b-${ri}-${bi}-r`) }));
        })),
        roomLine(r));
    });
    return h('section', { class: 'ewd-structure', 'aria-labelledby': 'ewd-structure-h' },
      h('h2', { id: 'ewd-structure-h' }, t('template.d_structure')),
      h('p', { class: 'ew-help' }, t('template.d_structure_help')),
      h('ol', { class: 'ewd-s-rows' }, ...rows),
      button(t('template.d_add_row'), () => change((d) => {
        const id = freeId(d, 'row');
        const bid = freeId({ ...d, rows: [...d.rows, { id, role: 'none', width: 'content', background: 'page', blocks: [] }] }, 'block');
        const at = Math.max(0, d.rows.findIndex((r) => r.role === 'contentinfo'));
        const row: Row = { id, role: 'none', width: 'content', background: 'page', blocks: [{ id: bid, kind: 'area', role: 'none', width: 0, positions: [freePosition(d, bid)], layout: 'row', align: 'start' }] };
        const idx = d.rows.some((r) => r.role === 'contentinfo') ? at : d.rows.length;
        d.rows.splice(idx, 0, row);
        sel = { row: idx, block: 0 };
      }), { id: cid('add-row'), disabled: draft.rows.length >= LIMITS.max_rows }));
  };

  // a field bound to the draft: change on "change" (not on every key), so typing is not interrupted
  const sel1 = (k: string, label: string, options: string[], value: string, labelOf: (o: string) => string, set: (d: Design, v: string) => void, help?: string) => {
    const s = select(options.map((o) => ({ value: o, label: labelOf(o) })), value);
    s.id = cid(k);
    s.addEventListener('change', () => change((d) => set(d, s.value)));
    return field(label, s, { help });
  };
  const num = (k: string, label: string, value: number, min: number, max: number, set: (d: Design, v: number) => void, help?: string) => {
    const i = textInput({ type: 'number', value: String(value), id: cid(k) });
    i.min = String(min);
    i.max = String(max);
    i.step = '1';
    i.addEventListener('change', () => {
      const v = Number(i.value);
      if (Number.isFinite(v)) change((d) => set(d, Math.round(v)));
    });
    return field(label, i, { help });
  };
  const txt = (k: string, label: string, value: string, set: (d: Design, v: string) => void, help?: string) => {
    const i = textInput({ value, id: cid(k) });
    i.addEventListener('change', () => change((d) => set(d, i.value.trim())));
    return field(label, i, { help });
  };
  const chk = (k: string, label: string, value: boolean, set: (d: Design, v: boolean) => void, help?: string) => {
    const c = checkbox(label, value, { help });
    c.input.id = cid(k);
    c.el.querySelector('label')?.setAttribute('for', c.input.id);
    c.input.addEventListener('change', () => change((d) => set(d, c.input.checked)));
    return c.el;
  };
  const list = (s: string) => s.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);

  const viewFields = (r: Row, ri: number, v: 'tablet' | 'mobile'): HTMLElement => {
    const manual = r[v];
    const auto = viewOf(draft, r, v);
    const vname = t(`template.view_${v}`);
    const kids: (HTMLElement | null)[] = [h('legend', null, vname)];
    kids.push(h('p', { class: 'ew-help' }, manual ? t('template.d_view_manual') : t('template.d_view_auto', {
      how: auto.stack ? t('template.d_auto_stack') : t('template.d_auto_side'), drawer: auto.drawer.length ? auto.drawer.join(', ') : '–',
    })));
    if (!manual) {
      kids.push(button(t('template.d_view_adjust', { view: vname }), () => change((d) => {
        d.rows[ri][v] = { stack: auto.stack, ...(auto.drawer.length ? { drawer: [...auto.drawer] } : {}) };
      }), { variant: 'subtle', id: cid(`r${ri}-${v}-adjust`) }));
    } else {
      kids.push(chk(`r${ri}-${v}-stack`, t('template.d_view_stack'), manual.stack, (d, x) => { d.rows[ri][v]!.stack = x; }));
      for (const b of r.blocks) {
        if (b.kind === 'content') continue;
        const name = pick(b.label) || b.id;
        const toggle = (key: 'hidden' | 'drawer', on: boolean) => (d: Design) => {
          const vw = d.rows[ri][v] as View;
          const cur = new Set(vw[key] ?? []);
          if (on) cur.add(b.id); else cur.delete(b.id);
          if (cur.size) vw[key] = [...cur]; else delete vw[key];
        };
        kids.push(h('div', { class: 'ewd-view-block' },
          h('strong', null, name),
          chk(`r${ri}-${v}-${b.id}-hidden`, t('template.d_view_hidden'), !!manual.hidden?.includes(b.id), (d, x) => toggle('hidden', x)(d)),
          b.kind === 'brand' ? null : chk(`r${ri}-${v}-${b.id}-drawer`, t('template.d_view_drawer'), !!manual.drawer?.includes(b.id), (d, x) => toggle('drawer', x)(d)),
          num(`r${ri}-${v}-${b.id}-width`, t('template.d_view_width'), manual.widths?.[b.id] ?? b.width, 0, LIMITS.block_max_rem, (d, x) => {
            const vw = d.rows[ri][v] as View;
            const w = { ...(vw.widths ?? {}) };
            if (x === b.width) delete w[b.id]; else w[b.id] = x;
            if (Object.keys(w).length) vw.widths = w; else delete vw.widths;
          }, t('template.d_width_help'))));
      }
      kids.push(button(t('template.d_view_auto_again'), () => change((d) => { delete d.rows[ri][v]; }), { variant: 'subtle', id: cid(`r${ri}-${v}-auto`) }));
    }
    return h('fieldset', { class: 'ewd-viewset' }, ...kids.filter((x): x is HTMLElement => !!x));
  };

  // the desktop: only blocks behind a button (a menu as a button also there)
  const desktopFields = (r: Row, ri: number): HTMLElement | null => {
    const blocks = r.blocks.filter((b) => b.kind !== 'content' && b.kind !== 'brand');
    if (!blocks.length) return null;
    return h('fieldset', { class: 'ewd-viewset' }, h('legend', null, t('template.view_desktop')),
      h('p', { class: 'ew-help' }, t('template.d_desktop_drawer_help')),
      ...blocks.map((b) => chk(`r${ri}-desktop-${b.id}-drawer`, t('template.d_desktop_drawer', { name: pick(b.label) || b.id }), !!r.desktop?.drawer?.includes(b.id), (d, x) => {
        const cur = new Set(d.rows[ri].desktop?.drawer ?? []);
        if (x) cur.add(b.id); else cur.delete(b.id);
        if (cur.size) d.rows[ri].desktop = { drawer: [...cur] }; else delete d.rows[ri].desktop;
      })));
  };

  const properties = (): HTMLElement => {
    const r = draft.rows[sel.row];
    const body: HTMLElement[] = [];
    if (!r) {
      body.push(h('p', { class: 'ew-help' }, t('template.d_select')));
    } else if (sel.block === null) {
      const ri = sel.row;
      body.push(h('h3', null, t('template.d_row', { n: ri + 1, id: r.id })),
        txt(`r${ri}-id`, t('template.d_id'), r.id, (d, v) => { d.rows[ri].id = v; }, t('template.d_id_help')),
        sel1(`r${ri}-role`, t('template.d_rowrole'), ['banner', 'contentinfo', 'none'], r.role, (o) => t(`template.d_rowrole_${o}`), (d, v) => { d.rows[ri].role = v as Row['role']; }, t('template.d_rowrole_help')),
        sel1(`r${ri}-width`, t('template.d_rowwidth'), ['full', 'content'], r.width, (o) => t(`template.d_rowwidth_${o}`), (d, v) => { d.rows[ri].width = v as Row['width']; }),
        sel1(`r${ri}-bg`, t('template.d_rowbg'), ['page', 'subtle', 'header', 'footer'], r.background, (o) => t(`template.d_rowbg_${o}`), (d, v) => { d.rows[ri].background = v as Row['background']; }),
        widthBar(r, ri),
        r.role === 'contentinfo' ? chk(`r${ri}-fold`, t('template.d_fold'), !!r.collapse_mobile, (d, v) => { if (v) d.rows[ri].collapse_mobile = true; else delete d.rows[ri].collapse_mobile; }, t('template.d_fold_help')) : h('span'),
        ...[desktopFields(r, ri)].filter((x): x is HTMLElement => !!x), viewFields(r, ri, 'tablet'), viewFields(r, ri, 'mobile'));
    } else {
      const ri = sel.row;
      const bi = sel.block;
      const b = r.blocks[bi];
      if (!b) {
        body.push(h('p', { class: 'ew-help' }, t('template.d_select')));
      } else {
        const label = langFields(t('template.d_label'), b.label ?? {}, {});
        const labelBox = h('div', null, label.el);
        labelBox.addEventListener('change', () => change((d) => {
          const v = Object.fromEntries(Object.entries(label.get()).filter(([, x]) => x.trim() !== ''));
          if (Object.keys(v).length) d.rows[ri].blocks[bi].label = v; else delete d.rows[ri].blocks[bi].label;
        }));
        const pr = positionsRoom(b);
        body.push(h('h3', null, `${pick(b.label) || b.id} (${kindName(b.kind)})`),
          txt(`b${ri}-${bi}-id`, t('template.d_id'), b.id, (d, v) => { d.rows[ri].blocks[bi].id = v; }, t('template.d_id_help')),
          sel1(`b${ri}-${bi}-kind`, t('template.d_kind'), ['area', 'brand', 'content'], b.kind, (o) => t(`template.d_kind_${o}`), (d, v) => {
            const x = d.rows[ri].blocks[bi];
            x.kind = v as Block['kind'];
            if (x.kind === 'content') { x.width = 0; delete x.fit; x.role = 'none'; }
          }, t('template.d_kind_help')),
          b.kind === 'content' ? h('p', { class: 'ew-help' }, t('template.d_content_note')) : sel1(`b${ri}-${bi}-role`, t('template.d_role'), ['navigation', 'complementary', 'region', 'none'], b.role, (o) => t(`template.d_role_${o}`), (d, v) => { d.rows[ri].blocks[bi].role = v as Block['role']; }, t('template.d_role_help')),
          labelBox,
          b.kind === 'content' ? h('span') : num(`b${ri}-${bi}-width`, t('template.d_width'), b.width, 0, LIMITS.block_max_rem, (d, v) => { const x = d.rows[ri].blocks[bi]; x.width = v; if (v) delete x.fit; }, t('template.d_width_help_block', { min: Math.max((LIMITS.block_min_rem as Record<string, number>)[b.kind] ?? 0, pr), max: LIMITS.block_max_rem })),
          b.role === 'complementary' && b.kind !== 'content' ? chk(`b${ri}-${bi}-sticky`, t('template.d_sticky_col'), !!b.sticky, (d, v) => { const x = d.rows[ri].blocks[bi]; if (v) x.sticky = true; else delete x.sticky; }, t('template.d_sticky_col_help')) : h('span'),
          b.role === 'complementary' && b.kind !== 'content' ? chk(`b${ri}-${bi}-collapsible`, t('template.d_collapsible'), !!b.collapsible, (d, v) => { const x = d.rows[ri].blocks[bi]; if (v) x.collapsible = true; else delete x.collapsible; }, t('template.d_collapsible_help')) : h('span'),
          b.kind === 'content' ? h('span') : chk(`b${ri}-${bi}-fit`, t('template.d_fit'), !!b.fit, (d, v) => { const x = d.rows[ri].blocks[bi]; if (v) { x.fit = true; x.width = 0; } else delete x.fit; }, t('template.d_fit_help')),
          positionsField(ri, bi, b),
          b.kind === 'content' ? txt(`b${ri}-${bi}-after`, t('template.d_after'), (b.after ?? []).join(', '), (d, v) => { const x = d.rows[ri].blocks[bi]; const l = list(v); if (l.length) x.after = l; else delete x.after; }, t('template.d_after_help')) : h('span'),
          sel1(`b${ri}-${bi}-layout`, t('template.d_layout'), ['row', 'column'], b.layout, (o) => t(`template.d_layout_${o}`), (d, v) => { d.rows[ri].blocks[bi].layout = v as Block['layout']; }),
          pr > 0 ? h('p', { class: 'ew-help' }, t('template.d_positions_room', { n: b.positions.length, need: fmt(pr) })) : h('span'),
          sel1(`b${ri}-${bi}-align`, t('template.d_align'), ['start', 'center', 'end', 'between'], b.align, (o) => t(`template.d_align_${o}`), (d, v) => { d.rows[ri].blocks[bi].align = v as Block['align']; }),
          h('div', { class: 'ew-actions' },
            button(t('template.d_delete_block'), () => {
              if (b.kind === 'content') { toast(t('template.d_keep_content'), 'error'); return; }
              change((d) => { d.rows[ri].blocks.splice(bi, 1); sel = { row: ri, block: null }; });
            }, { variant: 'danger', id: cid(`b${ri}-${bi}-del`) }),
            draft.rows.length > 1 ? sel1(`b${ri}-${bi}-move`, t('template.d_move_to'), draft.rows.map((_, i) => String(i)), String(ri), (o) => t('template.d_row', { n: Number(o) + 1, id: draft.rows[Number(o)].id }), (d, v) => {
              const to = Number(v);
              if (to === ri) return;
              const [x] = d.rows[ri].blocks.splice(bi, 1);
              d.rows[to].blocks.push(x);
              sel = { row: to, block: d.rows[to].blocks.length - 1 };
            }) : h('span')));
      }
    }
    return h('section', { class: 'ewd-props', 'aria-labelledby': 'ewd-props-h' }, h('h2', { id: 'ewd-props-h' }, t('template.d_properties')), ...body);
  };

  const COLORS = ['bg', 'bg_subtle', 'text', 'text_muted', 'border', 'primary', 'on_primary', 'link', 'focus', 'header_bg', 'header_text', 'footer_bg', 'footer_text'] as const;
  const design = (): HTMLElement => {
    const k = draft.tokens;
    const colors = COLORS.map((c) => {
      const i = h('input', { type: 'color', class: 'ewd-color', value: k[c], id: cid(`c-${c}`) }) as HTMLInputElement;
      i.addEventListener('change', () => change((d) => { d.tokens[c] = i.value.toLowerCase(); }));
      return h('div', { class: 'ewd-color-field' }, h('label', { for: i.id }, t(`template.d_color_${c}`)), i, h('code', null, k[c]));
    });
    const pairs = checkPairs(k as unknown as Record<string, unknown>);
    const contrastList = h('ul', { class: 'ewd-contrast' }, ...pairs.map((p) => {
      const name = t('template.d_pair', { fore: t(`template.d_color_${p.pair.fore}`), back: t(`template.d_color_${p.pair.back}`) });
      return h('li', { class: p.ok ? 'ewd-ok' : 'ewd-bad' },
        p.ok ? t('template.d_pair_ok', { name, value: fmt(p.value), min: fmt(p.min) }) : t('template.d_pair_bad', { name, value: fmt(p.value), min: fmt(p.min) }), ' ',
        !p.ok && p.suggestion ? button(t('template.d_pair_take', { color: p.suggestion }), () => change((d) => { (d.tokens as unknown as Record<string, string>)[p.pair.fore] = p.suggestion!; }), { variant: 'subtle', ariaLabel: t('template.d_pair_take_named', { name, color: p.suggestion }), id: cid(`pair-${p.pair.fore}-${p.pair.back}`) }) : null);
    }));
    // shipped fonts and the uploaded ones (Templates → Own fonts)
    const fonts = [...(LIMITS.fonts as string[]), ...ownFonts.map((f) => f.token)];
    const fontName = (o: string) => ownFonts.find((f) => f.token === o)?.name ?? t(`template.d_font_${o}`);
    return h('section', { class: 'ewd-design', 'aria-labelledby': 'ewd-design-h' },
      h('h2', { id: 'ewd-design-h' }, t('template.d_design')),
      h('h3', null, t('template.d_colors')), h('div', { class: 'ewd-colors' }, ...colors),
      h('h3', null, t('template.d_contrast')), h('p', { class: 'ew-help' }, t('template.d_contrast_help')), contrastList,
      h('h3', null, t('template.d_type')),
      sel1('font', t('template.d_font'), fonts, k.font, fontName, (d, v) => { d.tokens.font = v; }, t('template.d_font_help')),
      sel1('heading_font', t('template.d_heading_font'), fonts, k.heading_font, fontName, (d, v) => { d.tokens.heading_font = v; }),
      num('scale', t('template.d_scale'), k.scale, LIMITS.scale_percent[0], LIMITS.scale_percent[1], (d, v) => { d.tokens.scale = v; }, t('template.d_range', { min: LIMITS.scale_percent[0], max: LIMITS.scale_percent[1] })),
      num('radius', t('template.d_radius'), k.radius, LIMITS.radius_px[0], LIMITS.radius_px[1], (d, v) => { d.tokens.radius = v; }, t('template.d_range', { min: LIMITS.radius_px[0], max: LIMITS.radius_px[1] })),
      num('content_width', t('template.d_content_width'), k.content_width, LIMITS.content_width_rem[0], LIMITS.content_width_rem[1], (d, v) => { d.tokens.content_width = v; }, t('template.d_range', { min: LIMITS.content_width_rem[0], max: LIMITS.content_width_rem[1] })),
      txt('logo', t('template.d_logo'), k.logo, (d, v) => { d.tokens.logo = v; }, t('template.d_logo_help')),
      h('h3', null, t('template.d_behavior')),
      chk('sticky', t('template.d_sticky'), draft.options.sticky_header, (d, v) => { d.options.sticky_header = v; }, t('template.d_sticky_help', { vh: LIMITS.sticky_max_vh })),
      chk('shrink', t('template.d_shrink'), !!draft.options.header_shrink, (d, v) => { if (v) { d.options.header_shrink = true; d.options.sticky_header = true; } else delete d.options.header_shrink; }, t('template.d_shrink_help')),
      chk('hide', t('template.d_hide'), !!draft.options.header_hide, (d, v) => { if (v) { d.options.header_hide = true; d.options.sticky_header = true; } else delete d.options.header_hide; }, t('template.d_hide_help')),
      chk('print', t('template.d_print'), !!draft.options.print_plain, (d, v) => { if (v) d.options.print_plain = true; else delete d.options.print_plain; }, t('template.d_print_help')),
      chk('totop', t('template.d_totop'), draft.options.back_to_top, (d, v) => { d.options.back_to_top = v; }),
      cssField());
  };

  // the positions of a block; removing a standard position names the
  // building blocks that stand there by default (allowed, but they lose
  // their place, concept M19 3.3)
  const positionsField = (ri: number, bi: number, b: Block): HTMLElement => {
    const i = textInput({ value: b.positions.join(', '), id: cid(`b${ri}-${bi}-pos`) });
    i.addEventListener('change', () => void (async () => {
      const next = list(i.value);
      const offered = new Set(draft.rows.flatMap((r, rj) => r.blocks.flatMap((x, bj) => (rj === ri && bj === bi ? [] : [...x.positions, ...(x.after ?? [])]))));
      const removed = b.positions.filter((p) => !next.includes(p) && !offered.has(p));
      if (removed.length) {
        const defaults = (await placeableBlocks()).filter((x) => x.kind === 'site' && removed.includes(x.position));
        if (defaults.length && !(await confirmDialog(t('template.d_remove_pos', {
          positions: removed.map((p) => positionLabel(p)).join(', '), blocks: defaults.map((x) => x.label).join(', '),
        }), t('template.d_remove_pos_ok'), true))) {
          i.value = b.positions.join(', ');
          return;
        }
      }
      change((d) => { d.rows[ri].blocks[bi].positions = next; });
    })());
    return field(t('template.d_positions'), i, { help: t('template.d_positions_help') });
  };

  // the widths of the blocks of a row on the desktop, changed by dragging the
  // edge of a block or with the arrow keys on it (a slider each)
  const widthBar = (r: Row, ri: number): HTMLElement => {
    const total = room(draft, r, 'desktop');
    const bar = h('div', { class: 'ewd-widthbar' });
    for (const [bi, b] of r.blocks.entries()) {
      const min = Math.max((LIMITS.block_min_rem as Record<string, number>)[b.kind] ?? 0, positionsRoom(b));
      const fixed = b.kind !== 'content' && !b.fit;
      let w = b.width || 0;
      const seg = h('div', { class: ['ewd-widthbar-seg', b.kind === 'content' && 'ewd-widthbar-seg--content'] }, h('span', null, pick(b.label) || b.id));
      const size = () => { seg.style.flex = w > 0 ? `0 0 ${(w / total) * 100}%` : '1 1 0'; };
      size();
      bar.append(seg);
      if (!fixed) continue;
      const name = pick(b.label) || b.id;
      const handle = h('div', { id: cid(`r${ri}-wb-${bi}`), class: 'ewd-widthbar-handle', role: 'slider', tabindex: '0', 'aria-label': t('template.d_width_of', { name }),
        'aria-valuemin': String(min), 'aria-valuemax': String(LIMITS.block_max_rem), 'aria-valuenow': String(w || min), 'aria-valuetext': w ? `${w} rem` : t('template.d_width_rest') });
      const show = () => { size(); handle.setAttribute('aria-valuenow', String(w || min)); handle.setAttribute('aria-valuetext', w ? `${w} rem` : t('template.d_width_rest')); };
      const commit = () => change((d) => { const x = d.rows[ri].blocks[bi]; x.width = w; if (w) delete x.fit; });
      const clamp = (v: number) => Math.min(LIMITS.block_max_rem, Math.max(min, Math.round(v)));
      handle.addEventListener('keydown', (ev) => {
        const step = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1, PageUp: 4, PageDown: -4 }[ev.key];
        if (step !== undefined) w = clamp((w || min) + step);
        else if (ev.key === 'Home') w = min;
        else if (ev.key === 'End') w = LIMITS.block_max_rem;
        else return;
        ev.preventDefault();
        show();
        commit();
      });
      handle.addEventListener('pointerdown', (ev) => {
        ev.preventDefault();
        handle.setPointerCapture(ev.pointerId);
        const start = ev.clientX;
        const from = w || min;
        const remPx = bar.clientWidth / total;
        const move = (e: PointerEvent) => { w = clamp(from + (e.clientX - start) / remPx); show(); };
        const up = () => { handle.removeEventListener('pointermove', move); commit(); };
        handle.addEventListener('pointermove', move);
        handle.addEventListener('pointerup', up, { once: true });
      });
      seg.append(handle);
    }
    return h('div', { class: 'ewd-widthbar-box' }, h('h4', null, t('template.d_widths')), h('p', { class: 'ew-help' }, t('template.d_widths_help')), bar);
  };

  // own CSS: only while the administration allows it; checked like on the server
  const cssField = (): HTMLElement => {
    const allowed = !!state.site.custom_css;
    // own CSS of a template whose rights are limited: only those who manage it
    const manages = tpl.may?.manage ?? true;
    const area = textArea({ value: draft.css ?? '', rows: 8 });
    area.id = cid('css');
    area.spellcheck = false;
    area.classList.add('ewe-mono');
    area.disabled = !allowed && !draft.css;
    area.readOnly = !manages;
    const issues = h('ul', { class: 'ew-form-alert', 'aria-live': 'polite' });
    const show = () => {
      const ps = cssProblems(area.value);
      issues.replaceChildren(...ps.map((p) => h('li', null, t('template.d_css_problem', { line: p.line, what: p.what }))));
    };
    area.addEventListener('input', show);
    area.addEventListener('change', () => change((d) => { const v = area.value.trim(); if (v) d.css = v; else delete d.css; }));
    show();
    return h('div', null, h('h3', null, t('template.d_css')),
      h('p', { class: allowed && manages ? 'ew-help' : 'ew-notice ew-notice--info' }, !allowed ? t('template.d_css_off') : manages ? t('template.d_css_help') : t('template.d_css_manage')),
      field(t('template.d_css_label'), area), issues);
  };

  // keyboard: undo and redo in the visual view (text fields keep their own)
  visual.addEventListener('keydown', (ev) => {
    if (!(ev.ctrlKey || ev.metaKey) || (ev.target instanceof HTMLInputElement && ev.target.type !== 'checkbox' && ev.target.type !== 'color')) return;
    const k = ev.key.toLowerCase();
    if (k === 'z' && !ev.shiftKey) { ev.preventDefault(); undo(); } else if ((k === 'z' && ev.shiftKey) || k === 'y') { ev.preventDefault(); redo(); }
  });

  // ------------------------------------------------------------ save, versions
  const save = button(t('common.save'), () => void (async () => {
    if (!(await check())) { (mode === 'text' ? text : problems).focus?.(); return; }
    try {
      const design = mode === 'text' ? JSON.parse(text.value) as Design : draft;
      const r = await api.put<Tpl>(`${BASE}/templates/${key}`, { names: names.get(), design, version });
      version = r.version;
      savedJSON = JSON.stringify(design);
      saved(r);
      toast(t('template.saved', { version: r.version }));
      void showVersions();
    } catch (e) { toast(errorText(e), 'error'); }
  })(), { variant: 'primary' });
  const versionsBox = h('section', { 'aria-labelledby': 'ewd-versions-h' });
  const showVersions = async () => {
    const vs = await api.get<{ version: number; design: Design; created_at: string }[]>(`${BASE}/templates/${key}/versions`);
    versionsBox.replaceChildren(h('h2', { id: 'ewd-versions-h' }, t('template.versions')),
      h('p', { class: 'ew-help' }, t('template.versions_help')),
      h('ul', { class: 'ewd-versions' }, ...vs.map((v) => h('li', null,
        t('template.version_line', { version: v.version, date: fmtDate(v.created_at, uiLang()) }), ' ',
        v.version === version ? h('span', { class: 'ew-help' }, t('template.version_current'))
          : button(t('template.version_take'), () => {
            commit(clone(v.design));
            toast(t('template.version_taken', { version: v.version }));
          }, { variant: 'subtle', ariaLabel: t('template.version_take_named', { version: v.version }) })))));
  };
  void showVersions();

  const limitsBox = h('details', { class: 'ewd-limits' }, h('summary', null, t('template.limits')), h('ul', null,
    ...VIEWS.map((v) => h('li', null, t('template.limit_view', { view: t(`template.view_${v}`), px: LIMITS.views[v].min_px, rem: LIMITS.views[v].content_min_rem }))),
    h('li', null, t('template.limit_blocks', { min: LIMITS.block_min_rem.area, max: LIMITS.block_max_rem })),
    h('li', null, t('template.limit_counts', { rows: LIMITS.max_rows, per_row: LIMITS.max_blocks_per_row, blocks: LIMITS.max_blocks, positions: LIMITS.max_positions })),
    h('li', null, t('template.limit_contrast', { text: LIMITS.contrast_text, ui: LIMITS.contrast_ui })),
    h('li', null, t('template.limit_fonts', { list: (LIMITS.fonts as string[]).join(', ') }))));
  // the limits as the server delivers them (the same file; also for API clients)
  void api.get<{ max_bytes: number }>(`${BASE}/limits`).then((l) => { limitsBox.querySelector('ul')?.append(h('li', null, t('template.limit_size', { kb: Math.round(l.max_bytes / 1024) }))); }).catch(() => {});

  const renderMode = () => {
    modeBox.replaceChildren(toggleGroup(t('template.d_mode'), [{ value: 'visual', label: t('template.d_mode_visual') }, { value: 'text', label: t('template.d_mode_text') }], mode, (m) => {
      if (m === mode) return;
      if (m === 'visual') {
        let d: unknown;
        try { d = JSON.parse(text.value); } catch { toast(t('template.d_text_first'), 'error'); return; }
        if (!shapeOK(d)) { toast(t('template.d_text_first'), 'error'); return; }
        commit(d, { fromText: true });
      }
      mode = m as 'visual' | 'text';
      visual.hidden = mode !== 'visual';
      textBox.hidden = mode !== 'text';
      renderMode();
      (mode === 'text' ? text : visual.querySelector<HTMLElement>('button'))?.focus();
    }));
  };
  const leave = () => void (async () => {
    const dirty = JSON.stringify(mode === 'text' ? (() => { try { return JSON.parse(text.value) as unknown; } catch { return text.value; } })() : draft) !== savedJSON;
    if (dirty && !(await confirmDialog(t('template.d_leave'), t('template.d_leave_ok'), true))) return;
    delete window.__ewdDraft;
    back();
  })();

  renderMode();
  renderVisual();
  renderPreview();
  syncButtons();
  window.__ewdDraft = draft;
  panel.replaceChildren(...([
    h('p', null, button(t('template.back'), leave, { variant: 'subtle' })),
    h('h1', null, t('template.edit_named', { name: pick(tpl.names) })),
    used.length ? h('p', { class: 'ew-help' }, t('template.used_by', { list: used.join(', ') })) : null,
    h('form', { class: 'ew-form ewd-designer', novalidate: true, onsubmit: (ev: Event) => ev.preventDefault() },
      names.el,
      h('div', { class: 'ewd-bar' }, modeBox, h('div', { role: 'group', 'aria-label': t('template.d_history'), class: 'ewd-toggle' }, undoBtn, redoBtn)),
      problems,
      h('div', { class: 'ewd-work' }, h('div', { class: 'ewd-edit' }, visual, textBox), previewBox),
      limitsBox,
      h('div', { class: 'ew-actions' }, save)),
    versionsBox,
  ] as (HTMLElement | null)[]).filter((n): n is HTMLElement => !!n));
  void check();
}
