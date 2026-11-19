// Choosing an icon (parameter of type custom:icon): a search over the names,
// search words and categories of the shipped set, the results as buttons
// (each named for screen readers), at most 120 at a time.
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { findIcons, iconIndex, iconURL, type IconEntry } from '../core/icons';
import { button, field, openDialog, select, textInput } from './ui';

const MAX = 120;

/** Opens the picker; done gets the name chosen. */
export function pickIcon(style: string, current: string, done: (name: string) => void): void {
  const search = textInput({ value: '' });
  search.type = 'search';
  const cat = select([{ value: '', label: t('icon.all_categories') }]);
  const grid = h('div', { class: 'ew-iconpick-grid', role: 'group', 'aria-label': t('icon.results') });
  const status = h('p', { class: 'ew-help', 'aria-live': 'polite' });
  let all: IconEntry[] = [];
  const draw = () => {
    const { list, total } = findIcons(all, search.value, style, cat.value, MAX);
    status.textContent = total > list.length ? t('icon.found_more', { n: total, shown: list.length }) : t('icon.found', { n: total });
    grid.replaceChildren(...list.map((e) => {
      const b = button(h('span', { class: 'ew-icon ew-icon--l', 'aria-hidden': 'true', style: `--ew-icon: url("${iconURL(e.n, style)}")` }), () => { done(e.n); dlg.close(); },
        { variant: e.n === current ? 'primary' : 'subtle', ariaLabel: e.n, pressed: e.n === current });
      b.title = e.n;
      return b;
    }));
  };
  search.addEventListener('input', draw);
  cat.addEventListener('change', draw);
  const dlg = openDialog(t('icon.pick'), h('div', { class: 'ew-iconpick' },
    h('div', { class: 'ew-iconpick-filters' }, field(t('icon.search'), search), field(t('icon.category'), cat)), status, grid), [], { wide: true });
  status.textContent = t('icon.loading');
  void iconIndex().then((list) => {
    all = list;
    const cats = [...new Set(list.map((e) => e.c).filter(Boolean))].sort();
    cat.append(...cats.map((c) => h('option', { value: c }, c)));
    draw();
    search.focus();
  });
}

/** The control of an icon parameter in the dialog of a macro. */
export function iconControl(value: string, ctx: { label: string; required: boolean; params: () => Record<string, string> }): { el: HTMLElement; get: () => string } {
  let name = value;
  const id = uid('icon');
  const preview = h('span', { class: 'ew-icon ew-icon--l', 'aria-hidden': 'true' });
  const shown = h('code', { id });
  const show = () => {
    const url = iconURL(name, ctx.params().style || 'outline');
    preview.style.setProperty('--ew-icon', url ? `url("${url}")` : 'none');
    shown.textContent = name || t('icon.none');
  };
  const open = button(t('icon.pick'), () => pickIcon(ctx.params().style || 'outline', name, (n) => { name = n; show(); }), { variant: 'secondary', describedBy: id });
  show();
  return {
    el: h('div', { class: 'ew-field' }, h('span', { class: 'ew-label' }, ctx.label + (ctx.required ? ' *' : '')), h('div', { class: 'ew-actions' }, preview, shown, open)),
    get: () => name,
  };
}
