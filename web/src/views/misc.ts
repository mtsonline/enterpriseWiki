// Search, login, profile and trash.
import { renderProfileSections } from '../sdk';
import { assignedWatchesSection } from './adminWatches';
import { api, ApiError, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { loadMessages, pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { changed, isAdmin, hasPerm, state } from '../core/state';
import { PERMS, type Principal } from '../core/types';
import { announce, toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, pageHeader, select, textArea, textInput } from '../components/ui';
import { invalidateTree, langName, setContext } from '../components/widgets';
import { typeLabel } from '../sdk';
import { categoryView, elementView, errorPage, notFoundPage } from './content';
import { watchDialog, type Watch } from '../components/watch';
import { feedTokenSection } from '../components/feeds';
import { profileCardForm } from '../components/profilecard';
import { goLink, linkNotice, profileIdentitySections } from './identity';
import { mfaApi, mfaMethodUI, type MFAAnswer, type MFAStep } from '../core/mfa';
import { webauthnCancelled, webauthnGet, webauthnSupported } from '../core/webauthn';

// ---------------------------------------------------------------- search

interface SearchResult {
  elements: { element_id: number; category_id: number; lang: string; type: string; title: string; snippet: string; tags: string[] }[];
  categories: { id: number; titles: Record<string, string>; tags: string[] }[];
}

/** Converts the server snippet (HTML-escaped text with <mark>) into nodes without innerHTML. */
function snippetNodes(s: string): Node[] {
  const out: Node[] = [];
  const parts = s.split(/(<mark>|<\/mark>)/);
  let marked = false;
  const txt = document.createElement('textarea');
  for (const p of parts) {
    if (p === '<mark>') { marked = true; continue; }
    if (p === '</mark>') { marked = false; continue; }
    txt.innerHTML = p; // decode entities only (textarea does not parse markup)
    const text = txt.value;
    out.push(marked ? h('mark', null, text) : document.createTextNode(text));
  }
  return out;
}

export async function searchView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const q = query.get('q') ?? '';
  const tags = query.get('tags') ?? '';
  const input = textInput({ value: q, type: 'search', name: 'q' });
  const tagInput = textInput({ value: tags, name: 'tags' });
  const form = h('form', { role: 'search', class: 'ew-form ew-search-form', 'aria-label': t('search.label') },
    field(t('search.term'), input), field(t('common.tags'), tagInput, { help: t('search.tags_help') }),
    button(t('search.submit'), () => undefined, { type: 'submit', variant: 'primary' }));
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    navigate(`/search${qs({ q: input.value.trim(), tags: tagInput.value.trim() })}`);
  });
  const results = h('div', { class: 'ew-results' });
  mount(main, pageHeader(t('search.title')), form, results);
  if (!q && !tags) {
    try {
      const cloud = await api.get<{ tag: string; count: number }[]>('tags');
      if (cloud.length) results.append(h('h2', null, t('search.popular_tags')), h('ul', { class: 'ew-tags' }, ...cloud.map((c) => h('li', null, h('a', { class: 'ew-tag', href: `/search?tags=${encodeURIComponent(c.tag)}` }, `#${c.tag} (${c.count})`)))));
    } catch { /* ignore */ }
    return;
  }
  try {
    const res = await api.get<SearchResult>(`search${qs({ q, tags, lang: state.lang })}`);
    const total = res.elements.length + res.categories.length;
    results.append(h('p', { role: 'status' }, t('search.count', { count: total })));
    if (res.categories.length) {
      results.append(h('h2', null, t('search.categories')), h('ul', { class: 'ew-list' }, ...res.categories.map((c) => h('li', null, h('a', { href: `/c/${c.id}` }, pick(c.titles, state.lang, state.site.default_language))))));
    }
    if (res.elements.length) {
      results.append(h('h2', null, t('search.pages')), h('ol', { class: 'ew-result-list' }, ...res.elements.map((r) => h('li', { class: 'ew-result' },
        h('h3', null, h('a', { href: `/e/${r.element_id}` }, r.title)),
        h('p', { class: 'ew-snippet', lang: r.lang }, ...snippetNodes(r.snippet)),
        h('p', { class: 'ew-meta' }, typeLabel(r.type), ' · ', langName(r.lang), r.tags.length ? ` · ${r.tags.map((x) => '#' + x).join(' ')}` : '')))));
    }
    announce(t('search.count', { count: total }));
  } catch (e) {
    results.append(h('p', { role: 'alert' }, errorText(e)));
  }
}

