// Properties of elements: the part in every editor, the properties above the
// content, the definitions (administration and category settings), the
// report widget and the controls of its macro dialog.
import { api, hasPerm, qs, registerAdminPage, registerCategorySettingsTab, registerEditField, registerElementExtension, registerOverviewColumns, registerWidget, state, t } from '@ewiki/coreplugin';
import './style.css';
import { BASE, plainValue, type Props } from './model';
import { propertiesForm } from './edit';
import { section } from './view';
import { defsManager } from './defs';
import { reportWidget, type Result } from './report';
import { databaseWidget } from './database';
import { registerControls } from './config';

export function register(): void {
  // the properties as columns of the table of a category overview; values
  // only of properties the reader may see (as everywhere in queries)
  registerOverviewColumns({
    id: 'core.properties',
    options: async (categoryId) => (await api.get<{ key: string; label: string }[]>(`${BASE}/keys${qs({ category: categoryId })}`))
      .map((k) => ({ key: k.key, label: k.label || k.key })),
    values: async (ids, keys, lang) => {
      const res = await api.post<Result>(`${BASE}/query`, { sources: [{ kind: 'elements', ids }], columns: keys, lang, limit: ids.length });
      const cols = res.columns.filter((c) => keys.includes(c.key));
      const values = new Map<number, Record<string, string>>();
      for (const r of res.rows ?? []) {
        const rec: Record<string, string> = {};
        for (const c of cols) rec[c.key] = r.values?.[c.key] === undefined ? '' : plainValue(c.type, r.values[c.key], c.options, res.refs ?? {});
        values.set(r.id, rec);
      }
      return { columns: cols.map((c) => ({ key: c.key, label: c.label || c.key })), values };
    },
  });
  registerEditField({
    id: 'core.properties',
    order: 10,
    render: async (ctx) => {
      let data: Props;
      try {
        data = ctx.elementId
          ? await api.get<Props>(`${BASE}/elements/${ctx.elementId}${qs({ lang: ctx.lang })}`)
          : await api.get<Props>(`${BASE}/new${qs({ category: ctx.categoryId, type: ctx.type, lang: ctx.lang })}`);
      } catch { return null; }
      // a draft's values over the stored ones
      const init = ctx.initial as { values?: Record<string, unknown>; free?: Record<string, { type: string; label: string }> } | undefined;
      if (init?.values) {
        for (const [k, v] of Object.entries(init.values)) {
          const it = data.items.find((x) => x.key === k);
          if (it) it.value = v;
          else if (init.free?.[k]) data.items.push({ key: k, label: init.free[k].label, type: init.free[k].type, def_id: null, value: v, show_on_page: true, can_edit: true });
        }
      }
      const form = await propertiesForm(data);
      if (form.empty) return null;
      return { el: form.el, value: () => form.value(), validate: () => form.validate() };
    },
  });
  registerElementExtension({ id: 'core.properties', order: 5, label: () => t('core.properties.block'), placement: 'top', render: section });
  registerWidget('core.properties.report', reportWidget);
  registerWidget('core.properties.database', databaseWidget);
  registerControls();
  registerAdminPage({ id: 'properties', area: 'content', label: () => t('props.admin_title'), permission: 'core.properties.admin', order: 40, render: (el) => defsManager(el, null) });
  registerCategorySettingsTab({
    id: 'properties', order: 20, label: () => t('props.title'),
    visible: (c) => !!state.user && ((c.permissions ?? []).includes('manage') || hasPerm('core.properties.admin')),
    render: (panel, c) => defsManager(panel, c.id),
  });
}
