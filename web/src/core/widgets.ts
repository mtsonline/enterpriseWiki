// Widgets hydrate server-rendered placeholders (<div data-ew-widget="name">)
// inside content, e.g. the interactive part of a workflow element.

export type WidgetFactory = (el: HTMLElement) => void | Promise<void>;

const widgets = new Map<string, WidgetFactory>();

/** Registers a widget for placeholders with data-ew-widget="name". */
export function registerWidget(name: string, factory: WidgetFactory): void {
  widgets.set(name, factory);
}

/** Activates all widget placeholders below root (each only once). */
export function hydrate(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('[data-ew-widget]').forEach((el) => {
    if (el.dataset.ewHydrated) return;
    const f = widgets.get(el.dataset.ewWidget ?? '');
    if (!f) return;
    el.dataset.ewHydrated = '1';
    Promise.resolve(f(el)).catch((e) => console.error(`widget ${el.dataset.ewWidget} failed`, e));
  });
}
