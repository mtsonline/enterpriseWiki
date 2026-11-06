// Administration of Collabora Online (settings and connection test).
import { officeAdminRender, registerAdminPage, t } from '@ewiki/sdk';

const ID = 'office.collabora';

export function register(): void {
  registerAdminPage({
    id: 'collabora', permission: 'core.settings.manage', label: () => t(`${ID}.title`), order: 70,
    render: officeAdminRender(ID, [
      { key: 'url', label: t(`${ID}.url`), placeholder: 'https://collabora.example.com', help: t(`${ID}.url_help`) },
      { key: 'verify_proof', label: t(`${ID}.verify_proof`), type: 'bool', default: true, help: t(`${ID}.verify_proof_help`) },
      { key: 'readonly_without_download', label: t(`${ID}.readonly`), type: 'bool', default: true, help: t(`${ID}.readonly_help`) },
    ], { proof: true }),
  });
}
