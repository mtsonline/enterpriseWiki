// The form on the page: conditions, prefilling, files by drag and drop or
// the file dialog, checks with an error summary (WCAG 3.3.1/3.3.3) and the
// submission. The server checks everything again.
import { announce, button, h, humanSize, state, t } from '@ewiki/sdk';

export const BASE = 'x/content.forms';

interface FieldError { field: string; code: string; param?: string }
interface Start { token: string; state: string; prefill: Record<string, string>; signed_in: boolean; pow_bits?: number }
interface Cond { field: string; op: string; value?: string }

export async function formWidget(root: HTMLElement): Promise<void> {
  const id = Number(root.dataset.element);
  const form = root.querySelector<HTMLFormElement>('form.ewfm-form');
  if (!id || !form) return;
  const submit = form.querySelector<HTMLButtonElement>('button[type=submit]')!;
  const status = form.querySelector<HTMLElement>('.ewfm-status')!;
  const summary = form.querySelector<HTMLElement>('.ewfm-summary')!;
  const wrappers = Array.from(form.querySelectorAll<HTMLElement>('.ewfm-field'));
  const files = new Map<string, { file: File; comment: string }[]>();
  const signatures = new Map<string, { drawn: () => boolean; blob: () => Promise<Blob | null> }>();

  let start: Start;
  try {
    const res = await fetch(`/api/v1/${BASE}/elements/${id}/start?lang=${encodeURIComponent(state.lang)}`, { credentials: 'same-origin', headers: { 'X-Requested-With': 'ewiki' } });
    if (!res.ok) throw new Error(String(res.status));
    start = (await res.json()) as Start;
  } catch {
    status.textContent = t('forms.error');
    return;
  }
  if (start.state) {
    status.textContent = t(`forms.state.${start.state}`);
    status.classList.add('ewfm-status--info');
    form.querySelector('.ewfm-fields')?.setAttribute('inert', '');
    return;
  }

  // prefill: signed-in person and URL parameters
  const params = new URLSearchParams(location.search);
  form.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('[data-prefill]').forEach((el) => {
    const p = el.dataset.prefill ?? '';
    const v = p.startsWith('param:') ? params.get(p.slice(6)) : start.prefill[el.name];
    if (v && !el.value) el.value = v;
  });

  // proof of work in the background (forms that ask for it)
  const nonce: Promise<string> | null = start.pow_bits ? solvePow(start.token, start.pow_bits) : null;

  // sliders show their value
  form.querySelectorAll<HTMLInputElement>('input[type=range]').forEach((r) => {
    const out = form.querySelector<HTMLOutputElement>(`output[for="${CSS.escape(r.id)}"]`);
    const upd = () => { if (out) out.value = r.value; };
    r.addEventListener('input', upd);
    upd();
  });

  // ---------------------------------------------------------------- values
  const valueOf = (name: string): string | string[] | boolean => {
    const w = wrappers.find((x) => x.dataset.field === name);
    if (!w || w.hidden) return '';
    switch (w.dataset.type) {
      case 'checkbox': case 'consent':
        return w.querySelector<HTMLInputElement>('input[type=checkbox]')?.checked ?? false;
      case 'checkboxes':
        return Array.from(w.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')).map((i) => i.value);
      case 'radio': case 'rating':
        return w.querySelector<HTMLInputElement>('input[type=radio]:checked')?.value ?? '';
      case 'multiselect':
        return Array.from(w.querySelector<HTMLSelectElement>('select')?.selectedOptions ?? []).map((o) => o.value);
      case 'file':
        return (files.get(name)?.length ?? 0) > 0 ? 'files' : '';
      case 'signature':
        return (w.querySelector<HTMLInputElement>('input[type=text]')?.value.trim() || (signatures.get(name)?.drawn() ? 'drawn' : ''));
      case 'calc':
        return w.querySelector('output')?.textContent ?? '';
      case 'repeat':
        return rowsOf(w).length ? 'rows' : '';
      default:
        return (w.querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select')?.value ?? '').trim();
    }
  };
  const asText = (v: string | string[] | boolean) => (typeof v === 'boolean' ? (v ? 'true' : '') : Array.isArray(v) ? v.join(',') : v);
  const visible = (c: Cond) => {
    const v = valueOf(c.field);
    const s = asText(v);
    const list = Array.isArray(v) ? v : [];
    switch (c.op) {
      case 'empty': return s === '';
      case 'notempty': return s !== '';
      case 'eq': return s === c.value || list.includes(c.value ?? '');
      case 'ne': return s !== c.value && !list.includes(c.value ?? '');
      case 'contains': return s.toLowerCase().includes((c.value ?? '').toLowerCase());
    }
    return true;
  };
  const applyConditions = () => {
    for (const w of wrappers) {
      const raw = w.dataset.showIf;
      if (!raw) continue;
      let show = true;
      try { show = visible(JSON.parse(raw) as Cond); } catch { /* malformed: shown */ }
      if (w.hidden === !show) continue;
      w.hidden = !show;
      // hidden fields are not focusable and not sent
      w.querySelectorAll<HTMLInputElement>('input, select, textarea, button').forEach((el) => { el.disabled = !show; });
    }
  };
  // computed fields
  const outputs = Array.from(form.querySelectorAll<HTMLOutputElement>('output.ewfm-calc'));
  const compute = () => {
    for (const o of outputs) {
      const vars: Record<string, number> = {};
      for (const w of wrappers) {
        const n = w.dataset.field!;
        if (n === o.name) break; // only fields above
        vars[n] = toNumber(valueOf(n));
      }
      const r = evalFormula(o.dataset.formula ?? '', vars);
      const d = Number(o.dataset.decimals) || 0;
      o.value = Number.isFinite(r) ? (Math.round(r * 10 ** d) / 10 ** d).toLocaleString(state.lang, { minimumFractionDigits: d, maximumFractionDigits: d }) : '0';
    }
  };
  const onChange = () => { applyConditions(); compute(); };
  form.addEventListener('input', onChange);
  form.addEventListener('change', onChange);
  onChange();

  // signatures: drawn with pointer, pen or finger, or typed
  form.querySelectorAll<HTMLElement>('.ewfm-sigpad').forEach((pad) => {
    const name = pad.dataset.field!;
    const canvas = h('canvas', { width: 600, height: 180, class: 'ewfm-sig-canvas', role: 'img', 'aria-label': t('forms.sig_canvas') }) as HTMLCanvasElement;
    const g = canvas.getContext('2d')!;
    g.lineWidth = 2.5; g.lineCap = 'round'; g.strokeStyle = '#111';
    let drawing = false, strokes = 0;
    const at = (e: PointerEvent) => { const r = canvas.getBoundingClientRect(); return [(e.clientX - r.left) * canvas.width / r.width, (e.clientY - r.top) * canvas.height / r.height]; };
    canvas.addEventListener('pointerdown', (e) => { drawing = true; canvas.setPointerCapture(e.pointerId); const [x, y] = at(e); g.beginPath(); g.moveTo(x, y); });
    canvas.addEventListener('pointermove', (e) => { if (!drawing) return; const [x, y] = at(e); g.lineTo(x, y); g.stroke(); });
    canvas.addEventListener('pointerup', () => { if (drawing) { strokes++; onChange(); } drawing = false; });
    const clear = button(t('forms.sig_clear'), () => { g.clearRect(0, 0, canvas.width, canvas.height); strokes = 0; onChange(); announce(t('forms.sig_cleared')); }, { variant: 'subtle' });
    pad.append(canvas, clear);
    signatures.set(name, { drawn: () => strokes > 0, blob: () => new Promise((res) => (strokes > 0 ? canvas.toBlob(res, 'image/png') : res(null))) });
  });

  // repeated groups: rows from the template, add and remove (keyboard: buttons)
  const rowsOf = (w: HTMLElement) => Array.from(w.querySelectorAll<HTMLElement>(':scope fieldset > .ewfm-rows > li'));
  const subValue = (sw: HTMLElement): unknown => {
    if (sw.dataset.type === 'checkbox') return sw.querySelector<HTMLInputElement>('input[type=checkbox]')?.checked ?? false;
    return (sw.querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>('input, textarea, select')?.value ?? '').trim();
  };
  const rowsValue = (w: HTMLElement) => rowsOf(w).map((li) => {
    const row: Record<string, unknown> = {};
    li.querySelectorAll<HTMLElement>('[data-sub]').forEach((sw) => { row[sw.dataset.sub!] = subValue(sw); });
    return row;
  });
  let rowSeq = 0;
  for (const w of wrappers.filter((x) => x.dataset.type === 'repeat')) {
    const fs = w.querySelector<HTMLFieldSetElement>('fieldset')!;
    const tpl = fs.querySelector('template')!;
    const ol = fs.querySelector<HTMLOListElement>('.ewfm-rows')!;
    const min = Number(fs.dataset.min) || 0;
    const max = Number(fs.dataset.max) || 20;
    const label = fs.querySelector('legend')?.textContent?.replace(/\s*\*.*$/, '') ?? '';
    const add = button(t('forms.row_add', { label }), () => { addRow(true); }, { variant: 'secondary' });
    const renumber = () => {
      rowsOf(w).forEach((li, i) => {
        li.querySelector('.ewfm-row-title')!.textContent = t('forms.row_n', { label, n: i + 1 });
        const rm = li.querySelector<HTMLButtonElement>('.ewfm-row-remove')!;
        rm.setAttribute('aria-label', t('forms.row_remove', { label, n: i + 1 }));
        rm.hidden = rowsOf(w).length <= Math.max(min, fs.dataset.required ? 1 : 0);
      });
      add.disabled = rowsOf(w).length >= max;
    };
    const addRow = (focus: boolean) => {
      const html = tpl.innerHTML.replaceAll('__N__', String(++rowSeq));
      const holder = document.createElement('template');
      holder.innerHTML = html; // our own server-rendered markup
      const li = holder.content.firstElementChild as HTMLElement;
      li.append(h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ewfm-row-remove', onclick: () => {
        const prev = li.previousElementSibling as HTMLElement | null;
        li.remove(); renumber(); onChange();
        announce(t('forms.row_removed'));
        (prev?.querySelector<HTMLElement>('input, select, textarea') ?? add).focus();
      } }, t('common.remove')));
      ol.append(li);
      renumber();
      onChange();
      if (focus) { li.querySelector<HTMLElement>('input, select, textarea')?.focus(); announce(t('forms.row_added', { n: rowsOf(w).length })); }
    };
    ol.after(add);
    for (let i = 0; i < Math.max(min, 1); i++) addRow(false);
  }

  // forms in several steps: one page at a time
  const pages: HTMLElement[][] = [[]];
  const titles: string[] = [''];
  for (const w of wrappers) {
    if (w.dataset.type === 'page') {
      if (pages[pages.length - 1].length || titles.length > 1 || titles[0]) { pages.push([]); titles.push(''); }
      titles[titles.length - 1] = w.querySelector('h2')?.textContent ?? '';
    }
    pages[pages.length - 1].push(w);
  }
  let page = 0;
  const progress = h('p', { class: 'ewfm-progress', 'aria-live': 'polite' });
  const back = button(t('forms.back'), () => showPage(page - 1, true), { variant: 'secondary' });
  const next = button(t('forms.next'), () => {
    const errs = check(pages[page]);
    if (errs.length) { showErrors(errs); return; }
    showErrors([]);
    showPage(page + 1, true);
  }, { variant: 'primary' });
  const showPage = (n: number, focus: boolean) => {
    page = Math.max(0, Math.min(pages.length - 1, n));
    pages.forEach((ws, i) => ws.forEach((w) => w.classList.toggle('ewfm-off', i !== page)));
    progress.textContent = t('forms.step', { n: page + 1, of: pages.length }) + (titles[page] ? `: ${titles[page]}` : '');
    back.hidden = page === 0;
    next.hidden = page === pages.length - 1;
    submit.hidden = page !== pages.length - 1;
    if (focus) {
      const first = pages[page].find((w) => !w.hidden && w.dataset.type !== 'page');
      (first ? control(first) : null)?.focus();
    }
  };
  if (pages.length > 1) {
    form.querySelector('.ewfm-fields')!.before(progress);
    submit.parentElement!.prepend(back, next);
    showPage(0, false);
  }

  // ---------------------------------------------------------------- files
  for (const w of wrappers.filter((x) => x.dataset.type === 'file')) {
    const name = w.dataset.field!;
    const input = w.querySelector<HTMLInputElement>('input[type=file]')!;
    const drop = w.querySelector<HTMLElement>('.ewfm-drop')!;
    const list = w.querySelector<HTMLElement>('.ewfm-files')!;
    const maxFiles = Number(input.dataset.maxFiles) || 1;
    const maxMB = Number(input.dataset.maxMb) || 10;
    const accept = (input.accept || '').split(',').filter(Boolean);
    const comment = input.dataset.comment === 'true';
    files.set(name, []);
    drop.append(h('span', { class: 'ewfm-drop-hint', 'aria-hidden': 'true' }, t('forms.files_drop')));
    const render = () => {
      const items = files.get(name)!;
      list.replaceChildren(...items.map((it, i) => h('li', null,
        h('span', { class: 'ewfm-file-name' }, it.file.name), ' ', h('span', { class: 'ew-muted' }, humanSize(it.file.size)),
        comment ? h('input', { class: 'ew-input ewfm-file-comment', type: 'text', maxlength: 1000, value: it.comment, 'aria-label': t('forms.file_comment', { name: it.file.name }),
          oninput: (e: Event) => { it.comment = (e.target as HTMLInputElement).value; } }) : null,
        h('button', { type: 'button', class: 'ew-btn ew-btn--subtle', 'aria-label': t('forms.file_remove', { name: it.file.name }),
          onclick: () => { items.splice(i, 1); render(); announce(t('forms.file_remove', { name: it.file.name })); input.focus(); applyConditions(); } }, '×'))));
    };
    const add = (picked: FileList | File[]) => {
      const items = files.get(name)!;
      const problems: string[] = [];
      for (const f of Array.from(picked)) {
        const ext = '.' + (f.name.split('.').pop() ?? '').toLowerCase();
        if (accept.length && !accept.includes(ext)) problems.push(t('forms.err.file_type', { param: f.name }));
        else if (f.size > maxMB * 1024 * 1024) problems.push(t('forms.err.file_size', { param: f.name }));
        else if (items.length >= maxFiles) problems.push(t('forms.err.files', { param: String(maxFiles) }));
        else items.push({ file: f, comment: '' });
      }
      render();
      setError(w, problems.join(' '));
      announce(problems.length ? problems.join(' ') : items.map((x) => x.file.name).join(', '));
      applyConditions();
    };
    input.addEventListener('change', () => { if (input.files) add(input.files); input.value = ''; });
    drop.addEventListener('dragover', (e) => { e.preventDefault(); drop.classList.add('is-over'); });
    drop.addEventListener('dragleave', () => drop.classList.remove('is-over'));
    drop.addEventListener('drop', (e) => { e.preventDefault(); drop.classList.remove('is-over'); if (e.dataTransfer?.files) add(e.dataTransfer.files); });
  }

  // ---------------------------------------------------------------- errors
  function control(w: HTMLElement): HTMLElement | null {
    return w.querySelector<HTMLElement>('input:not([type=hidden]), select, textarea');
  }
  function setError(w: HTMLElement, msg: string): void {
    const p = w.querySelector<HTMLElement>('.ewfm-error');
    if (p) { p.textContent = msg; p.hidden = !msg; }
    w.classList.toggle('has-error', !!msg);
    const group = w.querySelector<HTMLElement>('fieldset');
    const el = group ?? control(w);
    if (msg) el?.setAttribute('aria-invalid', 'true'); else el?.removeAttribute('aria-invalid');
  }
  // a field or a field of a row of a repeated group ("group.ROW.field")
  function findWrapper(path: string): HTMLElement | undefined {
    const [name, row, sub] = path.split('.');
    const w = wrappers.find((x) => x.dataset.field === name);
    if (!w || sub === undefined) return w;
    return rowsOf(w)[Number(row)]?.querySelector<HTMLElement>(`[data-sub="${CSS.escape(sub)}"]`) ?? w;
  }
  function showErrors(errs: FieldError[]): void {
    wrappers.forEach((w) => setError(w, ''));
    root.querySelectorAll<HTMLElement>('[data-sub]').forEach((w) => setError(w, ''));
    if (!errs.length) { summary.hidden = true; return; }
    const items: HTMLElement[] = [];
    for (const e of errs) {
      const w = findWrapper(e.field);
      const msg = t(`forms.err.${e.code}`, { param: e.param ?? '' });
      if (w) {
        const prev = w.querySelector<HTMLElement>('.ewfm-error')?.textContent;
        setError(w, prev ? `${prev} ${msg}` : msg);
      }
      const label = (w?.querySelector('label, legend')?.textContent ?? e.field).replace(/\s*\*.*$/, '').replace(/[:?]\s*$/, '');
      const rowTitle = w?.dataset.sub ? w.closest('li')?.querySelector('.ewfm-row-title')?.textContent : '';
      const shown = rowTitle ? `${rowTitle}: ${label}` : label;
      const target = w ? control(w) : null;
      items.push(h('li', null, h('a', { href: target ? `#${target.id}` : '#', onclick: (ev: Event) => { ev.preventDefault(); target?.focus(); } }, `${shown} – ${msg}`)));
    }
    summary.replaceChildren(h('p', null, t('forms.errors_title', { n: errs.length })), h('ul', null, ...items));
    summary.hidden = false;
    summary.focus();
  }
  // checks in the browser (the server checks again)
  function check(only?: HTMLElement[]): FieldError[] {
    const errs: FieldError[] = [];
    for (const w of only ?? wrappers) {
      if (w.hidden) continue;
      const name = w.dataset.field!;
      const type = w.dataset.type!;
      const v = valueOf(name);
      const required = !!w.querySelector('[required], fieldset[data-required]');
      const empty = v === '' || v === false || (Array.isArray(v) && v.length === 0);
      if (required && empty) { errs.push({ field: name, code: type === 'consent' ? 'consent' : type === 'signature' ? 'signature' : w.querySelector('fieldset') ? 'choose' : 'required' }); continue; }
      if (type === 'signature' || type === 'calc') continue;
      if (type === 'repeat') {
        rowsOf(w).forEach((li, i) => li.querySelectorAll<HTMLElement>('[data-sub]').forEach((sw) => {
          const v = subValue(sw);
          if (sw.querySelector('[required]') && (v === '' || v === false)) errs.push({ field: `${name}.${i}.${sw.dataset.sub}`, code: 'required' });
        }));
        continue;
      }
      const c = control(w) as HTMLInputElement | null;
      if (!empty && c && typeof c.checkValidity === 'function' && !['file', 'radio', 'checkbox'].includes(c.type) && !c.checkValidity()) {
        const vs = c.validity;
        const code = vs.typeMismatch ? (type === 'url' ? 'url' : 'email') : vs.patternMismatch ? 'pattern' : vs.tooLong ? 'too_long' : vs.tooShort ? 'too_short'
          : vs.rangeUnderflow ? 'min' : vs.rangeOverflow ? 'max' : vs.badInput ? 'number' : 'invalid';
        const param = code === 'min' ? c.min : code === 'max' ? c.max : code === 'too_short' ? c.getAttribute('minlength') ?? '' : code === 'too_long' ? c.getAttribute('maxlength') ?? '' : '';
        errs.push({ field: name, code, param });
      }
    }
    return errs;
  }

  function gotoError(errs: FieldError[]): void {
    if (pages.length < 2 || !errs.length) return;
    const i = pages.findIndex((ws) => ws.some((w) => w.dataset.field === errs[0].field.split('.')[0]));
    if (i >= 0 && i !== page) showPage(i, false);
  }

  // ---------------------------------------------------------------- submit
  submit.disabled = false;
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    applyConditions();
    const errs = check();
    if (errs.length) { gotoError(errs); showErrors(errs); return; }
    showErrors([]);
    const values: Record<string, unknown> = {};
    for (const w of wrappers) {
      const n = w.dataset.field!;
      if (w.hidden || ['heading', 'paragraph', 'divider', 'page', 'file', 'calc'].includes(w.dataset.type!)) continue;
      values[n] = w.dataset.type === 'signature' ? (w.querySelector<HTMLInputElement>('input[type=text]')?.value.trim() ?? '')
        : w.dataset.type === 'repeat' ? rowsValue(w) : valueOf(n);
    }
    const hp = form.querySelector<HTMLInputElement>('input[name=website]')?.value ?? '';
    const fd = new FormData();
    if (nonce) status.textContent = t('forms.preparing');
    const solved = nonce ? await nonce.catch(() => '') : '';
    fd.append('data', JSON.stringify({ values, token: start.token, website: hp, lang: state.lang, nonce: solved }));
    for (const [name, sig] of signatures) {
      const w = wrappers.find((x) => x.dataset.field === name);
      const typed = w?.querySelector<HTMLInputElement>('input[type=text]')?.value.trim();
      const b = !w || w.hidden || typed ? null : await sig.blob();
      if (b) fd.append(`file.${name}`, b, 'signature.png');
    }
    for (const [name, items] of files) {
      const w = wrappers.find((x) => x.dataset.field === name);
      if (!w || w.hidden) continue;
      items.forEach((it, i) => {
        fd.append(`file.${name}`, it.file, it.file.name);
        if (it.comment) fd.append(`comment.${name}.${i}`, it.comment);
      });
    }
    submit.disabled = true;
    status.textContent = t('forms.sending');
    try {
      const res = await fetch(`/api/v1/${BASE}/elements/${id}/submit?lang=${encodeURIComponent(state.lang)}`, {
        method: 'POST', body: fd, credentials: 'same-origin', headers: { 'X-Requested-With': 'ewiki' } });
      const body = await res.json().catch(() => ({})) as { message?: string; error?: { message?: string; fields?: FieldError[] } };
      if (res.ok) {
        const done = h('div', { class: 'ewfm-done', role: 'status', tabindex: '-1' }, h('p', null, body.message || t('forms.sent')));
        form.replaceWith(done);
        done.focus();
        return;
      }
      if (res.status === 422 && body.error?.fields) {
        status.textContent = '';
        gotoError(body.error.fields);
        showErrors(body.error.fields);
      } else {
        status.textContent = body.error?.message ?? t('forms.error');
      }
    } catch {
      status.textContent = t('forms.error');
    }
    submit.disabled = false;
  });
}

