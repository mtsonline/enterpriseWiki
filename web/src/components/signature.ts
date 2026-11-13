// Signatures: choose a method, sign in the browser and show the evidence
// with its verification (record, method, seal of the wiki).
import { api } from '../core/api';
import { fmtDate, h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { webauthnCancelled, webauthnGet } from '../core/webauthn';
import { button, errorText, openDialog } from './ui';

export interface SignatureMethod { id: string; label: string; level: 'advanced' | 'qualified'; available: boolean }
export interface SignChallenge { kind: 'webauthn' | 'redirect'; public?: Record<string, unknown>; url?: string }

/** The methods that meet a requirement ("advanced", "qualified" or a method id). */
export async function signatureMethods(require: string): Promise<SignatureMethod[]> {
  const all = await api.get<SignatureMethod[]>(`signature-methods?lang=${state.lang}`);
  return all.filter((m) => require === '' || require === 'advanced' || (require === 'qualified' ? m.level === 'qualified' : m.id === require));
}

/** Answers a challenge in the browser; throws a readable error when cancelled. */
export async function signChallenge(ch: SignChallenge): Promise<unknown> {
  if (ch.kind === 'webauthn' && ch.public) {
    try {
      return await webauthnGet(ch.public);
    } catch (e) {
      if (webauthnCancelled(e)) throw new Error(t('signature.cancelled'));
      throw e;
    }
  }
  throw new Error(t('signature.unsupported'));
}

interface Verification {
  valid: boolean;
  checks: { name: string; ok: boolean; detail?: string }[];
  signature: { id: number; signer: { name: string; email: string; username: string }; method: string; level: string; signed_at: string; digest: string };
}

/** Shows a signature with its verification and the export. */
export async function openSignature(id: number): Promise<void> {
  let v: Verification;
  try {
    v = await api.get<Verification>(`signatures/${id}/verify`);
  } catch (e) {
    openDialog(t('signature.title'), h('p', { class: 'ew-field-error', role: 'alert' }, errorText(e)));
    return;
  }
  const s = v.signature;
  const d = openDialog(t('signature.title'), h('div', null,
    h('p', { class: ['ew-notice', v.valid ? 'ew-notice--success' : 'ew-notice--danger'], role: 'status' },
      v.valid ? t('signature.valid') : t('signature.invalid')),
    h('dl', { class: 'ew-dl' },
      h('dt', null, t('signature.signer')), h('dd', null, `${s.signer.name} (${s.signer.email || s.signer.username})`),
      h('dt', null, t('signature.when')), h('dd', null, fmtDate(s.signed_at, state.lang)),
      h('dt', null, t('signature.method')), h('dd', null, `${s.method} · ${t(`signature.level.${s.level}`)}`),
      h('dt', null, t('signature.digest')), h('dd', null, h('code', null, s.digest))),
    h('h3', null, t('signature.checks')),
    h('ul', { class: 'ew-plain-list' }, ...v.checks.map((c) => h('li', null,
      h('span', { 'aria-hidden': 'true' }, c.ok ? '✓ ' : '✗ '), h('span', { class: 'sr-only' }, c.ok ? t('signature.check_ok') : t('signature.check_failed')),
      t(c.name.includes('.check.') ? c.name : `signature.check.${c.name}`), c.detail ? h('span', { class: 'ew-muted' }, ` – ${c.detail}`) : null))),
    h('p', { class: 'ew-help' }, t('signature.export_help'), ' ', h('a', { href: '/signatures/verify' }, t('signature.verify_link')))),
  [h('a', { class: 'ew-btn ew-btn--secondary', href: `/api/v1/signatures/${id}/export`, download: `signature-${id}.json` }, t('signature.export')),
    button(t('common.close'), () => d.close(), { variant: 'primary' })]);
}
