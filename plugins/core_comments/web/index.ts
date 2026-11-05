// Comments below element pages.
import { api, ApiError, button, prefetchReactions, reactionBar, confirmDialog, errorText, field, fmtDate, h, registerElementExtension, state, t, textArea, toast, trustedHTML, type ElementView } from '@ewiki/sdk';
import './style.css';

const BASE = 'x/core.comments';

interface Comment {
  id: number; element_id: number; parent_id: number | null; author: string; body: string; html: string;
  created_at: string; updated_at: string; edited: boolean; deleted_at: string | null; can_edit: boolean; can_delete: boolean;
}
interface ListResponse { comments: Comment[]; can_comment: boolean }

function editor(label: string, value: string, submitLabel: string, onSubmit: (body: string) => Promise<void>, onCancel?: () => void): HTMLFormElement {
  const ta = textArea({ value, rows: 4 });
  ta.maxLength = 10000;
  const err = h('p', { class: 'ew-field-error', role: 'alert' });
  const submit = button(submitLabel, () => undefined, { type: 'submit', variant: 'primary' });
  const form = h('form', { class: 'ewcm-form', novalidate: true }, field(label, ta, { required: true }), err,
    h('div', { class: 'ew-actions' }, submit, onCancel ? button(t('common.cancel'), onCancel) : null));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    err.textContent = '';
    if (!ta.value.trim()) { err.textContent = t('core.comments.empty_error'); ta.focus(); return; }
    submit.disabled = true;
    try { await onSubmit(ta.value); } catch (e) { err.textContent = errorText(e); submit.disabled = false; }
  });
  return form;
}

async function section(el: ElementView): Promise<Node | null> {
  let data: ListResponse;
  try {
    data = await api.get<ListResponse>(`${BASE}/elements/${el.id}/comments`);
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 401)) return null;
    throw e;
  }
  const root = h('section', { class: 'ewcm', 'aria-labelledby': 'ewcm-title' });
  const reload = async (focusId?: number) => {
    data = await api.get<ListResponse>(`${BASE}/elements/${el.id}/comments`);
    await prefetchReactions('comment', data.comments.filter((c) => !c.deleted_at).map((c) => c.id));
    draw();
    if (focusId) root.querySelector<HTMLElement>(`#comment-${focusId}`)?.focus();
  };
  const item = (c: Comment): HTMLElement => {
    const body = h('div', { class: 'ewcm-body' });
    const showBody = () => body.replaceChildren(c.deleted_at ? h('p', { class: 'ew-muted' }, t('core.comments.deleted')) : trustedHTML(c.html, 'ewcm-text'));
    showBody();
    const actions = h('div', { class: 'ewcm-actions' });
    if (!c.deleted_at) {
      if (data.can_comment) actions.append(button(t('core.comments.reply'), () => {
        const f = editor(t('core.comments.reply_label', { author: c.author }), '', t('core.comments.reply'), async (b) => {
          const n = await api.post<Comment>(`${BASE}/elements/${el.id}/comments`, { body: b, parent_id: c.id });
          toast(t('core.comments.added'));
          await reload(n.id);
        }, () => { f.remove(); }); 
        art.append(f);
        f.querySelector('textarea')?.focus();
      }, { variant: 'link' }));
      if (c.can_edit) actions.append(button(t('common.edit'), () => {
        const f = editor(t('core.comments.edit_label'), c.body, t('common.save'), async (b) => {
          await api.put(`${BASE}/comments/${c.id}`, { body: b });
          toast(t('core.comments.saved'));
          await reload(c.id);
        }, () => { showBody(); });
        body.replaceChildren(f);
        f.querySelector('textarea')?.focus();
      }, { variant: 'link' }));
      if (c.can_delete) actions.append(button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('core.comments.delete_confirm'), t('common.delete'), true))) return;
        try { await api.del(`${BASE}/comments/${c.id}`); toast(t('core.comments.removed')); await reload(); } catch (e) { toast(errorText(e), 'error'); }
      }, { variant: 'link' }));
    }
    const art = h('article', { class: 'ewcm-item', id: `comment-${c.id}`, tabindex: '-1', 'aria-labelledby': `comment-${c.id}-h` },
      h('header', { class: 'ewcm-head', id: `comment-${c.id}-h` }, h('strong', null, c.author), ' ',
        h('time', { datetime: c.created_at, class: 'ew-muted' }, fmtDate(c.created_at, state.lang)),
        c.edited && !c.deleted_at ? h('span', { class: 'ew-muted' }, ` · ${t('core.comments.edited')}`) : null),
      body, actions, c.deleted_at ? null : reactionBar('comment', c.id, t('core.comments.reaction_target', { author: c.author })));
    return art;
  };
  const draw = () => {
    const top = data.comments.filter((c) => !c.parent_id);
    const count = data.comments.filter((c) => !c.deleted_at).length;
    root.replaceChildren(
      h('h2', { id: 'ewcm-title' }, t('core.comments.title'), ' ', h('span', { class: 'ew-badge' }, String(count))),
      top.length ? h('ol', { class: 'ewcm-list' }, ...top.map((c) => {
        const replies = data.comments.filter((r) => r.parent_id === c.id);
        return h('li', null, item(c), replies.length ? h('ol', { class: 'ewcm-replies', 'aria-label': t('core.comments.replies') }, ...replies.map((r) => h('li', null, item(r)))) : null);
      })) : h('p', { class: 'ew-muted' }, t('core.comments.none')),
      data.can_comment
        ? editor(t('core.comments.new'), '', t('core.comments.add'), async (b) => {
          const n = await api.post<Comment>(`${BASE}/elements/${el.id}/comments`, { body: b });
          toast(t('core.comments.added'));
          await reload(n.id);
        })
        : h('p', { class: 'ew-muted' }, state.user ? t('core.comments.no_permission') : h('a', { href: `/login?next=${encodeURIComponent(location.pathname)}` }, t('core.comments.login'))));
  };
  await prefetchReactions('comment', data.comments.filter((c) => !c.deleted_at).map((c) => c.id));
  draw();
  return root;
}

export function register(): void {
  registerElementExtension({ id: 'core.comments', order: 50, render: section });
}
