// Admin page: approve or revoke shell commands used by the shell macro.
import { api, button, checkbox, confirmDialog, errorText, field, fmtDate, formDialog, h, mount, pageHeader, registerAdminPage, state, t, textArea, toast } from '@ewiki/sdk';
import './style.css';

const BASE = 'x/macro.shell';

interface Command {
  hash: string; command: string; status: 'pending' | 'approved' | 'revoked'; element_id: number | null;
  requested_by: string | null; requested_at: string; decided_by: string | null; decided_at: string | null;
  last_run_at: string | null; last_exit: number | null; last_ms: number | null;
}
interface ListResponse { commands: Command[]; enabled: boolean; can_approve: boolean }

const badge: Record<Command['status'], string> = { pending: 'ew-badge--warn', approved: 'ew-badge--ok', revoked: '' };

async function render(el: HTMLElement): Promise<void> {
  const data = await api.get<ListResponse>(`${BASE}/commands`);
  const refresh = () => void render(el).catch((e) => toast(errorText(e), 'error'));
  const act = async (fn: () => Promise<unknown>, msg: string) => {
    try { await fn(); toast(msg); refresh(); } catch (e) { toast(errorText(e), 'error'); }
  };
  const add = () => {
    const cmd = textArea({ rows: 3 });
    cmd.classList.add('ewh-mono');
    const approve = checkbox(t('macro.shell.approve_now'), data.can_approve);
    formDialog(t('macro.shell.add'), [field(t('macro.shell.command'), cmd, { required: true, help: t('macro.shell.command_help') }), data.can_approve ? approve.el : null],
      t('common.save'), async () => {
        await api.post(`${BASE}/commands`, { command: cmd.value, approve: data.can_approve && approve.input.checked });
        toast(t('macro.shell.saved'));
        refresh();
      }, { wide: true });
  };
  const rows = data.commands.map((c) => h('tr', null,
    h('td', null, h('pre', { class: 'ew-shell-cmd' }, c.command), h('span', { class: 'ew-muted ew-hash', title: c.hash }, c.hash.slice(0, 12))),
    h('td', null, h('span', { class: ['ew-badge', badge[c.status]] }, t(`macro.shell.status.${c.status}`)),
      c.decided_by ? h('div', { class: 'ew-muted' }, `${c.decided_by}, ${fmtDate(c.decided_at, state.lang)}`) : null),
    h('td', null, c.requested_by ?? '–', h('div', { class: 'ew-muted' }, fmtDate(c.requested_at, state.lang)),
      c.element_id ? h('a', { href: `/e/${c.element_id}` }, t('macro.shell.page')) : null),
    h('td', null, c.last_run_at ? `${fmtDate(c.last_run_at, state.lang)} · exit ${c.last_exit} · ${c.last_ms} ms` : '–'),
    h('td', { class: 'ew-actions' },
      data.can_approve && c.status !== 'approved' ? button(t('macro.shell.approve'), async () => {
        if (await confirmDialog(t('macro.shell.approve_confirm', { command: c.command }), t('macro.shell.approve'))) await act(() => api.post(`${BASE}/commands/${c.hash}/approve`, {}), t('macro.shell.approved'));
      }, { variant: 'primary' }) : null,
      data.can_approve && c.status === 'approved' ? button(t('macro.shell.revoke'), () => void act(() => api.post(`${BASE}/commands/${c.hash}/revoke`, {}), t('macro.shell.revoked')), { variant: 'secondary' }) : null,
      data.can_approve ? button(t('common.delete'), async () => {
        if (await confirmDialog(t('macro.shell.delete_confirm'), t('common.delete'), true)) await act(() => api.del(`${BASE}/commands/${c.hash}`), t('macro.shell.deleted'));
      }, { variant: 'danger' }) : null)));
  mount(el,
    pageHeader(t('macro.shell.title'), button(t('macro.shell.add'), add, { variant: 'primary' })),
    h('p', null, t('macro.shell.help')),
    !data.enabled ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('macro.shell.disabled')) : null,
    !data.can_approve ? h('p', { class: 'ew-notice ew-notice--info', role: 'note' }, t('macro.shell.no_approver', { user: state.user ? `${state.user.username}@${state.user.provider}` : '' })) : null,
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('macro.shell.title')),
      h('thead', null, h('tr', null, ...[t('macro.shell.command'), t('common.status'), t('macro.shell.requested'), t('macro.shell.last_run'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '5' }, t('macro.shell.empty')))]))));
}

export function register(): void {
  registerAdminPage({ id: 'shell', label: () => t('macro.shell.title'), order: 60, render });
}
