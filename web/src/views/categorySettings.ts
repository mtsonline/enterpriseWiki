// Category settings: texts, default page, permissions, history and rollback.
import { shortOnlyBox, shortURLInput } from '../components/shorturl';
import { api } from '../core/api';
import { fmtDate, h, mount, uid } from '../core/dom';
import { pick, pluginName, t } from '../core/i18n';
import { navigate, rerender, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { loadCatalog, ruleEditor, summary, type PermDef } from '../components/permissions';
import { type AuditEntry, type Category, type ElementSummary, type Group, type SpaceInfo, type Role, type Rule } from '../core/types';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, langFields, select, textInput } from '../components/ui';
import { breadcrumbs, can, ctx, invalidateTree, setContext, setSpace } from '../components/widgets';
import { errorPage, splitTags } from './content';
import { listCategorySettingsTabs } from '../sdk';
import { insightsPanel, spaceSettings } from './spaces';

interface CategoryResponse { category: Category; breadcrumbs: Category[]; elements: ElementSummary[]; space?: SpaceInfo | null }

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
  setContext(c.id, c.path, c.permissions, c.effective_template);
  setSpace(data.space ?? null);
  const title = pick(c.titles, state.lang, state.site.default_language);
  // tabs of plugins (e.g. properties) where they apply
  const extra = listCategorySettingsTabs().filter((x) => x.visible?.(c) ?? true);
  // top-level categories are spaces
  const isSpace = !c.parent_id && can('manage');
  const ids = [...TABS.slice(0, 1), ...(isSpace ? ['space'] : []), ...TABS.slice(1), ...(can('manage') ? ['insights'] : []), ...extra.map((x) => x.id)];
  const tabLabel = (id: string) => extra.find((x) => x.id === id)?.label() ?? t(`category.tab.${id}`);
  const tab = ids.includes(query.get('tab') ?? '') ? query.get('tab')! : 'general';
  const panel = h('div', { class: 'ew-tabpanel', id: 'ew-tabpanel', role: 'region', 'aria-label': tabLabel(tab) });
  mount(main,
    breadcrumbs([{ title: t('nav.home'), href: '/' }, { title, href: `/c/${c.id}` }, { title: t('category.settings') }]),
    h('h1', null, t('category.settings_title', { title })),
    h('nav', { class: 'ew-tabs', 'aria-label': t('category.settings') },
      h('ul', null, ...ids.map((tb) => h('li', null, h('a', { href: `/c/${c.id}/settings?tab=${tb}`, 'aria-current': tb === tab ? 'page' : undefined }, tabLabel(tb)))))),
    panel);
  if (tab === 'general') general(panel, c, data.elements);
  if (tab === 'space' && isSpace) await spaceSettings(panel, c);
  if (tab === 'insights' && can('manage')) await insightsPanel(panel, c);
  if (tab === 'permissions') await permissions(panel, c);
  if (tab === 'history') await history(panel, c);
  const x = extra.find((e) => e.id === tab);
  if (x) {
    try { await x.render(panel, c); } catch (e) { panel.append(h('p', { role: 'alert' }, errorText(e))); }
  }
}

