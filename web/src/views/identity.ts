// E-mail addresses, linked sign-ins, confirmation links and invitations
// (profile, administration, /verify-email, /login?link=…).
import { api } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { announce, toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, select, textInput } from '../components/ui';
import { setContext } from '../components/widgets';

export interface Email { id: number; email: string; verified: boolean; primary: boolean; created_at: string; verified_at: string | null }
export interface Identity { provider: string; subject: string; username: string; primary: boolean; created_at: string; last_login_at: string | null }
export interface LinkInfo { provider: string; username: string; email: string }
export interface Session { id: number; kind: string; created_at: string; last_seen_at: string; expires_at: string; ip: string; user_agent: string; provider?: string; current: boolean }

/** The API addresses for the own account or (administrators) for user uid. */
interface Paths {
  emails: string; email: (id: number) => string; primary: (id: number) => string; resend: (id: number) => string; identities: string;
  sessions: string; session: (id: number) => string;
}
const myPaths: Paths = {
  emails: 'me/emails', email: (id) => `me/emails/${id}`, primary: (id) => `me/emails/${id}/primary`, resend: (id) => `me/emails/${id}/resend`, identities: 'me/identities',
  sessions: 'me/sessions', session: (id) => `me/sessions/${id}`,
};
const userPaths = (uid: number): Paths => ({
  emails: `admin/users/${uid}/emails`, email: (id) => `admin/users/${uid}/emails/${id}`, primary: (id) => `admin/users/${uid}/emails/${id}/primary`,
  resend: () => '', identities: `admin/users/${uid}/identities`,
  sessions: `admin/users/${uid}/sessions`, session: (id) => `admin/users/${uid}/sessions/${id}`,
});

function table(caption: string, headers: string[], rows: HTMLElement[], empty: string): HTMLElement {
  return h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, caption),
    h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, empty))])));
}

const status = (e: Email) => h('span', null,
  e.primary ? h('span', { class: 'ew-badge ew-badge--info' }, t('email.primary')) : null, ' ',
  e.verified ? h('span', { class: 'ew-badge ew-badge--ok' }, t('email.verified')) : h('span', { class: 'ew-badge ew-badge--warn' }, t('email.unverified')));

/** The address list with its actions; administrators can confirm addresses directly. */
async function emailsBlock(p: Paths, admin: boolean): Promise<HTMLElement> {
  const box = h('div');
  const render = async () => {
    let list: Email[];
    try { list = await api.get<Email[]>(p.emails); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    const act = async (fn: () => Promise<unknown>, msg: string) => {
      try { await fn(); toast(msg); await render(); } catch (e) { toast(errorText(e), 'error'); }
    };
    const rows = list.map((e) => h('tr', null,
      h('th', { scope: 'row' }, e.email), h('td', null, status(e)),
      h('td', { class: 'ew-actions' },
        e.verified && !e.primary ? button(t('email.make_primary'), () => void act(() => api.post(p.primary(e.id)), t('email.primary_set')),
          { variant: 'link', ariaLabel: `${t('email.make_primary')}: ${e.email}` }) : null,
        !e.verified && !admin ? button(t('email.resend'), () => void act(() => api.post(p.resend(e.id)), t('email.sent', { email: e.email })),
          { variant: 'link', ariaLabel: `${t('email.resend')}: ${e.email}` }) : null,
        !e.primary ? button(t('common.remove'), async () => {
          if (!(await confirmDialog(t('email.remove_confirm', { email: e.email }), t('common.remove'), true))) return;
          await act(() => api.del(p.email(e.id)), t('email.removed'));
        }, { variant: 'link', ariaLabel: `${t('common.remove')}: ${e.email}` }) : null)));
    const addr = textInput({ type: 'email', autocomplete: 'email', required: true });
    const confirmNow = checkbox(t('email.confirm_now'), true, { help: t('email.confirm_now_help') });
    const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
    const form = h('form', { class: 'ew-form ew-inline-form', novalidate: true }, alert,
      field(t('email.add_label'), addr, { required: true, help: admin ? undefined : t('email.add_help') }), admin ? confirmNow.el : null,
      button(t('email.add'), () => undefined, { type: 'submit', variant: 'secondary' }));
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      alert.textContent = '';
      if (!addr.value.trim()) { alert.textContent = t('email.add_missing'); addr.focus(); return; }
      try {
        await api.post(p.emails, admin ? { email: addr.value.trim(), verified: confirmNow.input.checked } : { email: addr.value.trim() });
        toast(admin && confirmNow.input.checked ? t('email.added') : t('email.sent', { email: addr.value.trim() }));
        await render();
      } catch (e) { alert.textContent = errorText(e); addr.focus(); }
    });
    box.replaceChildren(table(t('email.title'), [t('email.address'), t('common.status'), t('common.actions')], rows, t('email.none')), form);
  };
  await render();
  return box;
}

