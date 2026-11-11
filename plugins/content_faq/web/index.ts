// FAQ element: editor and the "faq" widget (instant search, expand/collapse
// all, quick adding of questions and sections, feed, deep links #q-ID).
import {
  announce, api, button, categorySelect, checkbox, confirmDialog, errorText, feedButton, field, formDialog, h, langFields, mount, pick,
  registerEditor, registerWidget, rerender, select, state, t, textArea, textInput, toast, type EditorInstance, sourcesEditor,
} from '@ewiki/sdk';
import './style.css';

const BASE = '/api/v1/x/content.faq';

interface Body { category_id: number; recursive: boolean; more_sources?: string; intro: { format: string; source: string }; feed: boolean; open_sections: boolean; numbering: boolean; search: boolean }
interface Cat { id: number; depth: number; titles: Record<string, string>; can_create: boolean }
interface Info { categories: Cat[]; can_create: boolean; can_manage: boolean; missing: boolean }

const L = () => state.lang;
const D = () => state.site.default_language;

// ---------------------------------------------------------------- editor

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const b = { ...(raw as Body) };
  b.intro = b.intro ?? { format: 'markdown', source: '' };
  const err = h('p', { class: 'ew-field-error', role: 'alert' });
  const catBox = h('div');
  let cats: Awaited<ReturnType<typeof categorySelect>> | null = null;
  void categorySelect({ selected: b.category_id, perm: 'view', noneLabel: t('content.faq.new_source'), allowCreate: true })
    .then((cs) => { cats = cs; catBox.append(cs.field(t('content.faq.category'), { help: t('content.faq.category_help') })); });
  const rec = checkbox(t('content.faq.recursive'), b.recursive);
  const more = sourcesEditor(t('content.faq.more_sources'), b.more_sources ?? '', { help: t('content.faq.more_sources_help') });
  const fmt = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], b.intro.format);
  const intro = textArea({ value: b.intro.source, rows: 4 });
  const open = checkbox(t('content.faq.open_sections'), b.open_sections);
  const num = checkbox(t('content.faq.numbering'), b.numbering);
  const search = checkbox(t('content.faq.search_opt'), b.search);
  const feed = checkbox(t('content.faq.feed'), b.feed);
  mount(container, h('div', { class: 'ewq-editor' },
    h('fieldset', null, h('legend', null, t('content.faq.source')), catBox, rec.el, more.el),
    h('fieldset', null, h('legend', null, t('content.faq.intro')), field(t('content.faq.intro_format'), fmt), field(t('content.faq.intro_text'), intro, { help: t('content.faq.intro_help') })),
    h('fieldset', null, h('legend', null, t('content.faq.display')), open.el, num.el, search.el, feed.el),
    err));
  return {
    getBody: () => ({
      category_id: cats?.value() ?? b.category_id, recursive: rec.input.checked, more_sources: more.get(), intro: { format: fmt.value, source: intro.value },
      feed: feed.input.checked, open_sections: open.input.checked, numbering: num.input.checked, search: search.input.checked,
    }),
  };
}

// ---------------------------------------------------------------- widget

const catOptions = (cats: Cat[]) => cats.filter((c) => c.can_create)
  .map((c) => ({ value: String(c.id), label: c.depth === 0 ? t('content.faq.top') : `${'— '.repeat(c.depth)}${pick(c.titles, L(), D())}` }));

function newQuestion(id: number, info: Info, preselect?: number): void {
  const sec = select(catOptions(info.categories), preselect ? String(preselect) : undefined);
  const q = textInput({ required: true, maxlength: 250 });
  const a = textArea({ rows: 8 });
  formDialog(t('content.faq.new_question'), [
    info.categories.length > 1 ? field(t('content.faq.section'), sec) : null,
    field(t('content.faq.question'), q, { required: true }),
    field(t('content.faq.answer'), a, { help: t('content.faq.answer_help') }),
    h('p', { class: 'ew-help' }, h('a', { href: `/c/${sec.value}/new?type=core.html&return=${encodeURIComponent(`/e/${id}`)}` }, t('content.faq.open_editor'))),
  ], t('common.create'), async () => {
    await api.post(`${BASE}/elements/${id}/questions`, { category_id: Number(sec.value), lang: L(), title: q.value, body: { format: 'markdown', source: a.value } });
    toast(t('content.faq.created'));
    await rerender();
  }, { wide: true });
}

