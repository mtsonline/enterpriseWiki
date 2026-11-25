// Help texts: each person decides whether explanations are shown (profile
// or the "?" button, off by default, stored for the person – guests in this
// browser). Texts a person needs to fill in a field correctly (formats,
// limits, required values, warnings) always stay (WCAG 3.3.2): only the
// explanations listed here are hidden. A hidden text is gone for screen
// readers too (it leaves the description of its field), so everybody gets
// the same.
import { api } from './api';
import { t } from './i18n';
import { state } from './state';

/** The keys of explanations that can be hidden; help not listed stays. */
export const HIDE_HELP_KEYS = [
  'acl.preset_help', 'admin.is_admin_help', 'admin.role_permissions_help', 'apikey.allow_admin_help', 'apikey.category_help',
  'apikey.rate_help', 'audit.fw_secret_help', 'audit.log_denied_help', 'audit.log_logins_help', 'audit.text_help', 'auth.ldap.ca_cert_help',
  'auth.ldap.help', 'auth.ldap.sync_create_users_help', 'auth.ldap.sync_enabled_help', 'auth.oidc.help', 'auth.oidc.single_logout_help',
  'auth.shib.help', 'auth.shib.single_logout_help', 'auth.ssh_hint', 'authadmin.instance_help', 'authadmin.preview_help',
  'blocktpl.save_help', 'bo.file_help', 'bo.help', 'cache.enabled_help', 'cache.per_process', 'category.arrange_help',
  'category.default_page_help', 'category.edit_more', 'category.hidden_help', 'category.template_help', 'class.help', 'cms.banner_help',
  'cms.banner_image_help', 'cms.hero_image_help', 'cms.logo_height_help', 'cms.logo_light_help', 'content.blog.category_help',
  'content.blog.intro_help', 'content.blog.more_sources_help', 'content.catlist.category_help', 'content.catlist.depth_help',
  'content.events.categories_help', 'content.events.description_help', 'content.events.list_days_help', 'content.events.more_sources_help',
  'content.events.note_help', 'content.events.reminders_help', 'content.faq.answer_help', 'content.faq.category_help',
  'content.faq.intro_help', 'content.faq.more_sources_help', 'content.notes.hint', 'content.propdb.intro_help', 'content.team.fields_help',
  'content.team.idp_help', 'core.attachment.description_help', 'core.attachment.replace_help',
  'core.carousel.feed_items_help', 'core.carousel.name_help',
  'core.carousel.position_help', 'core.carousel.sources_help', 'core.menu.description_help',
  'core.menu.heading_help', 'core.menu.location_help', 'core.menu.new_submenu_help', 'core.menu.parent_help', 'core.menu.style_help',
  'core.menu.target_menu_help', 'core.reactions.other_help', 'dashboard.help', 'dashboard.offered_help', 'display.columns_help',
  'display.help', 'display.page_size_help', 'display.subcategories_help', 'display.view_help', 'editor.code_lines_help',
  'editor_tools.help', 'element.comment_help', 'email.help', 'erights.help', 'feed.setting_help', 'identity.help',
  'instance.rights_help', 'invite.help', 'jobs.help', 'login_admin.autofill_help', 'login_admin.help', 'login_admin.reg_enabled_help',
  'login_admin.reg_groups_help', 'login_admin.shown_help', 'macro.template_help', 'mail.bounce_help', 'mail.ca_cert_help',
  'mail.concurrency_help', 'mail.digest_help', 'mail.enabled_help', 'mail.sendmail_help', 'mail.transport_help',
  'mapping.email_verified_help', 'mapping.jit_help', 'mapping.keep_unmapped_help', 'media.category_help', 'media.library_help',
  'media.wm.help', 'mfa.admin.all_help', 'mfa.admin.counts_two_help', 'mfa.admin.providers_help', 'picker.source_help',
  'picker.store_in_help', 'placement.types_help', 'plugins.auto_restart_on_drop_help', 'plugins.sources.allow_external_help',
  'profile.card_about_help', 'profile.editor_help', 'profile.readability_help', 'props.cols_help', 'props.default_help', 'props.free_global_help', 'props.free_help',
  'props.group_help', 'props.name_help', 'props.show_on_page_help', 'protect.dl_help', 'protect.wm_help', 'publish.notify_help', 'scim.help',
  'scim.provider_help', 'secrets.help', 'settings.collaboration_help', 'settings.editor_help', 'settings.home_help',
  'settings.language_help', 'settings.languages_help', 'settings.logo_help', 'settings.open_external_help', 'settings.own_tree_help',
  'settings.personal_placement_help', 'settings.photo_default_help', 'settings.prerender_help', 'settings.preview_image_help',
  'settings.readable_urls_help', 'settings.template_help', 'settings.viewer_help', 'signature.keys_help', 'signature.smime.label_help',
  'signature.smime.on_expired_help', 'signature.smime.set_mail_signing_help', 'signature.smime.set_personal_help',
  'signature.smime.set_seal_help', 'sites.accounts_help', 'space.from_template_help', 'space.is_template_help', 'space.mine_help',
  'space.with_pages_help', 'template.admin_help', 'template.d_contrast_help', 'template.d_desktop_drawer_help', 'template.d_preview_help',
  'template.d_structure_help', 'template.help', 'template.rights_help', 'template.versions_help', 'template_try.try_help',
  'translation.category_help', 'translation.original_lang_help', 'waf.mode_help', 'waf.paranoia_help', 'wassign.note_help',
  'watch.frequency_help', 'watch.help', 'webauthn.as_passkey_help', 'webauthn.name_help', 'workflow.approval_help', 'workflow.calendar_help',
  'workflow.condition_help', 'workflow.create_body_help', 'workflow.create_help', 'workflow.deadline_workdays_help', 'workflow.delay_help',
  'workflow.deputy.help', 'workflow.description_help', 'workflow.email_help', 'workflow.escalate_help', 'workflow.field_later_help',
  'workflow.fields_help', 'workflow.file_data_help', 'workflow.file_help', 'workflow.file_store_help', 'workflow.final_help',
  'workflow.http_help', 'workflow.links_help', 'workflow.membership_help', 'workflow.move_help', 'workflow.no_manual_help',
  'workflow.notices_help', 'workflow.notify_help', 'workflow.parallel_help', 'workflow.parallel_join_help', 'workflow.placeholders_help',
  'workflow.props_help', 'workflow.set_help', 'workflow.sign.step_help', 'workflow.step_form_help', 'workflow.steps_help',
  'workflow.subflow_help', 'workflow.subflow_wait_help', 'workflow.trigger_category_help', 'workflow.trigger_definer_help',
  'workflow.trigger_prop_help', 'workflow.trigger_sources_help', 'workflow.triggers_help', 'workflow.webhook_help',
];

