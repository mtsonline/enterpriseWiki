// Administration of Microsoft 365 for the web (settings and connection test).
import { officeAdminRender, registerAdminPage, t } from '@ewiki/sdk';

const ID = 'office.m365';

export function register(): void {
  registerAdminPage({
    id: 'm365', permission: 'core.settings.manage', label: () => t(`${ID}.title`), order: 71,
    render: officeAdminRender(ID, [
      { key: 'enabled', label: t(`${ID}.enabled`), type: 'bool', help: t(`${ID}.enabled_help`) },
      { key: 'url', label: t(`${ID}.url`), placeholder: 'https://onenote.officeapps.live.com/hosting/discovery', help: t(`${ID}.url_help`), when: (v) => v.enabled === true },
      { key: 'verify_proof', label: t(`${ID}.verify_proof`), type: 'bool', default: true, help: t(`${ID}.verify_proof_help`), when: (v) => v.enabled === true },
    ], { proof: true }),
  });
}