// ---------------------------------------------------------------- formulas

function toNumber(v: string | string[] | boolean): number {
  if (typeof v === 'boolean') return v ? 1 : 0;
  if (Array.isArray(v)) return v.reduce((a, x) => a + toNumber(x), 0);
  const n = Number(String(v).replace(',', '.'));
  return Number.isFinite(n) ? n : 0;
}

/** Evaluates a formula like the server: numbers, field ids, + - * / ( ) and round, abs, min, max, sum. */
export function evalFormula(src: string, vars: Record<string, number>): number {
  let i = 0;
  const peek = () => { while (src[i] === ' ') i++; return src[i] ?? ''; };
  const expr = (): number => {
    let a = term();
    for (let c = peek(); c === '+' || c === '-'; c = peek()) { i++; const b = term(); a = c === '+' ? a + b : a - b; }
    return a;
  };
  const term = (): number => {
    let a = factor();
    for (let c = peek(); c === '*' || c === '/'; c = peek()) { i++; const b = factor(); a = c === '*' ? a * b : b === 0 ? 0 : a / b; }
    return a;
  };
  const factor = (): number => {
    const c = peek();
    if (c === '-') { i++; return -factor(); }
    if (c === '(') { i++; const v = expr(); if (peek() === ')') i++; return v; }
    const m = /^[0-9.]+/.exec(src.slice(i));
    if (m) { i += m[0].length; return Number(m[0]) || 0; }
    const id = /^[a-z][a-z0-9_]*/.exec(src.slice(i));
    if (!id) throw new Error('formula');
    i += id[0].length;
    if (peek() !== '(') return vars[id[0]] ?? 0;
    i++;
    const args: number[] = [];
    for (;;) { args.push(expr()); if (peek() === ',') { i++; continue; } break; }
    if (peek() === ')') i++;
    switch (id[0]) {
      case 'round': { const p = 10 ** Math.max(0, Math.min(10, Math.round(args[1] ?? 0))); return Math.round(args[0] * p) / p; }
      case 'abs': return Math.abs(args[0]);
      case 'min': return Math.min(...args);
      case 'max': return Math.max(...args);
      default: return args.reduce((a, b) => a + b, 0);
    }
  };
  try { return expr(); } catch { return 0; }
}

// ---------------------------------------------------------------- proof of work

/** Finds a nonce so that SHA-256(token ":" nonce) starts with bits zero bits. */
async function solvePow(token: string, bits: number): Promise<string> {
  const enc = new TextEncoder();
  const ok = (h: Uint8Array) => {
    for (let b = 0; b < bits; b++) if (h[b >> 3] & (0x80 >> (b & 7))) return false;
    return true;
  };
  for (let base = 0; base < 1 << 26; base += 512) {
    const batch = await Promise.all(Array.from({ length: 512 }, (_, k) => crypto.subtle.digest('SHA-256', enc.encode(`${token}:${base + k}`))));
    const k = batch.findIndex((d) => ok(new Uint8Array(d)));
    if (k >= 0) return String(base + k);
    await new Promise((r) => setTimeout(r, 0)); // keep the page responsive
  }
  throw new Error('pow');
}
