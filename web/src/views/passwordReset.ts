// "Forgot your password?" for local accounts: ask for the link, and set a
// new password with it. The answer never tells whether an address has an
// account; the link is valid for one hour and is used once.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { announce } from '../core/announce';
import { button, errorText, field, textInput } from '../components/ui';
import { setContext } from '../components/widgets';

export async function resetView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const heading = h('h1', null, t('reset.title'));
  if (!state.site.login?.password_reset) {
    mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', null, t('reset.closed')), h('p', null, h('a', { href: '/login' }, t('auth.login')))));
    return;
  }
  const email = textInput({ type: 'email', name: 'email', autocomplete: 'email', required: true, maxlength: 254 });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert', tabindex: '-1' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    field(t('email.address'), email, { required: true, help: t('reset.email_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('reset.submit'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    if (!email.value.trim()) { alert.textContent = t('reset.missing'); email.focus(); return; }
    try {
      await api.post('auth/password-reset', { email: email.value.trim() });
      const done = h('div', { class: 'ew-notice ew-notice--ok', role: 'status', tabindex: '-1' },
        h('p', null, t('reset.sent', { email: email.value.trim(), minutes: 60 })), h('p', null, t('reset.sent_hint')));
      mount(main, h('div', { class: 'ew-narrow' }, heading, done, h('p', null, h('a', { href: '/login' }, t('reset.back')))));
      done.focus();
      announce(t('reset.sent', { email: email.value.trim(), minutes: 60 }));
    } catch (e) {
      alert.textContent = errorText(e);
      alert.focus();
    }
  });
  mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', null, t('reset.intro')), form,
    h('p', null, h('a', { href: '/login' }, t('reset.back')))));
}

export async function resetConfirmView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  // the token leaves the address bar (history, screenshots); kept here
  const token = query.get('token') ?? '';
  history.replaceState(history.state, '', '/password-reset/confirm');
  const heading = h('h1', null, t('reset.new_title'));
  const pw = textInput({ type: 'password', name: 'new-password', autocomplete: 'new-password', required: true });
  const pw2 = textInput({ type: 'password', name: 'new-password-2', autocomplete: 'new-password', required: true });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert', tabindex: '-1' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    field(t('reset.new_password'), pw, { required: true, help: t('register.password_help') }),
    field(t('register.password_again'), pw2, { required: true }),
    h('div', { class: 'ew-form-actions' }, button(t('reset.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    if (!pw.value || !pw2.value) { alert.textContent = t('register.missing'); (pw.value ? pw2 : pw).focus(); return; }
    if (pw.value !== pw2.value) { alert.textContent = t('register.password_mismatch'); pw2.focus(); return; }
    try {
      await api.post('auth/password-reset/confirm', { token, password: pw.value });
      const done = h('div', { class: 'ew-notice ew-notice--ok', role: 'status', tabindex: '-1' }, h('p', null, t('reset.done')),
        h('p', null, h('a', { class: 'ew-btn ew-btn--primary', href: '/login?local=1' }, t('auth.login'))));
      mount(main, h('div', { class: 'ew-narrow' }, heading, done));
      done.focus();
    } catch (e) {
      alert.textContent = errorText(e);
      alert.focus();
    }
  });
  mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', null, t('reset.new_intro')), form,
    h('p', null, h('a', { href: '/password-reset' }, t('reset.again')))));
}
