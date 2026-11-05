// Carousels at template positions, their administration and the publish
// option "as news". The widget follows the WAI-ARIA carousel pattern:
// region with aria-roledescription, previous/next and slide buttons, a
// pause button for automatic rotation (which also stops on hover, focus and
// with prefers-reduced-motion), live announcements only when not rotating.
import { activeTemplate, api, button, checkbox, confirmDialog, errorText, field, fmtDate, formDialog, h, pageHeader, pluginSettingsForm, positionOptions, registerAdminPage, registerPublishOption, registerSlot, select, state, t, textArea, textInput, toast, uid, uploadFile } from '@ewiki/sdk';
import './style.css';

const BASE = 'x/core.carousel';

interface Slide { id?: number; kind: 'manual' | 'news' | 'feed'; element_id?: number; title: string; text: string; link: string; image: string; image_alt: string; starts_at?: string | null; ends_at?: string | null; date?: string | null; source?: string }
interface Shown { id: number; name: string; autoplay: number; slides: Slide[] }
interface Carousel { id: number; name: string; position: string; audience: 'all' | 'guests' | 'users'; autoplay: number; max_slides: number; feeds: string[]; feed_items: number; enabled: boolean; order: number }

const reduced = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

function carousel(c: Shown): HTMLElement {
  const n = c.slides.length;
  let cur = 0;
  let timer = 0;
  let playing = c.autoplay > 0 && !reduced();
  let paused = false; // hover or focus inside
  const id = uid('ewcar');
  const live = h('div', { class: 'ewcar-slides', 'aria-live': playing ? 'off' : 'polite', id: `${id}-slides` });
  const slides = c.slides.map((s, i) => {
    const img = s.image ? h('img', { src: s.image, alt: s.image_alt || '', loading: i === 0 ? 'eager' : 'lazy', class: 'ewcar-img' }) : null;
    const titleEl = s.link ? h('a', { href: s.link, rel: s.kind === 'feed' ? 'noopener external' : undefined }, s.title) : s.title;
    return h('div', { class: 'ewcar-slide', role: 'group', 'aria-roledescription': t('core.carousel.slide'), 'aria-label': t('core.carousel.slide_of', { n: i + 1, total: n }), hidden: i !== 0 },
      img, h('div', { class: 'ewcar-caption' },
        h('h3', { class: 'ewcar-title' }, titleEl),
        s.text ? h('p', { class: 'ewcar-text' }, s.text) : null,
        h('p', { class: 'ewcar-meta' },
          s.kind === 'news' ? h('span', { class: 'ew-badge ew-badge--info' }, t('core.carousel.news')) : null,
          s.source ? h('span', null, ` ${s.source}`) : null,
          s.date ? h('time', { datetime: s.date }, ` ${fmtDate(s.date, state.lang)}`) : null)));
  });
  live.append(...slides);
  const dots = h('div', { class: 'ewcar-dots', role: 'group', 'aria-label': t('core.carousel.choose') });
  const dotBtns = c.slides.map((s, i) => {
    const b = h('button', { type: 'button', class: 'ewcar-dot', 'aria-label': t('core.carousel.goto', { n: i + 1, title: s.title }), 'aria-current': i === 0 ? 'true' : undefined, onclick: () => show(i, true) }) as HTMLButtonElement;
    dots.append(b);
    return b;
  });
  const show = (i: number, user = false) => {
    cur = (i + n) % n;
    slides.forEach((s, k) => { s.hidden = k !== cur; });
    dotBtns.forEach((b, k) => { if (k === cur) b.setAttribute('aria-current', 'true'); else b.removeAttribute('aria-current'); });
    if (user && playing) { playing = false; sync(); }
  };
  const pauseBtn = c.autoplay > 0 ? h('button', { type: 'button', class: 'ewcar-pause', onclick: () => { playing = !playing; sync(); } }) as HTMLButtonElement : null;
  const sync = () => {
    window.clearInterval(timer);
    live.setAttribute('aria-live', playing ? 'off' : 'polite');
    if (pauseBtn) {
      pauseBtn.setAttribute('aria-label', playing ? t('core.carousel.pause') : t('core.carousel.play'));
      pauseBtn.replaceChildren(h('span', { 'aria-hidden': 'true' }, playing ? '⏸' : '▶'));
    }
    if (playing && n > 1) timer = window.setInterval(() => { if (!paused && document.visibilityState === 'visible') show(cur + 1); }, c.autoplay * 1000);
  };
  const root = h('section', { class: 'ewcar', 'aria-roledescription': t('core.carousel.carousel'), 'aria-label': c.name, id },
    n > 1 ? h('div', { class: 'ewcar-controls' },
      pauseBtn,
      button(h('span', { 'aria-hidden': 'true' }, '‹'), () => show(cur - 1, true), { variant: 'subtle', ariaLabel: t('core.carousel.previous') }),
      button(h('span', { 'aria-hidden': 'true' }, '›'), () => show(cur + 1, true), { variant: 'subtle', ariaLabel: t('core.carousel.next') })) : null,
    live, n > 1 ? dots : null);
  root.addEventListener('mouseenter', () => { paused = true; });
  root.addEventListener('mouseleave', () => { paused = false; });
  root.addEventListener('focusin', () => { paused = true; });
  root.addEventListener('focusout', (ev) => { if (!root.contains(ev.relatedTarget as Node)) paused = false; });
  sync();
  return root;
}

