// Administration → Expert settings: the technical limits of the core and of
// all plugins in one place (sizes of requests, parallel work, lengths in the
// audit log …). Each has a default and an allowed range.
import { api } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { isAdmin, state } from '../core/state';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, errorText, field, pageHeader, setFieldError, textInput } from '../components/ui';

interface Limit {
  key: string; owner: string; group: string; label: string; help: string; unit: string;
  default: number; min: number; max: number; restart?: boolean; value: number; changed: boolean; active: boolean;
}

interface Proxy { header: string; trusted: number; untrusted: { peer: string; header: string; last: string; requests: number }[] }
interface SecretsStatus { mode: 'plain' | 'aes' | 'pq'; key_loaded: boolean; password: boolean; password_env: string; values: { plain: number; aes: number; pq: number }; rewrap_needed: boolean }
interface Key { id: number; name: string; prefix: string; owner: string; rate_per_second: number | null; rate_own: boolean }

/** translated text of a limit, or the English text the core or plugin declared */
const tr = (key: string, fallback: string) => { const s = t(key); return s === key ? fallback : s; };
const withUnit = (n: number, unit: string) => t(`limits.unit.${unit}`, { n: n.toLocaleString() });

export async function limitsAdmin(panel: HTMLElement): Promise<void> {
  const res = await api.get<{ limits: Limit[]; editable: boolean; proxy: Proxy }>('admin/limits');
  const keys = isAdmin() ? await api.get<Key[]>('admin/api-keys') : null;
  const inputs = new Map<string, HTMLInputElement>();
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const groups = new Map<string, Limit[]>();
  for (const l of res.limits) groups.set(l.group, [...(groups.get(l.group) ?? []), l]);

  const row = (l: Limit): HTMLElement => {
    const input = textInput({ value: String(l.value), type: 'number', name: l.key });
    input.min = String(l.min);
    input.max = String(l.max);
    input.step = '1';
    input.inputMode = 'numeric';
    input.disabled = !res.editable;
    inputs.set(l.key, input);
    const facts = [
      tr(`limit.${l.key}.help`, l.help),
      t('limits.range', { unit: t(`limits.unit_name.${l.unit}`), default: withUnit(l.default, l.unit), min: l.min.toLocaleString(), max: l.max.toLocaleString() }),
      l.owner === 'core' ? '' : t('limits.owner', { plugin: l.owner }) + (l.active ? '' : ` ${t('limits.inactive')}`),
      l.restart ? t('limits.restart') : '',
    ].filter(Boolean).join(' ');
    const reset = res.editable ? button(t('limits.reset'), () => { input.value = String(l.default); input.focus(); },
      { variant: 'subtle', ariaLabel: t('limits.reset_of', { name: tr(`limit.${l.key}`, l.label) }) }) : null;
    return h('div', { class: 'ew-limit' }, field(tr(`limit.${l.key}`, l.label), input, { help: facts }), reset);
  };

  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    ...[...groups].map(([g, list]) => h('section', { class: 'ew-section', 'aria-labelledby': `lim-${g}` },
      h('h2', { id: `lim-${g}` }, tr(`limits.group.${g}`, g)), ...list.map(row))),
    res.editable ? h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })) : null);

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const values: Record<string, number | null> = {};
    let firstBad: HTMLInputElement | null = null;
    for (const l of res.limits) {
      const input = inputs.get(l.key)!;
      setFieldError(input, '');
      const v = Number(input.value);
      if (input.value.trim() === '' || !Number.isInteger(v) || v < l.min || v > l.max) {
        setFieldError(input, t('limits.out_of_range', { min: l.min.toLocaleString(), max: l.max.toLocaleString() }));
        firstBad ??= input;
      } else if (v !== l.value) values[l.key] = v === l.default ? null : v;
    }
    if (firstBad) { firstBad.focus(); return; }
    if (!Object.keys(values).length) { toast(t('limits.unchanged')); return; }
    try {
      await api.put('admin/limits', { values });
      toast(t('limits.saved'));
      await limitsAdmin(panel);
    } catch (e) { alert.textContent = errorText(e); }
  });

  // where the address of visitors comes from, and proxies that are not configured
  const px = res.proxy;
  const proxy = h('section', { class: 'ew-section', 'aria-labelledby': 'lim-proxy' }, h('h2', { id: 'lim-proxy' }, t('limits.proxy.title')),
    h('p', null, px.trusted ? t('limits.proxy.info', { header: px.header, n: px.trusted }) : t('limits.proxy.none')),
    px.untrusted.length ? h('div', { class: 'ew-notice ew-notice--warning', role: 'status' }, h('p', null, t('limits.proxy.warning', { header: px.header })),
      h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('limits.proxy.title')),
        h('thead', null, h('tr', null, ...[t('limits.proxy.peer'), t('limits.proxy.requests'), t('limits.proxy.last')].map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...px.untrusted.map((u) => h('tr', null, h('th', { scope: 'row' }, h('code', null, u.peer)),
          h('td', null, u.requests.toLocaleString()), h('td', null, fmtDate(u.last, state.lang))))))) : null);

  // the own limit of each API key
  const keyRow = (k: Key): HTMLElement => {
    const label = `${k.owner} · ${k.name}`;
    const input = textInput({ value: k.rate_per_second ? String(k.rate_per_second) : '', type: 'number' });
    input.setAttribute('aria-label', t('limits.keys.rate_of', { name: label }));
    input.min = '1';
    input.step = '1';
    input.inputMode = 'numeric';
    const save = button(t('common.save'), async () => {
      const v = input.value.trim() === '' ? null : Number(input.value);
      setFieldError(input, '');
      if (v !== null && (!Number.isInteger(v) || v < 1)) { setFieldError(input, t('limits.out_of_range', { min: '1', max: '100.000' })); input.focus(); return; }
      try { await api.put(`admin/api-keys/${k.id}/rate`, { rate_per_second: v }); toast(t('limits.keys.saved')); } catch (e) { setFieldError(input, errorText(e)); input.focus(); }
    }, { variant: 'subtle', ariaLabel: `${t('common.save')}: ${label}` });
    return h('tr', null, h('th', { scope: 'row' }, k.owner), h('td', null, k.name, ' ', h('code', null, `${k.prefix}…`)), h('td', null, input), h('td', null, save));
  };
  const keySection = keys ? h('section', { class: 'ew-section', 'aria-labelledby': 'lim-keys' }, h('h2', { id: 'lim-keys' }, t('limits.keys.title')),
    h('p', null, t('limits.keys.help')),
    keys.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('limits.keys.title')),
      h('thead', null, h('tr', null, ...[t('limits.keys.owner'), t('limits.keys.name'), t('limits.keys.rate'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...keys.map(keyRow))) : h('p', null, t('limits.keys.none'))) : null;

  // how secrets in the database are stored (instance administrators)
  let secretsSection: HTMLElement | null = null;
  if (isAdmin() && res.editable) {
    try {
      const st = await api.get<SecretsStatus>('admin/secrets');
      const counts = t('secrets.values', { plain: st.values.plain, aes: st.values.aes, pq: st.values.pq });
      const status = h('p', { role: 'status' });
      secretsSection = h('section', { class: 'ew-section', 'aria-labelledby': 'lim-secrets' }, h('h2', { id: 'lim-secrets' }, t('secrets.title')),
        h('p', null, t(`secrets.mode.${st.mode}`)),
        h('p', null, st.key_loaded ? t(st.password ? 'secrets.key_password' : 'secrets.key_loaded') : t('secrets.no_key')),
        h('p', null, counts),
        st.rewrap_needed ? h('p', { class: 'ew-notice ew-notice--warning', role: 'note' }, t('secrets.rewrap_needed')) : null,
        h('p', { class: 'ew-help' }, t('secrets.help', { env: st.password_env })),
        button(t('secrets.rewrap'), async () => {
          try {
            const r = await api.post<{ changed: number }>('admin/secrets/rewrap', {});
            status.textContent = t('secrets.rewrapped', { n: r.changed });
          } catch (e) { status.textContent = errorText(e); }
        }, { variant: 'secondary' }), status);
    } catch { secretsSection = null; }
  }

  mount(panel, pageHeader(t('admin.limits')),
    h('p', null, t('limits.intro')),
    res.editable ? null : h('p', { role: 'note' }, t('limits.instance_only')),
    form, keySection, proxy, secretsSection);
}
