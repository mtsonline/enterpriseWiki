// A tile of the own dashboard: a section with a heading, its content and an
// optional link to everything.
import { h, uid } from '../core/dom';

export function tile(title: string, content: Node, more?: { href: string; label: string }): HTMLElement {
  const id = uid('tile');
  return h('section', { class: 'ew-tile-body', 'aria-labelledby': id },
    h('h2', { id, class: 'ew-tile-title' }, title), content,
    more ? h('p', { class: 'ew-tile-more' }, h('a', { href: more.href }, more.label)) : null);
}