function newSection(id: number, info: Info): void {
  const parent = select(catOptions(info.categories));
  const titles = langFields(t('common.title'), {}, { required: true });
  formDialog(t('content.faq.new_section'), [field(t('content.faq.parent_section'), parent), titles.el], t('common.create'), async () => {
    await api.post(`${BASE}/elements/${id}/categories`, { parent_id: Number(parent.value), titles: titles.get() });
    toast(t('content.faq.section_created'));
    await rerender();
  }, { wide: true });
}

/** Quick editing of a question: title and answer (Markdown) in a dialog. */
async function editQuestion(qid: number): Promise<void> {
  let el: { title: string; rev: number; type: string; body: { format?: string; source?: string } };
  try {
    el = await api.get(`/api/v1/elements/${qid}?lang=${L()}&edit=1`);
  } catch (e) {
    toast(errorText(e), 'error');
    return;
  }
  if (el.type !== 'core.html' || (el.body.format ?? 'markdown') !== 'markdown') {
    location.assign(`/e/${qid}/edit?lang=${L()}`); // other page types: the full editor
    return;
  }
  const q = textInput({ required: true, maxlength: 250, value: el.title });
  const a = textArea({ rows: 10, value: el.body.source ?? '' });
  formDialog(t('content.faq.edit_question_title'), [
    field(t('content.faq.question'), q, { required: true }),
    field(t('content.faq.answer'), a, { help: t('content.faq.answer_help') }),
    h('p', { class: 'ew-help' }, h('a', { href: `/e/${qid}/edit?lang=${L()}` }, t('content.faq.open_editor'))),
  ], t('common.save'), async () => {
    await api.put(`/api/v1/elements/${qid}`, { lang: L(), title: q.value, body: { format: 'markdown', source: a.value }, base_rev: el.rev });
    toast(t('common.saved'));
    await rerender();
  }, { wide: true });
}

