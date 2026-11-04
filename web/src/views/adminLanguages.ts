// Languages and translation of the user interface: progress per language
// and component, translation editor with plural forms, filters for missing
// and outdated texts, import and export (JSON, XLIFF 2.0, PO).
// Who may translate: core.i18n.manage (all languages, add/remove languages)
// or core.i18n.translate.<lang> (one language).
import { api, postRaw, qs } from '../core/api';
import { h, mount, uid } from '../core/dom';
import { pluralForm, t } from '../core/i18n';
import { navigate } from '../core/router';
import { canTranslate, hasPerm } from '../core/state';
import { toast } from '../core/announce';
import { button, confirmDialog, errorText, field, pageHeader, select, textArea, textInput } from '../components/ui';
import { langName } from '../components/widgets';

interface LangStatus { lang: string; total: number; translated: number; outdated: number; missing: number; percent: number; default: boolean; can_translate: boolean }
interface UIString { key: string; source: string; plural: boolean; forms?: string[]; reference: Record<string, string>; value: Record<string, string>; custom?: string[]; status: 'file' | 'custom' | 'outdated' | 'missing'; hash: string }
interface SetResult { written: number; removed: number; unchanged: number; unknown?: string[] }

const PAGE = 100;
const FILTERS = ['todo', 'missing', 'outdated', 'custom', 'all'] as const;
const FORMATS = [{ value: 'json', label: 'JSON' }, { value: 'xliff', label: 'XLIFF 2.0' }, { value: 'po', label: 'PO (gettext)' }];

const reload = () => navigate(location.pathname + location.search, { replace: true });

