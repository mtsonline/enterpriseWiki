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
  template: string;
  home_category: number;
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
  created_at: string;
  updated_at: string;
  has_children: boolean;
  element_count: number;
  permissions: string[] | null;
}

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
}

export interface LangStatus {
  lang: string;
  rev: number;
  source_rev: number | null;
  status: 'original' | 'current' | 'outdated' | 'missing';
  updated_at: string | null;
}

export interface ElementView extends ElementSummary {
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
  inherit: boolean;
}

export interface Group { id: number; name: string; description: string; source: string; members: number; roles: number[] }
export interface Role { id: number; name: string; description: string; is_admin: boolean; members: number }

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
