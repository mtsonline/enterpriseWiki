// Factors of a sign-in in the browser. Each factor (an MFA plugin) brings
// the part of the form to answer it (render) and, if people set it up in
// the browser, the part to set it up (enroll) - used in the profile and
// during a sign-in that needs more factors than the person has.
import { api } from './api';
import { h } from './dom';
import { t } from './i18n';
import { button, errorText, field, openDialog, textInput } from '../components/ui';
import { toast } from './announce';

export interface MFAStepContext {
  /** Prepares the factor (sends the code, returns the WebAuthn options). */
  challenge: () => Promise<unknown>;
  /** Checks the answer; afterwards the sign-in goes on. */
  verify: (answer: Record<string, unknown>) => Promise<void>;
  /** Shows a problem in the step's alert region. */
  fail: (msg: string) => void;
}

export interface MFAEnrollContext {
  /** Starts the setup (new secret, code sent, WebAuthn options). */
  begin: (options?: Record<string, unknown>) => Promise<unknown>;
  /** Completes it with the answer. */
  finish: (answer: Record<string, unknown>) => Promise<void>;
  fail: (msg: string) => void;
}

export interface MFAMethodUI {
  id: string;
  label: () => string;
  /** The part of the sign-in form to answer this factor. */
  render: (ctx: MFAStepContext) => HTMLElement;
  /** The part of a form to set the factor up (absent: set up elsewhere). */
  enroll?: (ctx: MFAEnrollContext) => HTMLElement;
  /** The part of the profile (status, set up, remove); a subsection (h3). */
  profile?: (where: { factor: number }) => Promise<HTMLElement | null>;
}

const methods: MFAMethodUI[] = [];

/** Adds the browser part of a factor (plugins: see sdk). */
export function registerMFAMethodUI(m: MFAMethodUI): void {
  const i = methods.findIndex((x) => x.id === m.id);
  if (i >= 0) methods[i] = m;
  else methods.push(m);
}

export function mfaMethodUI(id: string): MFAMethodUI | undefined {
  return methods.find((x) => x.id === id);
}

/** A form for a code (authenticator app, e-mail). */
export function mfaCodeForm(onCode: (code: string) => Promise<void>, fail: (msg: string) => void, label: string, help: string, before?: HTMLElement, submit = t('auth.login')): HTMLElement {
  const code = textInput({ name: 'code', autocomplete: 'one-time-code', maxlength: 8, required: true });
  code.setAttribute('inputmode', 'numeric');
  const form = h('form', { class: 'ew-form', novalidate: true }, before ?? '', field(label, code, { required: true, help }),
    h('div', { class: 'ew-form-actions' }, button(submit, () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    if (!code.value.trim()) { fail(t('auth.totp_needed')); code.focus(); return; }
    try { await onCode(code.value.trim()); } catch (e) { fail(errorText(e)); code.value = ''; code.focus(); }
  });
  queueMicrotask(() => code.focus());
  return form;
}

/** Calls the endpoints of a pending sign-in. */
export function mfaApi(token: string) {
  const tok = token || undefined;
  return {
    info: () => api.post<MFAStep>('auth/mfa/info', { token: tok }),
    challenge: (method: string) => api.post<unknown>('auth/mfa/challenge', { token: tok, method }),
    verify: (method: string, answer: Record<string, unknown>) => api.post<MFAAnswer>('auth/mfa/verify', { ...answer, token: tok, method }),
    enrollBegin: (method: string, options: Record<string, unknown> = {}) => api.post<unknown>('auth/mfa/enroll/begin', { ...options, token: tok, method }),
    enrollFinish: (method: string, answer: Record<string, unknown>) => api.post<MFAAnswer>('auth/mfa/enroll/finish', { ...answer, token: tok, method }),
  };
}

export interface MFAStep { kind: 'verify' | 'enroll' | 'done'; factor: number; methods: string[]; passed: number; required: number }
export interface MFAAnswer { done: boolean; step?: MFAStep; user?: unknown; next?: string }

/** Sets up a factor in the profile (dialog); done runs afterwards. */
export function enrollFactor(id: string, done: () => void): void {
  const ui = mfaMethodUI(id);
  if (!ui?.enroll) return;
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const handle = openDialog(t('mfa.setup_title', { name: ui.label() }), h('div', null, alert, ui.enroll({
    begin: (options = {}) => api.post<unknown>(`me/mfa/${encodeURIComponent(id)}/begin`, options),
    finish: async (answer) => {
      await api.post(`me/mfa/${encodeURIComponent(id)}/finish`, answer);
      toast(t('mfa.setup_done', { name: ui.label() }));
      handle.close();
      setTimeout(done, 0);
    },
    fail: (msg) => { alert.textContent = msg; },
  })), []);
}