const cache = new Map<string, Promise<Shown[]>>();

function widget(position: string): HTMLElement | null {
  const box = h('div', { class: 'ewcar-pos', hidden: true });
  const key = `${position}|${state.lang}|${state.user?.id ?? 0}`;
  if (!cache.has(key)) cache.set(key, api.get<Shown[]>(`${BASE}/position/${encodeURIComponent(position)}?lang=${encodeURIComponent(state.lang)}`).catch(() => []));
  void cache.get(key)!.then((list) => {
    if (!list.length) return;
    box.replaceChildren(...list.map(carousel));
    box.hidden = false;
  });
  return box;
}

// ---------------------------------------------------------------- administration

const isoDate = (s?: string | null) => (s ? s.slice(0, 10) : '');
const toISO = (d: string, end = false) => (d ? new Date(`${d}T${end ? '23:59:59' : '00:00:00'}`).toISOString() : null);

function carouselDialog(c: Carousel | null, done: () => void): void {
  const name = textInput({ value: c?.name ?? '', maxlength: 100 });
  const pos = select(positionOptions(c?.position), c?.position ?? (activeTemplate()?.positions.includes('content-top') ? 'content-top' : activeTemplate()?.positions[0]));
  const aud = select(['all', 'guests', 'users'].map((x) => ({ value: x, label: t(`core.carousel.audience.${x}`) })), c?.audience ?? 'all');
  const auto = textInput({ type: 'number', value: String(c?.autoplay ?? 0) });
  const max = textInput({ type: 'number', value: String(c?.max_slides ?? 10) });
  const feeds = textArea({ value: (c?.feeds ?? []).join('\n'), rows: 3 });
  const per = textInput({ type: 'number', value: String(c?.feed_items ?? 5) });
  const enabled = checkbox(t('core.carousel.enabled'), c?.enabled ?? true);
  formDialog(c ? t('core.carousel.edit') : t('core.carousel.create'), [
    field(t('core.carousel.name'), name, { required: true, help: t('core.carousel.name_help') }),
    field(t('core.carousel.position'), pos), field(t('core.carousel.audience'), aud),
    field(t('core.carousel.autoplay'), auto, { help: t('core.carousel.autoplay_help') }),
    field(t('core.carousel.max_slides'), max),
    field(t('core.carousel.feeds'), feeds, { help: t('core.carousel.feeds_help') }),
    field(t('core.carousel.feed_items'), per), enabled.el,
  ], t('common.save'), async () => {
    const body = { name: name.value, position: pos.value, audience: aud.value, autoplay: Number(auto.value) || 0, max_slides: Number(max.value) || 10,
      feeds: feeds.value.split('\n').map((x) => x.trim()).filter(Boolean), feed_items: Number(per.value) || 5, enabled: enabled.input.checked, order: c?.order ?? 0 };
    if (c) await api.put(`${BASE}/carousels/${c.id}`, body); else await api.post(`${BASE}/carousels`, body);
    cache.clear();
    toast(t('common.saved'));
    done();
  }, { wide: true });
}

