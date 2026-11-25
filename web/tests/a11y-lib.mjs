// Accessibility checks shared by the browser tests (e2e.test.mjs) and the
// quick local run (a11y-quick.mjs): axe-core with the WCAG 2.2 AA rules and
// our own overlap check.
//
// Why an own check: axe's target-size rule measures how much of a control is
// still visible next to whatever covers it, and passes from 24 px on. Whether
// a covered control ends at 31 px or at 23.9 px depends on the text widths of
// the machine, so a covered control could pass locally and fail in the CI
// (the focused skip link over the link to the start page, PR #225). The
// overlap check fails on any covering, independent of pixels.
import assert from 'node:assert/strict';

let axeSource = null;
try {
  axeSource = (await import('axe-core')).default.source;
} catch { /* reported by the callers that require axe */ }
export { axeSource };

export const AXE_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

/** Runs axe on the page; returns the report lines (empty: no violations). */
export async function axeViolations(pg) {
  if (!axeSource) return [];
  await pg.addScriptTag({ content: axeSource });
  const res = await pg.evaluate(async (tags) => {
    const r = await globalThis.axe.run(document, { runOnly: tags });
    // details for the report: what axe says and which element is on top of the node
    for (const v of r.violations) for (const n of v.nodes) {
      const el = document.querySelector(n.target.join(' '));
      const b = el?.getBoundingClientRect();
      const top = b ? document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2) : null;
      n.detail = `${(n.failureSummary ?? '').replace(/\s+/g, ' ').slice(0, 300)}${b ? ` [box ${Math.round(b.x)},${Math.round(b.y)} ${Math.round(b.width)}x${Math.round(b.height)}, on top: ${top?.outerHTML.slice(0, 120) ?? '-'}]` : ''}`;
    }
    return r.violations;
  }, AXE_TAGS);
  const v = res.map((x) => `${x.id} (${x.impact}): ${x.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(', ')}`);
  const details = res.flatMap((x) => x.nodes.slice(0, 3).map((n) => `  ${n.target.join(' ')}: ${n.detail}`));
  return v.length ? [...v, ...details] : [];
}

/**
 * Finds controls covered by other elements (runs in the page).
 *
 * Every visible control in the viewport (links, buttons, form fields,
 * summary, focusable or ARIA-interactive elements) is hit-tested at five
 * points: the centre and the four corners, 2 px inside the part of it that is
 * not clipped by a scrolling ancestor or the viewport. A point is covered when
 * the topmost element there is neither the control, nor inside it, nor one of
 * its ancestors, nor one of its labels.
 *
 * A link that wraps over several lines is tested line by line (its outer box
 * also holds the text next to it).
 *
 * Not covering, by design:
 * - ancestors on top: a rounded corner or a gap lets the parent through;
 * - the labels of the control: a styled checkbox or a file input lies under
 *   its own label;
 * - elements with pointer-events: none (clicks go through them);
 * - popups the user opened on purpose: what lies in the element named by
 *   aria-controls of a control with aria-expanded="true" (menus, the panel of
 *   the notifications, the navigation of the phone layout), in an open
 *   popover or in a listbox, menu or tooltip (suggestions while typing);
 * - a fixed, named region or dialog that stays until the user decides (the
 *   cookie banner; WCAG 2.4.11 allows content the user can dismiss);
 * - controls outside an open modal dialog (inert while it is open) and inside
 *   [inert] or closed dialogs/popovers (not rendered);
 * - visually hidden controls (.sr-only: 1 px box or clipped) and controls
 *   smaller than 4 px.
 * A sticky header over scrolled content is no exception: the caller scrolls
 * to the top first, where nothing is under the header.
 *
 * Besides covering, controls that are partly covered are reported with the
 * size that is left; MIN_VISIBLE (28 px) is our own margin above the 24 px of
 * WCAG 2.5.8, so near misses fail locally too.
 */
