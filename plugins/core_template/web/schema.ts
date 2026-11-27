// The schematic of a template of data (Administration → Templates): its rows
// and blocks as the engine builds them in each view (viewOf), with the
// blocks behind a button or hidden there. The views follow the width of the
// window, as the CSS of the engine does.
import { MAIN, t, type SchemaRow, type TemplateSchema } from '@ewiki/coreplugin';
import { LIMITS, pick, viewOf, type Design, type ViewName } from './engine';

/** The view of the engine for a width of window. */
export function viewForWidth(width: number): ViewName {
  return width >= LIMITS.views.desktop.min_px ? 'desktop' : width >= LIMITS.views.tablet.min_px ? 'tablet' : 'mobile';
}

/** The schematic of a design. */
export function designSchema(d: Design): TemplateSchema {
  return (size) => {
    const view = viewForWidth(size.width);
    return d.rows.map((r): SchemaRow => {
      const v = viewOf(d, r, view);
      return {
        role: r.role,
        columns: v.stack ? 1 : undefined,
        areas: r.blocks.map((b) => ({
          kind: b.kind,
          positions: b.kind === 'content' ? [...b.positions, MAIN, ...(b.after ?? [])] : b.positions,
          // a fixed width in rem against the content, which takes the rest
          grow: b.kind === 'content' ? 3 : b.width > 0 ? Math.max(0.5, b.width / 16) : 1,
          behind: v.drawer.includes(b.id) ? pick(b.label) || (b.role === 'navigation' ? t('nav.menu') : t('template.more')) : undefined,
          hidden: v.hidden.includes(b.id) || undefined,
        })),
      };
    });
  };
}
