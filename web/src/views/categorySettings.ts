// Category settings: texts, default page, permissions, history and rollback.
import { shortURLInput } from '../components/shorturl';
import { api } from '../core/api';
import { fmtDate, h, mount, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { PERMS, type AuditEntry, type Category, type ElementSummary, type Group, type Role, type Rule } from '../core/types';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, langFields, select, textInput } from '../components/ui';
import { breadcrumbs, can, invalidateTree, setContext } from '../components/widgets';
import { errorPage, splitTags } from './content';

interface CategoryResponse { category: Category; breadcrumbs: Category[]; elements: ElementSummary[] }

const TABS = ['general', 'permissions', 'history'] as const;

export async function categorySettingsView({ params, query, main }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  let data: CategoryResponse;
  try {
    data = await api.get<CategoryResponse>(`categories/${id}`);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  const c = data.category;
  setContext(c.id, c.path, c.permissions);
  const title = pick(c.titles, state.lang, state.site.default_language);
  const tab = (TABS as readonly string[]).includes(query.get('tab') ?? '') ? query.get('tab')! : 'general';
  const panel = h('div', { class: 'ew-tabpanel', id: 'ew-tabpanel', role: 'region', 'aria-label': t(`category.tab.${tab}`) });
  mount(main,
    breadcrumbs([{ title: t('nav.home'), href: '/' }, { title, href: `/c/${c.id}` }, { title: t('category.settings') }]),
    h('h1', null, t('category.settings_title', { title })),
    h('nav', { class: 'ew-tabs', 'aria-label': t('category.settings') },
      h('ul', null, ...TABS.map((tb) => h('li', null, h('a', { href: `/c/${c.id}/settings?tab=${tb}`, 'aria-current': tb === tab ? 'page' : undefined }, t(`category.tab.${tb}`)))))),
    panel);
  if (tab === 'general') general(panel, c, data.elements);
  if (tab === 'permissions') await permissions(panel, c);
  if (tab === 'history') await history(panel, c);
}

function general(panel: HTMLElement, c: Category, elements: ElementSummary[]): void {
  if (!can('edit')) {
    panel.append(h('p', null, t('error.forbidden')));
    return;
  }
  const titles = langFields(t('common.title'), c.titles, { required: true });
  const descs = langFields(t('common.description'), c.descriptions, { multiline: true });
  const tags = textInput({ value: c.tags.join(', ') });
  const short = shortURLInput(c.short_url);
  const def = select([{ value: '0', label: t('category.no_default') }, ...elements.map((e) => ({ value: String(e.id), label: pick(e.titles, state.lang, e.original_lang) }))], String(c.default_element_id ?? 0));
  const feed = select(['off', 'on', 'recursive'].map((v) => ({ value: v, label: t(`feed.setting_${v}`) })), c.feed ?? 'off');
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert, titles.el, descs.el,
    field(t('common.tags'), tags, { help: t('common.tags_help') }),
    field(t('shorturl.label'), short, { help: t('shorturl.help', { origin: location.origin }) }),
    field(t('category.default_page'), def, { help: t('category.default_page_help') }),
    field(t('feed.setting'), feed, { help: t('feed.setting_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put(`categories/${c.id}`, { titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value), short_url: short.value, default_element_id: Number(def.value), feed: feed.value });
      invalidateTree();
      toast(t('common.saved'));
    } catch (e) {
      alert.textContent = errorText(e);
    }
  });
  panel.append(form);
}

function permNames(bits: number): string {
  return PERMS.filter((p) => bits & p.bit).map((p) => t(`perm.${p.key}`)).join(', ');
}

async function permissions(panel: HTMLElement, c: Category): Promise<void> {
  if (!can('manage')) {
    panel.append(h('p', null, t('error.forbidden')));
    return;
  }
  let acl: { own: Rule[]; inherited: Rule[] };
  let groups: Group[] = [];
  let roles: Role[] = [];
  try {
    [acl, groups, roles] = await Promise.all([api.get<{ own: Rule[]; inherited: Rule[] }>(`categories/${c.id}/acl`), api.get<Group[]>('groups'), api.get<Role[]>('roles')]);
  } catch (e) {
    panel.append(h('p', { role: 'alert' }, errorText(e)));
    return;
  }
  const users = new Map<number, string>();
  const subject = (r: Rule): string => {
    switch (r.subject_type) {
      case 'guest': return t('acl.guest');
      case 'authenticated': return t('acl.authenticated');
      case 'group': return `${t('acl.group')}: ${groups.find((g) => g.id === r.subject_id)?.name ?? r.subject_id}`;
      case 'role': return `${t('acl.role')}: ${roles.find((x) => x.id === r.subject_id)?.name ?? r.subject_id}`;
      default: return `${t('acl.user')}: ${users.get(r.subject_id!) ?? '#' + r.subject_id}`;
    }
  };
  const table = (rules: Rule[], own: boolean, caption: string) => h('table', { class: 'ew-table' },
    h('caption', null, caption),
    h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('acl.subject')), h('th', { scope: 'col' }, t('acl.permissions')), h('th', { scope: 'col' }, t('acl.inherit')),
      own ? h('th', { scope: 'col' }, h('span', { class: 'sr-only' }, t('common.actions'))) : h('th', { scope: 'col' }, t('acl.from')))),
    h('tbody', null, ...(rules.length ? rules.map((r) => h('tr', null,
      h('th', { scope: 'row' }, subject(r)),
      h('td', null, permNames(r.permissions)),
      h('td', null, r.inherit ? t('common.yes') : t('common.no')),
      own ? h('td', null, button(t('common.remove'), async () => {
        if (!(await confirmDialog(t('acl.remove_confirm', { subject: subject(r) }), t('common.remove'), true))) return;
        try {
          await api.del(`acl/${r.id}`);
          toast(t('acl.removed'));
          navigate(location.pathname + location.search, { replace: true });
        } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: t('acl.remove_rule', { subject: subject(r) }) }))
        : h('td', null, h('a', { href: `/c/${r.category_id}/settings?tab=permissions` }, `#${r.category_id}`)))) : [h('tr', null, h('td', { colspan: '4' }, t('acl.none')))])));
  panel.append(
    h('p', null, t('acl.help')),
    button(t('acl.add'), () => addRuleDialog(c, groups, roles), { variant: 'primary' }),
    table(acl.own, true, t('acl.own_rules')),
    table(acl.inherited, false, t('acl.inherited_rules')));
}

