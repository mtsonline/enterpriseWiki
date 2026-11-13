// Event calendar: calendar settings editor and the "events" widget with
// month, week and list views, event details (registration, reminders,
// .ics), the event form (series, registration, reminders) and iCal
// subscription.
import {
  announce, api, button, checkbox, confirmDialog, errorText, feedURL, field, formDialog, h, mount, newest, openDialog, qs, registerEditor, registerWidget, select, state, t, textArea, textInput, toast, trustedHTML, uid, type EditorInstance, sourcesEditor, registerSourceKind, registerMacroParamControl, elementSelect,
} from '@ewiki/sdk';
import './style.css';

const BASE = '/api/v1/x/content.events';

interface Cat { id: string; label: string; color: string }
interface Config { intro: { format: string; source: string }; view: View; week_start: number; categories: Cat[]; ical: boolean; list_days: number; no_invitations?: boolean; more_sources?: string }
type View = 'month' | 'week' | 'list';
interface Ev {
  id: number; calendar_id: number; title: string; description: string; description_html: string; location: string; url: string; category: string;
  starts_at: string; ends_at: string; all_day: boolean; tz: string; rrule: string; exdates: string[]; registration: boolean; capacity: number;
  waitlist: boolean; reg_deadline: number; reminders: number[]; author: string; can_edit: boolean; can_manage: boolean; external?: boolean; source?: string;
  overrides?: Record<string, Override>;
}
interface Occ {
  event_id: number; date: string; start: string; end: string; registered: number; waiting: number; my_status?: string; full: boolean; registration_open: boolean;
  // a single changed occurrence of a series
  changed?: boolean; title?: string; location?: string; description_html?: string; original_start?: string;
}
interface Override { title?: string; description?: string; location?: string; starts_at?: string; ends_at?: string }
interface Data { calendar: { id: number; title: string; config: Config; can_create: boolean; signed_in: boolean }; events: Ev[]; occurrences: Occ[]; calendars?: Record<string, string> }

// names of the further sources of the calendar shown (other calendars)
let sourceNames: Record<string, string> = {};
const hostOf = (u: string) => { try { return new URL(u).host.replace(/^www\./, ''); } catch { return u; } };

