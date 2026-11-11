// Property database: the editor of its settings (sources, filters, columns,
// sorting, view, new entries) with the controls of the property report.
import { categorySelect, field, getMacroParamControl, h, mount, registerEditor, select, state, t, textArea, textInput, typeLabel, currentCategory, type EditorInstance } from '@ewiki/sdk';

interface Body {
  intro: { format: string; source: string }; source: string; filter: string; columns_text: string; sort_text: string;
  view: string; group: string; date: string; new_type: string; new_category: number; per_page: number; export_mode?: string;
}

function control(name: string, label: string, value: string, all: () => Record<string, string>): { el: HTMLElement; get: () => string } {
  const f = getMacroParamControl(name);
  if (f) return f(value, { label, required: false, params: all, categoryId: currentCategory() });
  const i = textInput({ value });
  return { el: field(label, i), get: () => i.value };
}

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const b = { ...(raw as Body) };
  b.intro = b.intro ?? { format: 'markdown', source: '' };
  const ctl: Record<string, { get: () => string }> = {};
  const all = () => Object.fromEntries(Object.entries(ctl).map(([k, c]) => [k, c.get()]));
  const src = control('properties.source', t('content.propdb.sources'), b.source ?? '', all); ctl.source = src;
  const flt = control('properties.filter', t('content.propdb.filters'), b.filter ?? '', all); ctl.filter = flt;
  const cols = control('properties.columns', t('content.propdb.columns'), b.columns_text ?? '', all); ctl.columns = cols;
  const sort = control('properties.sort', t('content.propdb.sort'), b.sort_text ?? '', all); ctl.sort = sort;
  const group = control('properties.key', t('content.propdb.group'), b.group ?? '', all);
  const date = control('properties.key', t('content.propdb.date'), b.date ?? '', all);
  const view = select(['table', 'cards', 'kanban', 'calendar', 'timeline', 'chart'].map((v) => ({ value: v, label: t(`props.view.${v}`) })), b.view || 'table');
  const newType = select(state.site.element_types.map((x) => ({ value: x.id, label: typeLabel(x.id) })), b.new_type || 'core.html');
  const per = textInput({ type: 'number', value: String(b.per_page || 50) });
  const exportMode = select([{ value: 'safe', label: t('content.propdb.export_safe') }, { value: 'original', label: t('content.propdb.export_original') }], b.export_mode || 'safe');
  const intro = textArea({ value: b.intro.source, rows: 3 });
  const catBox = h('div');
  let cats: Awaited<ReturnType<typeof categorySelect>> | null = null;
  void categorySelect({ selected: b.new_category || undefined, perm: 'create', noneLabel: t('content.propdb.same_category') })
    .then((cs) => { cats = cs; catBox.append(cs.field(t('content.propdb.new_category'), { help: t('content.propdb.new_category_help') })); });
  mount(container, h('div', { class: 'ewdb-editor' },
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('content.propdb.data')), src.el, flt.el),
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('content.propdb.display')), field(t('content.propdb.view'), view), cols.el, sort.el, group.el, date.el, field(t('content.propdb.per_page'), per),
      field(t('content.propdb.export_mode'), exportMode, { help: t('content.propdb.export_mode_help') })),
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('content.propdb.new_entries')), field(t('content.propdb.new_type'), newType), catBox),
    field(t('content.propdb.intro'), intro, { help: t('content.propdb.intro_help') })));
  return {
    getBody: () => ({
      intro: { format: 'markdown', source: intro.value }, source: src.get(), filter: flt.get(), columns_text: cols.get(), sort_text: sort.get(),
      view: view.value, group: group.get(), date: date.get(), new_type: newType.value, new_category: cats?.value() ?? b.new_category ?? 0, per_page: Number(per.value) || 50, export_mode: exportMode.value,
    }),
  };
}

export function register(): void {
  registerEditor('content.propdb', createEditor);
}