function findCovered() {
  const SEL = ['a[href]', 'area[href]', 'button', 'input:not([type=hidden])', 'select', 'textarea', 'summary',
    '[tabindex]:not([tabindex="-1"])', '[contenteditable=""]', '[contenteditable="true"]',
    ...['button', 'link', 'checkbox', 'radio', 'switch', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'option', 'treeitem', 'slider', 'spinbutton', 'combobox', 'textbox', 'searchbox'].map((r) => `[role=${r}]`)].join(',');
  const INSET = 2;
  const vw = document.documentElement.clientWidth;
  const vh = document.documentElement.clientHeight;
  const modals = [...document.querySelectorAll('dialog')].filter((d) => d.matches(':modal'));
  const modal = modals[modals.length - 1] ?? null;
  const short = (el) => {
    if (!el) return '-';
    const o = el.outerHTML;
    const tag = o.slice(0, o.indexOf('>') + 1);
    return (tag.length > 140 ? `${tag.slice(0, 140)}…` : tag).replace(/\s+/g, ' ');
  };
  const path = (el) => {
    const parts = [];
    for (let e = el; e && e !== document.body && parts.length < 4; e = e.parentElement) {
      let s = e.localName;
      if (e.id) { parts.unshift(`${s}#${CSS.escape(e.id)}`); break; }
      const cls = [...e.classList].slice(0, 2).map((c) => `.${CSS.escape(c)}`).join('');
      parts.unshift(s + cls);
    }
    return parts.join(' > ');
  };
  // the part of el that is not clipped by scrolling/clipping ancestors or the viewport
  const visibleRect = (el) => {
    const r = el.getBoundingClientRect();
    let x1 = Math.max(r.left, 0), y1 = Math.max(r.top, 0), x2 = Math.min(r.right, vw), y2 = Math.min(r.bottom, vh);
    for (let a = el.parentElement; a && a !== document.documentElement; a = a.parentElement) {
      const cs = getComputedStyle(a);
      if (cs.overflowX !== 'visible' || cs.overflowY !== 'visible' || cs.contain.includes('paint')) {
        const b = a.getBoundingClientRect();
        if (cs.overflowX !== 'visible') { x1 = Math.max(x1, b.left + a.clientLeft); x2 = Math.min(x2, b.left + a.clientLeft + a.clientWidth); }
        if (cs.overflowY !== 'visible') { y1 = Math.max(y1, b.top + a.clientTop); y2 = Math.min(y2, b.top + a.clientTop + a.clientHeight); }
      }
      if (cs.position === 'fixed') break;
    }
    return { x1, y1, x2, y2, w: x2 - x1, h: y2 - y1, full: r };
  };
  // popups opened on purpose and fixed banners: what they cover is not a finding
  const opened = [...document.querySelectorAll('[aria-expanded="true"][aria-controls]')]
    .flatMap((c) => c.getAttribute('aria-controls').split(/\s+/)).map((id) => document.getElementById(id)).filter(Boolean);
  const popup = (hit) => {
    if (opened.some((o) => o.contains(hit))) return true;
    if (hit.closest('[popover]:popover-open, [role=listbox], [role=menu], [role=tooltip]')) return true;
    for (let e = hit; e && e !== document.body; e = e.parentElement) {
      if (getComputedStyle(e).position !== 'fixed') continue;
      if (e.matches('dialog, [role=dialog], [role=alertdialog], [role=region], section[aria-labelledby], section[aria-label], aside[aria-labelledby], aside[aria-label]')) return true;
    }
    return false;
  };
  const out = [];
  for (const el of document.querySelectorAll(SEL)) {
    if (el.closest('[inert], [aria-hidden="true"]')) continue;
    if (modal && !modal.contains(el)) continue;
    if (el.matches(':disabled')) continue;
    if (!el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) {
      // a transparent input under its label (styled checkbox, file button) is checked through the label
      continue;
    }
    if (el.closest('.sr-only')) continue;
    const v = visibleRect(el);
    if (v.full.width < 4 || v.full.height < 4 || v.w < 4 || v.h < 4) continue;
    const labels = [...(el.labels ?? [])];
    const ok = (hit) => !hit || hit === el || el.contains(hit) || hit.contains(el) || labels.some((l) => l === hit || l.contains(hit)) || popup(hit);
    // the line boxes of a link that wraps, clipped like the whole element
    const rects = el.getClientRects().length > 1 && getComputedStyle(el).display === 'inline'
      ? [...el.getClientRects()].slice(0, 4).map((r) => ({ x1: Math.max(r.left, v.x1), y1: Math.max(r.top, v.y1), x2: Math.min(r.right, v.x2), y2: Math.min(r.bottom, v.y2) }))
        .map((r) => ({ ...r, w: r.x2 - r.x1, h: r.y2 - r.y1 })).filter((r) => r.w >= 4 && r.h >= 4)
      : [v];
    const pts = rects.flatMap((r) => {
      const ins = Math.min(INSET, r.w / 4, r.h / 4);
      return [[(r.x1 + r.x2) / 2, (r.y1 + r.y2) / 2], [r.x1 + ins, r.y1 + ins], [r.x2 - ins, r.y1 + ins], [r.x1 + ins, r.y2 - ins], [r.x2 - ins, r.y2 - ins]];
    });
    const covered = [];
    for (const [x, y] of pts) {
      const hit = document.elementFromPoint(x, y);
      if (!ok(hit)) covered.push({ x: Math.round(x), y: Math.round(y), by: hit });
    }
    if (!covered.length) continue;
    // what is left of the control next to the covering elements (on the axes through the centre)
    const cx = (v.x1 + v.x2) / 2, cy = (v.y1 + v.y2) / 2;
    let left = 0, top = 0;
    for (let x = Math.ceil(v.x1); x < v.x2; x++) if (ok(document.elementFromPoint(x, cy))) left++;
    for (let y = Math.ceil(v.y1); y < v.y2; y++) if (ok(document.elementFromPoint(cx, y))) top++;
    const by = [...new Set(covered.map((c) => c.by))];
    out.push(`${path(el)} ${short(el)} [box ${Math.round(v.full.x)},${Math.round(v.full.y)} ${Math.round(v.full.width)}x${Math.round(v.full.height)}; ` +
      `covered at ${covered.map((c) => `${c.x},${c.y}`).join(' ')}; visible about ${left}x${top} px${left < 28 || top < 28 ? ' (< 28)' : ''}] by ${by.map((b) => `${path(b)} ${short(b)}`).join(' | ')}`);
  }
  return out;
}

