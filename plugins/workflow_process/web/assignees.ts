// Who handles a step: the person who started the case, people, groups and
// roles together; any one of them may act (approvals count all of them).
// Groups and roles are kept as "#ID", so they stay valid when renamed.
// Names (from a workflow template or written by hand) are matched with this
// wiki; those it does not know stay in the list with a warning, so nothing
// is lost unseen.
import { button, checkbox, groupPicker, h, knownGroupsAndRoles, rolePicker, t, userPicker } from '@ewiki/coreplugin';
import type { Assignee } from './types';

type Named = { id: number; name: string };

/** The id of a group or role given as "#ID" or by name (0: not in this wiki). */
function idOf(list: Named[], value: string): number {
  const v = value.trim();
  const id = v.startsWith('#') ? Number(v.slice(1)) : (list.find((x) => x.name.toLowerCase() === v.toLowerCase())?.id ?? 0);
  return list.some((x) => x.id === id) ? id : 0;
}

/** Edits the assignees of a step; set receives the whole list after each change. */
export function assigneesEditor(get: () => Assignee[], set: (list: Assignee[]) => void): HTMLElement {
  const box = h('fieldset', { class: 'ew-fieldset ewf-assignees' }, h('legend', null, t('workflow.assignee')),
    h('p', { class: 'ew-help' }, t('workflow.assignees_help')));
  void (async () => {
    const { groups, roles } = await knownGroupsAndRoles();
    const start = get();
    const groupIds: number[] = [];
    const roleIds: number[] = [];
    const unknown: Assignee[] = [];
    for (const a of start) {
      if (a.type !== 'group' && a.type !== 'role') continue;
      const id = idOf(a.type === 'group' ? groups : roles, a.value);
      if (id) (a.type === 'group' ? groupIds : roleIds).push(id);
      else unknown.push({ ...a });
    }
    const update = () => set([
      ...(starter.input.checked ? [{ type: 'starter' as const, value: '' }] : []),
      ...people.handles().map((v) => ({ type: 'user' as const, value: v })),
      ...groupList.value().map((id) => ({ type: 'group' as const, value: `#${id}` })),
      ...roleList.value().map((id) => ({ type: 'role' as const, value: `#${id}` })),
      ...unknown,
    ]);
    const starter = checkbox(t('workflow.assignee.starter'), start.some((a) => a.type === 'starter'));
    starter.input.addEventListener('change', update);
    // people already chosen are known by their user name only (negative ids keep them apart)
    const people = userPicker({ label: t('workflow.assignee.people'), multiple: true, onChange: update,
      initial: start.filter((a) => a.type === 'user').map((a, i) => ({ id: -(i + 1), name: a.value, handle: a.value })) });
    const groupList = await groupPicker({ label: t('workflow.assignee.groups'), multiple: true, initial: groupIds, onChange: update });
    const roleList = await rolePicker({ label: t('workflow.assignee.roles'), multiple: true, initial: roleIds, onChange: update });
    const missing = h('ul', { class: 'ewf-assignees-unknown' });
    const drawMissing = () => missing.replaceChildren(...unknown.map((a, i) => h('li', { class: 'ew-notice ew-notice--warning' },
      t('workflow.assignee_unknown', { kind: t(`workflow.assignee.${a.type}`), name: a.value }), ' ',
      button(t('common.remove'), () => { unknown.splice(i, 1); drawMissing(); update(); },
        { variant: 'subtle', ariaLabel: t('common.remove_item', { name: a.value }) }))));
    drawMissing();
    box.append(starter.el, people.el, groupList.el, roleList.el, missing);
  })();
  return box;
}
