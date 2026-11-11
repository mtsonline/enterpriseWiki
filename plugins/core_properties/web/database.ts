// The element "Property database" in the browser: search, filters, views,
// grouping, changing the properties of a row, new entries, managing the
// definitions — everything with the rights of the reader (POST /query).
import { announce, api, button, checkbox, errorText, field, formDialog, h, qs, select, state, t, textInput, toast } from '@ewiki/sdk';
import { BASE, type Props } from './model';
import { propertiesForm } from './edit';
import { chart } from './charts';
import { calendarView, cardsView, chartData, colLabel, downloadButtons, kanbanView, tableView, timelineView, type Column, type Filter, type Result, type Row, type SortKey } from './report';

interface Source { kind: string; id?: number; sub?: boolean; tag?: string; type?: string; ids?: number[] }
interface Config {
  sources: Source[]; filters: Filter[]; columns: string[]; sort: SortKey[]; view: string; group: string; date: string;
  new_type: string; new_category: number; per_page: number;
}

const VIEWS = ['table', 'cards', 'kanban', 'calendar', 'timeline', 'chart'];

function filterOps(type: string): string[] {
  if (['integer', 'decimal', 'percent', 'currency', 'unit', 'rating', 'progress', 'duration', 'formula'].includes(type)) return ['eq', 'ne', 'lt', 'le', 'gt', 'ge', 'empty', 'notempty'];
  if (['date', 'datetime', 'daterange'].includes(type)) return ['eq', 'lt', 'le', 'gt', 'ge', 'empty', 'notempty'];
  if (['multiselect', 'user', 'group', 'element', 'category'].includes(type)) return ['has', 'hasnot', 'empty', 'notempty', ...(type === 'user' ? ['me'] : [])];
  return ['contains', 'eq', 'ne', 'empty', 'notempty'];
}

