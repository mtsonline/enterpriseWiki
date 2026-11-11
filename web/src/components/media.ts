// Media library: upload with rights and watermark, details with versions
// and usage, image editor, view-only viewer and a picker for editors.
// Server: /api/v1/media (docs/developer/media.md).
import { api, ApiError, humanSize, qs } from '../core/api';
import { append, fmtDate, h, uid, type Child } from '../core/dom';
import { t } from '../core/i18n';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { toast } from '../core/announce';
import type { Group, MediaRule, MediaVersion, Medium, Role, Watermark, WatermarkMode } from '../core/types';
import { categorySelect } from './pickers';
import { externalDefault, officeButtons, officeURL } from './office';
import { ctx } from './widgets';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, select, textArea, textInput } from './ui';

export const MEDIA_RIGHTS = ['view', 'download', 'edit', 'manage'] as const;

/** URL of the shown file (w: width for thumbnails). */
export function mediaURL(m: Pick<Medium, 'id'>, opts: { w?: number; v?: number; download?: boolean } = {}): string {
  const base = opts.download ? `/api/v1/media/${m.id}/download` : `/api/v1/media/${m.id}/view`;
  return `${base}${qs({ w: opts.w, v: opts.v })}`;
}

export const isImage = (m: Pick<Medium, 'mime'>): boolean => /^image\/(png|jpeg|gif|webp)$/.test(m.mime);
const isPDF = (m: Pick<Medium, 'mime'>): boolean => m.mime === 'application/pdf';
const has = (m: Medium, p: string): boolean => m.permissions.includes(p);

/** Uploads a file into the media library (multipart: meta, then file). */
export async function uploadMedium(file: File, meta: Record<string, unknown>, path = '/api/v1/media'): Promise<Medium> {
  const fd = new FormData();
  fd.append('meta', JSON.stringify(meta));
  fd.append('file', file, file.name);
  let res: Response;
  try {
    res = await fetch(path, { method: 'POST', body: fd, credentials: 'same-origin', headers: { 'X-Requested-With': 'ewiki' } });
  } catch {
    throw new ApiError(0, 'network', 'network error');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, data?.error?.code ?? 'error', data?.error?.message ?? res.statusText);
  return data as Medium;
}

/** Blocks the browser's save options on view-only content (best effort: screenshots stay possible). */
function protect(el: HTMLElement): HTMLElement {
  el.addEventListener('contextmenu', (ev) => ev.preventDefault());
  el.addEventListener('dragstart', (ev) => ev.preventDefault());
  el.classList.add('ew-protected');
  return el;
}

/** Thumbnail or type icon of a medium. */
export function mediaThumb(m: Medium, size = 200): HTMLElement {
  if (isImage(m)) {
    const img = h('img', { src: mediaURL(m, { w: size }), alt: m.alt || '', loading: 'lazy', draggable: m.view_only ? 'false' : 'true' });
    return m.view_only ? protect(img) : img;
  }
  const ext = (m.name.split('.').pop() ?? '').slice(0, 5).toUpperCase();
  return h('span', { class: 'ew-media-icon', 'aria-hidden': 'true' }, isPDF(m) ? 'PDF' : ext || '?');
}

// ---------------------------------------------------------------- watermark and rights fields

function watermarkFields(w: Watermark, creating: boolean): { el: HTMLElement; value: () => Watermark } {
  const modes: WatermarkMode[] = creating ? ['none', 'uploader_only', 'uploader_view', 'viewer_view', 'viewer_all']
    : w.mode === 'uploader_only' ? ['uploader_only'] : ['none', 'uploader_view', 'viewer_view', 'viewer_all'];
  const mode = select(modes.map((m) => ({ value: m, label: t(`media.wm.${m}`) })), w.mode);
  const user = checkbox(t('media.wm.user'), w.mode === 'none' ? true : w.user);
  const date = checkbox(t('media.wm.date'), w.mode === 'none' ? true : w.date);
  const comment = textInput({ value: w.comment, maxlength: 120 });
  const opts = h('div', { class: 'ew-media-wm-opts' }, user.el, date.el, field(t('media.wm.comment'), comment));
  const warn = h('p', { class: 'ew-help ew-warn', role: 'note' }, t('media.wm.uploader_only_warn'));
  const sync = () => {
    opts.hidden = mode.value === 'none';
    warn.hidden = mode.value !== 'uploader_only' || !creating;
  };
  mode.addEventListener('change', sync);
  sync();
  return {
    el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('media.wm.title')), field(t('media.wm.mode'), mode, { help: t('media.wm.help') }), warn, opts),
    value: () => ({ mode: mode.value as WatermarkMode, user: user.input.checked, date: date.input.checked, comment: comment.value.trim() }),
  };
}

