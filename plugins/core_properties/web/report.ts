// The property report in the browser: the views of the macro "properties"
// with sorting, filters for readers, paging and CSV download. The data comes
// from POST /report with the rights of the reader.
import { api, button, errorText, field, formDialog, h, select, state, t, textInput, toast, uid } from '@ewiki/sdk';
import { BASE, builtinLabel, choiceLabel, formatNumber, formatValue, plainValue, type Options, type Ref } from './model';
import { chart, type ChartData } from './charts';

export interface Column { key: string; label: string; type: string; options?: Options; builtin?: boolean }
export interface Row { can_edit?: boolean; id: number; title: string; link: string; type: string; category_id: number; tags: string[]; created_at: string; updated_at: string; created_by: number | null; updated_by: number | null; values: Record<string, unknown> }
export interface Group { key: unknown; key2?: unknown; value: number; count: number }
export interface Result { rows?: Row[]; groups?: Group[]; total: number; columns: Column[]; refs: Record<string, Ref> }
export interface Filter { key: string; op: string; value?: unknown }
export interface SortKey { key: string; desc?: boolean }

export const colLabel = (c: Column) => (c.builtin || !c.label ? builtinLabel(c.key) : c.label);

function value(r: Row, key: string): unknown {
  switch (key) {
    case 'title': return r.title;
    case 'type': return r.type;
    case 'tags': return r.tags;
    case 'category': return [r.category_id];
    case 'created_at': return r.created_at;
    case 'updated_at': return r.updated_at;
    case 'created_by': return r.created_by ? [r.created_by] : null;
    case 'updated_by': return r.updated_by ? [r.updated_by] : null;
  }
  return r.values[key];
}

const isNum = (c: Column) => ['integer', 'decimal', 'percent', 'currency', 'unit', 'rating', 'progress', 'duration', 'formula'].includes(c.type);

export function groupLabel(v: unknown, c: Column | undefined, refs: Record<string, Ref>): string {
  if (v === null || v === undefined || v === '') return t('props.empty_group');
  if (!c) return String(v);
  if (['user', 'group', 'element', 'category'].includes(c.type)) return refs[`${c.type}:${v}`]?.name ?? t('props.hidden_ref');
  if (c.type === 'boolean') return v ? t('common.yes') : t('common.no');
  if (['date', 'datetime', 'daterange', 'created_at', 'updated_at'].includes(c.type) || c.key.endsWith('_at')) return String(v).slice(0, 10);
  if (c.type === 'select' || c.type === 'status' || c.type === 'multiselect') return choiceLabel(c.options?.choices?.find((x) => x.value === v), String(v));
  if (typeof v === 'number') return formatNumber(v, c.type, c.options);
  return String(v);
}

function csvSafe(s: string): string {
  return s && '=+-@\t\r'.includes(s[0]) ? `'${s}` : s;
}

