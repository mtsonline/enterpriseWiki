// Admin page for OpenID Connect (several providers possible).
import { api, button, errorText, h, MAPPING_FIELDS, registerAdminPage, renderAuthProviderPage, t } from '@ewiki/sdk';

export function register(): void {
  registerAdminPage({
    permission: 'core.settings.manage',
    id: 'oidc', label: () => t('auth.oidc.title'), order: 21,
    render: (el) => renderAuthProviderPage(el, {
      pluginId: 'auth.oidc', base: 'oidc', title: t('auth.oidc.title'), help: t('auth.oidc.help'),
      fields: () => [
        { key: 'name', label: t('auth.oidc.name'), placeholder: 'Single sign-on' },
        { key: 'issuer', label: t('auth.oidc.issuer'), placeholder: 'https://keycloak.example.com/realms/acme' },
        { key: 'client_id', label: t('auth.oidc.client_id') },
        { key: 'client_secret', label: t('auth.oidc.client_secret'), type: 'secret', placeholder: '${OIDC_CLIENT_SECRET}' },
        { key: 'scopes', label: t('auth.oidc.scopes'), type: 'list', placeholder: 'openid, profile, email, groups' },
        { key: 'claim_username', label: t('auth.oidc.claim_username'), placeholder: 'preferred_username' },
        { key: 'claim_email', label: t('auth.oidc.claim_email'), placeholder: 'email' },
        { key: 'claim_display_name', label: t('auth.oidc.claim_display_name'), placeholder: 'name' },
        { key: 'claim_groups', label: t('auth.oidc.claim_groups'), placeholder: 'groups' },
        { key: 'single_logout', label: t('auth.oidc.single_logout'), type: 'bool', default: true, help: t('auth.oidc.single_logout_help') },
        { key: 'post_logout_redirect_uri', label: t('auth.oidc.post_logout'), placeholder: `${location.origin}/login?logged_out=1` },
        ...MAPPING_FIELDS(),
      ],
      extra: (provider) => {
        const result = h('div', { role: 'status', class: 'ew-test-result' });
        const check = button(t('auth.oidc.check'), async () => {
          result.textContent = '';
          try {
            const r = await api.get<{ ok: boolean; message?: string }>(`/api/v1/x/auth.oidc/check?instance=${encodeURIComponent(provider)}`);
            result.textContent = r.ok ? t('auth.oidc.check_ok') : t('auth.oidc.check_failed', { message: r.message ?? '' });
          } catch (e) { result.textContent = errorText(e); }
        });
        const base = `${location.origin}/api/v1/auth/sso/${provider}`;
        return h('div', { class: 'ew-panel' },
          h('p', null, t('auth.oidc.redirect_uri')), h('pre', { class: 'ew-code', tabindex: '0' }, `${base}/callback`),
          h('p', null, t('auth.oidc.backchannel_uri')), h('pre', { class: 'ew-code', tabindex: '0' }, `${base}/logout`),
          check, result);
      },
    }),
  });
}
