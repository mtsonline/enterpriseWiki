// Placement of the building blocks of the interface (search, notifications,
// category tree, blocks of plugins …) at the positions of the template, for
// one level: the site, a category (and its subcategories), the page of one
// category (category_page: only the positions of that page), one page
// (instance: only the positions of the page) or the own person. Operated
// with buttons and selects only (no dragging, WCAG 2.5.7).
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { activeTemplate, CATEGORY_POSITIONS, DASHBOARD_POSITIONS, EDITOR_POSITIONS, ELEMENT_POSITIONS, invalidatePlacements, placeableBlocks, positionFits, positionLabel, refreshPositions, resolvePlacements, tileOffered, type PlaceableBlock, type Placement, type PlacementLevel, type PositionKind } from '../coreplugin';
import { state } from '../core/state';
import { kindOf as positions0Kind } from '../core/placement';
import { button, checkbox, errorText, field, select } from './ui';
import { optionsDialog } from './blockoptions';
import type { BlockOptions } from '../core/blockoptions';

interface Row { block: string; position: string; required: boolean; options?: BlockOptions }

/** placementEditor edits the placements of one level. Rows of the level
 * replace the placements from above for the same block; blocks without a
 * row keep what the levels above say ("inherited"). */
export async function placementEditor(level: 'site' | 'category' | 'category_page' | 'instance' | 'user', target: string, opts: { onSaved?: () => void; heading?: boolean; category?: number; kinds?: PositionKind[] } = {}): Promise<HTMLElement> {
  // the own level without personal placement: only the own dashboard
  const kinds: PositionKind[] | undefined = opts.kinds ?? (level === 'user' && !state.site.personal_placement ? ['dashboard'] : level === 'instance' ? ['element'] : level === 'category_page' ? ['category'] : undefined);
  const kindOK = (k: PositionKind) => !kinds || kinds.includes(k);
  const box = h('section', { class: 'ew-placement', 'aria-labelledby': uid('pl') });
  const headingId = box.getAttribute('aria-labelledby')!;
  let current: PlacementLevel;
  let resolvedAbove = new Map<string, { position: string; sort: number; options?: BlockOptions }[]>();
  let blocks: PlaceableBlock[] = [];
  try {
    current = await api.get<PlacementLevel>(`placements/${level}${target ? `?target=${encodeURIComponent(target)}` : ''}`);
    // what the levels above say (to show the inherited state)
    const q = new URLSearchParams({ template: activeTemplate()?.id ?? '' });
    if (level === 'category' || level === 'category_page') q.set('category', target);
    if (level === 'instance') {
      if (opts.category) q.set('category', String(opts.category));
      q.set('element', target);
    }
    const above = await api.get<PlacementLevel[]>(`placements?${q}`);
    // one page arranges only the blocks of a page; the own dashboard only offered tiles
    blocks = (await placeableBlocks()).filter((b) => kindOK(b.kind) && (b.kind !== 'dashboard' || level !== 'user' || tileOffered(b.id)));
    resolvedAbove = resolvePlacements(above.filter((l) => !(l.level === level && l.target === target)), blocks, { personal: !!state.site.personal_placement });
  } catch (e) {
    box.append(h('p', { role: 'alert' }, errorText(e)));
    return box;
  }
  const nameOf = (id: string) => blocks.find((b) => b.id === id)?.label ?? id;
  // the positions of the template, of a page and of the editing page (one
  // page: only its own)
  const positions = [...(activeTemplate()?.positions ?? []), ...ELEMENT_POSITIONS, ...CATEGORY_POSITIONS, ...EDITOR_POSITIONS, ...DASHBOARD_POSITIONS]
    .filter((p) => kindOK(positions0Kind(p)));
  const fitting = (b: PlaceableBlock) => positions.filter((p) => positionFits(b.kind, p));
  const posOptionsOf = (b: PlaceableBlock) => [...fitting(b).map((p) => ({ value: p, label: `${positionLabel(p)} (${p})` })),
    ...(b.always ? [] : [{ value: '', label: t('placement.hidden') }])];
  // own rows per block; blocks without rows are inherited
  const own = new Map<string, Row[]>();
  for (const p of current.placements) own.set(p.block, [...(own.get(p.block) ?? []), { block: p.block, position: p.position, required: !!p.required, options: p.options && Object.keys(p.options).length ? p.options : undefined }]);
  // order within a position: by sort of the level, else of above
  const order = new Map<string, number>();
  current.placements.forEach((p) => order.set(`${p.block}@${p.position}`, p.sort));
  const status = h('p', { class: 'sr-only', 'aria-live': 'polite' });
  const list = h('div');
  const effective = (b: string): { position: string; sort: number; options?: BlockOptions }[] => own.has(b)
    ? own.get(b)!.filter((r) => r.position).map((r) => ({ position: r.position, sort: order.get(`${b}@${r.position}`) ?? 50, options: r.options }))
    : (resolvedAbove.get(b) ?? []);
  const draw = (focusKey?: string) => {
    const groups = positions.map((pos) => {
      const here = blocks.filter((b) => effective(b.id).some((x) => x.position === pos))
        .sort((a, b) => (effective(a.id).find((x) => x.position === pos)!.sort) - (effective(b.id).find((x) => x.position === pos)!.sort));
      const items = here.map((b, i) => {
        const inherited = !own.has(b.id);
        const key = `${b.id}@${pos}`;
        const move = (delta: number) => {
          const other = here[i + delta];
          if (!other) return;
          takeOver(b.id);
          takeOver(other.id);
          // renumber the position in steps of 10
          const ids = here.map((x) => x.id);
          [ids[i], ids[i + delta]] = [ids[i + delta], ids[i]];
          ids.forEach((id, n) => order.set(`${id}@${pos}`, (n + 1) * 10));
          status.textContent = t('placement.moved', { block: b.label, n: String(i + delta + 1) });
          draw(`${b.id}@${pos}:${delta < 0 ? 'up' : 'down'}`);
        };
        const where = select(posOptionsOf(b), pos);
        where.setAttribute('aria-label', t('placement.position_of', { block: b.label }));
        where.addEventListener('change', () => {
          takeOver(b.id);
          const rows = own.get(b.id)!;
          const r = rows.find((x) => x.position === pos)!;
          if (where.value === '') {
            // hidden: only an empty row when nothing else remains
            rows.splice(rows.indexOf(r), 1);
            if (!rows.some((x) => x.position)) own.set(b.id, [{ block: b.id, position: '', required: r.required }]);
          } else {
            r.position = where.value;
            order.set(`${b.id}@${where.value}`, 9999);
          }
          status.textContent = where.value ? t('placement.moved_to', { block: b.label, position: positionLabel(where.value) }) : t('placement.hidden_now', { block: b.label });
          draw();
        });
        const up = button('↑', () => move(-1), { variant: 'subtle', ariaLabel: t('placement.up', { block: b.label }) });
        const down = button('↓', () => move(1), { variant: 'subtle', ariaLabel: t('placement.down', { block: b.label }) });
        up.dataset.key = `${key}:up`;
        down.dataset.key = `${key}:down`;
        up.toggleAttribute('disabled', i === 0);
        down.toggleAttribute('disabled', i === here.length - 1);
        // options of this placement (look, when and for whom it is shown)
        const optsOf = () => (own.has(b.id) ? own.get(b.id)!.find((x) => x.position === pos)?.options : effective(b.id).find((x) => x.position === pos)?.options);
        const has = !!optsOf() && Object.keys(optsOf()!).length > 0;
        const optBtn = button(has ? t('bo.edit_set') : t('bo.edit'), () => optionsDialog(b.label, optsOf() ?? {}, (o) => {
          takeOver(b.id);
          const r = own.get(b.id)!.find((x) => x.position === pos);
          if (r) r.options = Object.keys(o).length ? o : undefined;
          status.textContent = Object.keys(o).length ? t('bo.changed', { block: b.label }) : t('bo.cleared', { block: b.label });
          draw(`${key}:opts`);
        }), { variant: 'subtle', ariaLabel: `${has ? t('bo.edit_set') : t('bo.edit')}: ${b.label}` });
        optBtn.dataset.key = `${key}:opts`;
        const req = level === 'site' ? checkbox(t('placement.required'), own.get(b.id)?.some((x) => x.required) ?? false, { help: t('placement.required_help') }) : null;
        req?.input.addEventListener('change', () => { takeOver(b.id); own.get(b.id)!.forEach((x) => { x.required = req.input.checked; }); });
        return h('li', { class: 'ew-placement-item' },
          h('span', { class: 'ew-placement-name' }, b.label, inherited ? h('span', { class: 'ew-muted' }, ` – ${t('placement.inherited')}`) : null),
          h('span', { class: 'ew-actions' }, up, down, where, optBtn), req?.el ?? null);
      });
      return h('li', null, h('h3', null, `${positionLabel(pos)} (${pos})`),
        items.length ? h('ol', { class: 'ew-placement-list' }, ...items) : h('p', { class: 'ew-muted' }, t('placement.empty')));
    });
    // blocks not shown anywhere (hidden or at a position this template lacks)
    const elsewhere = blocks.filter((b) => !effective(b.id).some((x) => positions.includes(x.position)));
    const addSel = select(elsewhere.map((b) => ({ value: b.id, label: b.label })));
    const addPos = select([]);
    // the positions that fit the block chosen
    const addPositions = () => {
      const b = blocks.find((x) => x.id === addSel.value);
      addPos.replaceChildren(...(b ? fitting(b) : []).map((p) => h('option', { value: p }, `${positionLabel(p)} (${p})`)));
    };
    addSel.addEventListener('change', addPositions);
    addPositions();
    const add = elsewhere.length ? h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('placement.add')),
      field(t('placement.block'), addSel), field(t('placement.position'), addPos),
      button(t('placement.add_button'), () => {
        own.set(addSel.value, [{ block: addSel.value, position: addPos.value, required: false }]);
        order.set(`${addSel.value}@${addPos.value}`, 9999);
        status.textContent = t('placement.moved_to', { block: nameOf(addSel.value), position: positionLabel(addPos.value) });
        draw();
      }, { variant: 'secondary' })) : null;
    // reset: only the blocks shown here (the own level also holds the rest of the interface)
    const shownOwn = blocks.filter((b) => own.has(b.id));
    const reset = shownOwn.length ? button(t('placement.reset'), () => { for (const b of shownOwn) own.delete(b.id); status.textContent = t('placement.reset_done'); draw(); }, { variant: 'link' }) : null;
    list.replaceChildren(h('ul', { class: 'ew-plain-list ew-placement-positions' }, ...groups), add ?? '', reset ?? '');
    if (focusKey) list.querySelector<HTMLElement>(`[data-key="${CSS.escape(focusKey)}"]:not([disabled])`)?.focus();
  };
  // a block taken over from above gets rows of its own (same positions)
  const takeOver = (b: string) => {
    if (own.has(b)) return;
    const eff = resolvedAbove.get(b) ?? [];
    own.set(b, eff.length ? eff.map((x) => { order.set(`${b}@${x.position}`, x.sort); return { block: b, position: x.position, required: false, options: x.options }; }) : [{ block: b, position: '', required: false }]);
  };
  draw();
  const save = button(t('common.save'), async () => {
    const out: Placement[] = [];
    for (const [b, rows] of own) for (const r of rows) out.push({ block: b, position: r.position, sort: order.get(`${b}@${r.position}`) ?? 50, required: r.required || undefined, options: r.position && r.options ? r.options : undefined });
    try {
      await api.put(`placements/${level}${target ? `?target=${encodeURIComponent(target)}` : ''}`, { placements: out });
      invalidatePlacements();
      await refreshPositions();
      toast(t('placement.saved'));
      opts.onSaved?.();
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'primary' });
  if (opts.heading === false) box.removeAttribute('aria-labelledby');
  else box.append(h('h2', { id: headingId }, t(`placement.title_${level}`)));
  box.append(h('p', { class: 'ew-help' }, t(`placement.help_${level}`)), status, list, h('div', { class: 'ew-actions' }, save));
  return box;
}
