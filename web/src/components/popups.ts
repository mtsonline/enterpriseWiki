// Pop-up notifications: a notification marked "pop-up" (for example the end
// of a workflow) appears once as a dialog when a page loads, one after the
// other; it is marked read when shown and stays in the list of the bell.
// The text is plain text (the server never sends HTML for it).
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { button, openDialog } from './ui';

interface Popup { id: number; title: string; body: string; link: string }

let busy = false;
let last = 0;

/** Shows the unread pop-ups of the person (on the first page load, then at
 * most once a minute when the page changes). */
export async function showPopups(): Promise<void> {
  if (!state.user || busy || Date.now() - last < 60_000) return;
  busy = true;
  last = Date.now();
  try {
    const res = await api.poll<{ items: Popup[] }>('me/inbox/popups');
    for (const p of res.items ?? []) await showOne(p);
  } catch {
    // no pop-ups now; the bell still shows the notifications
  } finally {
    busy = false;
  }
}

function showOne(p: Popup): Promise<void> {
  // shown once: read at once, also if the page is closed with the dialog open
  void api.post('me/inbox/read', { ids: [p.id] }).catch(() => undefined);
  return new Promise((resolve) => {
    const link = p.link.startsWith('/') && !p.link.startsWith('//') ? p.link : '';
    const actions = [button(t('popup.ok'), () => d.close(), { variant: 'primary' })];
    if (link) actions.push(button(t('popup.open'), () => { d.close(); navigate(link); }));
    const d = openDialog(p.title, [
      ...p.body.split('\n').filter((line) => line.trim() !== '').map((line) => h('p', null, line)),
      h('p', { class: 'ew-help' }, t('popup.note')),
    ], actions, { onClose: () => resolve() });
    d.dialog.classList.add('ew-popup');
  });
}
