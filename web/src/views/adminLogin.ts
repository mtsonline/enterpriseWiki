// Settings of the sign-in page: passkeys in the suggestions of the user
// name field, an automatic redirect to one provider, and the buttons of the
// providers with their own sign-in page (order, text, shown or direct link
// only). Stored apart from the other settings (/api/v1/admin/auth/login).
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, checkbox, errorText, field, select, textInput } from '../components/ui';
import type { Group, Role } from '../core/types';

interface LoginButton { order: number; hidden: boolean; label: string }
interface Registration { enabled: boolean; domains: string[]; groups: number[] }
interface LoginSettings {
  passkey_autofill: boolean;
  auto_redirect: string;
  buttons: Record<string, LoginButton>;
  providers: { id: string; name: string; kind: string; hidden?: boolean; order?: number }[];
  registration: Registration;
  password_reset: boolean;
  /** false on a further site that shares the accounts of the main site */
  registration_here: boolean;
  mail_ready: boolean;
}

/** Direct link to the sign-in with one provider (works for hidden buttons too). */
export function loginLink(id: string): string {
  return `${location.origin}/login?via=${encodeURIComponent(id)}`;
}

/** Form for the sign-in page; part of the settings of the administrators. */
export async function loginPageAdmin(): Promise<HTMLElement> {
  const [s, groups, roles] = await Promise.all([api.get<LoginSettings>('admin/auth/login'),
    api.get<Group[]>('groups').catch(() => [] as Group[]), api.get<Role[]>('roles').catch(() => [] as Role[])]);
  const redirect = s.providers.filter((p) => p.kind === 'redirect');
  const autofill = checkbox(t('login_admin.autofill'), s.passkey_autofill, { help: t('login_admin.autofill_help') });
  const auto = select([{ value: '', label: t('login_admin.auto_none') },
    ...redirect.map((p) => ({ value: p.id, label: `${p.name} (${p.id})` }))], s.auto_redirect);
  const rows = redirect.map((p) => {
    const b = s.buttons[p.id] ?? { order: 0, hidden: false, label: '' };
    const order = textInput({ type: 'number', value: String(b.order) });
    order.min = '-1000';
    order.max = '1000';
    const label = textInput({ value: b.label, maxlength: 80, placeholder: p.name });
    const shown = checkbox(t('login_admin.shown'), !b.hidden, { help: t('login_admin.shown_help') });
    const link = loginLink(p.id);
    const el = h('fieldset', { class: 'ew-subfield' }, h('legend', null, `${p.name} (${p.id})`),
      field(t('login_admin.order'), order, { help: t('login_admin.order_help') }),
      field(t('login_admin.label'), label, { help: t('login_admin.label_help') }), shown.el,
      h('p', { class: 'ew-help' }, t('login_admin.direct_link'), ' ', h('code', null, link)));
    return { id: p.id, el, value: (): LoginButton => ({ order: Number(order.value) || 0, hidden: !shown.input.checked, label: label.value.trim() }) };
  });
  const reset = checkbox(t('login_admin.reset'), s.password_reset, { help: t('login_admin.reset_help') });
  // registration of local accounts by the people themselves
  const reg = s.registration;
  const regOn = checkbox(t('login_admin.reg_enabled'), reg.enabled, { help: t('login_admin.reg_enabled_help') });
  regOn.input.disabled = !s.registration_here && !reg.enabled;
  const domains = textInput({ value: reg.domains.join(', '), placeholder: 'example.com, *.example.org' });
  const adminRoles = new Set(roles.filter((r) => r.is_admin).map((r) => r.id));
  const groupBoxes = groups.filter((g) => !(g.roles ?? []).some((r) => adminRoles.has(r)))
    .map((g) => ({ id: g.id, box: checkbox(g.name, reg.groups.includes(g.id)) }));
  const regBox = h('fieldset', null, h('legend', null, t('login_admin.reg_title')),
    !s.registration_here ? h('p', { class: 'ew-notice ew-notice--info' }, t('login_admin.reg_not_here')) : null,
    !s.mail_ready ? h('p', { class: 'ew-notice ew-notice--warning' }, t('login_admin.reg_no_mail')) : null,
    regOn.el,
    field(t('login_admin.reg_domains'), domains, { help: t('login_admin.reg_domains_help') }),
    groupBoxes.length ? h('fieldset', { class: 'ew-subfield' }, h('legend', null, t('login_admin.reg_groups')),
      h('p', { class: 'ew-help' }, t('login_admin.reg_groups_help')), ...groupBoxes.map((g) => g.box.el)) : null);
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true, 'aria-labelledby': 'ew-login-admin-h' },
    h('h2', { id: 'ew-login-admin-h' }, t('login_admin.title')), h('p', { class: 'ew-help' }, t('login_admin.help')), alert,
    autofill.el,
    field(t('login_admin.auto'), auto, { help: t('login_admin.auto_help') }),
    rows.length ? h('fieldset', null, h('legend', null, t('login_admin.buttons')), ...rows.map((r) => r.el))
      : h('p', { class: 'ew-help' }, t('login_admin.no_buttons')),
    h('fieldset', null, h('legend', null, t('login_admin.local_title')),
      !s.mail_ready ? h('p', { class: 'ew-notice ew-notice--warning' }, t('login_admin.reset_no_mail')) : null, reset.el),
    regBox,
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    // keep entries of providers that are not configured at the moment
    const buttons: Record<string, LoginButton> = { ...s.buttons };
    for (const r of rows) buttons[r.id] = r.value();
    try {
      // groups that are not offered (e.g. removed meanwhile) are not kept
      const registration: Registration = {
        enabled: regOn.input.checked,
        domains: domains.value.split(',').map((x) => x.trim()).filter(Boolean),
        groups: groupBoxes.filter((g) => g.box.input.checked).map((g) => g.id),
      };
      await api.put('admin/auth/login', { passkey_autofill: autofill.input.checked, auto_redirect: auto.value, buttons, registration, password_reset: reset.input.checked });
      toast(t('login_admin.saved'));
    } catch (e) { alert.textContent = errorText(e); }
  });
  return form;
}
