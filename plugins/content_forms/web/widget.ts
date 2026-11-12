// The form on the page: conditions, prefilling, files by drag and drop or
// the file dialog, checks with an error summary (WCAG 3.3.1/3.3.3) and the
// submission. The server checks everything again.
import { announce, h, humanSize, state, t } from '@ewiki/sdk';

export const BASE = 'x/content.forms';

interface FieldError { field: string; code: string; param?: string }
interface Start { token: string; state: string; prefill: Record<string, string>; signed_in: boolean }
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
  form.addEventListener('input', applyConditions);
  form.addEventListener('change', applyConditions);
  applyConditions();

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
  function showErrors(errs: FieldError[]): void {
    wrappers.forEach((w) => setError(w, ''));
    if (!errs.length) { summary.hidden = true; return; }
    const items: HTMLElement[] = [];
    for (const e of errs) {
      const w = wrappers.find((x) => x.dataset.field === e.field);
      const msg = t(`forms.err.${e.code}`, { param: e.param ?? '' });
      if (w) {
        const prev = w.querySelector<HTMLElement>('.ewfm-error')?.textContent;
        setError(w, prev ? `${prev} ${msg}` : msg);
      }
      const label = (w?.querySelector('label, legend')?.textContent ?? e.field).replace(/\s*\*.*$/, '').replace(/[:?]\s*$/, '');
      const target = w ? control(w) : null;
      items.push(h('li', null, h('a', { href: target ? `#${target.id}` : '#', onclick: (ev: Event) => { ev.preventDefault(); target?.focus(); } }, `${label} – ${msg}`)));
    }
    summary.replaceChildren(h('p', null, t('forms.errors_title', { n: errs.length })), h('ul', null, ...items));
    summary.hidden = false;
    summary.focus();
  }
  // checks in the browser (the server checks again)
  function check(): FieldError[] {
    const errs: FieldError[] = [];
    for (const w of wrappers) {
      if (w.hidden) continue;
      const name = w.dataset.field!;
      const type = w.dataset.type!;
      const v = valueOf(name);
      const required = !!w.querySelector('[required], fieldset[data-required]');
      const empty = v === '' || v === false || (Array.isArray(v) && v.length === 0);
      if (required && empty) { errs.push({ field: name, code: type === 'consent' ? 'consent' : w.querySelector('fieldset') ? 'choose' : 'required' }); continue; }
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

  // ---------------------------------------------------------------- submit
  submit.disabled = false;
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    applyConditions();
    const errs = check();
    if (errs.length) { showErrors(errs); return; }
    showErrors([]);
    const values: Record<string, unknown> = {};
    for (const w of wrappers) {
      const n = w.dataset.field!;
      if (w.hidden || ['heading', 'paragraph', 'divider', 'file'].includes(w.dataset.type!)) continue;
      values[n] = valueOf(n);
    }
    const hp = form.querySelector<HTMLInputElement>('input[name=website]')?.value ?? '';
    const fd = new FormData();
    fd.append('data', JSON.stringify({ values, token: start.token, website: hp, lang: state.lang }));
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
