// Administration → Consent: the cookie banner and the privacy policy people
// accept, and the proof who accepted which version when.
import { api, qs } from '../core/api';
import { personName } from '../components/person';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { hasPerm, state } from '../core/state';
import { toast } from '../core/announce';
import { button, checkbox, errorText, field, pageHeader, select, textArea } from '../components/ui';
import { elementSelect } from '../components/pickers';
import { langName } from '../components/widgets';

interface Consent {
  cookies: { enabled: boolean; required: boolean; version: number; text: Record<string, string> | null; page: number };
  privacy: { enabled: boolean; version: number; page: number; guests: '' | 'notice' | 'required'; text: Record<string, string> | null };
}
interface ConsentRow { user_id: number; username: string; display_name: string; version: number; accepted_at: string; open?: boolean }

const ver = (v: number) => Math.max(v, 1);

function langTexts(prefix: string, value: Record<string, string> | null) {
  const list = state.site.languages.map((l) => ({ l, a: textArea({ value: value?.[l] ?? '', rows: 3, lang: l, name: `${prefix}-${l}` }) }));
  return {
    els: list.map((x) => field(t('consent.text_in', { lang: langName(x.l) }), x.a, { help: t('consent.text_help') })),
    value: () => Object.fromEntries(list.filter((x) => x.a.value.trim()).map((x) => [x.l, x.a.value.trim()])),
  };
}

export async function consentAdmin(panel: HTMLElement): Promise<void> {
  const c = await api.get<Consent>('admin/consent');
  // cookies
  const ckOn = checkbox(t('consent.cookies_on'), c.cookies.enabled, { help: t('consent.cookies_on_help') });
  const ckReq = checkbox(t('consent.cookies_required'), c.cookies.required, { help: t('consent.cookies_required_help') });
  const ckText = langTexts('cookies', c.cookies.text);
  const ckPage = await elementSelect({ selected: c.cookies.page || undefined });
  const ckAgain = checkbox(t('consent.ask_again', { version: ver(c.cookies.version) }), false, { help: t('consent.ask_again_help') });
  // privacy
  const pvOn = checkbox(t('consent.privacy_on'), c.privacy.enabled, { help: t('consent.privacy_on_help') });
  const pvPage = await elementSelect({ selected: c.privacy.page || undefined });
  const pvGuests = select([
    { value: '', label: t('consent.guests_none') }, { value: 'notice', label: t('consent.guests_notice') }, { value: 'required', label: t('consent.guests_required') },
  ], c.privacy.guests, 'guests');
  const pvText = langTexts('privacy', c.privacy.text);
  const pvAgain = checkbox(t('consent.ask_again', { version: ver(c.privacy.version) }), false, { help: t('consent.ask_again_help') });
  const form = h('form', { class: 'ew-form', novalidate: true },
    h('fieldset', null, h('legend', null, t('consent.cookies_title')), ckOn.el, ckReq.el, ...ckText.els,
      h('fieldset', { class: 'ew-subfield' }, h('legend', null, t('consent.cookie_policy_page')), ckPage.el), ckAgain.el),
    h('fieldset', null, h('legend', null, t('consent.privacy_title')), pvOn.el,
      h('fieldset', { class: 'ew-subfield' }, h('legend', null, t('consent.privacy_policy_page')), pvPage.el),
      field(t('consent.guests'), pvGuests, { help: t('consent.guests_help') }), ...pvText.els, pvAgain.el),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const next: Consent = {
      cookies: { enabled: ckOn.input.checked, required: ckReq.input.checked, version: ver(c.cookies.version) + (ckAgain.input.checked ? 1 : 0), text: ckText.value(), page: ckPage.value() || 0 },
      privacy: { enabled: pvOn.input.checked, version: ver(c.privacy.version) + (pvAgain.input.checked ? 1 : 0), page: pvPage.value() || 0, guests: pvGuests.value as Consent['privacy']['guests'], text: pvText.value() },
    };
    try {
      await api.put('admin/consent', next);
      // the message once the page shows the saved state
      try { await consentAdmin(panel); } finally { toast(t('common.saved')); }
    } catch (e) { toast(errorText(e), 'error'); }
  });
  const parts: HTMLElement[] = [pageHeader(t('admin.consent')), h('p', null, t('consent.intro')), form];
  // proof: who accepted which version when
  if (hasPerm('core.audit.view')) {
    const res = await api.get<{ items: ConsentRow[]; total: number }>(`admin/consents${qs({ limit: 100 })}`);
    const rows = res.items.map((r) => h('tr', null, h('td', null, personName(r.display_name || r.username, r.open ? r.user_id : null)), h('td', null, r.username), h('td', null, String(r.version)), h('td', null, fmtDate(r.accepted_at, state.lang))));
    parts.push(h('section', { class: 'ew-section', 'aria-labelledby': 'cp-h' },
      h('h2', { id: 'cp-h' }, t('consent.proof')),
      h('p', null, t('consent.proof_count', { n: res.items.length, total: res.total }), ' ', h('a', { href: `/api/v1/admin/consents${qs({ format: 'csv' })}`, download: 'privacy-consents.csv' }, t('consent.proof_csv'))),
      h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('consent.proof')),
        h('thead', null, h('tr', null, ...[t('consent.person'), t('consent.username'), t('consent.version'), t('consent.accepted_at')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: '4' }, t('consent.proof_none')))]))))));
  }
  mount(panel, ...parts);
}
