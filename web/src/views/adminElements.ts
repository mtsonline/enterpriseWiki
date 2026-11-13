// Administration → Elements: all elements with search, filters and actions.
import { api, qs } from '../core/api';
import { newest } from '../core/newest';
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, confirmDialog, errorText, field, formDialog, pageHeader, select, textInput } from '../components/ui';
import { categorySelect, newElement } from '../components/pickers';
import { shortOnlyBox, shortURLInput, hrefOf } from '../components/shorturl';
import { invalidateTree } from '../components/widgets';
import { previewImageInput } from '../components/previewimage';
import { typeLabel } from '../sdk';

interface Found {
  id: number; title: string; type: string; category_id: number; category_path: string[]; short_url: string; short_only?: boolean; preview_image: string; tags: string[];
  updated_at: string; updated_by: string; is_default: boolean;
}

const PAGE = 50;

export async function elementsAdmin(panel: HTMLElement): Promise<void> {
  const q = textInput({ type: 'search', placeholder: t('picker.search_placeholder') });
  const type = select([{ value: '', label: t('elements.all_types') }, ...state.site.element_types.map((x) => ({ value: x.id, label: typeLabel(x.id) }))]);
  const sort = select(['updated', 'title', 'created', 'type'].map((v) => ({ value: v, label: t(`elements.sort_${v}`) })), 'updated');
  const cats = await categorySelect({ perm: 'view', noneLabel: t('picker.all_categories'), onChange: () => { offset = 0; void load(); } });
  const results = h('div', { class: 'ew-admin-results' });
  const status = h('p', { role: 'status', class: 'ew-help' });
  let offset = 0;
  let timer = 0;
  const fresh = newest();
  const load = async () => {
    const current = fresh();
    let data: { items: Found[]; total: number };
    try {
      data = await api.get(`elements${qs({ q: q.value.trim(), type: type.value, category: cats.value() || undefined, sort: sort.value, limit: PAGE, offset, lang: state.lang })}`);
    } catch (e) {
      if (current()) mount(results, h('p', { role: 'alert' }, errorText(e)));
      return;
    }
    if (!current()) return; // a newer search has started meanwhile
    status.textContent = data.total ? t('elements.range', { from: offset + 1, to: offset + data.items.length, total: data.total }) : t('elements.none');
    mount(results, data.items.length ? h('div', { class: 'ew-table-wrap' }, h('table', { class: 'ew-table ew-table--compact' }, h('caption', { class: 'sr-only' }, t('admin.elements')),
      h('thead', null, h('tr', null, ...[t('common.title'), t('element.type'), t('picker.category'), t('shorturl.label'), t('elements.updated'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...data.items.map((x) => h('tr', null,
        h('th', { scope: 'row' }, h('a', { href: hrefOf('e', x.id, x.short_url) }, x.title || `#${x.id}`), x.is_default ? h('span', { class: 'ew-badge' }, ' ', t('elements.default')) : null),
        h('td', null, typeLabel(x.type)),
        h('td', null, h('a', { href: `/c/${x.category_id}` }, x.category_path.join(' › '))),
        h('td', null, x.short_url ? h('code', null, `/${x.short_url}`) : '–'),
        h('td', { class: 'ew-nowrap', title: x.updated_by }, new Date(x.updated_at).toLocaleDateString(state.lang, { dateStyle: 'medium' })),
        h('td', { class: 'ew-actions' },
          h('a', { href: `/e/${x.id}/edit?lang=${state.lang}`, class: 'ew-btn ew-btn--link', 'aria-label': `${t('common.edit')}: ${x.title}` }, t('common.edit')),
          button(t('element.properties'), () => properties(x), { variant: 'link', ariaLabel: `${t('element.properties')}: ${x.title}` }),
          button(t('common.move'), () => void move(x), { variant: 'link', ariaLabel: `${t('common.move')}: ${x.title}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('element.delete_confirm', { title: x.title }), t('common.delete'), true))) return;
            try { await api.del(`elements/${x.id}`); toast(t('element.deleted')); invalidateTree(); void load(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${x.title}` }))))))) : null,
    data.total > PAGE ? h('nav', { class: 'ew-pager', 'aria-label': t('elements.pages') },
      offset > 0 ? button(t('elements.previous'), () => { offset = Math.max(0, offset - PAGE); void load(); }, { variant: 'secondary' }) : null,
      offset + PAGE < data.total ? button(t('elements.next'), () => { offset += PAGE; void load(); }, { variant: 'secondary' }) : null) : null);
  };
  const properties = (x: Found) => {
    const tags = textInput({ value: x.tags.join(', ') });
    const short = shortURLInput(x.short_url);
    const only = shortOnlyBox(!!x.short_only);
    const img = previewImageInput(x.preview_image ?? '');
    formDialog(`${t('element.properties')}: ${x.title}`, [field(t('common.tags'), tags, { help: t('common.tags_help') }),
      field(t('shorturl.label'), short, { help: t('shorturl.help', { origin: location.origin }) }), only.el, img.el], t('common.save'), async () => {
      await api.patch(`elements/${x.id}`, { tags: tags.value.split(/[,\s]+/).filter(Boolean), short_url: short.value, short_only: only.input.checked, preview_image: img.value() });
      toast(t('common.saved'));
      void load();
    });
  };
  const move = async (x: Found) => {
    const target = await categorySelect({ selected: x.category_id, perm: 'create' });
    formDialog(t('element.move_title', { title: x.title }), target.field(t('category.target')), t('common.move'), async () => {
      await api.post(`elements/${x.id}/move`, { category_id: target.value() });
      toast(t('element.moved'));
      invalidateTree();
      void load();
    });
  };
  q.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => { offset = 0; void load(); }, 250); });
  type.addEventListener('change', () => { offset = 0; void load(); });
  sort.addEventListener('change', () => { offset = 0; void load(); });
  mount(panel, pageHeader(t('admin.elements'), button(t('picker.new_element'), () => newElement(cats.value(), () => void load()), { variant: 'primary' })),
    h('p', null, t('elements.help')),
    h('div', { class: 'ew-filters', role: 'search' }, field(t('picker.search'), q), field(t('element.type'), type), cats.field(t('picker.in_category')), field(t('elements.sort'), sort)),
    status, results);
  await load();
}