// ---------------------------------------------------------------- login

/** Returns path if it stays on this site (same rules as the server's SafeNext), otherwise "/". */
export function safeLocalPath(path: string): string {
  // browsers drop tabs/line breaks and treat "\\" like "/": "/\t/evil.example" would leave the site
  // eslint-disable-next-line no-control-regex
  if (!path.startsWith('/') || path.startsWith('//') || path.length > 2048 || /[\u0000-\u0020\u007f-\u009f\\\u2028\u2029]/.test(path)) return '/';
  return path;
}

export async function loginView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const next = query.get('next') ?? '/';
  const safeNext = safeLocalPath(next);
  if (state.user) {
    navigate(safeNext, { replace: true });
    return;
  }
  let providers = state.site.auth_providers;
  try { providers = await api.get<{ id: string; name: string; kind: string }[]>('auth/providers'); } catch { /* use site info */ }
  const kind = (p: { id: string }) => (p as { kind?: string }).kind ?? 'password';
  const pwProviders = providers.filter((p) => kind(p) === 'password');
  const sso = providers.filter((p) => kind(p) === 'redirect');
  const passkey = providers.some((p) => kind(p) === 'passwordless' && p.id === 'webauthn') && webauthnSupported();
  const user = textInput({ name: 'username', autocomplete: 'username', required: true });
  const pw = textInput({ name: 'password', type: 'password', autocomplete: 'current-password', required: true });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' }, query.get('error') ?? '');
  // a sign-in waits to be linked to the account the person signs in with now
  const linkToken = query.get('link') ?? '';
  const link = linkToken ? await linkNotice(linkToken) : null;
  const form = h('form', { class: 'ew-form ew-login', novalidate: true },
    alert,
    field(t('auth.username'), user, { required: true, help: pwProviders.length > 1 ? t('auth.username_help', { providers: pwProviders.map((p) => p.id).join(', ') }) : undefined }),
    field(t('auth.password'), pw, { required: true }),
    h('div', { class: 'ew-form-actions' }, button(t('auth.login'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    if (!user.value.trim() || !pw.value) {
      alert.textContent = t('auth.missing');
      (user.value.trim() ? pw : user).focus();
      return;
    }
    try {
      const res = await api.post<{ user: Principal }>('auth/login', { username: user.value.trim(), password: pw.value });
      await signedIn(res.user, safeNext, link);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'mfa_required' && e.detail) {
        // factor 1 was right: now the further factors
        await mfaStep(main, e.detail, safeNext, link);
        return;
      }
      if (e instanceof ApiError && e.code === 'link_required' && e.detail) {
        // e.g. LDAP: the address belongs to an existing account
        goLink(e.detail, safeNext);
        return;
      }
      alert.textContent = errorText(e);
      pw.value = '';
      pw.focus();
    }
  });
  const ssoBox = sso.length ? h('section', { class: 'ew-sso', 'aria-labelledby': 'sso-h' }, h('h2', { id: 'sso-h' }, t('auth.sso_title')),
    h('ul', { class: 'ew-sso-list' }, ...sso.map((p) => h('li', null,
      h('a', { class: 'ew-btn ew-btn--secondary ew-sso-btn', href: `/api/v1/auth/sso/${encodeURIComponent(p.id)}/start?next=${encodeURIComponent(safeNext)}` }, t('auth.sso_with', { name: p.name })))))) : null;
  const passkeyBox = passkey ? h('section', { class: 'ew-sso', 'aria-labelledby': 'pk-h' }, h('h2', { id: 'pk-h', class: 'sr-only' }, t('auth.passkey_title')),
    button(t('auth.passkey_login'), async () => {
      alert.textContent = '';
      try {
        const b = await api.post<{ token: string; options: Record<string, unknown> }>('auth/passwordless/webauthn/begin');
        const credential = await webauthnGet(b.options);
        const res = await api.post<{ user: Principal }>('auth/passwordless/webauthn/finish', { token: b.token, credential });
        await signedIn(res.user, safeNext, link);
      } catch (e) {
        // the passkey was right: the account's further factors follow
        if (e instanceof ApiError && e.code === 'mfa_required' && e.detail) { await mfaStep(main, e.detail, safeNext, link); return; }
        alert.textContent = webauthnCancelled(e) ? t('auth.passkey_cancelled') : errorText(e);
      }
    }, { variant: 'secondary' })) : null;
  mount(main, h('div', { class: 'ew-narrow' }, h('h1', null, t('auth.login_title', { site: state.site.name })),
    query.get('logged_out') ? h('p', { class: 'ew-notice ew-notice--info', role: 'status' }, t('auth.logged_out')) : null,
    link?.el ?? null, ssoBox, passkeyBox, sso.length || passkeyBox ? h('h2', { class: 'ew-login-local' }, t('auth.local_title')) : null, form,
    h('p', { class: 'ew-help' }, t('auth.ssh_hint'))));
  if (query.get('error')) alert.focus?.();
  // single sign-on that waits for the second factor (token in a cookie)
  if (query.get('mfa')) await mfaStep(main, '', safeNext, link);
}

