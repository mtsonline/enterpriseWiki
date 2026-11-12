// Administration → Expert settings: the technical limits of the core and of
// all plugins in one place (sizes of requests, parallel work, lengths in the
// audit log …). Each has a default and an allowed range.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, errorText, field, pageHeader, setFieldError, textInput } from '../components/ui';

interface Limit {
  key: string; owner: string; group: string; label: string; help: string; unit: string;
  default: number; min: number; max: number; restart?: boolean; value: number; changed: boolean; active: boolean;
}

/** translated text of a limit, or the English text the core or plugin declared */
const tr = (key: string, fallback: string) => { const s = t(key); return s === key ? fallback : s; };
const withUnit = (n: number, unit: string) => t(`limits.unit.${unit}`, { n: n.toLocaleString() });

export async function limitsAdmin(panel: HTMLElement): Promise<void> {
  const res = await api.get<{ limits: Limit[]; editable: boolean }>('admin/limits');
  const inputs = new Map<string, HTMLInputElement>();
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const groups = new Map<string, Limit[]>();
  for (const l of res.limits) groups.set(l.group, [...(groups.get(l.group) ?? []), l]);

  const row = (l: Limit): HTMLElement => {
    const input = textInput({ value: String(l.value), type: 'number', name: l.key });
    input.min = String(l.min);
    input.max = String(l.max);
    input.step = '1';
    input.inputMode = 'numeric';
    input.disabled = !res.editable;
    inputs.set(l.key, input);
    const facts = [
      tr(`limit.${l.key}.help`, l.help),
      t('limits.range', { unit: t(`limits.unit_name.${l.unit}`), default: withUnit(l.default, l.unit), min: l.min.toLocaleString(), max: l.max.toLocaleString() }),
      l.owner === 'core' ? '' : t('limits.owner', { plugin: l.owner }) + (l.active ? '' : ` ${t('limits.inactive')}`),
      l.restart ? t('limits.restart') : '',
    ].filter(Boolean).join(' ');
    const reset = res.editable ? button(t('limits.reset'), () => { input.value = String(l.default); input.focus(); },
      { variant: 'subtle', ariaLabel: t('limits.reset_of', { name: tr(`limit.${l.key}`, l.label) }) }) : null;
    return h('div', { class: 'ew-limit' }, field(tr(`limit.${l.key}`, l.label), input, { help: facts }), reset);
  };

  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    ...[...groups].map(([g, list]) => h('section', { class: 'ew-section', 'aria-labelledby': `lim-${g}` },
      h('h2', { id: `lim-${g}` }, tr(`limits.group.${g}`, g)), ...list.map(row))),
    res.editable ? h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })) : null);

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const values: Record<string, number | null> = {};
    let firstBad: HTMLInputElement | null = null;
    for (const l of res.limits) {
      const input = inputs.get(l.key)!;
      setFieldError(input, '');
      const v = Number(input.value);
      if (input.value.trim() === '' || !Number.isInteger(v) || v < l.min || v > l.max) {
        setFieldError(input, t('limits.out_of_range', { min: l.min.toLocaleString(), max: l.max.toLocaleString() }));
        firstBad ??= input;
      } else if (v !== l.value) values[l.key] = v === l.default ? null : v;
    }
    if (firstBad) { firstBad.focus(); return; }
    if (!Object.keys(values).length) { toast(t('limits.unchanged')); return; }
    try {
      await api.put('admin/limits', { values });
      toast(t('limits.saved'));
      await limitsAdmin(panel);
    } catch (e) { alert.textContent = errorText(e); }
  });

  mount(panel, pageHeader(t('admin.limits')),
    h('p', null, t('limits.intro')),
    res.editable ? null : h('p', { role: 'note' }, t('limits.instance_only')),
    form);
}
