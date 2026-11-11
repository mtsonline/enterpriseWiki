// Spaces: the address /s/KEY, the directory of all spaces and the "Space"
// tab of the settings of a top-level category.
import { api } from '../core/api';
import { toast } from '../core/announce';
import { h, mount, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import type { Category, SpaceInfo } from '../core/types';
import { button, checkbox, errorText, field, pageHeader, select, textInput } from '../components/ui';
import { invalidateTree, langName, setContext } from '../components/widgets';
import { errorPage } from './content';
import { typeLabel } from '../sdk';

const L = () => state.lang;
const D = () => state.site.default_language;

/** /s/KEY opens the space with that key. */
export async function spaceKeyView({ params, main }: RouteCtx): Promise<void> {
  try {
    const sp = await api.get<SpaceInfo>(`spaces/${encodeURIComponent(params.key)}`);
    navigate(`/c/${sp.id}`, { replace: true });
  } catch (e) {
    setContext(0, [], []);
    errorPage(main, e);
  }
}

/** /spaces: all spaces the reader may see, with a filter. */
export async function spacesView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  let list: SpaceInfo[];
  try {
    list = await api.get<SpaceInfo[]>('spaces');
  } catch (e) {
    errorPage(main, e);
    return;
  }
  const filter = textInput({ type: 'search', placeholder: t('space.filter_placeholder') });
  const status = h('p', { class: 'sr-only', role: 'status', 'aria-live': 'polite' });
  const grid = h('ul', { class: 'ew-spaces' });
  const draw = () => {
    const q = filter.value.trim().toLowerCase();
    const shown = list.filter((s) => !q || [pick(s.titles, L(), D()), pick(s.descriptions, L(), D()), s.key].join(' ').toLowerCase().includes(q));
    grid.replaceChildren(...shown.map((s) => h('li', { class: 'ew-space-card' },
      h('h2', null, h('a', { href: s.key ? `/s/${s.key}` : `/c/${s.id}` }, pick(s.titles, L(), D()))),
      s.key ? h('p', { class: 'ew-space-key' }, h('span', { class: 'sr-only' }, `${t('space.key')}: `), s.key) : null,
      pick(s.descriptions, L(), D()) ? h('p', null, pick(s.descriptions, L(), D())) : null)));
    if (!shown.length) grid.append(h('li', { class: 'ew-muted' }, t('space.none')));
    status.textContent = t('space.count', { n: shown.length });
  };
  filter.addEventListener('input', draw);
  draw();
  mount(main, pageHeader(t('space.directory')), h('p', null, t('space.directory_help')),
    field(t('space.filter'), filter), status, grid);
}

/** The "Space" tab of the settings of a top-level category. */
export async function spaceSettings(panel: HTMLElement, c: Category): Promise<void> {
  let sp: SpaceInfo | null;
  try {
    sp = (await api.get<{ space: SpaceInfo | null }>(`categories/${c.id}`)).space ?? null;
  } catch (e) {
    panel.append(h('p', { role: 'alert' }, errorText(e)));
    return;
  }
  if (!sp) return;
  const key = textInput({ value: sp.key, maxlength: 16, autocomplete: 'off' });
  key.setAttribute('pattern', '[A-Za-z][A-Za-z0-9]{1,15}');
  const own = checkbox(t('space.own_tree'), sp.own_tree, { help: t('space.own_tree_help') });
  const lang = select([{ value: '', label: t('space.language_site') }, ...state.site.languages.map((l) => ({ value: l, label: langName(l) }))], sp.language);
  // shortcuts: label (UI language) and address
  const rows = h('ol', { class: 'ew-space-shortcuts' });
  const addRow = (l?: { label: Record<string, string>; url: string }) => {
    const n = rows.children.length + 1;
    const label = textInput({ value: l ? pick(l.label, L(), D()) : '', maxlength: 100 });
    const url = textInput({ value: l?.url ?? '', maxlength: 2000, placeholder: '/wiki/… https://…' });
    label.setAttribute('aria-label', t('space.shortcut_label', { n }));
    url.setAttribute('aria-label', t('space.shortcut_url', { n }));
    const li = h('li', { class: 'ew-space-shortcut' }, label, url);
    li.append(button(t('common.remove'), () => { li.remove(); rows.querySelector<HTMLElement>('input')?.focus(); }, { variant: 'link', ariaLabel: t('space.shortcut_remove', { n }) }));
    (li as HTMLElement & { get?: () => { label: Record<string, string>; url: string } | null }).get = () => {
      if (!label.value.trim() && !url.value.trim()) return null;
      return { label: { ...(l?.label ?? {}), [L()]: label.value.trim() }, url: url.value.trim() };
    };
    rows.append(li);
    return label;
  };
  sp.shortcuts.forEach((l) => addRow(l));
  // allowed element types (none checked = all)
  const typeBoxes = state.site.element_types.map((x) => ({ id: x.id, cb: checkbox(typeLabel(x.id), sp!.allowed_types.includes(x.id)) }));
  const typesId = uid('types');
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    h('p', null, t('space.help')),
    field(t('space.key'), key, { help: t('space.key_help', { origin: location.origin }) }),
    own.el,
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('space.shortcuts')), h('p', { class: 'ew-help' }, t('space.shortcuts_help')), rows,
      button(t('space.shortcut_add'), () => addRow().focus(), { variant: 'secondary' })),
    h('fieldset', { class: 'ew-fieldset', 'aria-describedby': typesId }, h('legend', null, t('space.allowed_types')),
      h('p', { class: 'ew-help', id: typesId }, t('space.allowed_types_help')), h('div', { class: 'ew-checks' }, ...typeBoxes.map((b) => b.cb.el))),
    field(t('space.language'), lang, { help: t('space.language_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const shortcuts = [...rows.children].map((li) => (li as HTMLElement & { get: () => { label: Record<string, string>; url: string } | null }).get()).filter((x) => x !== null);
    try {
      sp = await api.put<SpaceInfo>(`categories/${c.id}/space`, {
        key: key.value.trim(), own_tree: own.input.checked, shortcuts, language: lang.value,
        allowed_types: typeBoxes.filter((b) => b.cb.input.checked).map((b) => b.id),
      });
      key.value = sp.key;
      invalidateTree();
      toast(t('common.saved'));
    } catch (e) {
      alert.textContent = errorText(e);
      alert.focus?.();
    }
  });
  panel.append(form);
}