export async function languagesAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const edit = query.get('edit');
  if (edit) return translationEditor(panel, edit, query);
  const manage = hasPerm('core.i18n.manage');
  const st = await api.get<LangStatus[]>('i18n/status');
  const rows = st.map((l) => h('tr', null,
    h('th', { scope: 'row' }, langName(l.lang), ' ', h('code', null, l.lang), l.default ? h('span', { class: 'ew-badge ew-badge--info' }, ` ${t('languages.default')}`) : null),
    h('td', null, h('progress', { max: '100', value: String(l.percent), 'aria-label': t('languages.progress_of', { lang: langName(l.lang) }) }),
      ` ${t('languages.percent', { percent: l.percent, translated: l.translated, total: l.total })}`,
      l.outdated ? h('span', { class: 'ew-badge ew-badge--warn' }, ` ${t('languages.n_outdated', { n: l.outdated })}`) : null,
      l.missing ? h('span', { class: 'ew-badge' }, ` ${t('languages.n_missing', { n: l.missing })}`) : null),
    h('td', { class: 'ew-actions' },
      l.can_translate ? h('a', { href: `/admin/languages?edit=${l.lang}` }, t('languages.translate'), h('span', { class: 'sr-only' }, ` ${langName(l.lang)}`)) : null,
      manage && !l.default ? button(t('common.remove'), async () => {
        if (!(await confirmDialog(t('languages.remove_confirm', { lang: langName(l.lang) }), t('common.remove'), true))) return;
        try { await api.del(`admin/languages/${l.lang}`); setTimeout(() => location.reload(), 300); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.remove')} ${langName(l.lang)}` }) : null)));
  let add: HTMLElement | null = null;
  if (manage) {
    const code = textInput({ placeholder: 'fr', maxlength: 8 });
    add = h('form', { class: 'ew-inline-form', novalidate: true }, field(t('languages.code'), code, { inline: true, help: t('languages.code_help') }), button(t('languages.add'), () => undefined, { type: 'submit', variant: 'primary' }));
    add.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      try { await api.post('admin/languages', { code: code.value.trim() }); toast(t('languages.added')); setTimeout(() => location.reload(), 600); } catch (e) { toast(errorText(e), 'error'); }
    });
  }
  mount(panel, pageHeader(t('admin.languages')), h('p', null, t('languages.help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('admin.languages')),
      h('thead', null, h('tr', null, ...[t('common.language'), t('languages.progress'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...rows)),
    manage ? h('h2', null, t('languages.add_title')) : null, add,
    manage ? null : h('p', { class: 'ew-help' }, t('languages.translate_only')));
}

/** Numbers that use a plural form, e.g. "1, 21, 31" (shown next to the field). */
function examples(lang: string, form: string): string {
  const out: string[] = [];
  for (let i = 0; i <= 200 && out.length < 3; i++) if (pluralForm(i, lang) === form) out.push(String(i));
  if (!out.length && form === 'other') out.push('1.5');
  return out.join(', ');
}

function matches(s: UIString, filter: string, source: string, q: string): boolean {
  if (source && s.source !== source) return false;
  switch (filter) {
    case 'todo': if (s.status !== 'missing' && s.status !== 'outdated') return false; break;
    case 'missing': case 'outdated': if (s.status !== filter) return false; break;
    case 'custom': if (!s.custom?.length) return false; break;
  }
  if (!q) return true;
  const n = q.toLowerCase();
  return s.key.toLowerCase().includes(n) || [...Object.values(s.reference), ...Object.values(s.value)].some((x) => x.toLowerCase().includes(n));
}

async function translationEditor(panel: HTMLElement, lang: string, query: URLSearchParams): Promise<void> {
  if (!canTranslate(lang)) {
    mount(panel, h('h1', null, t('error.forbidden')));
    return;
  }
  const filter = query.get('filter') ?? 'todo';
  const source = query.get('source') ?? '';
  const q = query.get('q') ?? '';
  const page = Math.max(0, Number(query.get('page') ?? 0) || 0);
  const strings = await api.get<UIString[]>(`admin/i18n/${encodeURIComponent(lang)}`);
  const go = (p: Record<string, string | number>) => navigate(`/admin/languages${qs({ edit: lang, filter, source, q, ...p })}`, { replace: true });

  // progress per component
  const comps = new Map<string, { total: number; done: number; outdated: number; missing: number }>();
  for (const s of strings) {
    const c = comps.get(s.source) ?? { total: 0, done: 0, outdated: 0, missing: 0 };
    c.total++;
    if (s.status === 'missing') c.missing++; else if (s.status === 'outdated') c.outdated++; else c.done++;
    comps.set(s.source, c);
  }
  const compNames = [...comps.keys()].sort((a, b) => (a === 'core' ? -1 : b === 'core' ? 1 : a.localeCompare(b)));
  const compTable = h('table', { class: 'ew-table ew-table--compact' }, h('caption', null, t('languages.by_component')),
    h('thead', null, h('tr', null, ...[t('languages.component'), t('languages.progress'), t('languages.status.outdated'), t('languages.status.missing')].map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...compNames.map((name) => {
      const c = comps.get(name)!;
      const pct = c.total ? Math.floor((c.done * 100) / c.total) : 100;
      return h('tr', null,
        h('th', { scope: 'row' }, h('a', { href: `/admin/languages${qs({ edit: lang, filter: 'todo', source: name })}`, 'aria-current': name === source ? 'true' : undefined }, name)),
        h('td', null, `${pct} % (${c.done}/${c.total})`), h('td', null, String(c.outdated)), h('td', null, String(c.missing)));
    })));

  // filters
  const filterSel = select(FILTERS.map((f) => ({ value: f, label: t(`languages.filter.${f}`) })), filter);
  const compSel = select([{ value: '', label: t('languages.all_components') }, ...compNames.map((c) => ({ value: c, label: c }))], source);
  const search = textInput({ type: 'search', value: q });
  const ff = h('form', { class: 'ew-inline-form', role: 'search', 'aria-label': t('languages.filter') },
    field(t('languages.filter'), filterSel, { inline: true }), field(t('languages.component'), compSel, { inline: true }),
    field(t('languages.search'), search, { inline: true }), button(t('search.submit'), () => undefined, { type: 'submit', variant: 'secondary' }));
  ff.addEventListener('submit', (ev) => { ev.preventDefault(); go({ filter: filterSel.value, source: compSel.value, q: search.value.trim(), page: 0 }); });
  filterSel.addEventListener('change', () => go({ filter: filterSel.value, page: 0 }));
  compSel.addEventListener('change', () => go({ source: compSel.value, page: 0 }));

  // rows
  const shown = strings.filter((s) => matches(s, filter, source, q));
  const pageItems = shown.slice(page * PAGE, (page + 1) * PAGE);
  const changed = new Map<string, string>();
  const statusBadge = (s: UIString) => h('span', { class: `ew-badge ${s.status === 'outdated' ? 'ew-badge--warn' : s.status === 'missing' ? '' : 'ew-badge--ok'}` }, t(`languages.status.${s.status}`));
  const rows = pageItems.map((s) => {
    const forms = s.plural ? (s.forms ?? []) : [''];
    const controls = forms.map((f) => {
      const k = f ? `${s.key}_${f}` : s.key;
      const ref = s.reference[f] ?? '';
      const long = ref.length > 80 || ref.includes('\n');
      const id = uid('tr');
      const inp = long ? textArea({ value: s.value[f] ?? '', rows: 3, lang }) : textInput({ value: s.value[f] ?? '', lang, id });
      inp.id = id;
      inp.addEventListener('input', () => changed.set(k, inp.value));
      const label = f ? `${t('languages.form', { form: t(`languages.plural.${f}`), examples: examples(lang, f) })}` : t('languages.translation');
      return h('div', { class: 'ew-tr-form' },
        h('label', { for: id }, label, h('span', { class: 'sr-only' }, ` – ${s.key}`)),
        f ? h('div', { class: 'ew-help', lang: 'en' }, ref) : null, inp);
    });
    const confirm = s.status === 'outdated' ? button(t('languages.confirm'), async () => {
      const vals: Record<string, string> = {};
      for (const f of forms) vals[f ? `${s.key}_${f}` : s.key] = s.value[f] ?? '';
      try { await api.put(`admin/i18n/${encodeURIComponent(lang)}`, vals); toast(t('languages.confirmed')); reload(); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: `${t('languages.confirm')}: ${s.key}` }) : null;
    return h('tr', null,
      h('td', null, h('code', null, s.key), h('br'), h('span', { class: 'ew-help' }, s.source), h('br'), statusBadge(s), s.plural ? h('span', { class: 'ew-badge ew-badge--info' }, ` ${t('languages.plural_badge')}`) : null),
      h('td', { lang: 'en' }, s.plural ? h('ul', { class: 'ew-plain-list' }, ...Object.entries(s.reference).filter(([f]) => f !== '').map(([f, v]) => h('li', null, h('small', null, `${t(`languages.plural.${f}`)}: `), v))) : s.reference['']),
      h('td', null, ...controls, confirm));
  });
  const save = button(t('common.save'), async () => {
    if (!changed.size) { toast(t('languages.nothing_changed')); return; }
    try {
      const r = await api.put<SetResult>(`admin/i18n/${encodeURIComponent(lang)}`, Object.fromEntries(changed));
      toast(t('languages.saved', { n: r.written + r.removed }));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'primary' });
  const pager = shown.length > PAGE ? h('nav', { class: 'ew-pager', 'aria-label': t('languages.pages') },
    page > 0 ? button(t('common.previous'), () => go({ page: page - 1 }), { variant: 'secondary' }) : null,
    h('span', null, t('languages.range', { from: page * PAGE + 1, to: Math.min(shown.length, (page + 1) * PAGE), total: shown.length })),
    (page + 1) * PAGE < shown.length ? button(t('common.next'), () => go({ page: page + 1 }), { variant: 'secondary' }) : null) : null;

  mount(panel, pageHeader(t('languages.editor_title', { lang: langName(lang) }), save),
    h('p', null, h('a', { href: '/admin/languages' }, t('languages.back'))),
    compTable, exchange(lang, source, filter), ff,
    h('p', { role: 'status' }, t('languages.shown', { n: shown.length })),
    h('table', { class: 'ew-table ew-tr-table' }, h('caption', { class: 'sr-only' }, t('languages.editor_title', { lang: langName(lang) })),
      h('thead', null, h('tr', null, ...[t('languages.key'), t('languages.reference'), t('languages.translation')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '3' }, t('languages.nothing')))]))),
    pager, rows.length ? h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => save.click(), { variant: 'primary' })) : null);
}

/** Import and export of the texts of one language. */
function exchange(lang: string, source: string, filter: string): HTMLElement {
  const fmt = select(FORMATS, 'xliff');
  const scope = select([
    { value: 'all', label: t('languages.export_all') },
    { value: 'missing', label: t('languages.export_todo') },
    { value: 'custom', label: t('languages.export_custom') },
  ], filter === 'todo' || filter === 'missing' || filter === 'outdated' ? 'missing' : 'all');
  const link = h('a', { class: 'ew-btn ew-btn--secondary', download: '' }, t('languages.export'));
  const upd = () => { link.setAttribute('href', `/api/v1/admin/i18n/${encodeURIComponent(lang)}/export${qs({ format: fmt.value, source, filter: scope.value })}`); };
  fmt.addEventListener('change', upd);
  scope.addEventListener('change', upd);
  upd();
  const file = h('input', { type: 'file', accept: '.json,.xlf,.xliff,.po,application/json,application/xliff+xml,text/x-gettext-translation', class: 'ew-input' }) as HTMLInputElement;
  const result = h('div', { role: 'status' });
  const imp = button(t('languages.import'), async () => {
    const f = file.files?.[0];
    if (!f) { toast(t('languages.choose_file'), 'error'); return; }
    try {
      const r = await postRaw<SetResult>(`admin/i18n/${encodeURIComponent(lang)}/import`, f);
      mount(result, h('p', null, t('languages.imported', { written: r.written, unchanged: r.unchanged, unknown: r.unknown?.length ?? 0 })));
      toast(t('languages.import_done'));
      setTimeout(reload, 1500);
    } catch (e) { mount(result, h('p', { class: 'ew-form-alert' }, errorText(e))); }
  }, { variant: 'secondary' });
  return h('details', { class: 'ew-panel' }, h('summary', null, t('languages.exchange')),
    h('p', null, t('languages.exchange_help')),
    h('div', { class: 'ew-inline-form' }, field(t('languages.format'), fmt, { inline: true }), field(t('languages.export_scope'), scope, { inline: true }), link),
    source ? h('p', { class: 'ew-help' }, t('languages.export_component', { name: source })) : null,
    h('div', { class: 'ew-inline-form' }, field(t('languages.import_file'), file, { inline: true, help: t('languages.import_help') }), imp), result);
}
