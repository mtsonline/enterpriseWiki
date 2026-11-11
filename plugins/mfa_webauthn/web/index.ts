// Security keys and passkeys: the sign-in step, the setup and the profile
// part. As factor 1 every key is a passkey (sign-in without password).
import {
  api, ApiError, button, checkbox, confirmDialog, enrollFactor, errorText, field, fmtDate, h, registerMFAMethodUI, state, t, textInput, toast,
  webauthnCancelled, webauthnCreate, webauthnGet, webauthnSupported,
} from '@ewiki/sdk';
import type { MFAEnrollContext } from '@ewiki/sdk';

interface Credential { id: number; name: string; passkey: boolean; created_at: string; last_used_at: string | null }

const BASE = '/api/v1/x/mfa.webauthn';

const problem = (e: unknown): string =>
  webauthnCancelled(e) ? t('webauthn.cancelled') : e instanceof ApiError && e.code === 'reauth_required' ? t('webauthn.reauth') : errorText(e);

// the factor number of the own chain (factor 1: only passkeys)
let ownFactor = 2;

function enroll(ctx: MFAEnrollContext): HTMLElement {
  if (!webauthnSupported()) return h('p', null, t('webauthn.unsupported'));
  const name = textInput({ name: 'name', required: true, maxlength: 100 });
  const passkey = checkbox(t('webauthn.as_passkey'), ownFactor === 1, { help: t('webauthn.as_passkey_help') });
  if (ownFactor === 1) passkey.input.disabled = true;
  const form = h('form', { class: 'ew-form', novalidate: true },
    field(t('webauthn.name'), name, { required: true, help: t('webauthn.name_help') }), passkey.el,
    h('div', { class: 'ew-form-actions' }, button(t('webauthn.add'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const n = name.value.trim();
    if (!n) { ctx.fail(t('webauthn.name_needed')); name.focus(); return; }
    try {
      const options = await ctx.begin({ passkey: passkey.input.checked }) as Record<string, unknown>;
      const credential = await webauthnCreate(options);
      await ctx.finish({ name: n, credential });
    } catch (e) { ctx.fail(problem(e)); }
  });
  queueMicrotask(() => name.focus());
  return form;
}

export function register(): void {
  registerMFAMethodUI({
    id: 'webauthn', label: () => t('mfa.method.webauthn'),
    render: (ctx) => {
      const use = button(t('auth.webauthn_use'), async () => {
        try {
          const options = await ctx.challenge() as Record<string, unknown>;
          const credential = await webauthnGet(options);
          await ctx.verify({ credential });
        } catch (e) { ctx.fail(problem(e)); }
      }, { variant: 'primary' });
      if (!webauthnSupported()) use.disabled = true;
      queueMicrotask(() => use.focus());
      return h('div', null, webauthnSupported() ? '' : h('p', null, t('webauthn.unsupported')), use);
    },
    enroll,
    profile: async ({ factor }) => {
      ownFactor = factor;
      const box = h('div', { class: 'ew-subsection' });
      const draw = async () => {
        let list: Credential[];
        try { list = await api.get<Credential[]>(`${BASE}/credentials`); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
        const rows = list.map((c) => h('tr', null,
          h('th', { scope: 'row' }, c.name), h('td', null, c.passkey ? t('webauthn.kind_passkey') : t('webauthn.kind_key')),
          h('td', null, fmtDate(c.created_at, state.lang)), h('td', null, c.last_used_at ? fmtDate(c.last_used_at, state.lang) : t('webauthn.never')),
          h('td', null, button(t('webauthn.remove'), async () => {
            if (!(await confirmDialog(t('webauthn.remove_confirm', { name: c.name }), t('webauthn.remove'), true))) return;
            try { await api.del(`${BASE}/credentials/${c.id}`); toast(t('webauthn.removed')); await draw(); } catch (e) { toast(problem(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('webauthn.remove')}: ${c.name}` }))));
        const headers = [t('webauthn.name'), t('webauthn.kind'), t('webauthn.created'), t('webauthn.last_used'), t('common.actions')];
        box.replaceChildren(h('h3', null, t('webauthn.title')), h('p', null, factor === 1 ? t('webauthn.help_first') : t('webauthn.help')),
          h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('webauthn.title')),
            h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
            h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, t('webauthn.none')))]))),
          webauthnSupported()
            ? h('div', { class: 'ew-form-actions' }, button(t('webauthn.add'), () => enrollFactor('webauthn', () => void draw()), { variant: 'secondary' }))
            : h('p', null, t('webauthn.unsupported')));
      };
      await draw();
      return box;
    },
  });
}