function slideDialog(car: Carousel, s: Slide | null, done: () => void): void {
  const news = s?.kind === 'news';
  const title = textInput({ value: s?.title ?? '', maxlength: 200 });
  const text = textArea({ value: s?.text ?? '', rows: 3 });
  const link = textInput({ value: s?.link ?? '', placeholder: '/wiki/… https://…' });
  let image = s?.image ?? '';
  const preview = h('div', { class: 'ewcar-preview' });
  const showPreview = () => preview.replaceChildren(image ? h('img', { src: image, alt: '' }) : h('span', { class: 'ew-muted' }, t('core.carousel.no_image')));
  showPreview();
  const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp,image/gif', class: 'ew-input' }) as HTMLInputElement;
  file.addEventListener('change', async () => {
    const f = file.files?.[0];
    if (!f) return;
    try { const up = await uploadFile(f); image = `/api/v1/files/${up.hash}`; showPreview(); } catch (e) { toast(errorText(e), 'error'); }
  });
  const alt = textInput({ value: s?.image_alt ?? '', maxlength: 200 });
  const from = textInput({ type: 'date', value: isoDate(s?.starts_at) });
  const until = textInput({ type: 'date', value: isoDate(s?.ends_at) });
  const fields = news
    ? [h('p', null, t('core.carousel.news_help', { title: s?.title ?? '' })), field(t('core.carousel.from'), from), field(t('core.carousel.until'), until)]
    : [field(t('core.carousel.slide_title'), title, { required: true }), field(t('core.carousel.text'), text), field(t('core.carousel.link'), link),
      h('div', { class: 'ew-field' }, h('span', { class: 'ew-label' }, t('core.carousel.image')), preview), field(t('core.carousel.upload'), file),
      field(t('core.carousel.image_alt'), alt, { help: t('core.carousel.image_alt_help') }),
      field(t('core.carousel.from'), from), field(t('core.carousel.until'), until)];
  formDialog(s ? t('core.carousel.edit_slide') : t('core.carousel.add_slide'), fields, t('common.save'), async () => {
    const body = { title: title.value, text: text.value, link: link.value, image, image_alt: alt.value, starts_at: toISO(from.value), ends_at: toISO(until.value, true) };
    if (s?.id) await api.put(`${BASE}/carousels/${car.id}/slides/${s.id}`, body); else await api.post(`${BASE}/carousels/${car.id}/slides`, body);
    cache.clear();
    toast(t('common.saved'));
    done();
  }, { wide: true });
}

