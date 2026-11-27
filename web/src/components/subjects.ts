// Pickers for people, groups and roles (on top of choiceList: people are
// searched, groups are checkboxes or a list with a filter),
// and an editor for a list of subjects (who may see or change something).
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import type { Group, Role } from '../core/types';
import { type Choice, choiceList } from './choicelist';
import { button, field, select } from './ui';

export interface Subject { type: 'user' | 'group' | 'role' | 'authenticated' | 'guest'; id?: number }

let lists: Promise<{ groups: Group[]; roles: Role[] }> | null = null;
function groupsAndRoles() {
  lists ??= Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]).then(([groups, roles]) => ({ groups, roles }));
  return lists;
}

interface Chip { id: number; name: string; handle?: string }

/** Looks people up by name or user name (at least SEARCH_MIN letters; the server limits the answer). */
export async function searchUsers(q: string): Promise<Choice[]> {
  const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(q)}`);
  return res.map((u) => ({ id: u.id, name: u.display_name || u.handle, detail: u.handle, handle: u.handle }));
}

/** Picks one or more users by searching (name or user name). */
export function userPicker(opts: { label: string; multiple?: boolean; initial?: Chip[]; help?: string; required?: boolean }): { el: HTMLElement; value: () => number[]; names: () => string[]; handles: () => string[]; focus: () => void } {
  const list = choiceList({ label: opts.label, search: searchUsers, multiple: !!opts.multiple, selected: opts.initial ?? [], help: opts.help, required: opts.required });
  return { el: list.el, value: list.value, names: () => list.chosen().map((x) => x.name), handles: () => list.chosen().flatMap((x) => (x.handle ? [x.handle] : [])), focus: list.focus };
}

/** Picks one or more groups (several: checkboxes or a list with a filter, see choiceList). */
export async function groupPicker(opts: { label: string; multiple?: boolean; initial?: number[]; help?: string }): Promise<{ el: HTMLElement; value: () => number[] }> {
  const { groups } = await groupsAndRoles();
  if (!opts.multiple) {
    const s = select([{ value: '', label: '—' }, ...groups.map((g) => ({ value: String(g.id), label: g.name }))], opts.initial?.[0] ? String(opts.initial[0]) : '');
    return { el: field(opts.label, s, { help: opts.help }), value: () => (s.value ? [Number(s.value)] : []) };
  }
  const list = choiceList({ label: opts.label, items: groups.map((g) => ({ id: g.id, name: g.name })), selected: opts.initial, help: opts.help });
  return { el: list.el, value: list.value };
}

/** Edits a list of subjects (users, groups, roles, all signed-in people, everybody). */
export async function subjectsEditor(legend: string, help: string, initial: Subject[], names: Record<string, string> = {}): Promise<{ el: HTMLElement; value: () => Subject[] }> {
  const { groups, roles } = await groupsAndRoles();
  const list = h('ul', { class: 'ew-subjects' });
  const rows: { value: () => Subject | null }[] = [];
  const addRow = (s: Subject) => {
    const type = select((['group', 'role', 'user', 'authenticated', 'guest'] as const).map((x) => ({ value: x, label: t(`acl.${x}`) })), s.type);
    type.setAttribute('aria-label', t('acl.subject_type'));
    const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })), s.type === 'group' ? String(s.id) : undefined);
    groupSel.setAttribute('aria-label', t('acl.group'));
    const roleSel = select(roles.map((r) => ({ value: String(r.id), label: r.name })), s.type === 'role' ? String(s.id) : undefined);
    roleSel.setAttribute('aria-label', t('acl.role'));
    const user = userPicker({ label: t('acl.user'), initial: s.type === 'user' && s.id ? [{ id: s.id, name: names[`user:${s.id}`] ?? `#${s.id}` }] : [] });
    const who = h('span', { class: 'ew-subject-who' });
    const show = () => who.replaceChildren(type.value === 'group' ? groupSel : type.value === 'role' ? roleSel : type.value === 'user' ? user.el : '');
    type.addEventListener('change', show);
    show();
    const entry = {
      value: (): Subject | null => {
        switch (type.value) {
          case 'group': return groupSel.value ? { type: 'group', id: Number(groupSel.value) } : null;
          case 'role': return roleSel.value ? { type: 'role', id: Number(roleSel.value) } : null;
          case 'user': { const id = user.value()[0]; return id ? { type: 'user', id } : null; }
          default: return { type: type.value as Subject['type'] };
        }
      },
    };
    const row: HTMLElement = h('li', { class: 'ew-subject' }, type, who,
      button(t('common.remove'), () => { row.remove(); rows.splice(rows.indexOf(entry), 1); }, { variant: 'subtle' }));
    rows.push(entry);
    list.append(row);
  };
  initial.forEach(addRow);
  const add = button(t('acl.add_subject'), () => addRow({ type: 'group', id: groups[0]?.id }), { variant: 'secondary' });
  return {
    el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, legend), h('p', { class: 'ew-help' }, help), list, add),
    value: () => rows.map((r) => r.value()).filter((x): x is Subject => x !== null),
  };
}
