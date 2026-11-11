// Drafts, preview, templates and the publish dialog of the element editors.
import { api, qs } from '../core/api';
import { fmtDate, h, trustedHTML } from '../core/dom';
import { pick, t } from '../core/i18n';
import { hasPerm, state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, field, formDialog, openDialog, select, textArea, textInput } from './ui';
import { categorySelect } from './pickers';
import { currentSpace } from './widgets';
import { listPublishOptions } from '../sdk';

export interface Draft { id: number; element_id: number | null; category_id: number; type: string; lang: string; title: string; body?: unknown; tags: string[] | null; comment: string; base_rev: number; created_by: string; updated_by: string; updated_at: string; publish_at: string | null; fields?: Record<string, unknown> | null }
export interface Template { id: number; name: string; description: string; type: string; lang: string; title: string; body?: unknown; tags: string[]; global: boolean; mine: boolean; space_id?: number }

export interface EditorState { type: string; lang: string; title: string; body: unknown; tags?: string[]; comment?: string; baseRev?: number; elementId?: number; categoryId: number; fields?: Record<string, unknown> }

/** saveDraft stores the editor content as draft (new or existing draft). */
export async function saveDraft(draftId: number | null, s: EditorState): Promise<Draft> {
  const body = { element_id: s.elementId ?? null, category_id: s.categoryId, type: s.type, lang: s.lang, title: s.title, body: s.body, tags: s.tags ?? null, comment: s.comment ?? '', base_rev: s.baseRev ?? 0, fields: s.fields ?? null };
  const d = draftId ? await api.put<Draft>(`drafts/${draftId}`, body) : await api.post<Draft>('drafts', body);
  toast(t('draft.saved'));
  return d;
}

/** showPreview renders the editor content without saving. */
export async function showPreview(s: EditorState): Promise<void> {
  const res = await api.post<{ html: string }>('elements/preview', { type: s.type, lang: s.lang, body: s.body });
  openDialog(t('draft.preview_title', { title: s.title || t('element.untitled') }), h('div', { class: 'ew-preview', lang: s.lang },
    h('p', { class: 'ew-notice ew-notice--info' }, t('draft.preview_note')), h('h1', null, s.title), trustedHTML(res.html)), [], { wide: true });
}

/** saveTemplateDialog saves the editor content as template. */
export function saveTemplateDialog(s: EditorState): void {
  const name = textInput({ value: s.title, required: true, maxlength: 200 });
  const desc = textArea({ rows: 2 });
  // who may use it: only me, everybody in this space (space managers), everybody
  const sp = currentSpace?.can_manage ? currentSpace : null;
  const scopes = [{ value: 'mine', label: t('template.scope_mine') },
    ...(sp ? [{ value: 'space', label: t('template.scope_space', { name: pick(sp.titles, state.lang, state.site.default_language) }) }] : []),
    ...(hasPerm('core.templates.global') ? [{ value: 'global', label: t('template.scope_all') }] : [])];
  const scope = scopes.length > 1 ? select(scopes, 'mine') : null;
  formDialog(t('template.save_title'), [field(t('common.name'), name, { required: true }), field(t('common.description'), desc),
    scope ? field(t('template.scope'), scope) : null], t('common.save'), async () => {
    if (!name.value.trim()) throw new Error(t('error.title_required'));
    await api.post('templates', { name: name.value.trim(), description: desc.value, type: s.type, lang: s.lang, title: s.title, body: s.body, tags: s.tags ?? [],
      global: scope?.value === 'global', space_id: scope?.value === 'space' && sp ? sp.id : 0 });
    toast(t('template.saved'));
  });
}

/** templatePicker offers the templates of a type; onPick receives the full template. */
export async function templatePicker(type: string, onPick: (tpl: Template) => void, categoryId = 0): Promise<HTMLElement | null> {
  let list: Template[] = [];
  try { list = await api.get<Template[]>(`templates${qs({ type, category: categoryId || undefined })}`); } catch { return null; }
  if (!list.length) return null;
  const sel = select([{ value: '', label: t('template.none') }, ...list.map((x) => ({ value: String(x.id), label: `${x.name}${scopeLabel(x)}` }))], '');
  sel.addEventListener('change', async () => {
    if (!sel.value) return;
    const tpl = await api.get<Template>(`templates/${sel.value}`);
    onPick(tpl);
    toast(t('template.applied', { name: tpl.name }));
  });
  const del = button(t('template.manage'), () => void manageTemplates(type, categoryId), { variant: 'link' });
  return h('div', { class: 'ew-template-picker' }, field(t('template.label'), sel, { help: t('template.help') }), del);
}

const scopeLabel = (x: Template) => (x.space_id ? ` (${t('template.for_space')})` : x.global ? ` (${t('template.for_all')})` : '');

