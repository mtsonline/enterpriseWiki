// Share pages and categories with users, groups and roles.
import { api, button, choiceList, errorText, field, h, hasPerm, openDialog, registerPageAction, searchUsers, state, t, textArea, toast, type Group, type Role } from '@ewiki/coreplugin';

interface Result { recipients: number; without_access: string[]; sent: boolean }

async function shareDialog(target: { type: 'element' | 'category'; id: number; title: string }): Promise<void> {
  const [groups, roles] = await Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]);
  // people are searched (2 letters); groups and roles: checkboxes or a list with a filter
  const users = choiceList({ label: t('core.share.users'), search: searchUsers, help: t('core.share.users_help') });
  const groupList = groups.length ? choiceList({ label: t('core.share.groups'), items: groups.map((g) => ({ id: g.id, name: g.name })) }) : null;
  const shareRoles = roles.filter((r) => !r.is_admin);
  const roleList = shareRoles.length ? choiceList({ label: t('core.share.roles'), items: shareRoles.map((r) => ({ id: r.id, name: r.name })) }) : null;
  const message = textArea({ rows: 3 });
  message.maxLength = 2000;
  const status = h('div', { role: 'status', 'aria-live': 'polite' });
  const alert = h('p', { class: 'ew-field-error', role: 'alert' });
  const body = () => ({ target_type: target.type, target_id: target.id, users: users.value(),
    groups: groupList?.value() ?? [], roles: roleList?.value() ?? [], message: message.value });
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
    users.el, groupList?.el ?? null, roleList?.el ?? null,
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
    id: 'core.share', order: 20, label: () => t('core.share.button'),
    render: (target) => (state.user && hasPerm('core.share.use') ? button(t('core.share.button'), () => void shareDialog(target), { variant: 'secondary', ariaLabel: `${t('core.share.button')}: ${target.title}` }) : null),
  });
}
