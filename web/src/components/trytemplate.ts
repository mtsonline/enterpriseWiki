// Trying a website template only for oneself: the bar at the top of every
// page while a template is tried in this browser (coreplugin: triedTemplate).
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { hasPerm, state } from '../core/state';
import { button, errorText } from './ui';
import { toast } from '../core/announce';
import { setTriedTemplate, templateLabel } from '../coreplugin';

/** The bar: what is tried, end it, or (with the settings) use it for everybody. */
export function tryBar(id: string): HTMLElement {
  const end = () => { setTriedTemplate(''); location.reload(); };
  const useForAll = async () => {
    try {
      const s = await api.get<{ config: { ui: Record<string, unknown> } }>('admin/settings');
      await api.put('admin/settings', { ui: { ...s.config.ui, template: id } });
      setTriedTemplate('');
      toast(t('template_try.used', { name: templateLabel(id) }));
      setTimeout(() => location.reload(), 600);
    } catch (e) { toast(errorText(e), 'error'); }
  };
  return h('div', { class: 'ew-try-bar', role: 'region', 'aria-label': t('template_try.region') },
    h('p', null, t('template_try.bar', { name: templateLabel(id), site: templateLabel(state.site.template) })),
    h('div', { class: 'ew-actions' },
      hasPerm('core.settings.manage') ? button(t('template_try.use_for_all'), () => void useForAll(), { variant: 'primary' }) : null,
      button(t('template_try.end'), end)));
}