function addRuleDialog(c: Category, groups: Group[], roles: Role[]): void {
  const type = select(['group', 'role', 'user', 'authenticated', 'guest'].map((x) => ({ value: x, label: t(`acl.${x}`) })), 'group');
  const groupSel = select(groups.map((g) => ({ value: String(g.id), label: g.name })));
  const roleSel = select(roles.map((r) => ({ value: String(r.id), label: r.name })));
  const userInput = textInput({ placeholder: t('acl.user_search') });
  const userList = h('datalist', { id: uid('users') });
  userInput.setAttribute('list', userList.id);
  const userMap = new Map<string, number>();
  userInput.addEventListener('input', async () => {
    if (userInput.value.length < 2) return;
    const res = await api.get<{ id: number; handle: string; display_name: string }[]>(`users/lookup?q=${encodeURIComponent(userInput.value)}`);
    userList.replaceChildren(...res.map((u) => { userMap.set(u.handle, u.id); return h('option', { value: u.handle }, u.display_name); }));
  });
  const subjectWrap = h('div');
  const showSubject = () => {
    subjectWrap.replaceChildren(
      type.value === 'group' ? field(t('acl.group'), groupSel) : type.value === 'role' ? field(t('acl.role'), roleSel)
        : type.value === 'user' ? h('div', null, field(t('acl.user'), userInput, { help: t('acl.user_help') }), userList) : h('span'));
  };
  type.addEventListener('change', showSubject);
  showSubject();
  const boxes = PERMS.map((p) => ({ p, cb: checkbox(t(`perm.${p.key}`), p.key === 'view', { help: t(`perm.${p.key}_help`) }) }));
  const inherit = checkbox(t('acl.inherit_label'), true);
  formDialog(t('acl.add'), [field(t('acl.subject_type'), type), subjectWrap,
    h('fieldset', null, h('legend', null, t('acl.permissions')), ...boxes.map((b) => b.cb.el)), inherit.el], t('acl.add'), async () => {
    let subjectId: number | null = null;
    if (type.value === 'group') subjectId = Number(groupSel.value);
    if (type.value === 'role') subjectId = Number(roleSel.value);
    if (type.value === 'user') {
      subjectId = userMap.get(userInput.value) ?? null;
      if (!subjectId) throw new Error(t('acl.user_not_found'));
    }
    const permsBits = boxes.filter((b) => b.cb.input.checked).reduce((a, b) => a | b.p.bit, 0);
    if (!permsBits) throw new Error(t('acl.choose_perm'));
    await api.post(`categories/${c.id}/acl`, { subject_type: type.value, subject_id: subjectId, permissions: permsBits, inherit: inherit.input.checked });
    toast(t('acl.added'));
    navigate(location.pathname + location.search, { replace: true });
  });
}

