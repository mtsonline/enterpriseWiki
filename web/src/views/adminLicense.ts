// Administration → License: license key (offline check) and third-party licenses.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { button, confirmDialog, errorText, field, pageHeader, textArea } from '../components/ui';

export interface LicenseStatus {
  installed: boolean; valid: boolean; error?: string; active_users: number; over_users: boolean; expired: boolean; days_left?: number;
  level: 'ok' | 'info' | 'warning' | 'error';
  license?: { id: string; licensee: string; email?: string; edition: string; users: number; issued: string; expires?: string; note?: string };
}

const reload = () => navigate(location.pathname + location.search, { replace: true });
const date = (d?: string) => (d ? new Date(`${d}T00:00`).toLocaleDateString(state.lang) : '');

/** Short text about problems with the license (null when everything is fine). */
export function licenseNotice(st: LicenseStatus): HTMLElement | null {
  if (st.level === 'ok') return null;
  let text = t('license.none');
  if (st.error && !st.expired) text = t('license.invalid', { error: st.error });
  else if (st.expired) text = t('license.expired', { date: date(st.license?.expires) });
  else if (st.over_users) text = t('license.over_users', { active: st.active_users, users: st.license?.users ?? 0 });
  else if (st.days_left !== undefined) text = t('license.expires_soon', { days: st.days_left, date: date(st.license?.expires) });
  return h('div', { class: `ew-license-notice ew-license-notice--${st.level}`, role: st.level === 'error' ? 'alert' : 'note' },
    text, ' ', h('a', { href: '/admin/license' }, t('license.manage')));
}

export async function licenseAdmin(panel: HTMLElement): Promise<void> {
  const st = await api.get<LicenseStatus>('admin/license');
  const l = st.license;
  const key = textArea({ value: '', rows: 4 });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    field(st.installed ? t('license.replace_key') : t('license.key'), key, { help: t('license.key_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('license.install'), () => undefined, { type: 'submit', variant: 'primary' }),
      st.installed ? button(t('license.remove'), async () => {
        if (!(await confirmDialog(t('license.remove_confirm'), t('license.remove'), true))) return;
        try { await api.del('admin/license'); toast(t('license.removed')); reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'danger' }) : null));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try { await api.put('admin/license', { key: key.value.trim() }); toast(t('license.installed')); reload(); } catch (e) { alert.textContent = errorText(e); }
  });
  const facts: [string, string][] = l ? [
    [t('license.licensee'), l.licensee], [t('license.edition'), t(`license.edition.${l.edition}`)], [t('license.id'), l.id],
    [t('license.users'), l.users ? t('license.users_of', { active: st.active_users, users: l.users }) : t('license.users_unlimited', { active: st.active_users })],
    [t('license.valid_until'), l.expires ? date(l.expires) : t('license.unlimited')], [t('license.issued'), date(l.issued)],
    ...(l.email ? [[t('license.contact'), l.email] as [string, string]] : []), ...(l.note ? [[t('license.note'), l.note] as [string, string]] : []),
  ] : [];
  mount(panel, pageHeader(t('admin.license')),
    licenseNotice(st),
    l ? h('section', { class: 'ew-section', 'aria-labelledby': 'lic-h' }, h('h2', { id: 'lic-h' }, st.valid ? t('license.licensed') : t('license.not_valid')),
      h('dl', { class: 'ew-facts' }, ...facts.flatMap(([k, v]) => [h('dt', null, k), h('dd', null, v)]))) : null,
    h('p', null, t('license.terms')),
    form,
    h('section', { class: 'ew-section', 'aria-labelledby': 'lic-3p' }, h('h2', { id: 'lic-3p' }, t('license.third_party')),
      h('p', null, t('license.third_party_help'), ' ', h('a', { href: '/licenses.txt', target: '_blank', rel: 'noopener' }, t('license.third_party_link')))));
}
