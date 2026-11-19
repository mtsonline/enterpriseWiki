// core.template: website templates of data. On start the templates that can
// be used are loaded and registered like the templates of code; the page is
// built from them by the engine (engine.ts). The administration page
// "Templates" lists all templates (code and data) and manages those of data:
// copy, change (visual designer and text, designer.ts), trash,
// export and import.
import {
  api, button, confirmDialog, errorText, field, h, hasPerm, listTemplates, registerAdminPage, registerTemplate,
  state, t, templateLabel, toast,
} from '@ewiki/coreplugin';
import './style.css';
import { buildLayout, classOf, designCSS, type Design } from './engine';
import { designer } from './designer';
import { setupPreview } from './preview';

const BASE = 'x/core.template';

interface Tpl {
  id: string; key: string; names: Record<string, string>; design?: Design; based_on?: string; version: number; read_only?: boolean;
  updated_at?: string; deleted_at?: string | null;
}

/** The name of a button for a template: its visible text first (WCAG 2.5.3), then which template. */
const named = (label: string, name: string) => `${label}: ${name}`;

const pick = (m: Record<string, string>) => m[state.lang] || m[state.site.default_language] || Object.values(m)[0] || '';

/** Registers a template of data: its CSS (from checked values only) and its layout. */
function addDesign(tpl: Tpl): void {
  if (!tpl.design) return;
  const d = tpl.design;
  const cls = classOf(tpl.id);
  const styleId = `ewd-style-${cls}`;
  const style = (document.getElementById(styleId) as HTMLStyleElement | null) ?? h('style', { id: styleId });
  style.textContent = designCSS(d, cls);
  if (!style.isConnected) document.head.append(style);
  const positions = d.rows.flatMap((r) => r.blocks.flatMap((b) => [...b.positions, ...(b.after ?? [])]));
  registerTemplate({ id: tpl.id, className: cls, positions, layout: (ctx) => buildLayout(d, ctx) });
}

export async function register(): Promise<void> {
  registerAdminPage({ id: 'templates', label: () => t('template.admin_title'), permission: 'core.template.design', order: 35, render: adminPage });
  const list = await api.get<Tpl[]>(`${BASE}/designs`);
  for (const tpl of list) addDesign(tpl);
  // the frame of the designer's preview shows its draft instead
  setupPreview();
}

// ---------------------------------------------------------------- administration

