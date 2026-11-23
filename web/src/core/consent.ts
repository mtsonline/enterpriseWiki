// Consent to cookies and similar storage (Administration → Consent): the
// categories, the choice of this browser and the question "may I?" for the
// wiki and its plugins. The choice itself is necessary storage (kept in the
// browser, never sent anywhere).
import { t } from './i18n';
import { state } from './state';

export interface ConsentCategory {
  /** id, e.g. "external_media"; plugins use their plugin id as prefix */
  id: string;
  label: () => string;
  description: () => string;
}

const categories: ConsentCategory[] = [
  { id: 'external_media', label: () => t('consent.cat_external_media'), description: () => t('consent.cat_external_media_help') },
];

/** registerConsentCategory adds a category to the cookie banner (plugins: id with the plugin id as prefix). */
export function registerConsentCategory(c: ConsentCategory): void {
  if (!/^[a-z][a-z0-9_.-]{1,80}$/.test(c.id) || categories.some((x) => x.id === c.id)) return;
  categories.push(c);
}

export function consentCategories(): ConsentCategory[] {
  return [...categories];
}

const KEY = 'ewiki.consent.cookies';
const PRIVACY_KEY = 'ewiki.consent.privacy';

interface Choice { v: number; cats: string[]; at: string }

function read<T>(key: string): T | null {
  try {
    return JSON.parse(localStorage.getItem(key) || 'null') as T | null;
  } catch {
    return null;
  }
}

function write(key: string, v: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(v));
  } catch { /* no storage: asked again next time */ }
}

/** cookieChoice: the choice of this browser for the version in force (null: not yet made). */
export function cookieChoice(): string[] | null {
  const c = state.site.consent?.cookies;
  if (!c) return null;
  const v = read<Choice>(KEY);
  return v && v.v === c.version && Array.isArray(v.cats) ? v.cats.filter((x) => typeof x === 'string') : null;
}

const listeners: (() => void)[] = [];

/** onConsentChange runs fn after the choice changed. */
export function onConsentChange(fn: () => void): void {
  listeners.push(fn);
}

export function saveCookieChoice(cats: string[]): void {
  const c = state.site.consent?.cookies;
  if (!c) return;
  const known = new Set(categories.map((x) => x.id));
  write(KEY, { v: c.version, cats: cats.filter((x) => known.has(x)), at: new Date().toISOString() });
  for (const fn of listeners) fn();
}

/** consentGiven: may this category be used? Without the banner (switched
 * off) the wiki asks in other ways (e.g. two clicks for embeds): false. */
export function consentGiven(category: string): boolean {
  return cookieChoice()?.includes(category) ?? false;
}

/** guestPrivacyAccepted: a guest accepted the version in force in this browser. */
export function guestPrivacyAccepted(): boolean {
  const p = state.site.consent?.privacy;
  return !!p && read<{ v: number }>(PRIVACY_KEY)?.v === p.version;
}

export function saveGuestPrivacy(): void {
  const p = state.site.consent?.privacy;
  if (p) write(PRIVACY_KEY, { v: p.version, at: new Date().toISOString() });
}
