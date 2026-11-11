// Admin page for Shibboleth / SAML (several identity providers possible).
import { h, MAPPING_FIELDS, registerAdminPage, renderAuthProviderPage, t } from '@ewiki/sdk';

export function register(): void {
  registerAdminPage({
    permission: 'core.settings.manage',
    id: 'shibboleth', label: () => t('auth.shib.title'), order: 22,
    render: (el) => {
      const saml = (v: Record<string, unknown>) => (v.mode ?? 'saml') === 'saml';
      const header = (v: Record<string, unknown>) => v.mode === 'header';
      return renderAuthProviderPage(el, {
        pluginId: 'auth.shib', base: 'shib', title: t('auth.shib.title'), help: t('auth.shib.help'),
        fields: () => [
          { key: 'mode', label: t('auth.shib.mode'), type: 'select', options: [{ value: 'saml', label: t('auth.shib.mode.saml') }, { value: 'header', label: t('auth.shib.mode.header') }] },
          { key: 'name', label: t('auth.shib.name'), placeholder: 'Shibboleth' },
          { key: 'idp_metadata_url', label: t('auth.shib.idp_metadata_url'), when: saml, placeholder: 'https://idp.example.org/idp/shibboleth' },
          { key: 'idp_metadata', label: t('auth.shib.idp_metadata'), type: 'textarea', when: saml },
          { key: 'entity_id', label: t('auth.shib.entity_id'), when: saml },
          { key: 'attr_username', label: t('auth.shib.attr_username'), when: saml, placeholder: 'urn:oid:1.3.6.1.4.1.5923.1.1.1.6' },
          { key: 'attr_email', label: t('auth.shib.attr_email'), when: saml, placeholder: 'urn:oid:0.9.2342.19200300.100.1.3' },
          { key: 'attr_display_name', label: t('auth.shib.attr_display_name'), when: saml, placeholder: 'urn:oid:2.16.840.1.113730.3.1.241' },
          { key: 'attr_groups', label: t('auth.shib.attr_groups'), when: saml, placeholder: 'urn:oid:1.3.6.1.4.1.5923.1.5.1.1' },
          { key: 'single_logout', label: t('auth.shib.single_logout'), type: 'bool', default: true, when: saml, help: t('auth.shib.single_logout_help') },
          { key: 'header_username', label: t('auth.shib.header_username'), when: header, placeholder: 'REMOTE_USER' },
          { key: 'header_email', label: t('auth.shib.header_email'), when: header, placeholder: 'mail' },
          { key: 'header_display_name', label: t('auth.shib.header_display_name'), when: header, placeholder: 'displayName' },
          { key: 'header_groups', label: t('auth.shib.header_groups'), when: header, placeholder: 'isMemberOf' },
          { key: 'header_separator', label: t('auth.shib.header_separator'), when: header, placeholder: ';' },
          ...MAPPING_FIELDS(),
        ],
        extra: (provider) => {
          const md = `${location.origin}/api/v1/x/auth.shib/metadata${provider === 'shib' ? '' : `?instance=${encodeURIComponent(provider)}`}`;
          return h('p', { class: 'ew-panel' }, t('auth.shib.metadata'), ': ', h('a', { href: md, rel: 'external', target: '_blank' }, md));
        },
      });
    },
  });
}
