// Pickers for people, groups and roles: chips with a search field (users),
// and an editor for a list of subjects (who may see or change something).
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import type { Group, Role } from '../core/types';
import { button, field, select, textInput } from './ui';

export interface Subject { type: 'user' | 'group' | 'role' | 'authenticated' | 'guest'; id?: number }

let lists: Promise<{ groups: Group[]; roles: Role[] }> | null = null;
function groupsAndRoles() {
  lists ??= Promise.all([api.get<Group[]>('groups').catch(() => []), api.get<Role[]>('roles').catch(() => [])]).then(([groups, roles]) => ({ groups, roles }));
  return lists;
}

interface Chip { id: number; name: string }

/** Chips of chosen items with remove buttons; returns the list element and a redraw. */
function chips(label: string, items: Chip[], onChange: () => void): { el: HTMLElement; draw: () => void } {
  const el = h('ul', { class: 'ew-chips', 'aria-label': label });
  const draw = () => el.replaceChildren(...items.map((c, i) => h('li', { class: 'ew-chip' }, h('span', null, c.name),
    button('×', () => { items.splice(i, 1); draw(); onChange(); }, { variant: 'subtle', ariaLabel: t('common.remove_item', { name: c.name }) }))));
  draw();
  return { el, draw };
}

/** Picks one or more users by searching (name or user name). */
export function userPicker(opts: { label: string; multiple?: boolean; initial?: Chip[]; help?: string; required?: boolean }): { el: HTMLElement; value: () => number[]; focus: () => void } {
  const items: Chip[] = [...(opts.initial ?? [])];
  const input = textInput({ placeholder: t('acl.user_search'), autocomplete: 'off' });
  const dl = h('datalist', { id: uid('users') });
  input.setAttribute('list', dl.id);
  const found = new Map<string, Chip>();
  const status = h('span', { class: 'sr-only', 'aria-live': 'polite' });
  let list: ReturnType<typeof chips>;
  const sync = () => { input.hidden = !opts.multiple && items.length > 0; };
  list = chips(opts.label, items, sync);
  const take = () => {
    const c = found.get(input.value);
    if (!c) return;
    if (!items.some((x) => x.id === c.id)) items.push(c);
    input.value = '';
    list.draw();
    sync();
    status.textContent = t('common.added_item', { name: c.name });
  };
  let timer = 0;
  input.addEventListener('input', () => {
    if (found.has(input.value)) { take(); return; }
    window.clearTimeout(timer);
    if (input.value.trim().length < 2) return;
    timer = window.setTimeout(async () => {
      try {
        const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(input.value.trim())}`);
        dl.replaceChildren(...res.map((u) => {
          const key = `${u.display_name} (${u.handle})`;
          found.set(key, { id: u.id, name: u.display_name || u.handle });
          return h('option', { value: key });
        }));
      } catch { /* no search for guests */ }
    }, 200);
  });
  input.addEventListener('keydown', (ev) => { if (ev.key === 'Enter') { ev.preventDefault(); take(); } });
  sync();
  const box = h('div', { class: 'ew-picker' }, list.el, input, dl, status);
  return { el: field(opts.label, box, { help: opts.help, required: opts.required }), value: () => items.map((x) => x.id), focus: () => input.focus() };
}

/** Picks one or more groups. */
export async function groupPicker(opts: { label: string; multiple?: boolean; initial?: number[]; help?: string }): Promise<{ el: HTMLElement; value: () => number[] }> {
  const { groups } = await groupsAndRoles();
  if (!opts.multiple) {
    const s = select([{ value: '', label: '—' }, ...groups.map((g) => ({ value: String(g.id), label: g.name }))], opts.initial?.[0] ? String(opts.initial[0]) : '');
    return { el: field(opts.label, s, { help: opts.help }), value: () => (s.value ? [Number(s.value)] : []) };
  }
  const s = select(groups.map((g) => ({ value: String(g.id), label: g.name })));
  s.multiple = true;
  s.size = Math.min(6, Math.max(3, groups.length));
  for (const o of Array.from(s.options)) o.selected = !!opts.initial?.includes(Number(o.value));
  return { el: field(opts.label, s, { help: opts.help }), value: () => Array.from(s.selectedOptions).map((o) => Number(o.value)) };
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