/** Downloads rows as CSV or Excel file made by the server (body: a query or a report). */
export async function serverDownload(format: 'csv' | 'xlsx', body: unknown, name: string): Promise<void> {
  const res = await fetch(`/api/v1/${BASE}/query.${format}`, {
    method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: { message?: string } }).error?.message ?? msg; } catch { /* not JSON */ }
    throw new Error(msg);
  }
  const blob = await res.blob();
  const a = h('a', { href: URL.createObjectURL(blob), download: `${(name || 'report').replace(/[\\/:*?"<>|]+/g, '_')}.${format}` });
  document.body.append(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

/** Buttons "CSV" and "Excel" for a query or report body. */
export function downloadButtons(body: () => unknown, name: () => string): HTMLElement {
  const run = (format: 'csv' | 'xlsx') => async () => {
    try { await serverDownload(format, body(), name()); } catch (e) { toast(errorText(e), 'error'); }
  };
  return h('span', { class: 'ewprop-downloads', role: 'group', 'aria-label': t('props.download') },
    button(t('props.csv'), run('csv'), { variant: 'subtle' }), button(t('props.xlsx'), run('xlsx'), { variant: 'subtle' }));
}

export function download(res: Result, cols: Column[], name: string): void {
  const esc = (s: string) => `"${csvSafe(s).replace(/"/g, '""')}"`;
  const lines = [[t('props.element'), 'Link', ...cols.map(colLabel)].map(esc).join(';')];
  for (const r of res.rows ?? []) lines.push([r.title, location.origin + r.link, ...cols.map((c) => plainValue(c.type, value(r, c.key), c.options, res.refs))].map(esc).join(';'));
  const blob = new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' });
  const a = h('a', { href: URL.createObjectURL(blob), download: `${name || 'report'}.csv` });
  document.body.append(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}

// ---------------------------------------------------------------- views of rows

export function tableView(res: Result, cols: Column[], p: Record<string, string>, sort: SortKey[], onSort: (k: string) => void, rowAction?: (r: Row) => HTMLElement | null): HTMLElement {
  const rows = res.rows ?? [];
  const groupKey = p.group;
  const gcol = res.columns.find((c) => c.key === groupKey) ?? cols.find((c) => c.key === groupKey);
  const head = h('tr', null, ...[{ key: 'title', label: '', type: 'text', builtin: true } as Column, ...cols].map((c) => {
    const s = sort.find((x) => x.key === c.key);
    const label = c.key === 'title' ? t('props.element') : colLabel(c);
    return h('th', { scope: 'col', 'aria-sort': s ? (s.desc ? 'descending' : 'ascending') : 'none', class: isNum(c) ? 'ewprop-num' : '' },
      h('button', { type: 'button', class: 'ewprop-sort', onclick: () => onSort(c.key) }, label, h('span', { 'aria-hidden': 'true' }, s ? (s.desc ? ' ▼' : ' ▲') : ' ↕')));
  }), rowAction ? h('th', { scope: 'col' }, t('common.actions')) : '');
  const line = (r: Row) => h('tr', null, h('th', { scope: 'row' }, h('a', { href: r.link }, r.title)),
    ...cols.map((c) => h('td', { class: isNum(c) ? 'ewprop-num' : '' }, formatValue(c.type, value(r, c.key), c.options, res.refs))),
    rowAction ? h('td', null, rowAction(r) ?? '') : '');
  const totals = (list: Row[], label: string) => h('tr', { class: 'ewprop-total' }, h('th', { scope: 'row' }, label),
    ...cols.map((c) => {
      if (!isNum(c)) return h('td');
      const vals = list.map((r) => value(r, c.key)).filter((v) => typeof v === 'number') as number[];
      const sum = vals.reduce((a, v) => a + v, 0);
      // shares and ratings are averaged, amounts summed
      const avg = ['percent', 'progress', 'rating'].includes(c.type);
      const v = avg ? (vals.length ? Math.round((sum / vals.length) * 10) / 10 : 0) : sum;
      return h('td', { class: 'ewprop-num' }, formatNumber(v, c.type, c.options), avg ? h('span', { class: 'sr-only' }, ` (${t('props.average')})`) : '');
    }), rowAction ? h('td') : '');
  const body: HTMLElement[] = [];
  if (groupKey) {
    const groups = new Map<string, Row[]>();
    for (const r of rows) {
      const v = value(r, groupKey);
      const list = Array.isArray(v) ? (v.length ? v : [null]) : [v];
      for (const x of list) {
        const k = JSON.stringify(x ?? null);
        groups.set(k, [...(groups.get(k) ?? []), r]);
      }
    }
    for (const [k, list] of groups) {
      body.push(h('tr', { class: 'ewprop-group-row' }, h('th', { scope: 'rowgroup', colspan: String(cols.length + (rowAction ? 2 : 1)) }, `${groupLabel(JSON.parse(k), gcol, res.refs)} (${list.length})`)));
      body.push(...list.map(line));
      if (p.totals === 'true') body.push(totals(list, t('props.subtotal')));
    }
  } else body.push(...rows.map(line));
  if (p.totals === 'true' && rows.length) body.push(totals(rows, t('props.total')));
  return h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table ewprop-table' }, h('caption', { class: 'sr-only' }, p.title || t('props.report')),
    h('thead', null, head), h('tbody', null, ...body)));
}

function dl(r: Row, cols: Column[], refs: Record<string, Ref>): HTMLElement {
  return h('dl', { class: 'ewprop-card-props' }, ...cols.filter((c) => c.key !== 'title').flatMap((c) => [h('dt', null, colLabel(c)), h('dd', null, formatValue(c.type, value(r, c.key), c.options, refs))]));
}

