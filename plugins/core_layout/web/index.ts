// core.layout: the styles of the layout blocks, and the tabs: the server
// renders one section per part (readable without script); here they become
// a tablist after the WAI-ARIA pattern (arrow keys, Home, End).
import { h, registerWidget, uid } from '@ewiki/coreplugin';
import './style.css';

export function register(): void {
  registerWidget('ewl.tabs', (root) => {
    const parts = [...root.querySelectorAll<HTMLElement>(':scope > .ewl-tab')];
    if (parts.length < 1) return;
    const list = h('div', { role: 'tablist', class: 'ewl-tablist', 'aria-label': root.dataset.label || undefined });
    const tabs = parts.map((part, i) => {
      const name = part.querySelector<HTMLElement>(':scope > .ewl-tab-name')?.textContent ?? String(i + 1);
      const tabId = uid('ewl-tab');
      part.setAttribute('role', 'tabpanel');
      part.setAttribute('aria-labelledby', tabId);
      part.tabIndex = 0;
      const tab = h('button', { type: 'button', role: 'tab', id: tabId, 'aria-controls': part.id, 'aria-selected': 'false', tabindex: '-1' }, name) as HTMLButtonElement;
      tab.addEventListener('click', () => select(i, false));
      return tab;
    });
    const select = (i: number, focus: boolean) => {
      tabs.forEach((t, j) => {
        t.setAttribute('aria-selected', String(i === j));
        t.tabIndex = i === j ? 0 : -1;
        parts[j].hidden = i !== j;
      });
      if (focus) tabs[i].focus();
    };
    list.addEventListener('keydown', (ev) => {
      const at = tabs.indexOf(document.activeElement as HTMLButtonElement);
      if (at < 0) return;
      const to = ev.key === 'ArrowRight' ? (at + 1) % tabs.length : ev.key === 'ArrowLeft' ? (at - 1 + tabs.length) % tabs.length
        : ev.key === 'Home' ? 0 : ev.key === 'End' ? tabs.length - 1 : -1;
      if (to < 0) return;
      ev.preventDefault();
      select(to, true);
    });
    list.append(...tabs);
    root.prepend(list);
    root.classList.add('ewl-tabs--ready');
    // a link to one part (#ewl-tab-…) opens its tab
    const hash = location.hash.slice(1);
    const start = Math.max(0, parts.findIndex((p) => p.id === hash));
    select(start, false);
  });
}