let subjects: Promise<{ groups: Group[]; roles: Role[] }> | null = null;
function loadSubjects() {
  subjects ??= Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]).then(([groups, roles]) => ({ groups, roles }));
  return subjects;
}

/** Editor for the own rights of a medium (replace category and embedding rights). */
async function rightsFields(own: boolean, rules: MediaRule[]): Promise<{ el: HTMLElement; value: () => MediaRule[] | null }> {
  const { groups, roles } = await loadSubjects();
  const enable = checkbox(t('media.own_rights'), own, { help: t('media.own_rights_help') });
  const list = h('ul', { class: 'ew-media-rules' });
  const rows: { el: HTMLElement; value: () => MediaRule | null }[] = [];
  const userNames = new Map<string, number>();
  const addRow = (r: MediaRule) => {
    const type = select(['user', 'group', 'role', 'authenticated', 'guest'].map((x) => ({ value: x, label: t(`acl.${x}`) })), r.subject_type);
    const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })), r.subject_type === 'group' ? String(r.subject_id) : undefined);
    const roleSel = select(roles.map((x) => ({ value: String(x.id), label: x.name })), r.subject_type === 'role' ? String(r.subject_id) : undefined);
    const userInput = textInput({ placeholder: t('acl.user_search') });
    const dl = h('datalist', { id: uid('users') });
    userInput.setAttribute('list', dl.id);
    let userId = r.subject_type === 'user' ? r.subject_id ?? null : null;
    if (userId) userInput.value = `#${userId}`;
    userInput.addEventListener('input', async () => {
      userId = userNames.get(userInput.value) ?? null;
      if (userInput.value.length < 2) return;
      const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(userInput.value)}`);
      dl.replaceChildren(...res.map((u) => { userNames.set(u.handle, u.id); return h('option', { value: u.handle }, u.display_name); }));
    });
    const who = h('span', { class: 'ew-media-rule-who' });
    const showWho = () => who.replaceChildren(type.value === 'group' ? groupSel : type.value === 'role' ? roleSel : type.value === 'user' ? h('span', null, userInput, dl) : '');
    type.addEventListener('change', showWho);
    showWho();
    const boxes = MEDIA_RIGHTS.map((p) => checkbox(t(`media.right.${p}`), r.perms.includes(p)));
    const row: HTMLElement = h('li', { class: 'ew-media-rule' },
      h('label', { class: 'sr-only' }, t('acl.subject_type')), type, who, ...boxes.map((b) => b.el),
      button(t('common.remove'), () => { row.remove(); rows.splice(rows.indexOf(entry), 1); }, { variant: 'subtle', ariaLabel: t('common.remove') }));
    const entry = {
      el: row,
      value: (): MediaRule | null => {
        const perms = MEDIA_RIGHTS.filter((_, i) => boxes[i].input.checked);
        if (!perms.length) return null;
        let id: number | null = null;
        if (type.value === 'group') id = Number(groupSel.value);
        if (type.value === 'role') id = Number(roleSel.value);
        if (type.value === 'user') {
          id = userId ?? userNames.get(userInput.value) ?? null;
          if (!id) throw new Error(t('acl.user_not_found'));
        }
        return { subject_type: type.value, subject_id: id, perms: [...perms] };
      },
    };
    rows.push(entry);
    list.append(row);
  };
  rules.forEach(addRow);
  const add = button(t('media.add_right'), () => addRow({ subject_type: 'group', perms: ['view', 'download'] }), { variant: 'subtle' });
  const box = h('div', { class: 'ew-media-rights' }, list, add);
  const sync = () => { box.hidden = !enable.input.checked; };
  enable.input.addEventListener('change', sync);
  sync();
  return {
    el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('media.rights')), enable.el, box),
    value: () => (enable.input.checked ? rows.map((r) => r.value()).filter((x): x is MediaRule => x !== null) : null),
  };
}

// ---------------------------------------------------------------- upload

/** Upload dialog; resolves with the new medium (null when cancelled). */
export function uploadDialog(opts: { categoryId?: number; accept?: string } = {}): Promise<Medium | null> {
  return new Promise((resolve) => {
    void (async () => {
      let done = false;
      const file = h('input', { type: 'file', class: 'ew-input', accept: opts.accept ?? '' }) as HTMLInputElement;
      const cat = await categorySelect({ selected: opts.categoryId, perm: 'create' });
      const title = textInput();
      const alt = textInput();
      const desc = textArea({ rows: 2 });
      const download = checkbox(t('media.download_allowed'), true, { help: t('media.download_help') });
      const wm = watermarkFields({ mode: 'none', user: true, date: true, comment: '' }, true);
      const rights = await rightsFields(false, []);
      const altField = field(t('media.alt'), alt, { help: t('media.alt_help') });
      file.addEventListener('change', () => {
        const f = file.files?.[0];
        if (f && !title.value) title.value = f.name.replace(/\.[^.]+$/, '');
        altField.hidden = !!f && !f.type.startsWith('image/');
      });
      formDialog(t('media.upload'), [field(t('media.file'), file, { required: true }), cat.field(t('media.category'), { required: true, help: t('media.category_help') }),
        field(t('common.title'), title), altField, field(t('common.description'), desc), download.el, wm.el, rights.el], t('media.upload'), async () => {
        const f = file.files?.[0];
        if (!f) throw new Error(t('media.file'));
        const catId = cat.value();
        if (!catId) throw new Error(t('media.category'));
        const m = await uploadMedium(f, { category_id: catId, title: title.value.trim(), alt: alt.value.trim(), description: desc.value.trim(),
          download: download.input.checked, watermark: wm.value(), acl: rights.value() });
        done = true;
        toast(t('media.uploaded'));
        resolve(m);
      }, { wide: true, onClose: () => { if (!done) resolve(null); } });
    })();
  });
}

// ---------------------------------------------------------------- viewer

/** Shows a medium; view-only content is protected and PDFs are shown page by page as images. */
export async function mediaViewer(m: Medium): Promise<HTMLElement> {
  const wrap = h('div', { class: 'ew-media-view' });
  if (isImage(m)) {
    wrap.append(h('img', { src: mediaURL(m), alt: m.alt || m.title || '', draggable: m.view_only ? 'false' : 'true' }));
  } else if (isPDF(m)) {
    const info = await api.get<{ pages?: number }>(`/api/v1/media/${m.id}/view`).catch(() => null);
    if (info && info.pages) {
      // shown as images: protected or with a watermark
      let page = 1;
      const img = h('img', { alt: t('media.page_of', { page: '1', pages: String(info.pages) }) }) as HTMLImageElement;
      const label = h('span', { 'aria-live': 'polite' });
      const show = () => {
        img.src = `/api/v1/media/${m.id}/view${qs({ page })}`;
        img.alt = t('media.page_of', { page: String(page), pages: String(info.pages) });
        label.textContent = img.alt;
      };
      const prev = button(t('media.prev_page'), () => { if (page > 1) { page--; show(); } }, { variant: 'subtle' });
      const next = button(t('media.next_page'), () => { if (page < (info.pages ?? 1)) { page++; show(); } }, { variant: 'subtle' });
      show();
      wrap.append(h('div', { class: 'ew-media-pager' }, prev, label, next), img);
    } else {
      wrap.append(h('iframe', { src: mediaURL(m), title: m.title || m.name, class: 'ew-media-pdf' }));
    }
  } else if (m.mime.startsWith('video/')) {
    wrap.append(h('video', { src: mediaURL(m), controls: true, controlslist: m.view_only ? 'nodownload' : '' }));
  } else if (m.mime.startsWith('audio/')) {
    wrap.append(h('audio', { src: mediaURL(m), controls: true, controlslist: m.view_only ? 'nodownload' : '' }));
  } else {
    wrap.append(h('p', null, m.view_only ? t('media.not_viewable') : t('media.download_to_open')));
  }
  return m.view_only ? protect(wrap) : wrap;
}

// ---------------------------------------------------------------- image editor

interface EdOp { type: string; [k: string]: unknown }

/** Image editor: steps are previewed on a canvas and applied on the server as a new version. */
export function imageEditor(m: Medium, done: (m: Medium) => void): void {
  const ops: EdOp[] = [];
  const canvas = h('canvas', { class: 'ew-imged-canvas', role: 'img', 'aria-label': t('media.editor_preview') }) as HTMLCanvasElement;
  const ctx = canvas.getContext('2d');
  const src = new Image();
  src.src = mediaURL(m, { w: 1200 });
  const scale = () => (m.width > 1200 ? 1200 / m.width : 1);
  const steps = h('ol', { class: 'ew-imged-steps', 'aria-label': t('media.editor_steps') });
  let crop: { x: number; y: number; w: number; h: number } | null = null;

  // preview: apply the steps with canvas operations (the server does the real work)
  const render = () => {
    if (!ctx || !src.complete) return;
    let cur: HTMLCanvasElement = document.createElement('canvas');
    cur.width = src.naturalWidth;
    cur.height = src.naturalHeight;
    cur.getContext('2d')?.drawImage(src, 0, 0);
    const s = scale();
    for (const o of ops) {
      const n = document.createElement('canvas');
      const c = n.getContext('2d');
      if (!c) continue;
      if (o.type === 'crop') {
        const [x, y, w, hh] = [Number(o.x) * s, Number(o.y) * s, Number(o.w) * s, Number(o.h) * s];
        n.width = Math.max(1, Math.round(w));
        n.height = Math.max(1, Math.round(hh));
        c.drawImage(cur, x, y, w, hh, 0, 0, n.width, n.height);
      } else if (o.type === 'rotate') {
        const quarter = Number(o.deg) % 180 !== 0;
        n.width = quarter ? cur.height : cur.width;
        n.height = quarter ? cur.width : cur.height;
        c.translate(n.width / 2, n.height / 2);
        c.rotate((Number(o.deg) * Math.PI) / 180);
        c.drawImage(cur, -cur.width / 2, -cur.height / 2);
      } else if (o.type === 'flip') {
        n.width = cur.width;
        n.height = cur.height;
        if (o.vertical) { c.translate(0, n.height); c.scale(1, -1); } else { c.translate(n.width, 0); c.scale(-1, 1); }
        c.drawImage(cur, 0, 0);
      } else if (o.type === 'resize') {
        const w = Number(o.w) * s || (cur.width * (Number(o.h) * s)) / cur.height;
        const hh = Number(o.h) * s || (cur.height * w) / cur.width;
        n.width = Math.max(1, Math.round(w));
        n.height = Math.max(1, Math.round(hh));
        c.drawImage(cur, 0, 0, n.width, n.height);
      } else {
        n.width = cur.width;
        n.height = cur.height;
        const f: string[] = [];
        if (o.type === 'adjust') f.push(`brightness(${100 + Number(o.brightness ?? 0)}%)`, `contrast(${100 + Number(o.contrast ?? 0)}%)`, `saturate(${100 + Number(o.saturation ?? 0)}%)`);
        if (o.type === 'grayscale') f.push('grayscale(1)');
        if (o.type === 'blur') f.push(`blur(${Number(o.radius) * s}px)`);
        c.filter = f.join(' ') || 'none';
        c.drawImage(cur, 0, 0);
      }
      cur = n;
    }
    canvas.width = cur.width;
    canvas.height = cur.height;
    ctx.drawImage(cur, 0, 0);
    if (crop) {
      ctx.strokeStyle = '#fff';
      ctx.setLineDash([6, 4]);
      ctx.lineWidth = 2;
      ctx.strokeRect(crop.x, crop.y, crop.w, crop.h);
    }
    steps.replaceChildren(...ops.map((o) => h('li', null, t(`media.op.${o.type}`))));
  };
  src.addEventListener('load', render);

  // crop with the mouse (or the numeric fields)
  const cx = textInput({ type: 'number', value: '0' });
  const cy = textInput({ type: 'number', value: '0' });
  const cw = textInput({ type: 'number', value: String(m.width) });
  const ch = textInput({ type: 'number', value: String(m.height) });
  let start: { x: number; y: number } | null = null;
  const pos = (ev: PointerEvent) => {
    const r = canvas.getBoundingClientRect();
    return { x: ((ev.clientX - r.left) * canvas.width) / r.width, y: ((ev.clientY - r.top) * canvas.height) / r.height };
  };
  canvas.addEventListener('pointerdown', (ev) => { start = pos(ev); canvas.setPointerCapture(ev.pointerId); });
  canvas.addEventListener('pointermove', (ev) => {
    if (!start) return;
    const p = pos(ev);
    crop = { x: Math.min(start.x, p.x), y: Math.min(start.y, p.y), w: Math.abs(p.x - start.x), h: Math.abs(p.y - start.y) };
    const s = scale();
    [cx.value, cy.value, cw.value, ch.value] = [crop.x, crop.y, crop.w, crop.h].map((v) => String(Math.round(v / s)));
    render();
  });
  canvas.addEventListener('pointerup', () => { start = null; });

  const add = (o: EdOp) => { ops.push(o); crop = null; render(); };
  const bright = textInput({ type: 'range', value: '0' });
  const contrast = textInput({ type: 'range', value: '0' });
  const sat = textInput({ type: 'range', value: '0' });
  for (const r of [bright, contrast, sat]) { r.min = '-100'; r.max = '100'; }
  const blur = textInput({ type: 'number', value: '2' });
  const rw = textInput({ type: 'number', value: String(m.width) });
  const note = textInput({ placeholder: t('media.version_note') });

  const tools = h('div', { class: 'ew-imged-tools' },
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('media.op.crop')), h('p', { class: 'ew-help' }, t('media.crop_help')),
      h('div', { class: 'ew-imged-row' }, field('X', cx), field('Y', cy), field(t('media.width'), cw), field(t('media.height'), ch)),
      button(t('media.op.crop'), () => add({ type: 'crop', x: +cx.value, y: +cy.value, w: +cw.value, h: +ch.value }))),
    h('div', { class: 'ew-imged-row' },
      button(t('media.rotate_left'), () => add({ type: 'rotate', deg: 270 })), button(t('media.rotate_right'), () => add({ type: 'rotate', deg: 90 })),
      button(t('media.flip_h'), () => add({ type: 'flip' })), button(t('media.flip_v'), () => add({ type: 'flip', vertical: true })),
      button(t('media.op.grayscale'), () => add({ type: 'grayscale' }))),
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('media.op.adjust')),
      field(t('media.brightness'), bright), field(t('media.contrast'), contrast), field(t('media.saturation'), sat),
      button(t('common.apply'), () => add({ type: 'adjust', brightness: +bright.value, contrast: +contrast.value, saturation: +sat.value }))),
    h('div', { class: 'ew-imged-row' }, field(t('media.blur_radius'), blur), button(t('media.op.blur'), () => add({ type: 'blur', radius: +blur.value })),
      field(t('media.new_width'), rw), button(t('media.op.resize'), () => add({ type: 'resize', w: +rw.value }))),
    button(t('media.undo'), () => { ops.pop(); render(); }, { variant: 'subtle' }), field(t('media.version_note'), note));

  const dlg = openDialog(t('media.editor_title', { name: m.title || m.name }), h('div', { class: 'ew-imged' }, h('div', { class: 'ew-imged-stage' }, canvas, steps), tools), [
    button(t('common.cancel'), () => dlg.close(), { variant: 'subtle' }),
    button(t('media.save_version'), async () => {
      if (!ops.length) { toast(t('media.no_steps'), 'error'); return; }
      try {
        const res = await api.post<Medium>(`media/${m.id}/edit`, { ops, note: note.value.trim() });
        toast(t('media.version_saved', { v: String(res.version) }));
        dlg.close();
        done(res);
      } catch (e) {
        toast(errorText(e), 'error');
      }
    }, { variant: 'primary' }),
  ], { wide: true });
}

// ---------------------------------------------------------------- details

/** Details dialog: preview, texts, versions, usage, rights, delete. */
/** Opens a medium: in the default office program when the settings say "always open there", else the details. */
export async function openMedium(m: Medium, changed: () => void = () => undefined): Promise<void> {
  const ext = await externalDefault(m);
  if (ext) {
    navigate(officeURL(m.id, ext.viewer));
    return;
  }
  await mediaDetails(m.id, changed);
}

export async function mediaDetails(id: number, changed: () => void): Promise<void> {
  let m = await api.get<Medium>(`media/${id}`);
  const body = h('div', { class: 'ew-media-details' });
  const dlg = openDialog(m.title || m.name, body, [], { wide: true, onClose: changed });
  const draw = async () => {
    const parts: Child[] = [await mediaViewer(m)];
    const meta = h('dl', { class: 'ew-media-meta' },
      h('dt', null, t('media.file')), h('dd', null, m.name),
      h('dt', null, t('media.size')), h('dd', null, `${humanSize(m.size)}${m.width ? ` · ${m.width}×${m.height}` : ''}`),
      h('dt', null, t('media.version')), h('dd', null, String(m.version)),
      h('dt', null, t('media.changed')), h('dd', null, fmtDate(m.updated_at, state.lang)),
      h('dt', null, t('media.wm.title')), h('dd', null, t(`media.wm.${m.watermark.mode}`)),
      h('dt', null, t('media.access')), h('dd', null, m.view_only ? t('media.view_only') : t('media.downloadable')));
    parts.push(meta);
    const actions = h('div', { class: 'ew-form-actions' });
    actions.append(...await officeButtons(m));
    if (!m.view_only) actions.append(h('a', { href: mediaURL(m, { download: true }), class: 'ew-btn', download: '' }, t('media.download')));
    if (has(m, 'edit')) {
      actions.append(button(t('media.edit_texts'), () => void editTexts()));
      if (isImage(m)) actions.append(button(t('media.edit_image'), () => imageEditor(m, (n) => { m = n; void draw(); })));
      actions.append(button(t('media.replace'), () => void replaceFile()));
    }
    if (has(m, 'manage')) {
      actions.append(button(t('media.settings'), () => void editSettings()));
      actions.append(button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('media.delete_confirm', { name: m.title || m.name }), t('common.delete'), true))) return;
        await api.del(`media/${m.id}`);
        toast(t('media.deleted'));
        dlg.close();
      }, { variant: 'danger' }));
    }
    parts.push(actions);
    const usage = await api.get<number[]>(`media/${m.id}/usage`).catch(() => []);
    parts.push(h('h3', null, t('media.usage')), usage.length ? h('ul', null, ...usage.map((e) => h('li', null, h('a', { href: `/e/${e}` }, `#${e}`)))) : h('p', null, t('media.unused')));
    if (has(m, 'edit')) {
      const versions = await api.get<MediaVersion[]>(`media/${m.id}/versions`).catch(() => []);
      parts.push(h('h3', null, t('media.versions')), h('table', { class: 'ew-table' },
        h('thead', null, h('tr', null, ...[t('media.version'), t('media.changed'), t('media.size'), t('media.version_note'), ''].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...versions.map((v) => h('tr', null, h('td', null, String(v.version)), h('td', null, fmtDate(v.created_at, state.lang)),
          h('td', null, `${humanSize(v.size)}${v.width ? ` · ${v.width}×${v.height}` : ''}`), h('td', null, v.note || (v.ops.length ? t('media.edited') : '')),
          h('td', null, v.version === m.version ? t('media.current') : h('span', null,
            h('a', { href: mediaURL(m, { v: v.version }), target: '_blank', rel: 'noopener' }, t('media.show')), ' ',
            button(t('media.restore'), async () => { m = await api.post<Medium>(`media/${m.id}/versions/${v.version}/restore`); toast(t('media.restored')); void draw(); }, { variant: 'link' }))))))));
    }
    body.replaceChildren();
    append(body, parts);
  };
  const editTexts = async () => {
    const title = textInput({ value: m.title });
    const alt = textInput({ value: m.alt });
    const desc = textArea({ value: m.description, rows: 3 });
    const name = textInput({ value: m.name });
    formDialog(t('media.edit_texts'), [field(t('media.file_name'), name), field(t('common.title'), title), field(t('media.alt'), alt, { help: t('media.alt_help') }), field(t('common.description'), desc)],
      t('common.save'), async () => {
        m = await api.patch<Medium>(`media/${m.id}`, { name: name.value, title: title.value, alt: alt.value, description: desc.value });
        toast(t('common.saved'));
        void draw();
      });
  };
  const replaceFile = async () => {
    const file = h('input', { type: 'file', class: 'ew-input' }) as HTMLInputElement;
    const note = textInput();
    formDialog(t('media.replace'), [field(t('media.file'), file, { required: true }), field(t('media.version_note'), note)], t('media.upload'), async () => {
      const f = file.files?.[0];
      if (!f) throw new Error(t('media.file'));
      m = await uploadMedium(f, { note: note.value.trim() }, `/api/v1/media/${m.id}/file`);
      toast(t('media.version_saved', { v: String(m.version) }));
      void draw();
    });
  };
  const editSettings = async () => {
    const cat = await categorySelect({ selected: m.category_id, perm: 'create' });
    const download = checkbox(t('media.download_allowed'), m.download, { help: t('media.download_help') });
    const wm = watermarkFields(m.watermark, false);
    const rules = m.own_acl ? await api.get<MediaRule[]>(`media/${m.id}/acl`) : [];
    const rights = await rightsFields(m.own_acl, rules);
    formDialog(t('media.settings'), [cat.field(t('media.category')), download.el, wm.el, rights.el], t('common.save'), async () => {
      const r = rights.value();
      m = await api.patch<Medium>(`media/${m.id}`, { category_id: cat.value(), download: download.input.checked, watermark: wm.value(), own_acl: r !== null, ...(r !== null ? { acl: r } : {}) });
      toast(t('common.saved'));
      void draw();
    }, { wide: true });
  };
  await draw();
}