export function cardsView(res: Result, cols: Column[], list = false): HTMLElement {
  const rows = res.rows ?? [];
  if (list) return h('ul', { class: 'ewprop-list-view' }, ...rows.map((r) => h('li', null, h('a', { href: r.link }, r.title), ' ',
    h('span', { class: 'ew-muted' }, cols.filter((c) => c.key !== 'title').map((c) => plainValue(c.type, value(r, c.key), c.options, res.refs)).filter(Boolean).join(' · ')))));
  return h('ul', { class: 'ewprop-cards' }, ...rows.map((r) => h('li', { class: 'ewprop-card' }, h('h3', null, h('a', { href: r.link }, r.title)), dl(r, cols, res.refs))));
}

export function kanbanView(res: Result, cols: Column[], p: Record<string, string>, reload: () => void): HTMLElement {
  const key = p.group || 'status';
  const gcol = res.columns.find((c) => c.key === key);
  const rows = res.rows ?? [];
  const order: unknown[] = gcol?.options?.choices?.map((c) => c.value) ?? [];
  for (const r of rows) { const v = value(r, key) ?? null; if (!order.some((x) => JSON.stringify(x) === JSON.stringify(v))) order.push(v); }
  if (!order.includes(null)) order.push(null);
  const canMove = gcol && ['select', 'status', 'boolean'].includes(gcol.type);
  const move = async (r: Row, to: unknown) => {
    try {
      await api.put(`${BASE}/elements/${r.id}`, { values: { [key]: to } });
      toast(t('props.moved', { title: r.title, to: groupLabel(to, gcol, res.refs) }));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const board = h('div', { class: 'ewprop-board' }, ...order.map((g) => {
    const items = rows.filter((r) => JSON.stringify(value(r, key) ?? null) === JSON.stringify(g));
    const colId = uid('ewp-col');
    const zone = h('ul', { class: 'ewprop-board-list', 'aria-labelledby': colId });
    for (const r of items) {
      const card = h('li', { class: 'ewprop-card', draggable: canMove ? 'true' : undefined }, h('h4', null, h('a', { href: r.link }, r.title)),
        dl(r, cols.filter((c) => c.key !== key), res.refs));
      if (canMove) {
        const sel = select(order.map((o) => ({ value: JSON.stringify(o), label: groupLabel(o, gcol, res.refs) })), JSON.stringify(g));
        sel.setAttribute('aria-label', t('props.move_to', { title: r.title }));
        sel.classList.add('ewprop-move');
        sel.addEventListener('change', () => void move(r, JSON.parse(sel.value)));
        card.append(sel);
        card.addEventListener('dragstart', (ev) => { ev.dataTransfer?.setData('text/plain', String(r.id)); });
      }
      zone.append(card);
    }
    if (canMove) {
      zone.addEventListener('dragover', (ev) => { ev.preventDefault(); zone.classList.add('is-over'); });
      zone.addEventListener('dragleave', () => zone.classList.remove('is-over'));
      zone.addEventListener('drop', (ev) => {
        ev.preventDefault();
        zone.classList.remove('is-over');
        const r = rows.find((x) => String(x.id) === ev.dataTransfer?.getData('text/plain'));
        if (r) void move(r, g);
      });
    }
    return h('section', { class: 'ewprop-board-col' }, h('h3', { id: colId }, `${groupLabel(g, gcol, res.refs)} `, h('span', { class: 'ew-badge' }, String(items.length))), zone);
  }));
  return board;
}

function dateOf(r: Row, key: string): { from: Date; to: Date } | null {
  const v = value(r, key);
  if (!v) return null;
  if (typeof v === 'object' && v !== null && 'from' in v) {
    const x = v as { from: string; to: string };
    return { from: new Date(`${x.from}T00:00:00`), to: new Date(`${x.to}T00:00:00`) };
  }
  const d = new Date(String(v).length === 10 ? `${v}T00:00:00` : String(v));
  return Number.isNaN(d.getTime()) ? null : { from: d, to: d };
}

export function calendarView(res: Result, p: Record<string, string>): HTMLElement {
  const key = p.date || 'due';
  const rows = (res.rows ?? []).map((r) => ({ r, d: dateOf(r, key) })).filter((x) => x.d);
  const first = rows.map((x) => x.d!.from).sort((a, b) => a.getTime() - b.getTime())[0] ?? new Date();
  let month = new Date(first.getFullYear(), first.getMonth(), 1);
  const now = new Date();
  if (rows.some((x) => x.d!.from.getFullYear() === now.getFullYear() && x.d!.from.getMonth() === now.getMonth())) month = new Date(now.getFullYear(), now.getMonth(), 1);
  const box = h('div', { class: 'ewprop-calendar' });
  const draw = () => {
    const title = month.toLocaleDateString(state.lang, { month: 'long', year: 'numeric' });
    const start = new Date(month);
    start.setDate(1 - ((month.getDay() + 6) % 7));
    const weeks: HTMLElement[] = [];
    for (let w = 0; w < 6; w++) {
      const cells: HTMLElement[] = [];
      for (let d = 0; d < 7; d++) {
        const day = new Date(start);
        day.setDate(start.getDate() + w * 7 + d);
        const hits = rows.filter((x) => x.d!.from <= day && day <= x.d!.to);
        const other = day.getMonth() !== month.getMonth();
        cells.push(h('td', { class: other ? 'is-other' : '' }, h('span', { class: 'ewprop-day' }, String(day.getDate())),
          hits.length ? h('ul', null, ...hits.map((x) => h('li', null, h('a', { href: x.r.link }, x.r.title)))) : ''));
      }
      weeks.push(h('tr', null, ...cells));
      if (start.getMonth() !== month.getMonth() && w > 3) break;
    }
    const days = Array.from({ length: 7 }, (_, i) => new Date(2024, 0, 1 + i).toLocaleDateString(state.lang, { weekday: 'short' }));
    box.replaceChildren(
      h('div', { class: 'ewprop-cal-nav' },
        button('‹', () => { month = new Date(month.getFullYear(), month.getMonth() - 1, 1); draw(); }, { variant: 'subtle', ariaLabel: t('props.prev_month') }),
        h('h3', { 'aria-live': 'polite' }, title),
        button('›', () => { month = new Date(month.getFullYear(), month.getMonth() + 1, 1); draw(); }, { variant: 'subtle', ariaLabel: t('props.next_month') })),
      h('table', { class: 'ewprop-cal' }, h('caption', { class: 'sr-only' }, title), h('thead', null, h('tr', null, ...days.map((x) => h('th', { scope: 'col' }, x)))), h('tbody', null, ...weeks)));
  };
  draw();
  return box;
}

export function timelineView(res: Result, p: Record<string, string>): HTMLElement {
  const key = p.date || 'period';
  const rows = (res.rows ?? []).map((r) => ({ r, d: dateOf(r, key) })).filter((x) => x.d).sort((a, b) => a.d!.from.getTime() - b.d!.from.getTime());
  if (!rows.length) return h('p', { class: 'ew-muted' }, t('props.no_dates'));
  const min = rows[0].d!.from.getTime();
  const max = Math.max(...rows.map((x) => x.d!.to.getTime())) + 86400000;
  const span = Math.max(86400000, max - min);
  const fmt = (d: Date) => d.toLocaleDateString(state.lang, { day: '2-digit', month: '2-digit', year: 'numeric' });
  const today = Date.now();
  return h('div', { class: 'ewprop-timeline' },
    h('ol', { class: 'ewprop-gantt' }, ...rows.map((x) => {
      const left = ((x.d!.from.getTime() - min) / span) * 100;
      const width = Math.max(1, ((x.d!.to.getTime() + 86400000 - x.d!.from.getTime()) / span) * 100);
      const range = x.d!.from.getTime() === x.d!.to.getTime() ? fmt(x.d!.from) : `${fmt(x.d!.from)} – ${fmt(x.d!.to)}`;
      return h('li', { class: 'ewprop-gantt-row' }, h('a', { href: x.r.link, class: 'ewprop-gantt-label' }, x.r.title),
        h('span', { class: 'ewprop-gantt-track' }, h('span', { class: 'ewprop-gantt-bar', style: `left:${left}%;width:${Math.min(width, 100 - left)}%` }),
          today >= min && today <= max ? h('span', { class: 'ewprop-gantt-today', style: `left:${((today - min) / span) * 100}%`, 'aria-hidden': 'true' }) : ''),
        h('span', { class: 'ewprop-gantt-dates' }, range));
    })));
}

function overdueView(res: Result, cols: Column[], p: Record<string, string>): HTMLElement {
  const key = p.date || 'due';
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const soon = new Date(today);
  soon.setDate(soon.getDate() + 14);
  const rows = (res.rows ?? []).map((r) => ({ r, d: dateOf(r, key) })).filter((x) => x.d && x.d.to < soon).sort((a, b) => a.d!.to.getTime() - b.d!.to.getTime());
  const days = (d: Date) => Math.round((d.getTime() - today.getTime()) / 86400000);
  const part = (title: string, list: typeof rows, cls: string) => h('section', { class: `ewprop-due ${cls}` }, h('h3', null, `${title} (${list.length})`),
    list.length ? h('ul', null, ...list.map((x) => {
      const n = days(x.d!.to);
      return h('li', null, h('a', { href: x.r.link }, x.r.title), ' ', h('span', { class: 'ew-badge' }, n < 0 ? t('props.days_overdue', { n: -n }) : n === 0 ? t('props.due_today') : t('props.due_in', { n })),
        ' ', h('span', { class: 'ew-muted' }, cols.filter((c) => c.key !== key && c.key !== 'title').map((c) => plainValue(c.type, value(x.r, c.key), c.options, res.refs)).filter(Boolean).join(' · ')));
    })) : h('p', { class: 'ew-muted' }, t('props.none')));
  return h('div', { class: 'ewprop-due-view' }, part(t('props.overdue'), rows.filter((x) => x.d!.to < today), 'is-overdue'), part(t('props.due_soon'), rows.filter((x) => x.d!.to >= today), 'is-soon'));
}

function completenessView(res: Result, cols: Column[]): HTMLElement {
  const rows = res.rows ?? [];
  const keys = cols.filter((c) => c.key !== 'title' && !c.builtin);
  if (!keys.length) return h('p', { class: 'ew-muted' }, t('props.choose_columns'));
  const filled = (r: Row) => keys.filter((c) => { const v = value(r, c.key); return v !== null && v !== undefined && v !== '' && !(Array.isArray(v) && !v.length); });
  const total = rows.length * keys.length;
  const have = rows.reduce((a, r) => a + filled(r).length, 0);
  const pct = total ? Math.round((have / total) * 100) : 100;
  const missing = rows.map((r) => ({ r, miss: keys.filter((c) => !filled(r).includes(c)) })).filter((x) => x.miss.length).sort((a, b) => b.miss.length - a.miss.length);
  return h('div', { class: 'ewprop-complete' },
    h('p', { class: 'ewprop-kpi-big' }, h('progress', { max: '100', value: String(pct), 'aria-label': t('props.complete_pct', { n: pct }) }), ` ${t('props.complete_pct', { n: pct })}`),
    h('ul', { class: 'ewprop-kpis' }, ...keys.map((c) => {
      const n = rows.filter((r) => filled(r).includes(c)).length;
      return h('li', null, h('strong', null, colLabel(c)), ` ${n}/${rows.length}`);
    })),
    missing.length ? h('table', { class: 'ew-table' }, h('caption', null, t('props.missing_values')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('props.element')), h('th', { scope: 'col' }, t('props.missing')))),
      h('tbody', null, ...missing.map((x) => h('tr', null, h('th', { scope: 'row' }, h('a', { href: x.r.link }, x.r.title)), h('td', null, x.miss.map(colLabel).join(', '))))))
      : h('p', null, t('props.all_complete')));
}

