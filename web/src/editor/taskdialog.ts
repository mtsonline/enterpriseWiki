// "Task with person and date": a dialog that writes a task item of a
// checklist (`- [ ] Text @name :date{value=…}`), so nobody needs to know
// how it is written. The server keeps such items as tasks (core.html.task).
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { field, formDialog, textInput } from '../components/ui';
import { userPicker } from '../components/subjects';

/** The Markdown of a task item (no line break at the end). */
export function taskMarkdown(text: string, handles: string[], day: string): string {
  // the text stays text: no list, checkbox or block syntax from the field
  let clean = text.replace(/\s+/g, ' ').trim();
  for (let prev = ''; prev !== clean;) {
    prev = clean;
    clean = clean.replace(/^([-+*]\s+|\d+[.)]\s+|#+\s+|>\s*|\[[ xX]\]\s*)/, '');
  }
  clean = clean.replace(/([\\[\]{}:<>*_`~])/g, '\\$1');
  const people = handles.map((x) => `@${x.endsWith('@local') ? x.slice(0, -6) : x}`).join(' ');
  const date = /^\d{4}-\d{2}-\d{2}$/.test(day) ? `:date{value=${day}}` : '';
  return ['- [ ]', clean, people, date].filter(Boolean).join(' ');
}

/** Asks for the task; resolves with its Markdown, or null when cancelled. */
export function taskDialog(preset = ''): Promise<string | null> {
  return new Promise((resolve) => {
    let result: string | null = null;
    const text = textInput({ required: true, maxlength: 300, value: preset });
    const who = userPicker({ label: t('editor.task_people'), multiple: true, help: t('editor.task_people_help') });
    const due = textInput({ type: 'date' });
    formDialog(t('editor.task_title'), [
      h('p', { class: 'ew-help' }, t('editor.task_help')),
      field(t('editor.task_text'), text, { required: true }),
      who.el,
      field(t('editor.task_due'), due),
    ], t('editor.task_insert'), async () => {
      if (!text.value.trim()) throw new Error(t('error.required'));
      if (!who.handles().length && !due.value) throw new Error(t('editor.task_need'));
      result = taskMarkdown(text.value, who.handles(), due.value);
    }, { onClose: () => resolve(result) });
  });
}
