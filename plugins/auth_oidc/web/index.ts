// Admin page for OpenID Connect.
import { api, button, errorText, h, MAPPING_FIELDS, mount, pageHeader, pluginSettingsForm, registerAdminPage, t } from '@ewiki/sdk';

export function register(): void {
  registerAdminPage({
    id: 'oidc', label: () => t('auth.oidc.title'), order: 21,
    render: async (el) => {
      const form = await pluginSettingsForm('auth.oidc', [
        { key: 'name', label: t('auth.oidc.name'), placeholder: 'Single sign-on' },
        { key: 'issuer', label: t('auth.oidc.issuer'), placeholder: 'https://keycloak.example.com/realms/acme' },
        { key: 'client_id', label: t('auth.oidc.client_id') },
        { key: 'client_secret', label: t('auth.oidc.client_secret'), type: 'secret', placeholder: '${OIDC_CLIENT_SECRET}' },
        { key: 'scopes', label: t('auth.oidc.scopes'), type: 'list', placeholder: 'openid, profile, email, groups' },
        { key: 'claim_username', label: t('auth.oidc.claim_username'), placeholder: 'preferred_username' },
        { key: 'claim_email', label: t('auth.oidc.claim_email'), placeholder: 'email' },
        { key: 'claim_display_name', label: t('auth.oidc.claim_display_name'), placeholder: 'name' },
        { key: 'claim_groups', label: t('auth.oidc.claim_groups'), placeholder: 'groups' },
        ...MAPPING_FIELDS(),
      ]);
      const result = h('div', { role: 'status', class: 'ew-test-result' });
      const check = button(t('auth.oidc.check'), async () => {
        result.textContent = '';
        try {
          const r = await api.get<{ ok: boolean; message?: string; redirect_uri?: string }>('/api/v1/x/auth.oidc/check');
          result.textContent = r.ok ? t('auth.oidc.check_ok') : t('auth.oidc.check_failed', { message: r.message ?? '' });
        } catch (e) { result.textContent = errorText(e); }
      });
      const redirect = `${location.origin}/api/v1/auth/sso/oidc/callback`;
      mount(el, pageHeader(t('auth.oidc.title')), h('p', null, t('auth.oidc.help')), h('pre', { class: 'ew-code', tabindex: '0' }, redirect),
        form.el, h('div', { class: 'ew-panel' }, check, result));
    },
  });
}
