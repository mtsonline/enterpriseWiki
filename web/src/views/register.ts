// Registration of a local account by the person themselves (when the
// administrators switched it on): the form, and the page of the
// confirmation link. The account exists only after the link was used.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { announce } from '../core/announce';
import { button, errorText, field, textInput } from '../components/ui';
import { setContext } from '../components/widgets';

export async function registerView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const heading = h('h1', null, t('register.title', { site: state.site.name }));
  if (!state.site.login?.registration) {
    mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', null, t('register.closed')), h('p', null, h('a', { href: '/login' }, t('auth.login')))));
    return;
  }
  const email = textInput({ type: 'email', name: 'email', autocomplete: 'email', required: true, maxlength: 254 });
  const name = textInput({ name: 'name', autocomplete: 'name', required: true, maxlength: 100 });
  const pw = textInput({ type: 'password', name: 'new-password', autocomplete: 'new-password', required: true });
  const pw2 = textInput({ type: 'password', name: 'new-password-2', autocomplete: 'new-password', required: true });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert', tabindex: '-1' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    field(t('email.address'), email, { required: true, help: t('register.email_help') }),
    field(t('register.name'), name, { required: true }),
    field(t('auth.password'), pw, { required: true, help: t('register.password_help') }),
    field(t('register.password_again'), pw2, { required: true }),
    h('div', { class: 'ew-form-actions' }, button(t('register.submit'), () => undefined, { type: 'submit', variant: 'primary' })));
  const wrap = h('div', { class: 'ew-narrow' }, heading, h('p', null, t('register.intro')), form,
    h('p', null, h('a', { href: '/login' }, t('register.have_account'))));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const missing = [email, name, pw, pw2].find((x) => !x.value.trim());
    if (missing) { alert.textContent = t('register.missing'); missing.focus(); return; }
    if (pw.value !== pw2.value) { alert.textContent = t('register.password_mismatch'); pw2.focus(); return; }
    try {
      await api.post('auth/register', { email: email.value.trim(), display_name: name.value.trim(), password: pw.value, language: state.lang });
      const done = h('div', { class: 'ew-notice ew-notice--ok', role: 'status', tabindex: '-1' },
        h('p', null, t('register.sent', { email: email.value.trim(), hours: 48 })), h('p', null, t('register.sent_spam')));
      mount(main, h('div', { class: 'ew-narrow' }, heading, done));
      done.focus();
      announce(t('register.sent', { email: email.value.trim(), hours: 48 }));
    } catch (e) {
      alert.textContent = errorText(e);
      alert.focus();
    }
  });
  mount(main, wrap);
}

export async function registerConfirmView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const token = query.get('token') ?? '';
  const out = h('div', { role: 'status' }, t('register.confirming'));
  mount(main, h('div', { class: 'ew-narrow' }, h('h1', null, t('register.confirm_title')), out));
  // the token leaves the address bar (history, screenshots)
  history.replaceState(history.state, '', '/register/confirm');
  try {
    const res = await api.post<{ username: string; email: string }>('auth/register/confirm', { token });
    out.replaceChildren(h('p', { class: 'ew-notice ew-notice--ok' }, t('register.created', { email: res.email })),
      h('p', null, h('a', { class: 'ew-btn ew-btn--primary', href: '/login?local=1' }, t('auth.login'))));
  } catch (e) {
    out.replaceChildren(h('p', { class: 'ew-notice ew-notice--error' }, errorText(e)),
      h('p', null, h('a', { href: '/register' }, t('register.again'))));
  }
}