async function adminPage(panel: HTMLElement): Promise<void> {
  const [data, trash] = await Promise.all([api.get<Tpl[]>(`${BASE}/templates`), api.get<Tpl[]>(`${BASE}/templates?deleted=1`)]);
  const reload = () => void adminPage(panel);
  const canSite = hasPerm('core.settings.manage');
  const useForSite = async (id: string) => {
    try {
      const s = await api.get<{ config: { ui: Record<string, unknown> } }>('admin/settings');
      await api.put('admin/settings', { ui: { ...s.config.ui, template: id } });
      toast(t('template.used_for_site', { name: templateLabel(id) }));
      setTimeout(() => location.reload(), 600);
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const copy = async (x: Tpl) => {
    try {
      const names: Record<string, string> = {};
      for (const [l, v] of Object.entries(x.names)) names[l] = t('template.copy_of', { name: v });
      const c = await api.post<Tpl>(`${BASE}/templates`, { names, copy_of: x.id });
      toast(t('template.copied', { name: pick(c.names) }));
      void designer(panel, c.key, reload, addDesign);
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const exportFile = async (x: Tpl) => {
    try {
      const f = await api.get<unknown>(`${BASE}/templates/${x.key}/export`);
      const a = h('a', { href: URL.createObjectURL(new Blob([JSON.stringify(f, null, 2)], { type: 'application/json' })), download: `${x.key}.template.json` });
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const site = state.site.template;
  // templates of code: registered by plugins, references (not in the designer)
  const code = listTemplates().filter((x) => (x.layout || x.extends) && !x.id.startsWith('core.template/'));
  const row = (name: string, kind: string, id: string, actions: (HTMLElement | null)[]) => h('tr', null,
    h('th', { scope: 'row' }, name, id === site ? [' ', h('span', { class: 'ew-badge ew-badge--info' }, t('template.site_badge'))] : null),
    h('td', null, kind), h('td', null, h('code', null, id)), h('td', null, h('div', { class: 'ew-actions' }, ...actions)));
  const importInput = h('input', { type: 'file', accept: 'application/json,.json', class: 'ew-input' }) as HTMLInputElement;
  importInput.addEventListener('change', () => void (async () => {
    const file = importInput.files?.[0];
    if (!file) return;
    try {
      const c = await api.post<Tpl>(`${BASE}/templates/import`, JSON.parse(await file.text()));
      toast(t('template.imported', { name: pick(c.names) }));
      reload();
    } catch (e) { toast(e instanceof SyntaxError ? t('template.not_json') : errorText(e), 'error'); }
  })());
  panel.replaceChildren(...[
    h('h1', null, t('template.admin_title')),
    h('p', { class: 'ew-help' }, t('template.admin_help')),
    h('div', { class: 'ew-table-wrap', role: 'region', 'aria-label': t('template.list'), tabindex: '0' },
      h('table', { class: 'ew-table' },
        h('caption', { class: 'sr-only' }, t('template.list')),
        h('thead', null, h('tr', null, ...['template.col_name', 'template.col_kind', 'template.col_id', 'common.actions'].map((k) => h('th', { scope: 'col' }, t(k))))),
        h('tbody', null,
          ...data.map((x) => row(pick(x.names), x.read_only ? t('template.kind_shipped') : t('template.kind_data'), x.id, [
            x.read_only ? null : button(t('common.edit'), () => void designer(panel, x.key, reload, addDesign), { variant: 'primary', ariaLabel: named(t('common.edit'), pick(x.names)) }),
            button(t('common.duplicate'), () => void copy(x), { ariaLabel: named(t('common.duplicate'), pick(x.names)) }),
            button(t('template.export'), () => void exportFile(x), { ariaLabel: named(t('template.export'), pick(x.names)) }),
            canSite && x.id !== site ? button(t('template.use_for_site'), () => void useForSite(x.id), { ariaLabel: named(t('template.use_for_site'), pick(x.names)) }) : null,
            x.read_only ? null : button(t('common.delete'), () => void (async () => {
              if (!(await confirmDialog(t('template.delete_confirm', { name: pick(x.names) }), t('common.delete'), true))) return;
              try { await api.del(`${BASE}/templates/${x.key}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
            })(), { variant: 'danger', ariaLabel: named(t('common.delete'), pick(x.names)) }),
          ])),
          ...code.map((x) => row(templateLabel(x.id), t('template.kind_code'), x.id, [
            h('span', { class: 'ew-muted' }, t('template.code_note')),
            canSite && x.id !== site ? button(t('template.use_for_site'), () => void useForSite(x.id), { ariaLabel: named(t('template.use_for_site'), templateLabel(x.id)) }) : null,
          ]))))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ewd-import-h' },
      h('h2', { id: 'ewd-import-h' }, t('template.import')), field(t('template.import_file'), importInput, { help: t('template.import_help') })),
    trash.length ? h('section', { class: 'ew-section', 'aria-labelledby': 'ewd-trash-h' },
      h('h2', { id: 'ewd-trash-h' }, t('template.trash')),
      h('ul', { class: 'ew-plain-list' }, ...trash.map((x) => h('li', null, pick(x.names), ' ',
        button(t('trash.restore'), () => void (async () => {
          try { await api.post(`${BASE}/templates/${x.key}/restore`, {}); reload(); } catch (e) { toast(errorText(e), 'error'); }
        })(), { ariaLabel: named(t('trash.restore'), pick(x.names)) }), ' ',
        button(t('trash.purge'), () => void (async () => {
          if (!(await confirmDialog(t('template.purge_confirm', { name: pick(x.names) }), t('trash.purge'), true))) return;
          try { await api.del(`${BASE}/templates/${x.key}?purge=1`); reload(); } catch (e) { toast(errorText(e), 'error'); }
        })(), { variant: 'danger', ariaLabel: named(t('trash.purge'), pick(x.names)) }))))) : null,
  ].filter((n): n is HTMLElement => !!n));
}

