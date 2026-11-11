// The properties above the content of an element, the quick editing and the
// history of changes.
import { api, button, fmtDate, formDialog, h, openDialog, qs, state, t, toast, type ElementView } from '@ewiki/sdk';
import { BASE, formatValue, plainValue, type Props } from './model';
import { propertiesForm } from './edit';

async function history(el: ElementView, data: Props): Promise<void> {
  const list = await api.get<{ key: string; value: unknown; at: string; by: string }[]>(`${BASE}/history/${el.id}`);
  const label = (k: string) => data.items.find((x) => x.key === k)?.label ?? k;
  const item = (k: string) => data.items.find((x) => x.key === k);
  openDialog(t('props.history_title', { title: el.title }), list.length
    ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('props.history')),
      h('thead', null, h('tr', null, h('th', { scope: 'col' }, t('props.when')), h('th', { scope: 'col' }, t('props.property')), h('th', { scope: 'col' }, t('props.new_value')), h('th', { scope: 'col' }, t('props.by')))),
      h('tbody', null, ...list.map((c) => {
        const it = item(c.key);
        return h('tr', null, h('td', null, fmtDate(c.at, state.lang)), h('th', { scope: 'row' }, label(c.key)),
          h('td', null, c.value === null ? h('em', null, t('props.removed')) : plainValue(it?.type ?? 'text', c.value, it?.options, data.refs)), h('td', null, c.by || '—'));
      })))
    : h('p', null, t('props.no_history')), [], { wide: true });
}

export async function section(el: ElementView): Promise<Node | null> {
  let data: Props;
  try { data = await api.get<Props>(`${BASE}/elements/${el.id}${qs({ lang: state.lang })}`); } catch { return null; }
  const root = h('section', { class: 'ewprop', 'aria-labelledby': `ewprop-${el.id}` });
  const draw = () => {
    const shown = data.items.filter((x) => x.show_on_page && x.value !== null && x.value !== undefined && !(Array.isArray(x.value) && !x.value.length));
    const editable = data.can_edit && (data.items.some((x) => x.can_edit) || data.can_add_free);
    root.hidden = !shown.length && !(editable && state.editMode);
    const actions = h('div', { class: 'ewprop-actions' },
      editable && state.editMode ? (() => { const l = shown.length ? t('props.edit') : t('props.add_first'); return button(l, () => void quickEdit(), { variant: 'secondary', ariaLabel: `${l}: ${el.title}` }); })() : null,
      shown.length ? button(t('props.history'), () => void history(el, data), { variant: 'subtle', ariaLabel: `${t('props.history')}: ${el.title}` }) : null);
    root.replaceChildren(
      h('h2', { id: `ewprop-${el.id}`, class: 'sr-only' }, t('props.title')),
      shown.length ? h('dl', { class: 'ewprop-list' }, ...shown.flatMap((x) => [h('dt', null, x.label), h('dd', null, formatValue(x.type, x.value, x.options, data.refs))])) : h('p', { class: 'ew-muted' }, t('props.none_yet')),
      actions);
  };
  const quickEdit = async () => {
    const form = await propertiesForm(data, { compact: true });
    formDialog(t('props.edit_title', { title: el.title }), [form.el], t('common.save'), async () => {
      const e = form.validate();
      if (e) throw new Error(e);
      data = await api.put<Props>(`${BASE}/elements/${el.id}${qs({ lang: state.lang })}`, form.value());
      const fresh = await api.get<Props>(`${BASE}/elements/${el.id}${qs({ lang: state.lang })}`);
      data = fresh;
      toast(t('props.saved'));
      draw();
    }, { wide: true });
  };
  draw();
  return root;
}
