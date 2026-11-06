// OnlyOffice Docs: administration and the editor of the OnlyOffice API.
import { officeAdminRender, registerAdminPage, registerOfficeRenderer, t } from '@ewiki/sdk';

const ID = 'office.onlyoffice';

interface DocsAPI { DocEditor: new (id: string, config: unknown) => { destroyEditor?: () => void } }

const loaded = new Map<string, Promise<void>>();

/** Loads api.js of the Document Server once (allowed by the page's CSP). */
function loadScript(src: string): Promise<void> {
  let p = loaded.get(src);
  if (!p) {
    p = new Promise<void>((resolve, reject) => {
      const s = document.createElement('script');
      s.src = src;
      s.async = true;
      s.onload = () => resolve();
      s.onerror = () => { loaded.delete(src); reject(new Error(t(`${ID}.script_failed`))); };
      document.head.append(s);
    });
    loaded.set(src, p);
  }
  return p;
}

export function register(): void {
  registerOfficeRenderer('onlyoffice', async (container, action) => {
    if (!action.script) throw new Error(t(`${ID}.script_failed`));
    await loadScript(action.script);
    const api = (window as unknown as { DocsAPI?: DocsAPI }).DocsAPI;
    if (!api) throw new Error(t(`${ID}.script_failed`));
    const holder = document.createElement('div');
    holder.id = `ew-onlyoffice-${Date.now()}`;
    holder.className = 'ew-office-frame';
    container.append(holder);
    const cfg = { ...(action.config as Record<string, unknown>), width: '100%', height: '100%', type: 'desktop' };
    const ed = new api.DocEditor(holder.id, cfg);
    return () => ed.destroyEditor?.();
  });
  registerAdminPage({
    id: 'onlyoffice', permission: 'core.settings.manage', label: () => t(`${ID}.title`), order: 72,
    render: officeAdminRender(ID, [
      { key: 'url', label: t(`${ID}.url`), placeholder: 'https://docs.example.com', help: t(`${ID}.url_help`) },
      { key: 'mode', label: t(`${ID}.mode`), type: 'select', help: t(`${ID}.mode_help`), options: [
        { value: 'api', label: t(`${ID}.mode.api`) }, { value: 'wopi', label: t(`${ID}.mode.wopi`) }] },
      { key: 'jwt_secret', label: t(`${ID}.jwt_secret`), type: 'secret', help: t(`${ID}.jwt_secret_help`), when: (v) => v.mode !== 'wopi' },
      { key: 'internal_url', label: t(`${ID}.internal_url`), placeholder: 'http://onlyoffice', help: t(`${ID}.internal_url_help`), when: (v) => v.mode !== 'wopi' },
      { key: 'verify_proof', label: t(`${ID}.verify_proof`), type: 'bool', default: true, help: t(`${ID}.verify_proof_help`), when: (v) => v.mode === 'wopi' },
      { key: 'readonly_without_download', label: t(`${ID}.readonly`), type: 'bool', default: true, help: t(`${ID}.readonly_help`) },
    ], { proof: true }),
  });
}