function mapView(res: Result, cols: Column[]): HTMLElement {
  const loc = cols.find((c) => c.type === 'location') ?? res.columns.find((c) => c.type === 'location');
  if (!loc) return h('p', { class: 'ew-muted' }, t('props.no_location'));
  const rows = (res.rows ?? []).map((r) => ({ r, l: value(r, loc.key) as { address: string; lat?: number; lng?: number } | null })).filter((x) => x.l);
  return h('ul', { class: 'ewprop-places' }, ...rows.map((x) => h('li', null, h('a', { href: x.r.link }, x.r.title), ': ', formatValue('location', x.l, undefined, res.refs))));
}

// ---------------------------------------------------------------- views of groups

export function chartData(res: Result, p: Record<string, string>): ChartData {
  const g1 = res.columns.find((c) => c.key === p.group);
  const g2 = res.columns.find((c) => c.key === p.group2);
  const vcol = res.columns.find((c) => c.key === p.value);
  const fmt = (n: number) => (p.agg && p.agg !== 'count' && vcol ? formatNumber(Math.round(n * 100) / 100, vcol.type, vcol.options) : new Intl.NumberFormat(state.lang, { maximumFractionDigits: 2 }).format(n));
  const groups = res.groups ?? [];
  const cats: string[] = [], keys: string[] = [];
  const order = g1?.options?.choices?.map((c) => JSON.stringify(c.value)) ?? [];
  for (const g of groups) { const k = JSON.stringify(g.key ?? null); if (!keys.includes(k)) keys.push(k); }
  keys.sort((a, b) => (order.indexOf(a) + 1 || 999) - (order.indexOf(b) + 1 || 999));
  for (const k of keys) cats.push(groupLabel(JSON.parse(k), g1, res.refs));
  const valueLabel = p.agg && p.agg !== 'count' ? `${t(`props.agg.${p.agg}`)} ${vcol ? colLabel(vcol) : ''}` : t('props.count');
  if (!p.group2 || !g2) return { categories: cats, series: [{ name: valueLabel, values: keys.map((k) => groups.find((g) => JSON.stringify(g.key ?? null) === k)?.value ?? 0) }], valueLabel, format: fmt };
  const s2: string[] = [];
  for (const g of groups) { const k = JSON.stringify(g.key2 ?? null); if (!s2.includes(k)) s2.push(k); }
  const order2 = g2.options?.choices?.map((c) => JSON.stringify(c.value)) ?? [];
  s2.sort((a, b) => (order2.indexOf(a) + 1 || 999) - (order2.indexOf(b) + 1 || 999));
  return {
    categories: cats, valueLabel, format: fmt,
    series: s2.map((k2) => ({ name: groupLabel(JSON.parse(k2), g2, res.refs), values: keys.map((k) => groups.find((g) => JSON.stringify(g.key ?? null) === k && JSON.stringify(g.key2 ?? null) === k2)?.value ?? 0) })),
  };
}

