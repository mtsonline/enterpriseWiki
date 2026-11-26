// Admin page of the web application firewall: status, mode, paranoia level.
import { api, errorText, h, mount, pageHeader, pluginSettingsForm, registerAdminPage, t, toast } from '@ewiki/coreplugin';

const BASE = 'x/core.waf';

interface Status { mode: 'detect' | 'block'; paranoia: number; rules: string; body_kb: number; loaded: boolean; error?: string }

async function render(el: HTMLElement): Promise<void> {
  const [st, form] = await Promise.all([api.get<Status>(`${BASE}/status`), pluginSettingsForm('core.waf', [
    { key: 'mode', label: t('waf.mode'), type: 'select', help: t('waf.mode_help'), options: [
      { value: 'block', label: t('waf.mode.block') }, { value: 'detect', label: t('waf.mode.detect') }] },
    { key: 'paranoia', label: t('waf.paranoia'), type: 'select', help: t('waf.paranoia_help'), options: [
      { value: '1', label: t('waf.paranoia.1') }, { value: '2', label: t('waf.paranoia.2') }] },
  ])]);
  mount(el, pageHeader(t('waf.title')), h('p', null, t('waf.help')),
    h('div', { class: ['ew-notice', st.loaded ? (st.mode === 'block' ? 'ew-notice--success' : 'ew-notice--info') : 'ew-notice--warning'], role: 'status' },
      h('p', null, st.loaded ? t(st.mode === 'block' ? 'waf.status.block' : 'waf.status.detect', { rules: st.rules, level: String(st.paranoia) }) : t('waf.status.error', { error: st.error ?? '' })),
      h('p', null, t('waf.status.body', { kb: String(st.body_kb) }))),
    form.el);
}

export function register(): void {
  registerAdminPage({ id: 'waf', permission: 'core.waf.manage', label: () => t('waf.title'), order: 66, render: (el) => render(el).catch((e) => toast(errorText(e), 'error')) });
}
