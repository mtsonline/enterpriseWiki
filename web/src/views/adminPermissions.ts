// Central permissions: choose a user, group or role and see and change its
// permissions in all categories the caller manages (including where they
// come from: own rule, group, role, inherited).
import { api, qs } from '../core/api';
import { h, mount } from '../core/dom';
import { pick, t } from '../core/i18n';

import { state } from '../core/state';
import type { Group, Role, Rule } from '../core/types';
import { toast } from '../core/announce';
import { userPicker } from '../components/subjects';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select } from '../components/ui';
import { loadCatalog, ruleEditor, summary, type PermDef } from '../components/permissions';
import { editRuleDialog } from './categorySettings';
import { elementRightsDialog } from '../components/elementRights';

interface Source extends Rule { inherited: boolean }
interface ElementRow { id: number; category_id: number; own_rights_only: boolean; titles: Record<string, string>; rules: { id: number; permissions: number; actions: string[] }[]; effective: string[] }
interface Row { id: number; parent_id: number | null; depth: number; titles: Record<string, string>; space_key?: string; personal?: boolean; rules: Rule[]; effective: string[]; sources: Source[]; elements?: ElementRow[] }

const CORE_BITS: Record<string, number> = { view: 1, edit: 2, create: 4, delete: 8, manage: 16, grant: 32 };

export async function permissionsAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const [groups, roles, cat] = await Promise.all([api.get<Group[]>('groups'), api.get<Role[]>('roles'), loadCatalog()]);
  const defs: PermDef[] = cat.permissions;
  const type = select(['user', 'group', 'role', 'authenticated', 'guest'].map((x) => ({ value: x, label: t(`acl.${x}`) })), query.get('subject_type') ?? 'group');
  const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })), query.get('subject_type') === 'group' ? query.get('subject_id') ?? undefined : undefined);
  const roleSel = select(roles.map((r) => ({ value: String(r.id), label: r.name })), query.get('subject_type') === 'role' ? query.get('subject_id') ?? undefined : undefined);
  // a person from the address (?subject_type=user&subject_id=…&user=name), so the view can be shared as a link
  const fromQuery = query.get('subject_type') === 'user' && Number(query.get('subject_id')) > 0;
  const user = userPicker({ label: t('acl.user'), help: t('acl.user_help'), initial: fromQuery ? [{ id: Number(query.get('subject_id')), name: query.get('user') || `#${query.get('subject_id')}` }] : [] });
  const subjectWrap = h('div');
  const showSubject = () => subjectWrap.replaceChildren(
    type.value === 'group' ? field(t('acl.group'), groupSel) : type.value === 'role' ? field(t('acl.role'), roleSel)
      : type.value === 'user' ? user.el : h('span'));
  type.addEventListener('change', showSubject);
  showSubject();
  const result = h('div', { 'aria-live': 'polite' });
  const subject = (): { type: string; id: number | null; name: string } | null => {
    switch (type.value) {
      case 'group': return groupSel.value ? { type: 'group', id: Number(groupSel.value), name: groups.find((g) => String(g.id) === groupSel.value)?.name ?? '' } : null;
      case 'role': return roleSel.value ? { type: 'role', id: Number(roleSel.value), name: roles.find((r) => String(r.id) === roleSel.value)?.name ?? '' } : null;
      case 'user': { const id = user.value()[0]; return id ? { type: 'user', id, name: user.names()[0] } : null; }
      default: return { type: type.value, id: null, name: t(`acl.${type.value}`) };
    }
  };
  const sourceText = (s: Source): string => {
    const who = s.subject_type === 'group' ? `${t('acl.group')} ${groups.find((g) => g.id === s.subject_id)?.name ?? s.subject_id}`
      : s.subject_type === 'role' ? `${t('acl.role')} ${roles.find((r) => r.id === s.subject_id)?.name ?? s.subject_id}`
        : t(`acl.${s.subject_type}`);
    return `${who}${s.inherited ? ` (${t('acl.inherited_short')})` : ''}`;
  };
  let sub0: { type: string; id: number | null; name: string } = { type: 'group', id: null, name: '' };
  const show = async () => {
    const sub = subject();
    if (!sub) { mount(result, h('p', { role: 'alert' }, t('acl.user_not_found'))); return; }
    const params: Record<string, string> = { subject_type: sub.type };
    if (sub.id !== null) params.subject_id = String(sub.id);
    if (sub.type === 'user') params.user = sub.name;
    history.replaceState(null, '', `/admin/permissions${qs(params)}`);
    let rows: Row[];
    try {
      rows = await api.get<Row[]>(`permissions/overview${qs({ subject_type: sub.type, subject_id: sub.id ?? undefined })}`);
    } catch (e) { mount(result, h('p', { role: 'alert' }, errorText(e))); return; }
    sub0 = sub;
    mount(result,
      h('table', { class: 'ew-table ew-perm-overview' },
        h('caption', null, t('permissions.overview_caption', { subject: sub.name })),
        h('thead', null, h('tr', null, ...[t('picker.category'), t('permissions.direct'), t('permissions.effective'), t('permissions.from'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(rows.length ? rows.flatMap((r) => [catRow(r), ...(r.elements ?? []).map((x) => elementRow(r, x))]) : [h('tr', null, h('td', { colspan: '5' }, t('permissions.no_categories')))]))));
  };
  // rules of the subject on single pages of a category
  const elementRow = (r: Row, x: ElementRow) => {
    const bits = x.effective.filter((e) => e in CORE_BITS).reduce((a, e) => a | CORE_BITS[e], 0);
    const acts = x.effective.filter((e) => !(e in CORE_BITS));
    const title = pick(x.titles, state.lang) || `#${x.id}`;
    return h('tr', { class: 'ew-perm-element' },
      h('th', { scope: 'row', class: 'ew-perm-cat' }, h('span', { class: `ew-indent ew-indent-${Math.min(r.depth + 1, 8)}` },
        `${t('permissions.page')}: `, h('a', { href: `/e/${x.id}` }, title),
        x.own_rights_only ? h('span', { class: 'ew-badge ew-badge--info' }, t('erights.own_only')) : null)),
      h('td', null, x.rules.map((y) => summary(y.permissions, y.actions ?? [], defs)).join('; ')),
      h('td', null, sub0.type === 'guest' ? '–' : summary(bits, acts, defs)),
      h('td', null, t('permissions.page_rule')),
      h('td', { class: 'ew-actions' }, button(t('erights.button'), () => void elementRightsDialog(x.id, title, () => void show()), { variant: 'link', ariaLabel: t('permissions.page_rights', { page: title }) })));
  };
  const catRow = (r: Row) => {
      const sub = sub0;
      const titleOf = (x: Row) => pick(x.titles, state.lang) || `#${x.id}`;
      const refresh = () => void show();
      const bits = r.effective.filter((e) => e in CORE_BITS).reduce((a, e) => a | CORE_BITS[e], 0);
      const acts = r.effective.filter((e) => !(e in CORE_BITS));
      const own = r.rules[0];
      return h('tr', null,
        h('th', { scope: 'row', class: 'ew-perm-cat' }, h('span', { class: `ew-indent ew-indent-${Math.min(r.depth, 8)}` }, h('a', { href: `/c/${r.id}/settings?tab=permissions` }, titleOf(r)),
          // top-level categories are spaces (their permissions are these rules)
          r.parent_id === null ? h('span', { class: 'ew-badge ew-badge--info ew-space-badge' }, r.personal ? t('space.personal') : r.space_key ? `${t('category.tab.space')} ${r.space_key}` : t('category.tab.space')) : null)),
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


