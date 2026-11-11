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
  const mine = checkbox(t('space.mine_only'), false, { help: t('space.mine_help') });
  const status = h('p', { class: 'sr-only', role: 'status', 'aria-live': 'polite' });
  const grid = h('ul', { class: 'ew-spaces' });
  const archivedGrid = h('ul', { class: 'ew-spaces' });
  const archivedSec = h('section', { class: 'ew-section', 'aria-labelledby': 'ew-arch-h', hidden: true }, h('h2', { id: 'ew-arch-h' }, t('archive.spaces')), archivedGrid);
  const card = (s: SpaceInfo) => h('li', { class: ['ew-space-card', s.archived && 'is-archived'] },
      h('h3', null, h('a', { href: s.key ? `/s/${s.key}` : `/c/${s.id}` }, pick(s.titles, L(), D()))),
      s.key ? h('p', { class: 'ew-space-key' }, h('span', { class: 'sr-only' }, `${t('space.key')}: `), s.key) : null,
      s.personal ? h('p', { class: 'ew-badge ew-badge--info' }, t('space.personal')) : null,
      pick(s.descriptions, L(), D()) ? h('p', null, pick(s.descriptions, L(), D())) : null);
  const draw = () => {
    const q = filter.value.trim().toLowerCase();
    const shown = list.filter((s) => (!q || [pick(s.titles, L(), D()), pick(s.descriptions, L(), D()), s.key].join(' ').toLowerCase().includes(q))
      && (!mine.input.checked || s.member || s.can_manage));
    const active = shown.filter((s) => !s.archived);
    const archived = shown.filter((s) => s.archived);
    grid.replaceChildren(...active.map(card));
    if (!active.length) grid.append(h('li', { class: 'ew-muted' }, t('space.none')));
    archivedGrid.replaceChildren(...archived.map(card));
    archivedSec.hidden = !archived.length;
    status.textContent = t('space.count', { n: shown.length });
  };
  filter.addEventListener('input', draw);
  mine.input.addEventListener('change', draw);
  draw();
  const admin = hasPerm('core.categories.admin');
  const create = admin ? button(t('space.new'), () => newSpaceDialog(list), { variant: 'primary' }) : null;
  const imp = admin ? button(t('space.import'), () => importDialog(0), { variant: 'secondary' }) : null;
  const own = list.find((s) => s.personal && s.personal === state.user?.id);
  const personal = own ? h('a', { class: 'ew-btn ew-btn--secondary', href: `/c/${own.id}` }, t('space.personal_open'))
    : hasPerm('core.spaces.personal') ? button(t('space.personal_create'), async () => {
      try {
        const sp = await api.post<SpaceInfo>('spaces/personal');
        invalidateTree();
        navigate(`/c/${sp.id}`);
      } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'secondary' }) : null;
  mount(main, pageHeader(t('space.directory'), create, imp, personal), h('p', null, t('space.directory_help')),
    field(t('space.filter'), filter), mine.el, status,
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

interface InsightRow { id: number; title: string; views?: number; updated_at: string }
interface Insights { days: number; pages: number; new_pages: number; views: number; views_per_day: { day: string; views: number }[]; popular: InsightRow[]; stale: InsightRow[]; contributors: { name: string; versions: number }[] }

/** Evaluation of a category with everything below it (views per page and day only, no people). */
export async function insightsPanel(panel: HTMLElement, c: Category): Promise<void> {
  const period = select([7, 30, 90, 365].map((d) => ({ value: String(d), label: t('insights.days', { n: d }) })), '30');
  const box = h('div', { 'aria-live': 'polite' });
  const draw = async () => {
    let d: Insights;
    try {
      d = await api.get<Insights>(`categories/${c.id}/insights?days=${period.value}&lang=${state.lang}`);
    } catch (e) {
      box.replaceChildren(h('p', { role: 'alert' }, errorText(e)));
      return;
    }
    const max = Math.max(1, ...d.views_per_day.map((x) => x.views));
    const table = (caption: string, head: string[], rows: (string | HTMLElement)[][]) => h('table', { class: 'ew-table' },
      h('caption', null, caption), h('thead', null, h('tr', null, ...head.map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows.map((r) => h('tr', null, ...r.map((x, i) => (i === 0 ? h('th', { scope: 'row' }, x) : h('td', null, x))))) : [h('tr', null, h('td', { colspan: String(head.length) }, t('insights.none')))])));
    const link = (r: InsightRow) => h('a', { href: `/e/${r.id}` }, r.title || `#${r.id}`);
    box.replaceChildren(
      h('ul', { class: 'ew-stats' },
        h('li', null, h('strong', null, String(d.views)), ' ', t('insights.views')),
        h('li', null, h('strong', null, String(d.pages)), ' ', t('insights.pages')),
        h('li', null, h('strong', null, String(d.new_pages)), ' ', t('insights.new_pages'))),
      // views per day: bars with the numbers as text (also readable without the picture)
      d.views_per_day.length ? h('figure', { class: 'ew-bars' },
        h('figcaption', null, t('insights.per_day')),
        h('ol', null, ...d.views_per_day.map((x) => h('li', null,
          h('span', { class: 'ew-bars-label' }, fmtDay(x.day)),
          h('span', { class: 'ew-bars-bar', style: `--w:${Math.round((x.views / max) * 100)}%`, 'aria-hidden': 'true' }),
          h('span', { class: 'ew-bars-value' }, String(x.views)))))) : '',
      table(t('insights.popular'), [t('common.title'), t('insights.views')], d.popular.map((r) => [link(r), String(r.views ?? 0)])),
      table(t('insights.stale'), [t('common.title'), t('insights.updated')], d.stale.map((r) => [link(r), fmtDay(r.updated_at)])),
      table(t('insights.contributors'), [t('common.name'), t('insights.versions')], d.contributors.map((x) => [x.name, String(x.versions)])));
  };
  period.addEventListener('change', () => void draw());
  panel.append(h('p', null, t('insights.help')), field(t('insights.period'), period), box);
  await draw();
}

const fmtDay = (iso: string) => new Date(iso.length === 10 ? `${iso}T12:00:00` : iso).toLocaleDateString(state.lang);
