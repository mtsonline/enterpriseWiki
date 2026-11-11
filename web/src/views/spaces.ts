// Spaces: the address /s/KEY, the directory of all spaces and the "Space"
// tab of the settings of a top-level category.
import { api } from '../core/api';
import { toast } from '../core/announce';
import { h, mount, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { hasPerm, state } from '../core/state';
import type { Category, SpaceInfo } from '../core/types';
import { button, checkbox, errorText, field, formDialog, langFields, openDialog, pageHeader, select, textInput } from '../components/ui';
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
  const archivedGrid = h('ul', { class: 'ew-spaces' });
  const archivedSec = h('section', { class: 'ew-section', 'aria-labelledby': 'ew-arch-h', hidden: true }, h('h2', { id: 'ew-arch-h' }, t('archive.spaces')), archivedGrid);
  const card = (s: SpaceInfo) => h('li', { class: ['ew-space-card', s.archived && 'is-archived'] },
      h('h3', null, h('a', { href: s.key ? `/s/${s.key}` : `/c/${s.id}` }, pick(s.titles, L(), D()))),
      s.key ? h('p', { class: 'ew-space-key' }, h('span', { class: 'sr-only' }, `${t('space.key')}: `), s.key) : null,
      pick(s.descriptions, L(), D()) ? h('p', null, pick(s.descriptions, L(), D())) : null);
  const draw = () => {
    const q = filter.value.trim().toLowerCase();
    const shown = list.filter((s) => !q || [pick(s.titles, L(), D()), pick(s.descriptions, L(), D()), s.key].join(' ').toLowerCase().includes(q));
    const active = shown.filter((s) => !s.archived);
    const archived = shown.filter((s) => s.archived);
    grid.replaceChildren(...active.map(card));
    if (!active.length) grid.append(h('li', { class: 'ew-muted' }, t('space.none')));
    archivedGrid.replaceChildren(...archived.map(card));
    archivedSec.hidden = !archived.length;
    status.textContent = t('space.count', { n: shown.length });
  };
  filter.addEventListener('input', draw);
  draw();
  const admin = hasPerm('core.categories.admin');
  const create = admin ? button(t('space.new'), () => newSpaceDialog(list), { variant: 'primary' }) : null;
  const imp = admin ? button(t('space.import'), () => importDialog(0), { variant: 'secondary' }) : null;
  mount(main, pageHeader(t('space.directory'), create, imp), h('p', null, t('space.directory_help')),
    field(t('space.filter'), filter), status,
    h('section', { class: 'ew-section', 'aria-labelledby': 'ew-act-h' }, h('h2', { id: 'ew-act-h' }, t('space.active')), grid), archivedSec);
}

/** New space: empty or from a space template (structure, settings, page templates, optionally pages). */
function newSpaceDialog(list: SpaceInfo[]): void {
  const titles = langFields(t('common.title'), {}, { required: true });
  const key = textInput({ maxlength: 16, autocomplete: 'off' });
  const tpls = list.filter((s) => s.is_template);
  const from = select([{ value: '0', label: t('space.template_none') }, ...tpls.map((s) => ({ value: String(s.id), label: pick(s.titles, L(), D()) }))], '0');
  const pages = checkbox(t('space.with_pages'), false, { help: t('space.with_pages_help') });
  formDialog(t('space.new'), [titles.el, field(t('space.key'), key, { help: t('space.key_help', { origin: location.origin }) }),
    tpls.length ? field(t('space.from_template'), from, { help: t('space.from_template_help') }) : null, tpls.length ? pages.el : null], t('common.create'), async () => {
    const tid = Number(from.value);
    let id: number;
    if (tid) {
      id = (await api.post<SpaceInfo>('spaces', { template_id: tid, titles: titles.get(), key: key.value.trim(), with_pages: pages.input.checked })).id;
    } else {
      id = (await api.post<Category>('categories', { titles: titles.get() })).id;
      if (key.value.trim()) await api.put(`categories/${id}/space`, { key: key.value.trim() });
    }
    invalidateTree();
    toast(t('space.created'));
    navigate(`/c/${id}`);
  });
}

/** Import a package (JSON Lines from "Export") as new space (parent 0) or below a category. */
export function importDialog(parent: number): void {
  const file = h('input', { type: 'file', accept: '.jsonl,.ndjson,application/x-ndjson', required: true }) as HTMLInputElement;
  formDialog(t('space.import'), [h('p', null, t(parent ? 'space.import_below_help' : 'space.import_help')), field(t('space.import_file'), file, { required: true })], t('space.import_start'), async () => {
    const f = file.files?.[0];
    if (!f) throw new Error(t('space.import_file_missing'));
    const res = await api.post<{ category_id: number; categories: number; elements: number; templates: number; skipped: string[] }>(
      `categories/import${parent ? `?parent=${parent}` : ''}`, new Blob([f], { type: 'application/x-ndjson' }));
    invalidateTree();
    toast(t('space.imported', { categories: res.categories, elements: res.elements }));
    if (res.skipped.length) {
      openDialog(t('space.import_skipped', { n: res.skipped.length }), h('ul', { class: 'ew-plain-list' }, ...res.skipped.map((x) => h('li', null, x))));
    }
    navigate(`/c/${res.category_id}`);
  });
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
  const isTpl = hasPerm('core.categories.admin') ? checkbox(t('space.is_template'), !!sp.is_template, { help: t('space.is_template_help') }) : null;
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
    own.el, isTpl?.el ?? null,
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('space.shortcuts')), h('p', { class: 'ew-help' }, t('space.shortcuts_help')), rows,
      button(t('space.shortcut_add'), () => addRow().focus(), { variant: 'secondary' })),
    h('fieldset', { class: 'ew-fieldset', 'aria-describedby': typesId }, h('legend', null, t('space.allowed_types')),
      h('p', { class: 'ew-help', id: typesId }, t('space.allowed_types_help')), h('div', { class: 'ew-checks' }, ...typeBoxes.map((b) => b.cb.el))),
    field(t('space.language'), lang, { help: t('space.language_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  const io = h('section', { class: 'ew-section', 'aria-labelledby': 'ew-io-h' }, h('h2', { id: 'ew-io-h' }, t('space.io')),
    h('p', null, t('space.io_help')),
    h('div', { class: 'ew-actions' },
      h('a', { class: 'ew-btn ew-btn--secondary', href: `/api/v1/categories/${c.id}/export`, download: '' }, t('space.export')),
      button(t('space.import_below'), () => importDialog(c.id), { variant: 'secondary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const shortcuts = [...rows.children].map((li) => (li as HTMLElement & { get: () => { label: Record<string, string>; url: string } | null }).get()).filter((x) => x !== null);
    try {
      sp = await api.put<SpaceInfo>(`categories/${c.id}/space`, {
        key: key.value.trim(), own_tree: own.input.checked, shortcuts, language: lang.value, is_template: isTpl ? isTpl.input.checked : !!sp?.is_template,
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
  panel.append(form, io);
}
