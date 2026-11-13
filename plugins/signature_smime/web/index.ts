// S/MIME: certificates of the organisation (administration) and personal
// certificates (profile) for signed e-mails.
import {
  api, button, checkbox, confirmDialog, errorText, field, formDialog, h, registerAdminPage, registerProfileSection,
  select, t, textInput, toast, uiLang,
} from '@ewiki/sdk';

const BASE = '/api/v1/x/signature.smime';

interface Cert {
  id: number; user_id: number | null; owner?: string; label: string; emails: string[]; subject: string; issuer: string;
  serial: string; fingerprint: string; not_before: string; not_after: string; trusted: boolean; sign_mail: boolean; seal: boolean;
  created_at: string; state: 'valid' | 'future' | 'expired'; days_left: number;
}
interface Settings { mail_signing: boolean; on_expired: 'unsigned' | 'hold'; personal: boolean; seal_evidence: boolean }
interface Me { personal: boolean; emails: string[]; certs: Cert[] }

/** Reads a file as base64 (without the data: prefix). */
function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ''));
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

function fmtDate(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime()) ? '' : d.toLocaleDateString(uiLang(), { dateStyle: 'medium' });
}

/** Validity as text: valid until …, expires in n days, expired, not yet valid. */
function stateText(c: Cert): string {
  if (c.state === 'expired') return t('signature.smime.state_expired', { date: fmtDate(c.not_after) });
  if (c.state === 'future') return t('signature.smime.state_future', { date: fmtDate(c.not_before) });
  if (c.days_left <= 60) return t('signature.smime.state_soon', { date: fmtDate(c.not_after), days: String(c.days_left) });
  return t('signature.smime.state_valid', { date: fmtDate(c.not_after) });
}

