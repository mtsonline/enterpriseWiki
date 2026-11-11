// Accessible SVG charts without dependencies: every chart has a title and a
// text summary for screen readers, direct value labels, a legend with the
// values, and the data as a table (in a disclosure), so colour is never the
// only carrier of meaning.
import { h, state, t, uid } from '@ewiki/sdk';

export interface Series { name: string; values: number[] }
export interface ChartData { categories: string[]; series: Series[]; valueLabel: string; format: (n: number) => string }

// categorical colours with ≥ 3:1 contrast to white and to each other's neighbours
const COLORS = ['#1f5fbf', '#c2410c', '#15803d', '#7e22ce', '#b91c1c', '#0f766e', '#a16207', '#4338ca', '#be185d', '#475569'];
const PATTERNS = ['', 'url(#ewp-p1)', 'url(#ewp-p2)', 'url(#ewp-p3)'];

const SVG = 'http://www.w3.org/2000/svg';
function s<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}, ...children: (Node | string)[]): SVGElementTagNameMap[K] {
  const el = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  for (const c of children) el.append(typeof c === 'string' ? document.createTextNode(c) : c);
  return el;
}

function defs(): SVGDefsElement {
  // hatch patterns distinguish series also without colour
  const pat = (id: string, d: string) => s('pattern', { id, width: 8, height: 8, patternUnits: 'userSpaceOnUse' }, s('rect', { width: 8, height: 8, fill: 'currentColor', 'fill-opacity': '0.15' }), s('path', { d, stroke: '#fff', 'stroke-width': 2 }));
  return s('defs', {}, pat('ewp-p1', 'M0 8L8 0'), pat('ewp-p2', 'M0 0L8 8'), pat('ewp-p3', 'M4 0V8'));
}

const short = (x: string, n = 18) => (x.length > n ? `${x.slice(0, n - 1)}…` : x);

function dataTable(d: ChartData, caption: string): HTMLElement {
  return h('details', { class: 'ewprop-chart-data' }, h('summary', null, t('props.chart_data')),
    h('table', { class: 'ew-table' }, h('caption', null, caption),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, ''), ...d.series.map((x) => h('th', { scope: 'col' }, x.name)))),
      h('tbody', null, ...d.categories.map((c, i) => h('tr', null, h('th', { scope: 'row' }, c), ...d.series.map((x) => h('td', null, d.format(x.values[i] ?? 0))))))));
}

function summary(d: ChartData): string {
  if (!d.categories.length) return t('props.chart_empty');
  const total = d.series.reduce((a, x) => a + x.values.reduce((p, v) => p + v, 0), 0);
  let best = 0, bestI = 0, bestS = 0;
  d.series.forEach((x, si) => x.values.forEach((v, i) => { if (v > best) { best = v; bestI = i; bestS = si; } }));
  return t('props.chart_summary', { n: d.categories.length, total: d.format(total), max: d.categories[bestI] + (d.series.length > 1 ? ` (${d.series[bestS].name})` : ''), value: d.format(best) });
}

function legend(d: ChartData): HTMLElement | null {
  if (d.series.length < 2) return null;
  return h('ul', { class: 'ewprop-legend' }, ...d.series.map((x, i) => h('li', null, h('span', { class: 'ewprop-legend-swatch', style: `background:${COLORS[i % COLORS.length]}`, 'aria-hidden': 'true' }), x.name)));
}

