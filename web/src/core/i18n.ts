// UI translations. All strings come from the API (core + plugins) so every
// string is translatable in the admin UI.
import { api } from './api';

let messages: Record<string, string> = {};
let current = 'en';

/** Loads the messages of lang. */
export async function loadMessages(lang: string): Promise<void> {
  messages = await api.get<Record<string, string>>(`i18n/${encodeURIComponent(lang)}`);
  current = lang;
  document.documentElement.lang = lang;
}

/** Current UI language. */
export function uiLang(): string {
  return current;
}

const rules = new Map<string, Intl.PluralRules>();

/** The plural form (CLDR category) of n in the UI language. */
export function pluralForm(n: number, lang = current): string {
  let r = rules.get(lang);
  if (!r) {
    try { r = new Intl.PluralRules(lang); } catch { r = new Intl.PluralRules('en'); }
    rules.set(lang, r);
  }
  return r.select(n);
}

/**
 * Translates key, replacing {name} placeholders. Plural texts have one key
 * per form (key_one, key_other, key_few …); the parameter count (or n)
 * selects the form. Missing keys show the key.
 */
export function t(key: string, params?: Record<string, string | number>): string {
  let s = messages[key];
  const n = params?.count ?? params?.n;
  if (messages[`${key}_other`] !== undefined) {
    const num = typeof n === 'number' ? n : Number(n);
    s = n !== undefined && !Number.isNaN(num) ? (messages[`${key}_${pluralForm(num)}`] ?? messages[`${key}_other`]) : (s ?? messages[`${key}_other`]);
  }
  s ??= key;
  if (params) for (const [k, v] of Object.entries(params)) s = s.split(`{${k}}`).join(String(v));
  return s;
}

/** Picks the text of a language map in lang, falling back to any language. */
export function pick(m: Record<string, string> | null | undefined, lang = current, fallback?: string): string {
  if (!m) return '';
  return m[lang] || (fallback ? m[fallback] : '') || Object.values(m)[0] || '';
}
