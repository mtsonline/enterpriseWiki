// Consent (Administration → Consent): the privacy policy to accept before
// the wiki is used, and the cookie banner with its categories.
import { api, qs } from '../core/api';
import { h, trustedHTML, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { isAdmin, state } from '../core/state';
import { consentCategories, cookieChoice, guestPrivacyAccepted, saveCookieChoice, saveGuestPrivacy } from '../core/consent';
import { button, errorText } from './ui';
import { logout } from './widgets';
import { toast } from '../core/announce';

/** paragraphs of a plain text set by the administration (or the standard text) */
function texts(own: Record<string, string> | undefined, fallback: string): HTMLElement[] {
  const text = pick(own ?? {}, state.lang, state.site.default_language) || fallback;
  return text.split(/\n{2,}/).map((p) => h('p', null, p));
}

/** A modal dialog without a close button: only the given choices end it. */
function mustChoose(title: string, body: HTMLElement[], actions: HTMLElement[], cls: string): HTMLDialogElement {
  const id = uid('consent');
  const d = h('dialog', { class: ['ew-dialog', 'ew-dialog--wide', 'ew-consent', cls], 'aria-labelledby': id },
    h('div', { class: 'ew-dialog-head' }, h('h2', { id }, title)),
    h('div', { class: 'ew-dialog-body' }, ...body),
    h('div', { class: 'ew-dialog-actions' }, ...actions)) as HTMLDialogElement;
  // Escape does not dismiss it
  d.addEventListener('cancel', (e) => e.preventDefault());
  document.body.append(d);
  d.showModal();
  d.querySelector<HTMLElement>('.ew-dialog-actions button')?.focus();
  return d;
}

/** the privacy policy as content (read with the rights of the reader) */
async function policy(page: number): Promise<HTMLElement> {
  const box = h('div', { class: 'ew-consent-policy', tabindex: '0', role: 'region', 'aria-label': t('consent.privacy_policy') });
  try {
    const v = await api.get<{ html: string; title?: string }>(`elements/${page}${qs({ lang: state.lang })}`);
    box.append(trustedHTML(v.html));
  } catch {
    box.append(h('p', null, h('a', { href: `/e/${page}`, target: '_blank', rel: 'noopener' }, t('consent.privacy_policy'))));
  }
  return box;
}

/**
 * privacyGate asks for the acceptance of the privacy policy before the wiki
 * is shown (resolves when it may go on). Signed-in people: Accept or Sign
 * out (administrators may also go on and are asked again next time; the
 * server never locks them out). Guests: as set (nothing, a notice, or
 * required, kept in this browser).
 */
export async function privacyGate(): Promise<void> {
  const p = state.site.consent?.privacy;
  if (!p) return;
  if (state.user) {
    const mine = await api.get<{ enabled: boolean; version: number; accepted_version: number }>('me/consent').catch(() => null);
    if (!mine?.enabled || mine.accepted_version >= mine.version) return;
    const body = [...texts(p.text, t('consent.privacy_text_user')), await policy(p.page)];
    await new Promise<void>((done) => {
      const accept = button(t('consent.accept'), async () => {
        try {
          await api.post('me/consent', { kind: 'privacy', version: p.version });
          d.close(); d.remove(); done();
        } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'primary' });
      const out = button(t('auth.logout'), () => { void logout(); });
      const actions = [accept, out];
      if (isAdmin()) actions.push(button(t('consent.later_admin'), () => { d.close(); d.remove(); done(); }, { variant: 'subtle' }));
      const d = mustChoose(t('consent.privacy_title'), body, actions, 'ew-consent--privacy');
    });
    return;
  }
  if (!p.guests || guestPrivacyAccepted()) return;
  if (p.guests === 'notice') {
    const bar = h('div', { class: 'ew-try-bar ew-consent-notice', role: 'region', 'aria-label': t('consent.privacy_title') },
      h('p', null, t('consent.privacy_notice'), ' ', h('a', { href: `/e/${p.page}` }, t('consent.privacy_policy'))),
      button(t('consent.ok'), () => { saveGuestPrivacy(); bar.remove(); }));
    document.body.prepend(bar);
    return;
  }
  const body = [...texts(p.text, t('consent.privacy_text_guest')), await policy(p.page)];
  await new Promise<void>((done) => {
    const d = mustChoose(t('consent.privacy_title'), body, [
      button(t('consent.accept'), () => { saveGuestPrivacy(); d.close(); d.remove(); done(); }, { variant: 'primary' }),
      h('a', { href: '/login', class: 'ew-btn ew-btn--secondary' }, t('auth.login')),
    ], 'ew-consent--privacy');
  });
}

/** The choice of categories, as check boxes (necessary: always on). */
function choices(current: string[]): { el: HTMLElement; value: () => string[] } {
  const boxes = consentCategories().map((c) => {
    const id = uid('cc');
    const input = h('input', { type: 'checkbox', id, value: c.id, 'aria-describedby': `${id}-d` }) as HTMLInputElement;
    input.checked = current.includes(c.id);
    return { input, el: h('div', { class: 'ew-check' }, input, h('label', { for: id }, c.label()), h('p', { id: `${id}-d`, class: 'ew-help' }, c.description())) };
  });
  const nid = uid('cc');
  const necessary = h('input', { type: 'checkbox', id: nid, checked: true, disabled: true, 'aria-describedby': `${nid}-d` });
  return {
    el: h('fieldset', { class: 'ew-consent-cats' }, h('legend', null, t('consent.categories')),
      h('div', { class: 'ew-check' }, necessary, h('label', { for: nid }, t('consent.cat_necessary')), h('p', { id: `${nid}-d`, class: 'ew-help' }, t('consent.cat_necessary_help'))),
      ...boxes.map((b) => b.el)),
    value: () => boxes.filter((b) => b.input.checked).map((b) => b.input.value),
  };
}

let open: HTMLElement | null = null;

/**
 * cookieBanner asks for the choice of cookies (when switched on and not yet
 * chosen in this version), or – with force – always (Cookie settings).
 * Required: a modal dialog until a choice is made; otherwise a region at
 * the bottom of the page. All three answers are equally visible.
 */
export function cookieBanner(force = false): void {
  const c = state.site.consent?.cookies;
  if (!c || open || (!force && cookieChoice() !== null)) return;
  const id = uid('cookies');
  const sel = choices(cookieChoice() ?? []);
  sel.el.hidden = true;
  const finish = (cats: string[]) => {
    saveCookieChoice(cats);
    if (box instanceof HTMLDialogElement) box.close();
    box.remove();
    open = null;
    toast(t('consent.saved'));
  };
  const all = button(t('consent.accept_all'), () => finish(consentCategories().map((x) => x.id)), { variant: 'primary' });
  const none = button(t('consent.only_necessary'), () => finish([]), { variant: 'primary' });
  const pickBtn = button(t('consent.choose'), () => {
    if (sel.el.hidden) {
      sel.el.hidden = false;
      pickBtn.textContent = t('consent.save_choice');
      sel.el.querySelector<HTMLInputElement>('input:not([disabled])')?.focus();
    } else finish(sel.value());
  }, { variant: 'primary' });
  const body = [
    ...texts(c.text, t('consent.cookies_text')),
    c.page ? h('p', null, h('a', { href: `/e/${c.page}`, target: '_blank', rel: 'noopener' }, t('consent.cookie_policy'))) : null,
    sel.el,
  ].filter(Boolean) as HTMLElement[];
  const actions = [all, none, pickBtn];
  let box: HTMLElement;
  if (c.required && !force) {
    box = mustChoose(t('consent.cookies_title'), body, actions, 'ew-consent--cookies');
  } else {
    box = h('section', { class: 'ew-consent-banner', 'aria-labelledby': id },
      h('h2', { id }, t('consent.cookies_title')), ...body,
      h('div', { class: 'ew-dialog-actions' }, ...actions,
        force ? button(t('common.cancel'), () => { box.remove(); open = null; }, { variant: 'subtle' }) : null));
    // first in the page, so it is met first (shown at the bottom); the page
    // gets room below, so nothing focused hides behind it (WCAG 2.4.11)
    document.body.prepend(box);
    const room = new ResizeObserver(() => {
      const px = box.isConnected ? `${box.offsetHeight}px` : '';
      document.body.style.paddingBottom = px;
      document.documentElement.style.scrollPaddingBottom = px;
    });
    room.observe(box);
    new MutationObserver((_, mo) => {
      if (!box.isConnected) { room.disconnect(); mo.disconnect(); document.body.style.paddingBottom = ''; document.documentElement.style.scrollPaddingBottom = ''; }
    }).observe(document.body, { childList: true });
    if (force) all.focus();
  }
  open = box;
}

/** The block "Cookie settings": opens the choice again (placed e.g. in the footer). */
export function cookieSettingsBlock(): HTMLElement | null {
  if (!state.site.consent?.cookies) return null;
  return h('p', { class: 'ew-cookie-settings' }, button(t('consent.settings'), () => cookieBanner(true), { variant: 'subtle' }));
}
