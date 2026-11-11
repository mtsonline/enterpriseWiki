// Authenticator app: the sign-in step, the setup and the profile part.
import { api, button, confirmDialog, enrollFactor, errorText, h, mfaCodeForm, registerMFAMethodUI, t, toast } from '@ewiki/sdk';
import type { MFAEnrollContext } from '@ewiki/sdk';

const BASE = '/api/v1/x/mfa.totp';

function enroll(ctx: MFAEnrollContext): HTMLElement {
  const box = h('div', { 'aria-live': 'polite' }, h('p', null, t('common.loading')));
  void (async () => {
    try {
      const st = await ctx.begin() as { secret: string; uri: string };
      // the link only opens an app (otpauth:), no address of a web page
      const link = h('a', { href: st.uri }, t('totp.open_app'));
      box.replaceChildren(
        h('ol', null,
          h('li', null, t('totp.step1')),
          h('li', null, t('totp.step2'), ' ', h('code', { class: 'ew-secret', translate: 'no' }, st.secret.replace(/(.{4})/g, '$1 ').trim()), ' (', link, ')'),
          h('li', null, t('totp.step3'))),
        mfaCodeForm((code) => ctx.finish({ code }), ctx.fail, t('totp.code'), t('totp.code_help'), undefined, t('totp.setup')));
    } catch (e) { box.replaceChildren(); ctx.fail(errorText(e)); }
  })();
  return box;
}

export function register(): void {
  registerMFAMethodUI({
    id: 'totp', label: () => t('mfa.method.totp'),
    render: (ctx) => mfaCodeForm((code) => ctx.verify({ code }), ctx.fail, t('totp.code'), t('totp.code_help')),
    enroll,
    profile: async () => {
      const box = h('div', { class: 'ew-subsection' });
      const draw = async () => {
        let on = false;
        try { on = (await api.get<{ enabled: boolean }>(`${BASE}/me`)).enabled; } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
        box.replaceChildren(h('h3', null, t('totp.title')), h('p', null, t('totp.help')),
          on
            ? h('p', null, h('strong', null, t('totp.on')), ' ', button(t('totp.remove'), async () => {
              if (!(await confirmDialog(t('totp.remove_confirm'), t('totp.remove'), true))) return;
              try { await api.del(`${BASE}/me`); toast(t('totp.removed')); await draw(); } catch (e) { toast(errorText(e), 'error'); }
            }, { variant: 'link' }))
            : button(t('totp.setup'), () => enrollFactor('totp', () => void draw()), { variant: 'secondary' }));
      };
      await draw();
      return box;
    },
  });
}
