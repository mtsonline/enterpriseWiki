// Emoji reactions below pages and categories and on comments (via the SDK
// hook reactionBar). Toggle buttons per emoji (aria-pressed) with the names
// of the people, a picker with common emojis (arrow keys move, Escape
// closes) and a field for any other emoji.
import { api, button, errorText, field, h, openDialog, registerAdminPage, registerCategoryExtension, registerElementExtension, registerReactions, state, t, textInput, toast, uid, pick } from '@ewiki/sdk';
import './style.css';

const BASE = 'x/core.reactions';

interface Reaction { emoji: string; count: number; mine: boolean; users: string[]; user_ids?: number[] }
interface Summary { target_type: string; target_id: number; reactions: Reaction[]; can_react: boolean }

const GROUPS: Record<string, string[]> = {
  smileys: ['😀', '😃', '😄', '😁', '😆', '😅', '😂', '🤣', '😊', '😇', '🙂', '😉', '😍', '🥰', '😘', '😋', '😎', '🤩', '🥳', '😏', '🤔', '🤨', '😐', '😶', '🙄', '😬', '😮', '😯', '😲', '😳', '🥺', '😢', '😭', '😤', '😡', '🤯', '😱', '😴', '🤒', '🤗', '🤫', '🤭', '🫡', '🫠'],
  gestures: ['👍', '👎', '👏', '🙌', '🙏', '👌', '🤝', '✌️', '🤞', '🤟', '👋', '💪', '👀', '🫶', '☝️', '👉', '✍️', '🤷', '🙋', '🙇'],
  hearts: ['❤️', '🧡', '💛', '💚', '💙', '💜', '🖤', '🤍', '💯', '✅', '❌', '⭐', '🌟', '✨', '🔥', '⚡', '💡', '❗', '❓', '⚠️', '🆗', '🆕', '➕', '🔝'],
  objects: ['🎉', '🎊', '🎁', '🏆', '🥇', '🎯', '🚀', '📌', '📎', '📝', '📅', '📣', '🔔', '🛠️', '💻', '☕', '🍕', '🍰', '🌈', '☀️', '🌱', '🌍', '🐞', '🦄'],
};

const cache = new Map<string, Summary>();
let blocked: string[] | null = null;
const key = (type: string, id: number) => `${type}:${id}`;
const base = (e: string) => e.replace(/[︎️]|\uD83C[\uDFFB-\uDFFF]/g, '');

async function loadBlocked(): Promise<string[]> {
  if (blocked) return blocked;
  try { blocked = (await api.get<{ blocked: string[] }>(`${BASE}/settings`)).blocked; } catch { blocked = []; }
  return blocked;
}

/** True if s looks like one emoji (the server checks again). */
function looksLikeEmoji(s: string): boolean {
  const seg = typeof Intl.Segmenter === 'function' ? [...new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(s)] : [{ segment: s }];
  return seg.length === 1 && /\p{Extended_Pictographic}|\p{Regional_Indicator}|⃣/u.test(s);
}

function names(r: Reaction): string {
  const list = r.users.map((u) => (u === state.user?.display_name || u === state.user?.username ? t('core.reactions.you') : u));
  if (r.count > r.users.length) list.push(t('core.reactions.and_more', { n: r.count - r.users.length }));
  return list.join(', ');
}