async function deleteQuestion(qid: number, title: string): Promise<void> {
  if (!await confirmDialog(t('content.faq.delete_confirm', { title }), t('common.delete'), true)) return;
  try {
    await api.del(`/api/v1/elements/${qid}`);
    toast(t('content.faq.deleted'));
    await rerender();
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

function editSection(cat: Cat): void {
  const titles = langFields(t('common.title'), cat.titles, { required: true });
  formDialog(t('content.faq.edit_section'), [titles.el], t('common.save'), async () => {
    await api.put(`/api/v1/categories/${cat.id}`, { titles: titles.get() });
    toast(t('common.saved'));
    await rerender();
  }, { wide: true });
}

async function deleteSection(cat: Cat): Promise<void> {
  const title = pick(cat.titles, L(), D());
  if (!await confirmDialog(t('content.faq.delete_section_confirm', { title }), t('common.delete'), true)) return;
  try {
    await api.del(`/api/v1/categories/${cat.id}`);
    toast(t('content.faq.section_deleted'));
    await rerender();
  } catch (e) {
    toast(errorText(e), 'error');
  }
}

const norm = (s: string) => s.toLocaleLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');

async function faqWidget(root: HTMLElement): Promise<void> {
  const id = Number(root.dataset.element);
  let data: { config: Body; info: Info };
  try { data = await api.get(`${BASE}/elements/${id}/questions?lang=${L()}&q=`); } catch { return; }
  if (data.info.missing) return;
  const c = data.config;
  const body = root.querySelector<HTMLElement>('.ewq-body');
  if (!body) return;
  const questions = Array.from(body.querySelectorAll<HTMLDetailsElement>('details.ewq-q'));
  const sections = Array.from(body.querySelectorAll<HTMLDetailsElement>('details.ewq-section'));
  const status = h('p', { class: 'ewq-status', role: 'status' });

  // instant filter over the rendered questions and answers
  const texts = new Map(questions.map((d) => [d, norm(d.textContent ?? '')]));
  const remembered = new Map<HTMLDetailsElement, boolean>();
  const filter = (term: string) => {
    const words = norm(term).split(/\s+/).filter(Boolean);
    if (!words.length) {
      questions.forEach((d) => { d.hidden = false; if (remembered.has(d)) d.open = remembered.get(d)!; });
      sections.forEach((s) => { s.hidden = false; if (remembered.has(s)) s.open = remembered.get(s)!; });
      remembered.clear();
      status.textContent = '';
      return;
    }
    if (!remembered.size) [...questions, ...sections].forEach((d) => remembered.set(d, d.open));
    let n = 0;
    for (const d of questions) {
      const hit = words.every((w) => texts.get(d)!.includes(w));
      d.hidden = !hit;
      d.open = hit && words.length > 0 && n < 3; // open the first matches
      if (hit) n++;
    }
    for (const s of sections) {
      const any = Array.from(s.querySelectorAll<HTMLDetailsElement>('details.ewq-q')).some((d) => !d.hidden);
      s.hidden = !any;
      s.open = any;
    }
    status.textContent = n ? t('content.faq.matches', { n }) : t('content.faq.no_matches');
  };
  let timer = 0;
  const q = textInput({ type: 'search', placeholder: t('content.faq.search_placeholder') });
  q.setAttribute('aria-label', t('content.faq.search'));
  q.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => { filter(q.value); if (q.value) announce(status.textContent ?? ''); }, 200); });
  const setAll = (open: boolean) => [...sections, ...questions].forEach((d) => { if (!d.hidden) d.open = open; });

  const title = document.querySelector('h1')?.textContent ?? 'FAQ';
  const toolbar = h('div', { class: 'ewq-toolbar' },
    c.search && questions.length ? h('div', { class: 'ewq-search', role: 'search' }, q) : null,
    h('div', { class: 'ew-actions' },
      questions.length ? button(t('content.faq.expand_all'), () => setAll(true), { variant: 'secondary' }) : null,
      questions.length ? button(t('content.faq.collapse_all'), () => setAll(false), { variant: 'secondary' }) : null,
      data.info.can_create ? button(t('content.faq.new_question'), () => newQuestion(id, data.info), { variant: 'primary' }) : null,
      data.info.can_manage && c.recursive ? button(t('content.faq.new_section'), () => newSection(id, data.info), { variant: 'secondary' }) : null,
      c.feed ? feedButton(`${BASE}/elements/${id}/feed`, title) : null));
  body.before(toolbar, status);

  // edit and delete in edit mode (the server checks the permissions)
  if (state.editMode && data.info.can_manage) {
    for (const sec of sections) {
      const cat = data.info.categories.find((x) => x.id === Number(sec.dataset.id));
      if (!cat || cat.depth === 0) continue;
      const name = pick(cat.titles, L(), D());
      sec.querySelector(':scope > .ewq-section-body')?.prepend(h('div', { class: 'ewq-edit ew-actions' },
        button(t('content.faq.edit_section'), () => editSection(cat), { variant: 'secondary', ariaLabel: `${t('content.faq.edit_section')}: ${name}` }),
        button(t('content.faq.delete_section'), () => void deleteSection(cat), { variant: 'danger', ariaLabel: `${t('content.faq.delete_section')}: ${name}` })));
    }
  }
  if (state.editMode && data.info.can_create) {
    for (const d of questions) {
      const qid = Number(d.dataset.id);
      const name = d.querySelector('.ewq-question')?.textContent?.replace(/^[\d.]+\s*/, '') ?? '';
      d.querySelector('.ewq-answer')?.append(h('div', { class: 'ewq-edit ew-actions' },
        button(t('content.faq.edit_question'), () => void editQuestion(qid), { variant: 'secondary', ariaLabel: `${t('content.faq.edit_question')}: ${name}` }),
        button(t('common.delete'), () => void deleteQuestion(qid, name), { variant: 'danger', ariaLabel: `${t('common.delete')}: ${name}` })));
    }
  }
  // deep link: /e/ID#q-123 opens the question
  const target = location.hash.startsWith('#q-') ? body.querySelector<HTMLDetailsElement>(location.hash) : null;
  if (target) {
    for (let el: HTMLElement | null = target; el && el !== body; el = el.parentElement) if (el instanceof HTMLDetailsElement) el.open = true;
    target.scrollIntoView({ block: 'start' });
    target.querySelector('summary')?.focus();
  }
  // remember the open question in the URL (shareable)
  for (const d of questions) {
    d.addEventListener('toggle', () => { if (d.open) history.replaceState(history.state, '', `#q-${d.dataset.id}`); });
  }
}

export function register(): void {
  registerEditor('content.faq', createEditor);
  registerWidget('faq', faqWidget);
}
