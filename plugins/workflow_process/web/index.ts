// Frontend of the workflow plugin: editor, page widget, "My tasks".
import { api, button, errorText, field, fmtDate, h, mount, pageHeader, pick, pluginSettingsForm, registerAdminPage, registerEditor, registerSlot, registerWidget, route, state, t, textInput, toast } from '@ewiki/sdk';
import { createWorkflowEditor } from './editor';
import { linkView } from './link';
import type { Instance } from './types';
import { BASE, openCase, statusBadge, workflowWidget } from './widget';
import './style.css';

async function tasksView({ main }: { main: HTMLElement }): Promise<void> {
  if (!state.user) {
    mount(main, h('h1', null, t('workflow.tasks')), h('p', null, h('a', { href: '/login?next=/tasks' }, t('workflow.login'))));
    return;
  }
  const data = await api.get<{ tasks: Instance[]; started: Instance[] }>(`${BASE}/tasks`);
  const reload = () => void tasksView({ main });
  const list = (items: Instance[], caption: string, empty: string) => items.length
    ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, caption),
      h('thead', null, h('tr', null, ...[t('workflow.case'), t('workflow.process'), t('common.status'), t('workflow.updated')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...items.map((x) => h('tr', null,
        h('th', { scope: 'row' }, button(x.title, () => void openCase(x.id, reload).catch((e) => toast(errorText(e), 'error')), { variant: 'link' })),
        h('td', null, h('a', { href: `/e/${x.element_id}` }, pick(x.element_titles, state.lang, x.lang))),
        h('td', null, statusBadge(x)), h('td', null, fmtDate(x.updated_at, state.lang))))))
    : h('p', { class: 'ew-muted' }, empty);
  mount(main, h('div', { class: 'ewf-tasks' },
    h('h1', null, t('workflow.tasks')),
    h('section', { 'aria-labelledby': 'ewf-t1' }, h('h2', { id: 'ewf-t1' }, t('workflow.tasks_waiting'), ' ', h('span', { class: 'ew-badge' }, String(data.tasks.length))),
      list(data.tasks, t('workflow.tasks_waiting'), t('workflow.tasks_none'))),
    h('section', { 'aria-labelledby': 'ewf-t2' }, h('h2', { id: 'ewf-t2' }, t('workflow.tasks_started')),
      list(data.started, t('workflow.tasks_started'), t('workflow.started_none'))),
    await deputySection(reload)));
}

interface DeputyInfo { deputy: { deputy: string; from: string; until: string; active: boolean } | null; stand_in_for: { name: string; from: string; until: string }[] }

/** While I am away my deputy handles my tasks. */
async function deputySection(reload: () => void): Promise<HTMLElement> {
  const info = await api.get<DeputyInfo>(`${BASE}/me/deputy`).catch(() => null);
  const status = h('p', { role: 'status' });
  const who = textInput({ autocomplete: 'off', placeholder: t('workflow.deputy.who_ph') });
  const from = textInput({ type: 'date', value: info?.deputy?.from ?? new Date().toISOString().slice(0, 10) });
  const until = textInput({ type: 'date', value: info?.deputy?.until ?? '' });
  const save = async () => {
    try {
      await api.put(`${BASE}/me/deputy`, { deputy: who.value.trim(), from: from.value, until: until.value });
      toast(t('workflow.deputy.saved'));
      reload();
    } catch (e) { status.textContent = errorText(e); }
  };
  return h('section', { 'aria-labelledby': 'ewf-t3' }, h('h2', { id: 'ewf-t3' }, t('workflow.deputy.title')),
    h('p', { class: 'ew-help' }, t('workflow.deputy.help')),
    info?.deputy ? h('p', null, t(info.deputy.active ? 'workflow.deputy.active' : 'workflow.deputy.planned', { name: info.deputy.deputy, from: info.deputy.from, until: info.deputy.until }), ' ',
      button(t('workflow.deputy.remove'), async () => { await api.del(`${BASE}/me/deputy`); toast(t('workflow.deputy.removed')); reload(); }, { variant: 'subtle' })) : null,
    h('form', { class: 'ew-form ewf-deputy', onsubmit: (ev: Event) => { ev.preventDefault(); void save(); } },
      h('div', { class: 'ewf-grid' }, field(t('workflow.deputy.who'), who, { required: true }), field(t('workflow.deputy.from'), from, { required: true }),
        field(t('workflow.deputy.until'), until, { required: true })),
      button(t('workflow.deputy.save'), () => undefined, { type: 'submit', variant: 'secondary' })), status,
    info?.stand_in_for.length ? h('p', null, t('workflow.deputy.stand_in', { names: info.stand_in_for.map((x) => `${x.name} (${x.from} – ${x.until})`).join(', ') })) : null);
}

async function adminPage(panel: HTMLElement): Promise<void> {
  const settings = await pluginSettingsForm('workflow.process', [
    { key: 'links_without_login', label: t('workflow.admin.links'), type: 'bool', help: t('workflow.admin.links_help') },
    { key: 'holidays', label: t('workflow.admin.holidays'), type: 'textarea', help: t('workflow.admin.holidays_help'), placeholder: '01-01\n12-25\n2027-03-29' },
  ]);
  panel.replaceChildren(pageHeader(t('workflow.admin.title')), h('p', null, t('workflow.admin.intro')), settings.el);
}

export function register(): void {
  registerAdminPage({ id: 'workflow', label: () => t('workflow.admin.title'), render: adminPage });
  registerEditor('workflow.process', createWorkflowEditor);
  registerWidget('workflow', workflowWidget);
  route('/tasks', tasksView);
  route('/workflow/link', linkView);
  registerSlot({
    id: 'workflow.tasks', position: 'sidebar', order: 5,
    render: async () => {
      if (!state.user) return null;
      let n = 0;
      try { n = (await api.get<{ tasks: Instance[] }>(`${BASE}/tasks`)).tasks.length; } catch { return null; }
      return h('p', { class: 'ewf-tasks-link' }, h('a', { href: '/tasks' }, t('workflow.tasks'), ' ',
        h('span', { class: ['ew-badge', n > 0 && 'ew-badge--warn'] }, h('span', { 'aria-hidden': 'true' }, String(n)), h('span', { class: 'sr-only' }, t('workflow.tasks_count', { n })))));
    },
  });
}
