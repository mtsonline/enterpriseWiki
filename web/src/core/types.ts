// Types of API objects.

export type LangMap = Record<string, string>;

export interface Principal {
  id: number;
  username: string;
  /** optional wiki user name; alias_source: provider that manages it */
  alias?: string;
  alias_source?: string;
  provider: string;
  display_name: string;
  email: string;
  language: string;
  /** personal settings of the user interface (e.g. editor) */
  prefs?: Record<string, string> | null;
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
  /** elements of this type have permissions of their own (a blog, a FAQ …) */
  instance: boolean;
  /** UI texts each instance may change (its "Labels") */
  instance_labels?: string[];
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
  /** active website templates (for the template of a category) */
  /** color_scheme: the default scheme of a template whose colours follow light and dark ("" = light only) */
  templates?: { id: string; name: string; names?: Record<string, string>; color_scheme?: string }[];
  home_category: number;
  home_element: number;
  /** default editor of text pages */
  editor?: 'visual' | 'simple';
  /** live editing of pages (several editors at the same time) */
  collaboration?: boolean;
  /** people may arrange the building blocks for themselves */
  personal_placement?: boolean;
  /** own CSS in templates of data is allowed (ui.custom_css) */
  custom_css?: boolean;
  /** Buttons of the formatting toolbar (['*'] = all). */
  editor_tools?: string[];
  /** Tiles offered for the own dashboard (empty = all). */
  dashboard_tiles?: string[];
  /** the own dashboard is the start page of signed-in people (ui.dashboard_home) */
  dashboard_home?: boolean;
  /** maintenance mode (Administration → Maintenance); absent: off */
  maintenance?: { mode: 'read_only' | 'closed'; message: Record<string, string>; until: string } | null;
  /** asking for consent (Administration → Consent); absent parts: off */
  consent?: {
    cookies?: { required: boolean; version: number; text: Record<string, string>; page: number };
    privacy?: { version: number; page: number; guests: '' | 'notice' | 'required'; text: Record<string, string> };
  } | null;
  /** pages shown on "not found" and "no permission" (0: the standard text) */
  not_found_element?: number;
  forbidden_element?: number;
  /** seconds without an answer to a change after which the user is asked whether to keep waiting */
  wait_notice_s?: number;
  languages: string[];
  default_language: string;
  plugins: string[] | null;
  element_types: ElementTypeInfo[];
  auth_providers: { id: string; name: string; kind?: string }[];
  /** Sign-in page: passkeys in the user name field, automatic redirect to one provider, self-registration. */
  login?: { passkey_autofill: boolean; auto_redirect: string; registration?: boolean; password_reset?: boolean };
  version: string;
  /** a valid license is installed (signature ok, not expired): the seal is shown */
  licensed?: boolean;
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
  /** website template of this category and below ('' = inherited) */
  template?: string;
  /** the template that applies ('' = the site template) */
  effective_template?: string;
  /** Buttons of the formatting toolbar here (null = inherited; ["*"] = all) and the choice that applies (null = the site's). */
  editor_tools?: string[] | null;
  effective_editor_tools?: string[] | null;
  /** key of the space (top level only; address /s/KEY) */
  space_key?: string;
  /** archived itself (date) / it or a category above is archived: read-only */
  archived_at?: string | null;
  archived?: boolean;
  /** classification set here (null = as above) and the one that applies: 0 public … 3 strictly confidential */
  classification?: number | null;
  effective_classification?: number;
  /** files: watermark (auto|off) and downloads (auto|allow) set here (null = as above) */
  watermark?: string | null;
  downloads?: string | null;
}

/** How a category page shows its pages (inherited until a subcategory chooses its own). */
export interface CategoryDisplay {
  view: 'list' | 'cards' | 'tiles' | 'tree' | 'handbook' | 'table' | 'accordion' | 'tabs' | 'az' | 'timeline' | 'all';
  /** further columns of the table (keys of plugins, e.g. properties) */
  columns?: string[];
  /** the date of the timeline */
  date?: 'updated' | 'created';
  sort: 'manual' | 'title' | 'updated' | 'created';
  /** pages per page (0 = the expert setting) */
  page_size: number;
  /** also the pages of the subcategories */
  subcategories: boolean;
  /** the beginning of the text of each page */
  excerpt: boolean;
}