async function history(panel: HTMLElement, c: Category): Promise<void> {
  let entries: AuditEntry[];
  try {
    entries = await api.get<AuditEntry[]>(`categories/${c.id}/history`);
  } catch (e) {
    panel.append(h('p', { role: 'alert' }, errorText(e)));
    return;
  }
  if (can('edit') && can('delete')) {
    const at = h('input', { type: 'datetime-local', class: 'ew-input', required: true });
    const rec = checkbox(t('rollback.recursive'), true, { help: t('rollback.recursive_help') });
    const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
    const form = h('form', { class: 'ew-form ew-panel', novalidate: true },
      h('h2', null, t('rollback.title')), h('p', null, t('rollback.help')), alert,
      field(t('rollback.at'), at, { required: true }), rec.el,
      h('div', { class: 'ew-form-actions' }, button(t('rollback.submit'), () => undefined, { type: 'submit', variant: 'danger' })));
    form.addEventListener('submit', async (ev) => {
      ev.preventDefault();
      alert.textContent = '';
      if (!at.value) {
        alert.textContent = t('error.required');
        at.focus();
        return;
      }
      const when = new Date(at.value);
      if (!(await confirmDialog(t('rollback.confirm', { date: fmtDate(when.toISOString(), state.lang) }), t('rollback.submit'), true))) return;
      try {
        const res = await api.post<{ categories: number; revisions: number; trashed: number }>(`categories/${c.id}/rollback`, { at: when.toISOString(), recursive: rec.input.checked });
        toast(t('rollback.done', { categories: res.categories, revisions: res.revisions, trashed: res.trashed }));
        navigate(location.pathname + location.search, { replace: true });
      } catch (e) {
        alert.textContent = errorText(e);
      }
    });
    panel.append(form);
  }
  panel.append(h('h2', null, t('category.history')), h('table', { class: 'ew-table' },
    h('caption', { class: 'sr-only' }, t('category.history')),
    h('thead', null, h('tr', null, ...[t('common.date'), t('common.author'), t('audit.action'), t('audit.object')].map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...entries.map((e) => h('tr', null,
      h('td', null, fmtDate(e.at, state.lang)), h('td', null, e.actor), h('td', null, t(`audit.${e.action}`) === `audit.${e.action}` ? e.action : t(`audit.${e.action}`)),
      h('td', null, e.entity_type === 'element' && e.entity_id ? h('a', { href: `/e/${e.entity_id}/history` }, `${t('audit.element')} #${e.entity_id}`) : `${t('audit.category')} #${e.entity_id}`))))));
}

