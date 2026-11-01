// Admin page of the external search engine.
import { api, button, errorText, fmtDate, h, mount, pageHeader, pluginSettingsForm, registerAdminPage, state, t, toast } from '@ewiki/sdk';

const BASE = 'x/search.external';

interface Status {
  configured: boolean; active: boolean; engine: string; url: string; index: string; pending_changes: number; documents: number;
  engine_documents?: number; engine_error?: string;
  state: { last_seq: number; ready: boolean; last_sync: string | null; last_error: string; indexed: number };
}

async function render(el: HTMLElement): Promise<void> {
  const [st, form] = await Promise.all([api.get<Status>(`${BASE}/status`), pluginSettingsForm('search.external', [
    { key: 'engine', label: t('search.external.engine'), type: 'select', options: [
      { value: '', label: t('search.external.engine.none') }, { value: 'opensearch', label: 'OpenSearch / Elasticsearch' }, { value: 'meilisearch', label: 'Meilisearch' }] },
    { key: 'url', label: t('search.external.url'), placeholder: 'http://localhost:9200', help: t('search.external.url_help'), when: (v) => !!v.engine },
    { key: 'index', label: t('search.external.index'), placeholder: 'ewiki', help: t('search.external.index_help'), when: (v) => !!v.engine },
    { key: 'api_key', label: t('search.external.api_key'), type: 'secret', help: t('search.external.api_key_help'), when: (v) => !!v.engine },
    { key: 'username', label: t('search.external.username'), when: (v) => v.engine === 'opensearch' },
    { key: 'password', label: t('search.external.password'), type: 'secret', when: (v) => v.engine === 'opensearch' },
  ])]);
  const refresh = () => void render(el).catch((e) => toast(errorText(e), 'error'));
  const s = st.state;
  const status = !st.configured ? t('search.external.off')
    : st.active ? t('search.external.active', { engine: st.engine })
      : t('search.external.building', { engine: st.engine });
  mount(el, pageHeader(t('search.external.title')), h('p', null, t('search.external.help')),
    h('div', { class: ['ew-notice', !st.configured ? 'ew-notice--info' : st.active && !s.last_error ? 'ew-notice--success' : 'ew-notice--warning'], role: 'status' },
      h('p', null, status), s.last_error ? h('p', null, t('search.external.error', { error: s.last_error })) : null,
      st.engine_error ? h('p', null, t('search.external.error', { error: st.engine_error })) : null),
    st.configured ? h('dl', { class: 'ew-dl' },
      h('dt', null, t('search.external.docs_db')), h('dd', null, String(st.documents)),
      h('dt', null, t('search.external.docs_engine')), h('dd', null, st.engine_documents === undefined ? '–' : String(st.engine_documents)),
      h('dt', null, t('search.external.pending')), h('dd', null, String(st.pending_changes)),
      h('dt', null, t('search.external.last_sync')), h('dd', null, s.last_sync ? fmtDate(s.last_sync, state.lang) : '–')) : null,
    st.configured ? h('div', { class: 'ew-actions' },
      button(t('search.external.test'), async () => {
        try { await api.post(`${BASE}/test`); toast(t('search.external.test_ok')); } catch (e) { toast(errorText(e), 'error'); }
      }),
      button(t('search.external.reindex'), async () => {
        try { await api.post(`${BASE}/reindex`); toast(t('search.external.reindex_started')); setTimeout(refresh, 1500); } catch (e) { toast(errorText(e), 'error'); }
      }),
      button(t('search.external.refresh'), refresh, { variant: 'link' })) : null,
    form.el);
}

export function register(): void {
  registerAdminPage({ id: 'search', label: () => t('search.external.title'), order: 65, render });
}
