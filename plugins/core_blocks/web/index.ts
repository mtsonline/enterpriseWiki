// Content blocks at template positions, and their administration page.
import {
  activeTemplate, api, button, checkbox, confirmDialog, errorText, field, formDialog, h, mount, pageHeader, pick, positionLabel, positionOptions,
  refreshPositions, registerAdminPage, registerSlot, select, state, t, templateLabel, textArea, textInput, toast, trustedHTML,
} from '@ewiki/sdk';
import './style.css';

const BASE = 'x/core.blocks';

interface Body { format: 'markdown' | 'html'; source: string }
interface Block { id: number; name: string; location: string; position: number; audience: 'all' | 'users' | 'guests'; enabled: boolean; titles: Record<string, string>; bodies: Record<string, Body> }
interface Rendered { id: number; location: string; position: number; title: string; lang: string; html: string }

// one request for all positions (shared while it is running), cached 30 s
let cache: { key: string; at: number; blocks: Promise<Rendered[]> } | null = null;

function blocks(): Promise<Rendered[]> {
  const key = `${state.user?.id ?? 0}:${state.lang}`;
  if (!cache || cache.key !== key || Date.now() - cache.at > 30000) {
    const p = api.get<Rendered[]>(`${BASE}/blocks?lang=${encodeURIComponent(state.lang)}`);
    cache = { key, at: Date.now(), blocks: p };
    p.catch(() => { cache = null; });
  }
  return cache.blocks;
}

async function slot(position: string): Promise<Node | null> {
  let list: Rendered[];
  try { list = (await blocks()).filter((b) => b.location === position); } catch { return null; }
  if (!list.length) return null;
  return h('div', { class: `ewb ewb--${position}` }, ...list.map((b) => {
    const id = `ewb-${b.id}`;
    return h('section', { class: 'ewb-block', 'aria-labelledby': b.title ? id : undefined, 'aria-label': b.title ? undefined : positionLabel(position), lang: b.lang },
      b.title ? h('h2', { id, class: 'ewb-title' }, b.title) : null, trustedHTML(b.html, 'ew-content ewb-content'));
  }));
}

// ---------------------------------------------------------------- admin

function blockDialog(b: Block | null, reload: () => void): void {
  const name = textInput({ value: b?.name ?? '', required: true, maxlength: 200 });
  const loc = select(positionOptions(b?.location), b?.location ?? 'footer');
  const pos = textInput({ type: 'number', value: String(b?.position ?? 0) });
  const audience = select(['all', 'users', 'guests'].map((x) => ({ value: x, label: t(`core.blocks.audience.${x}`) })), b?.audience ?? 'all');
  const enabled = checkbox(t('core.blocks.enabled'), b?.enabled ?? true);
  const langs = state.site.languages.map((l) => {
    const title = textInput({ value: b?.titles[l] ?? '', lang: l, maxlength: 200 });
    const format = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], b?.bodies[l]?.format ?? 'markdown');
    const src = textArea({ value: b?.bodies[l]?.source ?? '', rows: 6, lang: l });
    src.classList.add('ewb-source');
    const lname = t(`lang.${l}`) === `lang.${l}` ? l.toUpperCase() : t(`lang.${l}`);
    return { l, title, format, src, el: h('fieldset', { class: 'ewb-lang' }, h('legend', null, lname),
      field(t('core.blocks.title'), title, { help: t('core.blocks.title_help') }), field(t('core.blocks.format'), format, { inline: true }),
      field(t('core.blocks.content'), src, { help: t('core.blocks.content_help'), required: l === state.site.default_language })) };
  });
  formDialog(b ? b.name : t('core.blocks.add'), [
    field(t('core.blocks.name'), name, { required: true, help: t('core.blocks.name_help') }),
    field(t('core.blocks.position'), loc, { help: t('core.blocks.position_help', { template: templateLabel(activeTemplate()?.id ?? '') }) }),
    field(t('core.blocks.order'), pos), field(t('core.blocks.audience'), audience), enabled.el,
    ...langs.map((x) => x.el)], t('common.save'), async () => {
    const body = {
      name: name.value, location: loc.value, position: Number(pos.value) || 0, audience: audience.value, enabled: enabled.input.checked,
      titles: Object.fromEntries(langs.map((x) => [x.l, x.title.value])),
      bodies: Object.fromEntries(langs.filter((x) => x.src.value.trim()).map((x) => [x.l, { format: x.format.value, source: x.src.value }])),
    };
    if (b) await api.put(`${BASE}/blocks/${b.id}`, body);
    else await api.post(`${BASE}/blocks`, body);
    cache = null;
    toast(t('core.blocks.saved', { position: positionLabel(loc.value) }));
    await refreshPositions();
    reload();
  }, { wide: true });
}

async function adminPage(el: HTMLElement): Promise<void> {
  const list = await api.get<Block[]>(`${BASE}/blocks?all=1`);
  const reload = () => void adminPage(el).catch((e) => toast(errorText(e), 'error'));
  const offered = new Set(activeTemplate()?.positions ?? []);
  mount(el, pageHeader(t('core.blocks.title_admin'), button(t('core.blocks.add'), () => blockDialog(null, reload), { variant: 'primary' })),
    h('p', null, t('core.blocks.help'), ' ', h('a', { href: '/?positions=1' }, t('positions.show'))),
    list.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.blocks.title_admin')),
      h('thead', null, h('tr', null, ...[t('core.blocks.name'), t('core.blocks.position'), t('core.blocks.audience'), t('common.status'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...list.map((b) => h('tr', null,
        h('th', { scope: 'row' }, b.name, h('br'), h('span', { class: 'ew-help' }, pick(b.titles, state.lang, state.site.default_language))),
        h('td', null, positionLabel(b.location), ' ', h('code', null, b.location), ' · ', String(b.position)),
        h('td', null, t(`core.blocks.audience.${b.audience}`)),
        h('td', null, !b.enabled ? h('span', { class: 'ew-badge' }, t('core.blocks.disabled'))
          : offered.has(b.location) ? h('span', { class: 'ew-badge ew-badge--ok' }, t('core.blocks.visible'))
            : h('span', { class: 'ew-badge ew-badge--warn' }, t('core.blocks.hidden_position'))),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => blockDialog(b, reload), { variant: 'link', ariaLabel: `${t('common.edit')}: ${b.name}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('core.blocks.delete_confirm', { name: b.name }), t('common.delete'), true))) return;
            try { await api.del(`${BASE}/blocks/${b.id}`); cache = null; await refreshPositions(); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${b.name}` })))))) : h('p', null, t('core.blocks.empty')));
}

export function register(): void {
  registerSlot({ id: 'core.blocks', position: '*', order: 30, render: slot });
  registerAdminPage({ id: 'blocks', label: () => t('core.blocks.title_admin'), order: 11, render: adminPage });
}
