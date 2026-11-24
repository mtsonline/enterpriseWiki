// The block "Light and dark": three buttons (as the device is set, light,
// dark). The pressed one is the scheme that applies (the choice, else the
// default of the template). With a template that has light colours only the
// block says so instead of offering a choice that would change nothing.
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { chooseScheme, onSchemeChange, schemeOffered, schemeSetting, SCHEMES, type Scheme } from '../core/scheme';
import { button } from './ui';

export function schemeBlock(): HTMLElement {
  const label = t('scheme.label');
  const box = h('div', { class: 'ew-scheme', role: 'group', 'aria-label': label });
  const buttons = new Map<Scheme, HTMLButtonElement>();
  for (const s of SCHEMES) {
    const b = button(t(`scheme.short.${s}`), async () => {
      // pressing the scheme that applies again keeps it as the own choice
      await chooseScheme(s);
    }, { variant: 'subtle', pressed: false });
    b.title = t(`scheme.${s}`);
    buttons.set(s, b);
  }
  const only = h('p', { class: 'ew-scheme-only' }, t('scheme.light_only'));
  box.append(...buttons.values(), only);
  const update = () => {
    if (!box.isConnected && box.dataset.drawn) {
      stop();
      return;
    }
    box.dataset.drawn = '1';
    const offered = schemeOffered();
    const cur = schemeSetting();
    for (const [s, b] of buttons) {
      b.hidden = !offered;
      b.setAttribute('aria-pressed', String(cur === s));
    }
    only.hidden = offered;
  };
  const stop = onSchemeChange(update);
  update();
  return box;
}
