// Page properties: a table above the content (if a page has properties),
// an editor dialog for people with the Edit permission (add, remove,
// reorder rows; suggestions from the keys used in the category).
import { api, button, fmtDate, formDialog, h, registerElementExtension, select, state, t, textInput, toast, uid, type ElementView } from '@ewiki/sdk';
import './style.css';

const BASE = 'x/core.properties';

interface Property { key: string; value: string; type: string }
interface Props { items: Property[]; can_edit: boolean; types: string[] }

function valueNode(p: Property): Node {
  switch (p.type) {
    case 'date': return p.value ? h('time', { datetime: p.value }, fmtDate(`${p.value}T12:00:00`, state.lang).split(',')[0]) : document.createTextNode('');
    case 'url': return p.value ? h('a', { href: p.value, rel: 'noopener' }, p.value) : document.createTextNode('');
    case 'status': return p.value ? h('span', { class: 'ew-badge ewprop-status' }, p.value) : document.createTextNode('');
    default: return document.createTextNode(p.value);
  }
}

function editor(el: ElementView, data: Props, done: (p: Props) => void): void {
  const rows: Property[] = data.items.length ? data.items.map((x) => ({ ...x })) : [{ key: '', value: '', type: 'text' }];
  const list = h('div', { class: 'ewprop-edit' });
  const keysId = uid('ewprop-keys');
  const keys = h('datalist', { id: keysId });
  void api.get<{ key: string; type: string }[]>(`${BASE}/keys?category=${el.category_id}`).then((ks) => {
    keys.replaceChildren(...ks.map((k) => h('option', { value: k.key })));
  }).catch(() => undefined);
  const draw = (focus?: number) => {
    list.replaceChildren(...rows.map((r, i) => {
      const key = textInput({ value: r.key, maxlength: 80 });
      key.setAttribute('list', keysId);
      key.setAttribute('aria-label', t('core.properties.key_n', { n: i + 1 }));
      key.addEventListener('input', () => { r.key = key.value; });
      const type = select(data.types.map((x) => ({ value: x, label: t(`core.properties.type.${x}`) })), r.type);
      type.setAttribute('aria-label', t('core.properties.type_n', { n: i + 1 }));
      const val = textInput({ value: r.value, maxlength: 1000, type: r.type === 'date' ? 'date' : r.type === 'number' ? 'text' : r.type === 'url' ? 'url' : 'text' });
      val.setAttribute('aria-label', t('core.properties.value_n', { n: i + 1 }));
      val.addEventListener('input', () => { r.value = val.value; });
      type.addEventListener('change', () => { r.type = type.value; draw(i); });
      const row = h('div', { class: 'ewprop-row' }, key, type, val,
        h('span', { class: 'ew-actions' },
          i > 0 ? button('↑', () => { [rows[i - 1], rows[i]] = [rows[i], rows[i - 1]]; draw(i - 1); }, { variant: 'subtle', ariaLabel: t('core.properties.up_n', { n: i + 1 }) }) : null,
          i < rows.length - 1 ? button('↓', () => { [rows[i + 1], rows[i]] = [rows[i], rows[i + 1]]; draw(i + 1); }, { variant: 'subtle', ariaLabel: t('core.properties.down_n', { n: i + 1 }) }) : null,
          button(t('common.remove'), () => { rows.splice(i, 1); draw(Math.min(i, rows.length - 1)); }, { variant: 'link', ariaLabel: t('core.properties.remove_n', { n: i + 1 }) })));
      if (focus === i) queueMicrotask(() => key.focus());
      return row;
    }));
  };
  draw();
  formDialog(t('core.properties.edit_title', { title: el.title }), [
    h('p', { class: 'ew-help' }, t('core.properties.help')), keys,
    h('div', { class: 'ewprop-head', 'aria-hidden': 'true' }, h('span', null, t('core.properties.key')), h('span', null, t('core.properties.type')), h('span', null, t('core.properties.value'))),
    list, button(t('core.properties.add'), () => { rows.push({ key: '', value: '', type: 'text' }); draw(rows.length - 1); }, { variant: 'secondary' }),
  ], t('common.save'), async () => {
    const r = await api.put<Props>(`${BASE}/elements/${el.id}`, { items: rows.filter((x) => x.key.trim()) });
    toast(t('core.properties.saved'));
    done(r);
  }, { wide: true });
}

async function section(el: ElementView): Promise<Node | null> {
  let data: Props;
  try { data = await api.get<Props>(`${BASE}/elements/${el.id}`); } catch { return null; }
  const root = h('section', { class: 'ewprop', 'aria-labelledby': `ewprop-${el.id}` });
  const draw = () => {
    root.hidden = !data.items.length && !(data.can_edit && state.editMode);
    root.replaceChildren(...[
      h('h2', { id: `ewprop-${el.id}`, class: data.items.length ? 'sr-only' : 'ewprop-title' }, t('core.properties.title')),
      data.items.length ? h('table', { class: 'ew-table ewprop-table' }, h('caption', { class: 'sr-only' }, t('core.properties.title')),
        h('tbody', null, ...data.items.map((p) => h('tr', null, h('th', { scope: 'row' }, p.key), h('td', null, valueNode(p)))))) : null,
      data.can_edit && state.editMode ? button(data.items.length ? t('core.properties.edit') : t('core.properties.add_first'),
        () => editor(el, data, (n) => { data = n; draw(); }), { variant: 'secondary', ariaLabel: `${t('core.properties.edit')}: ${el.title}` }) : null,
    ].filter((x) => x !== null) as HTMLElement[]);
  };
  draw();
  return root;
}

export function register(): void {
  registerElementExtension({ id: 'core.properties', order: 5, placement: 'top', render: section });
}