const STORE = 'ewiki.help';
let matchers: RegExp[] | null = null;
let matchersLang = '';
let observer: MutationObserver | null = null;
let pending = 0;
const seen = new WeakMap<Element, { text: string; lang: string; explanation: boolean }>();

/** Whether the explanations are shown. */
export function helpShown(): boolean {
  if (state.user) return state.user.prefs?.help === 'show';
  try { return localStorage.getItem(STORE) === 'show'; } catch { return false; }
}

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Whether a text is one of the explanations (with any values filled in). */
export function isExplanation(text: string): boolean {
  if (!matchers || matchersLang !== state.lang) {
    matchersLang = state.lang;
    matchers = HIDE_HELP_KEYS.map((k) => {
      const parts = t(k).split(/\{[a-z_]+\}/);
      return new RegExp(`^${parts.map((p) => escapeRe(p.trim())).join('[\\s\\S]*')}$`);
    });
  }
  const x = text.trim();
  return x !== '' && matchers.some((m) => m.test(x));
}

/** Hides or shows the explanations below root and keeps the descriptions
 * of fields in step (a hidden text does not describe its field). */
export function syncHelp(root: ParentNode = document): void {
  const off = !helpShown();
  if (document.documentElement.classList.contains('ew-help-off') !== off) {
    document.documentElement.classList.toggle('ew-help-off', off);
    // the "?" buttons follow a change made elsewhere (profile)
    document.dispatchEvent(new CustomEvent('ew:help'));
  }
  const hidden = new Set<string>();
  root.querySelectorAll<HTMLElement>('.ew-help').forEach((el) => {
    const text = el.textContent ?? '';
    let c = seen.get(el);
    if (!c || c.text !== text || c.lang !== state.lang) {
      c = { text, lang: state.lang, explanation: isExplanation(text) };
      seen.set(el, c);
    }
    const hide = off && c.explanation;
    el.classList.toggle('ew-help--hidden', hide);
    if (hide && el.id) hidden.add(el.id);
  });
  document.querySelectorAll<HTMLElement>('[aria-describedby], [data-ew-describedby]').forEach((el) => {
    const all = el.dataset.ewDescribedby ?? el.getAttribute('aria-describedby') ?? '';
    const ids = all.split(/\s+/).filter(Boolean);
    const shown = ids.filter((id) => !(document.getElementById(id)?.classList.contains('ew-help--hidden')));
    if (shown.length === ids.length) {
      if (el.dataset.ewDescribedby !== undefined) { el.setAttribute('aria-describedby', all); delete el.dataset.ewDescribedby; }
      return;
    }
    el.dataset.ewDescribedby = all;
    if (shown.length) el.setAttribute('aria-describedby', shown.join(' '));
    else el.removeAttribute('aria-describedby');
  });
}

/** Keeps new parts of the page in step (once per frame). */
export function watchHelp(): void {
  if (observer) return;
  observer = new MutationObserver((list) => {
    // typing in an editor changes the page all the time: not a reason
    if (pending || list.every((m) => (m.target instanceof Element ? m.target : m.target.parentElement)?.closest('[contenteditable=true]'))) return;
    pending = requestAnimationFrame(() => { pending = 0; syncHelp(); });
  });
  observer.observe(document.body, { childList: true, subtree: true });
  syncHelp();
}

/** Shows or hides the explanations, for the person (or this browser). */
export async function setHelp(show: boolean): Promise<void> {
  if (state.user) {
    await api.put('me', { prefs: { help: show ? 'show' : '' } });
    const prefs = { ...(state.user.prefs ?? {}) };
    if (show) prefs.help = 'show';
    else delete prefs.help;
    state.user.prefs = prefs;
  } else {
    try {
      if (show) localStorage.setItem(STORE, 'show');
      else localStorage.removeItem(STORE);
    } catch {
      // storage blocked: the choice holds for this page only
    }
  }
  syncHelp();
}