async function identitiesBlock(p: Paths): Promise<HTMLElement> {
  const box = h('div');
  const render = async () => {
    let list: Identity[];
    try { list = await api.get<Identity[]>(p.identities); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    box.replaceChildren(table(t('identity.title'), [t('identity.provider'), t('auth.username'), t('admin.last_login'), t('common.actions')],
      list.map((i) => h('tr', null,
        h('th', { scope: 'row' }, providerName(i.provider), i.primary ? h('span', { class: 'ew-badge ew-badge--info' }, ' ', t('identity.primary')) : null),
        h('td', null, i.username), h('td', null, i.last_login_at ? fmtDate(i.last_login_at, state.lang) : '–'),
        h('td', null, i.primary ? null : button(t('identity.unlink'), async () => {
          if (!(await confirmDialog(t('identity.unlink_confirm', { provider: providerName(i.provider), name: i.username }), t('identity.unlink'), true))) return;
          try {
            await api.del(`${p.identities}?provider=${encodeURIComponent(i.provider)}&subject=${encodeURIComponent(i.subject)}`);
            toast(t('identity.unlinked'));
            await render();
          } catch (e) { toast(errorText(e), 'error'); }
        }, { variant: 'link', ariaLabel: `${t('identity.unlink')}: ${providerName(i.provider)} ${i.username}` })))), '–'));
  };
  await render();
  return box;
}

/** A short description of a browser ("Firefox, Linux") from its user agent. */
export function deviceLabel(ua: string, kind: string): string {
  if (kind === 'ssh') return t('session.ssh');
  const browser = /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome'
    : /Safari\//.test(ua) ? 'Safari' : /^ewiki|curl|python|Go-http/i.test(ua) ? t('session.program') : t('session.browser');
  const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Windows/.test(ua) ? 'Windows' : /Mac OS X|Macintosh/.test(ua) ? 'macOS'
    : /Linux/.test(ua) ? 'Linux' : '';
  return os ? `${browser}, ${os}` : browser;
}

/** Sessions of the own account (sign out elsewhere) or of a user (administrators). */
async function sessionsBlock(p: Paths, admin: boolean): Promise<HTMLElement> {
  const box = h('div');
  const render = async () => {
    let list: Session[];
    try { list = await api.get<Session[]>(p.sessions); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    const end = async (s: Session) => {
      const label = deviceLabel(s.user_agent, s.kind);
      if (!(await confirmDialog(t('session.end_confirm', { device: label }), t('session.end'), true))) return;
      try { await api.del(p.session(s.id)); toast(t('session.ended')); await render(); } catch (e) { toast(errorText(e), 'error'); }
    };
    const rows = list.map((s) => h('tr', null,
      h('th', { scope: 'row' }, deviceLabel(s.user_agent, s.kind), s.current ? h('span', { class: 'ew-badge ew-badge--info' }, ' ', t('session.current')) : null,
        s.user_agent ? [h('br'), h('small', { class: 'ew-muted', title: s.user_agent }, s.user_agent.slice(0, 80))] : null),
      h('td', null, s.ip || '–'),
      h('td', null, s.provider ? providerName(s.provider) : t('identity.local')),
      h('td', null, fmtDate(s.created_at, state.lang)),
      h('td', null, fmtDate(s.last_seen_at, state.lang)),
      h('td', null, s.current ? null : button(t('session.end'), () => void end(s),
        { variant: 'link', ariaLabel: `${t('session.end')}: ${deviceLabel(s.user_agent, s.kind)}, ${fmtDate(s.last_seen_at, state.lang)}` }))));
    const others = list.filter((s) => !s.current).length;
    const all = others ? button(admin ? t('session.end_all') : t('session.end_others'), async () => {
      if (!(await confirmDialog(admin ? t('session.end_all_confirm') : t('session.end_others_confirm'), admin ? t('session.end_all') : t('session.end_others'), true))) return;
      try { await api.del(p.sessions); toast(t('session.ended_all')); await render(); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'secondary' }) : null;
    box.replaceChildren(table(t('session.title'), [t('session.device'), t('session.ip'), t('identity.provider'), t('session.started'), t('session.last_seen'), t('common.actions')],
      rows, t('session.none')), ...(all ? [all] : []));
  };
  await render();
  return box;
}

function providerName(id: string): string {
  return state.site.auth_providers?.find((p) => p.id === id)?.name ?? (id === 'local' ? t('identity.local') : id === 'invite' ? t('identity.invite') : id);
}

/** Profile: own addresses and sign-ins. */
export async function profileIdentitySections(): Promise<HTMLElement[]> {
  return [
    h('section', { class: 'ew-section', 'aria-labelledby': 'em-h' }, h('h2', { id: 'em-h' }, t('email.title')),
      h('p', { class: 'ew-help' }, t('email.help')), await emailsBlock(myPaths, false)),
    h('section', { class: 'ew-section', 'aria-labelledby': 'id-h' }, h('h2', { id: 'id-h' }, t('identity.title')),
      h('p', { class: 'ew-help' }, t('identity.help')), await identitiesBlock(myPaths)),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ses-h' }, h('h2', { id: 'ses-h' }, t('session.title')),
      h('p', { class: 'ew-help' }, t('session.help')), await sessionsBlock(myPaths, false)),
  ];
}

/** Administration: addresses and sign-ins of a user. */
export async function adminIdentityDialog(uid: number, name: string): Promise<void> {
  const p = userPaths(uid);
  openDialog(`${t('identity.admin_title')}: ${name}`, h('div', null,
    h('h3', null, t('email.title')), await emailsBlock(p, true),
    h('h3', null, t('identity.title')), await identitiesBlock(p),
    h('h3', null, t('session.title')), await sessionsBlock(p, true)), [], { wide: true });
}

/** Administration: invite somebody by e-mail address. */
export function inviteDialog(groups: { id: number; name: string }[], roles: { id: number; name: string }[], done: () => void): void {
  const addr = textInput({ type: 'email', required: true });
  const name = textInput({});
  const lang = select([{ value: '', label: t('profile.lang_auto') }, ...state.site.languages.map((l) => ({ value: l, label: l }))]);
  const gs = groups.map((g) => ({ id: g.id, cb: checkbox(g.name) }));
  const rs = roles.map((r) => ({ id: r.id, cb: checkbox(r.name) }));
  formDialog(t('invite.title'), [h('p', { class: 'ew-help' }, t('invite.help')),
    field(t('email.address'), addr, { required: true }), field(t('profile.display_name'), name), field(t('profile.language'), lang),
    h('fieldset', null, h('legend', null, t('admin.groups')), ...gs.map((x) => x.cb.el)),
    h('fieldset', null, h('legend', null, t('admin.roles')), ...rs.map((x) => x.cb.el))], t('invite.submit'), async () => {
    await api.post('admin/users/invite', { email: addr.value.trim(), display_name: name.value.trim(), language: lang.value,
      groups: gs.filter((x) => x.cb.input.checked).map((x) => x.id), roles: rs.filter((x) => x.cb.input.checked).map((x) => x.id) });
    toast(t('invite.done', { email: addr.value.trim() }));
    done();
  }, { wide: true });
}

/** /verify-email?token=…: confirms an address (also without signing in). */
export async function verifyEmailView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const token = query.get('token') ?? '';
  const out = h('div', { role: 'status' }, t('email.verifying'));
  mount(main, h('div', { class: 'ew-narrow' }, h('h1', null, t('email.verify_title')), out));
  // the token leaves the address bar (history, screenshots)
  history.replaceState(history.state, '', '/verify-email');
  try {
    const res = await api.post<{ email: string }>('auth/verify-email', { token });
    out.replaceChildren(h('p', { class: 'ew-notice ew-notice--ok' }, t('email.verified_msg', { email: res.email })),
      h('p', null, h('a', { href: state.user ? '/profile' : '/login' }, state.user ? t('profile.title') : t('auth.login'))));
  } catch (e) {
    out.replaceChildren(h('p', { class: 'ew-notice ew-notice--error' }, errorText(e)));
  }
}

/**
 * Login page with ?link=TOKEN: explains the waiting sign-in; after signing
 * in with the existing account the link is confirmed.
 */
export async function linkNotice(token: string): Promise<{ el: HTMLElement; confirm: () => Promise<void> } | null> {
  let info: LinkInfo;
  try {
    info = await api.post<LinkInfo>('auth/link/info', { token });
  } catch (e) {
    return { el: h('p', { class: 'ew-notice ew-notice--error', role: 'alert' }, errorText(e)), confirm: async () => undefined };
  }
  const el = h('div', { class: 'ew-notice ew-notice--info' },
    h('p', null, t('identity.link_intro', { provider: providerName(info.provider), name: info.username, email: info.email })),
    h('p', null, t('identity.link_how')));
  return {
    el,
    confirm: async () => {
      try {
        await api.post('me/identities/link', { token });
        announce(t('identity.linked', { provider: providerName(info.provider) }));
        toast(t('identity.linked', { provider: providerName(info.provider) }));
      } catch (e) {
        toast(errorText(e), 'error');
      }
    },
  };
}

/** After a password sign-in that needs a link (e.g. LDAP): continue on the login page. */
export function goLink(token: string, next: string): void {
  navigate(`/login?link=${encodeURIComponent(token)}&next=${encodeURIComponent(next)}`);
}