/** Controls covered by other elements on the page (empty: none). */
export function coveredControls(pg) {
  return pg.evaluate(findCovered);
}

/** Waits until the transient toasts (fixed, bottom right, may cover controls) are gone. */
export async function noToasts(pg) {
  await pg.waitForFunction(() => !document.querySelector('.ew-toasts > *'), null, { timeout: 15000 }).catch(() => undefined);
}

/**
 * The full check of a page state: axe and the overlap check, from the top of
 * the page (Playwright scrolls controls into view without scroll-padding,
 * which can leave content half below the sticky header in the snapshot).
 * Throws an assertion error with all findings.
 */
export async function checkPage(name, pg) {
  await noToasts(pg);
  await pg.evaluate(() => window.scrollTo(0, 0));
  const axe = await axeViolations(pg);
  const cov = await coveredControls(pg);
  const msg = [];
  if (axe.length) msg.push(`accessibility violations on ${name}:`, ...axe);
  if (cov.length) msg.push(`covered controls on ${name} (another element lies on top; see web/tests/a11y-lib.mjs):`, ...cov.map((c) => `  ${c}`));
  assert.deepEqual(msg, [], msg.join('\n'));
}

/**
 * Focuses the skip link like a keyboard user (Tab from the start of the page)
 * and checks that the focused link covers no control.
 */
export async function checkSkipLink(name, pg) {
  await noToasts(pg);
  await pg.evaluate(() => { window.scrollTo(0, 0); document.activeElement?.blur?.(); });
  // Tab from the start of the document
  await pg.evaluate(() => {
    const s = document.createElement('span');
    s.tabIndex = -1;
    document.body.prepend(s);
    s.focus();
    s.remove();
  });
  await pg.keyboard.press('Tab');
  const focused = await pg.evaluate(() => document.activeElement?.className ?? '');
  assert.equal(focused, 'ew-skip', `${name}: the skip link is the first focusable element`);
  const cov = await coveredControls(pg);
  assert.deepEqual(cov, [], `covered controls on ${name} with the skip link focused:\n${cov.map((c) => `  ${c}`).join('\n')}`);
  await pg.evaluate(() => document.activeElement?.blur?.());
}
