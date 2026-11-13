// /signatures/verify: check an exported signature (JSON) against this
// wiki, and the public seal keys of the wiki to compare elsewhere.
import { api } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { errorText, field, pageHeader } from '../components/ui';
import { setContext } from '../components/widgets';

interface Key { id: number; fingerprint: string; created_at: string }
interface Result { valid: boolean; checks: { name: string; ok: boolean; detail?: string }[] }

export async function signatureVerifyView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  if (!state.user) {
    navigate('/login?next=/signatures/verify', { replace: true });
    return;
  }
  const file = h('input', { type: 'file', accept: 'application/json,.json', class: 'ew-input' }) as HTMLInputElement;
  const result = h('div', { 'aria-live': 'polite' });
  file.addEventListener('change', async () => {
    const f = file.files?.[0];
    result.replaceChildren();
    if (!f) return;
    try {
      if (f.size > 4 * 1024 * 1024) throw new Error(t('signature.verify_too_big'));
      const r = await api.post<Result>('signatures/verify', JSON.parse(await f.text()));
      result.replaceChildren(
        h('p', { class: ['ew-notice', r.valid ? 'ew-notice--success' : 'ew-notice--danger'], role: 'status' }, r.valid ? t('signature.valid') : t('signature.invalid')),
        h('ul', { class: 'ew-plain-list' }, ...r.checks.map((c) => h('li', null,
          h('span', { 'aria-hidden': 'true' }, c.ok ? '✓ ' : '✗ '), h('span', { class: 'sr-only' }, c.ok ? t('signature.check_ok') : t('signature.check_failed')),
          t(c.name.includes('.check.') ? c.name : `signature.check.${c.name}`), c.detail ? h('span', { class: 'ew-muted' }, ` – ${c.detail}`) : null))));
    } catch (e) {
      result.replaceChildren(h('p', { class: 'ew-field-error', role: 'alert' }, e instanceof SyntaxError ? t('signature.verify_not_json') : errorText(e)));
    }
  });
  let keys: Key[] = [];
  try { keys = await api.get<Key[]>('signature-keys'); } catch { /* shown as empty */ }
  mount(main, pageHeader(t('signature.verify_title')),
    h('p', null, t('signature.verify_help')),
    field(t('signature.verify_file'), file), result,
    h('h2', null, t('signature.keys')),
    h('p', { class: 'ew-help' }, t('signature.keys_help')),
    keys.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('signature.keys')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, 'ID'), h('th', { scope: 'col' }, t('signature.fingerprint')), h('th', { scope: 'col' }, t('signature.created')))),
      h('tbody', null, ...keys.map((k) => h('tr', null, h('td', null, String(k.id)), h('td', null, h('code', null, k.fingerprint)), h('td', null, fmtDate(k.created_at, state.lang))))))
      : h('p', { class: 'ew-muted' }, t('signature.no_keys')));
}
