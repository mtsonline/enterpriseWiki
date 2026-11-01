// Admin page for LDAP / Active Directory.
import { api, button, errorText, field, h, MAPPING_FIELDS, mount, pageHeader, pluginSettingsForm, registerAdminPage, t, textInput } from '@ewiki/sdk';

export function register(): void {
  registerAdminPage({
    id: 'ldap', label: () => t('auth.ldap.title'), order: 20,
    render: async (el) => {
      const form = await pluginSettingsForm('auth.ldap', [
        { key: 'name', label: t('auth.oidc.name'), placeholder: 'LDAP' },
        { key: 'url', label: t('auth.ldap.url'), help: t('auth.ldap.url_help'), placeholder: 'ldaps://ldap.example.com:636' },
        { key: 'start_tls', label: t('auth.ldap.start_tls'), type: 'bool' },
        { key: 'insecure_skip_verify', label: t('auth.ldap.insecure'), type: 'bool' },
        { key: 'bind_dn', label: t('auth.ldap.bind_dn'), placeholder: 'cn=wiki,ou=services,dc=example,dc=com' },
        { key: 'bind_password', label: t('auth.ldap.bind_password'), type: 'secret', placeholder: '${LDAP_BIND_PASSWORD}' },
        { key: 'base_dn', label: t('auth.ldap.base_dn'), placeholder: 'ou=people,dc=example,dc=com' },
        { key: 'user_filter', label: t('auth.ldap.user_filter'), help: t('auth.ldap.user_filter_help'), placeholder: '(uid={username})' },
        { key: 'attr_username', label: t('auth.ldap.attr_username'), placeholder: 'uid' },
        { key: 'attr_email', label: t('auth.ldap.attr_email'), placeholder: 'mail' },
        { key: 'attr_display_name', label: t('auth.ldap.attr_display_name'), placeholder: 'cn' },
        { key: 'attr_subject', label: t('auth.ldap.attr_subject'), help: t('auth.ldap.attr_subject_help'), placeholder: 'entryUUID' },
        { key: 'group_attribute', label: t('auth.ldap.group_attribute'), placeholder: 'memberOf' },
        { key: 'group_base_dn', label: t('auth.ldap.group_base_dn'), placeholder: 'ou=groups,dc=example,dc=com' },
        { key: 'group_filter', label: t('auth.ldap.group_filter'), placeholder: '(|(member={dn})(uniqueMember={dn})(memberUid={username}))' },
        { key: 'group_name_attribute', label: t('auth.ldap.group_name_attribute'), placeholder: 'cn' },
        ...MAPPING_FIELDS(),
      ]);
      const user = textInput({ autocomplete: 'off' });
      const pw = textInput({ type: 'password', autocomplete: 'off' });
      const result = h('div', { role: 'status', class: 'ew-test-result' });
      const test = h('form', { class: 'ew-form ew-panel', novalidate: true }, h('h2', null, t('auth.ldap.test')),
        field(t('auth.username'), user), field(t('auth.password'), pw),
        h('div', { class: 'ew-form-actions' }, button(t('auth.ldap.test'), () => undefined, { type: 'submit' })), result);
      test.addEventListener('submit', async (ev) => {
        ev.preventDefault();
        result.textContent = '';
        try {
          const r = await api.post<{ ok: boolean; message?: string; identity?: unknown }>('/api/v1/x/auth.ldap/test', { username: user.value, password: pw.value });
          mount(result, r.ok ? h('div', null, h('p', null, h('strong', null, t('auth.ldap.test_ok'))), h('pre', { class: 'ew-code', tabindex: '0' }, JSON.stringify(r.identity, null, 2)))
            : h('p', { class: 'ew-field-error' }, t('auth.ldap.test_failed', { message: r.message ?? '' })));
        } catch (e) { result.textContent = errorText(e); }
      });
      mount(el, pageHeader(t('auth.ldap.title')), h('p', null, t('auth.ldap.help')), form.el, test);
    },
  });
}
