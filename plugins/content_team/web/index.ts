// Team element: editor (description, groups, fields, layout) and the
// "team" widget (filter by name when the team is large).
import { announce, api, button, checkbox, errorText, field, h, mount, registerEditor, registerWidget, select, state, t, textArea, textInput, toast, type EditorInstance } from '@ewiki/sdk';
import './style.css';

const BASE = '/api/v1/x/content.team';

interface Body { description: { format: string; source: string }; groups: number[]; fields: string[]; labels: Record<string, string>; layout: string; by_group: boolean; sort: string }
interface Group { id: number; name: string; description: string; members: number }

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const b = { ...(raw as Body) };
  b.description = b.description ?? { format: 'markdown', source: '' };
  b.labels = { ...(b.labels ?? {}) };
  const fmtSel = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], b.description.format);
  const desc = textArea({ value: b.description.source, rows: 5 });
  const layout = select(['cards', 'list', 'table'].map((v) => ({ value: v, label: t(`content.team.layout_${v}`) })), b.layout);
  const sort = select(['name', 'job_title', 'department'].map((v) => ({ value: v, label: t(`content.team.sort_${v}`) })), b.sort);
  const byGroup = checkbox(t('content.team.by_group'), b.by_group);
  const groupBox = h('fieldset', { class: 'ewt-choice' }, h('legend', null, t('content.team.groups')), h('p', null, t('common.loading')));
  const fieldBox = h('fieldset', { class: 'ewt-choice' }, h('legend', null, t('content.team.fields')), h('p', null, t('common.loading')));
  const err = h('p', { class: 'ew-field-error', role: 'alert' });
  let groupInputs: { id: number; c: ReturnType<typeof checkbox> }[] = [];
  let fieldInputs: { f: string; c: ReturnType<typeof checkbox>; label?: HTMLInputElement }[] = [];
  void api.get<Group[]>(`${BASE}/groups`).then((gs) => {
    groupInputs = gs.map((g) => ({ id: g.id, c: checkbox(`${g.name} (${t('content.team.members_n', { n: g.members })})`, b.groups.includes(g.id), { help: g.description || undefined }) }));
    mount(groupBox, h('legend', null, t('content.team.groups')), ...(groupInputs.length ? groupInputs.map((x) => x.c.el) : [h('p', null, t('content.team.no_groups'))]));
  }).catch(() => mount(groupBox, h('legend', null, t('content.team.groups')), h('p', { role: 'alert' }, t('content.team.groups_error'))));
  void api.get<{ fields: string[]; attributes: string[] }>(`${BASE}/fields`).then((fs) => {
    const std = fs.fields.map((f) => ({ f, c: checkbox(t(`content.team.field_${f}`), b.fields.includes(f)) }));
    const attrs = [...new Set([...(fs.attributes ?? []), ...b.fields.filter((f) => f.startsWith('attr:')).map((f) => f.slice(5))])].map((a) => {
      const f = `attr:${a}`;
      const label = textInput({ value: b.labels[f] ?? '', placeholder: a });
      label.setAttribute('aria-label', t('content.team.attr_label', { name: a }));
      return { f, c: checkbox(a, b.fields.includes(f)), label };
    });
    fieldInputs = [...std, ...attrs];
    mount(fieldBox, h('legend', null, t('content.team.fields')), h('p', { class: 'ew-help' }, t('content.team.fields_help')), ...std.map((x) => x.c.el),
      attrs.length ? [h('h4', null, t('content.team.idp_attributes')), h('p', { class: 'ew-help' }, t('content.team.idp_help')),
        ...attrs.map((x) => h('div', { class: 'ewt-attr' }, x.c.el, x.label!))] : null);
  });
  mount(container, h('div', { class: 'ewt-editor' },
    h('fieldset', null, h('legend', null, t('content.team.description')), field(t('content.team.format'), fmtSel), field(t('content.team.description_text'), desc)),
    groupBox, fieldBox,
    h('fieldset', null, h('legend', null, t('content.team.display')), field(t('content.team.layout'), layout), field(t('content.team.sort'), sort), byGroup.el),
    err));
  return {
    getBody: () => {
      const fields = fieldInputs.length ? fieldInputs.filter((x) => x.c.input.checked).map((x) => x.f) : b.fields;
      const labels: Record<string, string> = {};
      fieldInputs.forEach((x) => { if (x.label?.value.trim()) labels[x.f] = x.label.value.trim(); });
      return {
        description: { format: fmtSel.value, source: desc.value }, groups: groupInputs.length ? groupInputs.filter((x) => x.c.input.checked).map((x) => x.id) : b.groups,
        fields, labels, layout: layout.value, sort: sort.value, by_group: byGroup.input.checked,
      };
    },
    validate: () => {
      const n = groupInputs.length ? groupInputs.filter((x) => x.c.input.checked).length : b.groups.length;
      err.textContent = n ? '' : t('content.team.err_groups');
      return n ? null : t('content.team.err_groups');
    },
  };
}