function certTable(list: Cert[], opts: { owner: boolean; seal: boolean; reload: () => void }): HTMLElement {
  if (!list.length) return h('p', null, t('signature.smime.none'));
  const head = [t('signature.smime.col_addresses'), ...(opts.owner ? [t('signature.smime.col_owner')] : []), t('signature.smime.col_valid'),
    t('signature.smime.col_use'), t('common.actions')];
  const rows = list.map((c) => {
    const name = c.label || c.emails.join(', ');
    const sign = checkbox(t('signature.smime.sign_mail'), c.sign_mail);
    sign.input.addEventListener('change', async () => {
      try { await api.patch(`${BASE}/certs/${c.id}`, { sign_mail: sign.input.checked }); toast(t('signature.smime.saved')); opts.reload(); } catch (e) { toast(errorText(e), 'error'); sign.input.checked = !sign.input.checked; }
    });
    const use: HTMLElement[] = [sign.el];
    if (opts.seal && c.user_id === null) {
      const seal = checkbox(t('signature.smime.seal'), c.seal);
      seal.input.addEventListener('change', async () => {
        try { await api.patch(`${BASE}/certs/${c.id}`, { seal: seal.input.checked }); toast(t('signature.smime.saved')); opts.reload(); } catch (e) { toast(errorText(e), 'error'); seal.input.checked = !seal.input.checked; }
      });
      use.push(seal.el);
    }
    const warn = c.state !== 'valid' || c.days_left <= 60;
    return h('tr', null,
      h('td', null, h('strong', null, name), c.label ? h('div', { class: 'ew-help' }, c.emails.join(', ')) : null,
        h('div', { class: 'ew-help' }, t('signature.smime.issuer', { issuer: c.issuer })),
        c.trusted ? null : h('div', { class: 'ew-help' }, t('signature.smime.untrusted'))),
      ...(opts.owner ? [h('td', null, c.owner || t('signature.smime.organisation'))] : []),
      h('td', null, warn ? h('strong', null, stateText(c)) : stateText(c)),
      h('td', null, ...use),
      h('td', null, button(t('common.remove'), async () => {
        if (!(await confirmDialog(t('signature.smime.remove_confirm', { name }), t('common.remove'), true))) return;
        try { await api.del(`${BASE}/certs/${c.id}`); toast(t('signature.smime.removed')); opts.reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.remove')}: ${name}` })));
  });
  return h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('signature.smime.title')),
    h('thead', null, h('tr', null, ...head.map((x) => h('th', { scope: 'col' }, x)))), h('tbody', null, ...rows));
}

/** Dialog to upload a certificate (PKCS#12 or PEM). */
function uploadDialog(scope: 'org' | 'mine', done: () => void): void {
  const file = h('input', { type: 'file', class: 'ew-input', accept: '.p12,.pfx,.pem,.crt,.key,application/x-pkcs12', required: true });
  const pass = textInput({ type: 'password', autocomplete: 'off' });
  const label = textInput({ maxlength: 100 });
  formDialog(t('signature.smime.upload'),
    h('div', null,
      field(t('signature.smime.file'), file, { help: t('signature.smime.file_help'), required: true }),
      field(t('signature.smime.password'), pass, { help: t('signature.smime.password_help') }),
      field(t('signature.smime.label'), label, { help: t('signature.smime.label_help') })),
    t('signature.smime.upload'), async () => {
      const f = file.files?.[0];
      if (!f) throw new Error(t('signature.smime.file_missing'));
      const data = await readBase64(f);
      try {
        await api.post(`${BASE}/certs`, { scope, file: data, password: pass.value, label: label.value });
      } finally { pass.value = ''; }
      toast(t('signature.smime.added'));
      done();
    });
}

async function adminPage(panel: HTMLElement): Promise<void> {
  const body = h('div', { 'aria-live': 'polite' });
  const draw = async (): Promise<void> => {
    let st: Settings; let list: Cert[]; let all: Cert[];
    try {
      [st, list, all] = await Promise.all([api.get<Settings>(`${BASE}/settings`), api.get<Cert[]>(`${BASE}/certs?scope=org`), api.get<Cert[]>(`${BASE}/certs?scope=all`)]);
    } catch (e) { body.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    const personal = all.filter((c) => c.user_id !== null);
    const signing = checkbox(t('signature.smime.set_mail_signing'), st.mail_signing, { help: t('signature.smime.set_mail_signing_help') });
    const pers = checkbox(t('signature.smime.set_personal'), st.personal, { help: t('signature.smime.set_personal_help') });
    const seal = checkbox(t('signature.smime.set_seal'), st.seal_evidence, { help: t('signature.smime.set_seal_help') });
    const expired = select([{ value: 'unsigned', label: t('signature.smime.on_expired_unsigned') }, { value: 'hold', label: t('signature.smime.on_expired_hold') }], st.on_expired);
    const form = h('form', { class: 'ew-form', novalidate: true },
      h('h2', null, t('signature.smime.settings')), signing.el,
      field(t('signature.smime.on_expired'), expired, { help: t('signature.smime.on_expired_help') }),
      pers.el, seal.el,
      h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      try {
        await api.put(`${BASE}/settings`, { mail_signing: signing.input.checked, on_expired: expired.value, personal: pers.input.checked, seal_evidence: seal.input.checked });
        toast(t('signature.smime.saved'));
      } catch (e) { toast(errorText(e), 'error'); }
    });
    body.replaceChildren(
      h('h2', null, t('signature.smime.org_certs')), h('p', null, t('signature.smime.org_help')),
      certTable(list, { owner: false, seal: true, reload: () => void draw() }),
      h('p', null, button(t('signature.smime.upload'), () => uploadDialog('org', () => void draw()), { variant: 'secondary' })),
      h('h2', null, t('signature.smime.personal_certs')), h('p', null, t('signature.smime.personal_admin_help')),
      certTable(personal, { owner: true, seal: false, reload: () => void draw() }),
      form);
  };
  panel.replaceChildren(h('div', { class: 'ew-page-header' }, h('h1', null, t('signature.smime.title'))), h('p', null, t('signature.smime.admin_help')), body);
  await draw();
}

async function profileSection(): Promise<Node | null> {
  let me: Me;
  try { me = await api.get<Me>(`${BASE}/me`); } catch { return null; }
  if (!me.personal && !me.certs.length) return null;
  const box = h('section', { class: 'ew-subsection', id: 'smime', 'aria-labelledby': 'smime-title' });
  const draw = async (): Promise<void> => {
    try { me = await api.get<Me>(`${BASE}/me`); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    const parts: Node[] = [
      h('h2', { id: 'smime-title' }, t('signature.smime.mine_title')),
      h('p', null, t('signature.smime.mine_help')),
      h('p', { class: 'ew-help' }, t('signature.smime.mine_legal'))];
    if (me.emails.length) parts.push(h('p', { class: 'ew-help' }, t('signature.smime.mine_addresses', { emails: me.emails.join(', ') })));
    parts.push(certTable(me.certs, { owner: false, seal: false, reload: () => void draw() }));
    if (me.personal) parts.push(h('p', null, button(t('signature.smime.upload'), () => uploadDialog('mine', () => void draw()), { variant: 'secondary' })));
    box.replaceChildren(...parts);
  };
  await draw();
  return box;
}

export function register(): void {
  registerAdminPage({ id: 'smime', label: () => t('signature.smime.title'), permission: 'signature.smime.manage', render: adminPage });
  registerProfileSection({ id: 'signature.smime', order: 60, render: profileSection });
}
