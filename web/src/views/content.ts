// Category and element views.
import { api, ApiError, qs } from '../core/api';
import { h, mount, trustedHTML, fmtDate, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { hasPerm, state } from '../core/state';
import type { Category, ElementSummary, ElementView, Revision } from '../core/types';
import { toast } from '../core/announce';
import { button, confirmDialog, errorText, field, formDialog, langFields, loading, openDialog, pageHeader, textArea, textInput, select } from '../components/ui';
import { breadcrumbs, can, categoryPicker, ctx, invalidateTree, langName, setContext } from '../components/widgets';
import { watchButton } from '../components/watch';
import { shortOnlyBox, shortURLInput } from '../components/shorturl';
import { imagesOf, previewImageInput } from '../components/previewimage';
import { feedButton } from '../components/feeds';
import { getEditor, pageActionButtons, renderCategoryExtensions, renderElementExtensions, typeLabel, type EditorInstance } from '../sdk';
import { draftNotice, publishDialog, publishVia, saveDraft, saveTemplateDialog, showPreview, templatePicker, type Draft, type EditorState } from '../components/publishing';

const L = () => state.lang;
const D = () => state.site.default_language;

/** Shows an error page for API errors (401 offers login). */
/**
 * Shows the readable path (/wiki/…) in the address bar instead of /e/12 or
 * /c/7, so copied links are readable (the page is not reloaded).
 */
export function showReadableURL(urls: Record<string, string> | undefined, fallback?: string): void {
  const url = urls?.[L()] || fallback;
  if (!url || url === location.pathname) return;
  // only technical addresses and readable paths (e.g. after switching the language), never a chosen short URL
  if (!/^\/(e|c)\/\d+\/?$/.test(location.pathname) && !location.pathname.startsWith('/wiki/')) return;
  history.replaceState(history.state, '', url + location.search + location.hash);
}

interface NotFoundInfo { deleted: { type: string; id: number; title: string; can_restore: boolean } | null; suggestions: { type: string; id: number; title: string; path: string }[] }

/** "Page not found" with a deleted page and probable pages (by address or former title). */
export function notFoundPage(main: HTMLElement, q: { path?: string; type?: string; id?: number }): void {
  const box = h('div');
  mount(main, h('h1', null, t('error.not_found')), box, h('p', null, h('a', { href: '/' }, t('nav.home'))));
  void api.get<NotFoundInfo>(`not-found${qs({ path: q.path, type: q.type, id: q.id, lang: L() })}`).then((info) => {
    const parts: HTMLElement[] = [];
    if (info.deleted) {
      parts.push(h('div', { class: 'ew-notice ew-notice--warning', role: 'status' },
        h('p', null, t('notfound.deleted', { title: info.deleted.title || '–' })),
        info.deleted.can_restore ? h('p', null, h('a', { href: '/trash', class: 'ew-btn ew-btn--secondary' }, t('notfound.to_trash'))) : null));
    }
    if (info.suggestions.length) {
      parts.push(h('section', { 'aria-labelledby': 'ew-nf-sugg' }, h('h2', { id: 'ew-nf-sugg' }, t('notfound.suggestions')),
        h('ul', { class: 'ew-list' }, ...info.suggestions.map((s) => h('li', null, h('a', { href: s.path || `/${s.type === 'element' ? 'e' : 'c'}/${s.id}` }, s.title))))));
    } else if (!info.deleted) {
      parts.push(h('p', null, t('notfound.search_hint'), ' ', h('a', { href: `/search${qs({ q: (q.path ?? '').split('/').pop()?.replace(/-/g, ' ') })}` }, t('notfound.search'))));
    }
    box.replaceChildren(...parts);
  }).catch(() => undefined);
}

export function errorPage(main: HTMLElement, e: unknown, target?: { type: string; id: number }): void {
  if (e instanceof ApiError && e.status === 401) {
    const next = encodeURIComponent(location.pathname + location.search);
    mount(main, h('h1', null, t('error.login_required')), h('p', null, t('error.login_required_text')),
      h('a', { href: `/login?next=${next}`, class: 'ew-btn ew-btn--primary' }, t('auth.login')));
    return;
  }
  if (e instanceof ApiError && e.status === 404 && target) {
    notFoundPage(main, target);
    return;
  }
  if (e instanceof ApiError && (e.status === 404 || e.status === 403)) {
    mount(main, h('h1', null, t(e.status === 404 ? 'error.not_found' : 'error.forbidden')), h('p', null, e.message), h('a', { href: '/' }, t('nav.home')));
    return;
  }
  mount(main, h('h1', null, t('error.title')), h('p', { role: 'alert' }, errorText(e)));
}

function tagList(tags: string[]): HTMLElement | null {
  if (!tags?.length) return null;
  return h('ul', { class: 'ew-tags', 'aria-label': t('common.tags') }, ...tags.map((tg) => h('li', null, h('a', { href: `/search?tags=${encodeURIComponent(tg)}`, class: 'ew-tag' }, `#${tg}`))));
}

// ---------------------------------------------------------------- home

export async function homeView(ctx: RouteCtx): Promise<void> {
  const { main } = ctx;
  setContext(0, [], []);
  if (state.site.home_element) {
    // the start page shows an element (e.g. a welcome page or a category overview)
    await elementView({ ...ctx, params: { id: String(state.site.home_element) } });
    return;
  }
  if (state.site.home_category) {
    navigate(`/c/${state.site.home_category}`, { replace: true });
    return;
  }
  try {
    const roots = await api.get<Category[]>('categories?parent=0');
    if (roots.length === 1 && !state.editMode) {
      navigate(`/c/${roots[0].id}`, { replace: true });
      return;
    }
    mount(main,
      pageHeader(state.site.name, hasPerm('core.categories.admin') ? button(t('category.new_root'), () => newCategoryDialog(0), { variant: 'primary' }) : null),
      roots.length
        ? h('ul', { class: 'ew-cards' }, ...roots.map((c, i) => h('li', { class: 'ew-card' },
          h('h2', null, h('a', { href: `/c/${c.id}` }, pick(c.titles, L(), D()))),
          h('p', null, pick(c.descriptions, L(), D())),
          state.editMode && hasPerm('core.categories.admin') ? reorderButtons(0, roots, i) : null)))
        : h('p', null, state.user ? t('home.empty') : t('home.empty_guest')));
  } catch (e) {
    errorPage(main, e);
  }
}

// ---------------------------------------------------------------- category

interface CategoryResponse {
  category: Category;
  breadcrumbs: Category[];
  elements: ElementSummary[];
}

function crumbs(bc: Category[], last?: { title: string; href?: string }): HTMLElement {
  const items: { title: string; href?: string }[] = bc.map((c) => ({ title: pick(c.titles, L(), D()), href: `/c/${c.id}` }));
  if (last) items.push(last);
  return breadcrumbs([{ title: t('nav.home'), href: '/' }, ...items]);
}

export async function categoryView({ params, main }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  let data: CategoryResponse;
  let subs: Category[];
  try {
    [data, subs] = await Promise.all([api.get<CategoryResponse>(`categories/${id}`), api.get<Category[]>(`categories?parent=${id}`)]);
  } catch (e) {
    setContext(0, [], []);
    errorPage(main, e, { type: 'category', id });
    return;
  }
  const c = data.category;
  setContext(c.id, c.path, c.permissions);
  showReadableURL(c.urls, c.url);
  const title = pick(c.titles, L(), D());
  const edit = state.editMode;
  const actions: HTMLElement[] = [];
  if (edit) {
    // editors work with elements, managers (and category administrators) with the category itself
    if (can('create')) actions.push(h('a', { href: `/c/${c.id}/new`, class: 'ew-btn ew-btn--primary' }, t('element.new')));
    if (can('manage')) {
      actions.push(button(t('category.new_sub'), () => newCategoryDialog(c.id), { variant: 'secondary' }));
      actions.push(button(t('category.edit'), () => editCategoryDialog(c), { variant: 'secondary' }));
      if (state.site.languages.length > 1) {
        actions.push(button([t('translation.title'), c.translations_up_to_date ? null : h('span', { class: 'ew-badge ew-badge--warn' }, ` ${c.translated_percent} %`)],
          () => categoryTranslationsDialog(c), { variant: 'secondary', ariaLabel: `${t('translation.title')}: ${c.translated_percent} %` }));
      }
      actions.push(h('a', { href: `/c/${c.id}/settings`, class: 'ew-btn ew-btn--secondary' }, t('category.settings')));
      actions.push(button(t('common.move'), () => moveCategoryDialog(c), { variant: 'secondary' }));
      actions.push(button(t('common.delete'), () => void deleteCategory(c), { variant: 'danger' }));
      actions.push(button(t('common.duplicate'), () => duplicateCategoryDialog(c), { variant: 'secondary' }));
    }
  }
  const watchC = watchButton('category', c.id, title);
  if (watchC) actions.push(watchC);
  actions.push(...pageActionButtons({ type: 'category', id: c.id, title, categoryId: c.id }));
  if (c.feed && c.feed !== 'off') actions.push(feedButton(`/api/v1/feeds/categories/${c.id}`, title));
  const view = h('article', { class: 'ew-category' },
    crumbs(data.breadcrumbs, { title }),
    h('div', { class: 'ew-page-header' },
      h('h1', null, edit ? h('span', { class: 'ew-num' }, `${c.number} `) : null, title),
      actions.length ? h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('common.actions') }, ...actions) : null),
    pick(c.descriptions, L(), D()) ? h('p', { class: 'ew-lead' }, pick(c.descriptions, L(), D())) : null,
    tagList(c.tags),
  );
  // default element shown inline
  const others = data.elements.filter((e) => e.id !== c.default_element_id);
  if (c.default_element_id) {
    try {
      const el = await api.get<ElementView>(`elements/${c.default_element_id}${qs({ lang: L() })}`);
      const elEdit = edit && el.permissions?.includes('edit');
      view.append(h('section', { class: ['ew-default-element', elEdit && 'ew-default-element--edit'], 'aria-labelledby': 'ew-default-title' },
        h('h2', { id: 'ew-default-title', class: el.title === title && !elEdit ? 'sr-only' : undefined }, el.title),
        elEdit ? h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('element.page_actions', { title: el.title }) },
          h('a', { href: `/e/${el.id}/edit?lang=${L()}`, class: 'ew-btn ew-btn--primary' }, t('element.edit_page')),
          button(t('element.properties'), () => tagsDialog(el), { variant: 'secondary' }),
          h('a', { href: `/e/${el.id}/history`, class: 'ew-btn ew-btn--secondary' }, t('element.history'))) : null,
        fallbackNotice(el),
        trustedHTML(el.html),
        elEdit && el.tags.length ? tagList(el.tags) : null,
        h('p', { class: 'ew-meta' },
          h('a', { href: `/e/${el.id}` }, t('element.open_page')), ' · ',
          t('element.updated', { date: fmtDate(el.updated_at, L()) }))));
    } catch {
      /* default element not visible */
    }
  }
  if (others.length || (edit && data.elements.length)) {
    view.append(h('section', { class: 'ew-section', 'aria-labelledby': 'ew-pages-title' },
      h('h2', { id: 'ew-pages-title' }, t('category.pages')),
      h('ul', { class: 'ew-list' }, ...(edit ? data.elements : others).map((e) => h('li', null,
        h('a', { href: `/e/${e.id}` }, pick(e.titles, L(), e.original_lang)),
        edit && can('edit') ? [' ', h('a', { href: `/e/${e.id}/edit?lang=${L()}`, class: 'ew-inline-action', 'aria-label': t('element.edit_named', { title: pick(e.titles, L(), e.original_lang) }) }, t('common.edit'))] : null,
        ' ', h('span', { class: 'ew-badge' }, typeLabel(e.type)),
        e.is_default ? h('span', { class: 'ew-badge ew-badge--info' }, t('element.default')) : null)))));
  }
  if (subs.length) {
    view.append(h('section', { class: 'ew-section', 'aria-labelledby': 'ew-subs-title' },
      h('h2', { id: 'ew-subs-title' }, t('category.subcategories')),
      h('ul', { class: 'ew-cards' }, ...subs.map((s, i) => h('li', { class: 'ew-card' },
        h('h3', null, h('a', { href: `/c/${s.id}` }, pick(s.titles, L(), D()))),
        pick(s.descriptions, L(), D()) ? h('p', null, pick(s.descriptions, L(), D())) : null,
        h('p', { class: 'ew-meta' }, t('category.counts', { pages: s.element_count })),
        edit && can('manage') ? reorderButtons(c.id, subs, i) : null)))));
  }
  if (!c.default_element_id && !data.elements.length && !subs.length) {
    view.append(h('p', { class: 'ew-muted' }, can('create') ? t('category.empty_editor') : t('category.empty')));
  }
  const extensions = h('div', { class: 'ew-extensions' });
  view.append(extensions);
  mount(main, view);
  void renderCategoryExtensions(extensions, c);
  // drafts of new elements and unpublished changes (editors)
  if (state.user && (can('create') || can('edit'))) {
    void api.get<Draft[]>(`drafts${qs({ category: c.id })}`).then((ds) => {
      if (!ds.length) return;
      view.append(h('section', { class: 'ew-section ew-drafts', 'aria-labelledby': 'ew-drafts-title' },
        h('h2', { id: 'ew-drafts-title' }, t('draft.list_title')),
        h('ul', { class: 'ew-plain-list' }, ...ds.map((d) => h('li', null,
          h('a', { href: d.element_id ? `/e/${d.element_id}/edit${qs({ lang: d.lang })}` : `/d/${d.id}/edit` }, d.title || t('element.untitled')),
          ' ', h('span', { class: 'ew-badge' }, d.element_id ? t('draft.changes') : t('draft.new')),
          d.publish_at ? h('span', { class: 'ew-badge ew-badge--info' }, ' ', t('draft.scheduled_for', { date: fmtDate(d.publish_at, state.lang) })) : null,
          h('span', { class: 'ew-meta' }, ' ', t('draft.updated', { user: d.updated_by || d.created_by || '–', date: fmtDate(d.updated_at, state.lang) })))))));
    }).catch(() => undefined);
  }
}