function bar(type: string, id: number, title: string): HTMLElement {
  const root = h('div', { class: 'ewre', role: 'group', 'aria-label': `${t('core.reactions.label')}: ${title}` });
  let picker: HTMLElement | null = null;
  const send = async (emoji: string, on: boolean) => {
    try {
      const s = await api.post<Summary>(`${BASE}/${type}/${id}`, { emoji, on });
      cache.set(key(type, id), s);
      draw(emoji);
      toast(t(on ? 'core.reactions.added' : 'core.reactions.removed', { emoji }));
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const closePicker = (focus = true) => {
    const pk = picker;
    picker = null; // first: removing it moves the focus (focusout calls closePicker again)
    pk?.remove();
    addBtn?.setAttribute('aria-expanded', 'false');
    if (focus) addBtn?.focus();
  };
  let addBtn: HTMLButtonElement | null = null;
  const openPicker = async () => {
    if (picker) { closePicker(); return; }
    const bl = await loadBlocked();
    const pid = uid('ewre-pick');
    const grid: HTMLButtonElement[] = [];
    const choose = (e: string) => { closePicker(); void send(e, true); };
    const groups = Object.entries(GROUPS).map(([g, list]) => h('div', { class: 'ewre-group' },
      h('h3', { class: 'ewre-group-title' }, t(`core.reactions.group.${g}`)),
      h('div', { class: 'ewre-grid' }, ...list.filter((e) => !bl.includes(base(e))).map((e) => {
        const b = h('button', { type: 'button', class: 'ewre-emoji', tabindex: '-1', onclick: () => choose(e) }, e) as HTMLButtonElement;
        grid.push(b);
        return b;
      }))));
    if (grid[0]) grid[0].tabIndex = 0;
    const other = textInput({ maxlength: 32 });
    other.setAttribute('autocomplete', 'off');
    const err = h('p', { class: 'ew-field-error', role: 'alert' });
    const otherForm = h('form', { class: 'ewre-other', novalidate: true },
      field(t('core.reactions.other'), other, { help: t('core.reactions.other_help') }), err,
      button(t('core.reactions.use'), () => undefined, { type: 'submit', variant: 'secondary' }));
    otherForm.addEventListener('submit', (ev) => {
      ev.preventDefault();
      const v = other.value.trim();
      if (!looksLikeEmoji(v)) { err.textContent = t('core.reactions.invalid'); other.focus(); return; }
      if (bl.includes(base(v))) { err.textContent = t('core.reactions.blocked'); other.focus(); return; }
      choose(v);
    });
    picker = h('div', { class: 'ewre-picker', id: pid, role: 'dialog', 'aria-label': t('core.reactions.picker') }, ...groups, otherForm);
    picker.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape') { ev.preventDefault(); ev.stopPropagation(); closePicker(); return; }
      const i = grid.indexOf(document.activeElement as HTMLButtonElement);
      if (i < 0) return;
      const cols = 8;
      const next = ({ ArrowRight: i + 1, ArrowLeft: i - 1, ArrowDown: i + cols, ArrowUp: i - cols, Home: 0, End: grid.length - 1 } as Record<string, number>)[ev.key];
      if (next === undefined) return;
      ev.preventDefault();
      const n = Math.max(0, Math.min(grid.length - 1, next));
      grid[i].tabIndex = -1;
      grid[n].tabIndex = 0;
      grid[n].focus();
    });
    picker.addEventListener('focusout', (ev) => { if (picker && !root.contains(ev.relatedTarget as Node)) closePicker(false); });
    root.append(picker);
    addBtn?.setAttribute('aria-expanded', 'true');
    addBtn?.setAttribute('aria-controls', pid);
    grid[0]?.focus();
  };
  const draw = (focusEmoji?: string) => {
    const s = cache.get(key(type, id));
    root.replaceChildren();
    picker = null;
    if (!s) return;
    root.hidden = !s.reactions.length && !s.can_react;
    for (const r of s.reactions) {
      const label = t('core.reactions.toggle', { n: r.count, emoji: r.emoji, names: names(r) });
      if (s.can_react) {
        const b = h('button', { type: 'button', class: ['ewre-btn', r.mine && 'is-mine'], 'aria-pressed': String(r.mine), title: names(r), 'aria-label': label, onclick: () => void send(r.emoji, !r.mine) },
          h('span', { 'aria-hidden': 'true' }, r.emoji), ' ', h('span', { class: 'ewre-count', 'aria-hidden': 'true' }, String(r.count)));
        root.append(b);
        if (focusEmoji === r.emoji) b.focus();
      } else {
        root.append(h('span', { class: 'ewre-btn ewre-static', title: names(r), role: 'img', 'aria-label': label },
          h('span', { 'aria-hidden': 'true' }, r.emoji, ' ', String(r.count))));
      }
    }
    if (s.reactions.some((r) => r.user_ids)) {
      root.append(button(t('core.reactions.manage'), () => manageDialog(type, id, title, () => draw()), { variant: 'link', ariaLabel: `${t('core.reactions.manage')}: ${title}` }));
    }
    if (s.can_react) {
      addBtn = h('button', { type: 'button', class: 'ewre-add', 'aria-expanded': 'false', 'aria-haspopup': 'dialog', 'aria-label': t('core.reactions.add_named', { title }), title: t('core.reactions.add'), onclick: () => void openPicker() },
        h('span', { 'aria-hidden': 'true' }, '😀+')) as HTMLButtonElement;
      root.append(addBtn);
      if (focusEmoji && !s.reactions.some((r) => r.emoji === focusEmoji)) addBtn.focus();
    }
  };
  if (cache.has(key(type, id))) draw();
  else {
    root.hidden = true;
    void api.get<Summary>(`${BASE}/${type}/${id}`).then((s) => { cache.set(key(type, id), s); draw(); }).catch(() => undefined);
  }
  return root;
}

