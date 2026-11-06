// Types of API objects.

export type LangMap = Record<string, string>;

export interface Principal {
  id: number;
  username: string;
  provider: string;
  display_name: string;
  email: string;
  language: string;
  groups: number[] | null;
  roles: number[] | null;
  is_admin: boolean;
  /** manages sites, users, groups, roles and the license */
  instance_admin?: boolean;
  permissions: string[];
  via: string;
}

export interface ElementTypeInfo {
  id: string;
  plugin: string;
  labels: LangMap;
  empty_body: unknown;
}

export interface Site {
  name: string;
  /** name of this site of the instance ("main" for the first one) */
  site: string;
  /** settings only instance administrators change */
  instance_only: string[];
  /** default accounts of new sites (shared | separate) and whether this site has its own */
  sites_accounts?: 'shared' | 'separate';
  separate_accounts?: boolean;
  template: string;
  home_category: number;
  home_element: number;
  languages: string[];
  default_language: string;
  plugins: string[] | null;
  element_types: ElementTypeInfo[];
  auth_providers: { id: string; name: string }[];
  version: string;
}

export interface Category {
  id: number;
  parent_id: number | null;
  path: number[];
  number: string;
  position: number;
  slug: string;
  titles: LangMap;
  descriptions: LangMap;
  tags: string[];
  default_element_id: number | null;
  feed: 'off' | 'on' | 'recursive';
  short_url: string;
  created_at: string;
  updated_at: string;
  has_children: boolean;
  element_count: number;
  permissions: string[] | null;
  original_lang: string;
  text_rev: number;
  translations: { lang: string; status: 'original' | 'current' | 'outdated' | 'missing'; based_on?: number }[];
  translated_percent: number;
  translations_up_to_date: boolean;
  /** readable path (/wiki/…), single categories only */
  url?: string;
  /** readable path per language */
  urls?: Record<string, string>;
  short_only?: boolean;
  /** not in navigation, menus, listings and search (e.g. folders for media) */
  hidden?: boolean;
}

/** A file of the media library (GET /api/v1/media/{id}). */
export interface Medium {
  id: number;
  category_id: number;
  name: string;
  title: string;
  alt: string;
  description: string;
  version: number;
  mime: string;
  size: number;
  width: number;
  height: number;
  download: boolean;
  own_acl: boolean;
  watermark: Watermark;
  created_by?: number;
  created_at: string;
  updated_at: string;
  permissions: string[];
  view_only: boolean;
  url: string;
}

export type WatermarkMode = 'none' | 'uploader_only' | 'uploader_view' | 'viewer_view' | 'viewer_all';
export interface Watermark { mode: WatermarkMode; user: boolean; date: boolean; comment: string }
export interface MediaRule { subject_type: string; subject_id?: number | null; perms: string[] }
export interface MediaVersion { version: number; mime: string; size: number; width: number; height: number; ops: unknown[]; note: string; created_by?: number; created_at: string }

export interface ElementSummary {
  id: number;
  category_id: number;
  type: string;
  position: number;
  original_lang: string;
  titles: LangMap;
  tags: string[];
  created_at: string;
  updated_at: string;
  updated_by: string;
  is_default: boolean;
  short_url: string;
  short_only?: boolean;
  preview_image: string;
}

export interface LangStatus {
  lang: string;
  rev: number;
  source_rev: number | null;
  status: 'original' | 'current' | 'outdated' | 'missing';
  updated_at: string | null;
}

export interface ElementView extends ElementSummary {
  /** readable path (/wiki/…) in the requested language */
  url?: string;
  /** readable path per language */
  urls?: Record<string, string>;
  lang: string;
  shown_lang: string;
  fallback: boolean;
  rev: number;
  title: string;
  body?: unknown;
  html: string;
  translations: LangStatus[];
  translated_percent: number;
  up_to_date: boolean;
  permissions: string[] | null;
  type_active: boolean;
}

export interface Revision {
  lang: string;
  rev: number;
  source_rev: number | null;
  title: string;
  comment: string;
  restored_from: number | null;
  author: string;
  created_at: string;
  body?: unknown;
  html?: string;
}

export interface Rule {
  id: number;
  category_id: number;
  subject_type: 'user' | 'group' | 'role' | 'guest' | 'authenticated';
  subject_id: number | null;
  permissions: number;
  actions: string[];
  inherit: boolean;
}

export interface Group { id: number; name: string; description: string; source: string; members: number; roles: number[] }
export interface Role { id: number; name: string; description: string; is_admin: boolean; members: number; permissions: string[] }

export interface AuditEntry {
  id: number;
  at: string;
  user_id: number | null;
  actor: string;
  action: string;
  entity_type: string;
  entity_id: number | null;
  before?: unknown;
  after?: unknown;
}

export const PERMS = [
  { bit: 1, key: 'view' },
  { bit: 2, key: 'edit' },
  { bit: 4, key: 'create' },
  { bit: 8, key: 'delete' },
  { bit: 16, key: 'manage' },
] as const;