/** After a complete sign-in: user state, language, announcement, next page. */
async function signedIn(u: Principal, next: string, link: { confirm: () => Promise<void> } | null): Promise<void> {
  state.user = u;
  void api.get<{ manages_categories?: boolean }>('auth/me').then((m) => { state.managesCategories = !!m.manages_categories; changed(); }).catch(() => undefined);
  if (u.language && state.site.languages.includes(u.language) && u.language !== state.lang) {
    state.lang = u.language;
    await loadMessages(state.lang);
  }
  invalidateTree();
  changed();
  announce(t('auth.logged_in', { name: u.display_name || u.username }));
  if (link) await link.confirm();
  navigate(next);
}

/**
 * The further factors of a sign-in, one step after the other: answer one of
 * the factors of the step (alternatives), or set one up when the sign-in
 * needs more factors than the person has.
 */
async function mfaStep(main: HTMLElement, token: string, next: string, link: { confirm: () => Promise<void> } | null, step?: MFAStep): Promise<void> {
  const mfa = mfaApi(token);
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const heading = h('h1', { tabindex: '-1' }, t('mfa.step_title'));
  try {
    step ??= await mfa.info();
  } catch (e) {
    mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', { class: 'ew-notice ew-notice--error', role: 'alert' }, errorText(e)),
      h('div', { class: 'ew-form-actions' }, h('a', { href: '/login', class: 'ew-btn ew-btn--secondary' }, t('mfa.again')))));
    heading.focus();
    return;
  }
  const enroll = step.kind === 'enroll';
  const methods = step.methods.filter((m) => enroll ? !!mfaMethodUI(m)?.enroll : !!mfaMethodUI(m));
  // the answer of a step: signed in, or the next step
  const after = async (res: MFAAnswer) => {
    if (res.done && res.user) {
      await signedIn(res.user as Principal, token ? next : safeLocalPath(res.next ?? next), link);
      return;
    }
    await mfaStep(main, token, next, link, res.step);
  };
  const body = h('div');
  const show = (id: string) => {
    alert.textContent = '';
    const ui = mfaMethodUI(id);
    if (!ui) return;
    const fail = (msg: string) => { alert.textContent = msg; };
    body.replaceChildren(enroll && ui.enroll
      ? ui.enroll({ begin: (o = {}) => mfa.enrollBegin(id, o), finish: async (answer) => after(await mfa.enrollFinish(id, answer)), fail })
      : ui.render({ challenge: () => mfa.challenge(id), verify: async (answer) => after(await mfa.verify(id, answer)), fail }));
  };
  const choice = methods.length > 1 ? h('fieldset', { class: 'ew-choice' }, h('legend', null, enroll ? t('mfa.choose_setup') : t('mfa.choose')),
    ...methods.map((id, i) => {
      const r = h('input', { type: 'radio', name: 'mfa-method', value: id, id: `mfa-${id}`, checked: i === 0 }) as HTMLInputElement;
      r.addEventListener('change', () => { if (r.checked) show(id); });
      return h('div', { class: 'ew-check' }, r, h('label', { for: `mfa-${id}` }, mfaMethodUI(id)!.label()));
    })) : null;
  const intro = enroll ? t('mfa.enroll_help', { required: step.required, passed: step.passed }) : t('mfa.step_help', { factor: step.factor });
  mount(main, h('div', { class: 'ew-narrow' }, heading, h('p', null, intro), alert, choice, body,
    h('div', { class: 'ew-form-actions' }, h('a', { href: '/login', class: 'ew-btn ew-btn--secondary' }, t('mfa.cancel')))));
  if (methods.length) show(methods[0]);
  else alert.textContent = t('mfa.none');
  heading.focus();
}

