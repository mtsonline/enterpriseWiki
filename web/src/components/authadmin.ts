// Admin page of a sign-in plugin with several provider instances (e.g. two
// OpenID Connect providers): choose, add and remove instances, edit the
// settings of one, and preview what a sign-in would lead to (mapping, JIT,
// linking) without changing anything.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, checkbox, confirmDialog, errorText, field, formDialog, pageHeader, select, textInput } from './ui';
import { loadPluginSettings, pluginSettingsForm, savePluginSettings, type SettingField } from './settings';

export interface AuthProviderPage {
  /** Plugin id, e.g. "auth.oidc". */
  pluginId: string;
  /** Provider id of the first instance, e.g. "oidc"; further ones are base-<id>. */
  base: string;
  title: string;
  help: string;
  fields: () => SettingField[];
  /** Instance specific parts (addresses to register, connection test); provider is the provider id. */
  extra?: (provider: string, instance: string) => HTMLElement | Promise<HTMLElement>;
}

interface Preview {
  outcome: string; user?: string; groups: string[]; new_groups: string[]; roles: string[]; missing_roles: string[];
  jit: boolean; trust_email: boolean; email_verified: boolean;
}

const ID_RE = /^[a-z0-9][a-z0-9-]{0,23}$/;

/** Renders the admin page into el; instance "" is the first instance. */
export async function renderAuthProviderPage(el: HTMLElement, p: AuthProviderPage, instance = ''): Promise<void> {
  const { settings } = await loadPluginSettings(p.pluginId);
  const instances = (Array.isArray(settings.instances) ? settings.instances : [])
    .filter((x): x is Record<string, unknown> => !!x && typeof x === 'object' && typeof (x as Record<string, unknown>).id === 'string');
  if (instance && !instances.some((x) => x.id === instance)) instance = '';
  const provider = instance ? `${p.base}-${instance}` : p.base;
  const label = (x: Record<string, unknown>, id: string) => `${String(x.name || '') || id} (${id})`;

  // choose / add / remove
  const choice = select([{ value: '', label: label(settings, p.base) },
    ...instances.map((x) => ({ value: String(x.id), label: label(x, `${p.base}-${String(x.id)}`) }))], instance);
  choice.id = 'ew-auth-instance';
  const focusChoice = () => document.getElementById('ew-auth-instance')?.focus();
  choice.addEventListener('change', () => void renderAuthProviderPage(el, p, choice.value).then(focusChoice));
  const add = button(t('authadmin.add'), () => {
    const id = textInput({ required: true, maxlength: 24, autocomplete: 'off' });
    const name = textInput({ required: true });
    formDialog(t('authadmin.add_title'), [h('p', { class: 'ew-help' }, t('authadmin.add_help')),
      field(t('authadmin.id'), id, { required: true, help: t('authadmin.id_help', { base: p.base }) }),
      field(t('authadmin.name'), name, { required: true })], t('authadmin.add'), async () => {
      const v = id.value.trim();
      if (!ID_RE.test(v)) throw new Error(t('authadmin.id_invalid'));
      if (instances.some((x) => x.id === v)) throw new Error(t('authadmin.id_taken', { id: v }));
      await savePluginSettings(p.pluginId, { ...settings, instances: [...instances, { id: v, name: name.value.trim() }] });
      toast(t('authadmin.added', { name: name.value.trim() }));
      // after the dialog has closed: show the new instance, focus its choice
      setTimeout(() => void renderAuthProviderPage(el, p, v).then(focusChoice), 0);
    });
  }, { variant: 'secondary' });
  const remove = instance ? button(t('authadmin.remove'), async () => {
    if (!(await confirmDialog(t('authadmin.remove_confirm', { id: provider }), t('authadmin.remove'), true))) return;
    try {
      await savePluginSettings(p.pluginId, { ...settings, instances: instances.filter((x) => x.id !== instance) });
      toast(t('authadmin.removed'));
      await renderAuthProviderPage(el, p, '');
      focusChoice();
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'danger' }) : null;

  const form = await pluginSettingsForm(p.pluginId, p.fields(), t('authadmin.settings_of', { id: provider }), instance || undefined);
  mount(el, pageHeader(p.title), h('p', null, p.help),
    h('div', { class: 'ew-panel ew-inline-form' }, field(t('authadmin.instance'), choice, { help: t('authadmin.instance_help') }), add, remove ?? ''),
    p.extra ? await p.extra(provider, instance) : '',
    form.el,
    previewPanel(provider));
}

/** Mapping and JIT preview for a sample sign-in. */
function previewPanel(provider: string): HTMLElement {
  const user = textInput({ required: true, autocomplete: 'off' });
  const email = textInput({ type: 'email', autocomplete: 'off' });
  const verified = checkbox(t('authadmin.preview_verified'));
  const groups = textInput({ autocomplete: 'off' });
  const result = h('div', { role: 'status', class: 'ew-test-result' });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form ew-panel', novalidate: true }, h('h2', null, t('authadmin.preview')),
    h('p', { class: 'ew-help' }, t('authadmin.preview_help')), alert,
    field(t('auth.username'), user, { required: true }), field(t('email.address'), email), verified.el,
    field(t('authadmin.preview_groups'), groups, { help: t('mapping.list_help_external') }),
    h('div', { class: 'ew-form-actions' }, button(t('authadmin.preview_run'), () => undefined, { type: 'submit' })), result);
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    result.textContent = '';
    if (!user.value.trim()) { alert.textContent = t('authadmin.preview_user_missing'); user.focus(); return; }
    try {
      const r = await api.post<Preview>(`admin/auth/providers/${encodeURIComponent(provider)}/preview`, {
        username: user.value.trim(), email: email.value.trim(), email_verified: verified.input.checked,
        groups: groups.value.split(',').map((x) => x.trim()).filter(Boolean),
      });
      const list = (xs: string[]) => (xs.length ? xs.join(', ') : '–');
      mount(result, h('dl', { class: 'ew-dl' },
        h('dt', null, t('authadmin.outcome')), h('dd', null, h('strong', null, t(`authadmin.outcome.${r.outcome}`, { user: r.user ?? '' }))),
        h('dt', null, t('admin.groups')), h('dd', null, list(r.groups)),
        r.new_groups.length ? [h('dt', null, t('authadmin.new_groups')), h('dd', null, list(r.new_groups))] : '',
        h('dt', null, t('admin.roles')), h('dd', null, list(r.roles)),
        r.missing_roles.length ? [h('dt', null, t('authadmin.missing_roles')), h('dd', { class: 'ew-field-error' }, list(r.missing_roles))] : ''));
    } catch (e) { alert.textContent = errorText(e); }
  });
  return form;
}