/** Up/down buttons that change the order of sibling categories. */
export function reorderButtons(parent: number, siblings: Category[], i: number): HTMLElement {
  const title = pick(siblings[i].titles, L(), D());
  const move = async (delta: number) => {
    const ids = siblings.map((x) => x.id);
    [ids[i], ids[i + delta]] = [ids[i + delta], ids[i]];
    try {
      await api.post(`categories/${parent}/reorder`, { order: ids });
      invalidateTree();
      toast(t('category.reordered'));
      navigate(location.pathname, { replace: true });
    } catch (e) {
      toast(errorText(e), 'error');
    }
  };
  return h('div', { class: 'ew-actions' },
    i > 0 ? button(t('category.move_up'), () => void move(-1), { variant: 'link', ariaLabel: t('category.move_up_named', { title }) }) : null,
    i < siblings.length - 1 ? button(t('category.move_down'), () => void move(1), { variant: 'link', ariaLabel: t('category.move_down_named', { title }) }) : null);
}

export function newCategoryDialog(parent: number): void {
  void (async () => {
    const parentSel = await categoryPicker('manage', parent, { allowRoot: true });
    const titles = langFields(t('common.title'), {}, { required: true });
    const descs = langFields(t('common.description'), {}, { multiline: true });
    const tags = textInput({ name: 'tags' });
    formDialog(t('category.new'), [field(t('category.parent'), parentSel), titles.el, descs.el, field(t('common.tags'), tags, { help: t('common.tags_help') })], t('common.create'), async () => {
      const c = await api.post<Category>('categories', {
        parent_id: Number(parentSel.value) || null, titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value), original_lang: L(),
      });
      invalidateTree();
      toast(t('category.created'));
      navigate(`/c/${c.id}`);
    }, { wide: true });
  })();
}

