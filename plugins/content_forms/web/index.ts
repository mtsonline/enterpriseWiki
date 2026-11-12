// Frontend of the forms plugin: the form on the page, the editor and the
// submissions below the form (creator and "See submissions").
import { ApiError, api, button, confirmDialog, fmtDate, h, humanSize, pageHeader, pluginSettingsForm, registerAdminPage, registerEditor, registerElementExtension, registerWidget, state, t, textInput, toast, errorText } from '@ewiki/sdk';
import type { ElementView } from '@ewiki/sdk';
import { createFormEditor, type FField } from './editor';
import { BASE, formWidget } from './widget';
import './style.css';

interface SFile { field: string; name: string; size: number; mime: string; comment?: string; medium?: number }
interface Submission { id: number; data: Record<string, unknown>; files: SFile[]; author: string; created_at: string; seen: boolean; pending: string[]; last_error?: string }

function valueText(f: FField, v: unknown): string {
  const label = (s: string) => f.options?.find((o) => o.value === s)?.label ?? s;
  if (v === true) return '✓';
  if (v === false || v === null || v === undefined) return '';
  if (Array.isArray(v)) return v.map((x) => label(String(x))).join(', ');
  return label(String(v));
}

async function submissions(el: ElementView): Promise<Node | null> {
  if (el.type !== 'content.forms' || !state.user) return null;
  const box = h('section', { class: 'ewfm-subs', 'aria-labelledby': `ewfm-subs-${el.id}` });
  const heading = h('h2', { id: `ewfm-subs-${el.id}` }, t('forms.submissions'));
  const search = textInput({ type: 'search', placeholder: t('forms.search') });
  search.setAttribute('aria-label', t('forms.search'));
  const body = h('div', { 'aria-live': 'polite' });
  let offset = 0;
  const load = async () => {
    let data: { total: number; items: Submission[]; fields: FField[] };
    try {
      data = await api.get(`${BASE}/elements/${el.id}/submissions?limit=50&offset=${offset}&q=${encodeURIComponent(search.value.trim())}&lang=${state.lang}`);
    } catch (e) {
      if (e instanceof ApiError && (e.status === 403 || e.status === 401)) { box.remove(); return false; }
      throw e;
    }
    const cols = data.fields.filter((f) => !['heading', 'paragraph', 'divider'].includes(f.type));
    heading.textContent = `${t('forms.submissions')} (${data.total})`;
    if (!data.items.length) { body.replaceChildren(h('p', { class: 'ew-muted' }, t('forms.no_submissions'))); return true; }
    const files = (s: Submission, f: FField) => s.files.map((x, i) => ({ x, i })).filter(({ x }) => x.field === f.id).map(({ x, i }) =>
      h('span', { class: 'ewfm-subs-file' }, h('a', { href: `/api/v1/${BASE}/submissions/${s.id}/files/${i}`, download: x.name }, x.name), ` (${humanSize(x.size)})`, x.comment ? ` – ${x.comment}` : ''));
    body.replaceChildren(h('div', { class: 'ew-table-wrap', tabindex: '0', role: 'region', 'aria-label': t('forms.submissions') },
      h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('forms.submissions_count', { n: data.total })),
        h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('forms.submitted_at')), h('th', { scope: 'col' }, t('forms.submitted_by')),
          ...cols.map((c) => h('th', { scope: 'col' }, c.label || c.id)), h('th', { scope: 'col' }, h('span', { class: 'sr-only' }, t('common.actions'))))),
        h('tbody', null, ...data.items.map((s) => h('tr', { id: `submission-${s.id}`, class: s.seen ? '' : 'is-new' },
          h('th', { scope: 'row' }, fmtDate(s.created_at, state.lang), s.seen ? '' : h('span', { class: 'ew-badge' }, t('forms.new')),
            s.pending.length ? h('p', { class: 'ew-help' }, t('forms.pending', { targets: s.pending.join(', ') })) : null,
            s.last_error ? h('p', { class: 'ewfm-error' }, t('forms.failed', { error: s.last_error })) : null),
          h('td', null, s.author || '—'),
          ...cols.map((c) => h('td', null, c.type === 'file' ? files(s, c) : valueText(c, s.data[c.id]))),
          h('td', null, button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('forms.delete_confirm')))) return;
            try { await api.del(`${BASE}/submissions/${s.id}`); await load(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'subtle', ariaLabel: `${t('forms.delete_submission')} ${fmtDate(s.created_at, state.lang)}` }))))))),
    data.total > offset + 50 || offset > 0 ? h('p', { class: 'ewfm-subs-pager' },
      offset > 0 ? button(t('common.previous'), () => { offset -= 50; void load(); }, { variant: 'secondary' }) : null,
      data.total > offset + 50 ? button(t('common.next'), () => { offset += 50; void load(); }, { variant: 'secondary' }) : null) : '');
    // a link to one submission (e-mail): scroll there and mark it seen
    const want = new URLSearchParams(location.search).get('submission');
    if (want) {
      document.getElementById(`submission-${want}`)?.scrollIntoView({ block: 'center' });
      void api.get(`${BASE}/submissions/${encodeURIComponent(want)}`).catch(() => undefined);
    }
    return true;
  };
  let timer = 0;
  search.addEventListener('input', () => { window.clearTimeout(timer); timer = window.setTimeout(() => { offset = 0; void load(); }, 300); });
  if (!(await load())) return null;
  box.append(heading, h('div', { class: 'ewfm-subs-tools' }, search,
    h('a', { class: 'ew-btn ew-btn--secondary', href: `/api/v1/${BASE}/elements/${el.id}/submissions.csv?lang=${state.lang}`, download: '' }, t('forms.export_csv')),
    h('a', { class: 'ew-btn ew-btn--secondary', href: `/api/v1/${BASE}/elements/${el.id}/submissions.xlsx?lang=${state.lang}`, download: '' }, t('forms.export_xlsx'))), body);
  return box;
}

async function adminPage(panel: HTMLElement): Promise<void> {
  const settings = await pluginSettingsForm('content.forms', [{ key: 'clamd', label: t('forms.admin.clamd'), type: 'text', help: t('forms.admin.clamd_help') }]);
  panel.replaceChildren(pageHeader(t('forms.admin.title')), h('p', null, t('forms.admin.intro')), settings.el);
}

export function register(): void {
  registerAdminPage({ id: 'forms', label: () => t('forms.admin.title'), render: adminPage });
  registerWidget('form', formWidget);
  registerEditor('content.forms', createFormEditor);
  registerElementExtension({ id: 'content.forms.submissions', order: 20, render: submissions });
}
