// Types of the properties API and the display of values.
import { api, fmtDate, h, state, t } from '@ewiki/sdk';

export const BASE = 'x/core.properties';

export interface Choice { value: string; labels?: Record<string, string>; color?: string }
export interface Options {
  choices?: Choice[]; min?: number; max?: number; decimals?: number; unit?: string; currency?: string;
  max_len?: number; pattern?: string; multiple?: boolean; prefix?: string; digits?: number; formula?: string;
}
export interface TypeDef { id: string; group: string; storage: string; options?: string[]; computed?: boolean }
export interface Subject { type: 'user' | 'group' | 'role' | 'authenticated' | 'guest'; id?: number }
export interface Def {
  id: number; category_id: number | null; key: string; labels: Record<string, string>; help: Record<string, string>; type: string; options: Options;
  required: boolean; default: unknown; element_types: string[]; group: string; position: number; show_on_page: boolean;
  view_subjects: Subject[]; edit_subjects: Subject[]; own?: boolean; inherited_from?: string;
}
export interface Shown {
  key: string; label: string; type: string; def_id: number | null; value: unknown; options?: Options; group?: string; help?: string;
  required?: boolean; show_on_page: boolean; can_edit: boolean;
}
export interface Ref { name: string; link?: string }
export interface Props { items: Shown[]; refs: Record<string, Ref>; can_edit: boolean; can_add_free: boolean; category_id: number }

let types: Promise<{ types: TypeDef[]; palette: string[]; builtins: string[] }> | null = null;
export function loadTypes(): Promise<{ types: TypeDef[]; palette: string[]; builtins: string[] }> {
  types ??= api.get(`${BASE}/types`);
  return types;
}

export const typeLabel = (id: string): string => t(`props.type.${id}`);

export function choiceLabel(c: Choice | undefined, value: string): string {
  if (!c) return value;
  return c.labels?.[state.lang] || c.labels?.en || c.value;
}

const numFmt = (o: Options | undefined, style?: 'percent' | 'currency'): Intl.NumberFormat => {
  const d = o?.decimals;
  if (style === 'currency') return new Intl.NumberFormat(state.lang, { style: 'currency', currency: o?.currency || 'EUR' });
  return new Intl.NumberFormat(state.lang, d !== undefined ? { minimumFractionDigits: d, maximumFractionDigits: d } : { maximumFractionDigits: 6 });
};

/** A number as shown (unit, percent, currency). */
export function formatNumber(n: number, type: string, o?: Options): string {
  switch (type) {
    case 'currency': return numFmt(o, 'currency').format(n);
    case 'percent': return `${numFmt(o).format(n)} %`;
    case 'progress': return `${Math.round(n)} %`;
    case 'duration': return formatDuration(n);
    case 'rating': return t('props.rating_of', { n, max: o?.max ?? 5 });
  }
  const s = numFmt(o).format(n);
  return o?.unit ? `${s} ${o.unit}` : s;
}

export function formatDuration(sec: number): string {
  const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  const parts = [d ? `${d} d` : '', hh ? `${hh} h` : '', m ? `${m} min` : ''].filter(Boolean);
  return parts.join(' ') || `${sec} s`;
}

export function formatDate(s: string, withTime = false): string {
  if (!s) return '';
  const d = withTime ? new Date(s) : new Date(`${s}T12:00:00`);
  if (Number.isNaN(d.getTime())) return s;
  return withTime ? fmtDate(s, state.lang) : d.toLocaleDateString(state.lang, { year: 'numeric', month: '2-digit', day: '2-digit' });
}

const refList = (kind: string, v: unknown): number[] => (Array.isArray(v) ? v : typeof v === 'number' ? [v] : []).filter((x): x is number => typeof x === 'number');

