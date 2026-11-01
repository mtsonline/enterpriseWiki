// Search, login, profile and trash.
import { api, qs } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { loadMessages, pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { changed, isAdmin, state } from '../core/state';
import { PERMS, type Principal } from '../core/types';
import { announce, toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, pageHeader, select, textArea, textInput } from '../components/ui';
import { invalidateTree, langName, setContext } from '../components/widgets';
import { typeLabel } from '../sdk';
import { errorPage } from './content';

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

export async function loginView({ query, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const next = query.get('next') ?? '/';
  const safeNext = next.startsWith('/') && !next.startsWith('//') ? next : '/';
  if (state.user) {
    navigate(safeNext, { replace: true });
    return;
  }
  const user = textInput({ name: 'username', autocomplete: 'username', required: true });
  const pw = textInput({ name: 'password', type: 'password', autocomplete: 'current-password', required: true });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  let providers = state.site.auth_providers;
  try { providers = await api.get<{ id: string; name: string }[]>('auth/providers'); } catch { /* use site info */ }
  const form = h('form', { class: 'ew-form ew-login', novalidate: true },
    alert,
    field(t('auth.username'), user, { required: true, help: providers.length > 1 ? t('auth.username_help', { providers: providers.map((p) => p.id).join(', ') }) : undefined }),
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
      state.user = res.user;
      if (res.user.language && state.site.languages.includes(res.user.language) && res.user.language !== state.lang) {
        state.lang = res.user.language;
        await loadMessages(state.lang);
      }
      invalidateTree();
      changed();
      announce(t('auth.logged_in', { name: res.user.display_name || res.user.username }));
      navigate(safeNext);
    } catch (e) {
      alert.textContent = errorText(e);
      pw.value = '';
      pw.focus();
    }
  });
  mount(main, h('div', { class: 'ew-narrow' }, h('h1', null, t('auth.login_title', { site: state.site.name })), form,
    h('p', { class: 'ew-help' }, t('auth.ssh_hint'))));
}

// ---------------------------------------------------------------- profile

interface APIKey { id: number; name: string; prefix: string; permissions: number; category_id: number | null; allow_admin: boolean; expires_at: string; created_at: string; last_used_at: string | null; revoked_at: string | null; token?: string }
interface SSHKey { id: number; name: string; fingerprint: string; created_at: string; last_used_at: string | null }

export async function profileView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const u = state.user;
  if (!u) {
    navigate('/login?next=/profile', { replace: true });
    return;
  }
  let keys: APIKey[] = [];
  let ssh: SSHKey[] = [];
  try {
    [keys, ssh] = await Promise.all([api.get<APIKey[]>('me/api-keys'), api.get<SSHKey[]>('me/ssh-keys')]);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  // profile form
  const name = textInput({ value: u.display_name, autocomplete: 'name' });
  const email = textInput({ value: u.email, type: 'email', autocomplete: 'email' });
  const lang = select([{ value: '', label: t('profile.lang_auto') }, ...state.site.languages.map((l) => ({ value: l, label: langName(l) }))], u.language);
  const pAlert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const pForm = h('form', { class: 'ew-form', novalidate: true }, pAlert, field(t('profile.display_name'), name), field(t('profile.email'), email), field(t('profile.language'), lang),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  pForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try {
      await api.put('me', { display_name: name.value, email: email.value, language: lang.value });
      Object.assign(u, { display_name: name.value, email: email.value, language: lang.value });
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
    pwSection,
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
          isAdmin() ? button(t('trash.purge'), async () => {
            if (!(await confirmDialog(t('trash.purge_confirm', { title: title(it) }), t('trash.purge'), true))) return;
            try { await api.del(`trash/${it.type}/${it.id}`); toast(t('trash.purged')); navigate('/trash', { replace: true }); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: t('trash.purge_named', { title: title(it) }) }) : null))) : [h('tr', null, h('td', { colspan: '6' }, t('trash.empty')))]))));
}

export function notFoundView({ main }: RouteCtx): void {
  setContext(0, [], []);
  mount(main, h('h1', null, t('error.not_found')), h('p', null, h('a', { href: '/' }, t('nav.home'))));
}
