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
    // a widget that claimed the controls of its page and ended (or failed)
    // without handing them over must not keep the page waiting
    const id = Number(el.dataset.element) || 0;
    let run: void | Promise<void>;
    try { run = f(el); } catch (e) { run = Promise.reject(e); } // started at once: it claims synchronously
    Promise.resolve(run)
      .catch((e) => console.error(`widget ${el.dataset.ewWidget} failed`, e))
      .finally(() => { if (id) settleWidgetControls(id, el); });
  });
}

// ---------------------------------------------------------------- controls
// On its own page, a widget (blog, FAQ …) hands its controls (search, "new
// post" …) to the page: they are element blocks there, placed like any other
// block. Embedded in another page, the widget shows them itself.

/** Creates one control (each call a new node: a block can be placed twice);
 * page is the page it is shown on. */
export type WidgetControl = (page: { title: string }) => Node | null;

interface Pending { root: HTMLElement | null; done: boolean; resolve: (c: Record<string, WidgetControl>) => void; promise: Promise<Record<string, WidgetControl>> }
const pending = new Map<number, Pending>();
const none: Promise<Record<string, WidgetControl>> = Promise.resolve({});

/** The page of element id is being shown: its widget may hand over its controls. */
export function expectWidgetControls(id: number): void {
  let resolve!: Pending['resolve'];
  const promise = new Promise<Record<string, WidgetControl>>((r) => { resolve = r; });
  pending.set(id, { root: null, done: false, resolve, promise });
}

/** Called by a widget at its start (before awaiting anything): true if it
 * is the widget of the page shown, which then hands its controls over with
 * provideWidgetControls instead of showing them. */
export function claimWidgetControls(id: number, root: HTMLElement): boolean {
  const p = pending.get(id);
  if (!p || p.root || p.done) return false;
  p.root = root;
  return true;
}

/** Hands the controls of the widget over (a claimed widget calls it once). */
export function provideWidgetControls(id: number, root: HTMLElement, controls: Record<string, WidgetControl>): void {
  const p = pending.get(id);
  if (!p || p.root !== root || p.done) return;
  p.done = true;
  p.resolve(controls);
}

/** Ends the waiting without controls: no widget claimed them, or the
 * claiming widget ended without handing any over. */
export function settleWidgetControls(id: number, root: HTMLElement | null = null): void {
  const p = pending.get(id);
  if (!p || p.done || p.root !== root) return;
  p.done = true;
  p.resolve({});
}

/** Whether a widget claimed the controls of element id. */
export function widgetControlsClaimed(id: number): boolean {
  return !!pending.get(id)?.root;
}

/** The controls of the widget of element id (empty if there is none). */
export function widgetControls(id: number): Promise<Record<string, WidgetControl>> {
  return pending.get(id)?.promise ?? none;
}
