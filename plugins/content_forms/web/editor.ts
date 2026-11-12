// Form editor: settings, the field list (add by click or drag from the
// palette, order by drag or buttons, edit in place), the free layout as a
// canvas (drag or arrow keys; numbers as alternative) and the targets.
import { announce, api, button, categorySelect, checkbox, elementSelect, field, h, select, t, textArea, textInput, type EditorContext, type EditorInstance } from '@ewiki/sdk';
import { BASE } from './widget';

interface Option { value: string; label: string }
interface Cond { field: string; op: string; value?: string }
export interface FField {
  id: string; type: string; label: string; help?: string; required?: boolean; placeholder?: string; default?: string; prefill?: string;
  options?: Option[]; min?: number | null; max?: number | null; step?: number; min_length?: number; max_length?: number; pattern?: string; pattern_help?: string;
  rows?: number; autocomplete?: string; show_if?: Cond | null; width?: number; x?: number; y?: number; w?: number; h?: number;
  formula?: string; decimals?: number; fields?: FField[]; min_rows?: number; max_rows?: number;
  accept?: string[]; max_files?: number; max_size_mb?: number; file_comment?: boolean; allow_macros?: boolean; allow_active?: boolean; media_category?: number;
}
interface EmailTarget { to: string; subject: string; body: string; files?: boolean }
interface Targets {
  store: boolean; email?: EmailTarget[]; webhook?: { url: string; secret?: string; secret_ref?: string } | null;
  propdb?: { element_id: number; title: string; mapping: Record<string, string> } | null;
  workflow?: { element_id: number; mapping: Record<string, string>; files?: boolean } | null;
  files?: { category_id: number; formats: string[]; name: string } | null;
}
export interface FormBody {
  intro: string; fields: FField[]; layout: 'grid' | 'free'; height?: number; submit_label?: string; success_message?: string;
  once_per_user?: boolean; signed_in?: boolean; opens_at?: string | null; closes_at?: string | null; max_submissions?: number;
  retention_days?: number; copy_to_submitter?: boolean; proof_of_work?: boolean; public_results?: boolean; targets: Targets;
}
interface Meta { input_types: string[]; static_types: string[]; cond_ops: string[]; file_kinds: Record<string, string[]>; autocomplete: string[]; http_allow: boolean; virus_scan: boolean }

const TYPES = ['text', 'textarea', 'email', 'tel', 'url', 'number', 'date', 'time', 'datetime', 'select', 'multiselect', 'radio', 'checkbox', 'checkboxes',
  'rating', 'range', 'calc', 'repeat', 'file', 'signature', 'consent', 'hidden', 'heading', 'paragraph', 'divider', 'page'];
const CHOICE = ['select', 'multiselect', 'radio', 'checkboxes'];
const STATIC = ['heading', 'paragraph', 'divider', 'page'];
const DRAG = 'application/x-ewiki-form-field';

const typeLabel = (x: string) => t(`forms.type.${x}`);
const num = (v: string) => (v.trim() === '' ? null : Number(v));
const localDT = (iso?: string | null) => (iso ? new Date(iso).toISOString().slice(0, 16) : '');

function newId(fields: FField[], type: string): string {
  const base = type.replace(/[^a-z]/g, '') || 'field';
  for (let i = 1; ; i++) {
    const id = `${base}${i}`;
    if (!fields.some((f) => f.id === id)) return id;
  }
}

