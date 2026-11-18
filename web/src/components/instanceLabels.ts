// Labels of one instance (a blog, a FAQ …): the texts its plugin declared
// as changeable ("instance_labels"), per active language. They are stored as
// UI texts "<key>@<plugin>/<instance>", never in the instance; without an own
// text the plugin's text applies.
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { state } from '../core/state';
import { rerender } from '../core/router';
import { button, formDialog, textInput } from './ui';
import { langName } from './widgets';

interface Label { key: string; default: Record<string, string>; value: Record<string, string> }
interface Labels { labels: Label[]; languages: string[]; can_edit: boolean }

const cache = new Map<number, Promise<Labels | null>>();

function load(id: number): Promise<Labels | null> {
  if (!cache.has(id)) cache.set(id, api.get<Labels>(`elements/${id}/labels`).catch(() => null));
  return cache.get(id)!;
}

/** instanceLabels returns the text lookup of instance id: its own text in
 * the language shown, else the plugin's text (t). */
export async function instanceLabels(id: number): Promise<(key: string, params?: Record<string, string | number>) => string> {
  const l = await load(id);
  return (key, params) => {
    let v = l?.labels.find((x) => x.key === key)?.value[state.lang];
    if (!v) return t(key, params);
    if (params) for (const [k, p] of Object.entries(params)) v = v.split(`{${k}}`).join(String(p));
    return v;
  };
}

/** instanceLabelsDialog changes the labels of instance id. */
export async function instanceLabelsDialog(id: number, title: string, done?: () => void): Promise<void> {
  cache.delete(id);
  const l = await load(id);
  if (!l) { toast(t('labels.load_failed'), 'error'); return; }
  const inputs: { key: string; lang: string; input: HTMLInputElement }[] = [];
  const groups = l.labels.map((lb) => {
    const legendId = uid('lbl');
    return h('fieldset', { class: 'ew-fieldset', 'aria-labelledby': legendId },
      h('legend', { id: legendId }, lb.default[state.lang] || t(lb.key)),
      ...l.languages.map((lang) => {
        const input = textInput({ value: lb.value[lang] ?? '', maxlength: 200, placeholder: lb.default[lang] ?? '' });
        input.lang = lang;
        input.disabled = !l.can_edit;
        inputs.push({ key: lb.key, lang, input });
        const id2 = uid('lbli');
        input.id = id2;
        return h('div', { class: 'ew-field' }, h('label', { for: id2 }, langName(lang)), input);
      }));
  });
  const help = h('p', { class: 'ew-help' }, l.can_edit ? t('labels.help') : t('labels.readonly'));
  formDialog(t('labels.title', { title }), [help, ...groups], t('common.save'), async () => {
    if (!l.can_edit) return;
    const values: Record<string, Record<string, string>> = {};
    for (const x of inputs) {
      const before = l.labels.find((y) => y.key === x.key)?.value[x.lang] ?? '';
      if (x.input.value.trim() === before) continue;
      (values[x.lang] ??= {})[x.key] = x.input.value.trim();
    }
    if (Object.keys(values).length) await api.put(`elements/${id}/labels`, { values });
    cache.delete(id);
    toast(t('labels.saved'));
    done?.();
  }, { wide: true });
}

/** instanceLabelsButton: page action for instances whose plugin offers
 * labels; shown only to those who may change them. */
export function instanceLabelsButton(target: { type: 'element' | 'category'; id: number; title: string; elementType?: string }): HTMLElement | null {
  if (target.type !== 'element' || !state.user) return null;
  const info = state.site.element_types.find((x) => x.id === target.elementType);
  if (!info?.instance || !info.instance_labels?.length) return null;
  const b = button(t('labels.button'), () => void instanceLabelsDialog(target.id, target.title, () => void rerender()), { variant: 'secondary' });
  b.hidden = true;
  void load(target.id).then((l) => { b.hidden = !l?.can_edit; });
  return b;
}
