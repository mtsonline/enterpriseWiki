// Central permissions: choose a user, group or role and see and change its
// permissions in all categories the caller manages (including where they
// come from: own rule, group, role, inherited).
import { api, qs } from '../core/api';
import { h, mount, uid } from '../core/dom';
import { pick, t } from '../core/i18n';

import { state } from '../core/state';
import type { Group, Role, Rule } from '../core/types';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select, textInput } from '../components/ui';
import { loadCatalog, ruleEditor, summary, type PermDef } from '../components/permissions';
import { editRuleDialog } from './categorySettings';

interface Source extends Rule { inherited: boolean }
interface Row { id: number; parent_id: number | null; depth: number; titles: Record<string, string>; rules: Rule[]; effective: string[]; sources: Source[] }

const CORE_BITS: Record<string, number> = { view: 1, edit: 2, create: 4, delete: 8, manage: 16 };

export async function permissionsAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const [groups, roles, cat] = await Promise.all([api.get<Group[]>('groups'), api.get<Role[]>('roles'), loadCatalog()]);
  const defs: PermDef[] = cat.permissions;
  const type = select(['user', 'group', 'role', 'authenticated', 'guest'].map((x) => ({ value: x, label: t(`acl.${x}`) })), query.get('subject_type') ?? 'group');
  const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })), query.get('subject_type') === 'group' ? query.get('subject_id') ?? undefined : undefined);
  const roleSel = select(roles.map((r) => ({ value: String(r.id), label: r.name })), query.get('subject_type') === 'role' ? query.get('subject_id') ?? undefined : undefined);
  const userInput = textInput({ placeholder: t('acl.user_search'), value: query.get('user') ?? '' });
  const userList = h('datalist', { id: uid('users') });
  userInput.setAttribute('list', userList.id);
  const userMap = new Map<string, number>();
  if (query.get('subject_type') === 'user' && query.get('user') && query.get('subject_id')) userMap.set(query.get('user')!, Number(query.get('subject_id')));
  userInput.addEventListener('input', async () => {
    if (userInput.value.length < 2) return;
    const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(userInput.value)}`);
    userList.replaceChildren(...res.map((u) => { userMap.set(u.handle, u.id); return h('option', { value: u.handle }, u.display_name); }));
  });
  const subjectWrap = h('div');
  const showSubject = () => subjectWrap.replaceChildren(
    type.value === 'group' ? field(t('acl.group'), groupSel) : type.value === 'role' ? field(t('acl.role'), roleSel)
      : type.value === 'user' ? h('div', null, field(t('acl.user'), userInput, { help: t('acl.user_help') }), userList) : h('span'));
  type.addEventListener('change', showSubject);
  showSubject();
  const result = h('div', { 'aria-live': 'polite' });
  const subject = (): { type: string; id: number | null; name: string } | null => {
    switch (type.value) {
      case 'group': return groupSel.value ? { type: 'group', id: Number(groupSel.value), name: groups.find((g) => String(g.id) === groupSel.value)?.name ?? '' } : null;
      case 'role': return roleSel.value ? { type: 'role', id: Number(roleSel.value), name: roles.find((r) => String(r.id) === roleSel.value)?.name ?? '' } : null;
      case 'user': { const id = userMap.get(userInput.value); return id ? { type: 'user', id, name: userInput.value } : null; }
      default: return { type: type.value, id: null, name: t(`acl.${type.value}`) };
    }
  };
  const sourceText = (s: Source): string => {
    const who = s.subject_type === 'group' ? `${t('acl.group')} ${groups.find((g) => g.id === s.subject_id)?.name ?? s.subject_id}`
      : s.subject_type === 'role' ? `${t('acl.role')} ${roles.find((r) => r.id === s.subject_id)?.name ?? s.subject_id}`
        : t(`acl.${s.subject_type}`);
    return `${who}${s.inherited ? ` (${t('acl.inherited_short')})` : ''}`;
  };
  const show = async () => {
    const sub = subject();
    if (!sub) { mount(result, h('p', { role: 'alert' }, t('acl.user_not_found'))); return; }
    const params: Record<string, string> = { subject_type: sub.type };
    if (sub.id !== null) params.subject_id = String(sub.id);
    if (sub.type === 'user') params.user = userInput.value;
    history.replaceState(null, '', `/admin/permissions${qs(params)}`);
    let rows: Row[];
    try {
      rows = await api.get<Row[]>(`permissions/overview${qs({ subject_type: sub.type, subject_id: sub.id ?? undefined })}`);
    } catch (e) { mount(result, h('p', { role: 'alert' }, errorText(e))); return; }
    const titleOf = (r: Row) => pick(r.titles, state.lang) || `#${r.id}`;
    const refresh = () => void show();
    mount(result,
      h('table', { class: 'ew-table ew-perm-overview' },
        h('caption', null, t('permissions.overview_caption', { subject: sub.name })),
        h('thead', null, h('tr', null, ...[t('picker.category'), t('permissions.direct'), t('permissions.effective'), t('permissions.from'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(rows.length ? rows.map((r) => {
          const bits = r.effective.filter((e) => e in CORE_BITS).reduce((a, e) => a | CORE_BITS[e], 0);
          const acts = r.effective.filter((e) => !(e in CORE_BITS));
          const own = r.rules[0];
          return h('tr', null,
            h('th', { scope: 'row', class: 'ew-perm-cat' }, h('span', { class: `ew-indent ew-indent-${Math.min(r.depth, 8)}` }, h('a', { href: `/c/${r.id}/settings?tab=permissions` }, titleOf(r)))),
            h('td', null, r.rules.length ? r.rules.map((x) => summary(x.permissions, x.actions ?? [], defs) + (x.inherit ? '' : ` (${t('permissions.this_only')})`)).join('; ') : '–'),
            h('td', null, sub.type === 'guest' ? '–' : summary(bits, acts, defs)),
            h('td', null, r.sources.length ? r.sources.map(sourceText).join(', ') : '–'),
            h('td', { class: 'ew-actions' },
              own ? button(t('common.edit'), () => void editRuleDialog(own, `${sub.name} – ${titleOf(r)}`, refresh), { variant: 'link', ariaLabel: t('permissions.edit_in', { category: titleOf(r) }) })
                : button(t('permissions.grant'), () => void grant(r, sub, refresh), { variant: 'link', ariaLabel: t('permissions.grant_in', { category: titleOf(r) }) }),
              own ? button(t('common.remove'), async () => {
                if (!(await confirmDialog(t('acl.remove_confirm', { subject: sub.name }), t('common.remove'), true))) return;
                try { await api.del(`acl/${own.id}`); toast(t('acl.removed')); refresh(); } catch (e) { toast(errorText(e), 'error'); }
              }, { variant: 'link', ariaLabel: t('permissions.remove_in', { category: titleOf(r) }) }) : null));
        }) : [h('tr', null, h('td', { colspan: '5' }, t('permissions.no_categories')))]))));
  };
  const form = h('form', { class: 'ew-form ew-panel', novalidate: true },
    field(t('acl.subject_type'), type), subjectWrap,
    h('div', { class: 'ew-form-actions' }, button(t('permissions.show'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', (ev) => { ev.preventDefault(); void show(); });
  mount(panel, pageHeader(t('admin.permissions')), h('p', null, t('permissions.help')), form, result);
  if (query.get('subject_type')) void show();
}

async function grant(r: Row, sub: { type: string; id: number | null; name: string }, done: () => void): Promise<void> {
  const ed = await ruleEditor(1, [], { guest: sub.type === 'guest' });
  const inherit = checkbox(t('acl.inherit_label'), true);
  formDialog(t('permissions.grant_title', { subject: sub.name, category: pick(r.titles, state.lang) }), [ed.el, inherit.el], t('permissions.grant'), async () => {
    const v = ed.value();
    if (!v.permissions && !v.actions.length) throw new Error(t('acl.choose_perm'));
    await api.post(`categories/${r.id}/acl`, { subject_type: sub.type, subject_id: sub.id, ...v, inherit: inherit.input.checked });
    toast(t('acl.added'));
    done();
  });
}


