// Category overview: editor (category or top level, depth, layout, elements).
import { categorySelect, checkbox, field, h, mount, registerEditor, select, t, textArea, textInput, type EditorInstance } from '@ewiki/sdk';
import './style.css';

interface Body { intro: { format: string; source: string }; category_id: number; depth: number; layout: string; descriptions: boolean; elements: boolean; max_elements: number }

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const b = { ...(raw as Body) };
  b.intro = b.intro ?? { format: 'markdown', source: '' };
  const catBox = h('div');
  let cats: Awaited<ReturnType<typeof categorySelect>> | null = null;
  void categorySelect({ selected: b.category_id, perm: 'view', noneLabel: t('content.catlist.top_level') })
    .then((cs) => { cats = cs; catBox.append(cs.field(t('content.catlist.category'), { help: t('content.catlist.category_help') })); });
  const depth = select([1, 2, 3, 4, 5].map((d) => ({ value: String(d), label: String(d) })), String(b.depth || 1));
  const layout = select(['cards', 'list'].map((v) => ({ value: v, label: t(`content.catlist.layout_${v}`) })), b.layout);
  const desc = checkbox(t('content.catlist.descriptions'), b.descriptions);
  const els = checkbox(t('content.catlist.elements'), b.elements);
  const max = textInput({ type: 'number', value: String(b.max_elements || 5) });
  const fmt = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], b.intro.format);
  const intro = textArea({ value: b.intro.source, rows: 3 });
  mount(container, h('div', { class: 'ewo-editor' },
    h('fieldset', null, h('legend', null, t('content.catlist.source')), catBox, field(t('content.catlist.depth'), depth, { help: t('content.catlist.depth_help') })),
    h('fieldset', null, h('legend', null, t('content.catlist.display')), field(t('content.catlist.layout'), layout), desc.el, els.el, field(t('content.catlist.max_elements'), max)),
    h('fieldset', null, h('legend', null, t('content.catlist.intro')), field(t('content.catlist.format'), fmt), field(t('content.catlist.intro_text'), intro))));
  return {
    getBody: () => ({
      intro: { format: fmt.value, source: intro.value }, category_id: cats?.value() ?? b.category_id, depth: Number(depth.value), layout: layout.value,
      descriptions: desc.input.checked, elements: els.input.checked, max_elements: Number(max.value) || 5,
    }),
  };
}

export function register(): void {
  registerEditor('content.catlist', createEditor);
}
