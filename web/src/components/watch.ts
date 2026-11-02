// Watch button and dialog for categories and elements (e-mail on changes).
import { api, qs } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, field, formDialog, select } from './ui';

export interface Watch {
  id: number;
  target_type: 'category' | 'element';
  target_id: number;
  mode: 'link' | 'content';
  recursive: boolean;
  created_at: string;
  titles: Record<string, string>;
  frequency: 'instant' | 'daily' | 'weekly';
}

/**
 * Returns a "Watch" button for logged-in users (null for guests). The label
 * shows whether the object is watched; clicking opens the watch dialog.
 */
export function watchButton(type: 'category' | 'element', id: number, title: string): HTMLButtonElement | null {
  if (!state.user) return null;
  let current: Watch | null = null;
  let enabled = true;
  const btn = button(t('watch.button'), () => watchDialog(type, id, title, current, enabled, (w) => { current = w; update(); }), { variant: 'secondary' });
  btn.classList.add('ew-watch-btn');
  const update = () => {
    btn.textContent = current ? t('watch.watching') : t('watch.button');
    btn.classList.toggle('is-active', !!current);
  };
  void api.get<{ watch: Watch | null; enabled: boolean }>(`me/watches${qs({ target_type: type, target_id: id })}`)
    .then((r) => { current = r.watch; enabled = r.enabled; update(); })
    .catch(() => undefined);
  return btn;
}

/** Opens the dialog to create, change or remove a watch. */
export function watchDialog(type: 'category' | 'element', id: number, title: string, current: Watch | null, enabled: boolean, done: (w: Watch | null) => void): void {
  const name = uid('watchmode');
  const radio = (value: 'link' | 'content', label: string) => {
    const input = h('input', { type: 'radio', name, value, id: uid('wm') }) as HTMLInputElement;
    input.checked = (current?.mode ?? 'link') === value;
    return { input, el: h('div', { class: 'ew-check' }, input, h('label', { for: input.id }, label)) };
  };
  const link = radio('link', t('watch.mode_link'));
  const content = radio('content', t('watch.mode_content'));
  const freq = select((['instant', 'daily', 'weekly'] as const).map((f) => ({ value: f, label: t(`watch.freq_${f}`) })), current?.frequency ?? 'instant');
  const rec = type === 'category' ? checkbox(t('watch.recursive'), current?.recursive ?? true) : null;
  let handle: { close: () => void } | null = null;
  const stop = current ? button(t('watch.stop'), () => {
    void api.del(`me/watches/${current!.id}`).then(() => {
      toast(t('watch.removed'));
      done(null);
      handle?.close();
    });
  }, { variant: 'danger' }) : null;
  handle = formDialog(t('watch.dialog_title', { title }), [
    enabled ? null : h('p', { class: 'ew-notice', role: 'note' }, t('watch.mail_disabled')),
    h('p', { class: 'ew-help' }, t('watch.help')),
    field(t('watch.frequency'), freq, { help: t('watch.frequency_help') }),
    h('fieldset', null, h('legend', null, t('watch.mode')), link.el, content.el),
    rec?.el ?? null,
    stop ? h('p', null, stop) : null,
  ], t('watch.save'), async () => {
    const w = await api.put<Watch>('me/watches', {
      target_type: type, target_id: id, mode: content.input.checked ? 'content' : 'link', frequency: freq.value, recursive: rec ? rec.input.checked : true,
    });
    toast(t('watch.saved'));
    done(w);
  });
}
