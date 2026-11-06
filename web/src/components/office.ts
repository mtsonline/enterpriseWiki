// Office programs (Collabora, OnlyOffice, Microsoft 365): open options of a
// medium, the full page office view and renderers for the launch kinds.
import { api, qs } from '../core/api';
import { h, mount, uid } from '../core/dom';
import { t } from '../core/i18n';
import type { RouteCtx } from '../core/router';
import { navigate } from '../core/router';
import { state } from '../core/state';
import type { Medium, OfficeAction, OpenOption, OpenResult } from '../core/types';
import { toast } from '../core/announce';
import { pluginSettingsForm, type SettingField } from './settings';
import { button, errorText, pageHeader } from './ui';

/** Starts an office program in container (returns a cleanup function). */
export type OfficeRenderer = (container: HTMLElement, action: OfficeAction, close: () => void) => (() => void) | void | Promise<(() => void) | void>;

const renderers = new Map<string, OfficeRenderer>();

/** Registers how the browser starts a launch kind (plugins with their own frontend API). */
export function registerOfficeRenderer(kind: string, fn: OfficeRenderer): void {
  renderers.set(kind, fn);
}

// WOPI: POST the access token into an iframe (the token never appears in a URL of the browser).
registerOfficeRenderer('wopi', (container, a, close) => {
  const name = uid('ew-office');
  const frame = h('iframe', {
    name, class: 'ew-office-frame', title: t('office.frame_title'),
    allow: 'clipboard-read; clipboard-write; fullscreen',
  }) as HTMLIFrameElement;
  const form = h('form', { method: 'post', action: a.url, target: name, class: 'ew-hidden' }) as HTMLFormElement;
  for (const [k, v] of Object.entries(a.form ?? {})) form.append(h('input', { type: 'hidden', name: k, value: v }));
  container.append(frame, form);
  form.submit();
  form.remove();
  // PostMessage API: close button of the office program
  const onMessage = (ev: MessageEvent) => {
    if (ev.origin !== a.origin || ev.source !== frame.contentWindow) return;
    let msg: { MessageId?: string } | null = null;
    try { msg = typeof ev.data === 'string' ? JSON.parse(ev.data) : ev.data; } catch { return; }
    if (msg?.MessageId === 'App_LoadingStatus') {
      frame.contentWindow?.postMessage(JSON.stringify({ MessageId: 'Host_PostmessageReady', SendTime: Date.now(), Values: {} }), a.origin);
    } else if (msg?.MessageId === 'UI_Close') {
      close();
    }
  };
  window.addEventListener('message', onMessage);
  return () => window.removeEventListener('message', onMessage);
});

/** Programs the caller may open a medium with (internal first). */
export function openOptions(id: number): Promise<OpenOption[]> {
  return api.get<OpenOption[]>(`/api/v1/media/${id}/open`);
}

/** URL of the full page office view. */
export function officeURL(id: number, viewer: string, mode?: string): string {
  return `/office/${id}${qs({ viewer, mode })}`;
}

/** Buttons "Open in …" / "Edit in …" for a medium (empty when no program fits). */
export async function officeButtons(m: Medium): Promise<HTMLElement[]> {
  const opts = await openOptions(m.id).catch(() => [] as OpenOption[]);
  const out: HTMLElement[] = [];
  for (const o of opts) {
    if (o.viewer === 'internal') continue;
    for (const mode of o.modes) {
      const label = mode === 'edit' ? t('office.edit_in', { program: o.label }) : t('office.view_in', { program: o.label });
      out.push(button(label, () => navigate(officeURL(m.id, o.viewer, mode)), { variant: o.default ? undefined : 'subtle' }));
    }
  }
  return out;
}

/** The default program when the settings say "always open there" (else null). */
export async function externalDefault(m: Medium): Promise<OpenOption | null> {
  const opts = await openOptions(m.id).catch(() => [] as OpenOption[]);
  return opts.find((o) => o.external && o.viewer !== 'internal') ?? null;
}

