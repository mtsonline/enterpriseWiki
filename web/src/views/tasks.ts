// "My tasks" (/tasks): sections of plugins (workflow steps, tasks in pages
// …), each with its own heading. The page itself belongs to the core, so
// it works with any of the plugins switched off.
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';

export interface TaskSection {
  id: string;
  order?: number;
  /** The section (with its heading), or null when it has nothing to show. */
  render: (reload: () => void) => Promise<HTMLElement | null>;
}

const sections: TaskSection[] = [];

/** Adds a section to "My tasks". */
export function registerTaskSection(s: TaskSection): void {
  sections.push(s);
  sections.sort((a, b) => (a.order ?? 50) - (b.order ?? 50));
}

export async function tasksView({ main }: { main: HTMLElement }): Promise<void> {
  const title = h('h1', null, t('tasks.title'));
  if (!state.user) {
    mount(main, title, h('p', null, h('a', { href: '/login?next=/tasks' }, t('tasks.login'))));
    return;
  }
  const reload = () => void tasksView({ main });
  const parts = await Promise.all(sections.map((s) => s.render(reload).catch((e) => {
    console.error(`task section ${s.id} failed`, e);
    return null;
  })));
  const shown = parts.filter((p): p is HTMLElement => p !== null);
  mount(main, h('div', { class: 'ew-tasks-page' }, title, ...(shown.length ? shown : [h('p', { class: 'ew-muted' }, t('tasks.none'))])));
}
