// Frontend of the workflow plugin: editor, page widget, "My tasks".
import { api, button, errorText, fmtDate, h, mount, pick, registerEditor, registerSlot, registerWidget, route, state, t, toast } from '@ewiki/sdk';
import { createWorkflowEditor } from './editor';
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
      list(data.started, t('workflow.tasks_started'), t('workflow.started_none')))));
}

export function register(): void {
  registerEditor('workflow.process', createWorkflowEditor);
  registerWidget('workflow', workflowWidget);
  route('/tasks', tasksView);
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
