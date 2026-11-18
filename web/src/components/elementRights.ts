// Own rights of one page (element): rules in addition to those of its
// category, "own rights only" and an own classification. Shown to those who
// may give rights on the page (managers of the category, administrators,
// people with the permission "give rights" on the page).
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import type { Group, Role } from '../core/types';
import { button, checkbox, confirmDialog, errorText, field, formDialog, openDialog, select } from './ui';
import { loadCatalog, ruleEditor, summary } from './permissions';
import { userPicker } from './subjects';

interface ElementRule { id: number; subject_type: 'user' | 'group' | 'role' | 'guest' | 'authenticated'; subject_id: number | null; permissions: number; actions: string[] }
interface ElementACL {
  element_id: number;
  category_id: number;
  own_rights_only: boolean;
  classification: number;
  effective_classification: number;
  hidden: boolean;
  rules: ElementRule[];
  users: Record<string, { handle: string; display_name: string }>;
  mine: string[];
}

const BITS: Record<string, number> = { view: 1, edit: 2, delete: 8, grant: 32 };

/** elementRightsDialog shows and changes the rights of page id; done runs
 * after a change (e.g. to show the new state of the page). */
export async function elementRightsDialog(id: number, title: string, done?: () => void): Promise<void> {
  let acl: ElementACL;
  let groups: Group[] = [];
  let roles: Role[] = [];
  try {
    [acl, groups, roles] = await Promise.all([api.get<ElementACL>(`elements/${id}/acl`), api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => []), loadCatalog()]);
  } catch (e) {
    toast(errorText(e), 'error');
    return;
  }
  const catalog = await loadCatalog();
  const defs = catalog.permissions;
  const mine = acl.mine.reduce((a, k) => a | (BITS[k] ?? 0), 0);
  const subject = (r: ElementRule): string => {
    switch (r.subject_type) {
      case 'guest': return t('acl.guest');
      case 'authenticated': return t('acl.authenticated');
      case 'group': return `${t('acl.group')}: ${groups.find((g) => g.id === r.subject_id)?.name ?? r.subject_id}`;
      case 'role': return `${t('acl.role')}: ${roles.find((x) => x.id === r.subject_id)?.name ?? r.subject_id}`;
      default: {
        const u = acl.users[String(r.subject_id)];
        return `${t('acl.user')}: ${u ? (u.display_name ? `${u.display_name} (${u.handle})` : u.handle) : `#${r.subject_id}`}`;
      }
    }
  };
  const reopen = () => { handle.close(); if (done) done(); void elementRightsDialog(id, title, done); };

  // own rights only and classification
  const own = checkbox(t('erights.own_only'), acl.own_rights_only, { help: t('erights.own_only_help') });
  const cls = select([0, 1, 2, 3].map((n) => ({ value: String(n), label: n === 0 ? t('erights.class_none') : t(`class.level.${n}`) })), String(acl.classification));
  const effective = h('p', { class: 'ew-help' }, t('erights.class_effective', { level: t(`class.level.${acl.effective_classification}`) }));
  const settingsForm = h('form', { class: 'ew-form', novalidate: true },
    h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('erights.settings')),
      own.el,
      field(t('erights.classification'), cls, { help: t('erights.classification_help') }),
      effective,
      button(t('common.save'), () => undefined, { variant: 'secondary', type: 'submit' })));
  settingsForm.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    try {
      await api.put(`elements/${id}/acl`, { own_rights_only: own.input.checked, classification: Number(cls.value) });
      toast(t('common.saved'));
      reopen();
    } catch (e) {
      toast(errorText(e), 'error');
    }
  });

  // rules
  const rows = acl.rules.length ? acl.rules.map((r) => h('tr', null,
    h('th', { scope: 'row' }, subject(r)),
    h('td', null, summary(r.permissions, r.actions ?? [], defs)),
    h('td', { class: 'ew-actions' },
      button(t('common.edit'), () => void editRule(r), { variant: 'link', ariaLabel: t('acl.edit_rule', { subject: subject(r) }) }),
      button(t('common.remove'), async () => {
        if (!(await confirmDialog(t('acl.remove_confirm', { subject: subject(r) }), t('common.remove'), true))) return;
        try {
          await api.del(`element-acl/${r.id}`);
          toast(t('acl.removed'));
          reopen();
        } catch (e) {
          toast(errorText(e), 'error');
        }
      }, { variant: 'link', ariaLabel: t('acl.remove_rule', { subject: subject(r) }) }))))
    : [h('tr', null, h('td', { colspan: '3' }, t('acl.none')))];
  const captionId = uid('erules');
  const table = h('table', { class: 'ew-table', 'aria-labelledby': captionId },
    h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('acl.subject')), h('th', { scope: 'col' }, t('acl.permissions')), h('th', { scope: 'col' }, h('span', { class: 'sr-only' }, t('common.actions'))))),
    h('tbody', null, ...rows));

  const editRule = async (r: ElementRule) => {
    const ed = await ruleEditor(r.permissions, r.actions ?? [], { guest: r.subject_type === 'guest', element: true, mine, mineActions: undefined });
    formDialog(t('acl.edit_title', { subject: subject(r) }), [ed.el], t('common.save'), async () => {
      const v = ed.value();
      if (!(v.permissions & 1)) throw new Error(t('erights.view_needed'));
      await api.put(`element-acl/${r.id}`, v);
      toast(t('common.saved'));
      reopen();
    });
  };

  const addRule = async () => {
    const type = select((['user', 'group', 'role', 'authenticated', 'guest'] as const).map((x) => ({ value: x, label: t(`acl.${x}`) })), 'user');
    const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })));
    const roleSel = select(roles.map((o) => ({ value: String(o.id), label: o.name })));
    const user = userPicker({ label: t('acl.user') });
    const who = h('div');
    const show = () => who.replaceChildren(type.value === 'group' ? field(t('acl.group'), groupSel) : type.value === 'role' ? field(t('acl.role'), roleSel) : type.value === 'user' ? user.el : '');
    type.addEventListener('change', show);
    show();
    const ed = await ruleEditor(1, [], { element: true, mine });
    formDialog(t('erights.add'), [field(t('acl.subject_type'), type), who, ed.el], t('acl.add'), async () => {
      const st = type.value;
      const sid = st === 'group' ? Number(groupSel.value) || null : st === 'role' ? Number(roleSel.value) || null : st === 'user' ? user.value()[0] ?? null : null;
      if ((st === 'user' || st === 'group' || st === 'role') && !sid) throw new Error(t(st === 'user' ? 'acl.user_not_found' : 'instance.choose_subject'));
      const v = ed.value();
      if (!(v.permissions & 1)) throw new Error(t('erights.view_needed'));
      if (st === 'guest' && (v.permissions !== 1 || v.actions.length)) throw new Error(t('acl.guest_view_only'));
      await api.post(`elements/${id}/acl`, { subject_type: st, subject_id: sid, ...v });
      toast(t('acl.added'));
      reopen();
    }, { wide: true });
  };

  const handle = openDialog(t('erights.title', { title }), h('div', { class: 'ew-element-rights' },
    h('p', { class: 'ew-help' }, t('erights.help')),
    acl.hidden ? h('p', { class: 'ew-notice ew-notice--info', role: 'note' }, t('erights.hidden_note')) : null,
    settingsForm,
    h('h3', { id: captionId }, t('erights.rules')),
    table,
    button(t('erights.add'), () => void addRule(), { variant: 'primary' })), [], { wide: true });
}

/** elementRightsNote tells people who may edit or give rights that a page
 * has rights of its own. */
export function elementRightsNote(el: { own_rights_only?: boolean; own_rules?: boolean; permissions?: string[] | null }): HTMLElement | null {
  if (!(el.own_rights_only || el.own_rules) || !(el.permissions?.includes('edit') || el.permissions?.includes('grant'))) return null;
  return h('p', { class: 'ew-notice ew-notice--info', role: 'note' }, t(el.own_rights_only ? 'erights.note_own_only' : 'erights.note_rules'));
}
