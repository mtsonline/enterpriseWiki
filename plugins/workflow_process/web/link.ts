// The page behind the decision links of task e-mails (/workflow/link?t=…).
import { api, button, errorText, field, fmtDate, h, mount, state, t, textArea } from '@ewiki/sdk';
import { BASE } from './widget';

interface LinkInfo {
  case_id: number; element_id: number; title: string; step: string; action: string; label: string;
  note: '' | 'optional' | 'required'; expires: string; sign?: boolean; mine: boolean; signed_in: boolean; without_login: boolean;
}

const label = (x: LinkInfo) => (x.action === 'approve' || x.action === 'reject') && (x.label === 'approve' || x.label === 'reject')
  ? t(`workflow.${x.label}`) : x.label;

export async function linkView({ main }: { main: HTMLElement }): Promise<void> {
  const tok = new URLSearchParams(location.search).get('t') ?? '';
  // the token stays out of the address bar, the history and later referrers
  if (tok) history.replaceState(history.state, '', '/workflow/link');
  const title = h('h1', null, t('workflow.link.title'));
  if (!tok) { mount(main, title, h('p', { class: 'ew-field-error' }, t('workflow.link.invalid'))); return; }
  let info: LinkInfo;
  try {
    info = await api.get<LinkInfo>(`${BASE}/links/info?t=${encodeURIComponent(tok)}`);
  } catch (e) { mount(main, title, h('p', { class: 'ew-field-error', role: 'alert' }, errorText(e))); return; }
  const target = `/e/${info.element_id}?case=${info.case_id}&act=${encodeURIComponent(info.action)}`;
  // signed in as the addressee: the regular dialog of the case with the decision
  if (info.mine) { location.assign(target); return; }
  const what = h('div', null, h('p', null, t('workflow.link.what', { action: label(info), step: info.step, title: info.title })),
    h('p', { class: 'ew-help' }, t('workflow.link.valid_until', { date: fmtDate(info.expires, state.lang) })));
  const login = h('a', { href: `/login?next=${encodeURIComponent(target)}` }, t('workflow.link.login'));
  if (info.signed_in && !info.mine) {
    mount(main, title, what, h('p', { class: 'ew-field-error' }, t('workflow.link.other_user')), h('p', null, login));
    return;
  }
  if (!info.without_login) { mount(main, title, what, h('p', null, t(info.sign ? 'workflow.link.sign' : 'workflow.link.need_login')), h('p', null, login)); return; }
  const status = h('p', { role: 'status', 'aria-live': 'polite' });
  const note = textArea({ rows: 3 });
  const send = button(t('workflow.link.confirm', { action: label(info) }), () => undefined, { type: 'submit', variant: 'primary' });
  const form = h('form', {
    class: 'ew-form',
    onsubmit: async (ev: Event) => {
      ev.preventDefault();
      if (info.note === 'required' && !note.value.trim()) { status.textContent = t('workflow.note_missing'); note.focus(); return; }
      send.disabled = true;
      try {
        await api.post(`${BASE}/links/act`, { t: tok, note: note.value.trim() });
        mount(main, title, h('p', { role: 'status' }, t('workflow.link.done', { action: label(info), title: info.title })));
      } catch (e) { status.textContent = errorText(e); send.disabled = false; }
    },
  },
  info.note ? field(t(info.note === 'required' ? 'workflow.note_required' : 'workflow.note_optional'), note, { required: info.note === 'required' }) : null,
  h('div', { class: 'ew-actions' }, send), status);
  mount(main, title, what, h('p', { class: 'ew-help' }, t('workflow.link.without_login_help'), ' ', login), form);
}