async function slidesPanel(panel: HTMLElement, car: Carousel, back: () => void): Promise<void> {
  const data = await api.get<{ slides: Slide[]; feeds: { feed: string; fetched_at: string | null; error: string; items: number }[] }>(`${BASE}/carousels/${car.id}/slides`);
  const reload = () => void slidesPanel(panel, car, back);
  const order = async (i: number, d: number) => {
    const ids = data.slides.map((x) => x.id!);
    [ids[i], ids[i + d]] = [ids[i + d], ids[i]];
    try { await api.put(`${BASE}/carousels/${car.id}/order`, { slides: ids }); cache.clear(); reload(); } catch (e) { toast(errorText(e), 'error'); }
  };
  const now = Date.now();
  const slideState = (s: Slide) => (s.ends_at && Date.parse(s.ends_at) < now ? t('core.carousel.expired') : s.starts_at && Date.parse(s.starts_at) > now ? t('core.carousel.scheduled') : t('core.carousel.active'));
  panel.replaceChildren(...[
    pageHeader(t('core.carousel.slides_of', { name: car.name }), button(t('core.carousel.add_slide'), () => slideDialog(car, null, reload), { variant: 'primary' })),
    h('p', null, button(t('core.carousel.back'), back, { variant: 'link' })),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.carousel.slides_of', { name: car.name })),
      h('thead', null, h('tr', null, ...[t('core.carousel.slide_title'), t('core.carousel.kind'), t('common.status'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(data.slides.length ? data.slides.map((s, i) => h('tr', null,
        h('th', { scope: 'row' }, s.title),
        h('td', null, t(`core.carousel.kind.${s.kind}`)),
        h('td', null, slideState(s), s.ends_at ? h('span', { class: 'ew-help' }, ` · ${t('core.carousel.until')}: ${fmtDate(s.ends_at, state.lang)}`) : null),
        h('td', { class: 'ew-actions' },
          i > 0 ? button(t('category.move_up'), () => void order(i, -1), { variant: 'link', ariaLabel: `${t('category.move_up')}: ${s.title}` }) : null,
          i < data.slides.length - 1 ? button(t('category.move_down'), () => void order(i, 1), { variant: 'link', ariaLabel: `${t('category.move_down')}: ${s.title}` }) : null,
          button(t('common.edit'), () => slideDialog(car, s, reload), { variant: 'link', ariaLabel: `${t('common.edit')}: ${s.title}` }),
          button(t('common.remove'), async () => {
            if (!(await confirmDialog(t('core.carousel.remove_slide', { title: s.title }), t('common.remove'), true))) return;
            try { await api.del(`${BASE}/carousels/${car.id}/slides/${s.id}`); cache.clear(); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.remove')}: ${s.title}` })))) : [h('tr', null, h('td', { colspan: '4' }, t('core.carousel.no_slides')))]))),
    car.feeds.length ? h('section', { class: 'ew-section', 'aria-labelledby': 'ewcar-feeds' }, h('h2', { id: 'ewcar-feeds' }, t('core.carousel.feeds')),
      h('ul', { class: 'ew-list' }, ...car.feeds.map((f) => {
        const st = data.feeds.find((x) => x.feed === f);
        return h('li', null, h('code', null, f), ' – ', st ? (st.error ? h('span', { class: 'ew-badge ew-badge--warn' }, st.error) : t('core.carousel.feed_ok', { n: st.items, date: st.fetched_at ? fmtDate(st.fetched_at, state.lang) : '–' })) : t('core.carousel.feed_pending'));
      })),
      button(t('core.carousel.refresh'), async () => {
        try { await api.post(`${BASE}/carousels/${car.id}/refresh`, {}); toast(t('core.carousel.refreshed')); } catch (e) { toast(errorText(e), 'error'); }
        cache.clear();
        reload();
      }, { variant: 'secondary' })) : null,
  ].filter((x) => x !== null) as HTMLElement[]);
}

async function adminPage(panel: HTMLElement): Promise<void> {
  const list = await api.get<Carousel[]>(`${BASE}/carousels`);
  const reload = () => void adminPage(panel);
  const positions = new Set(activeTemplate()?.positions ?? []);
  const settings = await pluginSettingsForm('core.carousel', [{ key: 'allow_private', label: t('core.carousel.allow_private'), type: 'bool', help: t('core.carousel.allow_private_help') }]);
  panel.replaceChildren(
    pageHeader(t('core.carousel.admin'), button(t('core.carousel.create'), () => carouselDialog(null, reload), { variant: 'primary' })),
    h('p', null, t('core.carousel.admin_help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.carousel.admin')),
      h('thead', null, h('tr', null, ...[t('core.carousel.name'), t('core.carousel.position'), t('core.carousel.audience'), t('common.status'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(list.length ? list.map((c) => h('tr', null,
        h('th', { scope: 'row' }, c.name),
        h('td', null, c.position, positions.has(c.position) ? null : h('span', { class: 'ew-badge ew-badge--warn' }, ` ${t('core.carousel.not_in_template')}`)),
        h('td', null, t(`core.carousel.audience.${c.audience}`)),
        h('td', null, c.enabled ? t('core.carousel.on') : t('core.carousel.off')),
        h('td', { class: 'ew-actions' },
          button(t('core.carousel.slides'), () => void slidesPanel(panel, c, reload), { variant: 'link', ariaLabel: `${t('core.carousel.slides')}: ${c.name}` }),
          button(t('common.edit'), () => carouselDialog(c, reload), { variant: 'link', ariaLabel: `${t('common.edit')}: ${c.name}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('core.carousel.delete_confirm', { name: c.name }), t('common.delete'), true))) return;
            try { await api.del(`${BASE}/carousels/${c.id}`); cache.clear(); reload(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${c.name}` })))) : [h('tr', null, h('td', { colspan: '5' }, t('core.carousel.none')))]))),
    h('section', { class: 'ew-section', 'aria-labelledby': 'ewcar-set' }, h('h2', { id: 'ewcar-set' }, t('core.carousel.settings')), settings.el));
}

export function register(): void {
  registerSlot({ id: 'core.carousel', position: '*', order: 5, render: (position) => widget(position) });
  registerAdminPage({ id: 'carousels', label: () => t('core.carousel.admin'), permission: 'core.carousel.manage', render: adminPage });
  // "as news" when publishing: only carousels placed in the active template
  registerPublishOption({
    id: 'core.carousel',
    render: async () => {
      let list: Carousel[];
      try { list = await api.get<Carousel[]>(`${BASE}/targets`); } catch { return null; }
      const positions = new Set(activeTemplate()?.positions ?? []);
      list = list.filter((c) => positions.has(c.position));
      if (!list.length) return null;
      const boxes = list.map((c) => ({ c, cb: checkbox(t('core.carousel.as_news_in', { name: c.name }), false) }));
      const until = textInput({ type: 'date' });
      const el = h('fieldset', { class: 'ewcar-publish' }, h('legend', null, t('core.carousel.as_news')),
        ...boxes.map((b) => b.cb.el), field(t('core.carousel.news_until'), until, { help: t('core.carousel.news_until_help') }));
      return { el, value: () => { const ids = boxes.filter((b) => b.cb.input.checked).map((b) => b.c.id); return ids.length ? { carousels: ids, until: until.value } : null; } };
    },
  });
}
