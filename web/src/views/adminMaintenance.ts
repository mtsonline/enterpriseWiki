// Administration → Maintenance: the wiki read-only or closed for everyone
// but administrators, with a message per language and the expected end.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, errorText, field, pageHeader, select, textArea, textInput } from '../components/ui';
import { langName } from '../components/widgets';

interface Maintenance { mode: '' | 'read_only' | 'closed'; message: Record<string, string> | null; until: string; run_jobs: boolean }

/** local value of a datetime-local field from an RFC 3339 time, and back */
const toLocal = (iso: string): string => {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
};
const fromLocal = (v: string): string => {
  if (!v) return '';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '';
  const off = -d.getTimezoneOffset();
  const p = (n: number) => String(Math.floor(Math.abs(n))).padStart(2, '0');
  return `${v}:00${off >= 0 ? '+' : '-'}${p(off / 60)}:${p(off % 60)}`;
};

export async function maintenanceAdmin(panel: HTMLElement): Promise<void> {
  const m = await api.get<Maintenance>('admin/maintenance');
  const mode = select(['', 'read_only', 'closed'].map((v) => ({ value: v, label: t(`maintenance.mode_${v || 'off'}`) })), m.mode, 'mode');
  const texts = state.site.languages.map((l) => {
    const a = textArea({ value: m.message?.[l] ?? '', rows: 3, lang: l, name: `message-${l}` });
    return { l, a, el: field(t('maintenance.message_in', { lang: langName(l) }), a, { help: t('maintenance.message_help') }) };
  });
  const until = textInput({ type: 'datetime-local', value: toLocal(m.until), name: 'until' });
  const jobs = checkbox(t('maintenance.run_jobs'), m.run_jobs, { help: t('maintenance.run_jobs_help') });
  const form = h('form', { class: 'ew-form', novalidate: true },
    field(t('maintenance.mode'), mode, { help: t('maintenance.mode_help') }),
    ...texts.map((x) => x.el),
    field(t('maintenance.until_label'), until, { help: t('maintenance.until_help') }),
    jobs.el,
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const message: Record<string, string> = {};
    for (const x of texts) if (x.a.value.trim()) message[x.l] = x.a.value.trim();
    try {
      await api.put('admin/maintenance', { mode: mode.value, message, until: fromLocal(until.value), run_jobs: jobs.input.checked });
      toast(t(mode.value ? 'maintenance.saved_on' : 'maintenance.saved_off'));
      setTimeout(() => location.reload(), 600);
    } catch (e) { toast(errorText(e), 'error'); }
  });
  mount(panel, pageHeader(t('admin.maintenance')), h('p', null, t('maintenance.intro')), form);
}
