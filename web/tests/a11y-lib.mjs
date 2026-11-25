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
    // text and font help when the layout differs between machines (CI)
    const text = (el.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 60);
    const cs = getComputedStyle(el);
    out.push(`${path(el)} ${short(el)}${text ? ` "${text}"` : ''} (${cs.fontSize} ${cs.fontFamily.split(',')[0]}) [box ${Math.round(v.full.x)},${Math.round(v.full.y)} ${Math.round(v.full.width)}x${Math.round(v.full.height)}; ` +
      `covered at ${covered.map((c) => `${c.x},${c.y}`).join(' ')}; visible about ${left}x${top} px${left < 28 || top < 28 ? ' (< 28)' : ''}] by ${by.map((b) => `${path(b)} ${short(b)}`).join(' | ')}`);
  }
  return out;
}

/** Controls covered by other elements on the page (empty: none). */
export function coveredControls(pg) {
  return pg.evaluate(findCovered);
}

/**
 * Finds content that does not fit (runs in the page):
 * - the page is wider than the window (a horizontal scroll bar; WCAG 1.4.10).
 *   Named are the outermost elements that reach past the right edge and are
 *   not inside a box that scrolls or clips (a table in .ew-table-wrap
 *   scrolls by design);
 * - text cut off by a box that clips it (overflow hidden or clip, ellipsis,
 *   line clamp) without the full text in a title of the box or of the
 *   link/button around it. Boxes that scroll (overflow auto/scroll) are no
 *   finding, nor are visually hidden ones (.sr-only).
 */
function findOverflow() {
  const out = [];
  const vw = document.documentElement.clientWidth;
  const short = (el) => {
    const o = el.outerHTML;
    const tag = o.slice(0, o.indexOf('>') + 1);
    return (tag.length > 120 ? `${tag.slice(0, 120)}…` : tag).replace(/\s+/g, ' ');
  };
  const text = (el) => (el.textContent ?? '').replace(/\s+/g, ' ').trim();
  const hidden = (el) => !el.checkVisibility({ visibilityProperty: true }) || el.closest('.sr-only, [aria-hidden="true"], [inert]');
  const clips = (cs) => cs.overflowX !== 'visible' || cs.overflowY !== 'visible' || cs.contain.includes('paint');
  // 1. wider than the window
  if (document.documentElement.scrollWidth > vw + 1) {
    const wide = [];
    const walk = (el) => {
      for (const c of el.children) {
        if (hidden(c)) continue;
        const r = c.getBoundingClientRect();
        const cs = getComputedStyle(c);
        if (r.right > vw + 1 && r.width > 0 && cs.position !== 'fixed') { wide.push(c); continue; }
        if (!clips(cs)) walk(c);
      }
    };
    walk(document.body);
    // nothing outside a scrolling box: an absolutely positioned element escapes
    // one (e.g. .sr-only text in a table whose box is not positioned); name
    // the innermost elements past the edge
    if (!wide.length) {
      for (const e of document.body.querySelectorAll('*')) {
        if (e.getBoundingClientRect().right <= vw + 1) continue;
        if ([...e.children].some((c) => c.getBoundingClientRect().right > vw + 1)) continue;
        wide.push(e);
      }
    }
    out.push(`the page is ${document.documentElement.scrollWidth} px wide in a window of ${vw} px; too wide: ` +
      wide.slice(0, 5).map((e) => `${short(e)} "${text(e).slice(0, 40)}" (right edge ${Math.round(e.getBoundingClientRect().right)})`).join(' | '));
  }
  // 2. text cut off by its box
  for (const el of document.body.querySelectorAll('*')) {
    if (el.children.length > 20) continue; // a container, not a box of text
    const cs = getComputedStyle(el);
    const hx = cs.overflowX === 'hidden' || cs.overflowX === 'clip';
    const hy = cs.overflowY === 'hidden' || cs.overflowY === 'clip';
    const clamp = cs.webkitLineClamp && cs.webkitLineClamp !== 'none';
    if (!hx && !hy && !clamp) continue;
    if (!text(el) || hidden(el)) continue;
    const own = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
    if (!own && cs.textOverflow !== 'ellipsis' && !clamp) continue; // e.g. a card that rounds its corners
    if (el.clientWidth < 2 || el.clientHeight < 2) continue;
    const cutX = hx && el.scrollWidth > el.clientWidth + 1;
    const cutY = (hy || clamp) && el.scrollHeight > el.clientHeight + 1;
    if (!cutX && !cutY) continue;
    const full = text(el);
    const titled = el.closest('[title]')?.getAttribute('title')?.includes(full.slice(0, 30));
    if (titled) continue;
    out.push(`text cut off ${cutX ? 'at the side' : 'at the bottom'}: ${short(el)} "${full.slice(0, 60)}" (shows ${el.clientWidth}x${el.clientHeight} of ${el.scrollWidth}x${el.scrollHeight} px, no title with the full text)`);
  }
  return out;
}

