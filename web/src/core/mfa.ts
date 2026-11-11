// The second step of a sign-in: the person proves a second factor. Each
// factor (MFA method) has its own part of the form; built in are the
// authenticator app and codes by e-mail, plugins add more (security keys).
import { api } from './api';
import { h } from './dom';
import { t } from './i18n';
import { button, errorText, field, textInput } from '../components/ui';

export interface MFAStepContext {
  /** Prepares the factor (sends the code, returns the WebAuthn options). */
  challenge: () => Promise<unknown>;
  /** Checks the answer; on success the sign-in is complete. */
  verify: (answer: Record<string, unknown>) => Promise<void>;
  /** Shows a problem in the step's alert region. */
  fail: (msg: string) => void;
}

export interface MFAMethodUI {
  id: string;
  label: () => string;
  /** Renders the part of the form for this factor. */
  render: (ctx: MFAStepContext) => HTMLElement;
}

const methods: MFAMethodUI[] = [];

/** Adds the sign-in step of a second factor (plugins: see sdk). */
export function registerMFAMethodUI(m: MFAMethodUI): void {
  const i = methods.findIndex((x) => x.id === m.id);
  if (i >= 0) methods[i] = m;
  else methods.push(m);
}

export function mfaMethodUI(id: string): MFAMethodUI | undefined {
  return methods.find((x) => x.id === id);
}

/** A form for a code (authenticator app or e-mail). */
function codeForm(ctx: MFAStepContext, label: string, help: string, before?: HTMLElement): HTMLElement {
  const code = textInput({ name: 'code', autocomplete: 'one-time-code', maxlength: 8, required: true });
  code.setAttribute('inputmode', 'numeric');
  const form = h('form', { class: 'ew-form', novalidate: true }, before ?? '', field(label, code, { required: true, help }),
    h('div', { class: 'ew-form-actions' }, button(t('auth.login'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    if (!code.value.trim()) { ctx.fail(t('auth.totp_needed')); code.focus(); return; }
    try { await ctx.verify({ code: code.value.trim() }); } catch (e) { ctx.fail(errorText(e)); code.value = ''; code.focus(); }
  });
  queueMicrotask(() => code.focus());
  return form;
}

registerMFAMethodUI({
  id: 'totp', label: () => t('mfa.method.totp'),
  render: (ctx) => codeForm(ctx, t('auth.totp'), t('auth.totp_help')),
});

registerMFAMethodUI({
  id: 'email', label: () => t('mfa.method.email'),
  render: (ctx) => {
    const status = h('p', { role: 'status' });
    const box = h('div');
    const send = button(t('auth.mfa_email_send'), async () => {
      try {
        const r = await ctx.challenge() as { address?: string } | null;
        status.textContent = t('mfa.code_sent_to', { address: r?.address ?? '' });
        box.replaceChildren(codeForm(ctx, t('mfa.code'), t('mfa.code_help')));
      } catch (e) { ctx.fail(errorText(e)); }
    }, { variant: 'primary' });
    box.append(send);
    return h('div', null, status, box);
  },
});

/** Calls the second-factor endpoints of a pending sign-in. */
export function mfaApi(token: string) {
  return {
    info: () => api.post<{ methods: string[] }>('auth/mfa/info', { token: token || undefined }),
    challenge: (method: string) => api.post<unknown>('auth/mfa/challenge', { token: token || undefined, method }),
    verify: <T>(method: string, answer: Record<string, unknown>) => api.post<T>('auth/mfa/verify', { ...answer, token: token || undefined, method }),
  };
}
