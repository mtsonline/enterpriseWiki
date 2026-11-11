// Allowed networks: a factor without a question (the address of the
// sign-in). The profile part sets the own networks.
import { api, button, errorText, field, h, registerMFAMethodUI, registerUserAdminField, t, textArea, toast } from '@ewiki/sdk';

const BASE = '/api/v1/x/mfa.network';

const lines = (s: string): string[] => s.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);

export function register(): void {
  registerMFAMethodUI({
    id: 'network', label: () => t('mfa.method.network'),
    // checked by the server without a question; never shown as a step
    render: () => h('p', null, t('mnet.help')),
    profile: async () => {
      const st = await api.get<{ networks: string[]; current_ip: string; enforce_always: boolean }>(`${BASE}/me`);
      const area = textArea({ name: 'networks', rows: 4, value: st.networks.join('\n') });
      area.setAttribute('spellcheck', 'false');
      const form = h('form', { class: 'ew-form', novalidate: true },
        field(t('mnet.label'), area, { help: `${t('mnet.help')}${st.enforce_always ? ' ' + t('mnet.help_always') : ''}` }),
        h('p', null, t('mnet.current', { ip: st.current_ip })),
        h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'secondary' })));
      form.addEventListener('submit', async (ev) => {
        ev.preventDefault();
        try {
          const res = await api.put<{ networks: string[] }>(`${BASE}/me`, { networks: lines(area.value) });
          area.value = res.networks.join('\n');
          toast(t('mnet.saved'));
        } catch (e) { toast(errorText(e), 'error'); }
      });
      return h('div', { class: 'ew-subsection' }, h('h3', null, t('mnet.title')), form);
    },
  });

  // administrators: the networks of a user (dialog "edit user")
  registerUserAdminField({
    id: 'mfa.network',
    render: (uid) => {
      const area = textArea({ name: 'networks', rows: 2 });
      area.setAttribute('spellcheck', 'false');
      let loaded = false;
      const el = field(t('mnet.title'), area, { help: t('mnet.admin_help') });
      api.get<{ networks: string[] }>(`${BASE}/users/${uid}`).then((r) => { area.value = r.networks.join('\n'); loaded = true; })
        .catch((e) => { area.disabled = true; el.append(h('p', { role: 'alert' }, errorText(e))); });
      return {
        el,
        save: async () => {
          if (!loaded) return;
          await api.put(`${BASE}/users/${uid}`, { networks: lines(area.value) });
        },
      };
    },
  });
}