export async function databaseWidget(el: HTMLElement): Promise<void> {
  let cfg: Config;
  try { cfg = JSON.parse(el.dataset.config ?? '{}') as Config; } catch { return; }
  const categoryId = Number(el.dataset.category) || 0;
  const elementId = Number(el.dataset.element) || 0;
  const sources = cfg.sources?.length ? cfg.sources : [{ kind: 'category', id: categoryId, sub: true }];
  let view = VIEWS.includes(cfg.view) ? cfg.view : 'table';
  let group = cfg.group || '';
  let sort: SortKey[] = cfg.sort ?? [];
  let offset = 0;
  let search = '';
  const extra: Filter[] = [];
  const out = h('div', { class: 'ewprop-out', 'aria-live': 'polite' });
  const chips = h('ul', { class: 'ew-chips ewdb-filters', 'aria-label': t('props.db.active_filters') });
  const pager = h('div', { class: 'ewprop-pager' });
  const count = h('p', { class: 'ewdb-count', role: 'status' });
  let columns: Column[] = [];
  // where new entries go and whether the reader may manage definitions
  const target = cfg.new_category || categoryId;
  let canCreate = false, canManage = false, canEditDb = false;
  try {
    const c = await api.get<{ category: { permissions: string[] | null } }>(`categories/${target}`);
    canCreate = !!c.category.permissions?.includes('create');
    const own = target === categoryId ? c : await api.get<{ category: { permissions: string[] | null } }>(`categories/${categoryId}`);
    canManage = !!own.category.permissions?.includes('manage');
    canEditDb = !!own.category.permissions?.includes('edit');
  } catch { /* guests or hidden */ }
  const query = (withGroup: boolean) => ({
    sources, filters: [...(cfg.filters ?? []), ...extra], q: search, sort, columns: cfg.columns?.length ? ['title', ...cfg.columns] : [],
    limit: cfg.per_page || 50, offset, lang: state.lang, exclude: elementId,
    ...(withGroup && group ? { group, agg: 'count' } : {}),
  });
  const editRow = async (r: Row) => {
    let data: Props;
    try { data = await api.get<Props>(`${BASE}/elements/${r.id}${qs({ lang: state.lang })}`); } catch (e) { toast(errorText(e), 'error'); return; }
    const form = await propertiesForm(data, { compact: true });
    formDialog(t('props.edit_title', { title: r.title }), [form.el], t('common.save'), async () => {
      const e = form.validate();
      if (e) throw new Error(e);
      await api.put(`${BASE}/elements/${r.id}${qs({ lang: state.lang })}`, form.value());
      toast(t('props.saved'));
      void load();
    }, { wide: true });
  };
  const params = (): Record<string, string> => ({ group: group || 'status', date: cfg.date || 'due', view, totals: 'false' });
  const load = async () => {
    out.setAttribute('aria-busy', 'true');
    try {
      if (view === 'chart') {
        if (!group) { out.replaceChildren(h('p', { class: 'ew-muted' }, t('props.db.choose_group'))); return; }
        const res = await api.post<Result>(`${BASE}/query`, query(true));
        count.textContent = t('props.db.count', { n: res.total });
        out.replaceChildren(chart('bar', chartData(res, { group, agg: 'count' }), t('props.db.chart_title', { name: colLabel(res.columns[0] ?? { key: group, label: group, type: 'text' }) })));
        pager.replaceChildren();
        return;
      }
      const res = await api.post<Result>(`${BASE}/query`, query(false));
      el.querySelector('.ewprop-fallback')?.remove();
      columns = res.columns;
      const cols = res.columns.filter((c) => c.key !== 'title');
      count.textContent = t('props.db.count', { n: res.total });
      let node: HTMLElement;
      if (!res.rows?.length) node = h('p', { class: 'ew-muted' }, t('props.no_match'));
      else switch (view) {
        case 'cards': node = cardsView(res, cols); break;
        case 'kanban': node = kanbanView(res, cols, params(), () => void load()); break;
        case 'calendar': node = calendarView(res, params()); break;
        case 'timeline': node = timelineView(res, params()); break;
        default: node = tableView(res, cols, { group }, sort, (k) => {
          const cur = sort.find((x) => x.key === k);
          sort = [{ key: k, desc: cur ? !cur.desc : false }];
          offset = 0;
          void load();
        }, (r) => (r.can_edit ? button(t('props.edit'), () => void editRow(r), { variant: 'link', ariaLabel: `${t('props.edit')}: ${r.title}` }) : null));
      }
      out.replaceChildren(node);
      pager.replaceChildren();
      const n = res.rows?.length ?? 0;
      const lim = cfg.per_page || 50;
      if (res.total > n || offset > 0) {
        pager.append(h('span', null, t('props.shown_of', { from: offset + 1, to: offset + n, total: res.total })), ' ',
          offset > 0 ? button(t('props.prev_page'), () => { offset = Math.max(0, offset - lim); void load(); }, { variant: 'subtle' }) : '',
          offset + n < res.total ? button(t('props.next_page'), () => { offset += lim; void load(); }, { variant: 'subtle' }) : '');
      }
    } catch (e) {
      out.replaceChildren(h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, errorText(e)));
    } finally {
      out.setAttribute('aria-busy', 'false');
    }
  };
  // keys for filters and grouping
  const keys = await api.get<{ key: string; type: string; label: string }[]>(`${BASE}/keys${qs({ category: categoryId, lang: state.lang })}`).catch(() => []);
  const drawChips = () => chips.replaceChildren(...extra.map((f, i) => {
    const k = keys.find((x) => x.key === f.key);
    const text = `${k?.label ?? f.key} ${t(`props.op.${f.op}`)}${f.value !== undefined ? ` ${Array.isArray(f.value) ? f.value.join(', ') : String(f.value)}` : ''}`;
    return h('li', { class: 'ew-chip' }, h('span', null, text), button('×', () => { extra.splice(i, 1); drawChips(); offset = 0; void load(); }, { variant: 'subtle', ariaLabel: t('common.remove_item', { name: text }) }));
  }));
  const addFilter = () => {
    const key = select(keys.map((k) => ({ value: k.key, label: k.label })));
    const op = select([]);
    const val = textInput();
    const sync = () => {
      const type = keys.find((k) => k.key === key.value)?.type ?? 'text';
      op.replaceChildren(...filterOps(type).map((o) => h('option', { value: o }, t(`props.op.${o}`))));
      val.type = ['date', 'datetime', 'daterange'].includes(type) ? 'date' : 'text';
    };
    key.addEventListener('change', sync);
    op.addEventListener('change', () => { val.disabled = ['empty', 'notempty', 'me'].includes(op.value); });
    sync();
    formDialog(t('props.db.add_filter'), [field(t('props.flt.key'), key), field(t('props.flt.op'), op), field(t('props.flt.value'), val, { help: t('props.flt.date_hint') })], t('props.db.apply'), async () => {
      const type = keys.find((k) => k.key === key.value)?.type ?? 'text';
      const f: Filter = { key: key.value, op: op.value };
      if (!['empty', 'notempty', 'me'].includes(op.value)) {
        if (!val.value.trim()) throw new Error(t('props.required', { name: t('props.flt.value') }));
        const num = Number(val.value.replace(',', '.'));
        const v: unknown = ['integer', 'decimal', 'percent', 'currency', 'unit', 'rating', 'progress', 'formula'].includes(type) && !Number.isNaN(num) ? num : val.value.trim();
        f.value = ['has', 'hasnot'].includes(op.value) ? String(v).split(',').map((x) => x.trim()) : v;
      }
      extra.push(f);
      drawChips();
      offset = 0;
      await load();
    });
  };
  const q = textInput({ type: 'search', placeholder: t('props.search_title') });
  q.setAttribute('aria-label', t('props.search_title'));
  let timer = 0;
  q.addEventListener('input', () => { window.clearTimeout(timer); timer = window.setTimeout(() => { search = q.value.trim(); offset = 0; void load(); }, 300); });
  const viewSel = select(VIEWS.map((v) => ({ value: v, label: t(`props.view.${v}`) })), view);
  viewSel.setAttribute('aria-label', t('props.db.view'));
  viewSel.addEventListener('change', () => { view = viewSel.value; offset = 0; void load(); });
  const groupSel = select([{ value: '', label: t('props.db.no_group') }, ...keys.filter((k) => !['textarea', 'url', 'email'].includes(k.type)).map((k) => ({ value: k.key, label: k.label }))], group);
  groupSel.setAttribute('aria-label', t('props.db.group_by'));
  groupSel.addEventListener('change', () => { group = groupSel.value; void load(); });
  // saved views: personal ones and shared ones of the database
  interface SavedView { id: number; name: string; shared: boolean; mine: boolean; state: { search: string; view: string; group: string; filters: Filter[]; sort: SortKey[] } }
  let saved: SavedView[] = [];
  const savedSel = select([]);
  savedSel.setAttribute('aria-label', t('props.db.saved_views'));
  const drawSaved = () => {
    savedSel.replaceChildren(h('option', { value: '' }, t('props.db.saved_choose')),
      ...saved.map((v) => h('option', { value: String(v.id) }, v.shared ? `${v.name} (${t('props.db.shared')})` : v.name)));
    savedSel.hidden = !saved.length;
    delSaved.hidden = !saved.length;
  };
  const loadSaved = async () => {
    if (!elementId) return;
    try { saved = await api.get<SavedView[]>(`${BASE}/databases/${elementId}/views`); } catch { saved = []; }
    drawSaved();
  };
  savedSel.addEventListener('change', () => {
    const v = saved.find((x) => String(x.id) === savedSel.value);
    if (!v) return;
    search = v.state.search ?? ''; q.value = search;
    view = VIEWS.includes(v.state.view) ? v.state.view : view; viewSel.value = view;
    group = v.state.group ?? ''; groupSel.value = group;
    extra.splice(0, extra.length, ...(v.state.filters ?? []));
    sort = v.state.sort ?? [];
    offset = 0;
    drawChips();
    void load();
    announce(t('props.db.view_applied', { name: v.name }));
  });
  const saveView = button(t('props.db.save_view'), () => {
    const name = textInput({ maxlength: 100, required: true });
    const shared = canEditDb ? checkbox(t('props.db.share_view'), false, { help: t('props.db.share_view_help') }) : null;
    formDialog(t('props.db.save_view'), [field(t('common.name'), name, { required: true }), shared?.el ?? ''], t('common.save'), async () => {
      if (!name.value.trim()) throw new Error(t('props.required', { name: t('common.name') }));
      await api.post(`${BASE}/databases/${elementId}/views`, { name: name.value.trim(), shared: !!shared?.input.checked, state: { search, view, group, filters: [...extra], sort } });
      toast(t('common.saved'));
      await loadSaved();
    });
  }, { variant: 'subtle' });
  const delSaved = button(t('props.db.delete_view'), async () => {
    const v = saved.find((x) => String(x.id) === savedSel.value);
    if (!v) { toast(t('props.db.saved_choose'), 'error'); return; }
    if (!v.mine && !(v.shared && canEditDb)) { toast(t('error.forbidden'), 'error'); return; }
    try { await api.del(`${BASE}/views/${v.id}`); toast(t('props.db.view_deleted')); await loadSaved(); } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'subtle' });
  const tools = h('div', { class: 'ewprop-tools ewdb-tools' }, q, viewSel, groupSel,
    elementId && state.user ? savedSel : '', elementId && state.user ? saveView : '', elementId && state.user ? delSaved : '',
    button(t('props.db.add_filter'), addFilter, { variant: 'secondary' }),
    downloadButtons(() => ({ ...query(false), limit: 1000, offset: 0 }), () => 'database'),
    canCreate ? h('a', { class: 'ew-btn ew-btn--primary', href: `/c/${target}/new${qs({ type: cfg.new_type || undefined, return: location.pathname })}` }, t('props.db.new')) : '',
    canManage ? h('a', { class: 'ew-btn ew-btn--subtle', href: `/c/${categoryId}/settings?tab=properties` }, t('props.db.manage')) : '');
  el.prepend(tools, chips, count);
  el.append(out, pager);
  void columns;
  void loadSaved();
  await load();
}
