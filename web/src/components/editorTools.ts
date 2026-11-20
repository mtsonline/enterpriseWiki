// Choice of the buttons of the formatting toolbar, for the site and for a
// category (inherited downwards). Hiding a button does not forbid the
// formatting: shortcuts and Markdown keep working.
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { checkbox } from './ui';

/** The buttons (names as content.EditorTools on the server). */
export const EDITOR_TOOLS = ['block', 'strong', 'em', 'underline', 'strike', 'code', 'link', 'bullet', 'ordered', 'task', 'quote',
  'table', 'table_edit', 'image', 'hr', 'macro', 'snippet', 'undo', 'redo'] as const;

/**
 * A fieldset to choose the buttons. current: the choice stored at this
 * level (null = inherited; ['*'] = all); inherit: offer "as above" (a
 * category). get() returns [] for inherited, ['*'] for all, else the names;
 * it throws (with a message for the person) if no button is chosen.
 */
export function editorToolsField(current: string[] | null | undefined, opts: { inherit: boolean; inherited?: string[] }): { el: HTMLElement; get: () => string[] } {
  const name = uid('tools');
  const mode = !current || current.length === 0 ? (opts.inherit ? 'inherit' : 'all') : current.includes('*') ? 'all' : 'choose';
  const radio = (value: string, label: string) => {
    const id = uid('tools-mode');
    const input = h('input', { type: 'radio', name, id, value, checked: mode === value }) as HTMLInputElement;
    return { input, el: h('div', { class: 'ew-check' }, input, h('label', { for: id }, label)) };
  };
  const inheritedText = (list: string[] | undefined) =>
    !list || list.includes('*') ? t('editor_tools.all') : list.map((x) => t(`editor_tools.${x}`)).join(', ');
  const modes = [
    ...(opts.inherit ? [radio('inherit', opts.inherited === undefined ? t('editor_tools.inherit_plain') : t('editor_tools.inherit', { tools: inheritedText(opts.inherited) }))] : []),
    radio('all', t('editor_tools.all')),
    radio('choose', t('editor_tools.choose')),
  ];
  const chosen = new Set(mode === 'choose' ? current : EDITOR_TOOLS);
  const boxes = EDITOR_TOOLS.map((x) => ({ name: x, box: checkbox(t(`editor_tools.${x}`), chosen.has(x)) }));
  const list = h('div', { class: 'ew-check-grid' }, ...boxes.map((b) => b.box.el));
  const sync = () => { list.hidden = !modes.find((m) => m.input.value === 'choose')!.input.checked; };
  for (const m of modes) m.input.addEventListener('change', sync);
  sync();
  const el = h('fieldset', { class: 'ew-fieldset' },
    h('legend', null, t('editor_tools.legend')),
    h('p', { class: 'ew-help' }, t('editor_tools.help')),
    ...modes.map((m) => m.el), list);
  return {
    el,
    get: () => {
      const m = modes.find((x) => x.input.checked)?.input.value ?? 'all';
      if (m === 'inherit') return [];
      if (m === 'all') return ['*'];
      const picked = boxes.filter((b) => b.box.input.checked).map((b) => b.name);
      // none: that is no choice (to show no toolbar, hide its block in the placement)
      if (!picked.length) throw new Error(t('editor_tools.none'));
      return picked;
    },
  };
}
