// Generic settings form for plugins: values live in config.yaml under
// plugin_settings.<plugin-id> and are saved through PUT /api/v1/admin/settings.
import { api, uploadFile } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, checkbox, errorText, field, select, textArea, textInput } from './ui';

export interface SettingField {
  key: string;
  label: string;
  type?: 'text' | 'secret' | 'bool' | 'list' | 'map' | 'select' | 'textarea' | 'number' | 'image' | 'color';
  /** Default for color fields (string) and bool fields (boolean). */
  default?: string | boolean;
  help?: string;
  placeholder?: string;
  options?: { value: string; label: string }[];
  /** Field is only shown when this predicate (on current values) is true. */
  when?: (values: Record<string, unknown>) => boolean;
}

export const MAPPING_FIELDS = (): SettingField[] => [
  { key: 'group_mapping', label: t('mapping.group_mapping'), type: 'map', help: t('mapping.group_mapping_help') },
  { key: 'role_mapping', label: t('mapping.role_mapping'), type: 'map', help: t('mapping.role_mapping_help') },
  { key: 'default_groups', label: t('mapping.default_groups'), type: 'list', help: t('mapping.list_help') },
  { key: 'default_roles', label: t('mapping.default_roles'), type: 'list', help: t('mapping.list_help') },
  { key: 'keep_unmapped_groups', label: t('mapping.keep_unmapped'), type: 'bool', help: t('mapping.keep_unmapped_help') },
];

type Getter = () => unknown;

function mapToText(v: unknown): string {
  if (!v || typeof v !== 'object') return '';
  return Object.entries(v as Record<string, unknown>).map(([k, x]) => `${k} = ${x}`).join('\n');
}

function textToMap(s: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of s.split('\n')) {
    const i = line.lastIndexOf('=');
    if (i <= 0) continue;
    const k = line.slice(0, i).trim();
    if (k) out[k] = line.slice(i + 1).trim();
  }
  return out;
}

/**
 * Renders a settings form for a plugin. Unknown keys already present in the
 * configuration are preserved.
 */
export async function pluginSettingsForm(pluginId: string, fields: SettingField[], legend?: string): Promise<{ el: HTMLElement; values: () => Record<string, unknown> }> {
  const res = await api.get<{ config: { plugin_settings?: Record<string, Record<string, unknown>> }; path: string }>('admin/settings');
  const stored = res.config.plugin_settings?.[pluginId] ?? {};
  const getters: Record<string, Getter> = {};
  const rows: { f: SettingField; el: HTMLElement }[] = [];
  for (const f of fields) {
    const v = stored[f.key];
    let el: HTMLElement;
    switch (f.type ?? 'text') {
      case 'bool': {
        const cb = checkbox(f.label, v === undefined ? f.default === true || f.key === 'keep_unmapped_groups' : v === true, { help: f.help });
        getters[f.key] = () => cb.input.checked;
        el = cb.el;
        break;
      }
      case 'map': {
        const ta = textArea({ value: mapToText(v), rows: 4 });
        ta.placeholder = 'cn=hr,ou=groups,dc=example,dc=com = HR';
        getters[f.key] = () => textToMap(ta.value);
        el = field(f.label, ta, { help: f.help });
        break;
      }
      case 'list': {
        const inp = textInput({ value: Array.isArray(v) ? v.join(', ') : String(v ?? ''), placeholder: f.placeholder });
        getters[f.key] = () => inp.value.split(',').map((x) => x.trim()).filter(Boolean);
        el = field(f.label, inp, { help: f.help });
        break;
      }
      case 'select': {
        const sel = select(f.options ?? [], String(v ?? f.options?.[0]?.value ?? ''));
        getters[f.key] = () => sel.value;
        el = field(f.label, sel, { help: f.help });
        break;
      }
      case 'textarea': {
        const ta = textArea({ value: String(v ?? ''), rows: 6 });
        getters[f.key] = () => ta.value.trim();
        el = field(f.label, ta, { help: f.help });
        break;
      }
      case 'color': {
        const inp = h('input', { type: 'color', class: 'ew-input ew-color-input', value: String(v ?? (typeof f.default === 'string' ? f.default : '#0a3760')) }) as HTMLInputElement;
        getters[f.key] = () => inp.value;
        el = field(f.label, inp, { help: f.help });
        break;
      }
      case 'image': {
        // an uploaded image, stored as its SHA-256 (served at /api/v1/files/{hash})
        let hash = typeof v === 'string' ? v : '';
        const preview = h('div', { class: 'ew-image-preview' });
        const draw = () => preview.replaceChildren(hash ? h('img', { src: `/api/v1/files/${hash}`, alt: '' }) : h('span', { class: 'ew-help' }, t('settings.no_image')));
        draw();
        const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/gif,image/webp', class: 'ew-input' }) as HTMLInputElement;
        file.addEventListener('change', async () => {
          const fl = file.files?.[0];
          if (!fl) return;
          try { hash = (await uploadFile(fl)).hash; draw(); toast(t('settings.image_uploaded')); } catch (e) { toast(errorText(e), 'error'); }
        });
        getters[f.key] = () => hash;
        el = h('div', null, field(f.label, file, { help: f.help }), preview,
          button(t('settings.remove_image'), () => { hash = ''; file.value = ''; draw(); }, { variant: 'link', ariaLabel: `${t('settings.remove_image')}: ${f.label}` }));
        break;
      }
      case 'number': {
        const inp = textInput({ type: 'number', value: v === undefined ? '' : String(v) });
        getters[f.key] = () => (inp.value === '' ? undefined : Number(inp.value));
        el = field(f.label, inp, { help: f.help });
        break;
      }
      default: {
        const inp = textInput({ value: String(v ?? ''), placeholder: f.placeholder, type: f.type === 'secret' ? 'password' : 'text', autocomplete: 'off' });
        getters[f.key] = () => inp.value.trim();
        el = field(f.label, inp, { help: [f.help, f.type === 'secret' ? t('settings.secret_help') : ''].filter(Boolean).join(' ') });
      }
    }
    rows.push({ f, el });
  }
  const values = (): Record<string, unknown> => {
    const out: Record<string, unknown> = { ...stored };
    for (const [k, g] of Object.entries(getters)) {
      const x = g();
      if (x === undefined || x === '') delete out[k];
      else out[k] = x;
    }
    return out;
  };
  const refresh = () => {
    const cur = values();
    for (const r of rows) r.el.hidden = r.f.when ? !r.f.when(cur) : false;
  };
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    h('fieldset', null, h('legend', null, legend ?? t('settings.plugin_settings')), ...rows.map((r) => r.el)),
    h('p', { class: 'ew-help' }, t('settings.file_note', { path: res.path })),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('change', refresh);
  refresh();
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put('admin/settings', { plugin_settings: { [pluginId]: values() } });
      toast(t('settings.saved'));
    } catch (e) {
      alert.textContent = errorText(e);
    }
  });
  return { el: form, values };
}