/** Edits title, description and tags of a category. */
function editCategoryDialog(c: Category): void {
  const titles = langFields(t('common.title'), c.titles, { required: true });
  const descs = langFields(t('common.description'), c.descriptions, { multiline: true });
  const tags = textInput({ value: c.tags.join(', ') });
  const short = shortURLInput(c.short_url);
  const only = shortOnlyBox(!!c.short_only);
  const orig = select(state.site.languages.map((l) => ({ value: l, label: langName(l) })), c.original_lang || D());
  formDialog(t('category.edit_title', { title: pick(c.titles, L(), D()) }), [titles.el, descs.el,
    state.site.languages.length > 1 ? field(t('translation.original_lang'), orig, { help: t('translation.original_lang_help') }) : null,
    field(t('common.tags'), tags, { help: t('common.tags_help') }),
    field(t('shorturl.label'), short, { help: t('shorturl.help', { origin: location.origin }) }), only.el,
    h('p', { class: 'ew-help' }, t('category.edit_more'), ' ', h('a', { href: `/c/${c.id}/settings` }, t('category.settings')))], t('common.save'), async () => {
    await api.put(`categories/${c.id}`, { titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value), short_url: short.value, short_only: only.input.checked, original_lang: orig.value });
    invalidateTree();
    toast(t('common.saved'));
    navigate(location.pathname, { replace: true });
  }, { wide: true });
}

