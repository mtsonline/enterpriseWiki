// Security keys and passkeys: the sign-in step and the profile section.
import {
  api, ApiError, button, confirmDialog, errorText, field, fmtDate, formDialog, h, registerMFAMethodUI, registerProfileSection, state, t, textInput, toast,
  webauthnCancelled, webauthnCreate, webauthnGet, webauthnSupported,
} from '@ewiki/sdk';

interface Credential { id: number; name: string; passkey: boolean; created_at: string; last_used_at: string | null }

const BASE = '/api/v1/x/mfa.webauthn';

const problem = (e: unknown): string =>
  webauthnCancelled(e) ? t('webauthn.cancelled') : e instanceof ApiError && e.code === 'reauth_required' ? t('webauthn.reauth') : errorText(e);

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
  });

  registerProfileSection({
    id: 'webauthn', order: 80,
    render: async () => {
      const box = h('section', { class: 'ew-section', 'aria-labelledby': 'wa-h' });
      const draw = async () => {
        let list: Credential[];
        try { list = await api.get<Credential[]>(`${BASE}/credentials`); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
        const add = (passkey: boolean) => () => {
          const name = textInput({ required: true, maxlength: 100 });
          formDialog(passkey ? t('webauthn.add_passkey') : t('webauthn.add_key'), field(t('webauthn.name'), name, { required: true, help: t('webauthn.name_help') }),
            passkey ? t('webauthn.add_passkey') : t('webauthn.add_key'), async () => {
              try {
                const options = await api.post<Record<string, unknown>>(`${BASE}/register/begin`, { passkey });
                const credential = await webauthnCreate(options);
                await api.post(`${BASE}/register/finish`, { name: name.value.trim(), credential });
              } catch (e) { throw new Error(problem(e)); }
              toast(t('webauthn.added', { name: name.value.trim() }));
              setTimeout(() => void draw(), 0);
            });
        };
        const rows = list.map((c) => h('tr', null,
          h('th', { scope: 'row' }, c.name), h('td', null, c.passkey ? t('webauthn.kind_passkey') : t('webauthn.kind_key')),
          h('td', null, fmtDate(c.created_at, state.lang)), h('td', null, c.last_used_at ? fmtDate(c.last_used_at, state.lang) : t('webauthn.never')),
          h('td', null, button(t('webauthn.remove'), async () => {
            if (!(await confirmDialog(t('webauthn.remove_confirm', { name: c.name }), t('webauthn.remove'), true))) return;
            try { await api.del(`${BASE}/credentials/${c.id}`); toast(t('webauthn.removed')); await draw(); } catch (e) { toast(problem(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('webauthn.remove')}: ${c.name}` }))));
        const headers = [t('webauthn.name'), t('webauthn.kind'), t('webauthn.created'), t('webauthn.last_used'), t('common.actions')];
        box.replaceChildren(h('h2', { id: 'wa-h' }, t('webauthn.title')), h('p', null, t('webauthn.help')),
          h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('webauthn.title')),
            h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
            h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, t('webauthn.none')))]))),
          webauthnSupported()
            ? h('div', { class: 'ew-form-actions' }, button(t('webauthn.add_key'), add(false), { variant: 'secondary' }), button(t('webauthn.add_passkey'), add(true), { variant: 'secondary' }))
            : h('p', null, t('webauthn.unsupported')));
      };
      await draw();
      return box;
    },
  });
}
