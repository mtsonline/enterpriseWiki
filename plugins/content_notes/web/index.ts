// Shared notes: editors write directly in the page view, live together
// with the others; the first editor (leader) saves automatically.
import { api, ApiError, errorText, getEditor, h, loadMarkdownEditor, registerEditor, registerWidget, state, t, uid, type ElementView } from '@ewiki/sdk';
import './style.css';

const QUIET = 4000; // save this long after the last change …
const EVERY = 30000; // … but at most this often while typing goes on

async function notesWidget(root: HTMLElement): Promise<void> {
  const id = Number(root.dataset.element);
  if (!id || !state.user) return;
  let el: ElementView;
  try {
    el = await api.get<ElementView>(`elements/${id}?lang=${encodeURIComponent(state.lang)}&edit=1`);
  } catch {
    return; // readers keep the rendered notes
  }
  if (!el.permissions?.includes('edit')) return;
  if (el.fallback) {
    root.prepend(h('p', { class: 'ew-help' }, t('content.notes.other_lang')));
    return;
  }
  const body = (el.body ?? {}) as { format?: string; source?: string };
  if ((body.format ?? 'markdown') !== 'markdown') return; // the conversion job has not run yet
  const mod = await loadMarkdownEditor();
  if (!root.isConnected) return;
  const labelId = uid('notes');
  const status = h('p', { class: 'ewn-status', role: 'status' }, '');
  const box = h('div', { class: 'ewn-editor' });
  root.replaceChildren(h('p', { class: 'ewn-label', id: labelId }, t('content.notes.editor')), h('p', { class: 'ew-help' }, t('content.notes.hint')), box, status);

  let baseRev = el.rev;
  let dirty = false;
  let saving = false;
  let lastSave = 0;
  let timer = 0;
  const time = () => new Date().toLocaleTimeString(state.lang, { hour: '2-digit', minute: '2-digit' });

  const ed = mod.createMarkdownEditor(box, {
    markdown: body.source ?? '', lang: el.lang, labelId,
    collab: state.site.collaboration === false ? undefined : {
      elementId: id, userId: state.user.id,
      onSaved: (rev, by) => {
        baseRev = rev;
        if (!ed.live().leader) dirty = false;
        status.textContent = by ? t('content.notes.saved_by', { name: by.name, time: time() }) : t('content.notes.saved', { time: time() });
      },
    },
    onChange: () => { dirty = true; schedule(); },
  });

  // only one editor saves: the leader (or everybody without live editing)
  const mine = () => { const l = ed.live(); return !l.on || l.leader; };
  function schedule() {
    clearTimeout(timer);
    const wait = Math.max(QUIET, lastSave + EVERY - Date.now());
    timer = window.setTimeout(() => void save(), wait);
  }
  async function save(final = false): Promise<void> {
    if (!dirty || saving) return;
    if (!mine()) { schedule(); return; } // the leader saves; check again later (it may leave)
    saving = true;
    dirty = false;
    status.textContent = t('content.notes.saving');
    const payload = { lang: el.lang, title: el.title, body: { format: 'markdown', source: ed.getMarkdown() }, comment: t('content.notes.autosave') };
    try {
      let res: ElementView;
      try {
        res = await api.put<ElementView>(`elements/${id}`, { ...payload, base_rev: baseRev });
      } catch (e) {
        if (!(e instanceof ApiError) || e.status !== 409) throw e;
        // saved meanwhile (e.g. in the full editor): the shared state wins
        const cur = await api.get<ElementView>(`elements/${id}?lang=${encodeURIComponent(el.lang)}`);
        res = await api.put<ElementView>(`elements/${id}`, { ...payload, base_rev: cur.rev });
      }
      baseRev = res.rev;
      lastSave = Date.now();
      ed.saved(res.rev);
      status.textContent = t('content.notes.saved', { time: time() });
    } catch (e) {
      dirty = true;
      status.textContent = t('content.notes.failed', { error: errorText(e) });
      if (!final) timer = window.setTimeout(() => void save(), 10000);
    } finally {
      saving = false;
    }
  }
  // leaving the page: save what is left, then end live editing
  const onHide = () => { void save(true); };
  window.addEventListener('pagehide', onHide);
  const watch = window.setInterval(() => {
    if (root.isConnected) return;
    clearInterval(watch);
    clearTimeout(timer);
    window.removeEventListener('pagehide', onHide);
    void save(true).finally(() => ed.destroy());
  }, 1000);
}

export function register(): void {
  // the full editor of the notes is the one of core.html
  registerEditor('content.notes', (container, body, ctx) => {
    const base = getEditor('core.html');
    if (!base) throw new Error('core.html editor missing');
    return base(container, body, ctx);
  });
  registerWidget('notes', notesWidget);
}
