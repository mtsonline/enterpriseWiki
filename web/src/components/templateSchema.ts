// The schematic of a template (Administration → Templates): its rows and
// areas as boxes, each position with its name at its place, for one device.
// Positions closed behind a button there (menu, tools) are drawn inside a
// dashed box with the name of that button; hidden ones are marked. The
// order of the positions is the reading order of the page, so a screen
// reader reads the schematic in the same order as the page.
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { DEVICES, MAIN, deviceSize, type Device, type SchemaArea, type SchemaRow } from '../core/templateSchema';
import { positionLabel, resolveTemplate, templateLabel } from '../coreplugin';

/** The device shown last (kept while the page is open). */
let lastDevice: Device = 'desktop';

/** The device the schematics show, until it is switched. */
export function currentDevice(): Device { return lastDevice; }

/** A group of buttons to switch the device (one is pressed). */
export function deviceSwitch(onChange: (d: Device) => void): HTMLElement {
  const buttons = DEVICES.map((d) => {
    const b = h('button', { type: 'button', class: 'ew-btn ew-btn--secondary', 'aria-pressed': String(d.id === lastDevice), 'data-device': d.id },
      t(`sitetpl.device_${d.id}`)) as HTMLButtonElement;
    b.addEventListener('click', () => {
      lastDevice = d.id;
      for (const x of buttons) x.setAttribute('aria-pressed', String(x === b));
      onChange(d.id);
    });
    return b;
  });
  const id = uid('dev');
  return h('div', { class: 'ew-sch-devices' }, h('span', { id, class: 'ew-sch-devices-label' }, t('sitetpl.device')),
    h('div', { role: 'group', 'aria-labelledby': id, class: 'ew-actions' }, ...buttons));
}

/** How a position is shown in the schematic. */
export type SchemaState = 'shown' | 'behind' | 'hidden';

export interface SchematicOptions {
  /** the name of the template (default: its label) */
  name?: string;
  /** the rows of the page drawn in place of the content (a category page,
   * a page, the editor or the dashboard) */
  main?: SchemaRow[];
  /** what a position offers (a button in the schematic of the placements) */
  position?: (name: string, state: SchemaState) => HTMLElement;
}

/** The schematic of a template on a device; without a description of its
 * rows, the list of its positions. */
export function schematic(templateId: string, device: Device, opts: SchematicOptions = {}): HTMLElement {
  const tpl = resolveTemplate(templateId, templateId);
  const name = opts.name ?? templateLabel(templateId);
  const label = t('sitetpl.schema_label', { name, device: t(`sitetpl.device_${device}`) });
  if (!tpl?.schema) {
    return h('p', { class: 'ew-help ew-sch-none' }, t('sitetpl.schema_none', { list: (tpl?.positions ?? []).map(positionLabel).join(', ') || '–' }));
  }
  const rows = tpl.schema(deviceSize(device));
  return h('div', { class: ['ew-sch', `ew-sch--${device}`], role: 'group', 'aria-label': label, 'data-template': templateId, 'data-device': device },
    ...rows.map((r) => rowEl(r, opts)));
}

function rowEl(r: SchemaRow, opts: SchematicOptions): HTMLElement {
  const areas = r.areas.filter((a) => a.kind !== 'area' || a.positions.length);
  const el = h('div', { class: ['ew-sch-row', `ew-sch-row--${r.role}`] }, ...areas.map((a) => areaEl(a, opts)));
  if (r.columns) el.style.setProperty('--ew-sch-cols', String(r.columns));
  el.classList.toggle('ew-sch-row--grid', !!r.columns);
  return el;
}

function areaEl(a: SchemaArea, opts: SchematicOptions): HTMLElement {
  const state: SchemaState = a.hidden ? 'hidden' : a.behind ? 'behind' : 'shown';
  const box = h('div', { class: ['ew-sch-area', `ew-sch-area--${a.kind}`, `ew-sch-area--${state}`] });
  box.style.setProperty('--ew-sch-grow', String(a.grow ?? 1));
  if (a.kind === 'brand') box.append(h('span', { class: 'ew-sch-brand' }, t('sitetpl.schema_brand')));
  if (a.behind) box.append(h('span', { class: 'ew-sch-behind' }, h('span', { 'aria-hidden': 'true' }, '☰ '), t('sitetpl.schema_behind', { label: a.behind })));
  if (a.hidden) box.append(h('span', { class: 'ew-sch-behind' }, t('sitetpl.schema_hidden')));
  for (const p of a.positions) {
    if (p === MAIN) {
      box.append(opts.main?.length ? h('div', { class: 'ew-sch-main ew-sch-page' }, ...opts.main.map((r) => rowEl(r, { ...opts, main: undefined })))
        : h('span', { class: 'ew-sch-main' }, t('sitetpl.schema_main')));
      continue;
    }
    box.append(opts.position ? opts.position(p, state) : h('span', { class: 'ew-sch-pos', 'data-schema-position': p, 'data-state': state }, positionLabel(p)));
  }
  return box;
}
