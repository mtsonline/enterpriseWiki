// core.template: website templates of data. On start the templates that can
// be used are loaded and registered like the templates of code; the page is
// built from them by the engine (engine.ts). The administration page
// "Templates" lists all templates (code and data) and manages those of data:
// copy, change (as text for now; the visual designer follows), trash,
// export and import.
import {
  api, button, fmtDate, uiLang, confirmDialog, errorText, field, h, hasPerm, langFields, listTemplates, registerAdminPage, registerTemplate,
  state, t, templateLabel, textArea, toast,
} from '@ewiki/coreplugin';
import './style.css';
import { buildLayout, classOf, designCSS, type Design } from './engine';

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
      void editor(panel, c.key, reload);
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
            x.read_only ? null : button(t('common.edit'), () => void editor(panel, x.key, reload), { variant: 'primary', ariaLabel: named(t('common.edit'), pick(x.names)) }),
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

/** Changing a template: names and the design as text, checked while typing
 * (the same check as on saving). The visual designer follows. */
async function editor(panel: HTMLElement, key: string, back: () => void): Promise<void> {
  const { template: tpl, used_by: used } = await api.get<{ template: Tpl; used_by: string[] }>(`${BASE}/templates/${key}`);
  let version = tpl.version;
  const names = langFields(t('template.name'), tpl.names, { required: true });
  const text = textArea({ value: JSON.stringify(tpl.design, null, 2), rows: 28 });
  text.classList.add('ewd-json');
  text.spellcheck = false;
  const problems = h('div', { class: 'ewd-problems', role: 'status', 'aria-live': 'polite' });
  let timer = 0;
  const check = async () => {
    let design: unknown;
    try { design = JSON.parse(text.value); } catch (e) {
      problems.replaceChildren(h('p', { class: 'ew-form-alert' }, t('template.not_json_detail', { error: String((e as Error).message) })));
      return false;
    }
    const r = await api.post<{ problems: { where: string; text: string }[] }>(`${BASE}/check`, { design });
    problems.replaceChildren(r.problems.length
      ? h('div', { class: 'ew-form-alert' }, h('p', null, t('template.problems', { n: r.problems.length })),
        h('ul', null, ...r.problems.map((p) => h('li', null, h('strong', null, p.where), ': ', p.text))))
      : h('p', { class: 'ew-ok' }, t('template.no_problems')));
    return r.problems.length === 0;
  };
  text.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => void check(), 600); });
  const save = button(t('common.save'), () => void (async () => {
    if (!(await check())) { text.focus(); return; }
    try {
      const r = await api.put<Tpl>(`${BASE}/templates/${key}`, { names: names.get(), design: JSON.parse(text.value), version });
      version = r.version;
      if (r.design) addDesign(r);
      toast(t('template.saved', { version: r.version }));
      void showVersions();
    } catch (e) { toast(errorText(e), 'error'); }
  })(), { variant: 'primary' });
  // the saved versions; one can be taken into the text and saved again
  const versionsBox = h('section', { 'aria-labelledby': 'ewd-versions-h' });
  const showVersions = async () => {
    const vs = await api.get<{ version: number; design: unknown; created_at: string }[]>(`${BASE}/templates/${key}/versions`);
    versionsBox.replaceChildren(h('h2', { id: 'ewd-versions-h' }, t('template.versions')),
      h('p', { class: 'ew-help' }, t('template.versions_help')),
      h('ul', { class: 'ewd-versions' }, ...vs.map((v) => h('li', null,
        t('template.version_line', { version: v.version, date: fmtDate(v.created_at, uiLang()) }), ' ',
        v.version === version ? h('span', { class: 'ew-help' }, t('template.version_current'))
          : button(t('template.version_take'), () => {
            text.value = JSON.stringify(v.design, null, 2);
            void check();
            toast(t('template.version_taken', { version: v.version }));
            text.focus();
          }, { variant: 'subtle', ariaLabel: t('template.version_take_named', { version: v.version }) })))));
  };
  void showVersions();
  panel.replaceChildren(...([
    h('p', null, button(t('template.back'), back, { variant: 'subtle' })),
    h('h1', null, t('template.edit_named', { name: pick(tpl.names) })),
    used.length ? h('p', { class: 'ew-help' }, t('template.used_by', { list: used.join(', ') })) : null,
    h('form', { class: 'ew-form', novalidate: true, onsubmit: (ev: Event) => ev.preventDefault() },
      names.el,
      field(t('template.design_text'), text, { help: t('template.design_text_help') }),
      problems,
      h('div', { class: 'ew-actions' }, save)),
    versionsBox,
  ] as (HTMLElement | null)[]).filter((n): n is HTMLElement => !!n));
  void check();
}