/** /office/:id?viewer=…&mode=…: the office program in the whole content area. */
export async function officeView({ main, params, query }: RouteCtx): Promise<void> {
  const id = Number(params.id);
  const viewer = query.get('viewer') ?? '';
  const mode = query.get('mode') ?? '';
  const back = () => { if (history.length > 1) history.back(); else navigate(`/media${qs({ open: id })}`); };
  const m = await api.get<Medium>(`media/${id}`);
  document.title = `${m.title || m.name} – ${state.site.name}`;
  let res: OpenResult;
  try {
    res = await api.post<OpenResult>(`/api/v1/media/${id}/open`, { viewer, mode });
  } catch (e) {
    mount(main, h('h1', null, m.title || m.name), h('p', { class: 'ew-notice ew-notice--warning', role: 'alert' }, errorText(e)),
      button(t('office.back'), back));
    return;
  }
  if (res.viewer === 'internal' || !res.action) {
    navigate(`/media${qs({ open: id })}`, { replace: true });
    return;
  }
  const fn = renderers.get(res.action.kind);
  const stage = h('div', { class: 'ew-office-stage' });
  const bar = h('div', { class: 'ew-office-bar' },
    h('h1', { class: 'ew-office-title' }, m.title || m.name),
    h('span', { class: 'ew-help' }, res.mode === 'edit' ? t('office.editing') : res.restricted ? t('office.read_only_protected') : t('office.read_only')),
    button(t('office.close'), back, { variant: 'subtle' }));
  mount(main, h('section', { class: 'ew-office', 'aria-label': m.title || m.name }, bar, stage));
  if (!fn) {
    stage.append(h('p', { class: 'ew-notice ew-notice--warning', role: 'alert' }, t('office.no_renderer')));
    return;
  }
  const cleanup = await fn(stage, res.action, back);
  if (cleanup) {
    const stop = () => { cleanup(); window.removeEventListener('popstate', stop); };
    window.addEventListener('popstate', stop);
  }
}

// ---------------------------------------------------------------- administration of office plugins

interface OfficeStatus {
  ok: boolean; error?: string; configured: boolean; discovery?: string; actions: number;
  view: string[]; edit: string[]; proof_key: boolean; origins: string[]; mode?: string; secret?: boolean;
}

/** Render function for the admin page of an office plugin: help, settings, connection test. */
export function officeAdminRender(pluginId: string, fields: SettingField[], opts: { proof?: boolean } = {}): (el: HTMLElement) => Promise<void> {
  const render = async (el: HTMLElement): Promise<void> => {
    const form = await pluginSettingsForm(pluginId, fields);
    const status = h('div', { role: 'status', 'aria-live': 'polite' });
    const show = (st: OfficeStatus) => {
      if (!st.configured) {
        status.replaceChildren(h('p', { class: 'ew-notice ew-notice--info' }, t('office.not_configured')));
        return;
      }
      if (!st.ok) {
        status.replaceChildren(h('p', { class: 'ew-notice ew-notice--warning' }, t('office.test_failed', { error: st.error ?? '' })));
        return;
      }
      const kids: HTMLElement[] = [h('p', null, t(`${pluginId}.test_ok`, { view: String(st.view.length), edit: String(st.edit.length) }))];
      if (st.discovery) kids.push(h('p', { class: 'ew-help' }, t('office.using', { url: st.discovery })));
      if (st.edit.length) kids.push(h('p', { class: 'ew-help' }, t('office.edit_types', { types: st.edit.join(', ') })));
      if (opts.proof && !st.proof_key && form.values().verify_proof !== false) kids.push(h('p', { class: 'ew-notice ew-notice--warning' }, t(`${pluginId}.proof_missing`)));
      if (st.mode === 'api' && !st.secret) kids.push(h('p', { class: 'ew-notice ew-notice--warning' }, t(`${pluginId}.jwt_missing`)));
      status.replaceChildren(h('div', { class: 'ew-notice ew-notice--success' }, ...kids));
    };
    const test = button(t(`${pluginId}.test`), async () => {
      try { show(await api.post<OfficeStatus>(`x/${pluginId}/test`)); } catch (e) { toast(errorText(e), 'error'); }
    });
    mount(el, pageHeader(t(`${pluginId}.title`)), h('p', null, t(`${pluginId}.help`)), h('p', { class: 'ew-help' }, t(`${pluginId}.default_hint`)),
      form.el, h('div', { class: 'ew-actions' }, test), status);
  };
  return render;
}
