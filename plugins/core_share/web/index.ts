// Share pages and categories with users, groups and roles.
import { api, button, checkbox, errorText, field, h, hasPerm, openDialog, registerPageAction, state, t, textArea, textInput, toast, uid, type Group, type Role } from '@ewiki/sdk';

interface Result { recipients: number; without_access: string[]; sent: boolean }

async function shareDialog(target: { type: 'element' | 'category'; id: number; title: string }): Promise<void> {
  const [groups, roles] = await Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]);
  const chosen = new Map<number, string>();
  const chips = h('ul', { class: 'ew-chips', 'aria-live': 'polite' });
  const drawChips = () => chips.replaceChildren(...[...chosen.entries()].map(([id, name]) => h('li', null, name, ' ',
    button('×', () => { chosen.delete(id); drawChips(); }, { variant: 'subtle', ariaLabel: t('core.share.remove', { name }) }))));
  const user = textInput({ placeholder: t('core.share.users_help') });
  const list = h('datalist', { id: uid('share-users') });
  user.setAttribute('list', list.id);
  const found = new Map<string, { id: number; name: string }>();
  user.addEventListener('input', async () => {
    const v = user.value.trim();
    if (found.has(v)) { const u = found.get(v)!; chosen.set(u.id, u.name); user.value = ''; drawChips(); return; }
    if (v.length < 2) return;
    const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(v)}`).catch(() => []);
    list.replaceChildren(...res.map((u) => { found.set(u.handle, { id: u.id, name: u.display_name || u.handle }); return h('option', { value: u.handle }, u.display_name); }));
  });
  const groupBoxes = groups.map((g) => ({ g, cb: checkbox(g.name, false) }));
  const roleBoxes = roles.filter((r) => !r.is_admin).map((r) => ({ r, cb: checkbox(r.name, false) }));
  const message = textArea({ rows: 3 });
  message.maxLength = 2000;
  const status = h('div', { role: 'status', 'aria-live': 'polite' });
  const alert = h('p', { class: 'ew-field-error', role: 'alert' });
  const body = () => ({ target_type: target.type, target_id: target.id, users: [...chosen.keys()],
    groups: groupBoxes.filter((x) => x.cb.input.checked).map((x) => x.g.id), roles: roleBoxes.filter((x) => x.cb.input.checked).map((x) => x.r.id), message: message.value });
  const show = (r: Result) => status.replaceChildren(h('p', null, t('core.share.recipients', { n: r.recipients })),
    r.without_access.length ? h('p', { class: 'ew-notice ew-notice--warning' }, t('core.share.without_access', { names: r.without_access.join(', ') })) : '');
  const valid = () => {
    alert.textContent = '';
    const b = body();
    if (!b.users.length && !b.groups.length && !b.roles.length) { alert.textContent = t('core.share.none'); return false; }
    return true;
  };
  const dlg = openDialog(t('core.share.title', { title: target.title }), h('div', { class: 'ew-form' },
    h('p', null, t('core.share.help')),
    field(t('core.share.users'), user, { help: t('core.share.users_help') }), list, chips,
    groups.length ? h('fieldset', null, h('legend', null, t('core.share.groups')), h('div', { class: 'ew-check-grid' }, ...groupBoxes.map((x) => x.cb.el))) : null,
    roleBoxes.length ? h('fieldset', null, h('legend', null, t('core.share.roles')), h('div', { class: 'ew-check-grid' }, ...roleBoxes.map((x) => x.cb.el))) : null,
    field(t('core.share.message'), message), alert, status,
    h('div', { class: 'ew-dialog-actions' },
      button(t('core.share.check'), async () => {
        if (!valid()) return;
        try { show(await api.post<Result>('x/core.share/check', body())); } catch (e) { alert.textContent = errorText(e); }
      }, { variant: 'secondary' }),
      button(t('core.share.send'), async () => {
        if (!valid()) return;
        try {
          const r = await api.post<Result>('x/core.share/share', body());
          toast(t('core.share.sent', { n: r.recipients }));
          if (r.without_access.length) show(r); else dlg.close();
        } catch (e) { alert.textContent = errorText(e); }
      }, { variant: 'primary' }))), [], { wide: true });
}

export function register(): void {
  registerPageAction({
    id: 'core.share', order: 20,
    render: (target) => (state.user && hasPerm('core.share.use') ? button(t('core.share.button'), () => void shareDialog(target), { variant: 'secondary', ariaLabel: `${t('core.share.button')}: ${target.title}` }) : null),
  });
}
