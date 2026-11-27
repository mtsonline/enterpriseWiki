// Administration → Templates: the templates of the website in tabs. The
// overview shows every template as a card with its schematic for the device
// chosen (desktop, tablet, phone upright and sideways), which template the
// site uses and how many categories choose each one. Plugins add tabs (the
// designer, fonts, trash of core.template) and buttons on the cards. The
// tab chosen is part of the address (?tab=…), so "edit" can lead straight
// to the designer and a link opens the same tab again.
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { hasPerm, state } from '../core/state';
import { listTemplateCardSources, listTemplateTabs, listTemplates, setTriedTemplate, templateLabel, triedTemplate, type TemplateCardInfo, type TemplateTabContext } from '../coreplugin';
import { button, errorText, field, select } from '../components/ui';
import { placementEditor, typeLayouts } from '../components/placement';
import { offeredTilesForm } from './dashboard';
import { settingsTab } from './templateSettings';
import { currentDevice, deviceSwitch, schematic } from '../components/templateSchema';
import type { Device } from '../core/templateSchema';

interface Tab { id: string; label: string; render: (panel: HTMLElement, ctx: TemplateTabContext) => Promise<void> | void }

/** Whether the person may open Administration → Templates. */
export function mayOpenTemplates(): boolean {
  return hasPerm('core.settings.manage') || hasPerm('core.template.design');
}

/** The page with its tabs (WAI-ARIA tabs: arrow keys, Home and End move
 * between the tabs; only the tab chosen is in the tab order). */
export async function templatesAdmin(panel: HTMLElement, query: URLSearchParams): Promise<void> {
  const tabs: Tab[] = [
    { id: 'overview', label: t('sitetpl.tab_overview'), render: overview },
    ...(hasPerm('core.settings.manage') ? [{ id: 'settings', label: t('sitetpl.tab_settings'), render: settingsTab }, { id: 'positions', label: t('sitetpl.tab_positions'), render: positionsTab }] : []),
    ...listTemplateTabs().filter((x) => !x.permission || hasPerm(x.permission)).map((x) => ({ id: x.id, label: x.label(), render: x.render })),
  ];
  const buttons: HTMLButtonElement[] = [];
  const body = h('div', { role: 'tabpanel', tabindex: '0', class: 'ew-tabpanel' });
  let current = '';
  const open = async (id: string, params: Record<string, string> = {}, focus = false) => {
    const i = Math.max(0, tabs.findIndex((x) => x.id === id));
    const tab = tabs[i];
    buttons.forEach((b, j) => { b.setAttribute('aria-selected', String(i === j)); b.tabIndex = i === j ? 0 : -1; });
    body.setAttribute('aria-labelledby', buttons[i].id);
    if (focus) buttons[i].focus();
    // the address keeps the tab and its parameters (a link opens it again)
    const q = new URLSearchParams({ tab: tab.id, ...params });
    if (tab.id === 'overview' && !Object.keys(params).length) q.delete('tab');
    history.replaceState(history.state, '', `${location.pathname}${q.size ? `?${q}` : ''}`);
    current = tab.id;
    const ctx: TemplateTabContext = { params: q, open: (x, p) => void open(x, p) };
    body.replaceChildren();
    try {
      await tab.render(body, ctx);
    } catch (e) {
      if (current === tab.id) body.replaceChildren(h('p', { role: 'alert' }, errorText(e)));
    }
  };
  tabs.forEach((x, i) => {
    const b = h('button', { type: 'button', role: 'tab', id: uid('tpltab'), class: 'ew-tab', 'data-tab': x.id }, x.label) as HTMLButtonElement;
    b.addEventListener('click', () => void open(x.id));
    b.addEventListener('keydown', (ev) => {
      const n = tabs.length;
      const next = ev.key === 'ArrowRight' ? (i + 1) % n : ev.key === 'ArrowLeft' ? (i - 1 + n) % n : ev.key === 'Home' ? 0 : ev.key === 'End' ? n - 1 : -1;
      if (next < 0) return;
      ev.preventDefault();
      void open(tabs[next].id, {}, true);
    });
    buttons.push(b);
  });
  panel.replaceChildren(h('h1', null, t('sitetpl.title')), h('p', { class: 'ew-help' }, t('sitetpl.help')),
    h('div', { role: 'tablist', 'aria-label': t('sitetpl.tabs'), class: 'ew-tablist' }, ...buttons), body);
  const params = Object.fromEntries([...query].filter(([k]) => k !== 'tab'));
  await open(query.get('tab') ?? 'overview', params);
}

