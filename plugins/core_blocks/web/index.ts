// Blocks as elements: the editor (that of pages), the blocks at positions
// and, on the page of a block, where it is shown.
import {
  activeTemplate, api, currentPlacementLevels, button, checkbox, errorText, field, formDialog, getEditor, h, hasPerm, pick, invalidatePlacements, positionLabel, positionOptions,
  refreshPositions, registerEditor, registerElementExtension, registerSlot, select, state, t, toast, trustedHTML, uid,
} from '@ewiki/coreplugin';
import './style.css';

const TYPE = 'core.block';
const BASE = 'x/core.blocks';
const PREFIX = 'core.blocks/';

interface Listed { id: number; title: string; category_id: number }
interface Shown { id: number; type: string; title: string; html: string; shown_lang: string }
interface Placement { level: string; target: string; position: string; only_category_page: boolean; no_subcategories: boolean }

// the blocks the person may see (for the placement), cached 30 s
let listCache: { key: string; at: number; list: Promise<Listed[]> } | null = null;
function listed(): Promise<Listed[]> {
  const key = `${state.user?.id ?? 0}:${state.lang}`;
  if (!listCache || listCache.key !== key || Date.now() - listCache.at > 30000) {
    const p = api.get<Listed[]>(`${BASE}/blocks?lang=${encodeURIComponent(state.lang)}`);
    listCache = { key, at: Date.now(), list: p };
    p.catch(() => { listCache = null; });
  }
  return listCache.list;
}

// the content of the placed blocks: one request for all blocks of a page
let pending: { ids: Set<number>; done: Promise<Map<number, Shown>> } | null = null;
const shownCache = new Map<string, Promise<Shown | undefined>>();
function shown(id: number): Promise<Shown | undefined> {
  const key = `${state.user?.id ?? 0}:${state.lang}:${id}`;
  const have = shownCache.get(key);
  if (have) return have;
  if (!pending) {
    const ids = new Set<number>();
    const done = new Promise<Map<number, Shown>>((resolve) => {
      queueMicrotask(async () => {
        pending = null;
        const m = new Map<number, Shown>();
        try {
          // the elements the person may view (others are left out by the server)
          const r = await api.get<{ items: Shown[] }>(`elements/batch?ids=${[...ids].join(',')}&lang=${encodeURIComponent(state.lang)}`);
          // only elements of the type block: a placement never shows another page
          for (const x of r.items ?? []) if (x.type === TYPE) m.set(x.id, x);
        } catch { /* nothing to show */ }
        resolve(m);
      });
    });
    pending = { ids, done };
  }
  pending.ids.add(id);
  const p = pending.done.then((m) => m.get(id));
  shownCache.set(key, p);
  setTimeout(() => shownCache.delete(key), 30000);
  return p;
}

/** A block at a position: its content, named by its title for screen readers. */
async function slot(position: string, key?: string): Promise<Node | null> {
  const b = key && /^[1-9][0-9]{0,18}$/.test(key) ? await shown(Number(key)) : undefined;
  if (!b) return null;
  const id = uid(`ewb-${b.id}`);
  return h('div', { class: `ewb ewb--${position}` },
    h('section', { class: 'ewb-block', 'aria-labelledby': id, lang: b.shown_lang },
      h('h2', { id, class: 'sr-only' }, b.title), trustedHTML(b.html, 'ew-content ewb-content')));
}

// ---------------------------------------------------------------- where it is shown

const names = new Map<string, Promise<string>>();
/** The title of a category or page (its number if it cannot be read). */
function nameOf(kind: 'categories' | 'elements', id: string): Promise<string> {
  const key = `${kind}/${id}`;
  let p = names.get(key);
  if (!p) {
    p = api.get<{ titles?: Record<string, string>; title?: string }>(`${kind}/${id}?lang=${encodeURIComponent(state.lang)}`)
      .then((x) => x.title || pick(x.titles ?? {}, state.lang, state.site.default_language) || `#${id}`).catch(() => `#${id}`);
    names.set(key, p);
  }
  return p;
}

async function describe(p: Placement): Promise<string> {
  const where = p.level === 'site' || p.level === 'template' ? t('core.blocks.scope_site')
    : p.level === 'type' ? t('core.blocks.scope_type', { type: p.target })
      : p.level === 'instance' ? t('core.blocks.scope_page', { name: await nameOf('elements', p.target) })
        : p.level === 'category_page' ? t('core.blocks.scope_category_page', { name: await nameOf('categories', p.target) })
          : t(p.no_subcategories ? 'core.blocks.scope_category_only' : 'core.blocks.scope_category', { name: await nameOf('categories', p.target) });
  const extra = p.level === 'category' && p.only_category_page ? ` ${t('core.blocks.scope_only_page')}` : '';
  return `${p.position ? positionLabel(p.position) : t('core.blocks.hidden_here')} – ${where}${extra}`;
}