/** One page of the overview of a category (GET categories/{id}/overview). */
export interface CategoryOverview {
  category_titles: LangMap;
  display: CategoryDisplay;
  items: { id: number; category_id: number; type: string; titles: LangMap; original_lang: string; preview_image: string; created_at: string; updated_at: string; hidden?: boolean; excerpt?: string }[];
  total: number;
  page: number;
  pages: number;
  page_size: number;
}

/** A space: a top-level category with settings for its whole subtree. */
export interface SpaceInfo {
  id: number;
  key: string;
  titles: Record<string, string>;
  descriptions: Record<string, string>;
  own_tree: boolean;
  shortcuts: { label: Record<string, string>; url: string }[];
  allowed_types: string[];
  /** macros allowed in the space (empty: all) */
  allowed_macros?: string[];
  /** color of links and buttons in the space; logo picture */
  accent?: string;
  logo?: string;
  language: string;
  /** offered as template for new spaces */
  is_template?: boolean;
  /** the category the own navigation starts from (0: the space) */
  tree_root?: number;
  archived: boolean;
  /** the reader may change the space (settings, templates) */
  can_manage: boolean;
  /** the reader has own permissions here (as user, group or role) */
  member: boolean;
  /** owner of a personal space */
  personal?: number;
}

/** A file of the media library (GET /api/v1/media/{id}). */
/** How the browser starts an office program (see POST /api/v1/media/{id}/open). */
export interface OfficeAction {
  kind: string;
  url: string;
  form?: Record<string, string>;
  script?: string;
  config?: unknown;
  origin: string;
}

/** One way to open a medium (internal viewer or an office program). */
export interface OpenOption {
  viewer: string;
  label: string;
  modes: ('edit' | 'view')[];
  restricted: boolean;
  default: boolean;
  external: boolean;
}

export interface OpenResult {
  viewer: string;
  mode: 'edit' | 'view';
  restricted: boolean;
  action?: OfficeAction;
}

export interface MediaViewers {
  viewers: { id: string; label: string; restrict: boolean }[];
  default: string;
  open_external: boolean;
}

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
  /** only when the viewer may open the person (menu on the name) */
  updated_by_id?: number;
  is_default: boolean;
  short_url: string;
  short_only?: boolean;
  preview_image: string;
  /** not in listings, sources, feeds and the search (reachable by its link) */
  hidden?: boolean;
  /** only the element's own rules apply, not those of its category */
  own_rights_only?: boolean;
  /** classification set on the element (the strictest of element and categories applies) */
  classification?: number;
  /** the element has own permission rules */
  own_rules?: boolean;
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
  /** only when the viewer may open the person */
  author_id?: number;
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

/** A group; source, members and roles only for people with core.groups.manage. */
export interface Group { id: number; name: string; description: string; source?: string; members?: number; roles?: number[] }
/** A role; members and permissions only for people with core.roles.manage. */
export interface Role { id: number; name: string; description: string; is_admin: boolean; members?: number; permissions?: string[] }

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

/** Bits of a rule on one element (page): view, edit, delete, give rights. */
export const ELEMENT_PERMS = [
  { bit: 1, key: 'view' },
  { bit: 2, key: 'edit' },
  { bit: 8, key: 'delete' },
  { bit: 32, key: 'grant' },
] as const;

/** A parameter of a macro (for editors). */
export interface MacroParam {
  name: string;
  labels: Record<string, string>;
  type: 'text' | 'textarea' | 'number' | 'select' | 'bool' | 'url' | 'date' | 'time' | `custom:${string}`;
  options?: string[];
  option_labels?: Record<string, Record<string, string>>;
  default?: string;
  required?: boolean;
}

/** A macro of an active plugin (GET /api/v1/macros). */
export interface MacroInfo {
  name: string;
  plugin: string;
  labels: Record<string, string>;
  description: Record<string, string>;
  params: MacroParam[] | null;
  has_body: boolean;
  /** used inside a line of text (:name[text]{...}) */
  inline?: boolean;
  /** group in the menu of the editor (content.MacroGroups) */
  group?: string;
  /** icon of an own toolbar button */
  toolbar?: string;
  /** the body is split into parts at horizontal rules */
  parts?: MacroParts;
  /** the visual editor shows the result (cheap, no side effects) */
  picture?: boolean;
}

/** The parts of a macro (tabs, accordion, columns …). */
export interface MacroParts {
  /** the name of one part by language */
  labels: Record<string, string>;
  /** the first heading of a part is its title */
  titled: boolean;
}

