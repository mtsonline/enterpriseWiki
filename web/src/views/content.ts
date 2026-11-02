// Category and element views.
import { api, ApiError, qs } from '../core/api';
import { h, mount, trustedHTML, fmtDate, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { isAdmin, state } from '../core/state';
import type { Category, ElementSummary, ElementView, Revision } from '../core/types';
import { toast } from '../core/announce';
import { button, confirmDialog, errorText, field, formDialog, langFields, loading, openDialog, pageHeader, textInput, select } from '../components/ui';
import { breadcrumbs, can, categoryPicker, ctx, invalidateTree, langName, setContext } from '../components/widgets';
import { watchButton } from '../components/watch';
import { feedButton } from '../components/feeds';
import { getEditor, renderElementExtensions, typeLabel, type EditorInstance } from '../sdk';

const L = () => state.lang;
const D = () => state.site.default_language;

/** Shows an error page for API errors (401 offers login). */
export function errorPage(main: HTMLElement, e: unknown): void {
  if (e instanceof ApiError && e.status === 401) {
    const next = encodeURIComponent(location.pathname + location.search);
    mount(main, h('h1', null, t('error.login_required')), h('p', null, t('error.login_required_text')),
      h('a', { href: `/login?next=${next}`, class: 'ew-btn ew-btn--primary' }, t('auth.login')));
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

export async function homeView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
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
      pageHeader(state.site.name, isAdmin() ? button(t('category.new_root'), () => newCategoryDialog(0), { variant: 'primary' }) : null),
      roots.length
        ? h('ul', { class: 'ew-cards' }, ...roots.map((c, i) => h('li', { class: 'ew-card' },
          h('h2', null, h('a', { href: `/c/${c.id}` }, pick(c.titles, L(), D()))),
          h('p', null, pick(c.descriptions, L(), D())),
          state.editMode && isAdmin() ? reorderButtons(0, roots, i) : null)))
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
    errorPage(main, e);
    return;
  }
  const c = data.category;
  setContext(c.id, c.path, c.permissions);
  const title = pick(c.titles, L(), D());
  const edit = state.editMode;
  const actions: HTMLElement[] = [];
  if (edit) {
    if (can('create')) {
      actions.push(h('a', { href: `/c/${c.id}/new`, class: 'ew-btn ew-btn--primary' }, t('element.new')));
      actions.push(button(t('category.new_sub'), () => newCategoryDialog(c.id), { variant: 'secondary' }));
    }
    if (can('edit')) actions.push(button(t('category.edit'), () => editCategoryDialog(c), { variant: 'secondary' }));
    if (can('edit') || can('manage')) actions.push(h('a', { href: `/c/${c.id}/settings`, class: 'ew-btn ew-btn--secondary' }, t('category.settings')));
    if (can('delete')) {
      actions.push(button(t('common.move'), () => moveCategoryDialog(c), { variant: 'secondary' }));
      actions.push(button(t('common.delete'), () => void deleteCategory(c), { variant: 'danger' }));
    }
    actions.push(button(t('common.duplicate'), () => duplicateCategoryDialog(c), { variant: 'secondary' }));
  }
  const watchC = watchButton('category', c.id, title);
  if (watchC) actions.push(watchC);
  if (c.feed && c.feed !== 'off') actions.push(feedButton(`/api/v1/feeds/categories/${c.id}`, title));
  const view = h('article', { class: 'ew-category' },
    crumbs(data.breadcrumbs),
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
          button(t('common.tags'), () => tagsDialog(el), { variant: 'secondary' }),
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
        edit && can('edit') ? reorderButtons(c.id, subs, i) : null)))));
  }
  if (!c.default_element_id && !data.elements.length && !subs.length) {
    view.append(h('p', { class: 'ew-muted' }, can('create') ? t('category.empty_editor') : t('category.empty')));
  }
  mount(main, view);
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
    const parentSel = await categoryPicker('create', parent, { allowRoot: true });
    const titles = langFields(t('common.title'), {}, { required: true });
    const descs = langFields(t('common.description'), {}, { multiline: true });
    const tags = textInput({ name: 'tags' });
    formDialog(t('category.new'), [field(t('category.parent'), parentSel), titles.el, descs.el, field(t('common.tags'), tags, { help: t('common.tags_help') })], t('common.create'), async () => {
      const c = await api.post<Category>('categories', {
        parent_id: Number(parentSel.value) || null, titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value),
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
  formDialog(t('category.edit_title', { title: pick(c.titles, L(), D()) }), [titles.el, descs.el, field(t('common.tags'), tags, { help: t('common.tags_help') }),
    h('p', { class: 'ew-help' }, t('category.edit_more'), ' ', h('a', { href: `/c/${c.id}/settings` }, t('category.settings')))], t('common.save'), async () => {
    await api.put(`categories/${c.id}`, { titles: titles.get(), descriptions: descs.get(), tags: splitTags(tags.value) });
    invalidateTree();
    toast(t('common.saved'));
    navigate(location.pathname, { replace: true });
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
    h('p', null, h('progress', { max: '100', value: String(el.translated_percent), 'aria-label': t('translation.progress') }), ' ',
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
    errorPage(main, e);
    return;
  }
  setContext(el.category_id, cat.category.path, el.permissions);
  const edit = state.editMode;
  const actions: HTMLElement[] = [];
  if (edit) {
    if (can('edit')) actions.push(h('a', { href: `/e/${el.id}/edit?lang=${L()}`, class: 'ew-btn ew-btn--primary' }, t('common.edit')));
    actions.push(h('a', { href: `/e/${el.id}/history`, class: 'ew-btn ew-btn--secondary' }, t('element.history')));
    if (can('edit')) actions.push(button(t('common.tags'), () => tagsDialog(el), { variant: 'secondary' }));
    if (can('edit') && !el.is_default) actions.push(button(t('element.make_default'), () => void makeDefault(el), { variant: 'secondary' }));
    if (can('delete')) actions.push(button(t('common.move'), () => moveElementDialog(el), { variant: 'secondary' }));
    actions.push(button(t('common.duplicate'), () => void duplicateElement(el), { variant: 'secondary' }));
    if (can('delete')) actions.push(button(t('common.delete'), () => void deleteElement(el), { variant: 'danger' }));
  }
  const watchE = watchButton('element', el.id, el.title);
  if (watchE) actions.push(watchE);
  const extensions = h('div', { class: 'ew-element-extensions', lang: state.lang });
  mount(main, h('article', { class: 'ew-element', lang: el.shown_lang },
    crumbs([...cat.breadcrumbs, cat.category], { title: el.title }),
    h('div', { class: 'ew-page-header' }, h('h1', null, el.title),
      actions.length ? h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('common.actions') }, ...actions) : null),
    fallbackNotice(el),
    edit ? translationPanel(el) : null,
    trustedHTML(el.html),
    h('footer', { class: 'ew-meta' },
      tagList(el.tags),
      h('p', null, t('element.updated_by', { date: fmtDate(el.updated_at, L()), user: el.updated_by || '–' }), ' · ', t('element.revision', { rev: el.rev }), ' · ',
        h('a', { href: `/e/${el.id}/history` }, t('element.history')))),
    extensions));
  void renderElementExtensions(extensions, el);
}

function tagsDialog(el: ElementView): void {
  // used on element pages and for the default page of a category
  const input = textInput({ value: el.tags.join(', ') });
  formDialog(t('common.tags'), field(t('common.tags'), input, { help: t('common.tags_help') }), t('common.save'), async () => {
    await api.patch(`elements/${el.id}`, { tags: splitTags(input.value) });
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
  const form = h('form', { class: 'ew-form ew-edit-form', novalidate: true },
    h('div', { class: 'ew-page-header' }, h('h1', null, t(isNewTranslation ? 'element.translate_title' : 'element.edit_title', { title: el.title, lang: langName(lang) }))),
    isNewTranslation ? h('p', { class: 'ew-notice-inline', role: 'note' }, t('element.translation_start', { lang: langName(el.shown_lang) })) : null,
    alert,
    field(t('common.title'), title, { required: true }),
    h('div', { class: 'ew-field' }, h('span', { class: 'ew-label', id: labelId }, t('core.html.content')), editorBox),
    el.original_lang !== lang ? h('details', { class: 'ew-original' }, h('summary', null, t('element.show_original', { lang: langName(el.original_lang) })), h('div', { 'data-original': '1' }, loading())) : null,
    field(t('element.comment'), comment, { help: t('element.comment_help') }),
    h('div', { class: 'ew-form-actions' },
      button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' }),
      h('a', { href: `/e/${id}`, class: 'ew-btn ew-btn--secondary' }, t('common.cancel'))));
  mount(main, form);
  const editor = mountEditor(editorBox, el.type, el.body, lang, labelId, el.id);
  const orig = form.querySelector<HTMLElement>('[data-original]');
  if (orig) {
    api.get<ElementView>(`elements/${id}${qs({ lang: el.original_lang })}`).then((o) => mount(orig, h('h2', null, o.title), trustedHTML(o.html))).catch(() => mount(orig, ''));
  }
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const err = editor.validate?.();
    if (err) {
      alert.textContent = err;
      return;
    }
    if (!title.value.trim()) {
      alert.textContent = t('error.title_required');
      title.focus();
      return;
    }
    try {
      await api.put(`elements/${id}`, { lang, title: title.value, body: editor.getBody(), comment: comment.value, base_rev: baseRev });
      editor.destroy?.();
      toast(t('common.saved'));
      navigate(`/e/${id}`);
    } catch (e) {
      alert.textContent = errorText(e);
      alert.focus?.();
    }
  });
}

export async function newElementView({ params, query, main }: RouteCtx): Promise<void> {
  const catId = Number(params.id);
  let cat: CategoryResponse;
  try {
    cat = await api.get<CategoryResponse>(`categories/${catId}`);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  setContext(catId, cat.category.path, cat.category.permissions);
  if (!can('create')) {
    errorPage(main, new ApiError(403, 'forbidden', t('error.forbidden')));
    return;
  }
  let types = state.site.element_types;
  try { types = await api.get<typeof types>('element-types'); } catch { /* use site info */ }
  const typeId = query.get('type') && types.some((x) => x.id === query.get('type')) ? query.get('type')! : (types.find((x) => x.id === 'core.html')?.id ?? types[0]?.id);
  const typeSel = select(types.map((x) => ({ value: x.id, label: typeLabel(x.id) })), typeId);
  typeSel.addEventListener('change', () => navigate(`/c/${catId}/new${qs({ type: typeSel.value, return: query.get('return') ?? undefined })}`, { replace: true }));
  const title = textInput({ required: true, maxlength: 250 });
  const tags = textInput();
  const labelId = uid('content');
  const editorBox = h('div', { class: 'ew-editor-box' });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form ew-edit-form', novalidate: true },
    crumbs([...cat.breadcrumbs, cat.category], { title: t('element.new') }),
    h('div', { class: 'ew-page-header' }, h('h1', null, t('element.new_in', { title: pick(cat.category.titles, L(), D()) }))),
    alert,
    field(t('element.type'), typeSel),
    field(t('common.title'), title, { required: true }),
    h('div', { class: 'ew-field' }, h('span', { class: 'ew-label', id: labelId }, t('core.html.content')), editorBox),
    field(t('common.tags'), tags, { help: t('common.tags_help') }),
    h('div', { class: 'ew-form-actions' },
      button(t('common.create'), () => undefined, { type: 'submit', variant: 'primary' }),
      h('a', { href: query.get('return')?.startsWith('/') && !query.get('return')!.startsWith('//') ? query.get('return')! : `/c/${catId}`, class: 'ew-btn ew-btn--secondary' }, t('common.cancel'))));
  mount(main, form);
  const empty = types.find((x) => x.id === typeId)?.empty_body ?? {};
  const editor = mountEditor(editorBox, typeId, empty, L(), labelId);
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    if (!title.value.trim()) {
      alert.textContent = t('error.title_required');
      title.focus();
      return;
    }
    const err = editor.validate?.();
    if (err) {
      alert.textContent = err;
      return;
    }
    try {
      const el = await api.post<ElementView>('elements', { category_id: catId, type: typeId, lang: L(), title: title.value, body: editor.getBody(), tags: splitTags(tags.value) });
      editor.destroy?.();
      invalidateTree();
      toast(t('element.created'));
      const ret = query.get('return');
      navigate(ret && ret.startsWith('/') && !ret.startsWith('//') ? ret : `/e/${el.id}`);
    } catch (e) {
      alert.textContent = errorText(e);
    }
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