/** A value as plain text (sorting, summaries, screen readers). */
export function plainValue(type: string, v: unknown, o: Options | undefined, refs: Record<string, Ref>): string {
  if (v === null || v === undefined || v === '') return '';
  switch (type) {
    case 'boolean': return v ? t('common.yes') : t('common.no');
    case 'date': return formatDate(String(v));
    case 'datetime': return formatDate(String(v), true);
    case 'daterange': { const r = v as { from: string; to: string }; return `${formatDate(r.from)} – ${formatDate(r.to)}`; }
    case 'select': case 'status': return choiceLabel(o?.choices?.find((c) => c.value === v), String(v));
    case 'multiselect': return (v as string[]).map((x) => choiceLabel(o?.choices?.find((c) => c.value === x), x)).join(', ');
    case 'user': case 'group': case 'element': case 'category':
      return refList(type, v).map((id) => refs[`${type}:${id}`]?.name ?? t('props.hidden_ref')).join(', ');
    case 'location': { const l = v as { address: string; lat?: number; lng?: number }; return l.address || (l.lat !== undefined ? `${l.lat}, ${l.lng}` : ''); }
    case 'country': try { return new Intl.DisplayNames([state.lang], { type: 'region' }).of(String(v)) ?? String(v); } catch { return String(v); }
    case 'language': try { return new Intl.DisplayNames([state.lang], { type: 'language' }).of(String(v)) ?? String(v); } catch { return String(v); }
  }
  if (typeof v === 'number') return formatNumber(v, type, o);
  return String(v);
}

/** A value as shown on pages and in reports. */
export function formatValue(type: string, v: unknown, o: Options | undefined, refs: Record<string, Ref>): Node {
  if (v === null || v === undefined || v === '' || (Array.isArray(v) && !v.length)) return h('span', { class: 'ewprop-empty' }, '—');
  switch (type) {
    case 'status': {
      const c = o?.choices?.find((x) => x.value === v);
      return h('span', { class: `ew-status ew-status--${c?.color || 'grey'}` }, choiceLabel(c, String(v)));
    }
    case 'multiselect':
      return h('span', { class: 'ewprop-tags' }, ...(v as string[]).map((x) => h('span', { class: 'ew-badge' }, choiceLabel(o?.choices?.find((c) => c.value === x), x))));
    case 'url': {
      const s = String(v);
      return h('a', { href: s, rel: 'noopener noreferrer', target: s.startsWith('/') ? undefined : '_blank' }, s.replace(/^https?:\/\//, ''));
    }
    case 'email': return h('a', { href: `mailto:${String(v)}` }, String(v));
    case 'phone': return h('a', { href: `tel:${String(v).replace(/[^+0-9]/g, '')}` }, String(v));
    case 'color': return h('span', { class: 'ewprop-color' }, h('span', { class: 'ewprop-swatch', style: `background:${/^#[0-9a-f]{6}$/i.test(String(v)) ? String(v) : 'transparent'}`, 'aria-hidden': 'true' }), String(v));
    case 'date': case 'datetime': return h('time', { datetime: String(v) }, plainValue(type, v, o, {}));
    case 'progress': {
      const n = Number(v);
      return h('span', { class: 'ewprop-progress' }, h('progress', { max: '100', value: String(n), 'aria-label': `${n} %` }), ` ${n} %`);
    }
    case 'rating': {
      const n = Number(v), max = o?.max ?? 5;
      return h('span', { class: 'ewprop-rating', role: 'img', 'aria-label': t('props.rating_of', { n, max }) }, '★'.repeat(n) + '☆'.repeat(Math.max(0, max - n)));
    }
    case 'user': case 'group': case 'element': case 'category': {
      const ids = refList(type, v);
      return h('span', { class: 'ewprop-refs' }, ...ids.flatMap((id, i) => {
        const r = refs[`${type}:${id}`];
        const n: Node = r?.link ? h('a', { href: r.link }, r.name) : h('span', null, r?.name ?? t('props.hidden_ref'));
        return i ? [', ', n] : [n];
      }));
    }
    case 'media': {
      const s = String(v);
      return h('a', { href: s }, s.split('/').pop() ?? s);
    }
    case 'location': {
      const l = v as { address: string; lat?: number; lng?: number };
      const text = plainValue(type, v, o, refs);
      if (l.lat === undefined) return h('span', null, text);
      return h('a', { href: `https://www.openstreetmap.org/?mlat=${l.lat}&mlon=${l.lng}#map=16/${l.lat}/${l.lng}`, rel: 'noopener noreferrer', target: '_blank' }, text);
    }
    case 'textarea': return h('span', { class: 'ewprop-multiline' }, String(v));
  }
  return document.createTextNode(plainValue(type, v, o, refs));
}

/** Label of a built-in key. */
export function builtinLabel(key: string): string {
  return t(`props.builtin.${key}`);
}