const L = () => state.lang;
const browserTZ = () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
const pad = (n: number) => String(n).padStart(2, '0');
const ymd = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const hm = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`;
const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
const sameDay = (a: Date, b: Date) => ymd(a) === ymd(b);
const fmt = (d: Date, o: Intl.DateTimeFormatOptions) => d.toLocaleString(L(), o);

// ---------------------------------------------------------------- settings editor

function createEditor(container: HTMLElement, raw: unknown): EditorInstance {
  const c = { ...(raw as Config) };
  c.intro = c.intro ?? { format: 'markdown', source: '' };
  const cats: Cat[] = (c.categories ?? []).map((x) => ({ ...x }));
  const fmtSel = select([{ value: 'markdown', label: 'Markdown' }, { value: 'html', label: 'HTML' }], c.intro.format);
  const intro = textArea({ value: c.intro.source, rows: 3 });
  const view = select((['month', 'week', 'list'] as View[]).map((v) => ({ value: v, label: t(`content.events.view_${v}`) })), c.view);
  const ws = select([{ value: '1', label: t('content.events.monday') }, { value: '0', label: t('content.events.sunday') }], String(c.week_start ?? 1));
  const days = textInput({ type: 'number', value: String(c.list_days || 90) });
  const ical = checkbox(t('content.events.ical_opt'), c.ical !== false);
  const noInv = checkbox(t('content.events.no_invitations'), !!c.no_invitations);
  const more = sourcesEditor(t('content.events.more_sources'), c.more_sources ?? '', { kinds: ['calendar', 'ical'], help: t('content.events.more_sources_help') });
  const list = h('div', { class: 'ewe-cat-rows' });
  const draw = () => {
    mount(list, ...cats.map((k, i) => {
      const label = textInput({ value: k.label });
      label.addEventListener('input', () => { k.label = label.value; });
      const color = h('input', { type: 'color', value: k.color || '#0c66e4', class: 'ewe-color' }) as HTMLInputElement;
      color.addEventListener('input', () => { k.color = color.value; });
      return h('div', { class: 'ewe-cat-row' }, field(`${t('content.events.category')} ${i + 1}`, label), field(t('content.events.color'), color),
        button(t('content.events.remove'), () => { cats.splice(i, 1); draw(); }, { variant: 'link', ariaLabel: `${t('content.events.remove')}: ${t('content.events.category')} ${i + 1}` }));
    }));
  };
  draw();
  mount(container, h('div', { class: 'ewe-editor' },
    h('fieldset', null, h('legend', null, t('content.events.intro')), field(t('content.events.intro_format'), fmtSel), field(t('content.events.intro_text'), intro)),
    h('fieldset', null, h('legend', null, t('content.events.display')), field(t('content.events.default_view'), view), field(t('content.events.week_start'), ws),
      field(t('content.events.list_days'), days, { help: t('content.events.list_days_help') }), ical.el, noInv.el),
    more.el,
    h('fieldset', null, h('legend', null, t('content.events.categories')), h('p', { class: 'ew-help' }, t('content.events.categories_help')), list,
      button(t('content.events.add_category'), () => { cats.push({ id: '', label: '', color: '#0c66e4' }); draw(); list.querySelector<HTMLInputElement>('.ewe-cat-row:last-child input')?.focus(); }, { variant: 'secondary' }))));
  return {
    getBody: () => ({
      intro: { format: fmtSel.value, source: intro.value }, view: view.value, week_start: Number(ws.value), list_days: Number(days.value) || 90,
      ical: ical.input.checked, no_invitations: noInv.input.checked, categories: cats.filter((k) => k.label.trim()), more_sources: more.get(),
    }),
  };
}

// ---------------------------------------------------------------- helpers

/** The event as shown for one occurrence (changes of single occurrences applied). */
function eff(e: Ev, o: Occ): Ev {
  if (!o.changed) return e;
  return { ...e, title: o.title || e.title, location: o.location || e.location, description_html: o.description_html || e.description_html };
}

function catOf(cfg: Config, id: string): Cat | undefined { return cfg.categories.find((c) => c.id === id); }

function whenText(e: Ev, o: Occ): string {
  const s = new Date(o.start);
  const en = new Date(o.end);
  const day: Intl.DateTimeFormatOptions = { weekday: 'short', day: 'numeric', month: 'long', year: 'numeric' };
  if (e.all_day) {
    const last = addDays(en, -1);
    return sameDay(s, last) ? fmt(s, day) : `${fmt(s, day)} – ${fmt(last, day)}`;
  }
  const time: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' };
  return sameDay(s, en) ? `${fmt(s, day)}, ${fmt(s, time)}–${fmt(en, time)}` : `${fmt(s, day)}, ${fmt(s, time)} – ${fmt(en, day)}, ${fmt(en, time)}`;
}

const WD = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'];

function ruleText(rrule: string): string {
  if (!rrule) return '';
  const p = Object.fromEntries(rrule.split(';').map((x) => x.split('=')));
  const n = Number(p.INTERVAL ?? 1);
  let s = t(`content.events.every_${(p.FREQ as string).toLowerCase()}`, { n });
  if (p.BYDAY) {
    const days = (p.BYDAY as string).split(',').map((d) => {
      const m = /^(-?\d)?([A-Z]{2})$/.exec(d);
      if (!m) return d;
      const wd = new Date(2024, 0, 7 + WD.indexOf(m[2])).toLocaleDateString(L(), { weekday: 'long' });
      return m[1] ? `${t(`content.events.nth_${m[1]}`)} ${wd}` : wd;
    });
    s += ` (${days.join(', ')})`;
  }
  if (p.COUNT) s += `, ${t('content.events.times', { n: p.COUNT })}`;
  if (p.UNTIL) {
    const u = p.UNTIL as string;
    s += `, ${t('content.events.until', { date: new Date(Number(u.slice(0, 4)), Number(u.slice(4, 6)) - 1, Number(u.slice(6, 8))).toLocaleDateString(L()) })}`;
  }
  return s;
}

function statusText(e: Ev, o: Occ): string {
  if (!e.registration) return '';
  const seats = e.capacity ? t('content.events.seats', { n: o.registered, max: e.capacity }) : t('content.events.registered_n', { n: o.registered });
  return o.waiting ? `${seats} · ${t('content.events.waiting_n', { n: o.waiting })}` : seats;
}

// ---------------------------------------------------------------- event details

async function showEvent(series: Ev, o: Occ, cfg: Config, reload: () => Promise<void>): Promise<void> {
  const e = eff(series, o);
  const cat = catOf(cfg, e.category);
  const signedIn = !!state.user;
  const close = () => d.close();
  const act = async (fn: () => Promise<unknown>, msg: string) => {
    try { await fn(); toast(msg); close(); await reload(); } catch (err) { toast(errorText(err), 'error'); }
  };
  const reg = `${BASE}/events/${e.id}/registrations`;
  const started = new Date(o.start) <= new Date();
  const actions: (HTMLElement | null)[] = [];
  if (signedIn && !started) {
    if (o.my_status === 'registered' || o.my_status === 'waitlist') {
      actions.push(button(o.my_status === 'registered' ? t('content.events.unregister') : t('content.events.leave_waitlist'),
        () => void act(() => api.del(`${reg}${qs({ occurrence: o.date })}`), t('content.events.unregistered')), { variant: 'secondary' }));
    } else if (e.registration && o.registration_open) {
      const label = o.full ? (e.waitlist ? t('content.events.join_waitlist') : '') : t('content.events.register');
      if (label) {
        actions.push(button(label, async () => {
          const note = textInput({ maxlength: 500 });
          formDialog(t('content.events.register_title', { title: e.title }), [h('p', null, whenText(e, o)), field(t('content.events.note'), note, { help: t('content.events.note_help') })], label, async () => {
            const r = await api.post<{ status: string }>(reg, { occurrence: o.date, note: note.value });
            toast(t(`content.events.status_${r.status}`));
            close();
            await reload();
          });
        }, { variant: 'primary' }));
      }
    }
    if (e.reminders.length && !o.my_status) {
      actions.push(button(t('content.events.remind_me'), () => void act(() => api.post(reg, { occurrence: o.date, interested: true }), t('content.events.status_interested')), { variant: 'secondary' }));
    } else if (o.my_status === 'interested') {
      actions.push(button(t('content.events.stop_remind'), () => void act(() => api.del(`${reg}${qs({ occurrence: o.date })}`), t('content.events.unregistered')), { variant: 'secondary' }));
    }
  }
  if (!e.external) actions.push(h('a', { href: `${BASE}/events/${e.id}/ical`, class: 'ew-btn ew-btn--secondary', download: '' }, t('content.events.add_to_calendar')));
  if (e.can_manage && e.registration) actions.push(button(t('content.events.participants'), () => void participants(e, o), { variant: 'secondary' }));
  if (e.can_edit) {
    if (e.rrule) {
      actions.push(button(t('content.events.edit_occurrence'), () => { close(); occurrenceForm(series, o, reload); }, { variant: 'secondary' }));
      if (o.changed) {
        actions.push(button(t('content.events.reset_occurrence'), async () => {
          if (!(await confirmDialog(t('content.events.reset_occurrence_confirm'), t('content.events.reset_occurrence')))) return;
          await act(() => api.del(`${BASE}/events/${e.id}/occurrences/${o.date}`), t('content.events.occurrence_reset'));
        }, { variant: 'secondary' }));
      }
    }
    actions.push(button(e.rrule ? t('content.events.edit_series') : t('common.edit'), () => { close(); void eventForm(e.calendar_id, cfg, series, null, reload); }, { variant: 'secondary' }));
    if (e.rrule) {
      actions.push(button(t('content.events.cancel_occurrence'), async () => {
        if (!(await confirmDialog(t('content.events.cancel_occurrence_confirm', { date: new Date(o.start).toLocaleDateString(L()) }), t('content.events.cancel_occurrence'), true))) return;
        await act(() => api.put(`${BASE}/events/${e.id}`, { ...toInput(series), exdates: [...series.exdates, o.date] }), t('content.events.occurrence_cancelled'));
      }, { variant: 'danger' }));
    }
    actions.push(button(t('common.delete'), async () => {
      if (!(await confirmDialog(t(e.rrule ? 'content.events.delete_series_confirm' : 'content.events.delete_confirm', { title: e.title }), t('common.delete'), true))) return;
      await act(() => api.del(`${BASE}/events/${e.id}`), t('content.events.deleted'));
    }, { variant: 'danger' }));
  }
  const myStatus = o.my_status ? h('p', { class: `ewe-mystatus ewe-mystatus--${o.my_status}`, role: 'status' }, t(`content.events.you_${o.my_status}`)) : null;
  const d = openDialog(e.title, h('div', { class: 'ewe-details', id: `event-${e.id}-${o.date}` },
    h('dl', { class: 'ewe-facts' },
      h('dt', null, t('content.events.when')), h('dd', null, whenText(e, o), e.tz && e.tz !== browserTZ() && !e.all_day ? h('span', { class: 'ewe-tz' }, ` (${t('content.events.your_time')})`) : null),
      e.rrule ? [h('dt', null, t('content.events.repeats')), h('dd', null, ruleText(e.rrule))] : null,
      o.original_start ? [h('dt', null, t('content.events.originally')), h('dd', null, whenText(series, { ...o, start: o.original_start, end: new Date(new Date(o.original_start).getTime() + new Date(series.ends_at).getTime() - new Date(series.starts_at).getTime()).toISOString() }))] : null,
      e.location ? [h('dt', null, t('content.events.location')), h('dd', null, e.location)] : null,
      e.url ? [h('dt', null, t('content.events.link')), h('dd', null, h('a', { href: e.url, rel: 'noopener noreferrer', target: '_blank' }, e.url))] : null,
      cat ? [h('dt', null, t('content.events.category')), h('dd', null, h('span', { class: 'ewe-dot', style: `background:${cat.color}`, 'aria-hidden': 'true' }), ' ', cat.label)] : null,
      e.registration ? [h('dt', null, t('content.events.registration')), h('dd', null, statusText(e, o), !o.registration_open && !started ? ` · ${t('content.events.reg_closed')}` : null)] : null,
      e.author ? [h('dt', null, t('content.events.organizer')), h('dd', null, e.author)] : null,
      e.external || sourceNames[String(e.calendar_id)] ? [h('dt', null, t('content.events.from_source')), h('dd', null, e.external ? hostOf(e.source ?? '') : sourceNames[String(e.calendar_id)])] : null),
    o.changed ? h('p', { class: 'ewe-changed', role: 'note' }, t('content.events.occurrence_changed_note')) : null,
    myStatus,
    e.description_html ? h('div', { class: 'ewe-desc' }, trustedHTML(e.description_html)) : null,
    !signedIn && (e.registration || e.reminders.length) ? h('p', { class: 'ew-help' }, h('a', { href: `/login?next=${encodeURIComponent(location.pathname + `#event-${e.id}-${o.date}`)}` }, t('content.events.login_to_register'))) : null,
  ), actions.filter(Boolean) as HTMLElement[], { wide: true });
  history.replaceState(history.state, '', `#event-${e.id}-${o.date}`);
}