/** The overview: a card per template with its schematic. */
async function overview(panel: HTMLElement, ctx: TemplateTabContext): Promise<void> {
  const canSite = hasPerm('core.settings.manage');
  const reload = () => ctx.open('overview');
  const [usage, ...sources] = await Promise.all([
    canSite ? api.get<{ site: string; categories: Record<string, number> }>('admin/templates/usage') : Promise.resolve(null),
    ...listTemplateCardSources().map((s) => s({ ...ctx, reload })),
  ]);
  const site = state.site.template;
  const useForSite = async (id: string) => {
    try {
      const s = await api.get<{ config: { ui: Record<string, unknown> } }>('admin/settings');
      await api.put('admin/settings', { ui: { ...s.config.ui, template: id } });
      toast(t('sitetpl.used_for_site', { name: templateLabel(id) }));
      setTimeout(() => location.reload(), 600);
    } catch (e) { toast(errorText(e), 'error'); }
  };
  // templates that build a page (the preview of the designer is none of them)
  const nameOf = (id: string) => sources.map((f) => f(id)).find((x) => x)?.name ?? templateLabel(id);
  const ids = listTemplates().filter((x) => (x.layout || x.extends) && x.id !== 'core.template/preview').map((x) => x.id)
    .sort((a, b) => Number(b === site) - Number(a === site) || nameOf(a).localeCompare(nameOf(b), state.lang));
  const list = h('ul', { class: 'ew-tpl-cards' });
  const draw = (device: Device) => {
    list.replaceChildren(...ids.map((id) => {
      const info: TemplateCardInfo = sources.map((f) => f(id)).find((x) => x) ?? { actions: [] };
      const name = info.name ?? templateLabel(id);
      const cats = usage?.categories[id] ?? 0;
      const hid = uid('tplc');
      const named = (label: string) => `${label}: ${name}`;
      return h('li', { class: ['ew-tpl-card', id === site && 'ew-tpl-card--site'], 'aria-labelledby': hid, 'data-template': id },
        h('h3', { id: hid }, name),
        h('div', { class: 'ew-tpl-badges' },
          id === site ? h('span', { class: 'ew-badge ew-badge--info' }, t('sitetpl.active_site')) : null,
          id === triedTemplate() ? h('span', { class: 'ew-badge' }, t('sitetpl.tried')) : null,
          cats ? h('span', { class: 'ew-badge' }, t('sitetpl.used_categories', { count: cats })) : null),
        h('p', { class: 'ew-muted' }, info.kind ?? t('sitetpl.kind_code'), ' · ', h('code', null, id)),
        schematic(id, device, { name }),
        h('div', { class: 'ew-actions' },
          canSite ? button(t('sitetpl.tab_settings'), () => ctx.open('settings', { t: id }), { ariaLabel: named(t('sitetpl.tab_settings')) }) : null,
          ...info.actions,
          id === site || id === triedTemplate() ? null : button(t('template_try.try'), () => { setTriedTemplate(id); location.href = '/'; }, { ariaLabel: named(t('template_try.try')) }),
          canSite && id !== site ? button(t('sitetpl.use_for_site'), () => void useForSite(id), { ariaLabel: named(t('sitetpl.use_for_site')) }) : null));
    }));
  };
  draw(currentDevice());
  panel.replaceChildren(h('p', { class: 'ew-help' }, t('sitetpl.overview_help')), h('p', { class: 'ew-help' }, t('template_try.try_help')),
    deviceSwitch(draw), list);
}

/** The tab "Positions and placements": the schematic of a template with
 * its positions as buttons; what is placed there for the whole site. Then
 * the arrangement per type of page and the tiles offered for dashboards. */
async function positionsTab(panel: HTMLElement, ctx: TemplateTabContext): Promise<void> {
  const ids = listTemplates().filter((x) => (x.layout || x.extends) && x.id !== 'core.template/preview').map((x) => x.id);
  const chosen = ids.includes(ctx.params.get('t') ?? '') ? ctx.params.get('t')! : state.site.template;
  const sel = select(ids.map((id) => ({ value: id, label: templateLabel(id) })), chosen);
  sel.addEventListener('change', () => ctx.open('positions', { t: sel.value }));
  panel.replaceChildren(h('p', { class: 'ew-help' }, t('sitetpl.positions_help')),
    field(t('sitetpl.positions_template'), sel, { help: chosen === state.site.template ? t('sitetpl.active_site') : undefined }),
    await placementEditor('site', '', { schematic: true, templateId: chosen }), await typeLayouts(), await offeredTilesForm());
}