async function manageTemplates(type: string, categoryId = 0): Promise<void> {
  const list = await api.get<Template[]>(`templates${qs({ type, category: categoryId || undefined })}`);
  const rows = h('ul', { class: 'ew-plain-list' });
  const mayDelete = (x: Template) => x.mine || (x.global && hasPerm('core.templates.global')) || (!!x.space_id && x.space_id === currentSpace?.id && currentSpace.can_manage);
  const draw = (items: Template[]) => rows.replaceChildren(...items.map((x) => h('li', null, x.name, scopeLabel(x),
    mayDelete(x) ? button(t('common.delete'), async () => {
      await api.del(`templates/${x.id}`);
      draw(items.filter((y) => y.id !== x.id));
      toast(t('template.deleted'));
    }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${x.name}` }) : null)));
  draw(list);
  openDialog(t('template.manage'), rows);
}

/** draftNotice offers to load an existing draft of an element (language). */
export async function draftNotice(elementId: number, lang: string, onLoad: (d: Draft) => void): Promise<HTMLElement | null> {
  let list: Draft[] = [];
  try { list = await api.get<Draft[]>(`drafts${qs({ element: elementId })}`); } catch { return null; }
  const d = list.find((x) => x.lang === lang);
  if (!d) return null;
  const box = h('div', { class: 'ew-notice ew-notice--info', role: 'status' },
    h('p', null, t('draft.exists', { user: d.updated_by || d.created_by || '–', date: fmtDate(d.updated_at, state.lang) }),
      d.publish_at ? h('strong', null, ' ', t('draft.scheduled_for', { date: fmtDate(d.publish_at, state.lang) })) : null),
    h('div', { class: 'ew-actions' },
      button(t('draft.load'), async () => { const full = await api.get<Draft>(`drafts/${d.id}`); onLoad(full); box.remove(); }, { variant: 'primary' }),
      button(t('draft.discard'), async () => { await api.del(`drafts/${d.id}`); box.remove(); toast(t('draft.discarded')); }, { variant: 'secondary' })));
  return box;
}

export interface PublishChoice { categoryId: number; at: string | null; notify: boolean; options: Record<string, unknown> }

/** publishDialog asks where and when to publish (new elements: target category). */
export async function publishDialog(s: EditorState, isNew: boolean): Promise<PublishChoice | null> {
  const target = isNew ? await categorySelect({ selected: s.categoryId, perm: 'create' }) : null;
  const when = select([{ value: 'now', label: t('publish.now') }, { value: 'later', label: t('publish.later') }], 'now');
  const at = textInput({ type: 'datetime-local' });
  const atField = field(t('publish.at'), at, { help: t('publish.at_help') });
  atField.hidden = true;
  when.addEventListener('change', () => { atField.hidden = when.value !== 'later'; });
  const notify = checkbox(t('publish.notify'), true, { help: t('publish.notify_help') });
  const plugins: { id: string; value: () => unknown }[] = [];
  const pluginEls: HTMLElement[] = [];
  for (const o of listPublishOptions()) {
    const r = await o.render({ categoryId: s.categoryId, type: s.type, elementId: s.elementId ?? null, lang: s.lang });
    if (r) { plugins.push({ id: o.id, value: r.value }); pluginEls.push(r.el); }
  }
  return new Promise((resolve) => {
    let done = false;
    const dlg = formDialog(t('publish.title'), [
      target ? target.field(t('publish.category')) : null,
      field(t('publish.when'), when), atField, notify.el, ...pluginEls,
    ], t('publish.submit'), async () => {
      let iso: string | null = null;
      if (when.value === 'later') {
        if (!at.value) throw new Error(t('publish.at_required'));
        const d = new Date(at.value);
        if (Number.isNaN(d.getTime()) || d.getTime() < Date.now()) throw new Error(t('publish.at_future'));
        iso = d.toISOString();
      }
      const options: Record<string, unknown> = {};
      for (const p of plugins) { const v = p.value(); if (v !== undefined && v !== null) options[p.id] = v; }
      done = true;
      resolve({ categoryId: target ? target.value() || s.categoryId : s.categoryId, at: iso, notify: notify.input.checked, options });
    });
    dlg.dialog.addEventListener('close', () => { if (!done) resolve(null); });
  });
}

/** publishVia saves a draft and publishes it with the chosen options. */
export async function publishVia(draftId: number | null, s: EditorState, c: PublishChoice): Promise<{ elementId?: number; scheduled?: string }> {
  const d = await saveDraft(draftId, s);
  const res = await api.post<{ element?: { id: number }; scheduled?: string }>(`drafts/${d.id}/publish`, { category_id: c.categoryId, at: c.at, notify: c.notify, options: c.options });
  if (res.scheduled) toast(t('publish.scheduled', { date: fmtDate(res.scheduled, state.lang) }));
  return { elementId: res.element?.id, scheduled: res.scheduled };
}
