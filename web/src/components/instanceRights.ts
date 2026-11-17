// Permissions of one instance of a plugin (a blog, a FAQ …): the own ones
// for everybody, all rules for those who manage the instance.
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import type { Group, Role } from '../core/types';
import { button, checkbox, errorText, field, formDialog, openDialog, select } from './ui';
import { userPicker } from './subjects';

interface PermDef { key: string; label: string; description?: string }
interface Rule { subject_type: 'user' | 'group' | 'role' | 'authenticated'; subject_id: number | null; actions: string[]; name?: string }
interface Rights { type: string; available: PermDef[]; mine: string[]; manage: boolean; rules?: Rule[] }

const label = (d: PermDef) => t(`perm.${d.key}`) || d.label;
const help = (d: PermDef) => t(`perm.${d.key}_help`) || d.description || '';

/** Opens the permissions of instance id (title for the heading). */
export async function instanceRightsDialog(id: number, title: string): Promise<void> {
  let r: Rights;
  try { r = await api.get<Rights>(`elements/${id}/rights`); } catch (e) { toast(errorText(e), 'error'); return; }
  const heading = t('instance.rights_title', { title });
  if (!r.manage) {
    openDialog(heading, h('div', null,
      h('p', null, r.mine.length ? t('instance.rights_mine') : t('instance.rights_none')),
      r.mine.length ? h('ul', null, ...r.available.filter((d) => r.mine.includes(d.key)).map((d) => h('li', null, label(d)))) : null));
    return;
  }
  const [groups, roles] = await Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]);
  const subjectName = (x: Rule) => x.subject_type === 'authenticated' ? t('acl.authenticated')
    : `${t(`acl.${x.subject_type}`)}: ${x.name || (x.subject_type === 'group' ? groups.find((g) => g.id === x.subject_id)?.name : x.subject_type === 'role' ? roles.find((o) => o.id === x.subject_id)?.name : '') || `#${x.subject_id}`}`;
  const rows: { rule: Rule; boxes: Map<string, HTMLInputElement> }[] = [];
  const list = h('div', { class: 'ew-instance-rules' });
  const draw = () => list.replaceChildren(...(rows.length ? rows.map((row) => {
    const legendId = uid('rule');
    return h('fieldset', { class: 'ew-fieldset', 'aria-labelledby': legendId },
      h('legend', { id: legendId }, subjectName(row.rule)),
      ...r.available.map((d) => {
        const cb = checkbox(label(d), row.rule.actions.includes(d.key), { help: help(d) });
        // nobody grants what they do not have themselves
        cb.input.disabled = !r.mine.includes(d.key);
        row.boxes.set(d.key, cb.input);
        return cb.el;
      }),
      button(t('instance.remove_rule'), () => { rows.splice(rows.indexOf(row), 1); draw(); },
        { variant: 'subtle', ariaLabel: t('acl.remove_rule', { subject: subjectName(row.rule) }) }));
  }) : [h('p', null, t('acl.none'))]));
  for (const rule of r.rules ?? []) rows.push({ rule, boxes: new Map() });
  draw();
  // add a rule for a person, group, role or all signed-in people
  const type = select((['group', 'role', 'user', 'authenticated'] as const).map((x) => ({ value: x, label: t(`acl.${x}`) })), 'group');
  const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })));
  const roleSel = select(roles.map((o) => ({ value: String(o.id), label: o.name })));
  const user = userPicker({ label: t('acl.user') });
  const who = h('div');
  const show = () => who.replaceChildren(type.value === 'group' ? field(t('acl.group'), groupSel) : type.value === 'role' ? field(t('acl.role'), roleSel) : type.value === 'user' ? user.el : '');
  type.addEventListener('change', show);
  show();
  const add = button(t('instance.add_rule'), () => {
    const st = type.value as Rule['subject_type'];
    const sid = st === 'group' ? Number(groupSel.value) || null : st === 'role' ? Number(roleSel.value) || null : st === 'user' ? user.value()[0] ?? null : null;
    if (st !== 'authenticated' && !sid) { toast(t(st === 'user' ? 'acl.user_not_found' : 'instance.choose_subject'), 'error'); return; }
    if (rows.some((x) => x.rule.subject_type === st && x.rule.subject_id === sid)) { toast(t('instance.rule_exists'), 'error'); return; }
    rows.push({ rule: { subject_type: st, subject_id: sid, actions: [], name: st === 'user' ? user.names()[0] : undefined }, boxes: new Map() });
    draw();
    toast(t('instance.rule_added_choose'));
  }, { variant: 'secondary' });
  const adder = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('instance.add_rule')), field(t('acl.subject_type'), type), who, add);
  formDialog(heading, [h('p', { class: 'ew-help' }, t('instance.rights_help')), list, adder], t('common.save'), async () => {
    const rules = rows.map((row) => ({ subject_type: row.rule.subject_type, subject_id: row.rule.subject_id,
      actions: r.available.map((d) => d.key).filter((k) => row.boxes.get(k)?.checked) }));
    if (rules.some((x) => !x.actions.length)) throw new Error(t('acl.choose_perm'));
    await api.put(`elements/${id}/rights`, { rules });
    toast(t('instance.rights_saved'));
  }, { wide: true });
}

/** instanceRightsButton: page action for elements with permissions of their own. */
export function instanceRightsButton(target: { type: 'element' | 'category'; id: number; title: string; elementType?: string }): HTMLElement | null {
  if (target.type !== 'element' || !state.user) return null;
  const info = state.site.element_types.find((x) => x.id === target.elementType);
  if (!info?.instance) return null;
  return button(t('instance.rights'), () => void instanceRightsDialog(target.id, target.title), { variant: 'secondary' });
}
