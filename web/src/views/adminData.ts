// Administration → Data for reports: the kinds of data that blocks and
// plugins keep for reports (for example decisions), whether they are
// completely built, their entries, and building them again.
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { pick, t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, errorText, pageHeader } from '../components/ui';

interface DataField { key: string; type: string; labels: Record<string, string>; option_labels?: Record<string, Record<string, string>> }
interface DataKind { kind: string; version: number; labels: Record<string, string>; fields: DataField[]; plugin: string; complete: boolean }
interface DataEntry { element_id: number; title: string; path: string; lang: string; source: string; index: number; data: Record<string, unknown>; updated_at: string }
interface DataResult { items: DataEntry[]; total: number; complete: boolean; people?: Record<string, string> }

/** value shows a value of a field as text */
function value(f: DataField, v: unknown, people: Record<string, string>): string {
  if (v === undefined || v === null || v === '') return '';
  // a person: the name, if the reader may see them, otherwise the number
  if (f.type === 'person') return people[String(v)] ?? `#${String(v)}`;
  if (Array.isArray(v)) return v.join(', ');
  if (typeof v === 'boolean') return v ? t('common.yes') : t('common.no');
  const s = String(v);
  return f.option_labels?.[s] ? pick(f.option_labels[s]) : s;
}

export async function dataAdmin(panel: HTMLElement): Promise<void> {
  const kinds = await api.get<DataKind[]>('data');
  const entries = h('div', { 'aria-live': 'polite' });

  const rebuild = async (kind: string) => {
    try {
      await api.post('admin/data/rebuild', { kind });
      // the message once the page shows the new state
      try { await dataAdmin(panel); } finally { toast(t('data.rebuild_started')); }
    } catch (e) { toast(errorText(e), 'error'); }
  };

  const show = async (k: DataKind) => {
    try {
      const res = await api.get<DataResult>(`data/${encodeURIComponent(k.kind)}${qs({ limit: 50 })}`);
      const fields = k.fields.filter((f) => f.type !== 'long');
      const id = `data-${k.kind.replace(/[^a-z0-9]/g, '-')}`;
      const rows = res.items.map((it) => h('tr', null,
        h('td', null, h('a', { href: it.path }, it.title || `#${it.element_id}`)),
        h('td', null, it.lang),
        ...fields.map((f) => h('td', null, value(f, it.data[f.key], res.people ?? {}))),
        h('td', null, fmtDate(it.updated_at, state.lang))));
      mount(entries, h('section', { class: 'ew-section', 'aria-labelledby': id },
        h('h2', { id }, t('data.entries_of', { name: pick(k.labels) })),
        h('p', { class: 'ew-muted' }, t('data.shown', { n: res.items.length, total: res.total })),
        h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('data.entries_of', { name: pick(k.labels) })),
          h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('data.page')), h('th', { scope: 'col' }, t('data.lang')),
            ...fields.map((f) => h('th', { scope: 'col' }, pick(f.labels))), h('th', { scope: 'col' }, t('data.updated')))),
          h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(fields.length + 3) }, t('data.no_entries')))]))))));
    } catch (e) { toast(errorText(e), 'error'); }
  };

  const rows = kinds.map((k) => h('tr', null,
    h('th', { scope: 'row' }, pick(k.labels), h('br'), h('code', null, k.kind)),
    h('td', null, k.plugin),
    h('td', null, k.complete ? t('data.complete') : t('data.building')),
    h('td', null, h('div', { class: 'ew-actions' },
      button(t('data.show'), () => show(k), { ariaLabel: t('data.show_of', { name: pick(k.labels) }) }),
      button(t('data.rebuild'), () => rebuild(k.kind), { variant: 'subtle', ariaLabel: t('data.rebuild_of', { name: pick(k.labels) }) })))));

  mount(panel,
    pageHeader(t('admin.data'), kinds.length ? button(t('data.rebuild_all'), () => rebuild('')) : ''),
    h('p', null, t('data.intro')),
    h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('admin.data')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('data.kind')), h('th', { scope: 'col' }, t('data.plugin')),
        h('th', { scope: 'col' }, t('data.state')), h('th', { scope: 'col' }, t('common.actions')))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '4' }, t('data.no_kinds')))])))),
    entries);
}
