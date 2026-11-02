// Blog element: editor (category, layout, options) and the "blog" widget
// (search within the blog, paging, tag filter, new post / subcategory, feed).
import {
  announce, api, button, categorySelect, checkbox, errorText, feedButton, field, formDialog, h, langFields, mount, navigate, pick,
  registerEditor, registerWidget, select, state, t, textArea, textInput, toast, trustedHTML, type EditorInstance,
} from '@ewiki/sdk';
import './style.css';

const BASE = '/api/v1/x/content.blog';

interface Body {
  category_id: number; recursive: boolean; intro: { format: string; source: string }; feed: boolean;
  per_page: number; layout: string; show_author: boolean; show_date: boolean; full_content: boolean; summary_length: number; order: string;
}
interface Post { id: number; category_id: number; lang: string; title: string; summary: string; tags: string[]; author: string; created_at: string; updated_at: string; html?: string }
interface Hit { element_id: number; title: string; snippet: string; lang: string }
interface Cat { id: number; depth: number; titles: Record<string, string>; can_create: boolean }
interface Info { categories: Cat[]; can_create: boolean; can_manage: boolean; missing: boolean }
interface Posts { config: Body; info: Info; items: Post[]; hits?: Hit[]; total: number; page: number; pages: number }

const L = () => state.lang;
const D = () => state.site.default_language;

// ---------------------------------------------------------------- editor

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const b = { ...(raw as Body) };
  b.intro = b.intro ?? { format: 'markdown', source: '' };
  const err = h('p', { class: 'ew-field-error', role: 'alert' });
  const catBox = h('div');
  let cats: Awaited<ReturnType<typeof categorySelect>> | null = null;
  void categorySelect({ selected: b.category_id, perm: 'view', noneLabel: t('content.blog.choose_category'), allowCreate: true })
    .then((cs) => { cats = cs; catBox.append(cs.field(t('content.blog.category'), { help: t('content.blog.category_help'), required: true })); });
  const rec = checkbox(t('content.blog.recursive'), b.recursive);
  const fmt = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], b.intro.format);
  const intro = textArea({ value: b.intro.source, rows: 4 });
  const layout = select(['list', 'cards', 'compact'].map((v) => ({ value: v, label: t(`content.blog.layout_${v}`) })), b.layout);
  const order = select(['created', 'updated'].map((v) => ({ value: v, label: t(`content.blog.order_${v}`) })), b.order);
  const per = textInput({ type: 'number', value: String(b.per_page || 10) });
  const sum = textInput({ type: 'number', value: String(b.summary_length || 280) });
  const date = checkbox(t('content.blog.show_date'), b.show_date);
  const author = checkbox(t('content.blog.show_author'), b.show_author);
  const full = checkbox(t('content.blog.full_content'), b.full_content);
  const feed = checkbox(t('content.blog.feed'), b.feed);
  mount(container, h('div', { class: 'ewb-editor' },
    h('fieldset', null, h('legend', null, t('content.blog.source')), catBox, rec.el),
    h('fieldset', null, h('legend', null, t('content.blog.intro')), field(t('content.blog.intro_format'), fmt), field(t('content.blog.intro_text'), intro, { help: t('content.blog.intro_help') })),
    h('fieldset', null, h('legend', null, t('content.blog.display')),
      h('div', { class: 'ewb-editor-grid' }, field(t('content.blog.layout'), layout), field(t('content.blog.order'), order),
        field(t('content.blog.per_page'), per), field(t('content.blog.summary_length'), sum)),
      date.el, author.el, full.el, feed.el),
    err));
  return {
    getBody: () => ({
      category_id: cats?.value() ?? b.category_id, recursive: rec.input.checked, intro: { format: fmt.value, source: intro.value }, feed: feed.input.checked,
      per_page: Number(per.value) || 10, layout: layout.value, order: order.value, show_author: author.input.checked, show_date: date.input.checked,
      full_content: full.input.checked, summary_length: Number(sum.value) || 280,
    }),
    validate: () => {
      const ok = (cats?.value() ?? b.category_id) > 0;
      err.textContent = ok ? '' : t('content.blog.err_category');
      return ok ? null : t('content.blog.err_category');
    },
  };
}

