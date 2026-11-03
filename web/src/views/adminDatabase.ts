// PostgreSQL version: notice on administration pages and facts on the settings page.
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';

export interface DatabaseStatus { version: string; major: number; recommended: number; eol: string; days_left: number; level: 'ok' | 'info' | 'warning' | 'error'; docker: boolean }

const date = (d: string) => new Date(`${d}T00:00`).toLocaleDateString(state.lang);
const params = (st: DatabaseStatus) => ({ version: st.version, major: st.major, recommended: st.recommended, eol: date(st.eol), days: st.days_left });

function howto(st: DatabaseStatus): HTMLElement {
  return h('span', null, ' ', st.docker
    ? [t('pgversion.howto_docker'), ' ', h('code', null, './docker-postgres-upgrade.sh')]
    : t('pgversion.howto_other'));
}

/** Notice for administration pages (null when nothing needs attention). */
export function databaseNotice(st: DatabaseStatus, withInfo = false): HTMLElement | null {
  if (st.level === 'ok' || (st.level === 'info' && !withInfo)) return null;
  const key = st.level === 'error' ? 'pgversion.ended' : st.level === 'warning' ? 'pgversion.ends_soon' : 'pgversion.recommended';
  return h('div', { class: `ew-license-notice ew-license-notice--${st.level}`, role: st.level === 'error' ? 'alert' : 'note' }, t(key, params(st)), howto(st));
}

/** Facts for the settings page. */
export function databaseFacts(st: DatabaseStatus): HTMLElement {
  return h('section', { class: 'ew-section', 'aria-labelledby': 'db-h' }, h('h2', { id: 'db-h' }, t('pgversion.title')),
    databaseNotice(st, true),
    h('dl', { class: 'ew-dl' },
      h('dt', null, t('pgversion.running')), h('dd', null, `PostgreSQL ${st.version}`),
      h('dt', null, t('pgversion.recommended_label')), h('dd', null, `PostgreSQL ${st.recommended}`),
      h('dt', null, t('pgversion.support_until')), h('dd', null, date(st.eol))));
}