// ---------------------------------------------------------------- profile

interface APIKey { id: number; name: string; prefix: string; permissions: number; category_id: number | null; allow_admin: boolean; expires_at: string; created_at: string; last_used_at: string | null; revoked_at: string | null; token?: string }
interface SSHKey { id: number; name: string; fingerprint: string; created_at: string; last_used_at: string | null }

// E-mail notification preferences.
async function notificationSection(): Promise<HTMLElement | null> {
  let data: { email: string; prefs: Record<string, boolean>; kinds: string[]; enabled: boolean };
  try {
    data = await api.get('me/notifications');
  } catch {
    return null;
  }
  if (!data.enabled) {
    return h('section', { class: 'ew-section', 'aria-labelledby': 'nt-h' }, h('h2', { id: 'nt-h' }, t('notify.title')), h('p', null, t('notify.disabled')));
  }
  const boxes = data.kinds.map((k) => ({ k, c: checkbox(t(`notify.${k}`), data.prefs[k], { help: t(`notify.${k}_help`) }) }));
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const f = h('form', { class: 'ew-form', novalidate: true }, alert,
    h('fieldset', null, h('legend', null, t('notify.legend')), ...boxes.map((b) => b.c.el)),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  f.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try {
      await api.put('me/notifications', Object.fromEntries(boxes.map((b) => [b.k, b.c.input.checked])));
      toast(t('common.saved'));
    } catch (e) { alert.textContent = errorText(e); }
  });
  return h('section', { class: 'ew-section', 'aria-labelledby': 'nt-h' }, h('h2', { id: 'nt-h' }, t('notify.title')),
    h('p', null, data.email ? t('notify.help', { email: data.email }) : t('notify.no_email')), f);
}

// Business card (shown on team pages).
async function cardSection(name: string): Promise<HTMLElement | null> {
  try {
    return h('section', { class: 'ew-section', id: 'card', 'aria-labelledby': 'card-h' }, h('h2', { id: 'card-h' }, t('profile.card_title')),
      h('p', null, t('profile.card_help')), await profileCardForm('me/profile', name));
  } catch {
    return null;
  }
}