async function participants(e: Ev, o: Occ): Promise<void> {
  interface Reg { id: number; occurrence: string; status: string; note: string; name: string; email: string; created_at: string }
  let list: Reg[];
  try { list = await api.get<Reg[]>(`${BASE}/events/${e.id}/registrations${qs({ occurrence: o.date })}`); } catch (err) { toast(errorText(err), 'error'); return; }
  const csv = () => {
    const rows = [['name', 'email', 'status', 'note', 'registered_at'], ...list.map((r) => [r.name, r.email, r.status, r.note, r.created_at])];
    const text = rows.map((r) => r.map((x) => `"${String(x).replace(/"/g, '""')}"`).join(';')).join('\r\n');
    const a = h('a', { href: URL.createObjectURL(new Blob(['﻿' + text], { type: 'text/csv' })), download: `participants-${e.id}-${o.date}.csv` });
    a.click();
  };
  const body = list.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('content.events.participants')),
    h('thead', null, h('tr', null, ...[t('content.events.name'), t('content.events.status'), t('content.events.note'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
    h('tbody', null, ...list.map((r) => h('tr', null, h('th', { scope: 'row' }, r.name, r.email ? h('br') : null, r.email ? h('small', null, r.email) : null),
      h('td', null, t(`content.events.st_${r.status}`)), h('td', null, r.note),
      h('td', null, button(t('content.events.remove'), async () => {
        if (!(await confirmDialog(t('content.events.remove_participant', { name: r.name }), t('content.events.remove'), true))) return;
        try { await api.del(`${BASE}/events/${e.id}/registrations/${r.id}`); toast(t('content.events.removed')); dlg.close(); } catch (err) { toast(errorText(err), 'error'); }
      }, { variant: 'link', ariaLabel: `${t('content.events.remove')}: ${r.name}` }))))))
    : h('p', null, t('content.events.no_participants'));
  const dlg = openDialog(`${t('content.events.participants')}: ${e.title}`, [h('p', null, whenText(e, o), ' · ', statusText(e, o)), body],
    list.length ? [button(t('content.events.export_csv'), csv, { variant: 'secondary' })] : [], { wide: true });
}

// ---------------------------------------------------------------- event form

function toInput(e: Ev): Record<string, unknown> {
  const { title, description, location, url, category, starts_at, ends_at, all_day, tz, rrule, exdates, registration, capacity, waitlist, reg_deadline, reminders } = e;
  return { title, description, location, url, category, starts_at, ends_at, all_day, tz, rrule, exdates, registration, capacity, waitlist, reg_deadline, reminders };
}

const REMINDERS = [10080, 1440, 120, 15];
const DEADLINES = [0, 60, 1440, 2880, 10080];

/** Changes one occurrence of a series (empty fields: as the series). */
function occurrenceForm(e: Ev, o: Occ, reload: () => Promise<void>): void {
  const s0 = new Date(o.start);
  const e0 = new Date(o.end);
  const title = textInput({ value: o.title || e.title, required: true, maxlength: 200 });
  const sd = h('input', { type: 'date', class: 'ew-input', value: ymd(s0), required: true }) as HTMLInputElement;
  const st = h('input', { type: 'time', class: 'ew-input', value: hm(s0) }) as HTMLInputElement;
  const et = h('input', { type: 'time', class: 'ew-input', value: hm(e0) }) as HTMLInputElement;
  const loc = textInput({ value: o.location || e.location, maxlength: 300 });
  const desc = textArea({ value: e.overrides?.[o.date]?.description ?? '', rows: 4 });
  const when = e.all_day
    ? field(t('content.events.start_date'), sd, { required: true })
    : h('div', { class: 'ewe-when' }, field(t('content.events.start_date'), sd, { required: true }), field(t('content.events.start_time'), st), field(t('content.events.end_time'), et));
  formDialog(t('content.events.edit_occurrence_title', { date: s0.toLocaleDateString(L()) }), [
    h('p', { class: 'ew-help' }, t('content.events.edit_occurrence_help')),
    field(t('content.events.title'), title, { required: true }),
    when,
    field(t('content.events.location'), loc),
    field(t('content.events.occurrence_description'), desc, { help: t('content.events.occurrence_description_help') }),
  ], t('common.save'), async () => {
    if (!sd.value) throw new Error(t('content.events.err_start'));
    const dur = e0.getTime() - s0.getTime();
    const s = new Date(`${sd.value}T${e.all_day ? '00:00' : st.value || hm(s0)}`);
    let en = e.all_day ? new Date(s.getTime() + dur) : new Date(`${sd.value}T${et.value || hm(e0)}`);
    if (!e.all_day && en < s) en = new Date(en.getTime() + 86400000); // ends after midnight
    const body: Override = { title: title.value.trim(), location: loc.value.trim(), description: desc.value, starts_at: s.toISOString(), ends_at: en.toISOString() };
    await api.put(`${BASE}/events/${e.id}/occurrences/${o.date}`, body);
    toast(t('content.events.saved'));
    await reload();
  }, { wide: true });
}

async function eventForm(calendar: number, cfg: Config, e: Ev | null, day: Date | null, reload: () => Promise<void>): Promise<void> {
  const start = e ? new Date(e.starts_at) : new Date((day ?? new Date()).getFullYear(), (day ?? new Date()).getMonth(), (day ?? new Date()).getDate(), day ? 10 : new Date().getHours() + 1, 0);
  const end = e ? new Date(e.ends_at) : new Date(start.getTime() + 3600000);
  const title = textInput({ value: e?.title ?? '', required: true, maxlength: 200 });
  const allDay = checkbox(t('content.events.all_day'), e?.all_day ?? false);
  const sd = h('input', { type: 'date', class: 'ew-input', value: ymd(start), required: true }) as HTMLInputElement;
  const st = h('input', { type: 'time', class: 'ew-input', value: hm(start) }) as HTMLInputElement;
  const ed = h('input', { type: 'date', class: 'ew-input', value: ymd(e?.all_day ? addDays(end, -1) : end) }) as HTMLInputElement;
  const et = h('input', { type: 'time', class: 'ew-input', value: hm(end) }) as HTMLInputElement;
  const times = [st, et];
  const syncAllDay = () => times.forEach((x) => { x.closest<HTMLElement>('.ew-field')!.hidden = allDay.input.checked; });
  allDay.input.addEventListener('change', syncAllDay);
  // keep the duration when the start date changes
  let lastSD = sd.value;
  sd.addEventListener('change', () => {
    const delta = (new Date(sd.value).getTime() - new Date(lastSD).getTime()) / 86400000;
    if (ed.value && !isNaN(delta)) ed.value = ymd(addDays(new Date(`${ed.value}T00:00`), delta));
    lastSD = sd.value;
  });
  const loc = textInput({ value: e?.location ?? '', maxlength: 300 });
  const url = textInput({ value: e?.url ?? '', type: 'url', placeholder: 'https://…' });
  const desc = textArea({ value: e?.description ?? '', rows: 5 });
  const cat = cfg.categories.length ? select([{ value: '', label: '–' }, ...cfg.categories.map((c) => ({ value: c.id, label: c.label }))], e?.category ?? '') : null;

  // recurrence
  const rp = Object.fromEntries((e?.rrule ?? '').split(';').filter(Boolean).map((x) => x.split('='))) as Record<string, string>;
  const freq = select(['', 'DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'].map((f) => ({ value: f, label: t(`content.events.freq_${f.toLowerCase() || 'none'}`) })), rp.FREQ ?? '');
  const interval = textInput({ type: 'number', value: rp.INTERVAL ?? '1' });
  const days = WD.map((d, i) => {
    const c = checkbox(new Date(2024, 0, 7 + i).toLocaleDateString(L(), { weekday: 'short' }), (rp.BYDAY ?? '').split(',').includes(d));
    return { d, c };
  });
  const order = [1, 2, 3, 4, 5, 6, 0];
  const dayBox = h('fieldset', { class: 'ewe-days' }, h('legend', null, t('content.events.on_days')), ...order.map((i) => days[i].c.el));
  const monthMode = select([{ value: 'day', label: '' }, { value: 'nth', label: '' }], /\d[A-Z]{2}/.test(rp.BYDAY ?? '') ? 'nth' : 'day');
  const syncMonthLabels = () => {
    const s = new Date(`${sd.value}T00:00`);
    const n = Math.ceil(s.getDate() / 7);
    const last = addDays(s, 7).getMonth() !== s.getMonth();
    monthMode.options[0].text = t('content.events.monthly_day', { day: s.getDate() });
    monthMode.options[1].text = t('content.events.monthly_nth', { nth: t(`content.events.nth_${last && n >= 4 ? -1 : n}`), weekday: s.toLocaleDateString(L(), { weekday: 'long' }) });
  };
  sd.addEventListener('change', syncMonthLabels);
  syncMonthLabels();
  const endMode = select([{ value: 'never', label: t('content.events.end_never') }, { value: 'until', label: t('content.events.end_until') }, { value: 'count', label: t('content.events.end_count') }],
    rp.UNTIL ? 'until' : rp.COUNT ? 'count' : 'never');
  const until = h('input', { type: 'date', class: 'ew-input', value: rp.UNTIL ? `${rp.UNTIL.slice(0, 4)}-${rp.UNTIL.slice(4, 6)}-${rp.UNTIL.slice(6, 8)}` : '' }) as HTMLInputElement;
  const count = textInput({ type: 'number', value: rp.COUNT ?? '10' });
  const fInterval = field(t('content.events.interval'), interval);
  const fMonth = field(t('content.events.monthly_mode'), monthMode);
  const fUntil = field(t('content.events.end_date'), until);
  const fCount = field(t('content.events.count'), count);
  const fEnd = field(t('content.events.series_end'), endMode);
  const syncRule = () => {
    const f = freq.value;
    fInterval.hidden = fEnd.hidden = !f;
    dayBox.hidden = f !== 'WEEKLY';
    fMonth.hidden = f !== 'MONTHLY';
    fUntil.hidden = !f || endMode.value !== 'until';
    fCount.hidden = !f || endMode.value !== 'count';
  };
  freq.addEventListener('change', syncRule);
  endMode.addEventListener('change', syncRule);
  const buildRule = (): string => {
    if (!freq.value) return '';
    const parts = [`FREQ=${freq.value}`];
    if (Number(interval.value) > 1) parts.push(`INTERVAL=${Number(interval.value)}`);
    if (freq.value === 'WEEKLY') {
      const sel = days.filter((x) => x.c.input.checked).map((x) => x.d);
      if (sel.length) parts.push(`BYDAY=${sel.join(',')}`);
    }
    if (freq.value === 'MONTHLY' && monthMode.value === 'nth') {
      const s = new Date(`${sd.value}T00:00`);
      const n = Math.ceil(s.getDate() / 7);
      const last = addDays(s, 7).getMonth() !== s.getMonth();
      parts.push(`BYDAY=${last && n >= 4 ? -1 : n}${WD[s.getDay()]}`);
    }
    if (endMode.value === 'until' && until.value) parts.push(`UNTIL=${until.value.replace(/-/g, '')}`);
    if (endMode.value === 'count') parts.push(`COUNT=${Math.max(1, Number(count.value) || 1)}`);
    return parts.join(';');
  };

  // registration and reminders
  const regOn = checkbox(t('content.events.reg_on'), e?.registration ?? false);
  const cap = textInput({ type: 'number', value: String(e?.capacity ?? 0) });
  const wl = checkbox(t('content.events.waitlist'), e?.waitlist ?? true);
  const dl = select(DEADLINES.map((m) => ({ value: String(m), label: t(`content.events.deadline_${m}`) })), String(e?.reg_deadline ?? 0));
  const regBox = h('div', { class: 'ewe-reg' }, field(t('content.events.capacity'), cap, { help: t('content.events.capacity_help') }), wl.el, field(t('content.events.deadline'), dl));
  const syncReg = () => { regBox.hidden = !regOn.input.checked; };
  regOn.input.addEventListener('change', syncReg);
  const rems = REMINDERS.map((m) => ({ m, c: checkbox(t(`content.events.reminder_${m}`), (e?.reminders ?? []).includes(m)) }));

  const fStart = field(t('content.events.start_date'), sd, { required: true });
  const fields = [
    field(t('content.events.title'), title, { required: true }),
    allDay.el,
    h('div', { class: 'ewe-when' }, fStart, field(t('content.events.start_time'), st), field(t('content.events.end_date'), ed), field(t('content.events.end_time'), et)),
    field(t('content.events.location'), loc),
    cat ? field(t('content.events.category'), cat) : null,
    field(t('content.events.description'), desc, { help: t('content.events.description_help') }),
    field(t('content.events.link'), url),
    h('fieldset', null, h('legend', null, t('content.events.repeat')), field(t('content.events.frequency'), freq), fInterval, dayBox, fMonth, fEnd, fUntil, fCount,
      e?.rrule ? h('p', { class: 'ew-help' }, t('content.events.series_edit_help')) : null),
    h('fieldset', null, h('legend', null, t('content.events.registration')), regOn.el, regBox),
    h('fieldset', null, h('legend', null, t('content.events.reminders')), h('p', { class: 'ew-help' }, t('content.events.reminders_help')), ...rems.map((r) => r.c.el)),
  ];
  formDialog(e ? t('content.events.edit_event') : t('content.events.new_event'), fields, t('common.save'), async () => {
    if (!sd.value) throw new Error(t('content.events.err_start'));
    const ad = allDay.input.checked;
    const s = new Date(`${sd.value}T${ad ? '00:00' : st.value || '00:00'}`);
    let en = new Date(`${ed.value || sd.value}T${ad ? '00:00' : et.value || st.value || '00:00'}`);
    if (ad) en = addDays(en, 1);
    if (en < s) throw new Error(t('content.events.err_end'));
    const body = {
      title: title.value, description: desc.value, location: loc.value, url: url.value.trim(), category: cat?.value ?? '',
      starts_at: s.toISOString(), ends_at: en.toISOString(), all_day: ad, tz: e?.tz || browserTZ(), rrule: buildRule(), exdates: e?.exdates ?? [],
      registration: regOn.input.checked, capacity: Math.max(0, Number(cap.value) || 0), waitlist: wl.input.checked, reg_deadline: Number(dl.value),
      reminders: rems.filter((r) => r.c.input.checked).map((r) => r.m),
    };
    if (e) await api.put(`${BASE}/events/${e.id}`, body);
    else await api.post(`${BASE}/calendars/${calendar}/events`, body);
    toast(t('content.events.saved'));
    await reload();
  }, { wide: true });
  syncAllDay();
  syncRule();
  syncReg();
}

// ---------------------------------------------------------------- subscription

async function subscribe(id: number, title: string): Promise<void> {
  const https = await feedURL(`${BASE}/calendars/${id}/ical`, 'atom');
  const clean = https.replace(/([?&])format=atom&?/, '$1').replace(/[?&]$/, '');
  const webcal = clean.replace(/^https?:/, 'webcal:');
  const row = (label: string, value: string) => {
    const input = h('input', { class: 'ew-input', readonly: true, value, id: uid('ical') }) as HTMLInputElement;
    input.addEventListener('focus', () => input.select());
    return h('div', { class: 'ew-field' }, h('label', { for: input.id }, label), h('div', { class: 'ew-picker' }, input,
      button(t('feed.copy'), () => void navigator.clipboard?.writeText(value).then(() => toast(t('feed.copied'))), { variant: 'secondary', ariaLabel: `${t('feed.copy')}: ${label}` })));
  };
  openDialog(t('content.events.subscribe_title', { title }), [
    h('p', null, t('content.events.subscribe_help')),
    row(t('content.events.subscribe_url'), clean),
    h('p', null, h('a', { href: webcal, class: 'ew-btn ew-btn--primary' }, t('content.events.subscribe_open'))),
    state.user ? h('p', { class: 'ew-help', role: 'note' }, t('feed.personal_help')) : null,
  ]);
}

// ---------------------------------------------------------------- views

function chip(series: Ev, o: Occ, cfg: Config, open: () => void, withTime = true): HTMLElement {
  const e = eff(series, o);
  const cat = catOf(cfg, e.category);
  const s = new Date(o.start);
  const label = `${withTime && !e.all_day ? `${fmt(s, { hour: '2-digit', minute: '2-digit' })} ` : ''}${e.title}`;
  const b = button(label, open, { variant: 'subtle' });
  b.classList.add('ewe-chip');
  if (e.all_day) b.classList.add('ewe-chip--allday');
  if (o.my_status === 'registered') b.classList.add('ewe-chip--mine');
  b.style.setProperty('--ewe-color', cat?.color ?? 'var(--ew-primary)');
  b.title = `${whenText(e, o)}${e.location ? ` · ${e.location}` : ''}`;
  return b;
}

/** Occurrences that touch a day (multi-day events appear on each day). */
function onDay(occ: Occ[], d: Date): Occ[] {
  const s = d.getTime();
  const en = addDays(d, 1).getTime();
  return occ.filter((o) => new Date(o.start).getTime() < en && (new Date(o.end).getTime() > s || new Date(o.start).getTime() === s));
}

function sortDay(list: Occ[], byId: Map<number, Ev>): Occ[] {
  return [...list].sort((a, b) => Number(byId.get(b.event_id)!.all_day) - Number(byId.get(a.event_id)!.all_day) || a.start.localeCompare(b.start));
}

async function eventsWidget(root: HTMLElement): Promise<void> {
  const id = Number(root.dataset.element);
  const small = window.matchMedia?.('(max-width: 48rem)').matches;
  let view = (sessionStorage.getItem(`ewe-view-${id}`) as View) || (small ? 'list' : root.dataset.view as View) || 'month';
  let cursor = new Date();
  let category = '';
  let q = '';
  let data: Data | null = null;
  const hash = /^#event-(\d+)-(\d{4}-\d{2}-\d{2})$/.exec(location.hash);
  if (hash) cursor = new Date(`${hash[2]}T00:00`);

  const title = h('h2', { class: 'ewe-title', 'aria-live': 'polite' });
  const body = h('div', { class: 'ewe-body' });
  const viewBtns = (['month', 'week', 'list'] as View[]).map((v) => {
    const b = button(t(`content.events.view_${v}`), () => { view = v; sessionStorage.setItem(`ewe-view-${id}`, v); void load(); }, { variant: 'secondary' });
    b.dataset.view = v;
    return b;
  });
  const step = (dir: number) => {
    if (view === 'month') cursor = new Date(cursor.getFullYear(), cursor.getMonth() + dir, 1);
    else if (view === 'week') cursor = addDays(cursor, 7 * dir);
    else cursor = addDays(cursor, (data?.calendar.config.list_days ?? 90) * dir);
    void load();
  };
  const prev = button(h('span', { 'aria-hidden': 'true' }, '‹'), () => step(-1), { variant: 'secondary', ariaLabel: t('content.events.previous', { unit: '' }) });
  const next = button(h('span', { 'aria-hidden': 'true' }, '›'), () => step(1), { variant: 'secondary', ariaLabel: t('content.events.next', { unit: '' }) });
  const today = button(t('content.events.today'), () => { cursor = new Date(); void load(); }, { variant: 'secondary' });
  const filters = h('div', { class: 'ewe-filters' });
  const actions = h('div', { class: 'ew-actions' });
  const search = textInput({ type: 'search', placeholder: t('content.events.search_placeholder') });
  search.setAttribute('aria-label', t('content.events.search'));
  let st = 0;
  search.addEventListener('input', () => { clearTimeout(st); st = window.setTimeout(() => { q = search.value.trim(); void load(); }, 300); });

  const range = (cfg: Config): [Date, Date] => {
    if (view === 'month') {
      const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
      const shift = (first.getDay() - cfg.week_start + 7) % 7;
      const from = addDays(first, -shift);
      return [from, addDays(from, 42)];
    }
    if (view === 'week') {
      const from = addDays(cursor, -((cursor.getDay() - cfg.week_start + 7) % 7));
      return [new Date(from.getFullYear(), from.getMonth(), from.getDate()), addDays(from, 7)];
    }
    const from = new Date(cursor.getFullYear(), cursor.getMonth(), cursor.getDate());
    return [from, addDays(from, cfg.list_days)];
  };

  const render = () => {
    if (!data) return;
    const cfg = data.calendar.config;
    const byId = new Map(data.events.map((e) => [e.id, e]));
    const [from, to] = range(cfg);
    const open = (o: Occ) => () => void showEvent(byId.get(o.event_id)!, o, cfg, load);
    viewBtns.forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.view === view)));
    const unit = t(`content.events.unit_${view}`);
    prev.setAttribute('aria-label', t('content.events.previous', { unit }));
    next.setAttribute('aria-label', t('content.events.next', { unit }));
    const now = new Date();
    if (view === 'month') {
      title.textContent = fmt(cursor, { month: 'long', year: 'numeric' });
      const head = Array.from({ length: 7 }, (_, i) => addDays(from, i));
      const rows: HTMLElement[] = [];
      for (let w = 0; w < 6; w++) {
        rows.push(h('tr', null, ...Array.from({ length: 7 }, (_, i) => {
          const d = addDays(from, w * 7 + i);
          const list = sortDay(onDay(data!.occurrences, d), byId);
          const shown = list.slice(0, 4);
          const label = fmt(d, { weekday: 'long', day: 'numeric', month: 'long' });
          return h('td', { class: ['ewe-day', d.getMonth() !== cursor.getMonth() && 'ewe-day--other', sameDay(d, now) && 'ewe-day--today'] },
            h('div', { class: 'ewe-day-head' },
              h('span', { class: 'ewe-day-num', 'aria-hidden': 'true' }, String(d.getDate())), h('span', { class: 'sr-only' }, label),
              data!.calendar.can_create ? button(h('span', { 'aria-hidden': 'true' }, '+'), () => void eventForm(id, cfg, null, d, load), { variant: 'subtle', ariaLabel: t('content.events.add_on', { date: label }) }) : null),
            shown.length ? h('ul', { class: 'ewe-day-list' }, ...shown.map((o) => h('li', null, chip(byId.get(o.event_id)!, o, cfg, open(o))))) : null,
            list.length > shown.length ? button(t('content.events.more', { n: list.length - shown.length }), () => {
              openDialog(label, h('ul', { class: 'ewe-day-list' }, ...list.map((o) => h('li', null, chip(byId.get(o.event_id)!, o, cfg, open(o))))));
            }, { variant: 'link', ariaLabel: `${t('content.events.more', { n: list.length - shown.length })}: ${label}` }) : null);
        })));
      }
      mount(body, h('table', { class: 'ewe-month' }, h('caption', { class: 'sr-only' }, title.textContent),
        h('thead', null, h('tr', null, ...head.map((d) => h('th', { scope: 'col' }, h('abbr', { title: fmt(d, { weekday: 'long' }) }, fmt(d, { weekday: 'short' })))))),
        h('tbody', null, ...rows)));
    } else if (view === 'week') {
      const last = addDays(to, -1);
      title.textContent = `${fmt(from, { day: 'numeric', month: 'short' })} – ${fmt(last, { day: 'numeric', month: 'short', year: 'numeric' })}`;
      mount(body, h('ol', { class: 'ewe-week' }, ...Array.from({ length: 7 }, (_, i) => {
        const d = addDays(from, i);
        const list = sortDay(onDay(data!.occurrences, d), byId);
        const hid = uid('wd');
        return h('li', { class: ['ewe-wday', sameDay(d, now) && 'ewe-day--today'], 'aria-labelledby': hid },
          h('h3', { id: hid }, fmt(d, { weekday: 'short' }), ' ', h('span', null, fmt(d, { day: 'numeric', month: 'numeric' }))),
          list.length ? h('ul', { class: 'ewe-day-list' }, ...list.map((o) => h('li', null, chip(byId.get(o.event_id)!, o, cfg, open(o))))) : h('p', { class: 'ewe-free' }, t('content.events.free')),
          data!.calendar.can_create ? button(h('span', { 'aria-hidden': 'true' }, '+'), () => void eventForm(id, cfg, null, d, load), { variant: 'subtle', ariaLabel: t('content.events.add_on', { date: fmt(d, { weekday: 'long', day: 'numeric', month: 'long' }) }) }) : null);
      })));
    } else {
      title.textContent = t('content.events.from_date', { date: fmt(from, { day: 'numeric', month: 'long', year: 'numeric' }) });
      const groups = new Map<string, Occ[]>();
      for (const o of data.occurrences) {
        if (new Date(o.end) < from) continue;
        const k = ymd(new Date(o.start) < from ? from : new Date(o.start));
        groups.set(k, [...(groups.get(k) ?? []), o]);
      }
      mount(body, groups.size ? h('div', { class: 'ewe-list' }, ...[...groups.entries()].map(([k, list]) => {
        const d = new Date(`${k}T00:00`);
        return h('section', { class: 'ewe-list-day' }, h('h3', null, fmt(d, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })),
          h('ul', null, ...sortDay(list, byId).map((o) => {
            const e = eff(byId.get(o.event_id)!, o);
            const cat = catOf(cfg, e.category);
            const s = new Date(o.start);
            return h('li', { class: 'ewe-list-item', style: `--ewe-color:${cat?.color ?? 'var(--ew-primary)'}` },
              h('span', { class: 'ewe-list-time' }, e.all_day ? t('content.events.all_day') : `${fmt(s, { hour: '2-digit', minute: '2-digit' })}–${fmt(new Date(o.end), { hour: '2-digit', minute: '2-digit' })}`),
              h('span', { class: 'ewe-list-main' }, button(e.title, open(o), { variant: 'link' }),
                e.location ? h('span', { class: 'ewe-list-loc' }, e.location) : null,
                cat ? h('span', { class: 'ewe-badge', style: `--ewe-color:${cat.color}` }, cat.label) : null,
                e.registration ? h('span', { class: 'ewe-list-reg' }, statusText(e, o)) : null,
                o.my_status ? h('span', { class: 'ew-badge ew-badge--ok' }, t(`content.events.st_${o.my_status}`)) : null));
          })));
      })) : h('p', { class: 'ewe-none' }, t('content.events.none')));
    }
  };

  const fresh = newest();
  const load = async () => {
    const current = fresh();
    const cfg = data?.calendar.config ?? { week_start: 1, list_days: 90 } as Config;
    const [from, to] = range(cfg);
    let got: Data;
    try {
      got = await api.get<Data>(`${BASE}/calendars/${id}/events${qs({ from: from.toISOString(), to: to.toISOString(), category, q, lang: L() })}`);
      if (got.calendar.config.week_start !== cfg.week_start || got.calendar.config.list_days !== cfg.list_days) {
        // settings known now: load the exact range once more
        const [f2, t2] = range(got.calendar.config);
        if (f2.getTime() !== from.getTime() || t2.getTime() !== to.getTime()) {
          got = await api.get<Data>(`${BASE}/calendars/${id}/events${qs({ from: f2.toISOString(), to: t2.toISOString(), category, q, lang: L() })}`);
        }
      }
    } catch (e) {
      if (current()) mount(body, h('p', { role: 'alert' }, errorText(e)));
      return;
    }
    if (!current()) return; // a newer request (search, other month) has started meanwhile
    data = got;
    sourceNames = data.calendars ?? {};
    render();
    announce(title.textContent ?? '');
  };

  await load();
  if (!data) return;
  const d0 = data as Data;
  const cfg = d0.calendar.config;
  if (cfg.categories.length) {
    const sel = select([{ value: '', label: t('content.events.all_categories') }, ...cfg.categories.map((c) => ({ value: c.id, label: c.label }))]);
    sel.setAttribute('aria-label', t('content.events.filter_category'));
    sel.addEventListener('change', () => { category = sel.value; void load(); });
    filters.append(sel);
  }
  filters.append(search);
  mount(actions,
    d0.calendar.can_create ? button(t('content.events.new_event'), () => void eventForm(id, cfg, null, null, load), { variant: 'primary' }) : null,
    cfg.ical ? button(t('content.events.subscribe'), () => void subscribe(id, d0.calendar.title), { variant: 'secondary' }) : null,
    state.user ? button(t('content.events.my_registrations'), () => void myRegistrations(), { variant: 'subtle' }) : null);
  const toolbar = h('div', { class: 'ewe-toolbar' },
    h('div', { class: 'ewe-nav', role: 'group', 'aria-label': t('content.events.navigation') }, prev, today, next),
    title,
    h('div', { class: 'ewe-views', role: 'group', 'aria-label': t('content.events.view') }, ...viewBtns));
  root.querySelector('.ewe-static')?.replaceWith(h('div', { class: 'ewe-app' }, toolbar, h('div', { class: 'ewe-bar' }, filters, actions), body));
  render();
  if (hash) {
    const o = d0.occurrences.find((x) => x.event_id === Number(hash[1]) && x.date === hash[2]);
    const e = d0.events.find((x) => x.id === Number(hash[1]));
    if (o && e) void showEvent(e, o, cfg, load);
  }
}