function pivotView(res: Result, p: Record<string, string>): HTMLElement {
  const d = chartData(res, p);
  const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);
  return h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table ewprop-pivot' }, h('caption', { class: 'sr-only' }, p.title || t('props.report')),
    h('thead', null, h('tr', null, h('td', null, ''), ...d.series.map((s) => h('th', { scope: 'col' }, s.name)), h('th', { scope: 'col' }, t('props.total')))),
    h('tbody', null, ...d.categories.map((c, i) => h('tr', null, h('th', { scope: 'row' }, c), ...d.series.map((s) => h('td', { class: 'ewprop-num' }, d.format(s.values[i] ?? 0))),
      h('td', { class: 'ewprop-num ewprop-total' }, d.format(sum(d.series.map((s) => s.values[i] ?? 0))))))),
    h('tfoot', null, h('tr', { class: 'ewprop-total' }, h('th', { scope: 'row' }, t('props.total')), ...d.series.map((s) => h('td', { class: 'ewprop-num' }, d.format(sum(s.values)))),
      h('td', { class: 'ewprop-num' }, d.format(sum(d.series.flatMap((s) => s.values))))))));
}

function kpiView(res: Result, p: Record<string, string>): HTMLElement {
  const d = chartData(res, p);
  const all = d.series[0]?.values ?? [];
  const total = p.agg === 'avg' ? all.reduce((a, b) => a + b, 0) / Math.max(1, all.length) : p.agg === 'max' ? Math.max(0, ...all) : p.agg === 'min' ? Math.min(...all) : all.reduce((a, b) => a + b, 0);
  return h('div', { class: 'ewprop-kpi' },
    h('p', { class: 'ewprop-kpi-big' }, h('span', { class: 'ewprop-kpi-value' }, d.format(total)), h('span', { class: 'ewprop-kpi-label' }, d.valueLabel)),
    p.group && p.group !== 'type' ? h('ul', { class: 'ewprop-kpis' }, ...d.categories.map((c, i) => h('li', null, h('span', { class: 'ewprop-kpi-value' }, d.format(all[i] ?? 0)), h('span', { class: 'ewprop-kpi-label' }, c)))) : '');
}

