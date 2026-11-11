// Administration of the factor chain: the factor number of each MFA plugin
// and, per sign-in provider (instance), its factors and how many factors a
// sign-in needs.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { mfaMethodUI } from '../core/mfa';
import { button, checkbox, errorText, field, formDialog, pageHeader, textInput } from '../components/ui';

interface Factor { id: string; plugin: string; factor: number; default_factor: number; enabled: boolean; passwordless: boolean; enroll: boolean; silent: boolean }
interface ProviderChain { id: string; name: string; kind: string; methods: string[] | null; required: number }
interface MFAConfig { factors: Factor[]; providers: ProviderChain[] }

const factorName = (f: Factor) => mfaMethodUI(f.id)?.label() ?? f.id;

function numberInput(value: number, min: number): HTMLInputElement {
  const i = textInput({ type: 'number', value: String(value), required: true });
  i.min = String(min);
  i.max = '9';
  i.step = '1';
  i.setAttribute('inputmode', 'numeric');
  return i;
}

/** Factor number of the factors of a plugin; asked when it is switched on. */
export async function askFactor(plugin: string): Promise<boolean> {
  let cfg: MFAConfig;
  try { cfg = await api.get<MFAConfig>('admin/mfa'); } catch { return true; }
  const fs = cfg.factors.filter((f) => f.plugin === plugin);
  if (!fs.length) return true;
  const inputs = fs.map((f) => ({ f, i: numberInput(f.factor || f.default_factor, f.passwordless ? 1 : 2) }));
  return new Promise((resolve) => {
    let ok = false;
    formDialog(t('mfa.admin.enable_title'), [h('p', null, t('mfa.admin.enable_help')),
      ...inputs.map(({ f, i }) => field(t('mfa.admin.factor_of', { name: factorName(f) }), i,
        { required: true, help: f.passwordless ? t('mfa.admin.factor_help_first') : t('mfa.admin.factor_help') }))],
    t('plugins.enable'), async () => {
      const factors: Record<string, number> = {};
      for (const { i } of inputs) {
        const n = Number(i.value);
        if (!Number.isInteger(n) || n < Number(i.min) || n > 9) { i.focus(); throw new Error(t('mfa.admin.factor_invalid', { min: i.min })); }
      }
      factors[plugin] = Number(inputs[0].i.value);
      await api.put('admin/mfa', { factors });
      ok = true;
    }, { onClose: () => resolve(ok) });
  });
}

export async function mfaAdmin(panel: HTMLElement): Promise<void> {
  const cfg = await api.get<MFAConfig>('admin/mfa');
  const active = cfg.factors.filter((f) => f.enabled);
  const factorInputs = cfg.factors.map((f) => ({ f, i: numberInput(f.factor, f.passwordless ? 1 : 2) }));
  const factorTable = h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('mfa.admin.factors')),
    h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('mfa.admin.method')), h('th', { scope: 'col' }, t('mfa.admin.plugin')),
      h('th', { scope: 'col' }, t('mfa.admin.factor')), h('th', { scope: 'col' }, t('mfa.admin.status')))),
    h('tbody', null, ...(factorInputs.length ? factorInputs.map(({ f, i }) => {
      i.setAttribute('aria-label', t('mfa.admin.factor_of', { name: factorName(f) }));
      return h('tr', null, h('th', { scope: 'row' }, factorName(f)), h('td', null, h('code', null, f.plugin)), h('td', null, i),
        h('td', null, f.enabled ? t('mfa.admin.on') : t('mfa.admin.off'), f.silent ? ` · ${t('mfa.admin.silent')}` : '', f.passwordless ? ` · ${t('mfa.admin.passwordless')}` : ''));
    }) : [h('tr', null, h('td', { colspan: '4' }, t('mfa.admin.no_factors')))])));

  const provs = cfg.providers.map((p) => {
    const all = checkbox(t('mfa.admin.all'), p.methods === null, { help: t('mfa.admin.all_help') });
    const boxes = active.map((f) => ({ f, c: checkbox(`${factorName(f)} (${t('mfa.factor_n', { n: f.factor })})`, p.methods === null || p.methods.includes(f.id)) }));
    const sync = () => boxes.forEach((b) => { b.c.input.disabled = all.input.checked; if (all.input.checked) b.c.input.checked = true; });
    all.input.addEventListener('change', sync);
    sync();
    const req = numberInput(p.required, 1);
    const el = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, `${p.name} (${p.id})`), all.el,
      boxes.length ? h('fieldset', null, h('legend', null, t('mfa.admin.methods')), ...boxes.map((b) => b.c.el)) : '',
      field(t('mfa.admin.required'), req, { required: true, help: t('mfa.admin.required_help') }));
    return { p, el, get: () => ({ all: all.input.checked, methods: boxes.filter((b) => b.c.input.checked).map((b) => b.f.id), required: Number(req.value) }) };
  });

  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert,
    h('section', { class: 'ew-section', 'aria-labelledby': 'mfaf-h' }, h('h2', { id: 'mfaf-h' }, t('mfa.admin.factors')), h('p', { class: 'ew-help' }, t('mfa.admin.factors_help')), factorTable),
    h('section', { class: 'ew-section', 'aria-labelledby': 'mfap-h' }, h('h2', { id: 'mfap-h' }, t('mfa.admin.providers')), h('p', { class: 'ew-help' }, t('mfa.admin.providers_help')), ...provs.map((x) => x.el)),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    const factors: Record<string, number> = {};
    for (const { f, i } of factorInputs) {
      const n = Number(i.value);
      if (!Number.isInteger(n) || n < Number(i.min) || n > 9) { alert.textContent = t('mfa.admin.factor_invalid', { min: i.min }); i.focus(); return; }
      factors[f.plugin] = n;
    }
    const providers: Record<string, unknown> = {};
    for (const x of provs) providers[x.p.id] = x.get();
    try {
      await api.put('admin/mfa', { factors, providers });
      toast(t('common.saved'));
      await mfaAdmin(panel);
    } catch (e) { alert.textContent = errorText(e); alert.scrollIntoView({ block: 'nearest' }); }
  });
  mount(panel, pageHeader(t('admin.mfa')), h('p', null, t('mfa.admin.help')), form);
}