export function createFormEditor(container: HTMLElement, raw: unknown, _ctx: EditorContext): EditorInstance {
  const body = structuredClone((raw ?? {}) as FormBody);
  body.fields ??= [];
  body.layout ??= 'grid';
  body.targets ??= { store: true };
  body.targets.email ??= [];
  let meta: Meta | null = null;
  void api.get<Meta>(`${BASE}/meta`).then((m) => { meta = m; drawFields(); }).catch(() => undefined);

  // ---------------------------------------------------------------- settings
  const intro = textArea({ value: body.intro ?? '', rows: 3 });
  intro.addEventListener('input', () => { body.intro = intro.value; });
  const layout = select([{ value: 'grid', label: t('forms.ed.layout.grid') }, { value: 'free', label: t('forms.ed.layout.free') }], body.layout);
  const height = textInput({ type: 'number', value: String(body.height ?? 600) });
  const layoutHint = h('p', { class: 'ew-help ewfm-ed-hint', role: 'note' }, t('forms.ed.layout_hint'));
  const heightField = field(t('forms.ed.height'), height);
  const canvasBox = h('div', { class: 'ewfm-ed-canvasbox' });
  const syncLayout = () => {
    body.layout = layout.value as 'grid' | 'free';
    body.height = Number(height.value) || 600;
    const free = body.layout === 'free';
    layoutHint.hidden = heightField.hidden = canvasBox.hidden = !free;
    drawCanvas();
  };
  layout.addEventListener('change', () => { syncLayout(); drawFields(); });
  height.addEventListener('input', syncLayout);
  const submitLabel = textInput({ value: body.submit_label ?? '', maxlength: 100, placeholder: t('forms.submit') });
  submitLabel.addEventListener('input', () => { body.submit_label = submitLabel.value; });
  const success = textArea({ value: body.success_message ?? '', rows: 2 });
  success.addEventListener('input', () => { body.success_message = success.value; });

  // ---------------------------------------------------------------- fields
  const list = h('ol', { class: 'ewfm-ed-list', 'aria-label': t('forms.ed.fields') });
  const open = new Set<string>();
  const palette = h('ul', { class: 'ewfm-ed-palette', 'aria-label': t('forms.ed.palette') }, ...TYPES.map((ty) => {
    const b = button(h('span', null, h('span', { 'aria-hidden': 'true' }, '+ '), typeLabel(ty)), () => addField(ty, body.fields.length), {
      variant: 'secondary', ariaLabel: t('forms.ed.add_named', { type: typeLabel(ty) }) });
    b.draggable = true;
    b.addEventListener('dragstart', (e) => e.dataTransfer?.setData(DRAG, JSON.stringify({ type: ty })));
    return h('li', null, b);
  }));

  function addField(type: string, at: number): void {
    const f: FField = { id: newId(body.fields, type), type, label: STATIC.includes(type) && type === 'divider' ? '' : typeLabel(type), width: 12 };
    if (CHOICE.includes(type)) f.options = [{ value: 'a', label: 'A' }, { value: 'b', label: 'B' }];
    if (type === 'file') { f.accept = ['pdf', 'image']; f.max_files = 1; f.max_size_mb = 10; }
    if (type === 'consent') f.required = true;
    if (type === 'repeat') { f.fields = [{ id: 'name', type: 'text', label: typeLabel('text') }]; f.min_rows = 1; f.max_rows = 20; }
    if (type === 'calc') { const nums = body.fields.filter((x) => ['number', 'range', 'rating'].includes(x.type)).map((x) => x.id); f.formula = nums.join(' + ') || '0'; }
    if (body.layout === 'free') { f.x = 16; f.y = 16 + 72 * body.fields.length; f.w = 320; f.h = 64; }
    body.fields.splice(at, 0, f);
    open.add(f.id);
    drawFields();
    announce(t('forms.ed.added', { type: typeLabel(type) }));
    list.querySelector<HTMLElement>(`[data-id="${CSS.escape(f.id)}"] .ewfm-ed-props input`)?.focus();
  }
  function move(i: number, to: number): void {
    if (to < 0 || to >= body.fields.length || to === i) return;
    const [f] = body.fields.splice(i, 1);
    body.fields.splice(to, 0, f);
    drawFields();
    announce(t('forms.ed.moved', { n: to + 1 }));
  }
  list.addEventListener('dragover', (e) => { if (e.dataTransfer?.types.includes(DRAG)) e.preventDefault(); });
  list.addEventListener('drop', (e) => {
    const raw = e.dataTransfer?.getData(DRAG);
    if (!raw) return;
    e.preventDefault();
    const d = JSON.parse(raw) as { type?: string; from?: number };
    const target = (e.target as HTMLElement).closest<HTMLElement>('li[data-index]');
    const at = target ? Number(target.dataset.index) : body.fields.length;
    if (d.type) addField(d.type, at);
    else if (d.from !== undefined) move(d.from, Math.min(at, body.fields.length - 1));
  });

  function drawFields(): void {
    list.replaceChildren(...body.fields.map((f, i) => fieldRow(f, i)));
    drawCanvas();
  }

  function fieldRow(f: FField, i: number): HTMLElement {
    const name = f.label || typeLabel(f.type);
    const handle = h('span', { class: 'ewfm-ed-handle', 'aria-hidden': 'true', title: t('forms.ed.moved', { n: '…' }) }, '⠿');
    const li = h('li', { class: 'ewfm-ed-field', 'data-index': String(i), 'data-id': f.id });
    handle.addEventListener('pointerdown', () => { li.draggable = true; });
    li.addEventListener('dragstart', (e) => e.dataTransfer?.setData(DRAG, JSON.stringify({ from: i })));
    li.addEventListener('dragend', () => { li.draggable = false; });
    const isOpen = open.has(f.id);
    const toggle = h('button', { type: 'button', class: 'ewfm-ed-toggle', 'aria-expanded': String(isOpen), 'aria-label': t('forms.ed.edit', { label: name }) },
      h('strong', null, name), ' ', h('span', { class: 'ew-muted' }, `${typeLabel(f.type)} · ${f.id}${f.required ? ' · *' : ''}${f.show_if ? ' · ⤷' : ''}`));
    toggle.addEventListener('click', () => { if (open.has(f.id)) open.delete(f.id); else open.add(f.id); drawFields(); list.querySelector<HTMLElement>(`[data-id="${CSS.escape(f.id)}"] .ewfm-ed-toggle`)?.focus(); });
    const tools = h('span', { class: 'ewfm-ed-tools' },
      button('↑', () => move(i, i - 1), { variant: 'subtle', ariaLabel: t('forms.ed.move_up', { label: name }) }),
      button('↓', () => move(i, i + 1), { variant: 'subtle', ariaLabel: t('forms.ed.move_down', { label: name }) }),
      button('⧉', () => { const c = structuredClone(f); c.id = newId(body.fields, f.type); body.fields.splice(i + 1, 0, c); drawFields(); }, { variant: 'subtle', ariaLabel: t('forms.ed.duplicate', { label: name }) }),
      button('×', () => { body.fields.splice(i, 1); drawFields(); announce(t('forms.ed.removed', { label: name })); }, { variant: 'subtle', ariaLabel: t('forms.ed.remove', { label: name }) }));
    li.append(h('div', { class: 'ewfm-ed-head' }, handle, toggle, tools));
    if (isOpen) li.append(props(f, i));
    return li;
  }

  function props(f: FField, i: number): HTMLElement {
    const box = h('div', { class: 'ewfm-ed-props' });
    const txt = (label: string, key: keyof FField, opts: { type?: string; help?: string; area?: boolean } = {}) => {
      const v = f[key];
      const el = opts.area ? textArea({ value: String(v ?? ''), rows: 2 }) : textInput({ type: opts.type, value: v === undefined || v === null ? '' : String(v) });
      el.addEventListener('change', () => {
        const s = el.value;
        (f as unknown as Record<string, unknown>)[key] = opts.type === 'number' ? num(s) : s;
        if (key === 'label' || key === 'id') drawFields();
      });
      box.append(field(label, el, { help: opts.help }));
    };
    const flag = (label: string, key: keyof FField) => {
      const c = checkbox(label, !!f[key]);
      c.input.addEventListener('change', () => { (f as unknown as Record<string, unknown>)[key] = c.input.checked; });
      box.append(c.el);
    };
    if (f.type !== 'divider') txt(t('forms.ed.label'), 'label', { area: f.type === 'paragraph' });
    txt(t('forms.ed.id'), 'id');
    if (STATIC.includes(f.type)) return box;
    txt(t('forms.ed.help'), 'help');
    if (f.type === 'calc') {
      const above = body.fields.slice(0, i).filter((x) => !STATIC.includes(x.type) && !['file', 'signature'].includes(x.type)).map((x) => x.id);
      txt(t('forms.ed.formula'), 'formula', { help: t('forms.ed.formula_help', { ids: above.join(', ') || '—' }) });
      txt(t('forms.ed.decimals'), 'decimals', { type: 'number' });
      return box;
    }
    if (f.type === 'repeat') {
      flag(t('forms.ed.required'), 'required');
      txt(t('forms.ed.min_rows'), 'min_rows', { type: 'number' });
      txt(t('forms.ed.max_rows'), 'max_rows', { type: 'number' });
      box.append(subFields(f));
      return box;
    }
    if (f.type !== 'hidden') flag(t('forms.ed.required'), 'required');
    if (['text', 'textarea', 'email', 'tel', 'url', 'number'].includes(f.type)) txt(t('forms.ed.placeholder'), 'placeholder');
    if (!['file', 'checkboxes', 'multiselect'].includes(f.type)) txt(t('forms.ed.default'), 'default');
    if (['text', 'email', 'hidden', 'textarea'].includes(f.type)) {
      const cur = f.prefill?.startsWith('param:') ? 'param' : f.prefill ?? '';
      const pre = select([{ value: '', label: t('forms.ed.prefill.none') }, ...['user.name', 'user.email', 'user.username', 'param'].map((v) => ({ value: v, label: t(`forms.ed.prefill.${v}`) }))], cur);
      const param = textInput({ value: f.prefill?.startsWith('param:') ? f.prefill.slice(6) : '', placeholder: 'name' });
      param.setAttribute('aria-label', t('forms.ed.prefill.param'));
      const sync = () => { param.hidden = pre.value !== 'param'; f.prefill = pre.value === 'param' ? `param:${param.value.trim()}` : pre.value; };
      pre.addEventListener('change', sync);
      param.addEventListener('input', sync);
      param.hidden = pre.value !== 'param';
      box.append(field(t('forms.ed.prefill'), pre), param);
    }
    if (['text', 'email', 'tel', 'url', 'textarea'].includes(f.type)) {
      txt(t('forms.ed.min_length'), 'min_length', { type: 'number' });
      txt(t('forms.ed.max_length'), 'max_length', { type: 'number' });
    }
    if (f.type === 'text') { txt(t('forms.ed.pattern'), 'pattern'); txt(t('forms.ed.pattern_help'), 'pattern_help'); }
    if (f.type === 'textarea') txt(t('forms.ed.rows'), 'rows', { type: 'number' });
    if (['number', 'range', 'rating'].includes(f.type)) {
      if (f.type !== 'rating') txt(t('forms.ed.min'), 'min', { type: 'number' });
      txt(t('forms.ed.max'), 'max', { type: 'number' });
      if (f.type !== 'rating') txt(t('forms.ed.step'), 'step', { type: 'number' });
    }
    if (['text', 'email', 'tel', 'url'].includes(f.type)) {
      const ac = select([{ value: '', label: '—' }, ...(meta?.autocomplete ?? ['name', 'email', 'tel']).map((v) => ({ value: v, label: v }))], f.autocomplete ?? '');
      ac.addEventListener('change', () => { f.autocomplete = ac.value || undefined; });
      box.append(field(t('forms.ed.autocomplete'), ac));
    }
    if (CHOICE.includes(f.type)) {
      const opts = textArea({ value: (f.options ?? []).map((o) => (o.value === o.label ? o.label : `${o.value}|${o.label}`)).join('\n'), rows: 4 });
      opts.addEventListener('change', () => {
        f.options = opts.value.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
          const [v, ...rest] = l.split('|');
          return rest.length ? { value: v.trim(), label: rest.join('|').trim() } : { value: l, label: l };
        });
      });
      box.append(field(t('forms.ed.options'), opts));
    }
    if (f.type === 'file') {
      const kinds = Object.keys(meta?.file_kinds ?? { image: [], pdf: [], office: [], text: [], archive: [], audio: [], video: [] });
      const acc = textInput({ value: (f.accept ?? []).join(', ') });
      acc.addEventListener('change', () => { f.accept = acc.value.split(',').map((s) => s.trim().toLowerCase()).filter(Boolean); });
      box.append(field(t('forms.ed.accept'), acc, { help: kinds.map((k) => `${k} = ${t(`forms.kind.${k}`)}`).join(', ') }));
      txt(t('forms.ed.max_files'), 'max_files', { type: 'number' });
      txt(t('forms.ed.max_size'), 'max_size_mb', { type: 'number' });
      flag(t('forms.ed.file_comment'), 'file_comment');
      flag(t('forms.ed.allow_macros'), 'allow_macros');
      flag(t('forms.ed.allow_active'), 'allow_active');
      const media = h('div');
      box.append(media);
      void categorySelect({ selected: f.media_category ?? 0, perm: 'edit', noneLabel: '—', onChange: (id) => { f.media_category = id || undefined; } })
        .then((cs) => media.append(cs.field(t('forms.ed.media_category'))));
      if (meta) box.append(h('p', { class: 'ew-help' }, meta.virus_scan ? t('forms.ed.virus_on') : t('forms.ed.virus_off')));
    }
    // condition: an input field above this one
    const before = body.fields.slice(0, i).filter((x) => !STATIC.includes(x.type) && x.type !== 'file');
    if (before.length) {
      const cf = select([{ value: '', label: t('forms.ed.show_always') }, ...before.map((x) => ({ value: x.id, label: x.label || x.id }))], f.show_if?.field ?? '');
      const op = select(['eq', 'ne', 'empty', 'notempty', 'contains'].map((o) => ({ value: o, label: t(`forms.ed.cond.${o}`) })), f.show_if?.op ?? 'eq');
      const val = textInput({ value: f.show_if?.value ?? '' });
      op.setAttribute('aria-label', t('forms.ed.show_if'));
      val.setAttribute('aria-label', t('forms.ed.show_if'));
      const sync = () => {
        f.show_if = cf.value ? { field: cf.value, op: op.value, value: val.value } : null;
        op.hidden = val.hidden = !cf.value;
        val.hidden ||= op.value === 'empty' || op.value === 'notempty';
      };
      [cf, op].forEach((x) => x.addEventListener('change', sync));
      val.addEventListener('input', sync);
      sync();
      box.append(field(t('forms.ed.show_if'), cf), h('div', { class: 'ewfm-ed-row' }, op, val));
    }
    if (body.layout === 'grid') {
      const w = select(Array.from({ length: 12 }, (_, k) => ({ value: String(k + 1), label: `${k + 1}/12` })), String(f.width ?? 12));
      w.addEventListener('change', () => { f.width = Number(w.value); });
      box.append(field(t('forms.ed.width'), w));
    } else {
      const pos = h('div', { class: 'ewfm-ed-row', role: 'group', 'aria-label': t('forms.ed.position') });
      (['x', 'y', 'w', 'h'] as const).forEach((k) => {
        const inp = textInput({ type: 'number', value: String(f[k] ?? 0) });
        inp.setAttribute('aria-label', `${t('forms.ed.position')}: ${k}`);
        inp.addEventListener('change', () => { f[k] = Number(inp.value) || 0; drawCanvas(); });
        pos.append(inp);
      });
      box.append(h('p', { class: 'ew-label' }, t('forms.ed.position')), pos);
    }
    return box;
  }

  // fields of a row of a repeated group (simple types)
  function subFields(g: FField): HTMLElement {
    const ROW = ['text', 'textarea', 'email', 'tel', 'url', 'number', 'date', 'time', 'select', 'checkbox'];
    g.fields ??= [];
    const box = h('fieldset', { class: 'ew-fieldset ewfm-ed-sub' }, h('legend', null, t('forms.ed.sub_fields')));
    const draw = () => {
      box.replaceChildren(h('legend', null, t('forms.ed.sub_fields')), ...g.fields!.map((sf, k) => {
        const ty = select(ROW.map((x) => ({ value: x, label: typeLabel(x) })), sf.type);
        ty.setAttribute('aria-label', `${t('forms.ed.type')} ${k + 1}`);
        ty.addEventListener('change', () => { sf.type = ty.value; if (sf.type === 'select' && !sf.options?.length) sf.options = [{ value: 'a', label: 'A' }]; draw(); });
        const lab = textInput({ value: sf.label });
        lab.setAttribute('aria-label', `${t('forms.ed.label')} ${k + 1}`);
        lab.addEventListener('input', () => { sf.label = lab.value; });
        const id = textInput({ value: sf.id });
        id.setAttribute('aria-label', `${t('forms.ed.id')} ${k + 1}`);
        id.addEventListener('input', () => { sf.id = id.value.trim(); });
        const req = checkbox(t('forms.ed.required'), !!sf.required);
        req.input.addEventListener('change', () => { sf.required = req.input.checked; });
        const row = h('div', { class: 'ewfm-ed-row' }, ty, lab, id, req.el,
          button('×', () => { g.fields!.splice(k, 1); draw(); }, { variant: 'subtle', ariaLabel: t('forms.ed.remove', { label: sf.label || sf.id }) }));
        if (sf.type === 'select') {
          const opts = textArea({ value: (sf.options ?? []).map((o) => (o.value === o.label ? o.label : `${o.value}|${o.label}`)).join('\n'), rows: 2 });
          opts.setAttribute('aria-label', `${t('forms.ed.options')} ${k + 1}`);
          opts.addEventListener('change', () => { sf.options = opts.value.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => { const [v, ...r] = l.split('|'); return r.length ? { value: v.trim(), label: r.join('|').trim() } : { value: l, label: l }; }); });
          return h('div', null, row, opts);
        }
        return row;
      }), button(t('forms.ed.sub_add'), () => { g.fields!.push({ id: newId(g.fields!, 'text'), type: 'text', label: typeLabel('text') }); draw(); }, { variant: 'secondary' }));
    };
    draw();
    return box;
  }

  // ---------------------------------------------------------------- free layout canvas
  function drawCanvas(): void {
    if (body.layout !== 'free') { canvasBox.replaceChildren(); return; }
    const canvas = h('div', { class: 'ewfm-ed-canvas', style: `height:${body.height ?? 600}px` });
    body.fields.forEach((f, i) => {
      f.w ||= 320; f.h ||= 64;
      const box = h('button', { type: 'button', class: 'ewfm-ed-box', style: `left:${f.x ?? 0}px;top:${f.y ?? 0}px;width:${f.w}px;height:${f.h}px`,
        'aria-label': `${i + 1}. ${f.label || typeLabel(f.type)}: x ${f.x ?? 0}, y ${f.y ?? 0}, ${f.w} × ${f.h}` }, `${i + 1}. ${f.label || typeLabel(f.type)}`);
      // keyboard: arrows move, with Shift resize (10 px; with Alt 1 px)
      box.addEventListener('keydown', (e) => {
        const d = e.altKey ? 1 : 10;
        const dx = e.key === 'ArrowLeft' ? -d : e.key === 'ArrowRight' ? d : 0;
        const dy = e.key === 'ArrowUp' ? -d : e.key === 'ArrowDown' ? d : 0;
        if (!dx && !dy) return;
        e.preventDefault();
        if (e.shiftKey) { f.w = Math.max(40, (f.w ?? 0) + dx); f.h = Math.max(24, (f.h ?? 0) + dy); } else { f.x = Math.max(0, (f.x ?? 0) + dx); f.y = Math.max(0, (f.y ?? 0) + dy); }
        place(box, f, i);
      });
      // pointer: drag moves, the corner resizes
      box.addEventListener('pointerdown', (e) => {
        const r = box.getBoundingClientRect();
        const resize = e.clientX > r.right - 14 && e.clientY > r.bottom - 14;
        const sx = e.clientX, sy = e.clientY, ox = f.x ?? 0, oy = f.y ?? 0, ow = f.w ?? 0, oh = f.h ?? 0;
        box.setPointerCapture(e.pointerId);
        const mv = (ev: PointerEvent) => {
          const dx = Math.round((ev.clientX - sx) / 4) * 4, dy = Math.round((ev.clientY - sy) / 4) * 4;
          if (resize) { f.w = Math.max(40, ow + dx); f.h = Math.max(24, oh + dy); } else { f.x = Math.max(0, ox + dx); f.y = Math.max(0, oy + dy); }
          place(box, f, i);
        };
        const up = () => { box.removeEventListener('pointermove', mv); box.removeEventListener('pointerup', up); };
        box.addEventListener('pointermove', mv);
        box.addEventListener('pointerup', up);
      });
      canvas.append(box);
    });
    canvasBox.replaceChildren(h('p', { class: 'ew-help' }, t('forms.ed.position')), canvas);
  }
  function place(box: HTMLElement, f: FField, i: number): void {
    box.style.left = `${f.x}px`; box.style.top = `${f.y}px`; box.style.width = `${f.w}px`; box.style.height = `${f.h}px`;
    box.setAttribute('aria-label', `${i + 1}. ${f.label || typeLabel(f.type)}: x ${f.x}, y ${f.y}, ${f.w} × ${f.h}`);
  }

  // ---------------------------------------------------------------- limits
  const limits = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.limits')));
  const flagB = (label: string, key: 'signed_in' | 'once_per_user' | 'copy_to_submitter' | 'proof_of_work' | 'public_results') => {
    const c = checkbox(label, !!body[key]);
    c.input.addEventListener('change', () => { body[key] = c.input.checked; });
    limits.append(c.el);
  };
  flagB(t('forms.ed.signed_in'), 'signed_in');
  flagB(t('forms.ed.once'), 'once_per_user');
  flagB(t('forms.ed.copy'), 'copy_to_submitter');
  flagB(t('forms.ed.pow'), 'proof_of_work');
  flagB(t('forms.ed.public_results'), 'public_results');
  const dt = (label: string, key: 'opens_at' | 'closes_at') => {
    const i = textInput({ type: 'datetime-local', value: localDT(body[key]) });
    i.addEventListener('change', () => { body[key] = i.value ? new Date(i.value).toISOString() : null; });
    limits.append(field(label, i));
  };
  dt(t('forms.ed.opens'), 'opens_at');
  dt(t('forms.ed.closes'), 'closes_at');
  const nf = (label: string, key: 'max_submissions' | 'retention_days') => {
    const i = textInput({ type: 'number', value: String(body[key] ?? 0) });
    i.addEventListener('change', () => { body[key] = Math.max(0, Number(i.value) || 0); });
    limits.append(field(label, i));
  };
  nf(t('forms.ed.max_submissions'), 'max_submissions');
  nf(t('forms.ed.retention'), 'retention_days');

  // ---------------------------------------------------------------- targets
  const targets = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.targets')));
  const store = checkbox(t('forms.ed.store'), body.targets.store !== false);
  store.input.addEventListener('change', () => { body.targets.store = store.input.checked; });
  const emails = h('div', { class: 'ewfm-ed-emails' });
  const drawEmails = () => {
    emails.replaceChildren(...(body.targets.email ?? []).map((m, i) => {
      const to = textInput({ value: m.to });
      to.addEventListener('input', () => { m.to = to.value; });
      const subj = textInput({ value: m.subject, maxlength: 300 });
      subj.addEventListener('input', () => { m.subject = subj.value; });
      const text = textArea({ value: m.body, rows: 3 });
      text.addEventListener('input', () => { m.body = text.value; });
      return h('fieldset', { class: 'ew-fieldset' }, h('legend', null, `${t('forms.ed.email')} ${i + 1}`),
        field(t('forms.ed.email_to'), to), field(t('forms.ed.email_subject'), subj), field(t('forms.ed.email_body'), text, { help: t('forms.ed.placeholders') }),
        button(t('common.remove'), () => { body.targets.email!.splice(i, 1); drawEmails(); }, { variant: 'subtle' }));
    }), button(t('forms.ed.email_add'), () => { body.targets.email!.push({ to: '', subject: '', body: '' }); drawEmails(); }, { variant: 'secondary' }));
  };
  drawEmails();
  const hookURL = textInput({ type: 'url', value: body.targets.webhook?.url ?? '', placeholder: 'https://' });
  // the secret is never sent back: empty keeps the stored one
  const hookSecret = textInput({ value: '', type: 'password', autocomplete: 'new-password',
    placeholder: body.targets.webhook?.secret_ref ? t('forms.ed.webhook_secret_kept') : '' });
  const syncHook = () => {
    const ref = body.targets.webhook?.secret_ref;
    body.targets.webhook = hookURL.value.trim() ? { url: hookURL.value.trim(), secret: hookSecret.value || undefined, secret_ref: hookSecret.value ? undefined : ref } : null;
  };
  hookURL.addEventListener('input', syncHook);
  hookSecret.addEventListener('input', syncHook);
  const mapping = (target: string, map: Record<string, string>) => {
    const box = h('div', { class: 'ewfm-ed-map' }, h('p', { class: 'ew-label' }, t('forms.ed.mapping', { target })));
    for (const f of body.fields.filter((x) => !STATIC.includes(x.type))) {
      const i = textInput({ value: map[f.id] ?? '', placeholder: f.id });
      i.setAttribute('aria-label', t('forms.ed.target_key', { label: f.label || f.id }));
      i.addEventListener('input', () => { if (i.value.trim()) map[f.id] = i.value.trim(); else delete map[f.id]; });
      box.append(h('div', { class: 'ewfm-ed-row' }, h('span', null, f.label || f.id), i));
    }
    return box;
  };
  const propBox = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.propdb')));
  const propTitle = textInput({ value: body.targets.propdb?.title ?? '' });
  propTitle.addEventListener('input', () => { if (body.targets.propdb) body.targets.propdb.title = propTitle.value; });
  void elementSelect({ selected: body.targets.propdb?.element_id ?? 0, type: 'content.propdb', onChange: (id) => {
    body.targets.propdb = id ? { element_id: id, title: propTitle.value, mapping: body.targets.propdb?.mapping ?? {} } : null;
    drawMaps();
  } }).then((s) => propBox.insertBefore(field(t('forms.ed.propdb_element'), s.el), propBox.children[1] ?? null));
  propBox.append(field(t('forms.ed.propdb_title'), propTitle, { help: t('forms.ed.placeholders') }));
  const propMap = h('div');
  propBox.append(propMap);
  const wfBox = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.workflow')));
  void elementSelect({ selected: body.targets.workflow?.element_id ?? 0, type: 'workflow.process', onChange: (id) => {
    body.targets.workflow = id ? { element_id: id, mapping: body.targets.workflow?.mapping ?? {} } : null;
    drawMaps();
  } }).then((s) => wfBox.insertBefore(field(t('forms.ed.workflow_element'), s.el), wfBox.children[1] ?? null));
  const wfMap = h('div');
  wfBox.append(wfMap);
  const drawMaps = () => {
    propMap.replaceChildren(body.targets.propdb ? mapping(t('forms.ed.propdb'), body.targets.propdb.mapping) : '');
    wfMap.replaceChildren(body.targets.workflow ? mapping(t('forms.ed.workflow'), body.targets.workflow.mapping) : '');
  };
  drawMaps();
  // files in the media library
  const filesBox = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.files')));
  const fname = textInput({ value: body.targets.files?.name ?? '' });
  const formats = ['pdf', 'json', 'csv', 'txt'].map((fm) => {
    const c = checkbox(fm.toUpperCase(), (body.targets.files?.formats ?? ['pdf']).includes(fm));
    c.input.dataset.format = fm;
    return c;
  });
  const syncFiles = (cat?: number) => {
    const id = cat ?? body.targets.files?.category_id ?? 0;
    body.targets.files = id ? { category_id: id, name: fname.value, formats: formats.filter((c) => c.input.checked).map((c) => c.input.dataset.format!) } : null;
  };
  fname.addEventListener('input', () => syncFiles());
  formats.forEach((c) => c.input.addEventListener('change', () => syncFiles()));
  void categorySelect({ selected: body.targets.files?.category_id ?? 0, perm: 'edit', noneLabel: '—', onChange: (id) => syncFiles(id || 0) })
    .then((cs) => filesBox.insertBefore(cs.field(t('forms.ed.files_category')), filesBox.children[1] ?? null));
  filesBox.append(h('div', { class: 'ewfm-ed-row', role: 'group', 'aria-label': t('forms.ed.files_formats') }, ...formats.map((c) => c.el)),
    field(t('forms.ed.files_name'), fname, { help: t('forms.ed.placeholders') }));
  targets.append(store.el, h('p', { class: 'ew-label' }, t('forms.ed.email')), emails,
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.webhook')), field(t('forms.ed.webhook_url'), hookURL), field(t('forms.ed.webhook_secret'), hookSecret, { help: t('forms.ed.webhook_secret_help') })),
    propBox, wfBox, filesBox);

  container.append(h('div', { class: 'ewfm-ed' },
    field(t('forms.ed.intro'), intro),
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('forms.ed.layout')), field(t('forms.ed.layout'), layout), layoutHint, heightField),
    h('section', { class: 'ewfm-ed-fields', 'aria-label': t('forms.ed.fields') },
      h('h2', { class: 'ewfm-ed-h' }, t('forms.ed.fields')),
      h('div', { class: 'ewfm-ed-layout' }, h('div', { class: 'ewfm-ed-palettebox' }, h('p', { class: 'ew-label' }, t('forms.ed.palette')), palette), list)),
    canvasBox,
    field(t('forms.ed.submit_label'), submitLabel), field(t('forms.ed.success'), success),
    limits, targets));
  syncLayout();
  drawFields();

  return {
    getBody: () => body,
    validate: () => {
      const ids = new Set<string>();
      for (const f of body.fields) {
        if (!/^[a-z][a-z0-9_]{0,39}$/.test(f.id)) return `${t('forms.ed.id')}: ${f.id}`;
        if (ids.has(f.id)) return `${t('forms.ed.id')}: ${f.id}`;
        ids.add(f.id);
      }
      return null;
    },
  };
}