/** Content that does not fit (empty: none). */
export function overflowFindings(pg) {
  return pg.evaluate(findOverflow);
}

/**
 * Variants of the presentation the interface must survive, whatever font a
 * template chooses or a person sets:
 * - spacing: the text spacing of WCAG 1.4.12 (line height 1.5, letter
 *   spacing .12em, word spacing .16em, paragraph spacing 2em);
 * - text200: text at 200 % (WCAG 1.4.4; the size of the root font, as the
 *   font size setting of a browser does), from 800 px window width on (at
 *   390 px it would be a window of 195 px, below what WCAG 1.4.10 asks);
 * - wide / narrow: a wide and a narrow font instead of the one of the template
 *   (fonts of the system; where none is installed the next in the list).
 */
const FONT_RULE = (stack) => `body, body *:not(code):not(pre):not(kbd):not(samp):not(svg *) { font-family: ${stack} !important; }`;
export const VARIANTS = {
  spacing: { css: '* { line-height: 1.5 !important; letter-spacing: .12em !important; word-spacing: .16em !important; } p { margin-bottom: 2em !important; }' },
  text200: { css: 'html { font-size: 200% !important; }', minWidth: 800 },
  wide: { css: FONT_RULE('"DejaVu Sans", Verdana, "Liberation Mono", monospace') },
  narrow: { css: FONT_RULE('"DejaVu Sans Condensed", "Liberation Sans Narrow", "Arial Narrow", "Source Sans 3 Variable", sans-serif') },
};

/** Applies a variant to the loaded page (until the next navigation). */
export async function applyVariant(pg, name) {
  await pg.addStyleTag({ content: VARIANTS[name].css });
  // the layout settles: fonts of the system load at once, a frame to be sure
  await pg.evaluate(() => document.fonts.ready.then(() => new Promise((r) => requestAnimationFrame(() => r()))));
}

/**
 * Checks a page in every variant at the given widths (navigating to path for
 * each, so the variants do not add up). Returns the findings.
 */
export async function variantFindings(name, pg, path, widths = [390, 800, 1280], variants = Object.keys(VARIANTS)) {
  const found = [];
  const vp = pg.viewportSize();
  try {
    for (const v of variants) {
      for (const width of widths) {
        if (width < (VARIANTS[v].minWidth ?? 0)) continue;
        await pg.setViewportSize({ width, height: 900 });
        await pg.goto(new URL(path, pg.url()).href);
        await pg.waitForLoadState('networkidle');
        await pg.waitForFunction(() => !document.querySelector('main[aria-busy="true"]'));
        await applyVariant(pg, v);
        try { await checkPage(`${name} (${path}, ${width} px, ${v})`, pg); } catch (e) { found.push(e.message); }
      }
    }
  } finally {
    // back to the page as it was, without the styles of the last variant
    if (vp) await pg.setViewportSize(vp);
    await pg.goto(new URL(path, pg.url()).href);
    await pg.waitForLoadState('networkidle');
    await pg.waitForFunction(() => !document.querySelector('main[aria-busy="true"]'));
  }
  return found;
}

/** Waits until the transient toasts (fixed, bottom right, may cover controls) are gone. */
export async function noToasts(pg) {
  await pg.waitForFunction(() => !document.querySelector('.ew-toasts > *'), null, { timeout: 15000 }).catch(() => undefined);
}

/**
 * The full check of a page state: axe, the overlap check and the overflow check, from the top of
 * the page (Playwright scrolls controls into view without scroll-padding,
 * which can leave content half below the sticky header in the snapshot).
 * Throws an assertion error with all findings.
 */
export async function checkPage(name, pg) {
  await noToasts(pg);
  await pg.evaluate(() => window.scrollTo(0, 0));
  const axe = await axeViolations(pg);
  const cov = await coveredControls(pg);
  const ovf = await overflowFindings(pg);
  const msg = [];
  if (axe.length) msg.push(`accessibility violations on ${name}:`, ...axe);
  if (cov.length) msg.push(`covered controls on ${name} (another element lies on top; see web/tests/a11y-lib.mjs):`, ...cov.map((c) => `  ${c}`));
  if (ovf.length) msg.push(`content that does not fit on ${name} (see findOverflow in web/tests/a11y-lib.mjs):`, ...ovf.map((c) => `  ${c}`));
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
