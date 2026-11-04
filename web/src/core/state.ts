// Global UI state with change notifications.
import type { Principal, Site } from './types';

type Listener = () => void;

export const state = {
  site: null as unknown as Site,
  user: null as Principal | null,
  lang: 'en',
  editMode: false,
  /** the user manages at least one category (central permissions page) */
  managesCategories: false,
};

const listeners = new Set<Listener>();

/** Subscribes to state changes; returns an unsubscribe function. */
export function onChange(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Notifies listeners after a state change. */
export function changed(): void {
  for (const l of [...listeners]) l();
}

function store(key: string, value: string | null): void {
  try {
    if (value === null) sessionStorage.removeItem(key);
    else sessionStorage.setItem(key, value);
  } catch {
    /* storage unavailable */
  }
}

export function stored(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

export function setEditMode(on: boolean): void {
  state.editMode = on;
  store('ewiki.edit', on ? '1' : null);
  changed();
}

export function rememberLang(lang: string): void {
  try {
    localStorage.setItem('ewiki.lang', lang);
  } catch {
    /* ignore */
  }
}

export function rememberedLang(): string | null {
  try {
    return localStorage.getItem('ewiki.lang');
  } catch {
    return null;
  }
}

export function isAdmin(): boolean {
  return !!state.user?.is_admin;
}

/** hasPerm reports whether the user has a global permission (administrators have all). */
/** translatePerm is the permission to translate the user interface into lang. */
export function translatePerm(lang: string): string {
  return `core.i18n.translate.${lang.toLowerCase().replace(/-/g, '_')}`;
}

/** canTranslate: the user may translate the user interface into lang. */
export function canTranslate(lang: string): boolean {
  return hasPerm('core.i18n.manage') || hasPerm(translatePerm(lang));
}

/** canTranslateAny: the user may translate at least one language. */
export function canTranslateAny(): boolean {
  return hasPerm('core.i18n.manage') || state.site.languages.some((l) => hasPerm(translatePerm(l)));
}

export function hasPerm(key: string): boolean {
  return isAdmin() || !!state.user?.permissions?.includes(key);
}