/** Places a block: position and where (whole site, this category). */
function placeDialog(blockId: number, categoryId: number, done: () => void): void {
  const pos = select(positionOptions(undefined, ['header-tools', 'cms-nav']), activeTemplate()?.positions.includes('footer') ? 'footer' : (activeTemplate()?.positions[0] ?? ''));
  const scopes = [{ value: 'category', label: t('core.blocks.place_category') }];
  if (hasPerm('core.settings.manage')) scopes.push({ value: 'site', label: t('core.blocks.place_site') });
  const scope = select(scopes, 'category');
  const pages = checkbox(t('placement.scope_pages'), true);
  const subs = checkbox(t('placement.scope_sub'), true);
  const note = h('p', { class: 'ew-help' }, t('core.blocks.not_listed'));
  const sync = () => { pages.el.hidden = subs.el.hidden = scope.value !== 'category'; };
  scope.addEventListener('change', sync);
  sync();
  formDialog(t('core.blocks.place'), [field(t('core.blocks.position'), pos), field(t('core.blocks.where'), scope), pages.el, subs.el, note], t('core.blocks.place'), async () => {
    const level = scope.value === 'site' ? 'site' : 'category';
    const target = level === 'site' ? '' : String(categoryId);
    const q = target ? `?target=${encodeURIComponent(target)}` : '';
    const cur = await api.get<{ placements: Record<string, unknown>[] }>(`placements/${level}${q}`);
    const list = (cur.placements ?? []).map((x) => ({ ...x }));
    list.push({ block: `${PREFIX}${blockId}`, position: pos.value, sort: 50, ...(level === 'category' ? { only_category_page: !pages.input.checked, no_subcategories: !subs.input.checked } : {}) });
    await api.put(`placements/${level}${q}`, { placements: list });
    invalidatePlacements();
    await refreshPositions();
    toast(t('core.blocks.placed', { position: positionLabel(pos.value) }));
    done();
  });
}

/** On the page of a block (for those who may edit it): where it is shown. */
async function whereShown(el: { id: number; type: string; category_id: number; permissions?: string[] }): Promise<Node | null> {
  if (el.type !== TYPE || !el.permissions?.includes('edit')) return null;
  const box = h('section', { class: 'ewb-where', 'aria-labelledby': `ewb-where-${el.id}` });
  const draw = async () => {
    let list: Placement[] = [];
    try { list = await api.get<Placement[]>(`${BASE}/blocks/${el.id}/placements`); } catch (e) { toast(errorText(e), 'error'); }
    box.replaceChildren(
      h('h2', { id: `ewb-where-${el.id}` }, t('core.blocks.where_shown')),
      list.length ? h('ul', null, ...(await Promise.all(list.map(describe))).map((x) => h('li', null, x))) : h('p', null, t('core.blocks.nowhere')),
      h('p', { class: 'ew-help' }, t('core.blocks.not_listed')),
      button(t('core.blocks.place'), () => placeDialog(el.id, el.category_id, () => void draw()), { variant: 'secondary' }));
  };
  await draw();
  return box;
}

export function register(): void {
  // a block is written like a page
  registerEditor(TYPE, (container, body, ctx) => {
    const base = getEditor('core.html');
    if (!base) throw new Error('core.html editor missing');
    return base(container, body, ctx);
  });
  registerSlot({
    id: 'core.blocks', position: '*', order: 30, label: () => t('core.blocks.block'), render: slot,
    // every block element is a block of its own; it has no default position
    // (those placed for the page shown always, also beyond the limit of the list)
    instances: async () => {
      const [list, levels] = await Promise.all([state.user ? listed().catch(() => []) : Promise.resolve([] as Listed[]), currentPlacementLevels()]);
      const out = new Map(list.map((b) => [String(b.id), `${t('core.blocks.block')}: ${b.title}`]));
      for (const l of levels) {
        for (const p of l.placements) {
          const k = p.block.startsWith(`${PREFIX}`) ? p.block.slice(PREFIX.length) : '';
          if (/^[1-9][0-9]{0,18}$/.test(k) && !out.has(k)) out.set(k, `${t('core.blocks.block')} #${k}`);
        }
      }
      return [...out].map(([key, label]) => ({ key, label, position: '', order: 30 }));
    },
  });
  registerElementExtension({ id: 'core.blocks.where', placement: 'top', label: () => t('core.blocks.where_shown'), render: (el) => whereShown(el as never) });
}
