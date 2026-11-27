// core.template: website templates of data. On start the templates that can
// be used are loaded and registered like the templates of code; the page is
// built from them by the engine (engine.ts). The administration page
// "Templates" lists all templates (code and data) and manages those of data:
// copy, change (visual designer and text, designer.ts), trash,
// export and import.
import {
  api, button, checkbox, confirmDialog, errorText, field, fmtDate, formDialog, h, select, textInput, hasPerm, listTemplates, registerAdminPage, registerTemplate,
  setTriedTemplate, state, subjectsEditor, t, templateLabel, toast, triedTemplate,
} from '@ewiki/coreplugin';
import './style.css';
import { buildLayout, classOf, designCSS, type Design } from './engine';
import { designer } from './designer';
import { setupPreview } from './preview';
import { guessFace } from './fonts';

const BASE = 'x/core.template';

interface Tpl {
  id: string; key: string; names: Record<string, string>; design?: Design; based_on?: string; version: number; read_only?: boolean;
  updated_at?: string; deleted_at?: string | null;
  edit_subjects?: Subject[]; manage_subjects?: Subject[];
  /** what the caller may do with it (stored templates) */
  may?: { edit: boolean; manage: boolean };
}
interface OwnFont { id: number; token: string; name: string; license: string; license_note: string; bytes: number; created_at: string; used_by: string[]; faces: { n: number; weight: string; style: string; format: string }[] }
type Subject = { type: 'user' | 'group' | 'role' | 'authenticated' | 'guest'; id?: number };

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
  registerAdminPage({ id: 'templates', area: 'appearance', label: () => t('template.admin_title'), permission: 'core.template.design', order: 35, render: adminPage });
  const list = await api.get<Tpl[]>(`${BASE}/designs`);
  for (const tpl of list) addDesign(tpl);
  // the frame of the designer's preview shows its draft instead
  setupPreview();
}

// ---------------------------------------------------------------- administration