/** Lists everybody's reactions on a target; managers remove single ones. */
function manageDialog(type: string, id: number, title: string, changed: () => void): void {
  const body = h('div');
  const draw = () => {
    const s = cache.get(key(type, id));
    const rows = (s?.reactions ?? []).flatMap((r) => (r.user_ids ?? []).map((u, i) => h('tr', null,
      h('td', null, r.emoji), h('td', null, r.users[i] ?? `#${u}`),
      h('td', null, button(t('common.remove'), async () => {
        try {
          await api.del(`${BASE}/${type}/${id}/users/${u}?emoji=${encodeURIComponent(r.emoji)}`);
          cache.set(key(type, id), await api.get<Summary>(`${BASE}/${type}/${id}`));
          draw();
          changed();
          toast(t('core.reactions.removed', { emoji: r.emoji }));
        } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('common.remove')}: ${r.emoji} ${r.users[i] ?? ''}` })))));
    body.replaceChildren(rows.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.reactions.label')),
      h('thead', null, h('tr', null, ...[t('core.reactions.emoji'), t('core.reactions.person'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...rows)) : h('p', null, t('core.reactions.none')));
  };
  draw();
  openDialog(`${t('core.reactions.manage')}: ${title}`, body, []);
}

async function adminPage(panel: HTMLElement): Promise<void> {
  blocked = null;
  const list = await loadBlocked();
  const input = textInput({ value: list.join(' ') });
  const f = h('form', { class: 'ew-form', novalidate: true },
    field(t('core.reactions.blocklist'), input, { help: t('core.reactions.blocklist_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  f.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const items = input.value.split(/\s+/).map((x) => x.trim()).filter(Boolean);
    try {
      const r = await api.put<{ blocked: string[] }>(`${BASE}/settings`, { blocked: items });
      blocked = r.blocked;
      input.value = r.blocked.join(' ');
      cache.clear();
      toast(t('core.reactions.saved'));
    } catch (e) { toast(errorText(e), 'error'); }
  });
  panel.replaceChildren(h('div', { class: 'ew-page-header' }, h('h1', null, t('core.reactions.admin'))), h('p', null, t('core.reactions.admin_help')), f);
}

export function register(): void {
  registerReactions({
    prefetch: async (type, ids) => {
      const missing = ids.filter((id) => !cache.has(key(type, id)));
      for (let i = 0; i < missing.length; i += 200) {
        const list = await api.get<Summary[]>(`${BASE}/batch?type=${encodeURIComponent(type)}&ids=${missing.slice(i, i + 200).join(',')}`);
        for (const s of list) cache.set(key(type, s.target_id), s);
      }
    },
    render: (type, id, title) => bar(type, id, title),
  });
  registerElementExtension({ id: 'core.reactions', order: 10, render: (el) => h('div', { class: 'ewre-section' }, bar('element', el.id, el.title)) });
  registerCategoryExtension({ id: 'core.reactions', order: 10, render: (c) => h('div', { class: 'ewre-section' }, bar('category', c.id, pick(c.titles, state.lang, state.site.default_language))) });
  registerAdminPage({ id: 'reactions', label: () => t('core.reactions.admin'), permission: 'core.reactions.manage', render: adminPage });
}