function tagcloudView(res: Result, p: Record<string, string>): HTMLElement {
  const groups = (res.groups ?? []).filter((g) => g.key !== null);
  const max = Math.max(1, ...groups.map((g) => g.count));
  const col = res.columns.find((c) => c.key === p.group);
  return h('ul', { class: 'ewprop-cloud' }, ...groups.map((g) => {
    const size = 0.85 + (g.count / max) * 1.4;
    const label = groupLabel(g.key, col, res.refs);
    const href = p.group === 'tags' ? `/search?tags=${encodeURIComponent(String(g.key))}` : undefined;
    return h('li', { style: `font-size:${size.toFixed(2)}rem` }, href ? h('a', { href }, label) : label, h('span', { class: 'sr-only' }, ` (${g.count})`));
  }));
}

// ---------------------------------------------------------------- the widget

export async function reportWidget(el: HTMLElement): Promise<void> {
  let p: Record<string, string> = {};
  try { p = JSON.parse(el.dataset.config ?? '{}') as Record<string, string>; } catch { return; }
  const categoryId = Number(el.dataset.category) || 0;
  const elementId = Number(el.dataset.element) || 0;
  const view = p.view || 'table';
  const grouped = ['chart', 'kpi', 'pivot', 'tagcloud', 'trend'].includes(view);
  let sort: SortKey[] = [];
  let offset = 0;
  const live: Filter[] = [];
  const out = h('div', { class: 'ewprop-out', 'aria-live': 'polite', 'aria-busy': 'true' });
  const tools = h('div', { class: 'ewprop-tools' });
  const pager = h('div', { class: 'ewprop-pager' });
  const fallback = el.querySelector('.ewprop-fallback');
  let last: Result | null = null;
  const load = async () => {
    out.setAttribute('aria-busy', 'true');
    if (view === 'trend') {
      // development of a property from its history (per week)
      try {
        const tr = await api.post<{ weeks: string[]; series: { value: string; values: number[] }[]; type: string }>(`${BASE}/trend`,
          { report: { params: p, category_id: categoryId, element_id: elementId, filters: live, lang: state.lang }, key: p.group, weeks: Number(p.weeks) || 12 });
        fallback?.remove();
        const name = (v: string) => (v === 'sum' ? t('props.trend.sum') : v === 'true' ? t('props.trend.yes') : v === 'false' ? t('props.trend.no') : v);
        out.replaceChildren(tr.series.length
          ? chart('line', { categories: tr.weeks.map((w) => new Date(`${w}T12:00:00`).toLocaleDateString(state.lang)), series: tr.series.map((x) => ({ name: name(x.value), values: x.values })),
            valueLabel: tr.type === 'select' || tr.type === 'status' || tr.type === 'boolean' ? t('props.trend.count') : t('props.trend.sum'),
            format: (n) => n.toLocaleString(state.lang) }, p.title || t('props.report'))
          : h('p', { class: 'ew-muted' }, t('props.no_match')));
      } catch (e) {
        out.replaceChildren(h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, errorText(e)));
      } finally {
        out.setAttribute('aria-busy', 'false');
      }
      return;
    }
    try {
      const res = await api.post<Result>(`${BASE}/report`, { params: p, category_id: categoryId, element_id: elementId, filters: live, sort, offset, lang: state.lang });
      last = res;
      fallback?.remove();
      draw(res);
    } catch (e) {
      out.replaceChildren(h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, errorText(e)));
    } finally {
      out.setAttribute('aria-busy', 'false');
    }
  };
  const draw = (res: Result) => {
    const cols = res.columns.filter((c) => c.key !== 'title');
    const empty = grouped ? !(res.groups?.length) : !(res.rows?.length);
    let node: HTMLElement;
    if (empty) node = h('p', { class: 'ew-muted' }, t('props.no_match'));
    else switch (view) {
      case 'list': node = cardsView(res, cols, true); break;
      case 'cards': node = cardsView(res, cols); break;
      case 'kanban': node = kanbanView(res, cols, p, () => void load()); break;
      case 'calendar': node = calendarView(res, p); break;
      case 'timeline': node = timelineView(res, p); break;
      case 'overdue': node = overdueView(res, cols, p); break;
      case 'completeness': node = completenessView(res, cols); break;
      case 'map': node = mapView(res, cols); break;
      case 'chart': node = chart(p.chart || 'bar', chartData(res, p), p.title || t('props.report')); break;
      case 'pivot': node = pivotView(res, p); break;
      case 'kpi': node = kpiView(res, p); break;
      case 'tagcloud': node = tagcloudView(res, p); break;
      default: node = tableView(res, cols, p, sort, (k) => {
        const cur = sort.find((x) => x.key === k);
        sort = [{ key: k, desc: cur ? !cur.desc : false }];
        offset = 0;
        void load();
      });
    }
    out.replaceChildren(node);
    // paging of rows
    pager.replaceChildren();
    const n = res.rows?.length ?? 0;
    if (!grouped && res.total > n) {
      const lim = Number(p.limit) || 100;
      pager.append(h('span', null, t('props.shown_of', { from: offset + 1, to: offset + n, total: res.total })), ' ',
        offset > 0 ? button(t('props.prev_page'), () => { offset = Math.max(0, offset - lim); void load(); }, { variant: 'subtle' }) : '',
        offset + n < res.total ? button(t('props.next_page'), () => { offset += lim; void load(); }, { variant: 'subtle' }) : '');
    }
  };
  // filters for readers
  const liveKeys = (p.live_filters ?? '').split(',').map((x) => x.trim()).filter(Boolean);
  const controls: HTMLElement[] = [];
  if (liveKeys.length) {
    const q = textInput({ type: 'search', placeholder: t('props.search_title') });
    q.setAttribute('aria-label', t('props.search_title'));
    let timer = 0;
    q.addEventListener('input', () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        const i = live.findIndex((f) => f.key === 'title');
        if (i >= 0) live.splice(i, 1);
        if (q.value.trim()) live.push({ key: 'title', op: 'contains', value: q.value.trim() });
        offset = 0;
        void load();
      }, 300);
    });
    controls.push(q);
    for (const k of liveKeys) {
      try {
        const vals = await api.get<Result>(`${BASE}/values?key=${encodeURIComponent(k)}&category=${categoryId}&lang=${state.lang}`);
        const col = vals.columns.find((c) => c.key === k);
        const opts = (vals.groups ?? []).filter((g) => g.key !== null).map((g) => ({ value: JSON.stringify(g.key), label: `${groupLabel(g.key, col, vals.refs)} (${g.count})` }));
        const sel = select([{ value: '', label: t('props.all_values', { name: col ? colLabel(col) : k }) }, ...opts]);
        sel.setAttribute('aria-label', t('props.filter_by', { name: col ? colLabel(col) : k }));
        sel.addEventListener('change', () => {
          const i = live.findIndex((f) => f.key === k);
          if (i >= 0) live.splice(i, 1);
          if (sel.value) {
            const v = JSON.parse(sel.value);
            const op = col && ['multiselect', 'user', 'group', 'element', 'category'].includes(col.type) ? 'has' : 'eq';
            live.push({ key: k, op, value: op === 'has' ? [v] : v });
          }
          offset = 0;
          void load();
        });
        controls.push(sel);
      } catch { /* property not visible */ }
    }
  }
  if (p.export !== 'false' && !grouped) {
    controls.push(downloadButtons(() => ({ params: { ...p, limit: '1000' }, category_id: categoryId, element_id: elementId, filters: live, sort, lang: state.lang }), () => p.title || 'report'));
  }
  // subscribe: the report by e-mail, daily or weekly
  if (state.user && elementId) {
    const same = (x: Record<string, string>) => JSON.stringify(Object.entries(x).sort()) === JSON.stringify(Object.entries(p).sort());
    const subBtn = button(t('props.sub.subscribe'), () => undefined, { variant: 'subtle' });
    let current: { id: number } | undefined;
    const sync = () => { subBtn.textContent = current ? t('props.sub.end') : t('props.sub.subscribe'); };
    void api.get<{ id: number; params: Record<string, string> }[]>(`${BASE}/subscriptions?element=${elementId}`)
      .then((list) => { current = list.find((x) => same(x.params ?? {})); sync(); }).catch(() => undefined);
    subBtn.addEventListener('click', () => {
      if (current) {
        void api.del(`${BASE}/subscriptions/${current.id}`).then(() => { current = undefined; sync(); toast(t('props.sub.ended')); }).catch((e: unknown) => toast(errorText(e), 'error'));
        return;
      }
      const freq = select([{ value: 'weekly', label: t('props.sub.weekly') }, { value: 'daily', label: t('props.sub.daily') }], 'weekly');
      formDialog(t('props.sub.subscribe'), [h('p', null, t('props.sub.help')), field(t('props.sub.frequency'), freq)], t('props.sub.subscribe'), async () => {
        current = await api.post<{ id: number }>(`${BASE}/subscriptions`, { element_id: elementId, category_id: categoryId, params: p, title: p.title || document.title.split(' – ')[0], frequency: freq.value });
        sync();
        toast(t('props.sub.done'));
      });
    });
    controls.push(subBtn);
  }
  if (controls.length) tools.append(...controls);
  el.append(tools, out, pager);
  void last;
  await load();
}