/** Translation state of a category: progress per language, translate, confirm. */
function categoryTranslationsDialog(c: Category): void {
  const name = pick(c.titles, L(), D());
  const after = () => { invalidateTree(); navigate(location.pathname, { replace: true }); };
  const rows = c.translations.map((tr) => h('tr', null,
    h('th', { scope: 'row' }, langName(tr.lang)),
    h('td', null, h('span', { class: ['ew-badge', `ew-badge--${tr.status}`] }, t(`translation.${tr.status}`)),
      tr.status === 'outdated' ? h('span', { class: 'ew-help' }, ' ', t('translation.based_on', { rev: tr.based_on ?? 0 })) : null),
    h('td', { lang: tr.lang }, c.titles[tr.lang] || '–'),
    h('td', { class: 'ew-actions' },
      button(tr.status === 'missing' ? t('translation.create') : t('common.edit'), () => { dlg.close(); translateCategoryDialog(c, tr.lang, after); },
        { variant: 'link', ariaLabel: `${tr.status === 'missing' ? t('translation.create') : t('common.edit')}: ${langName(tr.lang)}` }),
      tr.status === 'outdated' ? button(t('languages.confirm'), async () => {
        try { await api.put(`categories/${c.id}/translations/${tr.lang}`, { confirm: true }); toast(t('languages.confirmed')); dlg.close(); after(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('languages.confirm')}: ${langName(tr.lang)}` }) : null)));
  const dlg = openDialog(t('translation.category_title', { title: name }), h('div', null,
    h('p', null, h('progress', { max: '100', value: String(c.translated_percent), 'aria-label': t('translation.progress') }), ' ',
      t('translation.percent', { percent: c.translated_percent }), ' ',
      c.translations_up_to_date ? h('span', { class: 'ew-badge ew-badge--ok' }, t('translation.all_current')) : h('span', { class: 'ew-badge ew-badge--warn' }, t('translation.needs_work'))),
    h('p', { class: 'ew-help' }, t('translation.category_help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('translation.title')),
      h('thead', null, h('tr', null, ...[t('common.language'), t('common.status'), t('common.title'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...rows))), [], { wide: true });
}

/** Translates title and description of a category into one language (original shown for reference). */
function translateCategoryDialog(c: Category, lang: string, done: () => void): void {
  const orig = c.original_lang || D();
  const title = textInput({ value: c.titles[lang] ?? '', lang, maxlength: 200 });
  const desc = textArea({ value: c.descriptions[lang] ?? '', rows: 4, lang });
  const isOrig = lang === orig;
  formDialog(t('translation.category_translate', { lang: langName(lang) }), [
    isOrig ? null : h('dl', { class: 'ew-facts' },
      h('dt', null, t('translation.original_in', { lang: langName(orig) })), h('dd', { lang: orig }, c.titles[orig] ?? ''),
      c.descriptions[orig] ? h('dt', null, t('common.description')) : null, c.descriptions[orig] ? h('dd', { lang: orig }, c.descriptions[orig]) : null),
    field(t('common.title'), title, { required: isOrig, help: isOrig ? undefined : t('translation.empty_removes') }),
    field(t('common.description'), desc),
  ], t('common.save'), async () => {
    await api.put(`categories/${c.id}/translations/${lang}`, { title: title.value, description: desc.value });
    toast(t('common.saved'));
    done();
  }, { wide: true });
}

export function splitTags(s: string): string[] {
  return s.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean);
}

function moveCategoryDialog(c: Category): void {
  void (async () => {
    const sel = await categoryPicker('create', c.parent_id ?? 0, { allowRoot: true, exclude: c.id });
    formDialog(t('category.move_title', { title: pick(c.titles, L(), D()) }), field(t('category.new_parent'), sel), t('common.move'), async () => {
      await api.post(`categories/${c.id}/move`, { parent_id: Number(sel.value) });
      invalidateTree();
      toast(t('category.moved'));
      navigate(`/c/${c.id}`);
    });
  })();
}

function duplicateCategoryDialog(c: Category): void {
  void (async () => {
    const sel = await categoryPicker('create', c.parent_id ?? 0, { allowRoot: true });
    formDialog(t('category.duplicate_title', { title: pick(c.titles, L(), D()) }),
      [h('p', null, t('category.duplicate_help')), field(t('category.target'), sel)], t('common.duplicate'), async () => {
        const n = await api.post<Category>(`categories/${c.id}/duplicate`, { parent_id: Number(sel.value) });
        invalidateTree();
        toast(t('category.duplicated'));
        navigate(`/c/${n.id}`);
      });
  })();
}

async function deleteCategory(c: Category): Promise<void> {
  if (!(await confirmDialog(t('category.delete_confirm', { title: pick(c.titles, L(), D()) }), t('common.delete'), true))) return;
  try {
    await api.del(`categories/${c.id}`);
    invalidateTree();
    toast(t('category.deleted'));
    navigate(c.parent_id ? `/c/${c.parent_id}` : '/');
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

// ---------------------------------------------------------------- element

function fallbackNotice(el: ElementView): HTMLElement | null {
  if (!el.fallback) return null;
  return h('p', { class: 'ew-notice-inline', role: 'note' },
    t('element.fallback', { lang: langName(el.lang), shown: langName(el.shown_lang) }), ' ',
    state.editMode && el.permissions?.includes('edit') ? h('a', { href: `/e/${el.id}/edit?lang=${el.lang}` }, t('element.translate_now')) : null);
}

function statusLabel(s: string): string {
  return t(`translation.${s}`);
}

function translationPanel(el: ElementView): HTMLElement {
  const canEdit = el.permissions?.includes('edit');
  return h('section', { class: 'ew-panel', 'aria-labelledby': 'ew-tr-title' },
    h('h2', { id: 'ew-tr-title' }, t('translation.title')),
    el.translations.length < 2 ? null : h('p', null, h('progress', { max: '100', value: String(el.translated_percent), 'aria-label': t('translation.progress') }), ' ',
      t('translation.percent', { percent: el.translated_percent }), ' ',
      el.up_to_date ? h('span', { class: 'ew-badge ew-badge--ok' }, t('translation.all_current')) : h('span', { class: 'ew-badge ew-badge--warn' }, t('translation.needs_work'))),
    h('table', { class: 'ew-table' },
      h('caption', { class: 'sr-only' }, t('translation.title')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('common.language')), h('th', { scope: 'col' }, t('common.status')), h('th', { scope: 'col' }, t('common.revision')), h('th', { scope: 'col' }, h('span', { class: 'sr-only' }, t('common.actions'))))),
      h('tbody', null, ...el.translations.map((tr) => h('tr', null,
        h('th', { scope: 'row' }, langName(tr.lang)),
        h('td', null, h('span', { class: ['ew-badge', `ew-badge--${tr.status}`] }, statusLabel(tr.status)),
          tr.status === 'outdated' ? h('span', { class: 'ew-help' }, ' ', t('translation.based_on', { rev: tr.source_rev ?? 0 })) : null),
        h('td', null, tr.rev ? String(tr.rev) : '–'),
        h('td', null, canEdit ? h('a', { href: `/e/${el.id}/edit?lang=${tr.lang}` }, tr.status === 'missing' ? t('translation.create') : t('common.edit')) : null))))));
}

export async function elementView({ params, main }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  let el: ElementView;
  let cat: CategoryResponse;
  try {
    el = await api.get<ElementView>(`elements/${id}${qs({ lang: L() })}`);
    cat = await api.get<CategoryResponse>(`categories/${el.category_id}`);
  } catch (e) {
    setContext(0, [], []);
    errorPage(main, e, { type: 'element', id });
    return;
  }
  setContext(el.category_id, cat.category.path, el.permissions);
  showReadableURL(el.urls, el.url);
  const edit = state.editMode;
  const actions: HTMLElement[] = [];
  if (edit) {
    if (can('edit')) actions.push(h('a', { href: `/e/${el.id}/edit?lang=${L()}`, class: 'ew-btn ew-btn--primary' }, t('common.edit')));
    actions.push(h('a', { href: `/e/${el.id}/history`, class: 'ew-btn ew-btn--secondary' }, t('element.history')));
    if (can('edit')) actions.push(button(t('element.properties'), () => tagsDialog(el), { variant: 'secondary' }));
    if (can('edit') && !el.is_default) actions.push(button(t('element.make_default'), () => void makeDefault(el), { variant: 'secondary' }));
    if (can('delete')) actions.push(button(t('common.move'), () => moveElementDialog(el), { variant: 'secondary' }));
    actions.push(button(t('common.duplicate'), () => void duplicateElement(el), { variant: 'secondary' }));
    if (can('delete')) actions.push(button(t('common.delete'), () => void deleteElement(el), { variant: 'danger' }));
  }
  const watchE = watchButton('element', el.id, el.title);
  if (watchE) actions.push(watchE);
  actions.push(...pageActionButtons({ type: 'element', id: el.id, title: el.title, categoryId: el.category_id }));
  const extensions = h('div', { class: 'ew-element-extensions', lang: state.lang });
  const topExtensions = h('div', { class: 'ew-element-extensions ew-element-extensions--top', lang: state.lang });
  mount(main, h('article', { class: 'ew-element', lang: el.shown_lang },
    crumbs([...cat.breadcrumbs, cat.category], { title: el.title }),
    h('div', { class: 'ew-page-header' }, h('h1', null, el.title),
      actions.length ? h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('common.actions') }, ...actions) : null),
    fallbackNotice(el),
    edit ? translationPanel(el) : null,
    topExtensions,
    trustedHTML(el.html),
    h('footer', { class: 'ew-meta' },
      tagList(el.tags),
      el.short_url ? h('p', null, t('shorturl.label'), ': ', h('a', { href: `/${el.short_url}` }, `${location.host}/${el.short_url}`)) : null,
      h('p', null, t('element.updated_by', { date: fmtDate(el.updated_at, L()), user: el.updated_by || '–' }), ' · ', t('element.revision', { rev: el.rev }), ' · ',
        h('a', { href: `/e/${el.id}/history` }, t('element.history')))),
    extensions));
  void renderElementExtensions(extensions, el, topExtensions);
}

function tagsDialog(el: ElementView): void {
  // properties of an element: tags and short URL (element pages and the default page of a category)
  const input = textInput({ value: el.tags.join(', ') });
  const short = shortURLInput(el.short_url);
  const only = shortOnlyBox(!!el.short_only);
  const img = previewImageInput(el.preview_image ?? '', { candidates: imagesOf(el.html) });
  formDialog(t('element.properties'), [field(t('common.tags'), input, { help: t('common.tags_help') }),
    field(t('shorturl.label'), short, { help: t('shorturl.help', { origin: location.origin }) }), only.el, img.el], t('common.save'), async () => {
    await api.patch(`elements/${el.id}`, { tags: splitTags(input.value), short_url: short.value, short_only: only.input.checked, preview_image: img.value() });
    toast(t('common.saved'));
    navigate(location.pathname, { replace: true });
  });
}

async function makeDefault(el: ElementView): Promise<void> {
  try {
    await api.put(`categories/${el.category_id}`, { default_element_id: el.id });
    toast(t('element.default_set'));
    navigate(location.pathname, { replace: true });
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

function moveElementDialog(el: ElementView): void {
  void (async () => {
    const sel = await categoryPicker('create', el.category_id);
    formDialog(t('element.move_title', { title: el.title }), field(t('category.target'), sel), t('common.move'), async () => {
      await api.post(`elements/${el.id}/move`, { category_id: Number(sel.value) });
      toast(t('element.moved'));
      navigate(`/e/${el.id}`, { replace: true });
    });
  })();
}

async function duplicateElement(el: ElementView): Promise<void> {
  try {
    const n = await api.post<ElementSummary>(`elements/${el.id}/duplicate`, { category_id: 0 });
    toast(t('element.duplicated'));
    navigate(`/e/${n.id}`);
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

async function deleteElement(el: ElementView): Promise<void> {
  if (!(await confirmDialog(t('element.delete_confirm', { title: el.title }), t('common.delete'), true))) return;
  try {
    await api.del(`elements/${el.id}`);
    toast(t('element.deleted'));
    navigate(`/c/${el.category_id}`);
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

// ---------------------------------------------------------------- editing

function jsonEditor(container: HTMLElement, body: unknown, labelId: string): EditorInstance {
  const ta = h('textarea', { class: 'ew-input ew-code', rows: 16, 'aria-labelledby': labelId });
  ta.value = JSON.stringify(body ?? {}, null, 2);
  container.append(ta);
  return {
    getBody: () => JSON.parse(ta.value),
    validate: () => { try { JSON.parse(ta.value); return null; } catch { return t('editor.invalid_json'); } },
  };
}

function mountEditor(container: HTMLElement, type: string, body: unknown, lang: string, labelId: string, elementId?: number): EditorInstance {
  const f = getEditor(type);
  return f ? f(container, body, { lang, type, labelId, elementId }) : jsonEditor(container, body, labelId);
}

/** editorActions are the buttons of the element editors. */
function editorActions(opts: { cancelHref: string; publishLabel: string; onDraft: () => void; onPreview: () => void; onTemplate: () => void; onOptions: () => void }): HTMLElement {
  return h('div', { class: 'ew-form-actions' },
    button(opts.publishLabel, () => undefined, { type: 'submit', variant: 'primary' }),
    button(t('publish.with_options'), opts.onOptions, { variant: 'secondary' }),
    button(t('draft.save'), opts.onDraft, { variant: 'secondary' }),
    button(t('draft.preview'), opts.onPreview, { variant: 'secondary' }),
    button(t('template.save'), opts.onTemplate, { variant: 'secondary' }),
    h('a', { href: opts.cancelHref, class: 'ew-btn ew-btn--subtle' }, t('common.cancel')));
}

export async function editElementView({ params, query, main }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  const lang = query.get('lang') || L();
  let el: ElementView;
  try {
    el = await api.get<ElementView>(`elements/${id}${qs({ lang, edit: 1 })}`);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  if (!el.permissions?.includes('edit')) {
    errorPage(main, new ApiError(403, 'forbidden', t('error.forbidden')));
    return;
  }
  setContext(el.category_id, ctx.path, el.permissions);
  const isNewTranslation = el.fallback;
  const baseRev = isNewTranslation ? 0 : el.rev;
  const title = textInput({ value: el.title, required: true, maxlength: 250, lang });
  const comment = textInput({ maxlength: 500 });
  const labelId = uid('content');
  const editorBox = h('div', { class: 'ew-editor-box', lang });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const notices = h('div');
  let draftId: number | null = null;
  let editor: EditorInstance;
  const current = (): EditorState => ({ type: el.type, lang, title: title.value, body: editor.getBody(), comment: comment.value, baseRev, elementId: id, categoryId: el.category_id });
  const valid = (): boolean => {
    alert.textContent = '';
    const err = editor.validate?.();
    if (err) { alert.textContent = err; return false; }
    if (!title.value.trim()) { alert.textContent = t('error.title_required'); title.focus(); return false; }
    return true;
  };
  const run = async (fn: () => Promise<void>) => { try { await fn(); } catch (e) { alert.textContent = errorText(e); alert.focus?.(); } };
  const form = h('form', { class: 'ew-form ew-edit-form', novalidate: true },
    h('div', { class: 'ew-page-header' }, h('h1', null, t(isNewTranslation ? 'element.translate_title' : 'element.edit_title', { title: el.title, lang: langName(lang) }))),
    isNewTranslation ? h('p', { class: 'ew-notice-inline', role: 'note' }, t('element.translation_start', { lang: langName(el.shown_lang) })) : null,
    notices, alert,
    field(t('common.title'), title, { required: true }),
    h('div', { class: 'ew-field' }, h('span', { class: 'ew-label', id: labelId }, t('core.html.content')), editorBox),
    el.original_lang !== lang ? h('details', { class: 'ew-original' }, h('summary', null, t('element.show_original', { lang: langName(el.original_lang) })), h('div', { 'data-original': '1' }, loading())) : null,
    field(t('element.comment'), comment, { help: t('element.comment_help') }),
    editorActions({
      cancelHref: `/e/${id}`, publishLabel: t('publish.now_button'),
      onDraft: () => { if (valid()) void run(async () => { draftId = (await saveDraft(draftId, current())).id; }); },
      onPreview: () => { if (valid()) void run(() => showPreview(current())); },
      onTemplate: () => { if (valid()) saveTemplateDialog(current()); },
      onOptions: () => {
        if (!valid()) return;
        void run(async () => {
          const c = await publishDialog(current(), false);
          if (!c) return;
          const res = await publishVia(draftId, current(), c);
          editor.destroy?.();
          if (!res.scheduled) toast(t('common.saved'));
          navigate(`/e/${id}`);
        });
      },
    }));
  mount(main, form);
  editor = mountEditor(editorBox, el.type, el.body, lang, labelId, el.id);
  void draftNotice(id, lang, (d) => {
    draftId = d.id;
    title.value = d.title;
    comment.value = d.comment;
    editor.destroy?.();
    editorBox.replaceChildren();
    editor = mountEditor(editorBox, el.type, d.body, lang, labelId, el.id);
    toast(t('draft.loaded'));
  }).then((n) => { if (n) notices.append(n); });
  const orig = form.querySelector<HTMLElement>('[data-original]');
  if (orig) {
    api.get<ElementView>(`elements/${id}${qs({ lang: el.original_lang })}`).then((o) => mount(orig, h('h2', null, o.title), trustedHTML(o.html))).catch(() => mount(orig, ''));
  }
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    if (!valid()) return;
    void run(async () => {
      await api.put(`elements/${id}`, { lang, title: title.value, body: editor.getBody(), comment: comment.value, base_rev: baseRev });
      if (draftId) await api.del(`drafts/${draftId}`).catch(() => undefined); // published directly: the draft is obsolete
      editor.destroy?.();
      toast(t('common.saved'));
      navigate(`/e/${id}`);
    });
  });
}

export async function newElementView(rc: RouteCtx): Promise<void> {
  await newOrDraftView(rc, null);
}

/** draftEditView edits the draft of a new element (/d/:id/edit). */
export async function draftEditView(rc: RouteCtx): Promise<void> {
  let d: Draft;
  try {
    d = await api.get<Draft>(`drafts/${rc.params.id}`);
  } catch (e) {
    errorPage(rc.main, e);
    return;
  }
  if (d.element_id) {
    navigate(`/e/${d.element_id}/edit${qs({ lang: d.lang })}`, { replace: true });
    return;
  }
  await newOrDraftView({ ...rc, params: { id: String(d.category_id) } }, d);
}

async function newOrDraftView({ params, query, main }: RouteCtx, draft: Draft | null): Promise<void> {
  const catId = Number(params.id);
  let cat: CategoryResponse;
  try {
    cat = await api.get<CategoryResponse>(`categories/${catId}`);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  setContext(catId, cat.category.path, cat.category.permissions);
  if (!can('create') && !draft) {
    errorPage(main, new ApiError(403, 'forbidden', t('error.forbidden')));
    return;
  }
  let types = state.site.element_types;
  try { types = await api.get<typeof types>('element-types'); } catch { /* use site info */ }
  const typeId = draft?.type ?? (query.get('type') && types.some((x) => x.id === query.get('type')) ? query.get('type')! : (types.find((x) => x.id === 'core.html')?.id ?? types[0]?.id));
  const typeSel = select(types.map((x) => ({ value: x.id, label: typeLabel(x.id) })), typeId);
  if (draft) typeSel.disabled = true;
  typeSel.addEventListener('change', () => navigate(`/c/${catId}/new${qs({ type: typeSel.value, return: query.get('return') ?? undefined })}`, { replace: true }));
  const lang = draft?.lang ?? L();
  const title = textInput({ required: true, maxlength: 250, value: draft?.title ?? '' });
  const tags = textInput({ value: draft?.tags.join(', ') ?? '' });
  const labelId = uid('content');
  const editorBox = h('div', { class: 'ew-editor-box' });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const tplBox = h('div');
  let draftId: number | null = draft?.id ?? null;
  let editor: EditorInstance;
  const ret = query.get('return');
  const back = ret?.startsWith('/') && !ret.startsWith('//') ? ret : `/c/${catId}`;
  const current = (): EditorState => ({ type: typeId, lang, title: title.value, body: editor.getBody(), tags: splitTags(tags.value), categoryId: catId });
  const valid = (): boolean => {
    alert.textContent = '';
    if (!title.value.trim()) { alert.textContent = t('error.title_required'); title.focus(); return false; }
    const err = editor.validate?.();
    if (err) { alert.textContent = err; return false; }
    return true;
  };
  const run = async (fn: () => Promise<void>) => { try { await fn(); } catch (e) { alert.textContent = errorText(e); } };
  const finish = (elementId?: number, scheduled?: string) => {
    editor.destroy?.();
    invalidateTree();
    if (scheduled) { navigate(`/c/${catId}`); return; }
    toast(t('element.created'));
    navigate(ret && ret.startsWith('/') && !ret.startsWith('//') ? ret : `/e/${elementId}`);
  };
  const form = h('form', { class: 'ew-form ew-edit-form', novalidate: true },
    crumbs([...cat.breadcrumbs, cat.category], { title: draft ? t('draft.label') : t('element.new') }),
    h('div', { class: 'ew-page-header' }, h('h1', null, draft ? t('draft.edit_new', { title: draft.title }) : t('element.new_in', { title: pick(cat.category.titles, L(), D()) }))),
    draft?.publish_at ? h('p', { class: 'ew-notice ew-notice--info' }, t('draft.scheduled_for', { date: fmtDate(draft.publish_at, state.lang) }), ' ',
      button(t('draft.unschedule'), async () => { await api.del(`drafts/${draft.id}/schedule`); toast(t('draft.unscheduled')); navigate(location.pathname, { replace: true }); }, { variant: 'link' })) : null,
    alert,
    field(t('element.type'), typeSel),
    tplBox,
    field(t('common.title'), title, { required: true }),
    h('div', { class: 'ew-field' }, h('span', { class: 'ew-label', id: labelId }, t('core.html.content')), editorBox),
    field(t('common.tags'), tags, { help: t('common.tags_help') }),
    editorActions({
      cancelHref: back, publishLabel: t('publish.now_button'),
      onDraft: () => {
        if (!valid()) return;
        void run(async () => {
          const d = await saveDraft(draftId, current());
          if (!draftId) history.replaceState(null, '', `/d/${d.id}/edit`);
          draftId = d.id;
        });
      },
      onPreview: () => { if (valid()) void run(() => showPreview(current())); },
      onTemplate: () => { if (valid()) saveTemplateDialog(current()); },
      onOptions: () => {
        if (!valid()) return;
        void run(async () => {
          const c = await publishDialog(current(), true);
          if (!c) return;
          const res = await publishVia(draftId, current(), c);
          finish(res.elementId, res.scheduled);
        });
      },
    }),
    draft ? h('p', null, button(t('draft.discard'), async () => {
      await api.del(`drafts/${draft.id}`);
      toast(t('draft.discarded'));
      navigate(back);
    }, { variant: 'danger' })) : null);
  mount(main, form);
  const empty = draft?.body ?? types.find((x) => x.id === typeId)?.empty_body ?? {};
  editor = mountEditor(editorBox, typeId, empty, lang, labelId);
  if (!draft) {
    void templatePicker(typeId, (tpl) => {
      if (!title.value.trim()) title.value = tpl.title;
      if (!tags.value.trim()) tags.value = tpl.tags.join(', ');
      editor.destroy?.();
      editorBox.replaceChildren();
      editor = mountEditor(editorBox, typeId, tpl.body, lang, labelId);
    }).then((p) => { if (p) tplBox.append(p); });
  }
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    if (!valid()) return;
    void run(async () => {
      if (draftId) {
        const res = await publishVia(draftId, current(), { categoryId: catId, at: null, notify: true, options: {} });
        finish(res.elementId);
        return;
      }
      const el = await api.post<ElementView>('elements', { category_id: catId, type: typeId, lang, title: title.value, body: editor.getBody(), tags: splitTags(tags.value) });
      finish(el.id);
    });
  });
}

// ---------------------------------------------------------------- history

export async function historyView({ params, query, main }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  const lang = query.get('lang') ?? '';
  let el: ElementView;
  let revs: Revision[];
  try {
    [el, revs] = await Promise.all([api.get<ElementView>(`elements/${id}${qs({ lang: L() })}`), api.get<Revision[]>(`elements/${id}/revisions${qs({ lang })}`)]);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  setContext(el.category_id, ctx.path, el.permissions);
  const canEdit = el.permissions?.includes('edit');
  const filter = select([{ value: '', label: t('history.all_languages') }, ...state.site.languages.map((l) => ({ value: l, label: langName(l) }))], lang);
  filter.addEventListener('change', () => navigate(`/e/${id}/history${qs({ lang: filter.value })}`, { replace: true }));
  const latest = new Map<string, number>();
  for (const r of revs) latest.set(r.lang, Math.max(latest.get(r.lang) ?? 0, r.rev));
  mount(main,
    breadcrumbs([{ title: t('nav.home'), href: '/' }, { title: el.title, href: `/e/${id}` }, { title: t('element.history') }]),
    pageHeader(t('history.title', { title: el.title })),
    field(t('common.language'), filter, { inline: true }),
    h('table', { class: 'ew-table' },
      h('caption', { class: 'sr-only' }, t('history.title', { title: el.title })),
      h('thead', null, h('tr', null, ...[t('common.language'), t('common.revision'), t('common.date'), t('common.author'), t('element.comment'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...revs.map((r) => h('tr', null,
        h('td', null, langName(r.lang)),
        h('th', { scope: 'row' }, String(r.rev), latest.get(r.lang) === r.rev ? h('span', { class: 'ew-badge ew-badge--ok' }, ' ', t('history.current')) : null),
        h('td', null, fmtDate(r.created_at, L())),
        h('td', null, r.author || '–'),
        h('td', null, r.comment || (r.restored_from ? t('history.restored_from', { rev: r.restored_from }) : '')),
        h('td', { class: 'ew-actions' },
          button(t('common.view'), () => void showRevision(id, r), { variant: 'link', ariaLabel: t('history.view_rev', { rev: r.rev, lang: langName(r.lang) }) }),
          canEdit && latest.get(r.lang) !== r.rev ? button(t('history.restore'), () => void restoreRevision(id, r), { variant: 'link', ariaLabel: t('history.restore_rev', { rev: r.rev, lang: langName(r.lang) }) }) : null))))));
}

async function showRevision(id: number, r: Revision): Promise<void> {
  try {
    const full = await api.get<Revision>(`elements/${id}/revisions/${r.lang}/${r.rev}`);
    openDialog(t('history.rev_title', { rev: r.rev, lang: langName(r.lang) }), h('div', { lang: r.lang }, h('h3', null, full.title), trustedHTML(full.html ?? '')), [], { wide: true });
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

async function restoreRevision(id: number, r: Revision): Promise<void> {
  if (!(await confirmDialog(t('history.restore_confirm', { rev: r.rev, lang: langName(r.lang) }), t('history.restore')))) return;
  try {
    await api.post(`elements/${id}/restore`, { lang: r.lang, rev: r.rev });
    toast(t('history.restored'));
    navigate(location.pathname + location.search, { replace: true });
  } catch (e) {
    toast(errorText(e), 'error');
  }
}