async function myRegistrations(): Promise<void> {
  interface R { event_id: number; occurrence: string; status: string; title: string; calendar_id: number }
  interface U { event_id: number; calendar_id: number; calendar: string; title: string; start: string; all_day: boolean }
  let list: R[];
  let next: U[] = [];
  try {
    [list, next] = await Promise.all([api.get<R[]>(`${BASE}/me/registrations`), api.get<U[]>(`${BASE}/upcoming?limit=10&days=60`).catch(() => [])]);
  } catch (e) { toast(errorText(e), 'error'); return; }
  const day = (d: Date) => d.toLocaleDateString(L(), { dateStyle: 'medium' });
  openDialog(t('content.events.my_registrations'), [
    list.length ? h('ul', { class: 'ewe-my' }, ...list.map((r) => h('li', null,
      h('a', { href: `/e/${r.calendar_id}#event-${r.event_id}-${r.occurrence}` }, `${day(new Date(`${r.occurrence}T00:00`))} – ${r.title}`),
      ' ', h('span', { class: 'ew-badge' }, t(`content.events.st_${r.status}`))))) : h('p', null, t('content.events.no_registrations')),
    next.length ? [h('h3', null, t('content.events.upcoming_all')), h('ul', { class: 'ewe-my' }, ...next.map((u) => {
      const s = new Date(u.start);
      return h('li', null, h('a', { href: `/e/${u.calendar_id}#event-${u.event_id}-${ymd(s)}` },
        `${day(s)}${u.all_day ? '' : `, ${hm(s)}`} – ${u.title}`), ' ', h('span', { class: 'ewe-up-cal' }, u.calendar));
    }))] : null,
  ]);
}

export function register(): void {
  // further sources of calendars: other calendars and subscribed iCalendar addresses
  registerSourceKind({
    kind: 'calendar', label: () => t('content.events.src_calendar'),
    render: async (v) => {
      const p = await elementSelect({ selected: Number(v) || undefined, type: 'content.events' });
      return { el: p.el, get: () => (p.value() > 0 ? String(p.value()) : '') };
    },
  });
  registerSourceKind({
    kind: 'ical', label: () => t('content.events.src_ical'),
    render: (v) => {
      const i = textInput({ value: v, type: 'url', placeholder: 'https://example.org/calendar.ics' });
      i.setAttribute('aria-label', t('content.events.src_ical'));
      return { el: i, get: () => (/^https?:\/\//.test(i.value.trim()) ? i.value.trim() : '') };
    },
  });
  registerMacroParamControl('events.sources', (value, ctx) => sourcesEditor(ctx.label, value, { kinds: ['calendar', 'ical'], help: t('content.events.more_sources_help') }));
  registerEditor('content.events', createEditor);
  registerWidget('events', eventsWidget);
}
