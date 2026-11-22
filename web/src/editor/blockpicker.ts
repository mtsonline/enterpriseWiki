// The menu "Insert block" of the editor: a search field and the blocks in
// groups (a tree: arrow right opens a group, arrow left goes back), with the
// block templates as their own group. Screen readers hear the name of a
// block; its description only when the choice stays a moment.
import { api, qs } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import type { MacroInfo } from '../core/types';
import { button, openDialog } from '../components/ui';
import { currentCategory } from '../components/widgets';
import { manageTemplates } from '../components/publishing';
import { allowedMacros, label, loadMacros, MACRO_DESC_DELAY } from './macros';
import { fold, groupMacros, matches } from './blockgroups';

export type BlockChoice = { macro: string } | { template: number } | { save: true } | { share: true };

interface Leaf { key: string; name: string; desc: string; choice: BlockChoice; search: string }

/**
 * Opens the menu. canSave offers "Save selection as block template".
 * Resolves with the choice, or null when the menu was closed.
 */
export async function chooseBlock(opts: { macros: boolean; canSave: boolean; templates: boolean; canShare?: boolean }): Promise<BlockChoice | null> {
  const [macros, templates] = await Promise.all([
    opts.macros ? loadMacros().then(allowedMacros) : Promise.resolve([] as MacroInfo[]),
    opts.templates
      ? api.get<{ id: number; name: string; description?: string }[]>(`templates${qs({ kind: 'block', category: currentCategory() || undefined })}`).catch(() => [])
      : Promise.resolve([]),
  ]);
  if (!macros.length && !templates.length && !opts.canSave && !opts.canShare) throw new Error(t(opts.templates && !opts.macros ? 'blocktpl.none' : 'editor.macro_none'));
  const groups: { id: string; title: string; leaves: Leaf[] }[] = groupMacros(macros, (m) => label(m.labels) || m.name).map(({ group, macros: ms }) => ({
    id: group,
    title: t(`editor.group.${group}`),
    leaves: ms.map((m) => {
      const name = label(m.labels) || m.name;
      const desc = label(m.description);
      return { key: `m:${m.name}`, name, desc, choice: { macro: m.name }, search: fold(`${name} ${m.name} ${desc}`) };
    }),
  }));
  if (templates.length) {
    groups.push({
      id: 'templates', title: t('editor.group.templates'),
      leaves: templates.map((x) => ({ key: `t:${x.id}`, name: x.name, desc: x.description ?? '', choice: { template: x.id }, search: fold(`${x.name} ${x.description ?? ''}`) })),
    });
  }

  const treeId = uid('ewb-tree');
  const search = h('input', { type: 'search', class: 'ew-input', 'aria-controls': treeId, autocomplete: 'off', id: uid('ewb-search') }) as HTMLInputElement;
  const searchLabel = h('label', { for: search.id }, t('editor.block_search'));
  const tree = h('ul', { role: 'tree', id: treeId, class: 'ewb-tree', 'aria-label': t('editor.blocks') });
  const desc = h('p', { class: 'ew-help ewb-desc' });
  const spoken = h('p', { class: 'sr-only ewe-macro-spoken', 'aria-live': 'polite', 'aria-atomic': 'true' });
  const none = h('p', { class: 'ew-muted', hidden: true }, t('editor.block_none_found'));
  const count = h('p', { class: 'sr-only', 'aria-live': 'polite' });

  type Item = { el: HTMLElement; leaf?: Leaf; group?: { open: boolean; sub: HTMLUListElement; wrap: HTMLLIElement } };
  const items: Item[] = [];
  for (const g of groups) {
    const sub = h('ul', { role: 'group' }) as HTMLUListElement;
    // the group's item is its title only; its blocks belong to it through
    // aria-owns (so its name is not the text of all its blocks)
    sub.id = uid('ewb-g');
    const gel = h('span', { role: 'treeitem', 'aria-expanded': 'false', 'aria-owns': sub.id, tabindex: '-1', class: 'ewb-group ewb-label', 'data-group': g.id },
      h('span', { class: 'ewb-arrow', 'aria-hidden': 'true' }, '▸'), ` ${g.title}`) as HTMLElement;
    const wrap = h('li', { role: 'none' }, gel, sub) as HTMLLIElement;
    const gitem: Item = { el: gel, group: { open: false, sub, wrap } };
    sub.hidden = true;
    items.push(gitem);
    for (const leaf of g.leaves) {
      const lel = h('li', { role: 'treeitem', tabindex: '-1', class: 'ewb-leaf', 'data-block': leaf.key.slice(2) }, leaf.name) as HTMLLIElement;
      sub.append(lel);
      items.push({ el: lel, leaf });
    }
    tree.append(wrap);
  }
  let result: BlockChoice | null = null;
  let active: Item | null = null;
  let speakTimer = 0;
  const visible = () => items.filter((it) => (it.group ? !it.group.wrap.hidden : !it.el.hidden && !(it.el.parentElement as HTMLElement).hidden));
  const setOpen = (it: Item, open: boolean) => {
    if (!it.group) return;
    it.group.open = open;
    it.group.sub.hidden = !open;
    it.el.setAttribute('aria-expanded', String(open));
    it.el.querySelector('.ewb-arrow')!.textContent = open ? '▾' : '▸';
  };
  const activate = (it: Item | undefined, focus = true) => {
    if (!it) return;
    for (const x of items) x.el.tabIndex = -1;
    it.el.tabIndex = 0;
    if (focus) it.el.focus();
    if (active === it) return;
    active = it;
    desc.textContent = it.leaf?.desc ?? '';
    spoken.textContent = '';
    window.clearTimeout(speakTimer);
    if (it.leaf?.desc) speakTimer = window.setTimeout(() => { spoken.textContent = it.leaf!.desc; }, MACRO_DESC_DELAY);
  };
  const choose = (it: Item) => {
    if (it.leaf) { result = it.leaf.choice; dlg.close(); return; }
    setOpen(it, !it.group!.open);
  };
  const filter = () => {
    const q = search.value.trim();
    let n = 0;
    for (const it of items) {
      if (it.group) continue;
      it.el.hidden = !!q && !matches(it.leaf!.search, q);
      if (!it.el.hidden) n++;
    }
    for (const it of items) {
      if (!it.group) continue;
      const any = Array.from(it.group.sub.children).some((c) => !(c as HTMLElement).hidden);
      it.group.wrap.hidden = !any;
      setOpen(it, !!q && any);
    }
    none.hidden = n > 0;
    count.textContent = q ? t('editor.block_found', { count: String(n) }) : '';
    const first = visible()[0];
    if (first) activate(first, false);
  };
  search.addEventListener('input', filter);
  search.addEventListener('keydown', (ev) => {
    if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      const v = visible();
      // with a search: straight to the first block found
      activate(search.value.trim() ? v.find((it) => it.leaf) ?? v[0] : v[0]);
    } else if (ev.key === 'Enter') {
      ev.preventDefault();
      const first = visible().find((it) => it.leaf);
      if (search.value.trim() && first) choose(first);
    }
  });
  tree.addEventListener('click', (ev) => {
    const li = (ev.target as HTMLElement).closest('[role=treeitem]');
    const it = items.find((x) => x.el === li);
    if (!it) return;
    activate(it);
    choose(it);
  });
  tree.addEventListener('focusin', (ev) => {
    const it = items.find((x) => x.el === ev.target);
    if (it) activate(it, false);
  });
  tree.addEventListener('keydown', (ev) => {
    if (!active) return;
    const v = visible();
    const i = v.indexOf(active);
    const parent = active.leaf ? items.find((x) => x.group && x.group.sub === active!.el.parentElement) : undefined;
    switch (ev.key) {
      case 'ArrowDown': activate(v[Math.min(i + 1, v.length - 1)]); break;
      case 'ArrowUp': if (i === 0) search.focus(); else activate(v[i - 1]); break;
      case 'Home': activate(v[0]); break;
      case 'End': activate(v[v.length - 1]); break;
      case 'ArrowRight':
        if (active.group && !active.group.open) setOpen(active, true);
        else if (active.group) activate(v[i + 1]);
        break;
      case 'ArrowLeft':
        if (active.group?.open) setOpen(active, false);
        else if (parent) activate(parent);
        break;
      case 'Enter': case ' ': choose(active); break;
      default:
        // typing goes to the search
        if (ev.key.length === 1 && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
          search.focus();
          return;
        }
        return;
    }
    ev.preventDefault();
  });
  const save = opts.canSave ? button(t('editor.snippet_save'), () => { result = { save: true }; dlg.close(); }, { variant: 'link' }) : null;
  // the selection as a block of its own, shown here and wherever it is included
  const share = opts.canShare ? button(t('editor.block_share'), () => { result = { share: true }; dlg.close(); }, { variant: 'link' }) : null;
  const manage = opts.templates ? button(t('blocktpl.manage'), () => { dlg.close(); void manageTemplates('core.html', currentCategory(), undefined, 'block'); }, { variant: 'link' }) : null;
  const dlg = openDialog(t('editor.macro_insert'), h('div', { class: 'ewb-picker' },
    h('div', { class: 'ew-field' }, searchLabel, search, h('p', { class: 'ew-help' }, t('editor.block_keys'))),
    count, tree, none, desc, spoken, save || share || manage ? h('p', { class: 'ewb-actions' }, save, share, manage) : null), [], { wide: true, onClose: () => window.clearTimeout(speakTimer) });
  activate(items[0], false);
  search.focus();
  return new Promise((resolve) => { dlg.dialog.addEventListener('close', () => resolve(result)); });
}
