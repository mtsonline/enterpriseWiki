// Centrally assigned watches: administrators (permission core.watches.assign)
// let users, groups or roles watch categories or pages. Mandatory
// assignments cannot be unsubscribed; optional ones can (in the profile).
import { api } from '../core/api';
import { newest } from '../core/newest';
import { fmtDate, h, mount, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select, textArea, textInput } from '../components/ui';
import { categorySelect, elementSelect } from '../components/pickers';

export interface Assignment {
  id: number; subject_type: 'user' | 'group' | 'role'; subject_id: number; subject_name: string;
  target_type: 'category' | 'element'; target_id: number; target_titles: Record<string, string>;
  mode: string; recursive: boolean; frequency: string; mandatory: boolean; note: string;
  created_by: string; created_at: string; members: number; unsubscribed?: boolean;
}

interface Named { id: number; name: string }

const title = (a: Assignment) => pick(a.target_titles, state.lang, state.site.default_language) || t('watch.deleted');
const href = (a: Assignment) => (a.target_type === 'category' ? `/c/${a.target_id}` : `/e/${a.target_id}`);

export async function watchesAdmin(panel: HTMLElement): Promise<void> {
  const [list, groups, roles] = await Promise.all([
    api.get<Assignment[]>('admin/watch-assignments'), api.get<Named[]>('groups'), api.get<Named[]>('roles'),
  ]);
  const rows = list.map((a) => h('tr', null,
    h('th', { scope: 'row' }, `${t(`acl.${a.subject_type}`)}: ${a.subject_name}`),
    h('td', null, t(`watch.type.${a.target_type}`), ': ', h('a', { href: href(a) }, title(a)), a.target_type === 'category' && a.recursive ? ` (${t('wassign.with_sub')})` : ''),
    h('td', null, t(a.mode === 'content' ? 'watch.mode_content' : 'watch.mode_link'), ' · ', t(`watch.freq_${a.frequency || 'instant'}`)),
    h('td', null, h('span', { class: `ew-badge ${a.mandatory ? 'ew-badge--warn' : 'ew-badge--ok'}` }, t(a.mandatory ? 'wassign.mandatory' : 'wassign.optional'))),
    h('td', null, String(a.members)),
    h('td', null, a.created_by || '–', h('br'), h('small', null, fmtDate(a.created_at, state.lang))),
    h('td', { class: 'ew-actions' },
      button(t('common.edit'), () => void assignmentDialog(a, groups, roles), { variant: 'link', ariaLabel: `${t('common.edit')}: ${a.subject_name} – ${title(a)}` }),
      button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('wassign.delete_confirm', { subject: a.subject_name, title: title(a) }), t('common.delete'), true))) return;
        try { await api.del(`admin/watch-assignments/${a.id}`); toast(t('admin.deleted')); reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${a.subject_name} – ${title(a)}` }))));
  mount(panel,
    pageHeader(t('admin.watches'), button(t('wassign.create'), () => void assignmentDialog(null, groups, roles), { variant: 'primary' })),
    h('p', null, t('wassign.help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('admin.watches')),
      h('thead', null, h('tr', null, ...[t('acl.subject'), t('wassign.target'), t('watch.mode'), t('wassign.kind'), t('wassign.members'), t('wassign.created'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '7' }, t('wassign.none')))]))));
}

const reload = () => navigate(location.pathname + location.search, { replace: true });

async function assignmentDialog(a: Assignment | null, groups: Named[], roles: Named[]): Promise<void> {
  // subject
  const sType = select(['group', 'role', 'user'].map((x) => ({ value: x, label: t(`acl.${x}`) })), a?.subject_type ?? 'group');
  const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })), a?.subject_type === 'group' ? String(a.subject_id) : undefined);
  const roleSel = select(roles.map((r) => ({ value: String(r.id), label: r.name })), a?.subject_type === 'role' ? String(a.subject_id) : undefined);
  const userInput = textInput({ placeholder: t('acl.user_search'), value: a?.subject_type === 'user' ? a.subject_name : '' });
  const userList = h('datalist', { id: uid('wausers') });
  userInput.setAttribute('list', userList.id);
  const userMap = new Map<string, number>();
  if (a?.subject_type === 'user') userMap.set(a.subject_name, a.subject_id);
  const freshUsers = newest();
  userInput.addEventListener('input', async () => {
    const current = freshUsers();
    if (userInput.value.length < 2) return;
    try {
      const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(userInput.value)}`);
      if (!current()) return;
      userList.replaceChildren(...res.map((u) => { userMap.set(u.handle, u.id); return h('option', { value: u.handle }, u.display_name); }));
    } catch { /* lookup is optional */ }
  });
  const subjectWrap = h('div');
  const showSubject = () => subjectWrap.replaceChildren(
    sType.value === 'group' ? field(t('acl.group'), groupSel) : sType.value === 'role' ? field(t('acl.role'), roleSel)
      : h('div', null, field(t('acl.user'), userInput, { help: t('acl.user_help') }), userList));
  sType.addEventListener('change', showSubject);
  showSubject();
  // target
  const tType = select(['category', 'element'].map((x) => ({ value: x, label: t(`watch.type.${x}`) })), a?.target_type ?? 'category');
  const cat = await categorySelect({ selected: a?.target_type === 'category' ? a.target_id : undefined, perm: 'view' });
  const el = await elementSelect({ selected: a?.target_type === 'element' ? a.target_id : undefined });
  const recursive = checkbox(t('watch.recursive'), a?.recursive ?? true);
  const targetWrap = h('div');
  const showTarget = () => targetWrap.replaceChildren(...(tType.value === 'category'
    ? [cat.field(t('watch.type.category'), { required: true }), recursive.el]
    : [h('div', { class: 'ew-field' }, h('span', { class: 'ew-label' }, t('watch.type.element')), el.el)]));
  tType.addEventListener('change', showTarget);
  showTarget();
  // options
  const mode = select([{ value: 'link', label: t('watch.mode_link') }, { value: 'content', label: t('watch.mode_content') }], a?.mode ?? 'link');
  const freq = select(['instant', 'daily', 'weekly'].map((x) => ({ value: x, label: t(`watch.freq_${x}`) })), a?.frequency || 'instant');
  const mandatory = h('fieldset', null, h('legend', null, t('wassign.kind')),
    ...[['optional', 'wassign.optional_help'], ['mandatory', 'wassign.mandatory_help']].map(([v, help]) => {
      const id = uid('wa-kind');
      return h('div', { class: 'ew-check' },
        h('input', { type: 'radio', name: 'wa-kind', id, value: v, checked: (v === 'mandatory') === (a?.mandatory ?? false) }),
        h('label', { for: id }, t(`wassign.${v}`)), h('small', { class: 'ew-help' }, t(help)));
    }));
  const note = textArea({ value: a?.note ?? '', rows: 2 });
  formDialog(a ? t('wassign.edit') : t('wassign.create'), [
    h('fieldset', null, h('legend', null, t('acl.subject')), field(t('acl.subject_type'), sType), subjectWrap),
    h('fieldset', null, h('legend', null, t('wassign.target')), field(t('watch.type_col'), tType), targetWrap),
    field(t('watch.mode'), mode), field(t('watch.frequency'), freq, { help: t('watch.frequency_help') }), mandatory,
    field(t('wassign.note'), note, { help: t('wassign.note_help') }),
  ], t('common.save'), async () => {
    let subjectId = 0;
    if (sType.value === 'group') subjectId = Number(groupSel.value);
    else if (sType.value === 'role') subjectId = Number(roleSel.value);
    else subjectId = userMap.get(userInput.value) ?? 0;
    if (!subjectId) throw new Error(t(sType.value === 'user' ? 'acl.user_not_found' : 'wassign.choose_subject'));
    const targetId = tType.value === 'category' ? cat.value() : el.value();
    if (!targetId) throw new Error(t('wassign.choose_target'));
    const kind = (mandatory.querySelector('input:checked') as HTMLInputElement | null)?.value;
    const body = {
      subject_type: sType.value, subject_id: subjectId, target_type: tType.value, target_id: targetId,
      mode: mode.value, recursive: tType.value === 'category' ? recursive.input.checked : false, frequency: freq.value,
      mandatory: kind === 'mandatory', note: note.value,
    };
    if (a) await api.put(`admin/watch-assignments/${a.id}`, body);
    else await api.post('admin/watch-assignments', body);
    toast(t('common.saved'));
    reload();
  }, { wide: true });
}