async function adminPage(panel: HTMLElement): Promise<void> {
  const [data, trash, fonts] = await Promise.all([api.get<Tpl[]>(`${BASE}/templates`), api.get<Tpl[]>(`${BASE}/templates?deleted=1`), api.get<OwnFont[]>(`${BASE}/fonts`)]);
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
  // who may change and manage one template (empty: every designer)
  const rights = async (x: Tpl) => {
    try {
      const { template: cur, subject_names: names } = await api.get<{ template: Tpl; subject_names: Record<string, string> }>(`${BASE}/templates/${x.key}`);
      const edit = await subjectsEditor(t('template.rights_edit'), t('template.rights_edit_help'), cur.edit_subjects ?? [], names);
      const manage = await subjectsEditor(t('template.rights_manage'), t('template.rights_manage_help'), cur.manage_subjects ?? [], names);
      const dlg = formDialog(t('template.rights_title', { name: pick(cur.names) }), [h('p', { class: 'ew-help' }, t('template.rights_help')), edit.el, manage.el], t('common.save'), async () => {
        await api.put(`${BASE}/templates/${x.key}/rights`, { edit_subjects: edit.value(), manage_subjects: manage.value() });
        dlg.close();
        toast(t('template.rights_saved'));
        reload();
      }, { wide: true });
    } catch (e) { toast(errorText(e), 'error'); }
  };
  // see a template only for oneself first (this browser), then for everybody
  const tryIt = (id: string) => { setTriedTemplate(id); location.href = '/'; };
  const tryButton = (id: string, name: string) => id === site || id === triedTemplate() ? null
    : button(t('template_try.try'), () => tryIt(id), { ariaLabel: named(t('template_try.try'), name) });
  const may = (x: Tpl) => x.may ?? { edit: !x.read_only, manage: !x.read_only };
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
    h('p', { class: 'ew-help' }, t('template_try.try_help')),
    h('div', { class: 'ew-table-wrap', role: 'region', 'aria-label': t('template.list'), tabindex: '0' },
      h('table', { class: 'ew-table' },
        h('caption', { class: 'sr-only' }, t('template.list')),
        h('thead', null, h('tr', null, ...['template.col_name', 'template.col_kind', 'template.col_id', 'common.actions'].map((k) => h('th', { scope: 'col' }, t(k))))),
        h('tbody', null,
          ...data.map((x) => row(pick(x.names), x.read_only ? t('template.kind_shipped') : t('template.kind_data'), x.id, [
            x.read_only ? null : may(x).edit ? button(t('common.edit'), () => void designer(panel, x.key, reload, addDesign), { variant: 'primary', ariaLabel: named(t('common.edit'), pick(x.names)) })
              : h('span', { class: 'ew-muted' }, t('template.limited')),
            button(t('common.duplicate'), () => void copy(x), { ariaLabel: named(t('common.duplicate'), pick(x.names)) }),
            button(t('template.export'), () => void exportFile(x), { ariaLabel: named(t('template.export'), pick(x.names)) }),
            tryButton(x.id, pick(x.names)),
            canSite && x.id !== site ? button(t('template.use_for_site'), () => void useForSite(x.id), { ariaLabel: named(t('template.use_for_site'), pick(x.names)) }) : null,
            !x.read_only && may(x).manage ? button(t('template.rights'), () => void rights(x), { ariaLabel: named(t('template.rights'), pick(x.names)) }) : null,
            x.read_only || !may(x).manage ? null : button(t('common.delete'), () => void (async () => {
              if (!(await confirmDialog(t('template.delete_confirm', { name: pick(x.names) }), t('common.delete'), true))) return;
              try { await api.del(`${BASE}/templates/${x.key}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
            })(), { variant: 'danger', ariaLabel: named(t('common.delete'), pick(x.names)) }),
          ])),
          ...code.map((x) => row(templateLabel(x.id), t('template.kind_code'), x.id, [
            h('span', { class: 'ew-muted' }, t('template.code_note')),
            tryButton(x.id, templateLabel(x.id)),
            canSite && x.id !== site ? button(t('template.use_for_site'), () => void useForSite(x.id), { ariaLabel: named(t('template.use_for_site'), templateLabel(x.id)) }) : null,
          ]))))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ewd-import-h' },
      h('h2', { id: 'ewd-import-h' }, t('template.import')), field(t('template.import_file'), importInput, { help: t('template.import_help') })),
    fontSection(fonts, reload),
    trash.length ? h('section', { class: 'ew-section', 'aria-labelledby': 'ewd-trash-h' },
      h('h2', { id: 'ewd-trash-h' }, t('template.trash')),
      h('ul', { class: 'ew-plain-list' }, ...trash.map((x) => h('li', null, pick(x.names), ' ', !may(x).manage ? h('span', { class: 'ew-muted' }, t('template.limited')) : [
        button(t('trash.restore'), () => void (async () => {
          try { await api.post(`${BASE}/templates/${x.key}/restore`, {}); reload(); } catch (e) { toast(errorText(e), 'error'); }
        })(), { ariaLabel: named(t('trash.restore'), pick(x.names)) }), ' ',
        button(t('trash.purge'), () => void (async () => {
          if (!(await confirmDialog(t('template.purge_confirm', { name: pick(x.names) }), t('trash.purge'), true))) return;
          try { await api.del(`${BASE}/templates/${x.key}?purge=1`); reload(); } catch (e) { toast(errorText(e), 'error'); }
        })(), { variant: 'danger', ariaLabel: named(t('trash.purge'), pick(x.names)) })])))) : null,
  ].filter((n): n is HTMLElement => !!n));
}


// ---------------------------------------------------------------- own fonts

const WEIGHTS = ['100', '200', '300', '400', '500', '600', '700', '800', '900', '100 900'];

/** Uploaded fonts: one or more files each (WOFF2, WOFF, TTF, OTF; regular,
 * bold, italic … or one variable font), with the license and the
 * confirmation that it may be used on this website; removed only while no
 * template uses it. */
function fontSection(fonts: OwnFont[], reload: () => void): HTMLElement {
  const file = h('input', { type: 'file', multiple: true, accept: '.woff2,.woff,.ttf,.otf,font/woff2,font/woff,font/ttf,font/otf', class: 'ew-input' }) as HTMLInputElement;
  const name = textInput({ maxlength: 60 });
  const license = select(['OFL-1.1', 'Apache-2.0', 'MIT', 'other'].map((v) => ({ value: v, label: t(`template.font_license_${v.replace(/[.-]/g, '_').toLowerCase()}`) })), 'OFL-1.1');
  const note = textInput({ maxlength: 500 });
  const confirm = checkbox(t('template.font_confirm'), false);
  // one row per chosen file: weight and style
  const rows = h('div', { class: 'ewd-font-files' });
  let chosen: { file: File; weight: HTMLSelectElement; style: HTMLSelectElement }[] = [];
  file.addEventListener('change', () => {
    const files = [...(file.files ?? [])].slice(0, 8);
    chosen = files.map((f) => {
      const g = guessFace(f.name);
      const weight = select(WEIGHTS.map((w) => ({ value: w, label: w === '100 900' ? t('template.font_variable') : t('template.font_weight_n', { n: w }) })), g.weight);
      const style = select([{ value: 'normal', label: t('template.font_style_normal') }, { value: 'italic', label: t('template.font_style_italic') }], g.style);
      return { file: f, weight, style };
    });
    rows.replaceChildren(...chosen.map((c) => h('fieldset', { class: 'ew-fieldset' }, h('legend', null, c.file.name),
      field(t('template.font_weight'), c.weight), field(t('template.font_style'), c.style))));
    if (files[0] && !name.value.trim()) {
      name.value = files[0].name.replace(/\.(woff2?|ttf|otf)$/i, '')
        .replace(/[-_ ]?(variable|vf|regular|italic|oblique|bold|semibold|light|medium|black|thin|heavy|\[wght\]|\[[^\]]*\])+$/gi, '')
        .replace(/[^\p{L}\p{N} ._-]/gu, ' ').trim().slice(0, 60);
    }
  });
  const upload = async () => {
    if (!chosen.length) { toast(t('template.font_pick'), 'error'); return; }
    try {
      const files = await Promise.all(chosen.map(async (c) => {
        const buf = new Uint8Array(await c.file.arrayBuffer());
        let bin = '';
        for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
        return { data: btoa(bin), weight: c.weight.value, style: c.style.value };
      }));
      await api.post(`${BASE}/fonts`, { name: name.value, license: license.value, license_note: note.value, confirm: confirm.input.checked, files });
      toast(t('template.font_added', { name: name.value }));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const kb = (n: number) => `${Math.ceil(n / 1024)} KB`;
  // each file with a link to download it (unchanged, as uploaded)
  const faceName = (x: OwnFont['faces'][number]) => `${x.weight === '100 900' ? t('template.font_variable') : x.weight}${x.style === 'italic' ? ` ${t('template.font_style_italic')}` : ''} (${x.format})`;
  const faces = (f: OwnFont) => h('span', { class: 'ewd-font-faces' }, ...f.faces.flatMap((x, i) => [i ? ', ' : '',
    h('a', { href: `/api/v1/${BASE}/fonts/${f.id}/files/${x.n}`, download: '', 'aria-label': t('template.font_download', { name: f.name, face: faceName(x) }) }, faceName(x))]));
  return h('section', { class: 'ew-section', 'aria-labelledby': 'ewd-fonts-h' },
    h('h2', { id: 'ewd-fonts-h' }, t('template.fonts')),
    h('p', { class: 'ew-help' }, t('template.fonts_help')),
    fonts.length ? h('ul', { class: 'ew-plain-list' }, ...fonts.map((f) => h('li', null,
      h('strong', null, f.name), ' ', faces(f), ' ', h('span', { class: 'ew-muted' }, [f.license, f.license_note ? ` (${f.license_note})` : '', ` · ${kb(f.bytes)} · ${fmtDate(f.created_at, state.lang)}`].join('')), ' ',
      f.used_by.length ? h('span', { class: 'ew-muted' }, t('template.font_used', { list: f.used_by.map((x) => templateLabel(x)).join(', ') }))
        : button(t('common.delete'), () => void (async () => {
          if (!(await confirmDialog(t('template.font_delete_confirm', { name: f.name }), t('common.delete'), true))) return;
          try { await api.del(`${BASE}/fonts/${f.id}`); reload(); } catch (e) { toast(errorText(e), 'error'); }
        })(), { variant: 'danger', ariaLabel: `${t('common.delete')}: ${f.name}` })))) : h('p', null, t('template.fonts_none')),
    h('h3', null, t('template.font_upload')),
    field(t('template.font_file'), file, { help: t('template.font_file_help') }),
    rows,
    field(t('template.font_name'), name),
    field(t('template.font_license'), license),
    field(t('template.font_note'), note, { help: t('template.font_note_help') }),
    confirm.el,
    button(t('template.font_upload'), () => void upload(), { variant: 'primary' }));
}