// ---------------------------------------------------------------- widget

function dateOf(p: Post, c: Body): string {
  const d = new Date(c.order === 'updated' ? p.updated_at : p.created_at);
  return d.toLocaleDateString(L(), { dateStyle: 'medium' });
}

function renderPost(p: Post, c: Body, onTag: (tag: string) => void): HTMLElement {
  const iso = c.order === 'updated' ? p.updated_at : p.created_at;
  const meta = c.show_date || (c.show_author && p.author)
    ? h('p', { class: 'ewb-meta' }, c.show_date ? h('time', { datetime: iso }, dateOf(p, c)) : null,
      c.show_date && c.show_author && p.author ? ' · ' : null, c.show_author && p.author ? `${t('content.blog.by')} ${p.author}` : null)
    : null;
  return h('article', { class: 'ewb-post', lang: p.lang },
    h('h2', { class: 'ewb-title' }, h('a', { href: `/e/${p.id}` }, p.title)),
    meta,
    c.full_content && p.html ? h('div', { class: 'ewb-content' }, trustedHTML(p.html))
      : p.summary ? [h('p', { class: 'ewb-summary' }, p.summary),
        h('p', { class: 'ewb-readmore' }, h('a', { href: `/e/${p.id}` }, t('content.blog.more'), h('span', { class: 'sr-only' }, `: ${p.title}`)))] : null,
    p.tags.length ? h('ul', { class: 'ewb-tags', 'aria-label': t('common.tags') }, ...p.tags.map((x) => h('li', null,
      button(x, () => onTag(x), { variant: 'link', ariaLabel: `${x}: ${t('content.blog.filter_tag')}` })))) : null,
    state.editMode ? h('p', { class: 'ewb-edit' }, h('a', { href: `/e/${p.id}/edit?lang=${L()}` }, t('common.edit'), h('span', { class: 'sr-only' }, `: ${p.title}`))) : null);
}

function catOptions(cats: Cat[], onlyCreate: boolean): { value: string; label: string }[] {
  return cats.filter((c) => !onlyCreate || c.can_create).map((c) => ({ value: String(c.id), label: `${'— '.repeat(c.depth)}${pick(c.titles, L(), D())}` }));
}

/** Asks for the category (if there are several) and opens the editor for a new post. */
function newPost(id: number, cats: Cat[]): void {
  const allowed = cats.filter((c) => c.can_create);
  const go = (cat: string) => navigate(`/c/${cat}/new?type=core.html&return=${encodeURIComponent(`/e/${id}`)}`);
  if (allowed.length === 1) {
    go(String(allowed[0].id));
    return;
  }
  const sel = select(catOptions(cats, true));
  formDialog(t('content.blog.new_post'), [field(t('content.blog.post_category'), sel)], t('common.next'), async () => go(sel.value));
}

function newCategory(id: number, cats: Cat[], done: () => void): void {
  const parent = select(catOptions(cats, true));
  const titles = langFields(t('common.title'), {}, { required: true });
  formDialog(t('content.blog.new_category'), [field(t('category.parent'), parent), titles.el], t('common.create'), async () => {
    await api.post(`${BASE}/elements/${id}/categories`, { parent_id: Number(parent.value), titles: titles.get() });
    toast(t('category.created'));
    done();
  }, { wide: true });
}

