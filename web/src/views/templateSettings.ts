// Administration → Templates → Settings: the settings a template declared
// (colours, fonts, logos, texts …) as a form, grouped, each with its
// default and a way back to it, and a live preview of the website with the
// draft. Nothing is stored before "Save"; the server checks every value
// against the description of the template again.
import { api, uploadFile } from '../core/api';
import { h, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { toast } from '../core/announce';
import { contrast } from '../core/blockoptions';
import { state } from '../core/state';
import { DEVICES, type Device } from '../core/templateSchema';
import { imageSetting, listTemplates, templateLabel, type TemplateTabContext } from '../coreplugin';
import { button, checkbox, confirmDialog, errorText, field, select, textInput } from '../components/ui';
import { currentDevice, deviceSwitch } from '../components/templateSchema';

/** One setting as the server describes it (internal/tplsettings). */
interface Setting {
  key: string; kind: 'color' | 'font' | 'image' | 'number' | 'select' | 'bool' | 'text' | 'link'; group: string; role?: string;
  default?: unknown; min?: number; max?: number; step?: number; max_len?: number; options?: string[];
  label: Record<string, string>; help?: Record<string, string>; option_labels?: Record<string, Record<string, string>>; contrast_on?: string[];
}
interface SettingsState { settings: Setting[]; values: Record<string, unknown>; changed: string[] }

const GROUPS = ['colors', 'fonts', 'logos', 'header', 'content', 'footer', 'layout'];
/** Fonts every browser has (own fonts of the template designer come on top). */
const BASIC_FONTS = ['system', 'sans', 'serif', 'mono'];

const label = (s: Setting) => pick(s.label, state.lang, 'en');
const isColor = (v: unknown): v is string => typeof v === 'string' && /^#[0-9a-f]{6}$/i.test(v);

/** The link rule of the server (tplsettings.LinkOK): empty, /path or https://. */
export function linkOK(x: string): boolean {
  if (x === '') return true;
  if (x.length > 500 || /[\s"'<>\\`]/.test(x)) return false;
  if (x.startsWith('/')) return !x.startsWith('//');
  return /^https:\/\/[^/]/.test(x);
}

/** The tab: the template chosen (?t=…), its form and the preview. */
export async function settingsTab(panel: HTMLElement, ctx: TemplateTabContext): Promise<void> {
  const ids = listTemplates().filter((x) => (x.layout || x.extends) && x.id !== 'core.template/preview').map((x) => x.id);
  const id = ids.includes(ctx.params.get('t') ?? '') ? ctx.params.get('t')! : state.site.template;
  const sel = select(ids.map((x) => ({ value: x, label: templateLabel(x) })), id);
  sel.addEventListener('change', () => ctx.open('settings', { t: sel.value }));
  const head = [h('p', { class: 'ew-help' }, t('sitetpl.settings_help')), field(t('sitetpl.positions_template'), sel)];
  let cur: SettingsState;
  try {
    cur = await api.get<SettingsState>(`admin/templates/${encodeURIComponent(id)}/settings`);
  } catch {
    panel.replaceChildren(...head, h('p', null, t('sitetpl.settings_none')));
    return;
  }
  if (!cur.settings.length) {
    panel.replaceChildren(...head, h('p', null, t('sitetpl.settings_none')));
    return;
  }
  panel.replaceChildren(...head, await settingsForm(id, cur, () => ctx.open('settings', { t: id })));
}

/** The form of the settings of one template with its preview. */
async function settingsForm(id: string, cur: SettingsState, reload: () => void): Promise<HTMLElement> {
  const draft: Record<string, unknown> = { ...cur.values };
  // keys changed in this form: a value, or null for "back to the default"
  const edits = new Map<string, unknown>();
  const stored = new Set(cur.changed);
  const status = h('p', { class: 'sr-only', 'aria-live': 'polite' });
  const fonts = await ownFonts();
  const preview = previewFrame(id, draft);
  const changed = (key: string) => {
    preview.update(draft);
    warnings.forEach((w) => w());
    resets.get(key)?.();
  };
  const warnings: (() => void)[] = [];
  const resets = new Map<string, () => void>();
  const controls = new Map<string, () => void>();

  const fieldOf = (s: Setting): HTMLElement => {
    const isDefault = () => (edits.has(s.key) ? edits.get(s.key) === null : !stored.has(s.key));
    const set = (v: unknown) => { draft[s.key] = v; edits.set(s.key, v); changed(s.key); };
    let control: HTMLElement;
    let sync: () => void;
    const extra: HTMLElement[] = [];
    switch (s.kind) {
      case 'color': {
        const input = h('input', { type: 'color', class: 'ew-input ew-input--color', value: String(draft[s.key] ?? '#000000') }) as HTMLInputElement;
        const hex = h('output', { class: 'ew-muted' }, input.value);
        input.addEventListener('input', () => { hex.textContent = input.value; set(input.value); });
        sync = () => { input.value = String(draft[s.key] ?? '#000000'); hex.textContent = input.value; };
        control = input;
        extra.push(h('p', { class: 'ew-help' }, hex));
        if (s.contrast_on?.length) {
          const warn = h('p', { class: 'ew-help ew-warning', role: 'status' });
          const check = () => {
            const bad = (s.contrast_on ?? []).map((c) => (isColor(c) ? c : draft[c])).filter(isColor)
              .map((bg) => ({ bg, ratio: contrast(String(draft[s.key]), bg) })).filter((x) => x.ratio < 4.5);
            warn.textContent = bad.map((x) => t('sitetpl.contrast_low', { ratio: x.ratio.toFixed(1), color: x.bg })).join(' ');
            warn.hidden = !bad.length;
          };
          warnings.push(check);
          check();
          extra.push(warn);
        }
        break;
      }
      case 'font': {
        const names = [...BASIC_FONTS, ...fonts];
        const sl = select(names.map((f) => ({ value: f, label: BASIC_FONTS.includes(f) ? t(`sitetpl.font_${f}`) : f })), String(draft[s.key] ?? 'system'));
        sl.addEventListener('change', () => set(sl.value));
        sync = () => { sl.value = String(draft[s.key] ?? 'system'); };
        control = sl;
        break;
      }
      case 'image': {
        const shown = h('div', { class: 'ew-image-preview' });
        const draw = () => {
          const img = imageSetting(draft[s.key]);
          shown.replaceChildren(img ? h('img', { src: img.src, width: img.width, height: img.height, alt: '' }) : h('span', { class: 'ew-help' }, t('sitetpl.image_default')));
        };
        const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp', class: 'ew-input' }) as HTMLInputElement;
        file.addEventListener('change', () => void (async () => {
          const f = file.files?.[0];
          if (!f) return;
          try {
            const up = await uploadFile(f);
            const size = await imageSize(`/api/v1/files/${up.hash}`);
            draft[s.key] = { src: `/api/v1/files/${up.hash}`, ...size };
            edits.set(s.key, up.hash);
            draw();
            changed(s.key);
            status.textContent = t('settings.image_uploaded');
          } catch (e) { toast(errorText(e), 'error'); }
        })());
        sync = draw;
        draw();
        control = file;
        extra.push(shown);
        break;
      }
      case 'number': {
        const input = h('input', { type: 'number', class: 'ew-input ew-input--short', min: s.min, max: s.max, step: s.step || 'any', value: String(draft[s.key] ?? '') }) as HTMLInputElement;
        input.addEventListener('change', () => {
          const n = Number(input.value);
          if (input.value === '' || !Number.isFinite(n) || n < (s.min ?? -Infinity) || n > (s.max ?? Infinity)) {
            input.setAttribute('aria-invalid', 'true');
            return;
          }
          input.removeAttribute('aria-invalid');
          set(n);
        });
        sync = () => { input.value = String(draft[s.key] ?? ''); input.removeAttribute('aria-invalid'); };
        control = input;
        break;
      }
      case 'select': {
        const sl = select((s.options ?? []).map((o) => ({ value: o, label: s.option_labels?.[o] ? pick(s.option_labels[o], state.lang, 'en') : o })), String(draft[s.key] ?? ''));
        sl.addEventListener('change', () => set(sl.value));
        sync = () => { sl.value = String(draft[s.key] ?? ''); };
        control = sl;
        break;
      }
      case 'bool': {
        const cb = checkbox(label(s), draft[s.key] === true, { help: s.help ? pick(s.help, state.lang, 'en') : undefined });
        cb.input.addEventListener('change', () => set(cb.input.checked));
        sync = () => { cb.input.checked = draft[s.key] === true; };
        control = cb.el;
        break;
      }
      default: {
        const input = textInput({ value: String(draft[s.key] ?? ''), maxlength: s.kind === 'link' ? 500 : s.max_len || 200, placeholder: s.kind === 'link' ? '/…, https://…' : undefined });
        input.addEventListener('change', () => {
          const v = input.value.trim();
          if (s.kind === 'link' && !linkOK(v)) {
            input.setAttribute('aria-invalid', 'true');
            status.textContent = t('sitetpl.link_invalid');
            return;
          }
          input.removeAttribute('aria-invalid');
          set(v);
        });
        sync = () => { input.value = String(draft[s.key] ?? ''); input.removeAttribute('aria-invalid'); };
        control = input;
      }
    }
    controls.set(s.key, sync);
    const def = s.kind === 'image' ? t('sitetpl.image_default') : s.kind === 'bool' ? t(s.default ? 'common.yes' : 'common.no')
      : s.kind === 'select' && s.option_labels?.[String(s.default)] ? pick(s.option_labels[String(s.default)], state.lang, 'en') : String(s.default ?? '') || '–';
    const reset = button(t('sitetpl.reset'), () => {
      draft[s.key] = s.default;
      edits.set(s.key, null);
      sync();
      changed(s.key);
      status.textContent = t('sitetpl.reset_done', { name: label(s) });
    }, { variant: 'link', ariaLabel: `${t('sitetpl.reset')}: ${label(s)}` });
    const drawReset = () => { reset.hidden = isDefault(); };
    resets.set(s.key, drawReset);
    drawReset();
    const meta = h('p', { class: 'ew-help ew-tplset-default' }, t('sitetpl.default', { value: def }), ' ', reset);
    const body = s.kind === 'bool' ? h('div', { class: 'ew-field' }, control) : field(label(s), control, { help: s.help ? pick(s.help, state.lang, 'en') : undefined });
    return h('div', { class: 'ew-tplset-field', 'data-setting': s.key }, body, meta, ...extra);
  };

  const groups = GROUPS.filter((g) => cur.settings.some((s) => s.group === g)).map((g) => {
    const gid = uid('tplg');
    return h('fieldset', { class: 'ew-fieldset', 'aria-labelledby': gid }, h('legend', { id: gid }, t(`sitetpl.group_${g}`)),
      ...cur.settings.filter((s) => s.group === g).map(fieldOf));
  });
  const save = button(t('common.save'), async () => {
    if (!edits.size) { toast(t('sitetpl.nothing_changed')); return; }
    try {
      await api.put(`admin/templates/${encodeURIComponent(id)}/settings`, { values: Object.fromEntries(edits) });
      toast(t('sitetpl.saved'));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'primary' });
  const resetAll = button(t('sitetpl.reset_all'), async () => {
    if (!(await confirmDialog(t('sitetpl.reset_all_confirm', { name: templateLabel(id) }), t('sitetpl.reset_all'), true))) return;
    try {
      await api.del(`admin/templates/${encodeURIComponent(id)}/settings`);
      toast(t('sitetpl.saved'));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'secondary' });
  const pid = uid('tplp');
  return h('div', { class: 'ew-tplset' },
    h('form', { class: 'ew-tplset-form', onsubmit: (e: Event) => e.preventDefault() }, status, ...groups, h('div', { class: 'ew-actions' }, save, stored.size ? resetAll : null)),
    h('section', { class: 'ew-tplset-preview', 'aria-labelledby': pid }, h('h2', { id: pid }, t('sitetpl.preview')), h('p', { class: 'ew-help' }, t('sitetpl.preview_help')),
      deviceSwitch((d) => preview.resize(d)), preview.el));
}

/** The own fonts of the template designer (none without it). */
async function ownFonts(): Promise<string[]> {
  if (!(state.site.plugins ?? []).includes('core.template')) return [];
  try {
    return (await api.get<{ name: string }[]>('x/core.template/fonts')).map((f) => f.name).filter((f) => /^[A-Za-z0-9][A-Za-z0-9 _.-]*$/.test(f));
  } catch {
    return [];
  }
}

/** The size of an uploaded image (for the preview: the server reads it itself when saving). */
function imageSize(src: string): Promise<{ width: number; height: number }> {
  return new Promise((res, rej) => {
    const img = new Image();
    img.onload = () => res({ width: img.naturalWidth, height: img.naturalHeight });
    img.onerror = () => rej(new Error(t('sitetpl.image_unreadable')));
    img.src = src;
  });
}

/** The frame of the live preview: the website with the template and the
 * draft (see setupSettingsPreview in main.ts), as wide as the device chosen. */
function previewFrame(id: string, draft: Record<string, unknown>): { el: HTMLElement; update: (v: Record<string, unknown>) => void; resize: (d: Device) => void } {
  const w = window as unknown as { __ewTplSettingsDraft?: { id: string; values: Record<string, unknown> } };
  w.__ewTplSettingsDraft = { id, values: { ...draft } };
  const frame = h('iframe', { src: '/', title: t('sitetpl.preview_frame', { name: templateLabel(id) }), class: 'ew-tplset-frame', loading: 'lazy' }) as HTMLIFrameElement;
  // scrolls when the device is wider than the room: reachable with the keyboard
  const box = h('div', { class: 'ew-tplset-framebox', tabindex: '0', role: 'group', 'aria-label': t('sitetpl.preview') }, frame);
  const resize = (d: Device) => {
    const size = DEVICES.find((x) => x.id === d) ?? DEVICES[0];
    frame.style.width = `${size.width}px`;
    frame.style.height = `${Math.min(size.height, 900)}px`;
  };
  resize(currentDevice());
  return {
    el: box,
    resize,
    update: (v) => {
      w.__ewTplSettingsDraft = { id, values: { ...v } };
      try {
        (frame.contentWindow as unknown as { __ewTplSettingsUpdate?: (x: Record<string, unknown>) => void } | null)?.__ewTplSettingsUpdate?.({ ...v });
      } catch { /* not loaded yet: it reads the draft when it starts */ }
    },
  };
}
