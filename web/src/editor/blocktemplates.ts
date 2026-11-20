// Block templates: blocks of a page (text, layout and wiki blocks with their
// options) saved under a name and inserted into other pages. They are page
// templates of the kind "block" (Markdown), with the same scopes: only me,
// a category and below, a space, everybody.
import { api, qs } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { hasPerm, state } from '../core/state';
import { pick } from '../core/i18n';
import { button, field, formDialog, select, textArea, textInput, errorText } from '../components/ui';
import { can, currentCategory, currentSpace } from '../components/widgets';
import { manageTemplates, type Template } from '../components/publishing';

/** Lets the person choose a block template; resolves with its Markdown (or null). */
export async function chooseBlockTemplate(): Promise<string | null> {
  const cat = currentCategory();
  let list: Template[] = [];
  try { list = await api.get<Template[]>(`templates${qs({ kind: 'block', category: cat || undefined })}`); } catch (e) { toast(errorText(e), 'error'); return null; }
  if (!list.length) { toast(t('blocktpl.none')); return null; }
  const sel = select(list.map((x) => ({ value: String(x.id), label: x.name })), String(list[0].id));
  const desc = h('p', { class: 'ew-help', 'aria-live': 'polite' });
  const show = () => { desc.textContent = list.find((x) => String(x.id) === sel.value)?.description ?? ''; };
  sel.addEventListener('change', show);
  show();
  return new Promise((resolve) => {
    let done = false;
    const dlg = formDialog(t('blocktpl.insert'), [field(t('blocktpl.choose'), sel), desc,
      h('p', null, button(t('blocktpl.manage'), () => { dlg.close(); void manageTemplates('core.html', cat, undefined, 'block'); }, { variant: 'link' }))],
    t('blocktpl.insert_button'), async () => {
      const full = await api.get<Template>(`templates/${sel.value}`);
      const body = full.body as { source?: string } | undefined;
      done = true;
      resolve(body?.source ?? '');
    });
    dlg.dialog.addEventListener('close', () => { if (!done) resolve(null); });
  });
}

/** Saves Markdown (the selected blocks) as a block template. */
export function saveBlockTemplate(source: string): void {
  const name = textInput({ required: true, maxlength: 200 });
  const desc = textArea({ rows: 2 });
  const cat = currentCategory();
  const sp = currentSpace?.can_manage ? currentSpace : null;
  const inCategory = cat && can('manage') && cat !== sp?.id;
  const scopes = [{ value: 'mine', label: t('template.scope_mine') },
    ...(inCategory ? [{ value: 'category', label: t('template.scope_category') }] : []),
    ...(sp ? [{ value: 'space', label: t('template.scope_space', { name: pick(sp.titles, state.lang, state.site.default_language) }) }] : []),
    ...(hasPerm('core.templates.global') ? [{ value: 'global', label: t('template.scope_all') }] : [])];
  const scope = scopes.length > 1 ? select(scopes, 'mine') : null;
  formDialog(t('blocktpl.save'), [h('p', { class: 'ew-help' }, t('blocktpl.save_help')), field(t('common.name'), name, { required: true }), field(t('common.description'), desc),
    scope ? field(t('template.scope'), scope) : null], t('common.save'), async () => {
    if (!name.value.trim()) throw new Error(t('error.title_required'));
    await api.post('templates', { kind: 'block', name: name.value.trim(), description: desc.value, type: 'core.html', lang: state.lang,
      body: { format: 'markdown', source }, global: scope?.value === 'global', space_id: scope?.value === 'space' && sp ? sp.id : scope?.value === 'category' ? cat : 0 });
    toast(t('blocktpl.saved'));
  });
}
