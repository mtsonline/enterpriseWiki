// Codes by e-mail: the sign-in step, the setup and the profile part.
import { api, button, confirmDialog, enrollFactor, errorText, h, mfaCodeForm, registerMFAMethodUI, t, toast } from '@ewiki/sdk';
import type { MFAEnrollContext } from '@ewiki/sdk';

const BASE = '/api/v1/x/mfa.email';

/** A "send code" button; afterwards the form for the code. */
function codeStep(send: () => Promise<unknown>, onCode: (code: string) => Promise<void>, fail: (msg: string) => void, submit?: string): HTMLElement {
  const box = h('div', { 'aria-live': 'polite' });
  const status = h('p');
  const start = async () => {
    try {
      const res = await send() as { address?: string } | null;
      status.textContent = t('memail.sent', { address: res?.address ?? '' });
      box.replaceChildren(status, mfaCodeForm(onCode, fail, t('memail.code'), t('memail.code_help'), undefined, submit),
        button(t('memail.send'), () => void start(), { variant: 'link' }));
    } catch (e) { fail(errorText(e)); }
  };
  const first = button(t('memail.send'), () => void start(), { variant: 'primary' });
  box.append(first);
  queueMicrotask(() => first.focus());
  return box;
}

const enroll = (ctx: MFAEnrollContext): HTMLElement =>
  codeStep(() => ctx.begin(), (code) => ctx.finish({ code }), ctx.fail, t('memail.enable'));

export function register(): void {
  registerMFAMethodUI({
    id: 'email', label: () => t('mfa.method.email'),
    render: (ctx) => codeStep(ctx.challenge, (code) => ctx.verify({ code }), ctx.fail),
    enroll,
    profile: async () => {
      const box = h('div', { class: 'ew-subsection' });
      const draw = async () => {
        let st: { enabled: boolean; address: string; mail: boolean };
        try { st = await api.get(`${BASE}/me`); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
        const parts: (HTMLElement | string)[] = [h('h3', null, t('memail.title'))];
        if (st.enabled) {
          parts.push(h('p', null, t('memail.help', { address: st.address })), h('p', null, h('strong', null, t('memail.on')), ' ', button(t('memail.disable'), async () => {
            if (!(await confirmDialog(t('memail.disable_confirm'), t('memail.disable'), true))) return;
            try { await api.del(`${BASE}/me`); toast(t('memail.disabled')); await draw(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link' })));
        } else if (!st.mail) parts.push(h('p', null, t('memail.no_mail')));
        else if (!st.address) parts.push(h('p', null, t('memail.no_address')));
        else parts.push(h('p', null, t('memail.help', { address: st.address })), button(t('memail.enable'), () => enrollFactor('email', () => void draw()), { variant: 'secondary' }));
        box.replaceChildren(...parts);
      };
      await draw();
      return box;
    },
  });
}