function general(panel: HTMLElement, c: Category, elements: ElementSummary[]): void {
  if (!can('manage')) {
    panel.append(h('p', null, t('error.forbidden')));
    return;
  }
  panel.append(archiveSection(c));
  if (c.archived) return; // read-only: texts and settings stay as they are
  const titles = langFields(t('common.title'), c.titles, { required: true });
  const descs = langFields(t('common.description'), c.descriptions, { multiline: true });
  const tags = textInput({ value: c.tags.join(', ') });
  const short = shortURLInput(c.short_url);
  const only = shortOnlyBox(!!c.short_only);
  const hidden = checkbox(t('category.hidden'), !!c.hidden, { help: t('category.hidden_help') });
  const def = select([{ value: '0', label: t('category.no_default') }, ...elements.map((e) => ({ value: String(e.id), label: pick(e.titles, state.lang, e.original_lang) }))], String(c.default_element_id ?? 0));
  const feed = select(['off', 'on', 'recursive'].map((v) => ({ value: v, label: t(`feed.setting_${v}`) })), c.feed ?? 'off');
  // classification: as above or a level (only a stricter one than above has an effect)
  const eff = c.effective_classification ?? 0;
  const cls = select([{ value: '-1', label: c.classification == null ? t('class.inherit', { level: t(`class.level.${eff}`) }) : t('class.inherit_plain') },
    ...[0, 1, 2, 3].map((l) => ({ value: String(l), label: t(`class.level.${l}`) }))], c.classification == null ? '-1' : String(c.classification));
  // files of confidential content: watermark and downloads (the original is always available as choice)
  const wm = select([{ value: '', label: t('protect.inherit') }, { value: 'auto', label: t('protect.wm_auto') }, { value: 'off', label: t('protect.wm_off') }], c.watermark ?? '');
  const dl = select([{ value: '', label: t('protect.inherit') }, { value: 'auto', label: t('protect.dl_auto') }, { value: 'allow', label: t('protect.dl_allow') }], c.downloads ?? '');
  // website template: inherited ('' = from above or the site template) or one of the active templates
  const tpls = state.site.templates ?? [];
  const tplName = (id: string) => pluginName(id, tpls.find((x) => x.id === id)?.name ?? id);
  const inherited = c.template ? '' : (c.effective_template || state.site.template);
  const tpl = select([{ value: '', label: inherited ? t('category.template_inherit_named', { name: tplName(inherited) }) : t('category.template_inherit') },
    ...tpls.map((x) => ({ value: x.id, label: tplName(x.id) + (x.id === state.site.template ? ` (${t('category.template_site')})` : '') })),
    ...(c.template && !tpls.some((x) => x.id === c.template) ? [{ value: c.template, label: t('category.template_inactive', { name: c.template }) }] : [])], c.template ?? '');
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert, titles.el, descs.el,
    field(t('common.tags'), tags, { help: t('common.tags_help') }),
    field(t('shorturl.label'), short, { help: t('shorturl.help', { origin: location.origin }) }), only.el, hidden.el,
    field(t('category.default_page'), def, { help: t('category.default_page_help') }),
    field(t('feed.setting'), feed, { help: t('feed.setting_help') }),
    field(t('category.template'), tpl, { help: t('category.template_help') }),
    field(t('class.label'), cls, { help: t('class.help') }),
    field(t('protect.wm'), wm, { help: t('protect.wm_help') }),
    field(t('protect.dl'), dl, { help: t('protect.dl_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      const saved = await api.put<Category>(`categories/${c.id}`, { titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value), short_url: short.value, short_only: only.input.checked, hidden: hidden.input.checked, default_element_id: Number(def.value), feed: feed.value, template: tpl.value, classification: Number(cls.value), watermark: wm.value, downloads: dl.value });
      invalidateTree();
      c.template = saved.template;
      ctx.template = saved.effective_template ?? '';
      document.dispatchEvent(new CustomEvent('ewiki:template'));
      toast(t('common.saved'));
    } catch (e) {
      alert.textContent = errorText(e);
    }
  });
  panel.append(form);
}


/** Archive (read-only, out of navigation and search) or restore. */
function archiveSection(c: Category): HTMLElement {
  const sec = h('section', { class: 'ew-section', 'aria-labelledby': 'ew-archive-h' }, h('h2', { id: 'ew-archive-h' }, t('archive.title')));
  if (c.archived && !c.archived_at) {
    sec.append(h('p', null, t('archive.above')));
    return sec;
  }
  const on = !c.archived_at;
  sec.append(h('p', null, on ? t('archive.help') : t('archive.restore_help')),
    button(on ? t('archive.archive') : t('archive.restore'), async () => {
      if (on && !(await confirmDialog(t('archive.confirm'), t('archive.archive')))) return;
      try {
        await api.post(`categories/${c.id}/archive`, { archived: on });
        invalidateTree();
        toast(on ? t('archive.done') : t('archive.restored'));
        void rerender();
      } catch (e) {
        toast(errorText(e));
      }
    }, { variant: on ? 'secondary' : 'primary' }));
  return sec;
}