/** Draws a chart; kind: bar, hbar, stacked, line, area, pie, donut, heatmap. */
export function chart(kind: string, d: ChartData, title: string): HTMLElement {
  const titleId = uid('ewp-t'), descId = uid('ewp-d');
  const W = 720, H = 360, L = 56, B = 64, T = 16, R = 16;
  const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-labelledby': `${titleId} ${descId}`, class: 'ewprop-chart-svg', focusable: 'false' },
    s('title', { id: titleId }, title), s('desc', { id: descId }, summary(d)), defs());
  const nCat = d.categories.length;
  const stacked = kind === 'stacked' || kind === 'area';
  const totals = d.categories.map((_, i) => d.series.reduce((a, x) => a + Math.max(0, x.values[i] ?? 0), 0));
  const maxV = Math.max(1, ...(stacked ? totals : d.series.flatMap((x) => x.values)));
  const nice = (() => { const p = Math.pow(10, Math.floor(Math.log10(maxV))); const m = maxV / p; return (m <= 1 ? 1 : m <= 2 ? 2 : m <= 5 ? 5 : 10) * p; })();
  const axisY = (y0: number, y1: number) => {
    for (let k = 0; k <= 4; k++) {
      const v = (nice * k) / 4, y = y1 - ((y1 - y0) * k) / 4;
      svg.append(s('line', { x1: L, x2: W - R, y1: y, y2: y, class: 'ewprop-grid' }), s('text', { x: L - 6, y: y + 4, 'text-anchor': 'end', class: 'ewprop-axis' }, d.format(v)));
    }
  };
  const catLabel = (i: number, x: number, y: number) => {
    const tx = s('text', { x, y, 'text-anchor': nCat > 8 ? 'end' : 'middle', class: 'ewprop-axis', transform: nCat > 8 ? `rotate(-35 ${x} ${y})` : '' }, short(d.categories[i], nCat > 8 ? 14 : 18));
    tx.append(s('title', {}, d.categories[i]));
    svg.append(tx);
  };
  if (kind === 'pie' || kind === 'donut') {
    const vals = d.series[0]?.values ?? [];
    const sum = vals.reduce((a, v) => a + Math.max(0, v), 0) || 1;
    const cx = 200, cy = H / 2, r = 140, ir = kind === 'donut' ? 80 : 0;
    let a0 = -Math.PI / 2;
    vals.forEach((v, i) => {
      const a1 = a0 + (Math.max(0, v) / sum) * Math.PI * 2;
      const large = a1 - a0 > Math.PI ? 1 : 0;
      const p = (a: number, rr: number) => `${cx + rr * Math.cos(a)} ${cy + rr * Math.sin(a)}`;
      const path = vals.length === 1 ? `M${cx - r} ${cy}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0` + (ir ? `M${cx - ir} ${cy}a${ir} ${ir} 0 1 1 ${2 * ir} 0a${ir} ${ir} 0 1 1 ${-2 * ir} 0` : '')
        : `M${p(a0, r)}A${r} ${r} 0 ${large} 1 ${p(a1, r)}` + (ir ? `L${p(a1, ir)}A${ir} ${ir} 0 ${large} 0 ${p(a0, ir)}Z` : `L${cx} ${cy}Z`);
      const el = s('path', { d: path, fill: COLORS[i % COLORS.length], stroke: '#fff', 'stroke-width': 2, 'fill-rule': 'evenodd' });
      el.append(s('title', {}, `${d.categories[i]}: ${d.format(v)} (${Math.round((v / sum) * 100)} %)`));
      svg.append(el);
      a0 = a1;
    });
    // legend with values next to the pie
    vals.slice(0, 12).forEach((v, i) => {
      const y = 40 + i * 24;
      svg.append(s('rect', { x: 380, y: y - 11, width: 14, height: 14, fill: COLORS[i % COLORS.length] }),
        s('text', { x: 402, y, class: 'ewprop-axis' }, `${short(d.categories[i], 28)}: ${d.format(v)} (${Math.round((v / sum) * 100)} %)`));
    });
  } else if (kind === 'hbar') {
    const rowH = Math.min(36, (H - T - 20) / Math.max(1, nCat));
    const LL = 170;
    const vals = d.series[0]?.values ?? [];
    vals.forEach((v, i) => {
      const y = T + i * rowH, w = ((W - LL - R - 60) * Math.max(0, v)) / nice;
      const tx = s('text', { x: LL - 8, y: y + rowH / 2 + 4, 'text-anchor': 'end', class: 'ewprop-axis' }, short(d.categories[i], 24));
      tx.append(s('title', {}, d.categories[i]));
      svg.append(tx, s('rect', { x: LL, y: y + 4, width: Math.max(1, w), height: rowH - 8, fill: COLORS[0], rx: 2 }),
        s('text', { x: LL + w + 6, y: y + rowH / 2 + 4, class: 'ewprop-value' }, d.format(v)));
    });
  } else if (kind === 'heatmap') {
    // categories × series, colour intensity with the value written in each cell
    const LL = 140, cw = (W - LL - R) / Math.max(1, d.series.length), ch = (H - T - 40) / Math.max(1, nCat);
    const all = d.series.flatMap((x) => x.values);
    const mx = Math.max(1, ...all);
    d.series.forEach((x, j) => svg.append(s('text', { x: LL + j * cw + cw / 2, y: H - 18, 'text-anchor': 'middle', class: 'ewprop-axis' }, short(x.name, 14))));
    d.categories.forEach((c, i) => {
      svg.append(s('text', { x: LL - 8, y: T + i * ch + ch / 2 + 4, 'text-anchor': 'end', class: 'ewprop-axis' }, short(c, 20)));
      d.series.forEach((x, j) => {
        const v = x.values[i] ?? 0, k = v / mx;
        const cell = s('rect', { x: LL + j * cw + 1, y: T + i * ch + 1, width: cw - 2, height: ch - 2, fill: COLORS[0], 'fill-opacity': (0.08 + 0.92 * k).toFixed(2) });
        cell.append(s('title', {}, `${c} / ${x.name}: ${d.format(v)}`));
        svg.append(cell, s('text', { x: LL + j * cw + cw / 2, y: T + i * ch + ch / 2 + 4, 'text-anchor': 'middle', class: k > 0.55 ? 'ewprop-value ewprop-value--light' : 'ewprop-value' }, d.format(v)));
      });
    });
  } else {
    const y0 = T, y1 = H - B, plotW = W - L - R;
    axisY(y0, y1);
    const slot = plotW / Math.max(1, nCat);
    const ys = (v: number) => y1 - ((y1 - y0) * v) / nice;
    if (kind === 'line' || kind === 'area') {
      const base = new Array(nCat).fill(0);
      d.series.forEach((x, si) => {
        const pts = x.values.map((v, i) => { const top = stacked ? base[i] + Math.max(0, v) : v; return [L + slot * i + slot / 2, ys(top)] as const; });
        if (kind === 'area') {
          const bottom = x.values.map((_, i) => [L + slot * i + slot / 2, ys(base[i])] as const).reverse();
          svg.append(s('path', { d: `M${[...pts, ...bottom].map((p) => p.join(' ')).join('L')}Z`, fill: COLORS[si % COLORS.length], 'fill-opacity': 0.35 }));
          x.values.forEach((v, i) => { base[i] += Math.max(0, v); });
        }
        svg.append(s('polyline', { points: pts.map((p) => p.join(',')).join(' '), fill: 'none', stroke: COLORS[si % COLORS.length], 'stroke-width': 3, 'stroke-dasharray': ['', '8 4', '2 4', '12 4 2 4'][si % 4] }));
        pts.forEach((p, i) => {
          const c = s('circle', { cx: p[0], cy: p[1], r: 4, fill: COLORS[si % COLORS.length] });
          c.append(s('title', {}, `${d.categories[i]}${d.series.length > 1 ? ` – ${x.name}` : ''}: ${d.format(x.values[i])}`));
          svg.append(c);
        });
      });
    } else {
      const n = stacked ? 1 : d.series.length;
      const bw = Math.min(48, (slot * 0.75) / n);
      const base = new Array(nCat).fill(0);
      d.series.forEach((x, si) => {
        x.values.forEach((v, i) => {
          const xpos = L + slot * i + (slot - bw * n) / 2 + (stacked ? 0 : si * bw);
          const top = stacked ? base[i] + Math.max(0, v) : Math.max(0, v);
          const yTop = ys(top), yBot = stacked ? ys(base[i]) : y1;
          const r = s('rect', { x: xpos, y: yTop, width: bw - 2, height: Math.max(0, yBot - yTop), fill: COLORS[si % COLORS.length] });
          r.append(s('title', {}, `${d.categories[i]}${d.series.length > 1 ? ` – ${x.name}` : ''}: ${d.format(v)}`));
          svg.append(r);
          if (PATTERNS[si % PATTERNS.length] && d.series.length > 1) svg.append(s('rect', { x: xpos, y: yTop, width: bw - 2, height: Math.max(0, yBot - yTop), fill: PATTERNS[si % PATTERNS.length], 'pointer-events': 'none' }));
          if (!stacked && nCat * n <= 24 && v) svg.append(s('text', { x: xpos + (bw - 2) / 2, y: yTop - 4, 'text-anchor': 'middle', class: 'ewprop-value' }, d.format(v)));
          if (stacked) base[i] += Math.max(0, v);
        });
      });
      if (stacked && nCat <= 24) totals.forEach((v, i) => svg.append(s('text', { x: L + slot * i + slot / 2, y: ys(v) - 4, 'text-anchor': 'middle', class: 'ewprop-value' }, d.format(v))));
    }
    d.categories.forEach((_, i) => catLabel(i, L + slot * i + slot / 2, y1 + 18));
    svg.append(s('line', { x1: L, x2: W - R, y1, y2: y1, class: 'ewprop-axis-line' }));
  }
  return h('figure', { class: 'ewprop-chart', lang: state.lang }, svg, legend(d), h('figcaption', { class: 'sr-only' }, summary(d)), dataTable(d, title));
}