// Watched categories and elements.
async function watchesSection(): Promise<HTMLElement | null> {
  let data: { items: Watch[]; enabled: boolean };
  try {
    data = await api.get('me/watches');
  } catch {
    return null;
  }
  const sec = h('section', { class: 'ew-section', 'aria-labelledby': 'wt-h' });
  const render = () => {
    const rows = data.items.map((w) => {
      const title = pick(w.titles, state.lang, state.site.default_language);
      const href = w.target_type === 'category' ? `/c/${w.target_id}` : `/e/${w.target_id}`;
      return h('tr', null,
        h('th', { scope: 'row' }, title ? h('a', { href }, title) : t('watch.deleted')),
        h('td', null, t(`watch.type.${w.target_type}`)),
        h('td', null, t(w.mode === 'content' ? 'watch.mode_content' : 'watch.mode_link'), ' · ', t(`watch.freq_${w.frequency ?? 'instant'}`)),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => watchDialog(w.target_type, w.target_id, title, w, data.enabled, (n) => {
            data.items = n ? data.items.map((x) => (x.id === w.id ? n : x)) : data.items.filter((x) => x.id !== w.id);
            render();
          }), { variant: 'link', ariaLabel: `${t('common.edit')}: ${title}` }),
          button(t('watch.stop'), () => void api.del(`me/watches/${w.id}`).then(() => {
            data.items = data.items.filter((x) => x.id !== w.id);
            toast(t('watch.removed'));
            render();
          }), { variant: 'link', ariaLabel: `${t('watch.stop')}: ${title}` })));
    });
    mount(sec, h('h2', { id: 'wt-h' }, t('watch.list_title')),
      data.enabled ? null : h('p', { role: 'note' }, t('watch.mail_disabled')),
      rows.length ? h('table', { class: 'ew-table' },
        h('caption', { class: 'sr-only' }, t('watch.list_title')),
        h('thead', null, h('tr', null, ...[t('common.title'), t('watch.type_col'), t('watch.mode'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...rows)) : h('p', null, t('watch.none')));
  };
  render();
  return sec;
}

export async function profileView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const u = state.user;
  if (!u) {
    navigate('/login?next=/profile', { replace: true });
    return;
  }
  let keys: APIKey[] = [];
  let ssh: SSHKey[] = [];
  let prefs: { prefs: Record<string, string>; options: Record<string, string[]> } = { prefs: {}, options: {} };
  try {
    [keys, ssh, prefs] = await Promise.all([api.get<APIKey[]>('me/api-keys'), api.get<SSHKey[]>('me/ssh-keys'), api.get<typeof prefs>('me/prefs')]);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  // profile form
  const name = textInput({ value: u.display_name, autocomplete: 'name' });
  const lang = select([{ value: '', label: t('profile.lang_auto') }, ...state.site.languages.map((l) => ({ value: l, label: langName(l) }))], u.language);
  const editorSel = select([{ value: '', label: t('profile.editor_default', { name: t(`editor.kind.${state.site.editor ?? 'visual'}`) }) },
    ...(prefs.options.editor ?? []).map((v) => ({ value: v, label: t(`editor.kind.${v}`) }))], prefs.prefs.editor ?? '');
  const pAlert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const alias = textInput({ value: u.alias ?? '', autocomplete: 'username', maxlength: 64 });
  const aliasManaged = !!u.alias_source;
  if (aliasManaged) alias.readOnly = true;
  const pForm = h('form', { class: 'ew-form', novalidate: true }, pAlert, field(t('profile.display_name'), name),
    field(t('alias.label'), alias, { help: aliasManaged ? t('alias.managed_self') : t('alias.help_self') }), field(t('profile.language'), lang),
    field(t('profile.editor'), editorSel, { help: t('profile.editor_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  pForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try {
      if (!aliasManaged && alias.value.trim() !== (u.alias ?? '')) {
        await api.put('me/alias', { alias: alias.value.trim() });
        u.alias = alias.value.trim() || undefined;
      }
      await api.put('me', { display_name: name.value, language: lang.value, prefs: { editor: editorSel.value } });
      const prefs = { ...(u.prefs ?? {}) };
      if (editorSel.value) prefs.editor = editorSel.value;
      else delete prefs.editor;
      Object.assign(u, { display_name: name.value, language: lang.value, prefs });
      changed();
      toast(t('common.saved'));
    } catch (e) { pAlert.textContent = errorText(e); }
  });
  // password
  const pwSection = u.provider === 'local' ? (() => {
    const cur = textInput({ type: 'password', autocomplete: 'current-password' });
    const nw = textInput({ type: 'password', autocomplete: 'new-password' });
    const nw2 = textInput({ type: 'password', autocomplete: 'new-password' });
    const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
    const f = h('form', { class: 'ew-form', novalidate: true }, alert, field(t('profile.current_password'), cur), field(t('profile.new_password'), nw, { help: t('profile.password_rules') }), field(t('profile.repeat_password'), nw2),
      h('div', { class: 'ew-form-actions' }, button(t('profile.change_password'), () => undefined, { type: 'submit', variant: 'primary' })));
    f.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      alert.textContent = '';
      if (nw.value !== nw2.value) { alert.textContent = t('profile.password_mismatch'); return; }
      try {
        await api.put('me/password', { current: cur.value, new: nw.value });
        cur.value = nw.value = nw2.value = '';
        toast(t('profile.password_changed'));
      } catch (e) { alert.textContent = errorText(e); }
    });
    return h('section', { class: 'ew-section', 'aria-labelledby': 'pw-h' }, h('h2', { id: 'pw-h' }, t('profile.password')), f);
  })() : h('section', { class: 'ew-section' }, h('h2', null, t('profile.password')), h('p', null, t('profile.password_external', { provider: u.provider })));

  const keyRows = keys.map((k) => h('tr', null,
    h('th', { scope: 'row' }, k.name), h('td', null, h('code', null, `${k.prefix}…`)),
    h('td', null, PERMS.filter((p) => k.permissions & p.bit).map((p) => t(`perm.${p.key}`)).join(', ') + (k.allow_admin ? `, ${t('apikey.admin')}` : '')),
    h('td', null, k.category_id ? `#${k.category_id}` : t('apikey.all_categories')),
    h('td', null, fmtDate(k.expires_at, state.lang)),
    h('td', null, k.revoked_at ? t('apikey.revoked') : (k.last_used_at ? fmtDate(k.last_used_at, state.lang) : t('apikey.never'))),
    h('td', null, k.revoked_at ? null : button(t('apikey.revoke'), async () => {
      if (!(await confirmDialog(t('apikey.revoke_confirm', { name: k.name }), t('apikey.revoke'), true))) return;
      try { await api.del(`me/api-keys/${k.id}`); toast(t('apikey.revoked')); navigate('/profile', { replace: true }); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: t('apikey.revoke_named', { name: k.name }) }))));
  const sshRows = ssh.map((k) => h('tr', null, h('th', { scope: 'row' }, k.name), h('td', null, h('code', null, k.fingerprint)), h('td', null, fmtDate(k.created_at, state.lang)),
    h('td', null, k.last_used_at ? fmtDate(k.last_used_at, state.lang) : t('apikey.never')),
    h('td', null, button(t('common.remove'), async () => {
      if (!(await confirmDialog(t('ssh.remove_confirm', { name: k.name }), t('common.remove'), true))) return;
      try { await api.del(`me/ssh-keys/${k.id}`); navigate('/profile', { replace: true }); } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: t('ssh.remove_named', { name: k.name }) }))));

  mount(main, pageHeader(t('profile.title')),
    h('p', null, t('profile.logged_in_as', { handle: `${u.username}@${u.provider}` })),
    h('section', { class: 'ew-section', 'aria-labelledby': 'pr-h' }, h('h2', { id: 'pr-h' }, t('profile.data')), pForm),
    ...(await profileIdentitySections()),
    await notificationSection(),
    await cardSection(u.display_name || u.username),
    await watchesSection(),
    ...(await renderProfileSections()),
    await assignedWatchesSection(),
    feedTokenSection(),
    pwSection,
    ...(await mfaSections()),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ak-h' }, h('h2', { id: 'ak-h' }, t('apikey.title')), h('p', null, t('apikey.help')),
      button(t('apikey.create'), () => createKeyDialog(), { variant: 'primary' }),
      h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('apikey.title')),
        h('thead', null, h('tr', null, ...[t('common.name'), t('apikey.prefix'), t('acl.permissions'), t('apikey.scope'), t('apikey.expires'), t('apikey.last_used'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(keyRows.length ? keyRows : [h('tr', null, h('td', { colspan: '7' }, t('apikey.none')))])))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ssh-h' }, h('h2', { id: 'ssh-h' }, t('ssh.title')), h('p', null, t('ssh.help')),
      h('pre', { class: 'ew-code' }, `scripts/ewiki-ssh-login.sh ${location.origin} ${u.username} ~/.ssh/id_ed25519`),
      button(t('ssh.add'), () => addSSHDialog(), { variant: 'primary' }),
      h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('ssh.title')),
        h('thead', null, h('tr', null, ...[t('common.name'), t('ssh.fingerprint'), t('common.created'), t('apikey.last_used'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(sshRows.length ? sshRows : [h('tr', null, h('td', { colspan: '5' }, t('ssh.none')))])))));
}

interface MFAChainFactor { id: string; factor: number; passwordless: boolean; silent: boolean; counts_two?: boolean }

/** The factors of the own sign-in: overview and the part of each factor. */
async function mfaSections(): Promise<HTMLElement[]> {
  const st = await api.get<{ applies: boolean; methods: string[]; chain: MFAChainFactor[]; required: number }>('me/mfa');
  const head = h('h2', { id: 'mfa-h' }, t('mfa.title'));
  if (!st.applies) return [h('section', { class: 'ew-section', 'aria-labelledby': 'mfa-h' }, head, h('p', null, t('mfa.by_provider')))];
  const numbers = [...new Set([1, ...st.chain.map((f) => f.factor)])].sort((a, b) => a - b);
  const name = (f: MFAChainFactor) => (mfaMethodUI(f.id)?.label() ?? f.id) + (f.counts_two ? ` (${t('mfa.counts_two')})` : '');
  const overview = h('ol', { class: 'ew-mfa-chain' }, ...numbers.map((n) => {
    const fs = st.chain.filter((f) => f.factor === n);
    const names = [...(n === 1 ? [t('mfa.first_factor')] : []), ...fs.map((f) => name(f) + (st.methods.includes(f.id) ? ` (${t('mfa.set_up')})` : ''))];
    return h('li', null, h('strong', null, t('mfa.factor_n', { n })), ' ', names.join(` ${t('mfa.or')} `));
  }));
  const parts: HTMLElement[] = [];
  for (const f of st.chain) {
    const ui = mfaMethodUI(f.id);
    if (!ui?.profile) continue;
    try {
      const el = await ui.profile({ factor: f.factor });
      if (el) parts.push(el);
    } catch (e) { parts.push(h('p', { role: 'alert' }, errorText(e))); }
  }
  return [h('section', { class: 'ew-section', 'aria-labelledby': 'mfa-h' }, head,
    h('p', null, t('mfa.chain_help', { required: st.required })), overview, ...parts)];
}

function createKeyDialog(): void {
  const name = textInput({ required: true });
  const boxes = PERMS.map((p) => ({ p, cb: checkbox(t(`perm.${p.key}`), p.key === 'view') }));
  const admin = isAdmin() ? checkbox(t('apikey.allow_admin'), false, { help: t('apikey.allow_admin_help') }) : null;
  const cat = textInput({ type: 'number', placeholder: t('apikey.category_placeholder') });
  const exp = h('input', { type: 'date', class: 'ew-input', value: new Date(Date.now() + 90 * 864e5).toISOString().slice(0, 10) });
  formDialog(t('apikey.create'), [field(t('common.name'), name, { required: true }),
    h('fieldset', null, h('legend', null, t('acl.permissions')), ...boxes.map((b) => b.cb.el)), admin?.el ?? null,
    field(t('apikey.category'), cat, { help: t('apikey.category_help') }), field(t('apikey.expires'), exp)], t('common.create'), async () => {
    const perms = boxes.filter((b) => b.cb.input.checked).reduce((a, b) => a | b.p.bit, 0);
    const k = await api.post<APIKey>('me/api-keys', {
      name: name.value, permissions: perms, allow_admin: admin?.input.checked ?? false,
      category_id: cat.value ? Number(cat.value) : null, expires_at: new Date(`${exp.value}T23:59:59`).toISOString(),
    });
    window.setTimeout(() => {
      const d = openDialog(t('apikey.created'), h('div', null, h('p', null, t('apikey.copy_now')), h('pre', { class: 'ew-code', tabindex: '0' }, k.token ?? ''),
        h('p', { class: 'ew-help' }, t('apikey.usage'))), [button(t('common.close'), () => d.close(), { variant: 'primary' })], { onClose: () => navigate('/profile', { replace: true }) });
    }, 0);
  });
}

function addSSHDialog(): void {
  const name = textInput();
  const key = textArea({ rows: 4 });
  formDialog(t('ssh.add'), [field(t('common.name'), name), field(t('ssh.public_key'), key, { required: true, help: t('ssh.public_key_help') })], t('ssh.add'), async () => {
    await api.post('me/ssh-keys', { name: name.value, public_key: key.value });
    toast(t('ssh.added'));
    navigate('/profile', { replace: true });
  });
}

// ---------------------------------------------------------------- trash

interface TrashItem { type: 'category' | 'element'; id: number; titles: Record<string, string>; parent_id: number | null; deleted_at: string; deleted_by: string; contains: number }

export async function trashView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  if (!state.user) {
    navigate('/login?next=/trash', { replace: true });
    return;
  }
  let items: TrashItem[];
  try {
    items = await api.get<TrashItem[]>('trash');
  } catch (e) {
    errorPage(main, e);
    return;
  }
  const title = (it: TrashItem) => pick(it.titles, state.lang, state.site.default_language) || `#${it.id}`;
  mount(main, pageHeader(t('trash.title')), h('p', null, t('trash.help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('trash.title')),
      h('thead', null, h('tr', null, ...[t('common.title'), t('trash.type'), t('trash.deleted_at'), t('trash.deleted_by'), t('trash.contains'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(items.length ? items.map((it) => h('tr', null,
        h('th', { scope: 'row' }, title(it)), h('td', null, t(`trash.${it.type}`)), h('td', null, fmtDate(it.deleted_at, state.lang)), h('td', null, it.deleted_by),
        h('td', null, it.contains ? String(it.contains) : '–'),
        h('td', { class: 'ew-actions' },
          button(t('trash.restore'), async () => {
            try { await api.post(`trash/${it.type}/${it.id}/restore`); invalidateTree(); toast(t('trash.restored')); navigate('/trash', { replace: true }); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('trash.restore_named', { title: title(it) }) }),
          hasPerm('core.trash.manage') ? button(t('trash.purge'), async () => {
            if (!(await confirmDialog(t('trash.purge_confirm', { title: title(it) }), t('trash.purge'), true))) return;
            try { await api.del(`trash/${it.type}/${it.id}`); toast(t('trash.purged')); navigate('/trash', { replace: true }); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('trash.purge_named', { title: title(it) }) }) : null))) : [h('tr', null, h('td', { colspan: '6' }, t('trash.empty')))]))));
}

/** Unknown paths: short URLs of elements and categories (/kalender), else 404. */
export async function notFoundView(ctx: RouteCtx): Promise<void> {
  const path = ctx.path.replace(/^\/+|\/+$/g, '');
  if (path && !path.startsWith('api/')) {
    try {
      const r = await api.get<{ type: 'element' | 'category'; id: number; path: string; redirect: boolean; paths?: Record<string, string> }>(`resolve${qs({ path })}`);
      // a former address: show the current one (the views show the path in the language of the user)
      if (r.redirect && r.path) history.replaceState(history.state, '', r.path + location.search + location.hash);
      const params = { ...ctx.params, id: String(r.id) };
      if (r.type === 'element') await elementView({ ...ctx, params });
      else await categoryView({ ...ctx, params });
      return;
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        errorPage(ctx.main, e);
        return;
      }
    }
  }
  setContext(0, [], []);
  notFoundPage(ctx.main, { path });
}