async function permissions(panel: HTMLElement, c: Category): Promise<void> {
  if (!can('manage')) {
    panel.append(h('p', null, t('error.forbidden')));
    return;
  }
  let acl: { own: Rule[]; inherited: Rule[] };
  let groups: Group[] = [];
  let roles: Role[] = [];
  let defs: PermDef[] = [];
  try {
    let catalog;
    [acl, groups, roles, catalog] = await Promise.all([api.get<{ own: Rule[]; inherited: Rule[] }>(`categories/${c.id}/acl`), api.get<Group[]>('groups'), api.get<Role[]>('roles'), loadCatalog()]);
    defs = catalog.permissions;
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
      h('td', null, summary(r.permissions, r.actions ?? [], defs)),
      h('td', null, r.inherit ? t('common.yes') : t('common.no')),
      own ? h('td', { class: 'ew-actions' }, button(t('common.edit'), () => void editRuleDialog(r, subject(r)), { variant: 'link', ariaLabel: t('acl.edit_rule', { subject: subject(r) }) }), button(t('common.remove'), async () => {
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
    (await accessRequests(c)) ?? '',
    button(t('acl.add'), () => void addRuleDialog(c, groups, roles), { variant: 'primary' }),
    table(acl.own, true, t('acl.own_rules')),
    table(acl.inherited, false, t('acl.inherited_rules')));
}

/** Open access requests: grant (a permission rule for the person) or decline. */
async function accessRequests(c: Category): Promise<HTMLElement | null> {
  let list: { id: number; user: string; message: string; created_at: string }[] = [];
  try { list = await api.get<typeof list>(`categories/${c.id}/access-requests`); } catch { return null; }
  if (!list.length) return null;
  const sec = h('section', { class: 'ew-section ew-access-requests', 'aria-labelledby': 'ew-ar-h' }, h('h2', { id: 'ew-ar-h' }, t('access.open', { n: list.length })));
  const ul = h('ul', { class: 'ew-plain-list' });
  for (const r of list) {
    const answer = async (grant: boolean, preset?: string) => {
      try {
        await api.post(`access-requests/${r.id}`, { grant, preset });
        toast(grant ? t('access.granted', { user: r.user }) : t('access.declined'));
        void rerender();
      } catch (e) { toast(errorText(e), 'error'); }
    };
    ul.append(h('li', { class: 'ew-access-request' },
      h('p', null, h('strong', null, r.user), ' · ', fmtDate(r.created_at, state.lang)),
      r.message ? h('blockquote', null, r.message) : null,
      h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('access.answer_for', { user: r.user }) },
        button(t('access.grant_read'), () => void answer(true, 'reader'), { variant: 'primary', ariaLabel: `${t('access.grant_read')}: ${r.user}` }),
        button(t('access.grant_edit'), () => void answer(true, 'editor'), { variant: 'secondary', ariaLabel: `${t('access.grant_edit')}: ${r.user}` }),
        button(t('access.decline'), () => void answer(false), { variant: 'link', ariaLabel: `${t('access.decline')}: ${r.user}` }))));
  }
  sec.append(ul);
  return sec;
}

/** editRuleDialog changes the permissions of a rule. */
export async function editRuleDialog(r: Rule, subjectName: string, done?: () => void): Promise<void> {
  const ed = await ruleEditor(r.permissions, r.actions ?? [], { guest: r.subject_type === 'guest' });
  const inherit = checkbox(t('acl.inherit_label'), r.inherit);
  formDialog(t('acl.edit_title', { subject: subjectName }), [ed.el, inherit.el], t('common.save'), async () => {
    const v = ed.value();
    if (!v.permissions && !v.actions.length) throw new Error(t('acl.choose_perm'));
    await api.put(`acl/${r.id}`, { ...v, inherit: inherit.input.checked });
    toast(t('common.saved'));
    if (done) done(); else navigate(location.pathname + location.search, { replace: true });
  });
}

async function addRuleDialog(c: Category, groups: Group[], roles: Role[]): Promise<void> {
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
  const ed = await ruleEditor(1, []);
  const inherit = checkbox(t('acl.inherit_label'), true);
  formDialog(t('acl.add'), [field(t('acl.subject_type'), type), subjectWrap, ed.el, inherit.el], t('acl.add'), async () => {
    let subjectId: number | null = null;
    if (type.value === 'group') subjectId = Number(groupSel.value);
    if (type.value === 'role') subjectId = Number(roleSel.value);
    if (type.value === 'user') {
      subjectId = userMap.get(userInput.value) ?? null;
      if (!subjectId) throw new Error(t('acl.user_not_found'));
    }
    const v = ed.value();
    if (!v.permissions && !v.actions.length) throw new Error(t('acl.choose_perm'));
    if (type.value === 'guest' && (v.permissions & ~1 || v.actions.length)) throw new Error(t('acl.guest_view_only'));
    await api.post(`categories/${c.id}/acl`, { subject_type: type.value, subject_id: subjectId, ...v, inherit: inherit.input.checked });
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

