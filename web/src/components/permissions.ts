// Permission editors: category permissions (core bits, presets and named
// actions of plugins) and global permissions of roles. The permissions are
// declared by the core and the plugins (GET /api/v1/permissions).
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { PERMS } from '../core/types';
import { checkbox, field, select } from './ui';

export interface PermDef { key: string; scope: 'global' | 'category'; label: string; description?: string; presets?: string[]; group: string; implies?: string[] }
interface Catalog { permissions: PermDef[]; presets: Record<string, { permissions: number; actions: string[] }> }

let catalog: Promise<Catalog> | null = null;

/** loadCatalog returns the declared permissions (cached). */
export function loadCatalog(): Promise<Catalog> {
  catalog ??= api.get<Catalog>('permissions').catch((e) => { catalog = null; throw e; });
  return catalog;
}

/** permLabel is the translated label of a declared permission (English label as fallback). */
export function permLabel(d: PermDef): string {
  const k = `perm.${d.key}`;
  const v = t(k);
  return v === k ? d.label : v;
}

function permHelp(d: PermDef): string | undefined {
  const k = `perm.${d.key}_help`;
  const v = t(k);
  return v === k ? d.description : v;
}

/** groupLabel names the plugin (or the core) a permission belongs to. */
export function groupLabel(group: string): string {
  const k = `perm.group.${group}`;
  const v = t(k);
  return v === k ? group : v;
}

/** summary describes core bits and actions in words. */
export function summary(bits: number, actions: string[], defs: PermDef[]): string {
  const parts = PERMS.filter((p) => bits & p.bit).map((p) => t(`perm.${p.key}`));
  for (const a of actions) {
    const d = defs.find((x) => x.key === a);
    parts.push(d ? permLabel(d) : a);
  }
  return parts.join(', ') || '–';
}

function byGroup(defs: PermDef[]): Map<string, PermDef[]> {
  const m = new Map<string, PermDef[]>();
  for (const d of defs) m.set(d.group, [...(m.get(d.group) ?? []), d]);
  return m;
}

export interface RuleEditor { el: HTMLElement; value: () => { permissions: number; actions: string[] } }

/** ruleEditor edits the permissions of a category rule: a preset or single permissions. */
export async function ruleEditor(bits: number, actions: string[], opts: { guest?: boolean } = {}): Promise<RuleEditor> {
  const cat = await loadCatalog();
  const defs = cat.permissions.filter((d) => d.scope === 'category');
  const core = PERMS.map((p) => ({ p, cb: checkbox(t(`perm.${p.key}`), (bits & p.bit) !== 0, { help: t(`perm.${p.key}_help`) }) }));
  const acts = defs.map((d) => ({ d, cb: checkbox(permLabel(d), actions.includes(d.key), { help: permHelp(d) }) }));
  const presetOf = (): string => {
    const curBits = core.filter((c) => c.cb.input.checked).reduce((a, c) => a | c.p.bit, 0);
    const curActs = acts.filter((a) => a.cb.input.checked).map((a) => a.d.key).sort().join(',');
    for (const [name, p] of Object.entries(cat.presets)) {
      if (p.permissions === curBits && [...p.actions].sort().join(',') === curActs) return name;
    }
    return 'custom';
  };
  const preset = select(['reader', 'editor', 'manager', 'custom'].map((x) => ({ value: x, label: t(`acl.preset.${x}`) })), 'custom');
  preset.value = presetOf();
  preset.addEventListener('change', () => {
    const p = cat.presets[preset.value];
    if (!p) return;
    for (const c of core) c.cb.input.checked = (p.permissions & c.p.bit) !== 0;
    for (const a of acts) a.cb.input.checked = p.actions.includes(a.d.key);
  });
  for (const x of [...core.map((c) => c.cb.input), ...acts.map((a) => a.cb.input)]) x.addEventListener('change', () => { preset.value = presetOf(); });
  if (opts.guest) { // guests may only view
    for (const c of core) if (c.p.key !== 'view') c.cb.el.hidden = true;
    for (const a of acts) a.cb.el.hidden = true;
  }
  const groups = [...byGroup(defs).entries()];
  const el = h('div', { class: 'ew-perm-editor' },
    opts.guest ? null : field(t('acl.preset'), preset, { help: t('acl.preset_help') }),
    h('fieldset', null, h('legend', null, t('acl.core_permissions')), ...core.map((c) => c.cb.el)),
    ...(opts.guest ? [] : groups.map(([g, list]) => h('fieldset', null, h('legend', null, groupLabel(g)),
      ...acts.filter((a) => list.includes(a.d)).map((a) => a.cb.el)))));
  return {
    el,
    value: () => ({
      permissions: core.filter((c) => c.cb.input.checked).reduce((a, c) => a | c.p.bit, 0),
      actions: acts.filter((a) => a.cb.input.checked).map((a) => a.d.key),
    }),
  };
}

export interface GlobalEditor { el: HTMLElement; value: () => string[] }

/** globalEditor selects global permissions, grouped by plugin. Permissions the
 * caller does not have are shown but cannot be changed (mine = null: all). */
export async function globalEditor(selected: string[], mine: string[] | null): Promise<GlobalEditor> {
  const cat = await loadCatalog();
  const defs = cat.permissions.filter((d) => d.scope === 'global');
  const boxes = defs.map((d) => {
    const cb = checkbox(permLabel(d), selected.includes(d.key), { help: permHelp(d) });
    if (mine && !mine.includes(d.key)) cb.input.disabled = true;
    return { d, cb };
  });
  const id = uid('gperm');
  const el = h('div', { class: 'ew-perm-editor', id },
    ...[...byGroup(defs).entries()].map(([g, list]) => h('fieldset', null, h('legend', null, groupLabel(g)),
      ...boxes.filter((b) => list.includes(b.d)).map((b) => b.cb.el))));
  return { el, value: () => boxes.filter((b) => b.cb.input.checked).map((b) => b.d.key) };
}