// ---------------------------------------------------------------- library and picker

/** Grid of media with search and filters; onPick turns it into a picker. */
export async function mediaBrowser(opts: { categoryId?: number; type?: string; onPick?: (m: Medium) => void } = {}): Promise<HTMLElement> {
  const cat = await categorySelect({ selected: opts.categoryId, perm: 'view', noneLabel: t('media.all_categories'), onChange: () => void load() });
  const q = textInput({ type: 'search', placeholder: t('media.search') });
  const type = select(['', 'image', 'pdf', 'document', 'video', 'audio'].map((v) => ({ value: v, label: t(`media.type.${v || 'all'}`) })), opts.type ?? '');
  const grid = h('ul', { class: 'ew-media-grid', 'aria-live': 'polite' });
  const status = h('p', { class: 'ew-help', role: 'status' });
  let timer = 0;
  const load = async () => {
    status.textContent = t('common.loading');
    try {
      const list = await api.get<Medium[]>(`media${qs({ category: cat.value() || undefined, recursive: cat.value() ? 1 : undefined, q: q.value.trim() || undefined, type: type.value || undefined, limit: 120 })}`);
      grid.replaceChildren(...list.map((m) => h('li', { class: 'ew-media-item' },
        h('button', { type: 'button', class: 'ew-media-tile', 'aria-label': `${m.title || m.name}${m.view_only ? ` (${t('media.view_only')})` : ''}` },
          mediaThumb(m), h('span', { class: 'ew-media-name' }, m.title || m.name)))));
      grid.querySelectorAll<HTMLButtonElement>('.ew-media-tile').forEach((b, i) => b.addEventListener('click', () => {
        const m = list[i];
        if (opts.onPick) opts.onPick(m); else void openMedium(m, () => void load());
      }));
      status.textContent = list.length ? t('media.count', { n: String(list.length) }) : t('media.none');
    } catch (e) {
      status.textContent = errorText(e);
    }
  };
  q.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => void load(), 300); });
  type.addEventListener('change', () => void load());
  const up = button(t('media.upload'), async () => {
    const m = await uploadDialog({ categoryId: cat.value() || opts.categoryId });
    if (m) {
      if (opts.onPick) opts.onPick(m); else void load();
    }
  }, { variant: 'primary' });
  void load();
  return h('div', { class: 'ew-media-browser' },
    h('div', { class: 'ew-media-toolbar' }, cat.field(t('media.category')), field(t('media.search'), q), field(t('media.type.label'), type), up),
    status, grid);
}

/** Lets the user choose (or upload) a medium; null when cancelled. */
export function pickMedium(opts: { categoryId?: number; type?: string } = {}): Promise<Medium | null> {
  opts = { categoryId: ctx.categoryId || undefined, ...opts };
  return new Promise((resolve) => {
    let picked = false;
    void (async () => {
      const dlg = openDialog(t('media.pick'), h('div'), [button(t('common.cancel'), () => dlg.close(), { variant: 'subtle' })],
        { wide: true, onClose: () => { if (!picked) resolve(null); } });
      const browser = await mediaBrowser({ ...opts, onPick: (m) => { picked = true; resolve(m); dlg.close(); } });
      dlg.dialog.querySelector('.ew-dialog-body')?.replaceChildren(browser);
    })();
  });
}
