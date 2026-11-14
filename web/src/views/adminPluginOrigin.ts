// Origin of core plugins: the check before an installation, the origin
// column of the plugin list and the trusted sources (concept 5.11).
import { api, ApiError } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, textInput } from '../components/ui';

export type OriginLevel = 'official' | 'organisation' | 'external' | 'unknown';

export interface Origin {
  level: OriginLevel;
  source?: string;
  trusted?: string;
  ref?: string;
  commit?: string;
  installed_by?: string;
  installed_at?: string;
  accepted?: boolean;
}

interface SourceCheck { level: OriginLevel; source: string; trusted?: string; allowed: boolean }
interface TrustedSource { prefix: string; note?: string }

function table(caption: string, headers: string[], rows: HTMLElement[], emptyText: string): HTMLElement {
  return h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, caption),
    h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, emptyText))])));
}

const BADGE: Record<OriginLevel, string> = { official: 'ok', organisation: 'info', external: 'warn', unknown: 'warn' };

/** originBadge shows the origin level of an installed core plugin. */
export function originBadge(o: Origin | undefined): HTMLElement {
  const level = o?.level ?? 'unknown';
  return h('span', { class: `ew-badge ew-badge--${BADGE[level]}` }, t(`plugins.origin.level.${level}`));
}

/** originDetails: source and commit of an installed core plugin. */
export function originDetails(o: Origin | undefined): HTMLElement | null {
  if (!o?.source) return null;
  return h('p', { class: 'ew-help' }, h('span', null, o.source),
    o.commit ? [h('br'), t('plugins.origin.commit'), ' ', h('code', null, o.commit.slice(0, 12))] : null);
}

/**
 * confirmInstall checks a source and asks for confirmation: always with the
 * warning for core plugins, for external sources additionally with a box
 * that must be ticked. It answers null when the person cancels.
 */
export async function confirmInstall(source: string): Promise<{ acceptExternal: boolean } | null> {
  const c = await api.post<SourceCheck>('admin/plugins/origin', { source });
  if (!c.allowed) {
    toast(t('plugins.origin.external_forbidden'), 'error');
    return null;
  }
  return askInstall(c.level, c.source, c.trusted);
}

/** askExternal asks once more for an external source (e.g. before an update). */
export function askExternal(source: string): Promise<{ acceptExternal: boolean } | null> {
  return askInstall('external', source);
}

function askInstall(level: OriginLevel, source: string, trusted?: string): Promise<{ acceptExternal: boolean } | null> {
  const external = level === 'external' || level === 'unknown';
  const accept = checkbox(t('plugins.origin.accept_label'));
  if (external) accept.input.setAttribute('aria-describedby', 'origin-ext-warning');
  return new Promise((resolve) => {
    let result: { acceptExternal: boolean } | null = null;
    formDialog(t('plugins.origin.dialog_title'), h('div', null,
      h('p', null, t('plugins.origin.source_label'), ' ', h('code', null, source)),
      h('p', null, originBadge({ level }), ' ', t(`plugins.origin.level_help.${level}`),
        trusted ? [' ', t('plugins.origin.trusted_by', { prefix: trusted })] : null),
      h('p', { class: 'ew-notice ew-notice--warning' }, t('plugins.origin.core_warning')),
      external ? h('div', null, h('p', { class: 'ew-notice ew-notice--danger', id: 'origin-ext-warning' }, t('plugins.origin.external_warning')), accept.el) : null),
    t('plugins.install'), async () => {
      if (external && !accept.input.checked) {
        accept.input.focus();
        throw new Error(t('plugins.origin.accept_required'));
      }
      result = { acceptExternal: external };
    }, { onClose: () => resolve(result) });
  });
}

/** isExternalRefusal: the server wants the confirmation for an external source. */
export function isExternalRefusal(e: unknown): boolean {
  return e instanceof ApiError && e.code === 'external_not_accepted';
}

/** trustedSources is the panel for the trusted sources (instance administrators). */
export async function trustedSources(reload: () => void): Promise<HTMLElement> {
  const data = await api.get<{ sources: TrustedSource[]; allow_external: boolean }>('admin/plugins/sources');
  const prefix = textInput({ placeholder: 'https://git.example.com/team' });
  const note = textInput({ maxlength: 200 });
  const form = h('form', { class: 'ew-form', novalidate: true },
    h('h3', null, t('plugins.sources.add_title')),
    field(t('plugins.sources.prefix'), prefix, { required: true, help: t('plugins.sources.prefix_help') }),
    field(t('plugins.sources.note'), note),
    h('div', { class: 'ew-form-actions' }, button(t('plugins.sources.add'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    if (!prefix.value.trim()) { prefix.focus(); return; }
    try {
      await api.post('admin/plugins/sources', { prefix: prefix.value.trim(), note: note.value.trim() });
      toast(t('plugins.sources.added'));
      reload();
    } catch (e) { toast(errorText(e), 'error'); }
  });
  const allow = checkbox(t('plugins.sources.allow_external'), data.allow_external, { help: t('plugins.sources.allow_external_help') });
  allow.input.addEventListener('change', async () => {
    const on = allow.input.checked;
    try {
      await api.put('admin/plugins/sources/external', { allow: on });
      toast(t('common.saved'));
    } catch (e) {
      allow.input.checked = !on;
      toast(errorText(e), 'error');
    }
  });
  return h('section', { class: 'ew-panel', 'aria-labelledby': 'trusted-h' },
    h('h2', { id: 'trusted-h' }, t('plugins.sources.title')),
    h('p', null, t('plugins.sources.help')),
    table(t('plugins.sources.title'), [t('plugins.sources.prefix'), t('plugins.sources.note'), t('common.actions')],
      data.sources.map((s) => h('tr', null, h('th', { scope: 'row' }, h('code', null, s.prefix)), h('td', null, s.note ?? ''),
        h('td', null, button(t('common.remove'), async () => {
          if (!(await confirmDialog(t('plugins.sources.remove_confirm', { prefix: s.prefix }), t('common.remove'), true))) return;
          try {
            await api.del(`admin/plugins/sources?prefix=${encodeURIComponent(s.prefix)}`);
            toast(t('plugins.sources.removed'));
            reload();
          } catch (e) { toast(errorText(e), 'error'); }
        }, { variant: 'link', ariaLabel: t('plugins.sources.remove_named', { prefix: s.prefix }) })))), t('plugins.sources.none')),
    form, allow.el);
}
