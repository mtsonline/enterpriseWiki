// The page of a person (/people/:id): the business card as the viewer may
// see it (the server decides every field), "Send e-mail" when the address
// may be shown, and the entries of plugins (registerPersonAction).
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import type { RouteCtx } from '../core/router';
import { toast } from '../core/announce';
import { button, pageHeader } from '../components/ui';
import { setContext } from '../components/widgets';
import { personActions, type Person } from '../components/person';
import { errorPage } from './content';

const FIELDS = ['job_title', 'department', 'phone', 'mobile', 'office'];

function initials(name: string): string {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w.slice(0, 1)).join('').toUpperCase() || '?';
}

function tel(v: string): string {
  return v.replace(/[^0-9+]/g, '');
}

export async function personView({ params, main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  let p: Person;
  try {
    p = await api.get<Person>(`people/${Number(params.id)}`);
  } catch (e) {
    errorPage(main, e);
    return;
  }
  const name = p.display_name || p.handle;
  const photo = p.card.photo
    // the size is reserved (no layout shift): 8rem in the stylesheet, 128 px here
    ? h('img', { class: 'ew-person-photo', src: `/api/v1/files/${p.card.photo}`, alt: t('profile.photo_of', { name }), width: '128', height: '128' })
    : h('span', { class: 'ew-person-initials', 'aria-hidden': 'true' }, initials(name));
  const rows: HTMLElement[] = [];
  const row = (label: string, value: Node | string) => rows.push(h('dt', null, label), h('dd', null, value));
  if (p.handle) row(t('person.handle'), p.handle);
  for (const f of FIELDS) {
    const v = p.card[f];
    if (!v) continue;
    row(t(`profile.card_${f}`), f === 'phone' || f === 'mobile' ? h('a', { href: `tel:${tel(v)}` }, v) : v);
  }
  if (p.email) row(t('person.email'), h('a', { href: `mailto:${p.email}` }, p.email));
  const actions = h('div', { class: 'ew-person-actions' },
    p.email ? h('a', { class: 'ew-btn ew-btn--secondary', href: `mailto:${p.email}` }, t('person.send_email')) : null,
    p.self ? h('a', { class: 'ew-btn ew-btn--secondary', href: '/profile' }, t('person.edit_own')) : null,
    ...personActions(p).map((a) => button(a.label(p), async () => {
      try { await a.run(p); } catch { toast(t('person.action_failed'), 'error'); }
    }, { variant: 'secondary' })));
  mount(main,
    pageHeader(name),
    h('section', { class: 'ew-person-card', 'aria-label': t('person.card') },
      photo,
      h('div', null,
        rows.length ? h('dl', { class: 'ew-dl' }, ...rows) : null,
        p.card.about ? h('div', null, h('h2', null, t('profile.card_about')), h('p', { class: 'ew-person-about' }, p.card.about)) : null,
        actions.childElementCount ? actions : null)));
}