interface Member { name: string; fields: Record<string, string>; groups?: string[] }

/** Downloads the shown members as CSV (opens in Excel, imports into address books). */
async function exportCSV(id: number): Promise<void> {
  try {
    const d = await api.get<{ config: Body; sections: { name: string; members: Member[] }[] }>(`${BASE}/elements/${id}/members`);
    const fields = d.config.fields.filter((f) => f !== 'photo');
    const seen = new Set<string>();
    const rows = [[t('content.team.name'), ...fields.map((f) => d.config.labels[f] || (f.startsWith('attr:') ? f.slice(5) : t(`content.team.field_${f}`)))]];
    for (const s of d.sections) {
      for (const m of s.members) {
        if (seen.has(m.name)) continue;
        seen.add(m.name);
        rows.push([m.name, ...fields.map((f) => (f === 'groups' ? (m.groups ?? []).join(', ') : m.fields[f] ?? ''))]);
      }
    }
    const csv = rows.map((r) => r.map((x) => `"${String(x).replace(/"/g, '""')}"`).join(';')).join('\r\n');
    const a = h('a', { href: URL.createObjectURL(new Blob(['\ufeff' + csv], { type: 'text/csv' })), download: `team-${id}.csv` });
    a.click();
  } catch (e) { toast(errorText(e), 'error'); }
}

function teamWidget(root: HTMLElement): void {
  const id = Number(root.dataset.element);
  const members = Array.from(root.querySelectorAll<HTMLElement>('.ewt-member'));
  const tools = h('div', { class: 'ewt-tools' });
  if (state.user && members.length) tools.append(button(t('content.team.export'), () => void exportCSV(id), { variant: 'secondary' }));
  const status = h('p', { class: 'ewt-status', role: 'status' });
  if (members.length >= 9) {
    const q = textInput({ type: 'search', placeholder: t('content.team.filter_placeholder') });
    q.setAttribute('aria-label', t('content.team.filter'));
    let timer = 0;
    q.addEventListener('input', () => {
      clearTimeout(timer);
      timer = window.setTimeout(() => {
        const term = q.value.trim().toLocaleLowerCase();
        let n = 0;
        for (const m of members) {
          const hit = !term || (m.textContent ?? '').toLocaleLowerCase().includes(term);
          m.hidden = !hit;
          if (hit) n++;
        }
        root.querySelectorAll<HTMLElement>('.ewt-group').forEach((g) => { g.hidden = !g.querySelector('.ewt-member:not([hidden])'); });
        status.textContent = term ? t('content.team.found', { n }) : '';
        if (term) announce(status.textContent);
      }, 200);
    });
    tools.prepend(h('div', { class: 'ewt-filter', role: 'search' }, q));
  }
  if (tools.childElementCount) root.querySelector('.ewt-body')?.before(tools, status);
}

export function register(): void {
  registerEditor('content.team', createEditor);
  registerWidget('team', teamWidget);
}