/** The profile section listing watches assigned to the user. */
export async function assignedWatchesSection(): Promise<HTMLElement | null> {
  let list: Assignment[];
  try {
    list = await api.get<Assignment[]>('me/watch-assignments');
  } catch {
    return null;
  }
  if (!list.length) return null;
  const sec = h('section', { class: 'ew-section', 'aria-labelledby': 'wa-h' });
  const render = () => mount(sec, h('h2', { id: 'wa-h' }, t('wassign.profile_title')), h('p', null, t('wassign.profile_help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('wassign.profile_title')),
      h('thead', null, h('tr', null, ...[t('common.title'), t('watch.mode'), t('wassign.via'), t('common.status')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...list.map((a) => {
        const name = title(a);
        let status: HTMLElement;
        if (a.mandatory) status = h('span', { class: 'ew-badge ew-badge--warn' }, t('wassign.mandatory'));
        else {
          const cb = checkbox(t('wassign.subscribed'), !a.unsubscribed);
          cb.input.setAttribute('aria-label', `${t('wassign.subscribed')}: ${name}`);
          cb.input.addEventListener('change', async () => {
            try {
              await api.put(`me/watch-assignments/${a.id}`, { subscribed: cb.input.checked });
              a.unsubscribed = !cb.input.checked;
              toast(t(cb.input.checked ? 'wassign.resubscribed' : 'wassign.unsubscribed'));
            } catch (e) {
              cb.input.checked = !cb.input.checked;
              toast(errorText(e), 'error');
            }
          });
          status = cb.el;
        }
        return h('tr', null,
          h('th', { scope: 'row' }, h('a', { href: href(a) }, name), ' ', h('small', null, `(${t(`watch.type.${a.target_type}`)})`), a.note ? h('div', { class: 'ew-help' }, a.note) : null),
          h('td', null, t(a.mode === 'content' ? 'watch.mode_content' : 'watch.mode_link'), ' · ', t(`watch.freq_${a.frequency || 'instant'}`)),
          h('td', null, `${t(`acl.${a.subject_type}`)}: ${a.subject_name}`),
          h('td', null, status));
      }))));
  render();
  return sec;
}