async function blogWidget(root: HTMLElement): Promise<void> {
  const id = Number(root.dataset.element);
  let data: Posts;
  try { data = await api.get<Posts>(`${BASE}/elements/${id}/posts?lang=${L()}`); } catch { return; }
  if (data.info.missing) return;
  const c = data.config;
  const posts = root.querySelector<HTMLElement>('.ewb-posts') ?? root.appendChild(h('div', { class: 'ewb-posts' }));
  root.querySelector('.ewb-more-static')?.remove();
  const status = h('p', { class: 'ewb-status', role: 'status' });
  const pager = h('nav', { class: 'ewb-pager', 'aria-label': t('content.blog.pages') });
  let tag = '';

  const show = (d: Posts) => {
    if (d.hits) {
      posts.replaceChildren(...(d.hits.length ? d.hits.map((x) => h('article', { class: 'ewb-post ewb-hit', lang: x.lang },
        h('h2', { class: 'ewb-title' }, h('a', { href: `/e/${x.element_id}` }, x.title)), h('p', { class: 'ewb-summary' }, trustedHTML(x.snippet))))
        : [h('p', { class: 'ewb-empty' }, t('content.blog.no_hits'))]));
      status.textContent = t('content.blog.hits', { n: d.hits.length });
      pager.replaceChildren();
      return;
    }
    posts.replaceChildren(...(d.items.length ? d.items.map((p) => renderPost(p, c, (x) => void load(1, x))) : [h('p', { class: 'ewb-empty' }, t('content.blog.empty'))]));
    status.textContent = tag ? t('content.blog.tag_filter', { tag, n: d.total }) : '';
    mount(pager,
      d.page > 1 ? button(t('content.blog.newer'), () => void load(d.page - 1), { variant: 'secondary' }) : null,
      d.pages > 1 ? h('span', { class: 'ewb-page' }, t('content.blog.page_of', { page: d.page, pages: d.pages })) : null,
      d.page < d.pages ? button(t('content.blog.older'), () => void load(d.page + 1), { variant: 'secondary' }) : null,
      tag ? button(t('content.blog.clear_filter'), () => { tag = ''; void load(1); }, { variant: 'link' }) : null);
  };
  const load = async (page: number, newTag?: string) => {
    if (newTag !== undefined) tag = newTag;
    try {
      const d = await api.get<Posts>(`${BASE}/elements/${id}/posts?lang=${L()}&page=${page}${tag ? `&tag=${encodeURIComponent(tag)}` : ''}`);
      show(d);
      if (page > 1 || newTag !== undefined) {
        root.querySelector<HTMLElement>('.ewb-toolbar')?.scrollIntoView({ block: 'start' });
        announce(status.textContent || t('content.blog.page_of', { page: d.page, pages: d.pages }));
      }
    } catch (e) { toast(errorText(e), 'error'); }
  };

  // search within the blog
  const q = textInput({ type: 'search', placeholder: t('content.blog.search_placeholder') });
  q.setAttribute('aria-label', t('content.blog.search'));
  const form = h('form', { class: 'ewb-search', role: 'search' }, q, button(t('content.blog.search'), () => undefined, { type: 'submit', variant: 'secondary' }));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    if (!q.value.trim()) { void load(1); return; }
    try { show(await api.get<Posts>(`${BASE}/elements/${id}/posts?lang=${L()}&q=${encodeURIComponent(q.value)}`)); } catch (e) { toast(errorText(e), 'error'); }
  });
  q.addEventListener('search', () => { if (!q.value) void load(1); });

  const title = document.querySelector('h1')?.textContent ?? 'Blog';
  const actions = h('div', { class: 'ew-actions' },
    data.info.can_create ? button(t('content.blog.new_post'), () => newPost(id, data.info.categories), { variant: 'primary' }) : null,
    data.info.can_manage && c.recursive ? button(t('content.blog.new_category'), () => newCategory(id, data.info.categories, () => void load(1)), { variant: 'secondary' }) : null,
    c.feed ? feedButton(`${BASE}/elements/${id}/feed`, title) : null);
  posts.before(h('div', { class: 'ewb-toolbar' }, form, actions), status);
  posts.after(pager);
  show(data);
}

export function register(): void {
  registerEditor('content.blog', createEditor);
  registerWidget('blog', blogWidget);
}
