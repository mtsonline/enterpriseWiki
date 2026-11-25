// Browser end-to-end tests: real user flows in Chromium plus automated WCAG
// 2.2 AA checks with axe-core.
//
//   EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=... npm run test:e2e
//
// The server must have been set up with "ewiki setup" (welcome space).
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createHmac, generateKeyPairSync } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import net from 'node:net';
import http from 'node:http';
import { axeSource, checkPage, checkSkipLink, variantFindings, layoutShift, CLS_LIMIT } from './a11y-lib.mjs';

const BASE = process.env.EWIKI_E2E_URL ?? 'http://localhost:8080';
const USER = process.env.EWIKI_E2E_USER ?? 'admin';
const PASSWORD = process.env.EWIKI_E2E_PASSWORD ?? 'Admin-Passw0rd!';
const REQUIRE_AXE = process.env.EWIKI_E2E_REQUIRE_AXE === '1';
const run = Date.now().toString(36);

if (!axeSource && REQUIRE_AXE) throw new Error('axe-core is required (npm install)');

let browser, page;
const errors = [];

// Interface coverage: every API request the pages send is recorded and
// mapped to the actions of the register (tools/uicoverage). Requests the test
// sends itself from a page go through __e2eFetch, which marks them, so they
// do not count as use of the interface.
const DIRECT = 'X-Ewiki-E2E';
const uiRequests = [];
const markDirect = () => {
  // same origin only: a custom header would turn a cross-origin call into a preflight
  window.__e2eFetch = (u, o = {}) => {
    const url = new URL(u instanceof Request ? u.url : String(u), location.href);
    if (url.origin !== location.origin) return fetch(u, o); // e2e: the marking wrapper
    const headers = new Headers(o.headers ?? {});
    headers.set('X-Ewiki-E2E', 'direct');
    return fetch(u, { ...o, headers }); // e2e: the marking wrapper
  };
};
function recordContext(ctx) {
  ctx.addInitScript(markDirect);
  ctx.on('response', (res) => {
    const req = res.request();
    const url = new URL(req.url());
    if (!url.pathname.startsWith('/api/') || req.headers()[DIRECT.toLowerCase()]) return;
    uiRequests.push({ method: req.method(), path: url.pathname, status: res.status() });
  });
}

before(async () => {
  browser = await chromium.launch();
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (opts) => { const ctx = await newContext(opts); await recordContext(ctx); return ctx; };
  // like Browser.newPage: a context of its own that closes with the page
  browser.newPage = async (opts) => { const ctx = await browser.newContext(opts); const p = await ctx.newPage(); p.on('close', () => { ctx.close().catch(() => {}); }); return p; };
  // bypassCSP: the strict CSP of the wiki (script-src 'self') would block injecting axe-core
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error' && !/status of 40[134]/.test(m.text())) errors.push(m.text()); });
});
after(async () => {
  await browser?.close();
  if (process.env.EWIKI_E2E_UI_REQUESTS) writeFileSync(process.env.EWIKI_E2E_UI_REQUESTS, JSON.stringify(uiRequests));
});

test('requests of the test itself are marked, so they do not count as interface coverage', () => {
  const src = readFileSync(new URL(import.meta.url), 'utf8');
  // a bare fetch in a page would be counted as use of the interface
  const bare = src.split('\n').filter((l) => /(?<![\w.$])fetch\(/.test(l) && !l.includes('// e2e: the marking wrapper')); // e2e: the marking wrapper
  assert.deepEqual(bare, []);
});

// axe (WCAG 2.2 AA) and the overlap check (no control covered by another
// element), from the top of the page; see a11y-lib.mjs
async function a11y(name, pg = page) {
  if (!axeSource) return;
  await checkPage(name, pg);
}

// the same on a key page in the variants any font or setting may bring (text
// spacing of WCAG 1.4.12, text at 200 %, a wide and a narrow font) at 390, 800
// and 1280 px; only on a few pages, the quick run (npm run test:a11y) has more
async function a11yVariants(name, path, pg = page) {
  if (!axeSource) return;
  const found = await variantFindings(name, pg, path);
  assert.deepEqual(found, [], found.join('\n\n'));
}

/**
 * Chooses the option of a picker list whose text contains name. The list
 * can hold other matches first (more content, e.g. the demo), so picking
 * the first option would choose a different element.
 */
async function pickOption(list, name) {
  const idx = await list.evaluate((s, n) => [...s.options].findIndex((o) => o.textContent.includes(n)), name);
  assert.ok(idx >= 0, `no option with ${name}`);
  await list.selectOption({ index: idx });
}

async function idle() {
  await page.waitForLoadState('networkidle');
  await page.waitForFunction(() => !document.querySelector('main[aria-busy="true"]'));
}

test('guest sees the public first level and an accessible page', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.waitForSelector('main h1');
  assert.ok(await page.locator('a.ew-skip').count(), 'skip link present');
  assert.equal(await page.locator('html').getAttribute('lang') !== null, true);
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(() => document.activeElement?.className), 'ew-skip', 'skip link is the first focusable element');
  await a11y('guest start page');
  // the focused skip link must not cover the header (PR #225: it covered the link to the start page)
  await checkSkipLink('guest start page', page);
  await a11yVariants('guest start page', '/');
});

test('login', async () => {
  await page.goto(BASE + '/login');
  await idle();
  await page.getByLabel(/user ?name|benutzername/i).fill(USER);
  await page.getByLabel(/^(password|passwort)/i).fill('wrong-password');
  await page.locator('form.ew-login button[type=submit]').click();
  await page.waitForSelector('.ew-form-alert:not(:empty)');
  await a11y('login with error');
  await page.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
  await page.locator('form.ew-login button[type=submit]').click();
  await page.waitForSelector('.ew-user-menu');
  await idle();
  await checkSkipLink('start page after login', page);
});

test('create a subcategory: the current category is preselected as parent', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') await edit.click();
  await idle();
  const currentTitle = (await page.locator('main h1').innerText()).replace(/^[\d.]+\s*/, '').trim();
  await page.getByRole('button', { name: /new subcategory|neue unterkategorie/i }).click();
  const dialog = page.locator('dialog[open]');
  await dialog.waitFor();
  const selected = await dialog.locator('select').first().evaluate((s) => s.options[s.selectedIndex].text);
  assert.match(selected, new RegExp(currentTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  await a11y('new category dialog');
  await dialog.locator('input[lang="en"]').first().fill(`E2E ${run}`);
  await dialog.locator('button[type=submit]').click();
  await page.waitForFunction((t) => document.querySelector('main h1')?.textContent?.includes(t), `E2E ${run}`);
  await a11y('category in edit mode');
});

test('create, edit, translate and search a page', async () => {
  await page.getByRole('link', { name: /new page|neue seite/i }).click();
  await idle();
  await page.getByLabel(/^(title|titel)/i).fill(`Page ${run}`);
  const ed = page.locator('.ewe-prose');
  await ed.click();
  await page.keyboard.type(`Hello quokka${run} world`);
  await page.getByRole('button', { name: /^bold$|^fett$/i }).click();
  await page.keyboard.type(' bold');
  await a11y('page editor');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  assert.match(await page.locator('.ew-content').innerText(), new RegExp(`quokka${run}`));
  assert.ok(await page.locator('.ew-content strong, .ew-content b').count() > 0, 'bold text saved');
  await a11y('page view with translation panel');
  await a11yVariants('page view', new URL(page.url()).pathname);
  // translate into German
  const translate = page.locator('.ew-panel table a').filter({ hasText: /translate|übersetzen/i }).first();
  await translate.click();
  await idle();
  await page.getByLabel(/^(title|titel)/i).fill(`Seite ${run}`);
  await page.locator('.ewe-prose').fill(`Hallo quokka${run} Welt`);
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  // search
  await page.goto(`${BASE}/search?q=quokka${run}`);
  await idle();
  await page.waitForSelector('.ew-result');
  assert.ok(await page.locator('.ew-result mark').count() > 0, 'search hit highlighted');
  await a11y('search results');
  // history
  await page.locator('.ew-result a').first().click();
  await idle();
  await page.getByRole('link', { name: /^(history|verlauf)$/i }).first().click();
  await idle();
  assert.ok(await page.locator('table tbody tr').count() >= 2, 'history lists the versions');
  await a11y('history');
});

test('administration pages are accessible', async () => {
  // every page the administration offers (from its navigation, so new pages are checked too)
  await page.goto(`${BASE}/admin`);
  await idle();
  const sections = await page.locator('.ew-admin nav.ew-subnav a').evaluateAll((as) => as.map((a) => a.getAttribute('href')));
  assert.ok(sections.length >= 20, `administration pages: ${sections}`);
  for (const href of sections) {
    await page.goto(BASE + href);
    await idle();
    await page.waitForSelector('.ew-admin-panel h1');
    await a11y(href.slice(1));
  }
  // the insecure TLS setting of LDAP carries a permanent warning, announced with it
  await page.goto(`${BASE}/admin/ldap`);
  await idle();
  const insecure = page.getByRole('checkbox', { name: /do not verify the tls certificate|tls-zertifikat nicht prüfen/i });
  const described = await insecure.getAttribute('aria-describedby');
  assert.ok(described, 'warning not linked to the checkbox');
  const warning = page.locator(`[id="${described.split(' ').pop()}"]`);
  assert.match(await warning.innerText(), /read the passwords|passwörter aller personen mitlesen/i);
  assert.ok(await warning.evaluate((el) => el.classList.contains('ew-notice--warning')));
  await a11yVariants('licence', '/admin/license');
  await page.goto(`${BASE}/profile`);
  await idle();
  await a11y('profile');
  await a11yVariants('profile', '/profile');
  await page.goto(`${BASE}/trash`);
  await idle();
  await a11y('trash');
});

test('nothing moves while a page loads: layout shift below the limit, as a guest and signed in', async () => {
  // the layout shift of the browser (web vitals) per page, with an empty
  // cache: the toolbar, footer and blocks of a page, the editor and the
  // fonts arrive with the page, not after it (a click would land elsewhere)
  await page.goto(BASE + '/');
  await idle();
  const cat = await page.locator('.ew-tree a').first().getAttribute('href');
  await page.goto(BASE + cat);
  await idle();
  const el = await page.locator('main a[href^="/e/"]').first().getAttribute('href');
  assert.ok(el, 'a page in the first category');
  const found = [];
  for (const width of [390, 1280]) {
    for (const [who, paths] of [['guest', ['/', '/login']], ['signed in', ['/', el, `${el}/edit`, '/admin']]]) {
      const ctx = await browser.newContext({ viewport: { width, height: 900 }, ...(who === 'guest' ? {} : { storageState: await page.context().storageState() }) });
      try {
        for (const path of paths) {
          const r = await layoutShift(ctx, BASE + path);
          if (r.sum >= CLS_LIMIT) found.push(`${path} (${who}, ${width} px): layout shift ${r.sum.toFixed(3)} (limit ${CLS_LIMIT}): ${r.text}`);
        }
      } finally { await ctx.close(); }
    }
  }
  assert.deepEqual(found, [], found.join('\n'));
});

test('core plugins: origin check, warning, confirmation for external sources, trusted sources', async () => {
  // runs early: entering a trusted source needs a sign-in within 15 minutes
  await page.goto(BASE + '/admin/plugins');
  await idle();
  const install = async (source) => {
    const form = page.locator('form', { has: page.getByRole('heading', { name: /^(install a core plugin|core-plugin installieren)$/i }) });
    await form.getByLabel(/^(source|quelle)/i).fill(source);
    await form.locator('button[type=submit]').click();
    return page.getByRole('dialog', { name: /^(install core plugin|core-plugin installieren)$/i });
  };
  // an unknown source: warning, the box must be ticked
  let dlg = await install(`https://git.example.com/e2e-${run}/kanban`);
  await dlg.waitFor();
  await dlg.getByText(/same rights as the wiki|dieselben rechte wie das wiki/i).waitFor();
  await dlg.locator('.ew-badge', { hasText: /^(external|extern)$/i }).waitFor();
  const accept = dlg.getByRole('checkbox', { name: /install this core plugin anyway|installiere dieses core-plugin trotzdem/i });
  assert.equal(await accept.isChecked(), false);
  await a11y('external core plugin warning');
  await dlg.locator('button[type=submit]').click();
  await dlg.locator('.ew-form-alert', { hasText: /checkbox|kontrollkästchen/i }).waitFor();
  await dlg.getByRole('button', { name: /^(cancel|abbrechen)$/i }).click();
  await dlg.waitFor({ state: 'detached' });

  // enter the source as trusted: the same address is now the organisation's
  const panel = page.locator('section', { has: page.getByRole('heading', { name: /^(trusted sources|vertrauenswürdige quellen)$/i }) });
  await panel.getByLabel(/^(address or directory|adresse oder verzeichnis)/i).fill(`https://git.example.com/e2e-${run}`);
  await panel.getByLabel(/^(note|notiz)/i).fill('E2E team');
  await panel.getByRole('button', { name: /^(add|eintragen)$/i }).click();
  await page.locator('section code', { hasText: `https://git.example.com/e2e-${run}` }).first().waitFor();
  await idle();
  await a11y('trusted sources');
  dlg = await install(`https://git.example.com/e2e-${run}/kanban.git`);
  await dlg.waitFor();
  await dlg.locator('.ew-badge', { hasText: /^organisation$/i }).waitFor();
  assert.equal(await dlg.getByRole('checkbox').count(), 0, 'no confirmation box for a trusted source');
  await dlg.getByText(/same rights as the wiki|dieselben rechte wie das wiki/i).waitFor();
  await dlg.getByRole('button', { name: /^(cancel|abbrechen)$/i }).click();
  await dlg.waitFor({ state: 'detached' });
  // a sibling address is not covered by the entry
  dlg = await install(`https://git.example.com/e2e-${run}-evil/kanban`);
  await dlg.locator('.ew-badge', { hasText: /^(external|extern)$/i }).waitFor();
  await dlg.getByRole('button', { name: /^(cancel|abbrechen)$/i }).click();
  await dlg.waitFor({ state: 'detached' });

  // the origin of the installed core plugins is shown
  assert.ok(await page.locator('table .ew-badge', { hasText: /^(official|offiziell)$/i }).count() >= 1);

  // switch external sources off and on again, remove the entry
  const allow = page.getByRole('checkbox', { name: /allow core plugins from external sources|core-plugins aus externen quellen erlauben/i });
  await allow.uncheck();
  await page.locator('.ew-toasts', { hasText: /saved|gespeichert/i }).waitFor();
  dlg = null;
  const form = page.locator('form', { has: page.getByRole('heading', { name: /^(install a core plugin|core-plugin installieren)$/i }) });
  await form.getByLabel(/^(source|quelle)/i).fill('https://git.example.com/elsewhere/x');
  await form.locator('button[type=submit]').click();
  await page.locator('.ew-toasts', { hasText: /switched off|ausgeschaltet/i }).waitFor();
  assert.equal(await page.getByRole('dialog').count(), 0);
  await allow.check();
  await page.getByRole('button', { name: new RegExp(`^(remove source|quelle) https://git.example.com/e2e-${run}`, 'i') }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^(remove|entfernen)$/i }).click();
  await page.locator('.ew-toasts', { hasText: /source removed|quelle entfernt/i }).waitFor();
  // removing the source re-renders the view: wait for it, or the form filled below is replaced
  await page.locator('section code', { hasText: `https://git.example.com/e2e-${run}` }).first().waitFor({ state: 'detached' });
  await idle();

  // access data for private sources: saved, never shown again, removable
  const creds = page.locator('section', { has: page.getByRole('heading', { name: /^(access data for plugin sources|zugangsdaten für plugin-quellen)$/i }) });
  const secret = `e2e-token-${run}`;
  await creds.getByLabel(/^(address \(prefix\)|adresse \(präfix\))/i).fill(`https://git.example.com/private-${run}`);
  await creds.getByLabel(/^token/i).fill(secret);
  await creds.getByRole('button', { name: /^(save access data|zugangsdaten speichern)$/i }).click();
  await page.locator('.ew-toasts', { hasText: /access data saved|zugangsdaten gespeichert/i }).waitFor();
  await creds.locator('code', { hasText: `https://git.example.com/private-${run}` }).waitFor();
  await creds.getByRole('cell', { name: 'x-access-token' }).waitFor();
  await idle();
  assert.equal(await page.content().then((html) => html.includes(secret)), false, 'the token is never shown again');
  assert.equal(await creds.getByLabel(/^token/i).inputValue(), '', 'the field is emptied after saving');
  await a11y('access data for plugin sources');
  // a reference to the environment of the server is refused
  const logged = errors.length;
  await creds.getByLabel(/^(address \(prefix\)|adresse \(präfix\))/i).fill(`https://git.example.com/env-${run}`);
  await creds.getByLabel(/^token/i).fill('${EWIKI_DATABASE_URL}');
  await creds.getByRole('button', { name: /^(save access data|zugangsdaten speichern)$/i }).click();
  await page.locator('.ew-toasts', { hasText: /config\.yaml/i }).waitFor();
  // the refusal above is expected (400): not counted as an error
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 400/.test(e)));
  await page.getByRole('button', { name: new RegExp(`^(remove the access data for|zugangsdaten für) https://git.example.com/private-${run}`, 'i') }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^(remove|entfernen)$/i }).click();
  await page.locator('.ew-toasts', { hasText: /access data removed|zugangsdaten entfernt/i }).waitFor();
});

test('upload a PDF as file element and find its text', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  await page.getByRole('link', { name: /new page|neue seite/i }).click();
  await idle();
  await page.locator('main select').first().selectOption('core.attachment');
  await idle();
  await page.getByLabel(/^(title|titel)/i).fill(`File ${run}`);
  // a tiny PDF with one line of text
  const text = `walrus${run}`;
  const stream = `BT /F1 18 Tf 72 720 Td (${text}) Tj ET`;
  const pdf = `%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n4 0 obj << /Length ${stream.length} >> stream\n${stream}\nendstream endobj\n5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n`;
  await page.locator('.ewa-editor input[type=file]').setInputFiles({ name: 'report.pdf', mimeType: 'application/pdf', buffer: Buffer.from(pdf) });
  await page.waitForFunction(() => /report\.pdf/.test(document.querySelector('.ewa-status')?.textContent ?? ''));
  await a11y('file editor');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  assert.ok(await page.locator('iframe.ew-attachment-pdf').count() > 0, 'pdf preview shown');
  await a11y('file element');
  await page.goto(`${BASE}/search?q=${text}`);
  await idle();
  await page.waitForSelector('.ew-result');
});

test('two-factor setup dialog', async () => {
  await page.goto(BASE + '/profile');
  await idle();
  await page.getByRole('button', { name: /^(set up the authenticator app|authenticator-app einrichten)$/i }).click();
  await page.locator('dialog[open] .ew-secret').waitFor();
  assert.match(await page.locator('dialog[open] .ew-secret').innerText(), /^[A-Z2-7 ]{20,}$/);
  await a11y('authenticator app setup dialog');
  await page.keyboard.press('Escape');
});

test('administration of the sign-in factors', async () => {
  await page.goto(BASE + '/admin/mfa');
  await idle();
  await page.getByRole('heading', { name: /^(sign-in factors|anmeldefaktoren)$/i, level: 1 }).waitFor();
  const cfg = await call('GET', '/api/v1/admin/mfa');
  assert.ok(cfg.factors.some((f) => f.id === 'webauthn' && f.factor === 1), 'passkeys are factor 1');
  assert.ok(cfg.factors.some((f) => f.id === 'totp' && f.factor === 2), 'authenticator app is factor 2');
  await a11y('sign-in factors administration');
  // saving without changes keeps the configuration
  await page.getByRole('button', { name: /^(save|speichern)$/i }).last().click();
  await page.getByText(/^(saved\.|gespeichert\.)$/i).first().waitFor();
  assert.deepEqual(await call('GET', '/api/v1/admin/mfa'), cfg);
});

// insertBlock opens "Insert block", finds the block by its name in the
// search and opens its dialog (the dialog of the block is returned).
async function insertBlock(name) {
  await page.getByRole('button', { name: /^(insert block|baustein einfügen)$/i }).click();
  const picker = page.locator('dialog[open]').last();
  await picker.getByRole('searchbox', { name: /^(search blocks|baustein suchen)$/i }).fill(name);
  await picker.locator(`li[role=treeitem][data-block="${name}"]`).click();
  const dlg = page.locator('dialog[open]').last();
  await dlg.locator('.ewe-macro-params').waitFor();
  return dlg;
}

async function newPage(type, title) {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('link', { name: /new page|neue seite/i }).click();
  await idle();
  if (type !== 'core.html') {
    await page.locator('main select').first().selectOption(type);
    await idle();
  }
  await page.getByLabel(/^(title|titel)/i).fill(title);
}

test('insert macros with the macro dialog', async () => {
  await newPage('core.html', `Macros ${run}`);
  const ed = page.locator('.ewe-prose');
  await ed.click();
  await page.keyboard.type('Intro text');
  // the menu "Insert block": a search and the blocks in groups (a tree)
  await page.getByRole('button', { name: /^(insert block|baustein einfügen)$/i }).click();
  const picker = page.locator('dialog[open]');
  const tree = picker.getByRole('tree');
  await tree.waitFor();
  await a11y('block menu');
  assert.ok(await tree.getByRole('treeitem', { name: /^(text and emphasis|text und hervorhebung)$/i }).count());
  // keyboard: down into the tree, right opens a group, down to its first block
  await page.keyboard.press('ArrowDown');
  const first = tree.locator('[role=treeitem][data-group]').first();
  assert.equal(await first.getAttribute('aria-expanded'), 'false');
  await page.keyboard.press('ArrowRight');
  assert.equal(await first.getAttribute('aria-expanded'), 'true');
  await page.keyboard.press('ArrowDown');
  assert.equal(await page.evaluate(() => document.activeElement?.closest('ul[role=group]') !== null), true);
  // screen readers hear the name; the description only when the choice
  // stays a moment (not on every arrow key)
  const spoken = picker.locator('.ewe-macro-spoken');
  assert.equal(await spoken.textContent(), '');
  await page.waitForFunction(() => (document.querySelector('dialog[open] .ewe-macro-spoken')?.textContent ?? '').length > 10);
  assert.equal(await picker.locator('[aria-live]').filter({ hasText: /./ }).count() >= 1, true);
  await page.keyboard.press('ArrowLeft');
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-expanded')), 'true');
  await page.keyboard.press('ArrowLeft');
  assert.equal(await first.getAttribute('aria-expanded'), 'false');
  // the search finds a block across the groups (German and English names)
  await picker.getByRole('searchbox').fill('hinweis');
  if (await picker.locator('li[data-block="panel"]:visible').count() === 0) await picker.getByRole('searchbox').fill('panel');
  await picker.locator('li[data-block="panel"]').waitFor();
  assert.equal(await picker.locator('li[data-block="toc"]:visible').count(), 0);
  await a11y('block menu with a search');
  await page.keyboard.press('Enter');
  const dialog = page.locator('dialog[open]').last();
  await dialog.locator('.ewe-macro-params').waitFor();
  assert.ok(await dialog.locator('select').first().isDisabled(), 'the chosen block stays chosen');
  assert.equal(await dialog.locator('select').first().inputValue(), 'panel');
  await dialog.locator('select').nth(1).selectOption('warning');
  await dialog.getByLabel(/^(title|titel)/i).fill('Careful');
  await a11y('macro dialog');
  await dialog.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="panel"]');
  // the placeholder text of the body is selected: typing replaces it
  await page.keyboard.type('Panel body');
  // the edit button of the macro opens the dialog for this macro
  await page.locator('.ewe-macro[data-macro="panel"]').getByRole('button', { name: /^(edit block|baustein bearbeiten)/i }).click();
  assert.ok(await page.locator('dialog[open] select').first().isDisabled(), 'existing macro is edited');
  await page.locator('dialog[open]').getByLabel(/^(title|titel)/i).fill('Very careful');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  // below the panel: insert a second macro
  await page.locator('.ewe-prose > p').first().click();
  await page.keyboard.press('Control+End');
  await insertBlock('toc');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="toc"]');
  // the property report with its own controls (sources, filters, columns)
  await page.keyboard.press('Control+End');
  await insertBlock('properties');
  await page.locator('dialog[open]').getByRole('button', { name: /^(add filter|filter hinzufügen)$/i }).waitFor();
  // the source builder: a building block by click
  await page.locator('dialog[open]').getByRole('button', { name: /^(add category|kategorie hinzufügen)$/i }).click();
  await page.locator('dialog[open] .ew-source-zonebox--include .ew-source').first().waitFor();
  await page.locator('dialog[open]').getByRole('button', { name: /^(add filter|filter hinzufügen)$/i }).click();
  await page.locator('dialog[open]').getByLabel(/^(condition|bedingung)$/i).first().waitFor();
  await a11y('property report macro dialog');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="properties"]');
  await a11y('visual editor with macros');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  assert.ok(await page.locator('.ew-content .ew-macro-panel--warning').count() === 1, 'panel rendered');
  assert.match(await page.locator('.ew-macro-panel').innerText(), /Very careful/);
  assert.match(await page.locator('.ew-macro-panel').innerText(), /Panel body/);
  assert.ok(await page.locator('.ew-content .ew-toc, .ew-content .ew-macro--toc').count() === 1, 'toc rendered');
  await a11y('page with macros');
});

test('editor: macros with parts, pictures of blocks, preview keeps the work, leaving asks, icon names, phone menu', async () => {
  await newPage('core.html', `Parts ${run}`);
  const ed = page.locator('.ewe-prose');
  await ed.click();
  await page.keyboard.type('Intro');
  // an accordion starts with two parts; the help says how to add more
  const dialog = await insertBlock('accordion');
  assert.match(await dialog.locator('.ew-help').first().innerText(), /\+ (Part|Teil)/);
  await dialog.locator('button[type=submit]').click();
  const acc = page.locator('.ewe-macro[data-macro="accordion"]');
  await acc.waitFor();
  // the title of the first part is selected: typing replaces it
  await page.keyboard.type(`Question A${run}`);
  await acc.getByRole('button', { name: /^(add part|teil hinzufügen)$/i }).click();
  await page.keyboard.type(`Question C${run}`);
  await page.keyboard.press('Enter');
  await page.keyboard.type('Answer C');
  // the horizontal rule of the toolbar starts a new part; typing goes on below it
  await page.getByRole('button', { name: /^(horizontal line|trennlinie)$/i }).click();
  await page.keyboard.type(`Question D${run}`);
  assert.equal(await acc.locator('.ewe-macro-body > hr').count(), 3, 'four parts');
  assert.match(await acc.locator('.ewe-macro-body').innerText(), new RegExp(`Question A${run}[\\s\\S]*Question C${run}[\\s\\S]*Answer C[\\s\\S]*Question D${run}`));
  // a block without content shows how it looks
  await page.locator('.ewe-prose > p').first().click();
  await insertBlock('buttons');
  await dialog.getByLabel(/^(text of button 1|text der schaltfläche 1)/i).fill(`Go${run}`);
  await dialog.getByLabel(/^(link of button 1|link der schaltfläche 1)/i).fill('/');
  // every choice has a name, no bare values like "start"
  assert.ok(!(await dialog.locator('option').allInnerTexts()).some((x) => /^(start|center|end|row|full|stack|primary|secondary)$/.test(x)), 'named options');
  await dialog.locator('button[type=submit]').click();
  const pic = page.locator('.ewe-macro[data-macro="buttons"] .ewe-macro-picture');
  await pic.locator('a', { hasText: `Go${run}` }).waitFor();
  assert.equal(await pic.getAttribute('inert'), '', 'the picture takes no focus and no clicks');
  // the picture says it all: no settings as text in the head
  assert.equal((await page.locator('.ewe-macro[data-macro="buttons"] .ewe-macro-params').textContent()).trim(), '');
  // the mode button names the mode it switches to
  await page.getByRole('button', { name: /^(markdown mode|markdown-modus)$/i }).click();
  assert.match(await page.locator('textarea.ewe-markdown').inputValue(), new RegExp(`:::accordion[\\s\\S]*### Question A${run}[\\s\\S]*---`));
  await page.getByRole('button', { name: /^(visual mode|visueller modus)$/i }).click();
  await ed.waitFor();
  // icon buttons show their name on hover (the browser's title is not shown twice)
  const editBtn = acc.getByRole('button', { name: /^(edit block|baustein bearbeiten)$/i });
  await editBtn.hover();
  const tip = page.locator('.ew-tooltip');
  await tip.filter({ hasText: /^(edit block|baustein bearbeiten)$/i }).waitFor();
  assert.equal(await editBtn.getAttribute('title'), null);
  await page.keyboard.press('Escape');
  await tip.waitFor({ state: 'hidden' });
  await a11y('editor with parts and a picture of a block');
  // the edit mode is a switch with a name of its own (a toggle button)
  const sw = page.locator('.ew-pos--header-tools .ew-switch-btn');
  assert.equal(await sw.getAttribute('aria-pressed'), 'true');
  assert.match(await sw.evaluate((b) => b.textContent), /(Edit|Bearbeiten)/);
  // the preview keeps the changes as a draft; its links open in a new tab
  await page.getByRole('button', { name: /^(preview|vorschau)$/i }).click();
  const pv = page.locator('dialog[open] .ew-preview');
  await pv.waitFor();
  assert.match(await pv.innerText(), /saved as a draft|als Entwurf gespeichert/);
  assert.equal(await pv.locator('a', { hasText: `Go${run}` }).getAttribute('target'), '_blank');
  await page.waitForURL(/\/d\/\d+\/edit$/);
  await page.keyboard.press('Escape');
  await pv.waitFor({ state: 'detached' });
  // an answer that does not come from the wiki (a proxy, a firewall) says so, with the address
  await page.route('**/api/v1/elements/preview', (r) => r.fulfill({ status: 403, contentType: 'text/plain', body: 'forbidden' }));
  await page.getByRole('button', { name: /^(preview|vorschau)$/i }).click();
  const foreign = page.getByText(/elements\/preview/).first();
  await foreign.waitFor();
  assert.match(await foreign.innerText(), /(did not come from the wiki|kam nicht vom Wiki)[\s\S]*forbidden/);
  await page.unroute('**/api/v1/elements/preview');
  // leaving with unsaved changes asks first: keep editing, then discard
  await ed.locator('p').first().click();
  await page.keyboard.type(' more');
  const here = page.url();
  await page.locator('.ew-tree a').first().click();
  const ask = page.getByRole('alertdialog');
  await ask.waitFor();
  await a11y('question before leaving with unsaved changes');
  await ask.getByRole('button', { name: /^(keep editing|weiter bearbeiten)$/i }).click();
  await ask.waitFor({ state: 'detached' });
  assert.equal(page.url(), here);
  assert.match(await ed.innerText(), /Intro more/);
  await page.getByRole('link', { name: /^(cancel|abbrechen)$/i }).click();
  await ask.getByRole('button', { name: /^(discard changes|änderungen verwerfen)$/i }).click();
  await page.waitForURL((u) => !/\/edit$/.test(u.pathname));
  await idle();
  // the draft has the state of the preview (without " more")
  const drafts = await call('GET', '/api/v1/drafts');
  const d = (drafts.items ?? drafts).find((x) => x.title === `Parts ${run}`);
  assert.ok(d, 'draft kept');
  await call('DELETE', `/api/v1/drafts/${d.id}`);
  // phone: the page starts with its content, the menu button opens navigation and tools
  const size = page.viewportSize();
  try {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto(`${BASE}/`);
    await idle();
    const panel = page.locator('#ewc-header-panel');
    assert.ok(await panel.isHidden(), 'navigation and tools closed at first');
    const menu = page.locator('.ewc-sidebar-toggle');
    await menu.click();
    assert.equal(await menu.getAttribute('aria-expanded'), 'true');
    await panel.locator('.ew-pos--header-tools').waitFor();
    await a11y('phone menu open');
    await page.keyboard.press('Escape');
    assert.equal(await menu.getAttribute('aria-expanded'), 'false');
    assert.ok(await panel.isHidden());
    assert.equal(await page.evaluate(() => document.activeElement?.classList.contains('ewc-sidebar-toggle')), true, 'focus back on the menu button');
    // a long press on an icon button names it instead of pressing it
    const cdp = await page.context().newCDPSession(page);
    const box = await menu.boundingBox();
    const touch = (type) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x: box.x + box.width / 2, y: box.y + box.height / 2 }] });
    await touch('touchStart');
    await tip.waitFor({ state: 'visible', timeout: 3000 });
    await page.waitForTimeout(1000); // a finger that stays longer (found in the CI)
    await touch('touchEnd');
    await page.waitForTimeout(300);
    assert.equal(await menu.getAttribute('aria-expanded'), 'false', 'the long press did not open the menu');
    await cdp.detach();
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('visual editor: Markdown input, tables, Markdown mode, keyboard', async () => {
  await newPage('core.html', `Visual ${run}`);
  const ed = page.locator('.ewe-prose');
  await ed.click();
  // Markdown typed in the visual editor becomes formatting
  await page.keyboard.type('## Section\nSome **strong** and `code` text\n- [ ] open task\nsecond\n\n');
  assert.equal(await ed.locator('h2').innerText(), 'Section');
  assert.ok(await ed.locator('strong').count() === 1 && await ed.locator('code').count() === 1, 'inline marks from Markdown');
  assert.equal(await ed.locator('li.ewe-task').count(), 2, 'task list items');
  await ed.locator('li.ewe-task input[type=checkbox]').first().check();
  // a table; the table tools appear inside it, Tab moves to the next cell
  await page.getByRole('button', { name: /^(insert table|tabelle einfügen)$/i }).click();
  await page.keyboard.type('Head');
  await page.keyboard.press('Tab');
  await page.keyboard.type('Second');
  await page.getByRole('button', { name: /^(insert row below|zeile unterhalb einfügen)$/i }).click();
  assert.equal(await ed.locator('table tr').count(), 4, 'row added');
  await a11y('visual editor in a table');
  // toolbar: Alt+F10 from the text, arrow keys inside, Escape back to the text
  await page.keyboard.press('Alt+F10');
  assert.equal(await page.evaluate(() => document.activeElement?.closest('[role=toolbar]') !== null), true, 'Alt+F10 focuses the toolbar');
  await page.keyboard.press('ArrowRight');
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('tabindex')), '0', 'roving tab index');
  await page.keyboard.press('Escape');
  assert.equal(await page.evaluate(() => document.activeElement?.classList.contains('ewe-prose')), true, 'Escape returns to the text');
  // Escape, then Tab leaves the editor (no keyboard trap) also inside a table
  // (controls inside the text, like task checkboxes, come first)
  await page.keyboard.press('Escape');
  let left = false;
  for (let i = 0; i < 10 && !left; i++) {
    await page.keyboard.press('Tab');
    left = await page.evaluate(() => !document.activeElement?.closest('.ewe-prose'));
  }
  assert.ok(left, 'Esc + Tab leaves the editor');
  // Markdown mode shows the source and keeps changes made there
  await page.getByRole('button', { name: /^(markdown mode|markdown-modus)$/i }).click();
  const src = page.locator('textarea.ewe-markdown');
  const md = await src.inputValue();
  assert.match(md, /## Section/);
  assert.match(md, /- \[x\] open task\n- \[ \] second/);
  assert.match(md, /\| Head \| Second \|/);
  await src.fill(md + '\nAdded in *Markdown*\n');
  await a11y('editor in Markdown mode');
  await page.getByRole('button', { name: /^(visual mode|visueller modus)$/i }).click();
  await ed.locator('em', { hasText: 'Markdown' }).waitFor();
  // inline macro from its own toolbar button: the selected text becomes its text
  await ed.locator('em', { hasText: 'Markdown' }).dblclick();
  await page.getByRole('button', { name: /^(text color and font|textfarbe und schrift)$/i }).click();
  const sd = page.locator('dialog[open]');
  assert.equal(await sd.getByLabel(/^text\b(?! ?colou?r)/i).first().inputValue(), 'Markdown');
  await sd.getByLabel(/^(text color|textfarbe)$/i).selectOption('red');
  await sd.locator('button[type=submit]').click();
  await ed.locator('.ewe-macro_inline[data-macro="style"]').waitFor();
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  const content = page.locator('.ew-content').first();
  assert.equal(await content.locator('input[type=checkbox][checked]').count(), 1, 'checked task saved');
  assert.match(await content.locator('table').innerText(), /Head/);
  assert.equal(await content.locator('.ew-style.ew-fg-red', { hasText: 'Markdown' }).count(), 1, 'styled text');
});

test('live editing: two editors see each other', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Live ${run}`, body: { format: 'markdown', source: '## Shared\n\nStart text.\n' } });
  // a second editor in its own browser context (own session)
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`second editor: ${e.message}`));
  try {
    await p2.goto(BASE + '/login');
    await p2.getByLabel(/user ?name|benutzername/i).fill(USER);
    await p2.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    await page.goto(`${BASE}/e/${el.id}/edit`);
    await page.waitForFunction(() => /nobody|niemand/i.test(document.querySelector('.ewe-live')?.textContent ?? ''), null, { timeout: 15000 });
    await p2.goto(`${BASE}/e/${el.id}/edit`);
    await p2.waitForFunction(() => document.querySelectorAll('.ewe-live .ewe-peers li').length === 2, null, { timeout: 15000 });
    await a11y('live editing with another editor');
    // typing in one editor appears in the other, with the cursor of the writer
    await page.locator('.ewe-prose h2').click();
    await page.keyboard.press('End');
    await page.keyboard.type(` live${run}`);
    await p2.waitForFunction((x) => document.querySelector('.ewe-prose')?.textContent?.includes(x), `live${run}`, { timeout: 10000 });
    await p2.locator('.ewe-prose .ewe-cursor').waitFor();
    // the shared document is not duplicated when the second editor joins
    assert.equal(await p2.locator('.ewe-prose h2').count(), 1);
    // everybody is listed with the color and shape of the cursor
    assert.equal(await p2.locator('.ewe-peers li').count(), 2);
    const shape = await p2.locator('.ewe-peers li').nth(1).locator('.ewe-peer-mark').innerText();
    assert.ok((await p2.locator('.ewe-prose .ewe-cursor-label').innerText()).startsWith(shape), 'cursor shows the shape of the list');
    // Markdown source live: one switches, both are in the source and see each other
    await page.getByRole('button', { name: /^(markdown mode|markdown-modus)$/i }).click();
    const src1 = page.locator('textarea.ewe-markdown'), src2 = p2.locator('textarea.ewe-markdown');
    await src2.waitFor();
    assert.match(await src2.inputValue(), new RegExp(`## Shared live${run}`));
    await src2.click();
    await p2.keyboard.press('Control+End');
    await p2.keyboard.type(`\nsource${run}`);
    await page.waitForFunction((x) => document.querySelector('textarea.ewe-markdown')?.value.includes(x), `source${run}`, { timeout: 10000 });
    await page.locator('.ewe-md-mirror .ewe-md-caret').waitFor({ state: 'attached' });
    await a11y('live editing in the Markdown source');
    // back to the visual editor for everybody, with the text from the source
    await p2.getByRole('button', { name: /^(visual mode|visueller modus)$/i }).click();
    await page.locator('.ewe-prose').waitFor();
    await page.waitForFunction((x) => document.querySelector('.ewe-prose')?.textContent?.includes(x), `source${run}`, { timeout: 10000 });
    assert.ok(await src1.isHidden(), 'the first editor followed back to the visual editor');
    // saving in one editor: the other continues on the new revision without conflict
    await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
    await page.waitForURL(/\/e\/\d+$/);
    await p2.locator('.ew-notice--info[role=status]').waitFor();
    await p2.locator('.ewe-prose h2').click();
    await p2.keyboard.press('End');
    await p2.keyboard.type('!');
    await p2.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
    await p2.waitForURL(/\/e\/\d+$/);
    const saved = await call('GET', `/api/v1/elements/${el.id}?edit=1`);
    assert.match(saved.body.source, new RegExp(`## Shared live${run}!`));
  } finally {
    await ctx2.close();
  }
});

test('shared notes: write together in the page view, saved automatically', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.notes', title: `Notes ${run}`, body: { format: 'markdown', source: '- [ ] agenda\n' } });
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`second editor: ${e.message}`));
  try {
    await p2.goto(BASE + '/login');
    await p2.getByLabel(/user ?name|benutzername/i).fill(USER);
    await p2.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    await page.goto(`${BASE}/e/${el.id}`);
    await page.locator('.ewn .ewe-prose').waitFor();
    await page.waitForFunction(() => /nobody|niemand/i.test(document.querySelector('.ewn .ewe-live')?.textContent ?? ''), null, { timeout: 15000 });
    await p2.goto(`${BASE}/e/${el.id}`);
    await p2.waitForFunction(() => document.querySelectorAll('.ewn .ewe-live .ewe-peers li').length === 2, null, { timeout: 15000 });
    await a11y('shared notes');
    // the second person writes, the first sees it; the first one (leader) saves
    await p2.locator('.ewn .ewe-prose li p').first().click();
    await p2.keyboard.press('End');
    await p2.keyboard.press('Enter');
    await p2.locator('.ewn .ewe-prose li').nth(1).waitFor();
    await p2.keyboard.type(`minutes${run}`);
    await page.waitForFunction((x) => document.querySelector('.ewn .ewe-prose')?.textContent?.includes(x), `minutes${run}`, { timeout: 10000 });
    await p2.waitForFunction(() => /saved|gespeichert/i.test(document.querySelector('.ewn-status')?.textContent ?? ''), null, { timeout: 20000 });
    const saved = await call('GET', `/api/v1/elements/${el.id}?edit=1`);
    assert.match(saved.body.source, new RegExp(`- \\[ \\] agenda\\n- \\[ \\] minutes${run}`));
  } finally {
    await ctx2.close();
  }
});

test('e-mail addresses, sign-ins and invitations', async () => {
  const logged = errors.length;
  // profile: addresses and sign-ins
  await page.goto(`${BASE}/profile`);
  await idle();
  await page.locator('#em-h').waitFor();
  assert.ok(await page.locator('#id-h').isVisible(), 'sign-ins section');
  // without configured e-mail, adding an address explains why it does not work
  await page.getByLabel(/^(add an address|adresse hinzufügen)/i).fill(`me${run}@example.org`);
  await page.getByRole('button', { name: /^(add|hinzufügen)$/i }).click();
  await page.locator('.ew-section form .ew-form-alert', { hasText: /e-mail|e-mail-versand|sending/i }).waitFor();
  await a11y('profile with e-mail addresses and sign-ins');
  // administration: invite somebody and add a confirmed address
  await page.goto(`${BASE}/admin/users`);
  await idle();
  await page.getByRole('button', { name: /^(invite|einladen)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/^(e-mail address|e-mail-adresse)/i).fill(`guest${run}@example.org`);
  await dlg.getByLabel(/^(display name|anzeigename)/i).fill(`Guest ${run}`);
  await a11y('invite dialog');
  await dlg.locator('button[type=submit]').click();
  await dlg.waitFor({ state: 'detached' });
  const row = page.locator('tr', { hasText: `Guest ${run}` });
  await row.locator('.ew-badge', { hasText: /invited|eingeladen/i }).waitFor();
  await row.getByRole('button', { name: /^(e-mail & sign-ins|e-mail & anmeldungen):/i }).click();
  const idDlg = page.locator('dialog[open]');
  await idDlg.getByLabel(/^(add an address|adresse hinzufügen)/i).fill(`guest.work${run}@example.org`);
  await idDlg.getByRole('button', { name: /^(add|hinzufügen)$/i }).click();
  await idDlg.locator('th', { hasText: `guest.work${run}@example.org` }).waitFor();
  await a11y('e-mail and sign-ins of a user');
  await page.keyboard.press('Escape');
  // confirmation and link pages with invalid tokens
  await page.goto(`${BASE}/verify-email?token=invalid`);
  await page.locator('.ew-notice--error').waitFor();
  assert.equal(new URL(page.url()).search, '', 'token removed from the address bar');
  await a11y('e-mail confirmation');
  // the refusals above are expected (400): not counted as errors
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 400/.test(e)));
});

test('sessions: see where one is signed in and sign out elsewhere', async () => {
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`second browser: ${e.message}`));
  try {
    await p2.goto(BASE + '/login');
    await p2.getByLabel(/user ?name|benutzername/i).fill(USER);
    await p2.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    await page.goto(`${BASE}/profile`);
    await idle();
    const sec = page.locator('section', { has: page.locator('#ses-h') });
    await sec.locator('.ew-badge', { hasText: /this session|diese sitzung/i }).waitFor();
    assert.ok(await sec.getByRole('button', { name: /^(sign out|abmelden):/i }).count() >= 1, 'the other session can be ended');
    await a11y('profile with sessions');
    await sec.getByRole('button', { name: /^(sign out everywhere else|überall sonst abmelden)$/i }).click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(sign out everywhere else|überall sonst abmelden)$/i }).click();
    await sec.getByRole('button', { name: /^(sign out|abmelden):/i }).first().waitFor({ state: 'detached' });
    // the other browser is signed out, this one is not
    const me2 = await p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user);
    assert.equal(me2, null, 'other session ended');
    assert.ok((await call('GET', '/api/v1/auth/me')).user, 'own session kept');
  } finally {
    await ctx2.close();
  }
});

test('sign-in providers: several instances, mapping preview', async () => {
  const logged = errors.length;
  await page.goto(`${BASE}/admin/oidc`);
  await idle();
  const choice = page.getByLabel(/^(provider|anbieter)/i).first();
  await choice.waitFor();
  await page.getByRole('button', { name: /^(add provider|anbieter hinzufügen)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/^(id|kennung)/i).fill(`p${run}`);
  await dlg.getByLabel(/^(name on the login page|name auf der anmeldeseite)/i).fill(`Partner ${run}`);
  await dlg.locator('button[type=submit]').click();
  await dlg.waitFor({ state: 'detached' });
  await page.locator('legend', { hasText: `oidc-p${run}` }).waitFor();
  assert.equal(await choice.inputValue(), `p${run}`, 'the new instance is selected');
  await a11y('sign-in provider instances');
  // preview of a sign-in at the new instance (JIT on by default)
  await page.getByLabel(/^(user ?name|benutzername)/i).last().fill(`dora${run}`);
  await page.getByRole('button', { name: /^(show preview|vorschau anzeigen)$/i }).click();
  await page.locator('.ew-test-result dd', { hasText: /new account|neues konto/i }).waitFor();
  await a11y('sign-in preview');
  // remove it again
  await page.getByRole('button', { name: /^(remove provider|anbieter entfernen)$/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(remove provider|anbieter entfernen)$/i }).click();
  await page.locator('legend', { hasText: /oidc$/ }).waitFor();
  const s = await call('GET', '/api/v1/admin/settings');
  assert.ok(!(s.config.plugin_settings?.['auth.oidc']?.instances ?? []).some((x) => x.id === `p${run}`), 'instance removed');
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 400/.test(e)));
});

test('SCIM provisioning clients', async () => {
  const before = await call('GET', '/api/v1/admin/settings');
  const oidc = before.config.plugin_settings?.['auth.oidc'] ?? {};
  await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'auth.oidc': { ...oidc, issuer: 'https://idp.example.invalid', client_id: 'wiki' } } });
  try {
    await page.goto(`${BASE}/admin/scim`);
    await idle();
    await page.getByRole('button', { name: /^(add scim client|scim-zugang anlegen)$/i }).click();
    const dlg = page.locator('dialog[open]');
    await dlg.getByLabel(/^name/i).fill(`Entra ${run}`);
    await a11y('add SCIM client');
    await dlg.locator('button[type=submit]').click();
    const tok = page.locator('dialog[open]', { hasText: /scim_|token/i });
    await tok.getByLabel(/^token/i).waitFor();
    assert.match(await tok.getByLabel(/^token/i).inputValue(), /^scim_/);
    assert.match(await tok.getByLabel(/^(tenant url|tenant-url)/i).inputValue(), /\/scim\/v2$/);
    await a11y('SCIM token');
    await page.keyboard.press('Escape');
    const row = page.locator('tr', { hasText: `Entra ${run}` });
    await row.waitFor();
    await row.getByRole('button', { name: /^(delete|löschen):/i }).click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await row.waitFor({ state: 'detached' });
  } finally {
    await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'auth.oidc': oidc } });
  }
});

// TOTP code of a base32 secret (RFC 6238) for the second-factor tests
function totpCode(secret, offset = 0) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const ch of secret.replace(/[\s=]/g, '').toUpperCase()) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g).map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000) + offset));
  const mac = createHmac('sha1', key).update(counter).digest();
  const o = mac[mac.length - 1] & 0xf;
  return String((mac.readUInt32BE(o) & 0x7fffffff) % 1000000).padStart(6, '0');
}

test('second factor at sign-in and passkeys', async () => {
  const name = `mfa${run}`;
  const pw = 'Mfa-Passw0rd-1!';
  await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `MFA ${run}` });
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`mfa user: ${e.message}`));
  const login = async () => {
    await p2.goto(BASE + '/login');
    await p2.getByLabel(/user ?name|benutzername/i).fill(name);
    await p2.getByLabel(/^(password|passwort)/i).fill(pw);
    await p2.keyboard.press('Enter');
  };
  try {
    await login();
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    // the authenticator app (set up through the API of the signed-in browser)
    const secret = await p2.evaluate(async () => {
      const r = await __e2eFetch('/api/v1/me/mfa/totp/begin', { method: 'POST', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': 'application/json' }, body: '{}' });
      return (await r.json()).secret;
    });
    const enabled = await p2.evaluate(async (code) => (await __e2eFetch('/api/v1/me/mfa/totp/finish', { method: 'POST', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) })).status, totpCode(secret));
    assert.equal(enabled, 204);
    // a passkey with a virtual authenticator of the browser
    const cdp = await ctx2.newCDPSession(p2);
    await cdp.send('WebAuthn.enable');
    const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
    // the virtual authenticator answers passkey requests at once, also the offer
    // in the user name field: switch that off where a person would not pick it
    const presence = (enabled) => cdp.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled });
    await p2.goto(BASE + '/profile');
    await p2.locator('#mfa-h').waitFor();
    await a11y('sign-in factors in the profile', p2);
    await p2.getByRole('button', { name: /^(add security key|sicherheitsschlüssel hinzufügen)$/i }).click();
    const dlg = p2.locator('dialog[open]');
    await dlg.getByLabel(/^name/i).fill('Virtual');
    // passkeys are factor 1 here: every key is a passkey
    assert.ok(await dlg.getByLabel(/^(as passkey|als passkey)$/i).isChecked());
    await dlg.locator('button[type=submit]').click();
    await dlg.waitFor({ state: 'detached' });
    await p2.locator('.ew-subsection table th', { hasText: 'Virtual' }).waitFor();
    // sign out: the password alone is not enough any more
    await p2.evaluate(() => __e2eFetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-Requested-With': 'ewiki' } }));
    await presence(false);
    await login();
    await p2.getByRole('heading', { name: /^(confirm sign-in|anmeldung bestätigen)$/i }).waitFor();
    await a11y('second factor step', p2);
    // the authenticator app (the only factor 2 of this account)
    assert.equal(await p2.locator('input[name=mfa-method]').count(), 0);
    await p2.getByLabel(/code/i).first().fill(totpCode(secret, 1));
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    // remove the authenticator app in the profile (the chain would ask it after the passkey too)
    await p2.goto(BASE + '/profile');
    await p2.getByRole('button', { name: /^(remove the authenticator app|authenticator-app entfernen)$/i }).click();
    await p2.locator('dialog[open]').getByRole('button', { name: /^(remove the authenticator app|authenticator-app entfernen)$/i }).click();
    await p2.getByRole('button', { name: /^(set up the authenticator app|authenticator-app einrichten)$/i }).waitFor();
    const me = async () => p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user?.username);
    const logout = () => p2.evaluate(() => __e2eFetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-Requested-With': 'ewiki' } }));
    // the passkey from the suggestions of the user name field (picked at once by the virtual authenticator)
    await logout();
    await presence(true);
    await p2.goto(BASE + '/login');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    assert.equal(await me(), name);
    // the passkey button: sign-in without password (offer in the field switched off)
    await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: false, auto_redirect: '', buttons: {} });
    try {
      await logout();
      await p2.goto(BASE + '/login');
      await p2.getByRole('button', { name: /^(sign in with a passkey|mit passkey anmelden)$/i }).click();
      await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
      assert.equal(await me(), name);
    } finally {
      await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: true, auto_redirect: '', buttons: {} });
    }
  } finally {
    await ctx2.close();
  }
});

test('sign-in page: provider buttons, direct link, automatic redirect, passkeys in the user name field', async () => {
  const logged = errors.length;
  const before = (await call('GET', '/api/v1/admin/settings')).config.plugin_settings?.['auth.oidc'] ?? {};
  // a provider with its own sign-in page that cannot be reached: every start ends on the sign-in page with an error
  await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'auth.oidc': { ...before, name: `IdP ${run}`, issuer: 'http://127.0.0.1:9', client_id: 'wiki', instances: [] } } });
  const label = `Company ${run}`;
  await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: true, auto_redirect: '', buttons: { oidc: { order: 1, label, hidden: false } } });
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`sign-in page: ${e.message}`));
  const user = () => p2.getByLabel(/user ?name|benutzername/i);
  try {
    await p2.goto(BASE + '/login');
    await p2.getByRole('link', { name: new RegExp(label) }).waitFor();
    // passkeys in the suggestions of the user name field (where the browser offers it)
    const conditional = await p2.evaluate(async () => !!(await window.PublicKeyCredential?.isConditionalMediationAvailable?.()));
    assert.equal(await user().getAttribute('autocomplete'), conditional ? 'username webauthn' : 'username');
    await a11y('sign-in page with a provider button', p2);
    // hidden button, autofill off
    await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: false, auto_redirect: '', buttons: { oidc: { order: 1, label, hidden: true } } });
    await p2.goto(BASE + '/login');
    await user().waitFor();
    assert.equal(await user().getAttribute('autocomplete'), 'username');
    assert.equal(await p2.getByRole('link', { name: new RegExp(label) }).count(), 0, 'hidden button');
    // the direct link still leads to the hidden provider (here: back with an error, no loop)
    await p2.goto(BASE + '/login?via=oidc');
    await p2.waitForURL((u) => u.pathname === '/login' && u.searchParams.has('error'));
    await user().waitFor();
    // automatic redirect: straight to the provider, back on errors without a loop
    await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: false, auto_redirect: 'oidc', buttons: { oidc: { order: 1, label, hidden: true } } });
    await p2.goto(BASE + '/login');
    await p2.waitForURL((u) => u.pathname === '/login' && u.searchParams.has('error'));
    await p2.locator('.ew-form-alert').filter({ hasText: /\S/ }).first().waitFor();
    // emergency access: the full page
    await p2.goto(BASE + '/login?local=1');
    await user().waitFor();
    assert.ok(new URL(p2.url()).searchParams.has('local'), 'stays on the page');
    await a11y('sign-in page, emergency access', p2);
  } finally {
    await ctx2.close();
  }
  // the administration: settings of the sign-in page
  await page.goto(`${BASE}/admin/settings`);
  await idle();
  const form = page.locator('form[aria-labelledby="ew-login-admin-h"]');
  await form.waitFor();
  await a11y('settings of the sign-in page');
  assert.equal(await form.getByLabel(/^(redirect automatically to|automatisch weiterleiten zu)/i).inputValue(), 'oidc');
  await form.getByLabel(/^(redirect automatically to|automatisch weiterleiten zu)/i).selectOption('');
  await form.getByLabel(/suggest passkeys|passkeys im feld/i).check();
  await form.getByLabel(/^(show the button|knopf auf der anmeldeseite)/i).check();
  await form.getByLabel(/^(text of the button|text des knopfs)/i).fill(`Staff ${run}`);
  await form.getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.locator('.ew-toasts', { hasText: /sign-in page is saved|anmeldeseite ist gespeichert/i }).waitFor();
  const lp = await call('GET', '/api/v1/admin/auth/login');
  assert.equal(lp.auto_redirect, '');
  assert.equal(lp.passkey_autofill, true);
  assert.deepEqual(lp.buttons.oidc, { order: 1, hidden: false, label: `Staff ${run}` });
  // clean up
  await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: true, auto_redirect: '', buttons: {} });
  await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'auth.oidc': before } });
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of (4\d\d|502)/.test(e)));
});

test('create an account yourself: switched on by the administrators, link by e-mail, then sign in', async () => {
  const smtp = await smtpFake();
  const prevMail = await call('GET', '/api/v1/admin/settings');
  await call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: prevMail.config.mail.from || 'wiki@example.invalid',
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port: smtp.port, security: 'none', username: '', password: '' } } });
  const addr = `self${run}@example.invalid`;
  const pw = 'Selbst-Angelegt-2026!';
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`registration: ${e.message}`));
  try {
    // switched off: no link on the sign-in page
    await p2.goto(BASE + '/login');
    await p2.locator('input[name=username]').waitFor();
    assert.equal(await p2.getByRole('link', { name: /^(create an account|konto anlegen)$/i }).count(), 0);
    // the administrators switch it on (only this domain)
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    const form = page.locator('form[aria-labelledby="ew-login-admin-h"]');
    await form.getByLabel(/^(people may create a local account|personen dürfen selbst)/i).check();
    await form.getByLabel(/^(allowed domains|erlaubte domains)/i).fill('example.invalid');
    await a11y('settings of the registration');
    await form.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toasts', { hasText: /sign-in page is saved|anmeldeseite ist gespeichert/i }).waitFor();
    const lp = await call('GET', '/api/v1/admin/auth/login');
    assert.deepEqual(lp.registration, { enabled: true, domains: ['example.invalid'], groups: [] });
    // the person registers
    await p2.goto(BASE + '/login');
    await p2.getByRole('link', { name: /^(create an account|konto anlegen)$/i }).click();
    await p2.locator('input[name=email]').waitFor();
    await a11y('registration form', p2);
    await p2.locator('input[name=email]').fill(`self${run}@other.invalid`);
    await p2.locator('input[name=name]').fill(`Self ${run}`);
    await p2.locator('input[name=new-password]').fill(pw);
    await p2.locator('input[name=new-password-2]').fill(pw);
    await p2.getByRole('button', { name: /^(create account|konto anlegen)$/i }).click();
    await p2.locator('.ew-form-alert', { hasText: /domain/i }).waitFor();
    await p2.locator('input[name=email]').fill(addr);
    await p2.locator('input[name=new-password-2]').fill(pw + 'x');
    await p2.getByRole('button', { name: /^(create account|konto anlegen)$/i }).click();
    await p2.locator('.ew-form-alert', { hasText: /not the same|nicht gleich/i }).waitFor();
    await p2.locator('input[name=new-password-2]').fill(pw);
    await p2.getByRole('button', { name: /^(create account|konto anlegen)$/i }).click();
    await p2.locator('.ew-notice--ok', { hasText: addr }).waitFor();
    await a11y('registration sent', p2);
    // the link from the e-mail creates the account
    // sent before the answer (the link is secret and never queued)
    const mail = smtp.mails.find((m) => m.to.includes(addr));
    const link = /\/register\/confirm\?token=[A-Za-z0-9_-]+/.exec(mail?.text ?? '')?.[0];
    assert.ok(link, `link in the e-mail: ${mail?.text}`);
    await p2.goto(BASE + link);
    await p2.locator('.ew-notice--ok', { hasText: addr }).waitFor();
    assert.equal(new URL(p2.url()).search, '', 'token left the address bar');
    await a11y('registration confirmed', p2);
    // sign in with the address
    await p2.locator('main a[href="/login?local=1"]').click();
    await p2.locator('input[name=username]').fill(addr);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    const me = await p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user);
    assert.equal(me?.email, addr);
    assert.ok(!me?.is_admin, 'no administrator');
  } finally {
    await ctx2.close();
    const lp = await call('GET', '/api/v1/admin/auth/login');
    await call('PUT', '/api/v1/admin/auth/login', { passkey_autofill: lp.passkey_autofill, auto_redirect: lp.auto_redirect, buttons: lp.buttons, registration: { enabled: false, domains: [], groups: [] } });
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail });
    smtp.close();
  }
});

test('forgot your password: link by e-mail, new password, switch off in the administration', async () => {
  const smtp = await smtpFake();
  const prevMail = await call('GET', '/api/v1/admin/settings');
  await call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: prevMail.config.mail.from || 'wiki@example.invalid',
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port: smtp.port, security: 'none', username: '', password: '' } } });
  const addr = `self${run}@example.invalid`; // created by the registration test
  const pw = `Neu-Gesetzt-${run}!`;
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`password reset: ${e.message}`));
  try {
    await p2.goto(BASE + '/login');
    await p2.getByRole('link', { name: /^(forgot your password\?|passwort vergessen\?)$/i }).click();
    // (if this fails: where the page went and what it shows)
    await p2.locator('input[name=email]').waitFor().catch(async (e) => {
      throw new Error(`no form to ask for the link at ${p2.url()}: ${(await p2.locator('main').innerText().catch(() => '?')).slice(0, 300)} (${e.message.split('\n')[0]})`);
    });
    await a11y('forgot password form', p2);
    await p2.locator('input[name=email]').fill(addr);
    await p2.getByRole('button', { name: /^(send link|link senden)$/i }).click();
    await p2.locator('.ew-notice--ok', { hasText: addr }).waitFor();
    // the mail goes out in the background (same answer for every address)
    const deadline = Date.now() + 15000;
    let mail;
    while (!(mail = smtp.mails.find((m) => m.to.includes(addr))) && Date.now() < deadline) await p2.waitForTimeout(50);
    const link = /\/password-reset\/confirm\?token=[A-Za-z0-9_-]+/.exec(mail?.text ?? '')?.[0];
    assert.ok(link, `link in the e-mail: ${mail?.text}`);
    await p2.goto(BASE + link);
    await p2.locator('input[name=new-password]').waitFor();
    assert.equal(new URL(p2.url()).search, '', 'token left the address bar');
    await a11y('new password form', p2);
    await p2.locator('input[name=new-password]').fill(pw);
    await p2.locator('input[name=new-password-2]').fill(pw);
    await p2.getByRole('button', { name: /^(save password|passwort speichern)$/i }).click();
    await p2.locator('.ew-notice--ok').waitFor();
    await p2.locator('main a[href="/login?local=1"]').click();
    await p2.locator('input[name=username]').fill(addr);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    // switched off in the administration: no link any more
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    const form = page.locator('form[aria-labelledby="ew-login-admin-h"]');
    await form.getByLabel(/^(offer .forgot your password|„passwort vergessen)/i).uncheck();
    await form.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toasts', { hasText: /sign-in page is saved|anmeldeseite ist gespeichert/i }).waitFor();
    assert.equal((await call('GET', '/api/v1/admin/auth/login')).password_reset, false);
    const guest = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    try {
      const g = await guest.newPage();
      await g.goto(BASE + '/login');
      await g.locator('input[name=username]').waitFor();
      assert.equal(await g.getByRole('link', { name: /^(forgot your password\?|passwort vergessen\?)$/i }).count(), 0);
    } finally { await guest.close(); }
  } finally {
    await ctx2.close();
    const lp = await call('GET', '/api/v1/admin/auth/login');
    await call('PUT', '/api/v1/admin/auth/login', { ...lp, providers: undefined, registration_here: undefined, mail_ready: undefined, password_reset: true });
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail });
    smtp.close();
  }
});

test('a decision signed with a passkey, its verification and the verify page', async () => {
  const name = `sig${run}`;
  const pw = 'Sig-Passw0rd-1!x';
  await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Signer ${run}` });
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Signed ${run}` } })).id;
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'authenticated', permissions: 1, inherit: true });
  const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Contract ${run}`, body: {
    description: 'E2E', fields: [{ id: 'amount', label: 'Amount', type: 'number' }],
    steps: [
      { id: 'check', name: 'Check', type: 'task', sign: 'advanced', assignee: { type: 'user', value: name },
        actions: [{ id: 'ok', label: 'Agree', next: 'done', note: 'optional' }] },
      { id: 'done', name: 'Done', type: 'end', outcome: 'success' }] } });
  const x = await call('POST', `/api/v1/x/workflow.process/elements/${wf.id}/instances`, { data: { amount: 1200 } });
  // the editor offers the signature of the step
  await page.goto(`${BASE}/e/${wf.id}/edit`);
  await idle();
  assert.equal(await page.locator('.ewf-step-row').first().getByLabel(/^(signature of the decisions|signatur der entscheidungen)/i).inputValue(), 'advanced');
  await a11y('workflow editor with signature');
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`signer: ${e.message}`));
  try {
    const cdp = await ctx2.newCDPSession(p2);
    await cdp.send('WebAuthn.enable');
    await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
    await p2.goto(BASE + '/login');
    await p2.getByLabel(/user ?name|benutzername/i).fill(name);
    await p2.getByLabel(/^(password|passwort)/i).fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    // without a passkey the decision cannot be signed yet
    await p2.goto(`${BASE}/e/${wf.id}?case=${x.id}`);
    await p2.locator('dialog[open]').getByRole('button', { name: /^agree$/i }).click();
    await p2.getByText(/cannot sign this decision yet|noch nicht signieren/i).waitFor();
    await a11y('decision without a way to sign', p2);
    // set up the passkey in the profile
    await p2.goto(BASE + '/profile');
    await p2.getByRole('button', { name: /^(add security key|sicherheitsschlüssel hinzufügen)$/i }).click();
    const kd = p2.locator('dialog[open]');
    await kd.getByLabel(/^name/i).fill('Virtual');
    await kd.locator('button[type=submit]').click();
    await kd.waitFor({ state: 'detached' });
    // sign the decision
    await p2.goto(`${BASE}/e/${wf.id}?case=${x.id}`);
    await p2.locator('dialog[open]').getByRole('button', { name: /^agree$/i }).click();
    const form = p2.locator('dialog[open] form');
    await form.getByText(/you sign with|sie signieren mit/i).waitFor();
    await form.getByLabel(/^(note|notiz)/i).fill('Checked against the offer');
    await a11y('decision with signature', p2);
    await form.getByRole('button', { name: /^(sign and agree|signieren und agree)$/i }).click();
    await form.waitFor({ state: 'detached' });
    const done = await call('GET', `/api/v1/x/workflow.process/instances/${x.id}`);
    assert.equal(done.status, 'done');
    const sig = done.events.find((e) => e.action === 'ok')?.signature;
    assert.ok(sig, 'the decision carries its signature');
    // the evidence: valid, checks listed, export
    await p2.goto(`${BASE}/e/${wf.id}?case=${x.id}`);
    await p2.locator('dialog[open]').getByRole('button', { name: /^(signed – check|signiert – prüfen)$/i }).click();
    const sd = p2.locator('dialog[open]', { has: p2.getByRole('heading', { name: /^(signature|signatur)$/i }) });
    await sd.getByText(/the signature is valid|die signatur ist gültig/i).waitFor();
    await sd.getByText(/ml-dsa-65/i).waitFor();
    await a11y('signature verification', p2);
    const exp = await p2.evaluate(async (id) => (await __e2eFetch(`/api/v1/signatures/${id}/export`)).text(), sig);
    assert.match(exp, /"public_key"/);
    // the verify page: the export is valid, a changed one is not
    await p2.goto(BASE + '/signatures/verify');
    await a11y('verify page', p2);
    await p2.getByLabel(/^(exported signature|exportierte signatur)/i).setInputFiles({ name: 'sig.json', mimeType: 'application/json', buffer: Buffer.from(exp) });
    await p2.getByText(/the signature is valid|die signatur ist gültig/i).waitFor();
    const bad = JSON.parse(exp);
    bad.record.decision.note = 'Something else';
    await p2.getByLabel(/^(exported signature|exportierte signatur)/i).setInputFiles({ name: 'bad.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(bad)) });
    await p2.getByText(/the signature is not valid|die signatur ist nicht gültig/i).waitFor();
    await a11y('verify page with an invalid signature', p2);
  } finally {
    await ctx2.close();
  }
});

test('the simple editor as personal choice', async () => {
  await call('PUT', '/api/v1/me', { prefs: { editor: 'simple' } });
  try {
    const prefs = await call('GET', '/api/v1/me/prefs');
    assert.equal(prefs.prefs.editor, 'simple');
    await newPage('core.html', `Simple ${run}`);
    const ed = page.locator('.ewh-editable');
    await ed.click();
    await page.keyboard.type(`Simple text koala${run}`);
    await page.getByRole('button', { name: /^bold$|^fett$/i }).click();
    await page.keyboard.type(' bold');
    await a11y('simple editor');
    await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
    await page.waitForURL(/\/e\/\d+$/);
    const id = Number(/\/e\/(\d+)$/.exec(page.url())[1]);
    await idle();
    assert.match(await page.locator('.ew-content').first().innerText(), new RegExp(`koala${run}`));
    // saved as Markdown, whichever editor was used
    const el = await call('GET', `/api/v1/elements/${id}?edit=1`);
    assert.equal(el.body.format, 'markdown');
    assert.match(el.body.source, new RegExp(`koala${run}`));
    assert.match(el.body.source, /\*\*\s*bold\s*\*\*/);
    // unknown settings and values are refused (the browser logs the 400 responses)
    const logged = errors.length;
    await assert.rejects(call('PUT', '/api/v1/me', { prefs: { editor: 'fancy' } }));
    await assert.rejects(call('PUT', '/api/v1/me', { prefs: { nope: 'x' } }));
    await page.waitForTimeout(100);
    errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 400/.test(e)));
  } finally {
    await call('PUT', '/api/v1/me', { prefs: { editor: '' } });
  }
});

test('workflow: HR application from template, start and handle a case', async () => {
  await newPage('workflow.process', `Application ${run}`);
  await page.locator('.ewf-template:not(.ewf-add-step) select').selectOption('hr');
  await page.getByRole('button', { name: /use template|vorlage verwenden/i }).click();
  await page.waitForFunction(() => document.querySelectorAll('.ewf-step-row').length >= 4);
  assert.ok(await page.locator('.ewf-step-row--email').count() >= 2, 'template contains e-mail steps');
  await a11y('workflow editor');
  // add an approval step and change its type to a condition
  await page.locator('.ewf-add-step select').selectOption('approval');
  await page.getByRole('button', { name: /^(add step|schritt hinzufügen)$/i }).click();
  await page.waitForSelector('.ewf-step-row--approval');
  const last = page.locator('.ewf-step-row').last();
  await last.locator('.ewf-step-type').selectOption('condition');
  await page.waitForSelector('.ewf-step-row--condition');
  await a11y('workflow editor with approval and condition steps');
  await page.locator('.ewf-step-row').last().getByRole('button', { name: /^(delete|löschen)/i }).last().click();
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.waitForSelector('.ewf-cases');
  await a11y('workflow page');
  // only the assignees of a step carry it out: make the test user a member of the group hr
  const me = await call('GET', '/api/v1/auth/me');
  const groups = await call('GET', '/api/v1/groups');
  let hr = groups.find((g) => g.name === 'hr');
  if (!hr) hr = { id: (await call('POST', '/api/v1/admin/groups', { name: 'hr' })).id };
  if (!me.user.groups.includes(hr.id)) await call('PUT', `/api/v1/admin/users/${me.user.id}`, { groups: [...me.user.groups, hr.id] });
  await page.getByRole('button', { name: /start new case|neuen vorgang starten/i }).click();
  const form = page.locator('dialog[open]');
  await form.waitFor();
  await form.getByLabel(/full name|vor- und nachname/i).fill(`Ada ${run}`);
  await form.getByLabel(/^e-mail/i).fill('ada@example.com');
  await form.getByLabel(/^(position|stelle)/i).selectOption({ index: 1 });
  await form.locator('input[type=file]').setInputFiles({ name: 'cv.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4\n%%EOF\n') });
  await form.getByLabel(/agree|zustimme/i).check();
  await a11y('workflow start form');
  await form.locator('button[type=submit]').click();
  // the new case opens in a dialog
  await page.waitForFunction((t) => document.querySelector('dialog[open] h2')?.textContent?.includes(t), `Ada ${run}`);
  await a11y('workflow case dialog');
  await page.locator('dialog[open]').getByRole('button', { name: /invite to interview|zum gespräch einladen/i }).click();
  const note = page.locator('dialog[open] form');
  await note.waitFor();
  await note.locator('button[type=submit]').click();
  await page.waitForFunction(() => /interview|gespräch/i.test(document.querySelector('.ewf-cases tbody')?.textContent ?? ''));
  await page.goto(BASE + '/tasks');
  await idle();
  await page.waitForSelector('main h1');
  await a11y('my tasks');
});

test('workflow 2: automatic steps, deadline, triggers and a signed webhook really work', async () => {
  // an own category: the trigger "new page here" plus the step "create a page
  // here" form a chain (stopped after 5 links) that keeps adding pages in the
  // background; in a shared category those pages leaked into other tests
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Workflow 2 ${run}` } })).id;
  const job = (name) => call('POST', `/api/v1/admin/jobs/${name}/run`, {});
  const me = await call('GET', '/api/v1/auth/me');
  const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Onboarding ${run}`, body: {
    description: 'E2E', fields: [{ id: 'name', label: 'Name', type: 'text', required: true }, { id: 'amount', label: 'Amount', type: 'number' }],
    triggers: [
      { id: 'pages', type: 'content', name: 'New pages', title: '{{trigger.object.title}}', sources: `category:${cat}`, events: ['create'], data: { name: '{{trigger.object.title}}', amount: '1' } },
      { id: 'hook', type: 'webhook', name: 'Shop', title: '{{trigger.data.who}}', data: { name: '{{trigger.data.who}}', amount: '{{trigger.data.n}}' } }],
    steps: [
      { id: 'calc', name: 'Compute', type: 'set', next: 'note', set: [{ field: 'total', formula: 'amount * 2' }] },
      { id: 'note', name: 'Message', type: 'notify', next: 'mk', notify: { to: 'starter', channel: 'inbox', subject: `Case {{case.title}} ${run}` } },
      { id: 'mk', name: 'Page', type: 'create', next: 'check', create: { category: cat, title: `Welcome {{field.name}} ${run}`, store_url: 'page' } },
      { id: 'check', name: 'Check', assignee: { type: 'starter', value: '' }, actions: [{ id: 'ok', label: 'Done', next: 'done' }],
        deadline: { days: 3, workdays: true, warn: [24], escalate: { action: 'notify', notify: { to: 'starter', subject: 'late', channel: 'inbox' } } } },
      { id: 'done', name: 'Finished', type: 'end', outcome: 'success' }] } });
  // the editor shows triggers, deadline and the new steps accessibly
  await page.goto(`${BASE}/e/${wf.id}/edit`);
  await idle();
  await page.locator('#ewf-triggers-h').waitFor();
  await page.getByRole('button', { name: /^(create secret|geheimnis erzeugen)$/i }).click();
  const secret = await page.getByLabel(/^(secret|geheimnis)$/i).inputValue();
  assert.equal(secret.length, 64, 'the secret is shown once');
  await a11y('workflow 2 editor with triggers and secret');
  // start a case with the start form; the automatic steps run in the worker
  await page.goto(`${BASE}/e/${wf.id}`);
  await idle();
  await page.getByRole('button', { name: /start new case|neuen vorgang starten/i }).click();
  const form = page.locator('dialog[open]');
  await form.getByLabel(/^name/i).fill(`Ada ${run}`);
  await form.getByLabel(/^amount/i).fill('21');
  await form.locator('button[type=submit]').click();
  await page.waitForFunction((t) => document.querySelector('dialog[open] h2')?.textContent?.includes(t), `Ada ${run}`);
  await page.keyboard.press('Escape');
  await job('workflow.process.timers');
  const list = await call('GET', `/api/v1/x/workflow.process/elements/${wf.id}/instances`);
  const mine = list.instances.find((x) => x.title === `Ada ${run}`);
  assert.ok(mine, 'case started');
  const x = await call('GET', `/api/v1/x/workflow.process/instances/${mine.id}`);
  assert.equal(x.step, 'check', `case waits in the task: ${JSON.stringify(x)}`);
  assert.equal(x.data.total, '42');
  assert.match(String(x.data.page), /\/e\/\d+$/, 'page created');
  assert.ok(x.due_at, 'deadline set');
  const inbox = await call('GET', '/api/v1/me/inbox');
  assert.ok(inbox.items.some((n) => n.title === `Case Ada ${run} ${run}`), 'message of the notify step');
  // the task in the case dialog: values of the steps, deadline, action
  await page.reload();
  await idle();
  await page.locator('.ewf-cases').getByRole('button', { name: `Ada ${run}`, exact: true }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByText(/deadline|frist/i).first().waitFor();
  await a11y('workflow 2 case with deadline and values');
  await dlg.getByRole('button', { name: /^done$/i }).click();
  await page.locator('dialog[open] form button[type=submit]').click();
  await page.waitForFunction(() => !document.querySelector('dialog[open] form'));
  assert.equal((await call('GET', `/api/v1/x/workflow.process/instances/${mine.id}`)).status, 'done');
  // trigger: the page created by the case started another case (content trigger) – and a new page too
  await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Trigger page ${run}`, body: { format: 'html', source: '<p>x</p>' } });
  // the change reaches the queue right after its commit; the worker decides
  const started = async (title) => {
    for (let i = 0; i < 10; i++) {
      await job('workflow.process.triggers');
      await job('workflow.process.timers');
      const r = await call('GET', `/api/v1/x/workflow.process/elements/${wf.id}/instances`);
      const c = r.instances.find((y) => y.title === title);
      if (c) return c;
      await page.waitForTimeout(300);
    }
    return null;
  };
  assert.ok(await started(`Trigger page ${run}`), 'content trigger started a case');
  // signed webhook from another system (no session, no CSRF header)
  const body = JSON.stringify({ who: `Hook ${run}`, n: 5 });
  const ts = String(Math.floor(Date.now() / 1000));
  const sig = createHmac('sha256', secret).update(`${ts}.${body}`).digest('hex');
  const url = `${BASE}/api/v1/x/workflow.process/hooks/${wf.id}/pages`;
  const hook = `${BASE}/api/v1/x/workflow.process/hooks/${wf.id}/hook`;
  const post = (u, s) => globalThis.fetch(u, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Ewiki-Timestamp': ts, 'X-Ewiki-Signature': `sha256=${s}` }, body });
  assert.equal((await post(hook, '0'.repeat(64))).status, 403, 'wrong signature');
  assert.equal((await post(url, sig)).status, 403, 'not a webhook trigger');
  assert.equal((await post(hook, sig)).status, 202, 'signed webhook accepted');
  const hooked = await started(`Hook ${run}`);
  assert.ok(hooked, 'webhook started a case');
  assert.equal((await call('GET', `/api/v1/x/workflow.process/instances/${hooked.id}`)).data.total, '10');
  assert.ok(me.user.id > 0);
});

test('workflow 2: parallel branches, form of a step, hand-over and deputy', async () => {
  const cat = await m6Category();
  const job = (name) => call('POST', `/api/v1/admin/jobs/${name}/run`, {});
  const me = await call('GET', '/api/v1/auth/me');
  const other = `dep${run}`;
  await call('POST', '/api/v1/admin/users', { username: other, password: 'Dep-Passw0rd-1!', display_name: `Deputy ${run}` });
  const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Contracts ${run}`, body: {
    description: 'E2E', fields: [{ id: 'contract', label: 'Contract', type: 'text', required: true }, { id: 'departments', label: 'Departments', type: 'textarea', required: true },
      { id: 'note', label: 'Review note', type: 'textarea', later: true }],
    steps: [
      { id: 'par', name: 'Reviews', type: 'parallel', next: 'decide', parallel: { branches: ['review'], for_each: 'departments' } },
      { id: 'review', name: 'Review', assignee: { type: 'starter', value: '' }, actions: [{ id: 'ok', label: 'Reviewed', next: 'decide' }], form: [{ field: 'note', mode: 'required' }] },
      { id: 'decide', name: 'Decide', assignee: { type: 'starter', value: '' }, actions: [{ id: 'sign', label: 'Sign', next: 'done' }] },
      { id: 'done', name: 'Finished', type: 'end', outcome: 'success' }] } });
  // the editor shows the parallel step and the form of the step
  await page.goto(`${BASE}/e/${wf.id}/edit`);
  await idle();
  await page.locator('.ewf-step-row--parallel').waitFor();
  await page.locator('.ewf-step-row').nth(1).locator('summary', { hasText: /form of this step|formular dieses schritts/i }).click();
  await a11y('workflow editor with parallel step and step form');
  const c = await call('POST', `/api/v1/x/workflow.process/elements/${wf.id}/instances`, { data: { contract: `NDA ${run}`, departments: 'Legal\nPurchasing' } });
  await job('workflow.process.timers');
  const tasks = (await call('GET', '/api/v1/x/workflow.process/tasks')).tasks.filter((x) => x.title.startsWith(`NDA ${run} – `));
  assert.equal(tasks.length, 2, 'one branch per department');
  // the parent shows its branches
  await page.goto(`${BASE}/e/${wf.id}?case=${c.id}`);
  await idle();
  await page.locator('dialog[open] .ewf-branches li').nth(1).waitFor();
  await a11y('case with branches');
  await page.keyboard.press('Escape');
  // a branch task: the form of the step asks for the note; hand-over dialog
  await page.goto(`${BASE}/tasks`);
  await idle();
  await page.getByRole('button', { name: tasks[0].title }).click();
  let dlg = page.locator('dialog[open]');
  await dlg.getByRole('button', { name: /^(hand over|weitergeben)/i }).click();
  let form = page.locator('dialog[open] form');
  await form.getByLabel(/person who takes over|person, die übernimmt/i).waitFor();
  await a11y('hand-over dialog');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: tasks[0].title }).click();
  dlg = page.locator('dialog[open]');
  await dlg.getByRole('button', { name: /^reviewed$/i }).click();
  form = page.locator('dialog[open] form');
  await form.locator('button[type=submit]').click();
  await form.getByText(/review note|notiz/i).first().waitFor(); // required field of the step
  await form.getByLabel(/^review note/i).fill('fine');
  await a11y('decision with the form of the step');
  await form.locator('button[type=submit]').click();
  await page.waitForFunction(() => !document.querySelector('dialog[open] form'));
  await call('POST', `/api/v1/x/workflow.process/instances/${tasks[1].id}/actions`, { action: 'ok', data: { note: 'ok too' } });
  await job('workflow.process.timers');
  const x = await call('GET', `/api/v1/x/workflow.process/instances/${c.id}`);
  assert.equal(x.step, 'decide', `after both branches: ${x.step}`);
  // deputy in "My tasks"
  await page.goto(`${BASE}/tasks`);
  await idle();
  await page.getByLabel(/deputy \(user name\)|vertretung \(benutzername\)/i).fill(other);
  const until = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10);
  await page.getByLabel(/^(until|bis)/i).fill(until);
  await page.getByRole('button', { name: /^(save deputy|vertretung speichern)$/i }).click();
  await page.getByRole('button', { name: /^(end the deputyship|vertretung beenden)$/i }).waitFor();
  await a11y('my tasks with deputy');
  await page.getByRole('button', { name: /^(end the deputyship|vertretung beenden)$/i }).click();
  await page.getByRole('button', { name: /^(save deputy|vertretung speichern)$/i }).waitFor();
  assert.ok(me.user.id > 0);
});

test('workflow 2: decision links in e-mails, test run, evaluation and retention options', async () => {
  await m6Category();
  // an own category that every signed-in user may read (the addressee must see the case)
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Flows ${run}` } })).id;
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'authenticated', permissions: 1, inherit: true });
  const job = (name) => call('POST', `/api/v1/admin/jobs/${name}/run`, {});
  // a real e-mail delivery: a small SMTP server receives the task e-mail
  const smtp = await smtpFake();
  const prevMail = await call('GET', '/api/v1/admin/settings');
  await call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: prevMail.config.mail.from || 'wiki@example.invalid',
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port: smtp.port, security: 'none', username: '', password: '' } } });
  const who = `lnk${run}`;
  await call('POST', '/api/v1/admin/users', { username: who, password: 'Lnk-Passw0rd-1!', display_name: `Linda ${run}`, email: `${who}@example.invalid` });
  // the administrator allows decisions from e-mails without signing in
  await page.goto(`${BASE}/admin/workflow`);
  await idle();
  const allow = page.getByLabel(/decisions from e-mails without signing in|entscheidungen aus e-mails ohne anmeldung/i);
  await allow.check();
  await a11y('workflow admin page');
  await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.waitForFunction(async () => (await (await __e2eFetch('/api/v1/admin/settings')).json()).config.plugin_settings?.['workflow.process']?.links_without_login === true);
  try {
    const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Leave ${run}`, body: {
      description: 'E2E', links: true, link_hours: 48, retention: { days: 365, mode: 'anonymize' }, fields: [{ id: 'reason', label: 'Reason', type: 'text', required: true }],
      steps: [
        { id: 'mail', name: 'Inform', type: 'email', next: 'check', email: { to: 'outside@example.invalid', subject: 'Hi', body: 'x' } },
        { id: 'check', name: 'Check', assignee: { type: 'user', value: who }, actions: [{ id: 'ok', label: 'Agree', next: 'done' }, { id: 'no', label: 'Refuse', next: 'done', note: 'required' }] },
        { id: 'done', name: 'Finished', type: 'end', outcome: 'success' }] } });
    // the editor shows the options of the process
    await page.goto(`${BASE}/e/${wf.id}/edit`);
    await idle();
    await page.getByLabel(/decision links in task e-mails|entscheidungslinks in aufgaben-e-mails/i).waitFor();
    assert.equal(await page.getByLabel(/keep completed cases|abgeschlossene fälle aufbewahren/i).inputValue(), '365');
    assert.equal(await page.getByLabel(/^(links valid for|links gültig für)/i).inputValue(), '2');
    await a11y('workflow editor options');
    // a case: the task e-mail carries one link per decision
    const c = await call('POST', `/api/v1/x/workflow.process/elements/${wf.id}/instances`, { data: { reason: `Holiday ${run}` } });
    const out = await call('GET', `/api/v1/admin/mail/outbox?q=${encodeURIComponent(`${who}@example.invalid`)}`);
    assert.ok(out[0], 'task e-mail queued');
    // administrators never see the token, only the recipient gets it
    const shown = (await call('GET', `/api/v1/admin/mail/outbox/${out[0].id}`)).body;
    assert.match(shown, /\/workflow\/link\?t=\[hidden\]/, `outbox hides the link: ${shown}`);
    await job('mail');
    const body = smtp.mails.find((m) => m.to.includes(`${who}@example.invalid`))?.text ?? '';
    const tok = (body.split('\n').find((l) => l.startsWith('- Agree: ')) ?? '').split('?t=')[1]?.trim();
    assert.ok(tok, `link in the delivered e-mail: ${body}`);
    assert.ok(!(await call('GET', `/api/v1/admin/mail/outbox/${out[0].id}`)).body.includes(tok), 'token gone after delivery');
    // signed in as someone else: the page says so and does not decide
    await page.goto(`${BASE}/workflow/link?t=${tok}`);
    await page.getByText(/sent to another person|an eine andere person/i).waitFor();
    assert.equal(new URL(page.url()).search, '', 'the token leaves the address bar');
    await a11y('decision link of another person');
    // without signing in: confirm the decision
    const guest = await browser.newPage({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    try {
      await guest.goto(`${BASE}/workflow/link?t=${tok}`);
      const confirm = guest.getByRole('button', { name: /^(confirm|bestätigen): agree$/i });
      await confirm.waitFor();
      await guest.getByText(/valid until|gilt bis/i).waitFor();
      await a11y('decision link without signing in', guest);
      await confirm.click();
      await guest.getByText(/^(done|erledigt):/i).waitFor();
      await a11y('decision link done', guest);
      // once only
      await guest.goto(`${BASE}/workflow/link?t=${tok}`);
      await guest.getByRole('alert').waitFor();
    } finally { await guest.close(); }
    const x = await call('GET', `/api/v1/x/workflow.process/instances/${c.id}`);
    assert.equal(x.status, 'done', `decided by the link: ${x.status}`);
    // a test run from the page: all tasks come to the tester, nothing leaves the wiki
    await page.goto(`${BASE}/e/${wf.id}`);
    await idle();
    await page.getByRole('button', { name: /^(start test run|testlauf starten)$/i }).click();
    const form = page.locator('dialog[open] form');
    await form.getByLabel(/^reason/i).fill('Try');
    await a11y('test run start');
    await form.locator('button[type=submit]').click();
    const dlg = page.locator('dialog[open]');
    await dlg.locator('.ew-badge', { hasText: /test run|testlauf/i }).waitFor();
    await dlg.getByText(/skipped|übersprungen/i).waitFor();
    await dlg.getByRole('button', { name: /^agree$/i }).waitFor();
    await a11y('test run case');
    await page.keyboard.press('Escape');
    // the evaluation
    await page.locator('.ewf-stats summary').click();
    await page.locator('.ewf-stats table tbody tr').first().waitFor();
    await a11y('workflow evaluation');
  } finally {
    const s = await call('GET', '/api/v1/admin/settings');
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail,
      plugin_settings: { 'workflow.process': { ...(s.config.plugin_settings?.['workflow.process'] ?? {}), links_without_login: false } } });
    smtp.close();
  }
});

/** Creates a test CA and an S/MIME certificate for addr with OpenSSL; returns the files. */
function smimeFiles(addr) {
  const dir = mkdtempSync(join(tmpdir(), 'ewiki-smime-'));
  const ssl = (...a) => execFileSync('openssl', a, { cwd: dir, stdio: 'pipe' });
  ssl('req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-keyout', 'ca.key', '-out', 'ca.pem', '-days', '30',
    '-subj', '/CN=E2E S-MIME CA', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign');
  ssl('req', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-keyout', 's.key', '-out', 's.csr', '-subj', '/CN=E2E Sender');
  writeFileSync(join(dir, 'ext.cnf'), `subjectAltName=email:${addr}\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=emailProtection\n`);
  ssl('x509', '-req', '-in', 's.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key', '-CAcreateserial', '-out', 's.pem', '-days', '20', '-extfile', 'ext.cnf');
  ssl('pkcs12', '-export', '-inkey', 's.key', '-in', 's.pem', '-certfile', 'ca.pem', '-out', 's.p12', '-passout', 'pass:e2e-secret');
  return { dir, p12: join(dir, 's.p12'), ca: join(dir, 'ca.pem'), ssl };
}

test('S/MIME: certificate of the organisation in the administration, signed e-mails', async () => {
  const addr = `smime${run}@example.invalid`;
  const files = smimeFiles(addr);
  const smtp = await smtpFake();
  const prevMail = await call('GET', '/api/v1/admin/settings');
  await call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: addr,
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port: smtp.port, security: 'none', username: '', password: '' } } });
  let certID = 0;
  try {
    await page.goto(`${BASE}/admin/smime`);
    await idle();
    await page.getByRole('heading', { name: /certificates of the organisation|zertifikate der organisation/i }).waitFor();
    await a11y('S/MIME administration');
    await page.getByRole('button', { name: /^(upload certificate|zertifikat hochladen)$/i }).first().click();
    const dlg = page.locator('dialog[open]');
    await dlg.getByLabel(/^(file|datei)/i).setInputFiles(files.p12);
    await dlg.getByLabel(/^(password of the file|passwort der datei)/i).fill('e2e-secret');
    await dlg.getByLabel(/^(label|bezeichnung)/i).fill(`E2E ${run}`);
    await a11y('S/MIME upload dialog');
    await dlg.locator('button[type=submit]').click();
    await dlg.waitFor({ state: 'detached' });
    const row = page.locator('table tbody tr', { hasText: `E2E ${run}` });
    await row.waitFor();
    // valid for 20 days: shown as expiring soon
    await row.getByText(/in 20 (days|tagen)/i).waitFor();
    await a11y('S/MIME certificate list');
    const cert = (await call('GET', '/api/v1/x/signature.smime/certs?scope=org')).find((c) => c.label === `E2E ${run}`);
    certID = cert.id;
    // the details show the fingerprint to compare
    await row.getByRole('button', { name: /^details:/i }).click();
    const det = page.locator('dialog[open]');
    await det.getByText(cert.fingerprint).waitFor();
    await det.getByText(cert.serial).waitFor();
    await a11y('S/MIME certificate details');
    await det.getByRole('button', { name: /close|schließen/i }).first().click();
    await det.waitFor({ state: 'detached' });
    // the test e-mail is signed, and OpenSSL verifies it with the CA
    await call('POST', '/api/v1/admin/mail/test', { to: `rcpt${run}@example.invalid` });
    const m = smtp.mails.find((x) => x.to.includes(`rcpt${run}@example.invalid`));
    assert.ok(m, 'test e-mail delivered');
    assert.match(m.raw, /multipart\/signed; protocol="application\/pkcs7-signature"; micalg=sha-256/);
    writeFileSync(join(files.dir, 'mail.eml'), m.raw);
    files.ssl('smime', '-verify', '-in', 'mail.eml', '-CAfile', 'ca.pem', '-purpose', 'smimesign', '-out', 'content.txt');
    assert.match(readFileSync(join(files.dir, 'content.txt'), 'utf8'), /test message/i);
    // the profile explains personal certificates
    await page.goto(`${BASE}/profile`);
    await idle();
    await page.getByRole('heading', { name: /^(s\/mime certificate|s\/mime-zertifikat)$/i }).waitFor();
    await a11y('profile with S/MIME');
  } finally {
    if (certID) await call('DELETE', `/api/v1/x/signature.smime/certs/${certID}`);
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail });
    smtp.close();
    rmSync(files.dir, { recursive: true, force: true });
  }
});

/** A minimal SMTP server for the tests: keeps the decoded text of every message. */
function smtpFake() {
  const mails = [];
  const server = net.createServer((sock) => {
    let buf = '', data = false, msg = { to: [], raw: '' };
    const say = (x) => sock.write(`${x}\r\n`);
    say('220 e2e ESMTP');
    sock.on('data', (chunk) => {
      buf += chunk.toString('utf8');
      let i;
      while ((i = buf.indexOf('\r\n')) >= 0) {
        const line = buf.slice(0, i);
        buf = buf.slice(i + 2);
        if (data) {
          if (line === '.') {
            data = false;
            const body = msg.raw.split('\r\n\r\n').slice(1).join('\r\n\r\n');
            msg.text = body.replace(/=\r\n/g, '').replace(/=([0-9A-F]{2})/g, (_, h) => String.fromCharCode(parseInt(h, 16))).replace(/\r\n/g, '\n');
            msg.text = Buffer.from(msg.text, 'latin1').toString('utf8');
            mails.push(msg);
            msg = { to: [], raw: '' };
            say('250 ok');
          } else msg.raw += `${line.startsWith('..') ? line.slice(1) : line}\r\n`;
          continue;
        }
        const up = line.toUpperCase();
        if (up.startsWith('EHLO') || up.startsWith('HELO')) { sock.write('250-e2e\r\n'); say('250 8BITMIME'); }
        else if (up.startsWith('RCPT TO:')) { msg.to.push(line.slice(8).replace(/[<> ]/g, '')); say('250 ok'); }
        else if (up === 'DATA') { data = true; say('354 go'); }
        else if (up === 'QUIT') { say('221 bye'); sock.end(); }
        else say('250 ok');
      }
    });
    sock.on('error', () => undefined);
  });
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve({ port: server.address().port, mails, close: () => server.close() })));
}

test('workflow 3: a new account starts onboarding with permissions, group and archive', async () => {
  const job = (name) => call('POST', `/api/v1/admin/jobs/${name}/run`, {});
  await job('identity.events');
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `People ${run}` } })).id;
  const docs = (await call('POST', '/api/v1/categories', { titles: { en: `Handbook ${run}` } })).id;
  const arch = (await call('POST', '/api/v1/categories', { titles: { en: `Archive ${run}` } })).id;
  const grp = await call('POST', '/api/v1/admin/groups', { name: `Newcomers ${run}` });
  const pack = await call('POST', '/api/v1/elements', { category_id: docs, type: 'core.html', title: `Pack ${run}`, body: { format: 'markdown', source: 'hi' } });
  const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Onboarding ${run}`, body: {
    description: 'E2E', no_manual: true, fields: [{ id: 'who', label: 'Who', type: 'text' }, { id: 'page', label: 'Page', type: 'text' }],
    triggers: [{ id: 'new', type: 'event', event: 'user.created', title: 'Welcome {{trigger.data.username}}', data: { who: '{{trigger.data.username}}', page: String(pack.id) } },
      { id: 'grp', type: 'event', event: 'group.member_removed', group: grp.id, off: true }],
    steps: [
      { id: 'grant', name: 'Read', type: 'access', next: 'join', access: { mode: 'grant', category: docs, subject: 'user', user: '{{field.who}}', level: 'reader', days: 30 } },
      { id: 'join', name: 'Join', type: 'membership', next: 'move', membership: { mode: 'add', group: grp.id, user: '{{field.who}}' } },
      { id: 'move', name: 'Archive', type: 'move', next: 'done', move: { element: '{{field.page}}', category: arch } },
      { id: 'done', name: 'Done', type: 'end', outcome: 'success' }] } });
  // the editor shows the account trigger and the new steps
  await page.goto(`${BASE}/e/${wf.id}/edit`);
  await idle();
  await page.getByText(/only people who manage accounts|nur personen, die konten verwalten/i).first().waitFor();
  await page.locator('.ewf-step-row').first().getByLabel(/^(for days|für tage)/i).waitFor();
  assert.equal(await page.locator('.ewf-step-row').first().getByLabel(/^(for days|für tage)/i).inputValue(), '30');
  await page.locator('.ewf-step-row').nth(1).getByLabel(/^(group|gruppe)/i).waitFor();
  await a11y('workflow editor with account trigger and account steps');
  // a new person: the whole chain runs
  const who = `newbie${run}`;
  const u = await call('POST', '/api/v1/admin/users', { username: who, password: 'Newbie-Passw0rd-1!', display_name: `Newbie ${run}` });
  await job('identity.events');
  await job('workflow.process.triggers');
  for (let i = 0; i < 3; i++) await job('workflow.process.timers');
  const list = await call('GET', `/api/v1/x/workflow.process/elements/${wf.id}/instances?status=`);
  assert.equal(list.instances.length, 1, 'one onboarding case');
  const x = await call('GET', `/api/v1/x/workflow.process/instances/${list.instances[0].id}`);
  assert.equal(x.status, 'done', `onboarding: ${x.status} ${x.last_error ?? ''}`);
  assert.ok((await call('GET', `/api/v1/admin/groups/${grp.id}/members`)).includes(u.id), 'member of the group');
  assert.equal((await call('GET', `/api/v1/elements/${pack.id}`)).category_id, arch, 'page archived');
  // the case shows what happened
  await page.goto(`${BASE}/e/${wf.id}?case=${x.id}`);
  await idle();
  const dlg = page.locator('dialog[open]');
  await dlg.getByText(/permission granted|berechtigung vergeben/i).waitFor();
  await dlg.getByText(/page moved|seite verschoben/i).waitFor();
  await a11y('case with account steps');
  await page.keyboard.press('Escape');
  // the members of the group: remove and add again in the administration
  await page.goto(`${BASE}/admin/groups`);
  await idle();
  await page.getByRole('button', { name: new RegExp(`(members of|mitglieder von) .?newcomers ${run}`, 'i') }).click();
  const md = page.locator('dialog[open]');
  await md.getByRole('button', { name: new RegExp(`${who}`, 'i') }).click();
  await md.getByText(/no members|keine mitglieder/i).waitFor();
  const opt = (await md.locator('option', { hasText: who }).first().textContent()) ?? '';
  await md.getByLabel(/^(add to the group|zur gruppe hinzufügen)/i).selectOption({ label: opt });
  await md.getByRole('button', { name: /^(add to the group|zur gruppe hinzufügen)$/i }).click();
  await md.getByRole('button', { name: new RegExp(`${who}`, 'i') }).waitFor();
  await a11y('group members dialog');
  await page.keyboard.press('Escape');
  assert.ok((await call('GET', `/api/v1/admin/groups/${grp.id}/members`)).includes(u.id), 'member again');
});

test('workflow 3b: a case writes Word, PDF and Excel files and a calendar entry', async () => {
  const job = (name) => call('POST', `/api/v1/admin/jobs/${name}/run`, {});
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Requests ${run}` } })).id;
  const recs = (await call('POST', '/api/v1/categories', { titles: { en: `Records ${run}` } })).id;
  const cal = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.events', title: `Dates ${run}`, body: { view: 'list' } });
  const wf = await call('POST', '/api/v1/elements', { category_id: cat, type: 'workflow.process', title: `Records ${run}`, body: {
    description: 'E2E', fields: [{ id: 'who', label: 'Name', type: 'text', required: true }],
    steps: [
      { id: 'doc', name: 'Record', type: 'file', next: 'meet', file: { category: recs, name: 'Record {{field.who}}', formats: ['docx', 'pdf', 'xlsx'], content: '# Record\nFor {{field.who}}', data: true, store_url: 'doc' } },
      { id: 'meet', name: 'Meeting', type: 'calendar', next: 'done', calendar: { calendar: cal.id, title: 'Talk {{field.who}}', start: '+2d', minutes: 45, store_id: 'event' } },
      { id: 'done', name: 'Done', type: 'end', outcome: 'success' }] } });
  await page.goto(`${BASE}/e/${wf.id}/edit`);
  await idle();
  const rows = page.locator('.ewf-step-row');
  await rows.first().getByRole('group', { name: /^(formats|formate)$/i }).waitFor();
  assert.ok(await rows.first().getByLabel(/^word \(\.docx\)$/i).isChecked(), 'Word chosen');
  await rows.nth(1).getByLabel(/^(start|beginn)/i).waitFor();
  assert.equal(await rows.nth(1).getByLabel(/^(start|beginn)/i).inputValue(), '+2d');
  await a11y('workflow editor with file and calendar steps');
  // a case from the page
  await page.goto(`${BASE}/e/${wf.id}`);
  await idle();
  await page.getByRole('button', { name: /^(start|starten)/i }).first().click();
  const form = page.locator('dialog[open] form');
  await form.getByLabel(/^name/i).fill(`Lea ${run}`);
  await form.locator('button[type=submit]').click();
  await page.locator('dialog[open]').waitFor();
  await page.keyboard.press('Escape');
  // the steps run in the background: run the job until the case is done
  // (under load one run may not reach the end)
  // the start may still be on its way: wait for the case to exist
  let list;
  for (let i = 0; i < 50; i++) {
    list = await call('GET', `/api/v1/x/workflow.process/elements/${wf.id}/instances?status=`);
    if (list.instances?.length) break;
    await page.waitForTimeout(100);
  }
  assert.ok(list.instances?.length, 'the case was started');
  let x;
  for (let i = 0; i < 20; i++) {
    await job('workflow.process.timers');
    x = await call('GET', `/api/v1/x/workflow.process/instances/${list.instances[0].id}`);
    if (x.status === 'done' || x.last_error) break;
  }
  assert.equal(x.status, 'done', `files and calendar: ${x.status} ${x.last_error ?? ''}`);
  // the files are in the media library with their category
  await page.goto(`${BASE}/media?category=${recs}`);
  await idle();
  for (const ext of ['docx', 'pdf', 'xlsx']) await page.getByRole('button', { name: `Record Lea ${run} (${ext.toUpperCase()})` }).waitFor();
  await a11y('media library with generated files');
  const ev = await call('GET', `/api/v1/x/content.events/events/${x.data.event}`);
  assert.equal(ev.event.title, `Talk Lea ${run}`);
  assert.equal((new Date(ev.event.ends_at) - new Date(ev.event.starts_at)) / 60000, 45);
  // the history of the case names both steps
  await page.goto(`${BASE}/e/${wf.id}?case=${x.id}`);
  await idle();
  const dlg = page.locator('dialog[open]');
  await dlg.getByText(/file written|datei erzeugt/i).waitFor();
  await dlg.getByText(/calendar entry created|kalendereintrag angelegt/i).waitFor();
  await a11y('case with file and calendar steps');
  await page.keyboard.press('Escape');
});

test('drafts, preview, templates and publishing with options', async () => {
  const cat = await m6Category();
  await page.goto(`${BASE}/c/${cat}/new`);
  await idle();
  await page.getByLabel(/^(title|titel)/i).fill(`Draft ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type(`Draft text wombat${run}`);
  // preview without saving
  await page.getByRole('button', { name: /^(preview|vorschau)$/i }).click();
  await page.locator('dialog[open]').getByText(`wombat${run}`).waitFor();
  await a11y('preview dialog');
  await page.locator('dialog[open]').getByRole('button', { name: /close|schließen/i }).click();
  // save as draft: listed in the category, not published
  await page.getByRole('button', { name: /^(save as draft|als entwurf speichern)$/i }).click();
  await page.waitForURL(/\/d\/\d+\/edit$/);
  await page.goto(`${BASE}/c/${cat}`);
  await idle();
  await page.locator('.ew-drafts a', { hasText: `Draft ${run}` }).click();
  await page.waitForURL(/\/d\/\d+\/edit$/);
  await idle();
  // save as template
  await page.getByRole('button', { name: /^(save as template|als vorlage speichern)$/i }).click();
  await page.locator('dialog[open]').getByLabel(/^(name)/i).fill(`Template ${run}`);
  await page.locator('dialog[open] button[type=submit]').click();
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  // publish with options
  await page.getByRole('button', { name: /publish with options|veröffentlichen mit optionen/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/notify watchers|beobachter benachrichtigen/i).waitFor();
  await a11y('publish dialog');
  await dlg.locator('button[type=submit]').click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  assert.match(await page.locator('main').innerText(), new RegExp(`wombat${run}`));
  // the template is offered for new pages
  await page.goto(`${BASE}/c/${cat}/new`);
  await idle();
  const tpl = page.getByLabel(/^(template|vorlage)$/i);
  await tpl.waitFor();
  await tpl.selectOption({ label: `Template ${run}` });
  await page.waitForFunction((n) => document.querySelector('.ewe-prose')?.textContent?.includes(n), `wombat${run}`);
  // the template is changed: name, description and the content from the editor
  await page.locator('.ewe-prose').first().click();
  await page.keyboard.press('End');
  await page.keyboard.type(` numbat${run}`);
  await page.getByRole('button', { name: /^(manage templates|vorlagen verwalten)$/i }).click();
  const mgr = page.locator('dialog[open]').first();
  await mgr.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): Template ${run}$`, 'i') }).click();
  const ed = page.locator('dialog[open]').last();
  await ed.getByLabel(/^name/i).fill(`Template ${run} v2`);
  await ed.getByLabel(/^(description|beschreibung)/i).fill(`changed ${run}`);
  await ed.getByRole('checkbox', { name: /^(replace title, content and tags|titel, inhalt und schlagwörter)/i }).check();
  await a11y('change a template');
  await ed.getByRole('button', { name: /^(save|speichern)$/i }).click();
  await mgr.getByText(`Template ${run} v2`).waitFor();
  const changed = (await call('GET', '/api/v1/templates')).find((x) => x.name === `Template ${run} v2`);
  assert.equal(changed?.description, `changed ${run}`, 'description changed');
  assert.match(JSON.stringify((await call('GET', `/api/v1/templates/${changed.id}`)).body), new RegExp(`numbat${run}`), 'content from the editor');
  await mgr.getByRole('button', { name: /close|schließen/i }).first().click();
});

test('share a page, notification bell, assigned watches', async () => {
  const cat = await m6Category();
  // the test user is a member of the group hr (also when the workflow test did not get that far)
  const me = await call('GET', '/api/v1/auth/me');
  let hr = (await call('GET', '/api/v1/groups')).find((g) => g.name === 'hr');
  if (!hr) hr = { id: (await call('POST', '/api/v1/admin/groups', { name: 'hr' })).id };
  if (!me.user.groups.includes(hr.id)) await call('PUT', `/api/v1/admin/users/${me.user.id}`, { groups: [...me.user.groups, hr.id] });
  // share dialog on a page (does not change permissions; checks recipients)
  await page.goto(`${BASE}/search?q=wombat${run}`);
  await idle();
  await page.locator('.ew-result a').first().click();
  await idle();
  await page.getByRole('button', { name: /^(share|teilen):/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByRole('checkbox', { name: 'hr', exact: true }).check();
  await dlg.getByRole('button', { name: /^(check recipients|empfänger prüfen)$/i }).click();
  await dlg.getByText(/recipient|empfänger/i).first().waitFor();
  await a11y('share dialog');
  await dlg.getByRole('button', { name: /close|schließen/i }).first().click();
  // the bell opens the notification panel (the workflow test left a message of its notify step)
  const shared = (await call('GET', '/api/v1/me/inbox?limit=20')).items[0];
  assert.ok(shared, 'there is a notification');
  await page.getByRole('button', { name: /^(notifications|benachrichtigungen)/i }).click();
  await page.locator('.ew-inbox').waitFor();
  await a11y('notification panel');
  // a notification is deleted; the panel stays open
  await page.locator('.ew-inbox').getByRole('button', { name: `${/^de/.test(await page.evaluate(() => document.documentElement.lang)) ? 'Benachrichtigung löschen' : 'Delete notification'}: ${shared.title}` }).first().click();
  await page.locator('.ew-inbox').getByRole('heading', { name: /^(notifications|benachrichtigungen)$/i }).waitFor();
  for (const deadline = Date.now() + 5000; (await call('GET', '/api/v1/me/inbox?limit=20')).items.some((n) => n.id === shared.id) && Date.now() < deadline;) await page.waitForTimeout(100);
  assert.equal((await call('GET', '/api/v1/me/inbox?limit=20')).items.some((n) => n.id === shared.id), false, 'notification deleted');
  assert.equal(await page.locator('.ew-inbox').isVisible(), true, 'the panel stays open');
  await page.keyboard.press('Escape');
  // assign a watch to the group hr (the test user is a member) and see it in the profile
  const a = await call('POST', '/api/v1/admin/watch-assignments', { subject_type: 'group', subject_id: hr.id, target_type: 'category', target_id: cat, mode: 'link', frequency: 'daily', mandatory: false, note: `note ${run}` });
  await page.goto(`${BASE}/admin/watches`);
  await idle();
  await page.getByRole('button', { name: /^(assign watch|beobachtung zuweisen)$/i }).click();
  await page.locator('dialog[open]').getByLabel(/^(note|hinweis)/i).waitFor();
  await a11y('watch assignment dialog');
  await page.locator('dialog[open]').getByRole('button', { name: /cancel|abbrechen/i }).click();
  await page.goto(`${BASE}/profile`);
  await idle();
  const sec = page.locator('section[aria-labelledby=wa-h]');
  await sec.getByText(`note ${run}`).waitFor();
  const box = sec.getByRole('checkbox').first();
  await box.uncheck();
  await page.waitForFunction(() => document.querySelector('[role=status]')?.textContent?.length);
  await call('DELETE', `/api/v1/admin/watch-assignments/${a.id}`);
});

test('comments on a page', async () => {
  await page.goto(`${BASE}/search?q=quokka${run}`);
  await idle();
  await page.locator('.ew-result a').first().click();
  await idle();
  await page.waitForSelector('.ewcm');
  await page.getByLabel(/your comment|ihr kommentar/i).fill(`Nice page ${run}`);
  await page.getByRole('button', { name: /add comment|kommentar hinzufügen/i }).click();
  await page.waitForFunction((t) => document.querySelector('.ewcm-list')?.textContent?.includes(t), `Nice page ${run}`);
  await page.locator('.ewcm-item').first().getByRole('button', { name: /^(reply|antworten)$/i }).click();
  await page.locator('.ewcm-item form textarea').fill('A reply');
  await page.locator('.ewcm-item form button[type=submit]').click();
  await page.waitForSelector('.ewcm-replies .ewcm-item');
  await a11y('comments');
});

test('edit category texts and the default page from the category view', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(edit category|kategorie bearbeiten)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.waitFor();
  await dlg.getByLabel(/^(tags)/i).fill(`start, e2e${run}`);
  await a11y('edit category dialog');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector(`.ew-tags a[href*="e2e${run}"]`);
  assert.ok(await page.locator('.ew-default-element').getByRole('link', { name: /^(edit page|seite bearbeiten)$/i }).count() === 1, 'edit button for the default page');
});

test('menus, blocks and positions', async () => {
  // menu at a position of the active template, item pointing to a category
  await page.goto(BASE + '/admin/menus');
  await idle();
  await page.getByRole('button', { name: /^(add menu|menü hinzufügen)$/i }).click();
  let dlg = page.locator('dialog[open]').first();
  await dlg.waitFor();
  await dlg.getByLabel(/^(menu name|menüname)/i).fill(`Menu ${run}`);
  await dlg.locator('select').first().selectOption('sidebar-bottom');
  // items are listed; one item is edited at a time in its own dialog
  await dlg.getByRole('button', { name: /^(add item|eintrag hinzufügen)$/i }).click();
  const item = page.locator('dialog[open]').last();
  await item.getByLabel(/^(label|beschriftung) \(en\)/i).fill(`Item ${run}`);
  await a11y('menu item dialog');
  await item.locator('button[type=submit]').click();
  // a target is required
  await page.waitForFunction(() => /target|ziel/i.test([...document.querySelectorAll('dialog[open] .ew-form-alert')].pop()?.textContent ?? ''));
  await item.locator('.ewm-target select').first().selectOption({ index: 1 });
  await item.locator('button[type=submit]').click();
  await dlg.locator('.ewm-edit-title', { hasText: `Item ${run}` }).waitFor();
  await a11y('menu dialog');
  await dlg.locator('button[type=submit]').last().click();
  await page.waitForSelector(`.ew-pos--sidebar-bottom a:has-text("Item ${run}")`);
  await a11y('menu administration');
  // a block: an element of the type Block, placed from its own page
  const bcat = await call('POST', '/api/v1/categories', { titles: { en: `Blocks ${run}` } });
  const blk = await call('POST', '/api/v1/elements', { category_id: bcat.id, type: 'core.block', title: `Contact ${run}`, body: { format: 'html', source: `<p><strong>Contact</strong> block${run}</p>` } });
  assert.equal(blk.hidden, true, 'a block is never listed');
  await page.goto(BASE + `/e/${blk.id}`);
  await idle();
  const where = page.locator('.ewb-where');
  await where.getByText(/not placed yet|noch nicht platziert/i).waitFor();
  await where.getByRole('button', { name: /^(place the block|baustein platzieren)$/i }).click();
  dlg = page.locator('dialog[open]');
  await dlg.waitFor();
  await dlg.getByLabel(/^position$/i).selectOption('footer');
  await a11y('place block dialog');
  await dlg.locator('button[type=submit]').click();
  await where.locator('li').first().waitFor();
  await a11y('where a block is shown');
  await page.goto(BASE + `/c/${bcat.id}`);
  await idle();
  await page.waitForSelector(`.ew-pos--footer .ewb-block:has-text("block${run}")`);
  // the blocks are offered in the placement of the administration
  await page.goto(BASE + '/admin/placement');
  await idle();
  // position preview
  await page.goto(BASE + '/?positions=1');
  await idle();
  await page.waitForSelector('.ew-show-positions .ew-pos--content-top');
  await page.getByRole('button', { name: /^(hide positions|positionen ausblenden)$/i }).click();
  assert.equal(await page.locator('.ew-show-positions').count(), 0);
  // the template in use is marked on the plugin page
  await page.goto(BASE + '/admin/plugins');
  await idle();
  assert.ok(await page.getByText(/template in use|verwendete vorlage/i).count() >= 1);
  assert.ok(await page.getByRole('button', { name: /^(use as template|als vorlage verwenden):/i }).count() >= 1);
});

test('CMS template', async () => {
  const set = (tpl) => page.evaluate(async (tpl) => {
    const h = { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' };
    const cur = await (await __e2eFetch('/api/v1/admin/settings', { headers: h })).json();
    const res = await __e2eFetch('/api/v1/admin/settings', { method: 'PUT', headers: h, body: JSON.stringify({ ui: { ...cur.config.ui, template: tpl } }) });
    if (!res.ok) throw new Error(await res.text());
  }, tpl);
  await set('theme.cms');
  try {
    await page.goto(BASE + '/');
    await idle();
    await page.waitForSelector('.ewcms-header .ewcms-nav-list a');
    await a11y('cms template');
    await page.setViewportSize({ width: 420, height: 800 });
    const burger = page.locator('.ewcms-burger');
    await burger.click();
    assert.equal(await burger.getAttribute('aria-expanded'), 'true');
    assert.ok(await page.locator('.ewcms-nav.is-open').isVisible(), 'off-canvas navigation opens');
    await a11y('cms template mobile navigation');
    await page.keyboard.press('Escape');
    assert.equal(await burger.getAttribute('aria-expanded'), 'false');
    // the modern variant inherits layout and settings and only restyles
    await page.setViewportSize({ width: 1280, height: 900 });
    await set('theme.cms.modern');
    await page.goto(BASE + '/');
    await idle();
    await page.waitForSelector('body.ewcms.ewcms-modern .ewcms-header .ewcms-nav-list a');
    await a11y('cms modern template');
  } finally {
    await page.setViewportSize({ width: 1280, height: 900 });
    await set('theme.wiki');
  }
});

/** Calls the API with the browser session (the page must be on the wiki). */
const call = (method, path, body) => page.evaluate(async ([method, path, body]) => {
  const res = await __e2eFetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}, [method, path, body]);

let m6Cat = 0;
async function m6Category() {
  if (!m6Cat) {
    await page.goto(BASE + '/');
    await idle();
    const c = await call('POST', '/api/v1/categories', { titles: { en: `M6 ${run}` } });
    m6Cat = c.id;
  }
  return m6Cat;
}

test('multilevel menu: dropdown with a submenu', async () => {
  const cat = await m6Category();
  const sub = await call('POST', '/api/v1/x/core.menu/menus', { name: `Sub ${run}`, location: '', items: [
    { type: 'category', category_id: cat }, { type: 'url', label: { en: `Deep ${run}` }, url: '/search' }] });
  await call('POST', '/api/v1/x/core.menu/menus', { name: `Main ${run}`, location: 'header-nav', items: [
    { type: 'heading', label: { en: `Group ${run}` }, children: [{ type: 'menu', menu_id: sub.id, label: { en: `More ${run}` } }] }] });
  await page.goto(BASE + '/');
  await idle();
  const toggle = page.getByRole('button', { name: `Group ${run}` });
  await toggle.waitFor();
  assert.equal(await toggle.getAttribute('aria-expanded'), 'false');
  await toggle.click();
  assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
  // every target at least 24 × 24 px (WCAG 2.5.8; found by axe in the CI)
  for (const b of await page.locator('.ew-overview--tree .ew-tree-open, .ew-overview--tree .ew-tree-toggle').all()) {
    const box = await b.boundingBox();
    assert.ok(box && box.width >= 24 && box.height >= 24, `target of ${JSON.stringify(box)}`);
  }
  const more = page.getByRole('button', { name: `More ${run}` });
  await more.click();
  await page.getByRole('link', { name: `Deep ${run}` }).waitFor();
  await a11y('open multilevel menu');
  await page.keyboard.press('Escape');
  assert.equal(await more.getAttribute('aria-expanded'), 'false');
});

test('watch a page and subscribe to a category feed', async () => {
  const cat = await m6Category();
  await call('PUT', `/api/v1/categories/${cat}`, { feed: 'recursive' });
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Watched ${run}`, body: { format: 'html', source: '<p>x</p>' } });
  await page.goto(BASE + `/e/${el.id}`);
  await idle();
  await page.getByRole('button', { name: /^(watch|beobachten)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/new content|neuer inhalt/i).check();
  await a11y('watch dialog');
  await dlg.locator('button[type=submit]').click();
  await page.getByRole('button', { name: /^(watching|beobachtet)$/i }).waitFor();
  await page.goto(BASE + `/c/${cat}`);
  await idle();
  assert.equal(await page.locator('link[rel=alternate][type="application/atom+xml"]').count(), 1);
  await page.getByRole('button', { name: /^feed:/i }).click();
  await page.locator('dialog[open] input[readonly]').first().waitFor();
  assert.match(await page.locator('dialog[open] input[readonly]').first().inputValue(), /token=ft_/);
  await a11y('feed dialog');
  await page.keyboard.press('Escape');
});

test('blog: posts, search and new post', async () => {
  const cat = await m6Category();
  const blogCat = await call('POST', '/api/v1/categories', { parent_id: cat, titles: { en: `News ${run}` } });
  const blog = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.blog', title: `Blog ${run}`,
    body: { category_id: blogCat.id, recursive: true, per_page: 2, layout: 'cards', feed: true, show_date: true, show_author: true, intro: { format: 'markdown', source: 'Our **news**' } } });
  for (const n of [1, 2, 3]) {
    await call('POST', '/api/v1/elements', { via: blog.id, category_id: blogCat.id, type: 'core.html', title: `Post ${n} ${run}`, body: { format: 'html', source: `<p>About giraffes ${n}</p>` }, tags: [`tag${n % 2}`] });
  }
  await page.goto(BASE + `/e/${blog.id}`);
  await idle();
  // on its own page the controls of the blog are blocks of the page
  await page.locator('.ew-pos--element-header .ewb-search').waitFor();
  await page.locator('.ew-pos--element-toolbar').getByRole('button', { name: /^(new post|neuer beitrag)$/i }).waitFor();
  assert.equal(await page.locator('.ewb-toolbar').count(), 0, 'no toolbar of its own on the page of the blog');
  assert.equal(await page.locator('.ewb-post').count(), 2);
  await a11y('blog');
  await page.getByRole('button', { name: /older posts|ältere beiträge/i }).click();
  await page.waitForFunction(() => document.querySelectorAll('.ewb-post').length === 1);
  await page.getByRole('searchbox', { name: /search the blog|im blog suchen/i }).fill('giraffes');
  await page.locator('.ewb-search button[type=submit]').click();
  await page.waitForFunction(() => document.querySelectorAll('.ewb-hit').length === 3);
  // moved for this page: the search below the content, the feed hidden
  await call('PUT', `/api/v1/placements/instance?target=${blog.id}`, { placements: [{ block: 'content.blog.search', position: 'element-footer', sort: 1 }, { block: 'content.blog.feed', position: '' }] });
  await page.reload();
  await idle();
  await page.locator('.ew-pos--element-footer .ewb-search').waitFor();
  assert.equal(await page.getByRole('button', { name: /^feed:/i }).count(), 0, 'feed hidden');
  await page.getByRole('searchbox', { name: /search the blog|im blog suchen/i }).fill('giraffes');
  await page.locator('.ewb-search button[type=submit]').click();
  await page.waitForFunction(() => document.querySelectorAll('.ewb-hit').length === 3);
  // embedded (default element of a category): the blog shows its controls itself
  const front = await call('POST', '/api/v1/categories', { parent_id: cat, titles: { en: `Front ${run}` } });
  const shown = await call('POST', '/api/v1/elements', { category_id: front.id, type: 'content.blog', title: `Front blog ${run}`, body: { category_id: blogCat.id, per_page: 2 } });
  await call('PUT', `/api/v1/categories/${front.id}`, { default_element_id: shown.id });
  await page.goto(BASE + `/c/${front.id}`);
  await idle();
  await page.locator('.ewb-toolbar .ewb-search').waitFor();
  await page.locator('.ewb-toolbar').getByRole('button', { name: /^(new post|neuer beitrag)$/i }).click();
  await page.waitForURL(/\/c\/\d+\/new\?type=core\.html&via=\d+&return=/);
});

test('labels of an instance: own texts of the blog per language', async () => {
  const cat = await m6Category();
  const blog = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.blog', title: `Labelled ${run}`, body: { category_id: cat } });
  await page.goto(`${BASE}/e/${blog.id}`);
  await idle();
  await page.locator('.ew-pos--element-header .ewb-search').waitFor();
  await page.getByRole('button', { name: /^(labels|beschriftungen)$/i }).click();
  const dlg = page.locator('dialog[open]');
  const group = dlg.getByRole('group', { name: /^(new post|neuer beitrag)$/i });
  await group.waitFor();
  await a11y('labels of an instance');
  await group.getByRole('textbox', { name: /^(english|englisch)$/i }).fill('Write an article');
  await group.getByRole('textbox', { name: /^(german|deutsch)$/i }).fill('Artikel schreiben');
  await dlg.locator('button[type=submit]').click();
  await page.getByRole('button', { name: /^(write an article|artikel schreiben)$/i }).waitFor();
  const l = await call('GET', `/api/v1/elements/${blog.id}/labels`);
  const np = l.labels.find((x) => x.key === 'content.blog.new_post');
  assert.equal(np.value.en, 'Write an article');
  assert.equal(np.value.de, 'Artikel schreiben');
});

test('permissions of an instance: a blog editor without page permissions, own permissions', async () => {
  const cat = await m6Category();
  const blog = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.blog', title: `Rights blog ${run}`, body: { category_id: cat } });
  const name = `blogger${run}`;
  const pw = 'Blog-Passw0rd-1!';
  const u = await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Blogger ${run}` });
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'user', subject_id: u.id, permissions: 1 });
  await page.goto(BASE + `/e/${blog.id}`);
  await idle();
  await page.getByRole('button', { name: /^(permissions|rechte)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByText(/independent of the permissions for pages|unabhängig von den rechten für seiten/i).waitFor({ state: 'attached' }); // an explanation: hidden unless the person shows help texts
  await dlg.getByLabel(/^(applies to|gilt für)/i).selectOption('user');
  const who = dlg.getByPlaceholder(/name or user name|name oder benutzername/i);
  await who.fill(name);
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] datalist option')].some((o) => o.value.includes(n)), name);
  await who.fill(await page.evaluate((n) => [...document.querySelectorAll('dialog[open] datalist option')].find((o) => o.value.includes(n)).value, name));
  await dlg.getByRole('button', { name: /^(add a person, group or role|person, gruppe oder rolle hinzufügen)$/i }).click();
  const rule = dlg.getByRole('group', { name: new RegExp(`Blogger ${run}`) });
  await rule.getByRole('checkbox', { name: /^(edit all posts|alle beiträge bearbeiten)/i }).check();
  await a11y('permissions of an instance');
  await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.locator('.ew-toast', { hasText: /permissions saved|rechte gespeichert/i }).first().waitFor();
  const r = await call('GET', `/api/v1/elements/${blog.id}/rights`);
  assert.deepEqual(r.rules.map((x) => [x.subject_id, x.actions]), [[u.id, ['content.blog.edit']]]);
  // the person sees their own permissions (no rules of others)
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`blogger: ${e.message}`));
  try {
    await p2.goto(BASE + '/login?local=1');
    await p2.locator('input[name=username]').fill(name);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((x) => !x.pathname.startsWith('/login'));
    await p2.goto(BASE + `/e/${blog.id}`);
    await p2.getByRole('button', { name: /^(permissions|rechte)$/i }).click();
    const own = p2.locator('dialog[open]');
    await own.getByText(/you have these permissions here|diese rechte haben sie hier/i).waitFor();
    await own.getByText(/^(edit all posts|alle beiträge bearbeiten)$/i).waitFor();
    await own.getByText(/^(contribute posts|beiträge beitragen)$/i).waitFor();
    assert.equal(await own.getByRole('checkbox').count(), 0, 'no rules to change');
    await own.getByRole('button', { name: /close|schließen/i }).first().click();
    // a post in the real editor on behalf of the blog, without page permissions
    await p2.getByRole('button', { name: /^(new post|neuer beitrag)$/i }).click();
    await p2.waitForURL(/\/c\/\d+\/new\?type=core\.html&via=\d+&return=/);
    await p2.getByLabel(/^(title|titel)/i).fill(`Via ${run}`);
    await p2.locator('.ewe-prose').click();
    await p2.keyboard.type(`Written by the blogger ${run}`);
    await p2.locator('form.ew-edit-form button[type=submit]').first().click();
    await p2.waitForURL(new RegExp(`/e/${blog.id}$`));
    const posts = await call('GET', `/api/v1/x/content.blog/elements/${blog.id}/posts?lang=en`);
    const written = posts.items.find((x) => x.title === `Via ${run}`);
    assert.ok(written, 'the post is in the blog');
    assert.equal((await call('GET', `/api/v1/elements/${written.id}`)).owner_instance, blog.id, 'written on behalf of the blog');
  } finally { await ctx2.close(); }
});

test('placement: scope for a category – only on its page, without the subcategories', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Scope ${run}` } })).id;
  const sub = (await call('POST', '/api/v1/categories', { titles: { en: `Scope sub ${run}` }, parent_id: cat })).id;
  const inIt = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `In scope ${run}`, body: { format: 'markdown', source: 'x' } });
  try {
    await page.goto(`${BASE}/c/${cat}/settings?tab=placement`);
    await idle();
    await page.getByRole('combobox', { name: /^(position of search|position von suche)$/i }).selectOption('footer');
    const pages = page.getByRole('checkbox', { name: /^(on all pages of the category|auf allen seiten der kategorie): (search|suche)$/i });
    const subs = page.getByRole('checkbox', { name: /^(include subcategories|unterkategorien einbeziehen): (search|suche)$/i });
    assert.ok(await pages.isChecked() && await subs.isChecked(), 'by default everywhere');
    await pages.uncheck();
    await subs.uncheck();
    await page.locator('[aria-live=polite]', { hasText: /subcategories|unterkategorien/i }).waitFor({ state: 'attached' });
    assert.ok(!(await pages.isChecked()) && !(await subs.isChecked()), 'both unchecked after redrawing');
    await a11y('scope of a placement');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    const stored = (await call('GET', `/api/v1/placements/category?target=${cat}`)).placements.find((p) => p.block === 'core.search');
    assert.ok(stored.only_category_page && stored.no_subcategories, 'stored with its scope');
    // the page of the category: search in the footer
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    await page.locator('.ew-pos--footer form[role=search]').waitFor();
    // a page in it and the subcategory: the placement of the site (header)
    for (const url of [`${BASE}/e/${inIt.id}`, `${BASE}/c/${sub}`]) {
      await page.goto(url);
      await idle();
      await page.locator('.ew-pos--header-tools form[role=search]').waitFor();
      assert.equal(await page.locator('.ew-pos--footer form[role=search]').count(), 0, `no search in the footer: ${url}`);
    }
    // a block hidden in the category gets the same scope: hidden here, but
    // not in the subcategories
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
    await page.goto(`${BASE}/c/${cat}/settings?tab=placement`);
    await idle();
    await page.getByRole('combobox', { name: /^(position of search|position von suche)$/i }).selectOption('');
    const hidden = page.getByRole('region', { name: /^(hidden in this category|in dieser kategorie ausgeblendet)$/i });
    await hidden.getByText(/^(search|suche)$/i).waitFor();
    const hsubs = hidden.getByRole('checkbox', { name: /^(include subcategories|unterkategorien einbeziehen): (search|suche)$/i });
    assert.ok(await hsubs.isChecked(), 'hidden in the subcategories by default');
    await hsubs.uncheck();
    await page.locator('[aria-live=polite]', { hasText: /subcategories|unterkategorien/i }).waitFor({ state: 'attached' });
    await a11y('scope of a hidden block');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    const hiddenRow = (await call('GET', `/api/v1/placements/category?target=${cat}`)).placements.find((p) => p.block === 'core.search');
    assert.ok(hiddenRow && hiddenRow.position === '' && hiddenRow.no_subcategories && !hiddenRow.only_category_page, 'stored hidden, without the subcategories');
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    await page.locator('main').waitFor();
    assert.equal(await page.locator('form[role=search]').count(), 0, 'no search in the category');
    await page.goto(`${BASE}/c/${sub}`);
    await idle();
    await page.locator('.ew-pos--header-tools form[role=search]').waitFor();
    // and back to what is inherited
    await page.goto(`${BASE}/c/${cat}/settings?tab=placement`);
    await idle();
    await hidden.getByRole('button', { name: /^(show as above again|wieder wie darüber anzeigen): (search|suche)$/i }).click();
    assert.equal(await hidden.count(), 0, 'nothing hidden any more');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    assert.equal((await call('GET', `/api/v1/placements/category?target=${cat}`)).placements.length, 0, 'nothing of its own');
  } finally {
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
  }
});

test('placement: search in the footer for the site, no category tree in one category, the user menu stays', async () => {
  const cat = await m6Category();
  try {
    await page.goto(`${BASE}/admin/placement`);
    await idle();
    await page.getByRole('heading', { name: /^(placement of the building blocks|platzierung der bausteine)$/i }).waitFor();
    await a11y('placement administration');
    await page.getByRole('combobox', { name: /^(position of search|position von suche)$/i }).selectOption('footer');
    await page.getByRole('region', { name: /^(placement of the building blocks|platzierung der bausteine)$/i }).getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    await page.locator('.ew-pos--footer form[role=search]').waitFor();
    assert.equal(await page.locator('.ew-pos--header-tools form[role=search]').count(), 0, 'search left the header');
    // a category without the category tree (its subcategories too)
    await page.goto(`${BASE}/c/${cat}/settings?tab=placement`);
    await idle();
    await page.getByRole('combobox', { name: /^(position of category tree|position von kategoriebaum)$/i }).selectOption('');
    await a11y('placement of a category');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    await page.locator('.ew-pos--footer form[role=search]').waitFor();
    assert.equal(await page.locator('nav.ew-tree').count(), 0, 'no tree in the category');
    await page.goto(`${BASE}/`);
    await idle();
    await page.locator('nav.ew-tree').waitFor();
    // a menu is a block of its own: placed a second time in the footer
    const menu = await call('POST', '/api/v1/x/core.menu/menus', { name: `Placed ${run}`, location: 'sidebar', items: [{ label: { en: 'Home', de: 'Start' }, type: 'url', url: '/' }] });
    try {
      await page.goto(`${BASE}/admin/placement`);
      await idle();
      await page.getByRole('combobox', { name: new RegExp(`^(position of|position von) (menu|menü): placed ${run}$`, 'i') }).waitFor();
      await call('PUT', '/api/v1/placements/site', { placements: [{ block: `core.menu/${menu.id}`, position: 'sidebar', sort: 1 }, { block: `core.menu/${menu.id}`, position: 'footer', sort: 1 }] });
      await page.goto(`${BASE}/`);
      await idle();
      await page.locator('.ew-pos--footer nav', { hasText: 'Home' }).first().waitFor();
      await page.locator('.ew-pos--sidebar nav', { hasText: 'Home' }).first().waitFor();
      await a11y('a menu placed twice');
    } finally {
      await call('DELETE', `/api/v1/x/core.menu/menus/${menu.id}`);
    }
    // the user menu cannot get lost: hidden on the site, it still appears
    await call('PUT', '/api/v1/placements/site', { placements: [{ block: 'core.user', position: '' }] });
    await page.reload();
    await idle();
    await page.locator('.ew-user-menu').waitFor();
  } finally {
    await call('PUT', '/api/v1/placements/site', { placements: [] });
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
  }
});

test('arrange one page: title hidden (kept for screen readers), share below the content', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Arranged ${run}`, body: { format: 'html', source: '<p>arranged</p>' } });
  try {
    // the site placement offers the positions of a page
    await page.goto(`${BASE}/admin/placement`);
    await idle();
    await page.getByRole('heading', { name: /^(page: toolbar|seite: werkzeugleiste) \(element-toolbar\)$/i }).waitFor();
    await page.getByRole('combobox', { name: /^(position of title of the page|position von titel der seite)$/i }).waitFor();
    // the page: title in the header, actions in the toolbar
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    await page.locator('.ew-pos--element-header h1', { hasText: `Arranged ${run}` }).waitFor();
    await page.locator('.ew-pos--element-toolbar[role=group]').getByRole('button', { name: /^(share|teilen)/i }).waitFor();
    const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
    if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
    await page.getByRole('button', { name: /^(arrange page|seite anordnen)$/i }).click();
    const dlg = page.locator('dialog[open]');
    await dlg.getByRole('combobox', { name: /^(position of title of the page|position von titel der seite)$/i }).selectOption('');
    await dlg.getByRole('combobox', { name: /^(position of share|position von teilen)$/i }).selectOption('element-footer');
    // the management of the page cannot be hidden, and only positions of a page are offered
    const manage = dlg.getByRole('combobox', { name: /^(position of managing the page|position von seite verwalten)/i });
    assert.deepEqual(await manage.locator('option').evaluateAll((o) => o.map((x) => x.value)), ['element-header', 'element-toolbar', 'element-content', 'element-aside', 'element-footer']);
    await a11y('arranging one page');
    await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    await page.locator('dialog[open]').waitFor({ state: 'detached' });
    await page.locator('.ew-pos--element-footer').getByRole('button', { name: /^(share|teilen)/i }).waitFor();
    assert.equal(await page.locator('.ew-pos--element-header h1').count(), 0, 'no visible title');
    assert.equal(await page.getByRole('heading', { level: 1, name: `Arranged ${run}` }).count(), 1, 'the heading stays for screen readers');
    await a11y('page without a visible title');
    const lv = await call('GET', `/api/v1/placements/instance?target=${el.id}`);
    assert.deepEqual(lv.placements.map((x) => `${x.block}@${x.position}`).sort(), ['core.element.title@', 'core.share@element-footer']);
    const off = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
    if ((await off.getAttribute('aria-pressed')) === 'true') { await off.click(); await idle(); }
  } finally {
    await call('PUT', `/api/v1/placements/instance?target=${el.id}`, { placements: [] });
  }
});

test('editing page: tags in the side column, no formatting toolbar, actions stay; typed text survives', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Edited ${run}`, body: { format: 'html', source: '<p>edit me</p>' } });
  try {
    // default: the formatting toolbar above the content, tags and comment below, actions at the end
    await page.goto(`${BASE}/e/${el.id}/edit`);
    await idle();
    await page.locator('.ew-pos--editor-toolbar [role=toolbar]').waitFor();
    await page.locator('.ew-pos--editor-footer').getByRole('textbox', { name: /^(tags|schlagwörter)/i }).waitFor();
    await page.locator('.ew-pos--editor-statusbar').getByRole('button', { name: /^(publish|veröffentlichen)$/i }).waitFor();
    // the category arranges its editing page; hiding the actions is not possible (they come back)
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [
      { block: 'core.editor.tags', position: 'editor-aside', sort: 1 },
      { block: 'core.editor.format', position: '' },
      { block: 'core.editor.actions', position: '' },
    ] });
    await page.goto(`${BASE}/e/${el.id}/edit`);
    await idle();
    // (found by its label: with suggestions the field becomes a combobox)
    const tags = page.locator('.ew-pos--editor-aside[role=complementary]').getByLabel(/^(tags|schlagwörter)/i);
    await tags.waitFor();
    assert.equal(await page.locator('[role=toolbar]').count(), 0, 'no formatting toolbar');
    await page.locator('.ew-pos--editor-statusbar').getByRole('button', { name: /^(publish|veröffentlichen)$/i }).waitFor();
    await a11y('editing page arranged');
    // the parts keep what was typed when the positions are filled again
    // typing asks for tag suggestions (wait for the answer: it must not be cut off by leaving the page)
    const suggestions = page.waitForResponse((r) => r.url().includes('/api/v1/tags?prefix=') && r.ok());
    await tags.fill(`arranged${run}`);
    await suggestions;
    // (as when the template is switched: all positions are filled again)
    await page.evaluate(() => document.dispatchEvent(new Event('ewiki:template')));
    await idle();
    assert.equal(await tags.inputValue(), `arranged${run}`, 'typed text kept');
    await page.getByRole('textbox', { name: /^(title|titel)/i }).fill(`Edited again ${run}`);
    await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
    await page.waitForURL(new RegExp(`/e/${el.id}$`));
    const saved = await call('GET', `/api/v1/elements/${el.id}`);
    assert.equal(saved.title, `Edited again ${run}`);
    assert.deepEqual(saved.tags, [`arranged${run}`]);
  } finally {
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
  }
});

test('buttons of the formatting toolbar: chosen by a category; saving the settings keeps the other values', async () => {
  const cat = await m6Category();
  const sub = await call('POST', '/api/v1/categories', { parent_id: cat, titles: { en: `Simple ${run}` } });
  const el = await call('POST', '/api/v1/elements', { category_id: sub.id, type: 'core.html', title: `Few buttons ${run}`, body: { format: 'html', source: '<p>x</p>' } });
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  try {
    // the category chooses bold, italic and link
    await page.goto(`${BASE}/c/${sub.id}/settings`);
    await idle();
    const fs = page.getByRole('group', { name: /^(buttons of the formatting toolbar|knöpfe der formatierungsleiste)$/i });
    await fs.getByRole('radio', { name: /^(a choice|auswahl)$/i }).check();
    for (const b of await fs.getByRole('checkbox').all()) await b.uncheck();
    await a11y('choice of the toolbar buttons');
    // nothing chosen is refused with a message
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-form-alert', { hasText: /at least one button|mindestens einen knopf/i }).waitFor();
    for (const n of [/^(bold|fett)$/i, /^(italic|kursiv)$/i, /^link$/i]) await fs.getByRole('checkbox', { name: n }).check();
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /saved|gespeichert/i }).first().waitFor();
    assert.deepEqual((await call('GET', `/api/v1/categories/${sub.id}`)).category.editor_tools, ['strong', 'em', 'link']);
    // the editor shows only these (and the switch to Markdown)
    await page.goto(`${BASE}/e/${el.id}/edit`);
    await idle();
    const bar = page.locator('.ew-pos--editor-toolbar [role=toolbar]');
    await bar.getByRole('button', { name: /^(bold|fett)$/i }).waitFor();
    const names = await bar.locator('button, select').evaluateAll((xs) => xs.filter((x) => !x.hidden).map((x) => x.getAttribute('aria-label') ?? x.textContent));
    assert.equal(names.length, 4, `bold, italic, link, Markdown: ${names}`);
    assert.equal(await bar.getByRole('button', { name: /^(image|bild)$/i }).count(), 0, 'image hidden');
    // the general settings keep values the form does not show (personal arrangement, the site's toolbar)
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, personal_placement: true, editor_tools: ['*'] }, retention: { ...before.retention, mail_days: 17 } });
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    await page.getByRole('checkbox', { name: /^(allow personal arrangement|persönliche anordnung erlauben)/i }).waitFor();
    assert.equal(await page.getByRole('checkbox', { name: /^(allow personal arrangement|persönliche anordnung erlauben)/i }).isChecked(), true);
    await page.locator('form').filter({ has: page.getByRole('checkbox', { name: /^(allow personal arrangement|persönliche anordnung erlauben)/i }) }).getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /saved|gespeichert/i }).first().waitFor();
    const after = (await call('GET', '/api/v1/admin/settings')).config;
    assert.equal(after.ui.personal_placement, true, 'personal arrangement kept');
    assert.equal(after.retention.mail_days, 17, 'retention of the mail outbox kept');
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui, retention: before.retention });
  }
});

test('own dashboard: default tiles, arranged with the dialog and with the mouse, offered tiles', async () => {
  const me = (await call('GET', '/api/v1/auth/me')).user;
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  try {
    await page.goto(`${BASE}/dashboard`);
    await idle();
    await page.getByRole('heading', { level: 1, name: /^(my dashboard|mein dashboard)$/i }).waitFor();
    for (const n of [/^(notifications|benachrichtigungen)$/i, /^(edited by me last|zuletzt von mir bearbeitet)$/i, /^(changed recently|zuletzt geändert)$/i, /^(watched pages|beobachtete seiten)$/i]) {
      await page.locator('.ew-dashboard').getByRole('heading', { level: 2, name: n }).waitFor();
    }
    await a11y('own dashboard');
    // without the mouse: the dialog puts "Changed recently" over the full width
    await page.getByRole('button', { name: /^(arrange tiles|kacheln anordnen)$/i }).click();
    const dlg = page.locator('dialog[open]');
    await dlg.getByRole('combobox', { name: /^(position of changed recently|position von zuletzt geändert)$/i }).selectOption('dashboard-wide');
    // only positions of the dashboard are offered
    const opts = await dlg.getByRole('combobox', { name: /^(position of changed recently|position von zuletzt geändert)$/i }).locator('option').evaluateAll((o) => o.map((x) => x.value));
    assert.deepEqual(opts.filter(Boolean), ['dashboard-wide', 'dashboard-1', 'dashboard-2', 'dashboard-3']);
    await a11y('arranging the dashboard');
    await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('dialog[open]').waitFor({ state: 'detached' });
    await page.locator('.ew-pos--dashboard-wide').getByRole('heading', { name: /^(changed recently|zuletzt geändert)$/i }).waitFor();
    // with the mouse: the notifications go to column 3
    await page.getByRole('button', { name: /^(move with the mouse|mit der maus verschieben)$/i }).click();
    await page.locator('.ew-tile[draggable="true"]').first().waitFor();
    const saved = page.waitForResponse((r) => r.url().includes('/api/v1/placements/user') && r.request().method() === 'PUT');
    await page.locator('.ew-tile', { has: page.getByRole('heading', { name: /^(notifications|benachrichtigungen)$/i }) }).dragTo(page.locator('.ew-pos--dashboard-3'));
    assert.equal((await saved).status(), 200);
    await page.locator('.ew-pos--dashboard-3').getByRole('heading', { name: /^(notifications|benachrichtigungen)$/i }).waitFor();
    const own = await call('GET', `/api/v1/placements/user?target=${me.id}`);
    assert.ok(own.placements.some((p) => p.block === 'core.dash.notifications' && p.position === 'dashboard-3'), 'notifications in column 3');
    assert.ok(own.placements.some((p) => p.block === 'core.dash.recent' && p.position === 'dashboard-wide'), 'recent kept over the full width');
    // the administration offers fewer tiles: watched pages disappear
    await page.goto(`${BASE}/admin/placement`);
    await idle();
    const offered = page.getByRole('group', { name: /^(tiles for the own dashboard|kacheln für das eigene dashboard)$/i });
    await offered.getByRole('checkbox', { name: /^(watched pages|beobachtete seiten)$/i }).uncheck();
    await offered.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /offered tiles saved|angebotene kacheln gespeichert/i }).first().waitFor();
    await page.goto(`${BASE}/dashboard`);
    await idle();
    await page.locator('.ew-dashboard').getByRole('heading', { level: 2, name: /^(edited by me last|zuletzt von mir bearbeitet)$/i }).waitFor();
    assert.equal(await page.locator('.ew-dashboard').getByRole('heading', { name: /^(watched pages|beobachtete seiten)$/i }).count(), 0, 'not offered');
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('PUT', `/api/v1/placements/user?target=${me.id}`, { placements: [] });
  }
});

test('dashboard as the start page: set in the settings, tiles of the plugins, the state of the wiki', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  try {
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    await page.getByRole('checkbox', { name: /^(own dashboard as the start page|eigenes dashboard als startseite)$/i }).check();
    const put = page.waitForResponse((r) => r.url().endsWith('/api/v1/admin/settings') && r.request().method() === 'PUT');
    await page.locator('form.ew-form button[type=submit]').first().click();
    assert.equal((await put).status(), 200);
    assert.equal((await call('GET', '/api/v1/admin/settings')).config.ui.dashboard_home, true, 'saved');
    // the start page is the own dashboard now
    const status = page.waitForResponse((r) => r.url().includes('/api/v1/admin/status'));
    const forms = page.waitForResponse((r) => r.url().includes('/api/v1/x/content.forms/submissions/recent'));
    const events = page.waitForResponse((r) => r.url().includes('/api/v1/x/content.events/upcoming'));
    await page.goto(`${BASE}/`);
    await page.waitForURL(/\/dashboard$/);
    for (const r of await Promise.all([status, forms, events])) assert.equal(r.status(), 200, r.url());
    const board = page.locator('.ew-dashboard');
    for (const n of [/^(state of the wiki|zustand des wikis)$/i, /^(upcoming events|nächste termine)$/i, /^(new submissions|neue einsendungen)$/i, /^(cases i started|von mir gestartete vorgänge)$/i]) {
      await board.getByRole('heading', { level: 2, name: n }).waitFor();
    }
    // the state names problems with a word, the version is there
    await board.locator('.ew-tile-status').getByText(/^(version)$/i).waitFor();
    await a11y('dashboard with all tiles');
    // the start page of the site stays reachable
    await page.getByRole('link', { name: /^(go to the start page of the wiki|zur startseite des wikis)$/i }).click();
    // (the start page of the site may lead on to its only category)
    await page.waitForURL(/\/(start|c\/\d+)$/);
    await idle();
    assert.equal(await page.locator('.ew-dashboard').count(), 0, 'the start page of the site, not the dashboard');
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
  }
});

test('category display: cards set in the settings, handbook navigation, tree, featured page arranged on the page', async () => {
  const top = await call('POST', '/api/v1/categories', { titles: { en: `Manual ${run}` } });
  const sub = await call('POST', '/api/v1/categories', { parent_id: top.id, titles: { en: `Chapter ${run}` } });
  const mk = (c, title) => call('POST', '/api/v1/elements', { category_id: c, type: 'core.html', title, body: { format: 'markdown', source: `${title} text` } });
  const one = await mk(top.id, `First ${run}`);
  const two = await mk(top.id, `Second ${run}`);
  const deep = await mk(sub.id, `Deep ${run}`);
  // the display is chosen in the settings (tab "Appearance and content")
  await page.goto(`${BASE}/c/${top.id}/settings?tab=appearance`);
  await idle();
  const fs = page.getByRole('group', { name: /^(display of the pages|darstellung der seiten)$/i });
  await fs.getByLabel(/^(display|darstellung)$/i).selectOption('own');
  await fs.getByLabel(/^(kind|art)$/i).selectOption('cards');
  await a11y('category display settings');
  const put = page.waitForResponse((r) => r.url().endsWith(`/api/v1/categories/${top.id}/settings`) && r.request().method() === 'PUT');
  await page.locator('form.ew-form button[type=submit]').first().click();
  assert.equal((await put).status(), 200);
  assert.equal((await call('GET', `/api/v1/categories/${top.id}/settings`)).own.display.view, 'cards');
  // the category page shows cards; the subcategory inherits them
  const ov = page.waitForResponse((r) => r.url().includes(`/api/v1/categories/${top.id}/overview`));
  await page.goto(`${BASE}/c/${top.id}`);
  assert.equal((await ov).status(), 200);
  await idle();
  // (the first page is the default page of the category: shown in full above, not in the overview)
  await page.locator('.ew-overview--cards').getByRole('link', { name: `Second ${run}` }).waitFor();
  await page.locator('.ew-pos--category-content .ew-default-element').getByRole('heading', { name: `First ${run}` }).waitFor();
  await a11y('category page with cards');
  // the tree: the chapter opens with its pages
  await call('PUT', `/api/v1/categories/${top.id}/settings`, { display: { view: 'tree' } });
  await page.goto(`${BASE}/c/${top.id}`);
  await idle();
  const toggle = page.locator('.ew-overview--tree').getByRole('button', { name: `Chapter ${run}` });
  await toggle.click();
  await page.locator('.ew-overview--tree').getByRole('link', { name: `Deep ${run}` }).waitFor();
  assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
  await a11y('category page with a tree');
  // the handbook: the pages as navigation in the side column, also on a page
  await call('PUT', `/api/v1/categories/${top.id}/settings`, { display: { view: 'handbook' } });
  await page.goto(`${BASE}/e/${two.id}`);
  await idle();
  const nav = page.getByRole('navigation', { name: `Manual ${run}` });
  await nav.getByRole('link', { name: `First ${run}` }).waitFor();
  assert.equal(await nav.getByRole('link', { name: `Second ${run}` }).getAttribute('aria-current'), 'page');
  await a11y('page with the handbook navigation');
  // arranging the category page: the second page is featured in the side column
  await call('PUT', `/api/v1/categories/${top.id}/settings`, { display: { view: 'list' } });
  await page.goto(`${BASE}/c/${top.id}`);
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(arrange category page|kategorieseite anordnen)$/i }).click();
  const dlg = page.locator('dialog[open]');
  const add = dlg.getByRole('group', { name: /^(show a building block|baustein anzeigen)$/i });
  await add.getByLabel(/^(building block|baustein)$/i).selectOption(`core.category.element/${two.id}`);
  await add.getByLabel(/^(position)$/i).selectOption('category-aside');
  await add.getByRole('button').click();
  await a11y('arranging a category page');
  const saved = page.waitForResponse((r) => r.url().includes(`/api/v1/placements/category_page`) && r.request().method() === 'PUT');
  await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
  assert.equal((await saved).status(), 200);
  const batch = page.waitForResponse((r) => r.url().includes('/api/v1/elements/batch'));
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  assert.equal((await batch).status(), 200);
  await page.locator('.ew-pos--category-aside').getByRole('heading', { name: `Second ${run}` }).waitFor();
  const off = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await off.getAttribute('aria-pressed')) === 'true') { await off.click(); await idle(); }
  assert.ok(one.id > 0 && deep.id > 0);
});

test('category display: table with a property column, accordion, tabs with the keyboard, A–Z, timeline, all one below the other', async () => {
  const c = await call('POST', '/api/v1/categories', { titles: { en: `Views ${run}` } });
  const mk = (title) => call('POST', '/api/v1/elements', { category_id: c.id, type: 'core.html', title, body: { format: 'markdown', source: `Body of ${title}` } });
  const intro = await mk(`Intro ${run}`); // the default page: shown in full above the overview
  const apple = await mk(`Apple ${run}`);
  const zebra = await mk(`Zebra ${run}`);
  const pear = await mk(`Pear ${run}`);
  for (const [el, room] of [[apple, 'B 12'], [zebra, 'A 1'], [pear, 'C 7']]) {
    await call('PUT', `/api/v1/x/core.properties/elements/${el.id}`, { values: { room }, free: { room: { type: 'text', label: 'Room' } } });
  }
  const show = async (display) => {
    await call('PUT', `/api/v1/categories/${c.id}/settings`, { display });
    const ov = page.waitForResponse((r) => r.url().includes(`/api/v1/categories/${c.id}/overview`));
    await page.goto(`${BASE}/c/${c.id}`);
    assert.equal((await ov).status(), 200);
    await idle();
  };
  // the table: the column is chosen in the settings among those offered
  await page.goto(`${BASE}/c/${c.id}/settings?tab=appearance`);
  await idle();
  const fs = page.getByRole('group', { name: /^(display of the pages|darstellung der seiten)$/i });
  await fs.getByLabel(/^(display|darstellung)$/i).selectOption('own');
  await fs.getByLabel(/^(kind|art)$/i).selectOption('table');
  await fs.getByRole('checkbox', { name: 'Room' }).check();
  const put = page.waitForResponse((r) => r.url().endsWith(`/api/v1/categories/${c.id}/settings`) && r.request().method() === 'PUT');
  await page.locator('form.ew-form button[type=submit]').first().click();
  assert.equal((await put).status(), 200);
  assert.deepEqual((await call('GET', `/api/v1/categories/${c.id}/settings`)).own.display.columns, ['room']);
  const q = page.waitForResponse((r) => r.url().includes('/api/v1/x/core.properties/query'));
  const ov = page.waitForResponse((r) => r.url().includes(`/api/v1/categories/${c.id}/overview`));
  await page.goto(`${BASE}/c/${c.id}`);
  assert.equal((await ov).status(), 200);
  assert.equal((await q).status(), 200);
  await idle();
  const table = page.locator('.ew-overview-table');
  await table.getByRole('columnheader', { name: /room/i }).getByRole('button').click();
  assert.equal(await table.getByRole('columnheader', { name: /room/i }).getAttribute('aria-sort'), 'ascending');
  assert.deepEqual(await table.locator('tbody th').allTextContents(), [`Zebra ${run}`, `Apple ${run}`, `Pear ${run}`]);
  await a11y('category page as a table');
  // the accordion: opened with the button, the content was loaded at once
  const batch = page.waitForResponse((r) => r.url().includes('/api/v1/elements/batch') && r.url().includes(String(apple.id)));
  await show({ view: 'accordion' });
  assert.equal((await batch).status(), 200);
  const acc = page.locator('.ew-accordion').getByRole('button', { name: `Apple ${run}` });
  await acc.click();
  assert.equal(await acc.getAttribute('aria-expanded'), 'true');
  await page.locator('.ew-accordion').getByText(`Body of Apple ${run}`).waitFor();
  await a11y('category page as an accordion');
  // the tabs: arrow keys move between the tabs
  await show({ view: 'tabs', sort: 'title' });
  const first = page.getByRole('tab', { name: `Apple ${run}` });
  assert.equal(await first.getAttribute('aria-selected'), 'true');
  await first.focus();
  await page.keyboard.press('ArrowRight');
  const second = page.getByRole('tab', { name: `Pear ${run}` });
  assert.equal(await second.getAttribute('aria-selected'), 'true');
  assert.equal(await page.evaluate(() => document.activeElement?.textContent), `Pear ${run}`);
  await page.getByRole('tabpanel').getByText(`Body of Pear ${run}`).waitFor();
  await page.keyboard.press('End');
  assert.equal(await page.getByRole('tab', { name: `Zebra ${run}` }).getAttribute('aria-selected'), 'true');
  await a11y('category page as tabs');
  // A–Z: the letters lead to their pages
  await show({ view: 'az' });
  const letters = page.getByRole('navigation', { name: /^(first letters|anfangsbuchstaben)$/i });
  assert.deepEqual(await letters.getByRole('link').allTextContents(), ['A', 'P', 'Z']);
  await letters.getByRole('link', { name: 'Z' }).click();
  await page.locator('.ew-az').getByRole('link', { name: `Zebra ${run}` }).waitFor();
  await a11y('category page as an A–Z index');
  // the timeline and all one below the other
  await show({ view: 'timeline', date: 'created' });
  assert.equal(await page.locator('.ew-timeline time').count(), 3);
  await a11y('category page as a timeline');
  await show({ view: 'all' });
  for (const n of ['Apple', 'Zebra', 'Pear']) await page.locator('.ew-all-pages').getByRole('heading', { name: `${n} ${run}` }).waitFor();
  await page.locator('.ew-all-pages').getByText(`Body of Zebra ${run}`).waitFor();
  await a11y('category page with all pages one below the other');
  // on a small phone (320 px) nothing makes the page scroll sideways (WCAG 1.4.10)
  const size = page.viewportSize();
  try {
    await page.setViewportSize({ width: 320, height: 720 });
    for (const view of ['table', 'tabs', 'az', 'timeline', 'all', 'accordion']) {
      await show({ view, columns: ['room'] });
      const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(over <= 0, `${view}: the page scrolls sideways by ${over} px on a small phone`);
    }
  } finally {
    if (size) await page.setViewportSize(size);
  }
  assert.ok(intro.id > 0);
});

test('layout blocks: a section with options from the macro dialog, an icon box with the icon picker, grid, tabs with the keyboard, accordion; desktop and phone', async () => {
  // a block in the content gets its options in the dialog of the macro
  await newPage('core.html', `Layout ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type('Intro');
  const macroDlg = await insertBlock('section');
  await macroDlg.getByRole('button', { name: /^(options|optionen)$/i }).click();
  const opts = page.locator('dialog[open]').last();
  await opts.locator('summary', { hasText: /^(background|hintergrund)$/i }).click();
  await opts.getByLabel(/^(background colour: own colour|hintergrundfarbe: eigene farbe)$/i).check();
  await opts.getByLabel(/^(background colour|hintergrundfarbe)$/i).fill('#102040');
  await opts.locator('summary', { hasText: /^(text|schrift)$/i }).click();
  await opts.getByLabel(/^(text colour: own colour|textfarbe: eigene farbe)$/i).check();
  await opts.getByLabel(/^(text colour|textfarbe)$/i).fill('#ffffff');
  await opts.getByRole('button', { name: /^(apply|übernehmen)$/i }).click();
  await macroDlg.getByText(/^(2 option\(s\) set|2 option\(en\) gesetzt)$/i).waitFor();
  await a11y('macro dialog with options');
  await macroDlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="section"]');
  await page.keyboard.type('Section body');
  // an icon box: the icon from the picker (search), the whole box one link
  await page.keyboard.press('Control+End');
  const iconDlg = await insertBlock('iconbox');
  await iconDlg.getByRole('button', { name: /^(choose an icon|symbol auswählen)$/i }).click();
  const picker = page.locator('dialog[open]').last();
  await picker.getByLabel(/^(search|suchen)$/i).fill('rocket');
  const rocket = picker.getByRole('button', { name: /^(rocket|Rakete \(rocket\))$/ });
  await rocket.waitFor();
  await a11y('icon picker');
  // the German search words are shipped with the icons
  const de = await page.evaluate(async () => {
    const url = document.querySelector('.ew-iconpick-grid .ew-icon')?.getAttribute('style') ?? '';
    const base = /url\("([^"]+)\/(outline|filled)\//.exec(url)?.[1];
    const r = base ? await __e2eFetch(`${base}/terms.de.json`) : null;
    return r?.ok ? (await r.json()).words.rocket : null;
  });
  assert.deepEqual(de, ['Rakete']);
  await rocket.click();
  await iconDlg.locator('code', { hasText: 'rocket' }).waitFor();
  await iconDlg.getByLabel(/^(title|titel)/i).fill('Launch');
  await iconDlg.getByLabel(/^(link \(the whole box\)|link \(der ganze kasten\))/i).fill('/search');
  await iconDlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="iconbox"]');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  const frame = page.locator('.ew-content .ew-bo', { hasText: 'Section body' });
  await frame.waitFor();
  assert.deepEqual(await frame.evaluate((el) => { const c = getComputedStyle(el); return [c.backgroundColor, c.color]; }), ['rgb(16, 32, 64)', 'rgb(255, 255, 255)']);
  // the icon shows its file, the box is one link
  const icon = page.locator('.ewl-iconbox .ew-icon[data-icon="rocket"]');
  const url = await icon.evaluate((el) => getComputedStyle(el).getPropertyValue('--ew-icon'));
  assert.match(url, /\/assets\/icons\/[^/]+\/outline\/rocket\.svg/);
  assert.equal((await page.request.get(BASE + url.match(/url\("([^"]+)"\)/)[1])).status(), 200);
  assert.equal(await page.locator('.ewl-iconbox a').count(), 1);
  // without a label screen readers read the name of the icon
  await page.getByRole('img', { name: 'rocket', exact: true }).waitFor();
  await page.getByRole('link', { name: 'Launch' }).waitFor();
  // grid, tabs and accordion on one page
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Layout ${run}` } })).id;
  const src = ':::grid{desktop=3 tablet=2}\nOne\n***\nTwo\n***\nThree\n:::\n\n:::tabs{label="Steps"}\n## First\nText A\n***\n## Second\nText B\n***\n## Third\nText C\n:::\n\n:::accordion{single=true first=true}\n### Question one\nAnswer one\n***\n### Question two\nAnswer two\n:::\n\n::buttons{text1="Start" link1="/" text2="More" link2="/search" layout=full}\n';
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Blocks ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const tabs = page.getByRole('tablist', { name: 'Steps' });
  await tabs.waitFor();
  await page.getByRole('tab', { name: 'First' }).focus();
  await page.keyboard.press('ArrowRight');
  assert.equal(await page.getByRole('tab', { name: 'Second' }).getAttribute('aria-selected'), 'true');
  await page.getByRole('tabpanel', { name: 'Second' }).getByText('Text B').waitFor();
  assert.ok(!(await page.getByText('Text A').isVisible()), 'only the chosen tab is shown');
  await page.keyboard.press('End');
  assert.equal(await page.getByRole('tab', { name: 'Third' }).getAttribute('aria-selected'), 'true');
  // the accordion keeps one open
  assert.ok(await page.getByText('Answer one').isVisible());
  await page.locator('summary', { hasText: 'Question two' }).click();
  await page.getByText('Answer two').waitFor();
  assert.ok(!(await page.getByText('Answer one').isVisible()), 'only one part open');
  assert.equal(await page.locator('.ewl-grid').evaluate((g) => getComputedStyle(g).gridTemplateColumns.split(' ').length), 3);
  await a11y('layout blocks (desktop)');
  const size = page.viewportSize();
  try {
    await page.setViewportSize({ width: 375, height: 800 });
    assert.equal(await page.locator('.ewl-grid').evaluate((g) => getComputedStyle(g).gridTemplateColumns.split(' ').length), 1);
    const wide = await page.evaluate(() => {
      const w = document.documentElement.clientWidth;
      const out = [];
      for (const e of document.querySelectorAll('body *')) {
        if (e.getBoundingClientRect().right <= w + 1) continue;
        // inside an area that scrolls by itself (a wide table) is fine
        let p = e.parentElement;
        let inScroller = false;
        while (p) { const o = getComputedStyle(p).overflowX; if (o === 'auto' || o === 'scroll' || o === 'hidden') { inScroller = true; break; } p = p.parentElement; }
        if (!inScroller) out.push(`${e.tagName.toLowerCase()}.${[...e.classList].join('.')} ${Math.round(e.getBoundingClientRect().right)} in ${e.closest('.ew-panel')?.outerHTML.slice(0, 300) ?? ''}`);
      }
      return { scroll: document.documentElement.scrollWidth - w, out: out.slice(0, 6) };
    });
    assert.ok(wide.scroll <= 0, `the page scrolls sideways: ${JSON.stringify(wide)}`);
    await a11y('layout blocks (phone)');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('formulas: TeX set as MathML, nothing else of it reaches the page', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Formulas ${run}` } })).id;
  const src = 'Pythagoras: :math{tex="a^2 + b^2 = c^2"} in a line.\n\n:math{tex="\\\\sum_{i=1}^{n} i = \\\\frac{n(n+1)}{2}" display=block}\n\n' +
    '```math\n\\int_0^1 x\\,dx\n```\n\nColour: :math{tex="\\\\color{red}{x}"}. Hostile: :math{tex="\\\\href{javascript:alert(1)}{x} \\\\style{color:red}{y}"} and :math{tex="\\\\frac{"}\n';
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Formulas ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  await page.waitForFunction(() => document.querySelectorAll('.ew-content math').length >= 4);
  const info = await page.evaluate(() => {
    const maths = [...document.querySelectorAll('.ew-content math')];
    return {
      count: maths.length,
      ns: maths.every((m) => m.namespaceURI === 'http://www.w3.org/1998/Math/MathML'),
      block: maths.filter((m) => m.getAttribute('display') === 'block').length,
      bad: document.querySelectorAll('.ew-content math a, .ew-content math [href], .ew-content math [style], .ew-content math script, .ew-content math foreignObject').length,
      error: document.querySelectorAll('.ew-math--error').length,
      text: document.querySelector('.ew-content')?.textContent ?? '',
    };
  });
  assert.equal(info.count, 4, 'four formulas set');
  assert.ok(info.ns, 'as MathML');
  assert.equal(info.block, 2, 'two on their own line');
  assert.equal(info.bad, 0, 'no links, styles or scripts in a formula');
  assert.equal(info.error, 2, 'what is not allowed or not valid TeX stays as TeX');
  assert.ok(info.text.includes('\\frac{'), 'the TeX of the broken formula is readable');
  await a11y('formulas');
});

test('diagrams from text: drawn in a frame without access to the wiki, the text stays readable', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Diagrams ${run}` } })).id;
  const src = '```mermaid\nflowchart LR\n  A[Request] --> B{Approved?}\n  B -->|yes| C[Order okapi]\n  B -->|no| D[Reply]\n```\n\n' +
    '```mermaid\nthis is not a diagram <img src=x onerror=alert(1)>\n```\n';
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Diagrams ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const frames = page.locator('.ew-content iframe.ew-diagram-frame');
  assert.equal(await frames.count(), 2);
  assert.equal(await frames.first().getAttribute('sandbox'), 'allow-scripts', 'a frame without its own origin');
  await page.frameLocator('.ew-content iframe.ew-diagram-frame >> nth=0').locator('svg').getByText('Order okapi').waitFor();
  // the frame reaches neither the page nor the cookies of the wiki
  const f = page.frames().find((x) => x.url().endsWith('/diagram-frame'));
  assert.ok(f, 'the frame is there');
  const reach = await f.evaluate(() => {
    let parentDoc = true;
    try { parentDoc = !!window.parent.document.body; } catch { parentDoc = false; }
    let cookie = '';
    try { cookie = document.cookie; } catch { cookie = ''; }
    return { parentDoc, cookie, origin: window.origin };
  });
  assert.deepEqual(reach, { parentDoc: false, cookie: '', origin: 'null' });
  // its height follows the drawing
  assert.ok((await frames.first().boundingBox()).height > 60, 'the frame is as high as the drawing');
  // a text that is no diagram: a note, and the text below (never as HTML)
  await page.locator('.ew-diagram').nth(1).getByText(/could not be drawn|ließ sich nicht zeichnen/i).waitFor();
  assert.equal(await page.locator('.ew-content img[src="x"]').count(), 0, 'no HTML from the text of a diagram');
  await page.locator('.ew-diagram').first().locator('summary').click();
  await page.locator('.ew-diagram').first().getByText('A[Request] --> B{Approved?}').waitFor();
  await a11y('diagrams from text');
});

test('emoji: found by a word, chosen with the keyboard, inserted as text', async () => {
  await newPage('core.html', `Emoji ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type('Thanks ');
  await page.getByRole('button', { name: /^(insert emoji|emoji einfügen)$/i }).click();
  const dlg = page.locator('dialog[open]').last();
  const search = dlg.getByRole('combobox', { name: /^(search emoji|emoji suchen)$/i });
  await search.waitFor();
  // the groups without a search
  const list = dlg.getByRole('listbox', { name: /^emoji$/i });
  assert.ok(await list.getByRole('option').count() > 20, 'a group of emoji');
  await search.fill('thumbs up');
  await dlg.getByRole('status').filter({ hasText: /found|gefunden/ }).waitFor();
  const first = list.getByRole('option').first();
  assert.equal(await first.getAttribute('aria-selected'), 'true', 'the first one is active');
  await a11y('emoji dialog');
  await search.press('ArrowRight');
  await search.press('ArrowLeft');
  const chosen = (await first.locator('[aria-hidden="true"]').textContent()).trim();
  await search.press('Enter');
  await dlg.waitFor({ state: 'detached' });
  // the dialog loses its open attribute at once, but its close event (which
  // inserts the emoji) comes a moment later: wait for the text, not the dialog
  await page.waitForFunction((want) => document.querySelector('.ewe-prose')?.textContent?.includes(want), `Thanks ${chosen}`, { timeout: 5000 })
    .catch(async () => assert.fail(`inserted at the cursor: ${JSON.stringify(await page.locator('.ewe-prose').textContent())}`));
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ew-content').getByText(`Thanks ${chosen}`).waitFor();
});

test('wiki blocks: include a page chosen by title, a date, page tree with filter, search box for a category, new page from a template, spaces', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Wiki blocks ${run}` } })).id;
  const sub = (await call('POST', '/api/v1/categories', { titles: { en: `Chapter ${run}` }, parent_id: cat })).id;
  const md = (source) => ({ format: 'markdown', source });
  const rules = await call('POST', '/api/v1/elements', { category_id: sub, type: 'core.html', title: `Okapi rules ${run}`, body: md('Long text.\n\n:::excerpt\nIn short: lights off.\n:::\n') });
  await call('POST', '/api/v1/elements', { category_id: sub, type: 'core.html', title: `Zebra notes ${run}`, body: md('zebra') });
  const tpl = await call('POST', '/api/v1/templates', { name: `Minutes ${run}`, type: 'core.html', lang: 'en', title: 'Minutes', global: true, body: md('## Agenda\n\nokapi agenda') });
  // the page to include is chosen by its title, not by a number
  await newPage('core.html', `Hub ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type('Intro');
  const dlg = await insertBlock('include');
  const pick = dlg.getByRole('group', { name: /^(page|seite)$/i });
  await pick.getByRole('searchbox', { name: /^(search element|element suchen)$/i }).fill(`Okapi rules ${run}`);
  await pick.locator('option', { hasText: `Okapi rules ${run}` }).waitFor();
  await pick.locator('select.ew-picker-list').selectOption(String(rules.id));
  await dlg.getByLabel(/^(only the excerpt|nur den auszug)$/i).check();
  await a11y('macro dialog with a page picker');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="include"]');
  // a date in the text, chosen with the date field of the browser
  await page.keyboard.press('Control+End');
  await page.keyboard.type('Review on ');
  const dd = await insertBlock('date');
  await dd.locator('input[type=date]').fill('2026-12-15');
  await a11y('date dialog');
  await dd.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro_inline[data-macro="date"]');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('time.ew-date[datetime="2026-12-15"]').waitFor();
  await page.locator('.ew-include').getByText('In short: lights off.').waitFor();
  assert.equal(await page.locator('.ew-include').getByText('Long text.').count(), 0, 'only the excerpt');
  await page.locator('.ew-include').getByRole('link', { name: `Okapi rules ${run}` }).waitFor();
  // page tree, search box and the button for a new page on one page
  const src = `::pagetree{category=${cat} open=false}\n\n::searchbox{category=${sub} label="Search the chapter"}\n\n::newpage{template=${tpl.id} category=${sub}}\n\n::spacelist{layout=list}\n\n::spacedetails\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Blocks ${run}`, body: md(src) });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const tree = page.getByRole('navigation', { name: new RegExp(`^(pages of|seiten von) Wiki blocks ${run}$`, 'i') });
  await tree.waitFor();
  assert.ok(!(await tree.getByRole('link', { name: `Zebra notes ${run}` }).isVisible()), 'closed at first');
  await tree.getByLabel(/^(filter the tree|baum filtern)$/i).fill('zebra');
  await tree.getByRole('link', { name: `Zebra notes ${run}` }).waitFor();
  assert.ok(!(await tree.getByRole('link', { name: `Okapi rules ${run}` }).isVisible()), 'the filter hides what does not match');
  await tree.getByRole('status').filter({ hasText: /1/ }).waitFor();
  await tree.getByLabel(/^(filter the tree|baum filtern)$/i).fill('');
  await tree.getByRole('link', { name: `Okapi rules ${run}` }).waitFor({ state: 'hidden' });
  await page.getByRole('navigation', { name: /^(spaces|bereiche)$/i }).getByRole('link', { name: `Wiki blocks ${run}` }).waitFor();
  await page.locator('.ew-spacedetails').getByRole('link', { name: `Wiki blocks ${run}` }).waitFor();
  await a11y('page tree, search box, new page button, spaces');
  // the search box searches in its category; the search page can widen it
  await page.getByLabel('Search the chapter').fill('okapi');
  await page.getByLabel('Search the chapter').press('Enter');
  await page.waitForURL(new RegExp(`/search\\?.*category=${sub}`));
  await idle();
  const only = page.getByLabel(new RegExp(`^(only in|nur in) .*Chapter ${run}`, 'i'));
  assert.ok(await only.isChecked());
  await page.locator('.ew-result').first().waitFor();
  assert.deepEqual(await page.locator('.ew-result h3').allTextContents(), [`Okapi rules ${run}`]);
  await a11y('search in a category');
  await only.uncheck();
  await page.getByRole('button', { name: /^(search|suchen)$/i }).last().click();
  await page.waitForURL((u) => u.pathname === '/search' && !u.search.includes('category='));
  // the button opens a new page in the chapter, filled from the template
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  await page.getByRole('link', { name: new RegExp(`(new page|neue seite): Minutes ${run}`, 'i') }).click();
  await page.waitForURL(new RegExp(`/c/${sub}/new\\?type=core\\.html&template=${tpl.id}`));
  await idle();
  await page.locator('.ewe-prose').getByText('okapi agenda').waitFor();
  assert.equal(await page.getByLabel(/^(title|titel)/i).first().inputValue(), 'Minutes');
  await a11y('new page from a template');
  await page.getByRole('link', { name: /^(cancel|abbrechen)$/i }).click();
});

test('code blocks and attachments: colours, line numbers, copy, language in the editor; list of files with upload', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Code ${run}` } })).id;
  const md = (source) => ({ format: 'markdown', source });
  const code = 'package main\n\nfunc main() {\n\treturn\n}';
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Code ${run}`, body: md('```go lines\n' + code + '\n```\n\n::attachments{source=category}\n') });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const block = page.locator('.ew-codeblock').first();
  await block.locator('.hljs-keyword').first().waitFor();
  assert.equal(await block.locator('.hljs-keyword').first().evaluate((k) => getComputedStyle(k).color), 'rgb(207, 34, 46)');
  assert.equal(await block.locator('.ew-code-lang').textContent(), 'Go');
  // line numbers for the eye only
  assert.equal(await block.locator('.ew-code-gutter').getAttribute('aria-hidden'), 'true');
  assert.equal((await block.locator('.ew-code-gutter').textContent()).split('\n').length, 5);
  // copy gives the code alone
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: BASE });
  await block.getByRole('button', { name: /^(copy code|code kopieren)$/i }).click();
  await block.getByRole('button', { name: /^(copied|kopiert)$/i }).waitFor();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), code);
  // the list of files: empty, then a file uploaded right there
  const list = page.locator('.ew-attachments');
  await list.getByText(/^(no files\.|keine dateien\.)$/i).waitFor();
  await a11y('code block and attachments');
  await list.getByRole('button', { name: /^(upload a file|datei hochladen)$/i }).click();
  const dlg = page.locator('dialog[open]').last();
  await dlg.locator('input[type=file]').setInputFiles({ name: `handbook-${run}.txt`, mimeType: 'text/plain', buffer: Buffer.from('hello') });
  await dlg.locator('button[type=submit]').click();
  await list.getByRole('link', { name: `handbook-${run}` }).first().waitFor();
  await list.getByRole('link', { name: new RegExp(`^(download|herunterladen)\\s*:\\s*handbook-${run}$`, 'i') }).waitFor();
  await a11y('attachments with a file');
  // the language of a code block is chosen in the editor
  await page.goto(`${BASE}/e/${el.id}/edit`);
  await idle();
  await page.locator('.ewe-prose pre').click();
  await page.getByRole('button', { name: /^(language and line numbers|sprache und zeilennummern)$/i }).click();
  const cd = page.locator('dialog[open]').last();
  await cd.getByLabel(/^(language|sprache)$/i).selectOption('python');
  await cd.getByLabel(/^(line numbers|zeilennummern)$/i).uncheck();
  await a11y('code block settings');
  await cd.locator('button[type=submit]').click();
  await page.getByRole('button', { name: /^(publish|veröffentlichen|save|speichern)$/i }).first().click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ew-codeblock .ew-code-lang', { hasText: 'Python' }).waitFor();
  assert.equal(await page.locator('.ew-code-gutter').count(), 0);
});

test('title bar: path and title in one band with a background per category, inherited, nothing twice', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Area ${run}` } })).id;
  const sub = (await call('POST', '/api/v1/categories', { titles: { en: `Team ${run}` }, parent_id: cat })).id;
  const el = await call('POST', '/api/v1/elements', { category_id: sub, type: 'core.html', title: `Welcome ${run}`, body: { format: 'markdown', source: 'Hello' } });
  // before: path and title as two blocks
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  await page.getByRole('navigation', { name: /^(breadcrumbs|brotkrümelnavigation|pfad)/i }).first().waitFor();
  assert.equal(await page.locator('.ew-titlebar').count(), 0);
  try {
    // the title bar for the area: element and category pages, with a background
    const opts = { bg_color: '#102040', text_color: '#ffffff', padding_top: 4 };
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [
      { block: 'core.element.titlebar', position: 'element-header', sort: 5, options: opts },
      { block: 'core.category.titlebar', position: 'category-header', sort: 5, options: opts },
    ] });
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const bar = page.locator('.ew-titlebar');
    await bar.waitFor();
    // path and title inside, the single blocks left out: one heading, one path
    assert.equal(await bar.locator('h1').textContent(), `Welcome ${run}`);
    assert.equal(await page.locator('main h1:not(.sr-only)').count(), 1);
    assert.equal(await page.locator('main .ew-breadcrumbs').count(), 1);
    assert.equal(await bar.locator('.ew-breadcrumbs a', { hasText: `Area ${run}` }).count(), 1);
    const frame = page.locator('.ew-bo', { has: bar });
    assert.equal(await frame.evaluate((f) => getComputedStyle(f).backgroundColor), 'rgb(16, 32, 64)');
    await a11y('title bar of a page');
    // the subcategory page inherits it
    await page.goto(`${BASE}/c/${sub}`);
    await idle();
    await page.locator('.ew-titlebar h1', { hasText: `Team ${run}` }).waitFor();
    assert.equal(await page.locator('main h1:not(.sr-only)').count(), 1);
    await a11y('title bar of a category');
  } finally {
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
  }
});

test('layout of an element type: the pages of one type get the title bar, others keep their layout', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Typed ${run}` } })).id;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Typed page ${run}`, body: { format: 'markdown', source: 'x' } });
  try {
    await page.goto(`${BASE}/admin/placement`);
    await idle();
    const sec = page.getByRole('region', { name: /^(pages by element type|seiten nach elementtyp)$/i });
    await sec.getByLabel(/^(element type|elementtyp)$/i).selectOption('core.html');
    const add = sec.getByRole('group', { name: /^(show a building block|baustein anzeigen)$/i });
    await add.getByLabel(/^(building block|baustein)$/i).selectOption('core.element.titlebar');
    await add.getByLabel(/^(position)$/i).selectOption('element-header');
    await add.getByRole('button').click();
    await sec.getByRole('combobox', { name: /^(position of title bar \(path and title\)|position von titelleiste \(pfad und titel\))$/i }).waitFor();
    await a11y('layout of an element type');
    await sec.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toast', { hasText: /placement saved|platzierung gespeichert/i }).first().waitFor();
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    await page.locator('.ew-titlebar h1', { hasText: `Typed page ${run}` }).waitFor();
    assert.equal(await page.locator('main h1:not(.sr-only)').count(), 1);
    await a11y('a page with the layout of its type');
  } finally {
    await call('PUT', '/api/v1/placements/type?target=core.html', { placements: [] });
  }
});

test('shared block: the selection saved as a block of its own, included, changed in one place', async () => {
  await newPage('core.html', `Shared ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type(`Reception okapi ${run}`);
  await page.keyboard.press('Control+A');
  await page.getByRole('button', { name: /^(insert block|baustein einfügen)$/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(save the selection as a shared block|auswahl als gemeinsamen baustein speichern)$/i }).click();
  const dlg = page.locator('dialog[open]').last();
  await dlg.getByLabel(/^(title|titel)/i).first().fill(`Reception ${run}`);
  await a11y('save a shared block');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="include"]');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ew-content').getByText(`Reception okapi ${run}`).waitFor();
  // the block itself: never listed, changed in one place
  const found = await call('GET', `/api/v1/elements?q=${encodeURIComponent(`Reception ${run}`)}&type=core.block`);
  const blk = found.items.find((x) => x.title === `Reception ${run}`);
  assert.ok(blk, 'the block was made');
  await call('PUT', `/api/v1/elements/${blk.id}`, { lang: 'en', title: `Reception ${run}`, body: { format: 'markdown', source: `Reception lynx ${run}` } });
  await page.reload();
  await idle();
  await page.locator('.ew-content').getByText(`Reception lynx ${run}`).waitFor();
});

test('block templates: save selected blocks, insert them into another page', async () => {
  await newPage('core.html', `Source ${run}`);
  const prose = page.locator('.ewe-prose');
  await prose.click();
  await page.keyboard.type(`Opening hours ${run}`);
  await page.keyboard.press('Enter');
  await page.keyboard.type('Monday to Friday');
  await page.keyboard.press('Control+A');
  // in the menu "Insert block": save the selection as a template
  await page.getByRole('button', { name: /^(insert block|baustein einfügen)$/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(save as block template|als baustein-vorlage speichern)$/i }).click();
  const save = page.locator('dialog[open]').last();
  await save.getByLabel(/^(name)/i).first().fill(`Hours ${run}`);
  await a11y('save a block template');
  await save.locator('button[type=submit]').click();
  await page.locator('.ew-toast', { hasText: /block template saved|baustein-vorlage gespeichert/i }).first().waitFor();
  await page.getByRole('link', { name: /^(cancel|abbrechen)$/i }).click();
  // another page: the template inserted at the cursor
  await newPage('core.html', `Target ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type('Intro');
  await page.keyboard.press('Enter');
  // the templates are a group of the menu "Insert block"
  await page.getByRole('button', { name: /^(insert block|baustein einfügen)$/i }).click();
  const ins = page.locator('dialog[open]').last();
  await ins.getByRole('treeitem', { name: /^(block templates|baustein-vorlagen)$/i }).waitFor();
  await ins.getByRole('searchbox').fill(`Hours ${run}`);
  await a11y('insert a block template');
  await ins.getByRole('treeitem', { name: `Hours ${run}`, exact: true }).click();
  await page.locator('.ewe-prose').getByText(`Opening hours ${run}`).waitFor();
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ew-content').getByText('Monday to Friday').waitFor();
});

test('blocks: news as tiles with a tag filter, live search, a picture opens large, a background video with a pause button', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Tiles ${run}` } })).id;
  const md = (source) => ({ format: 'markdown', source });
  await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Gnu report ${run}`, body: md('gnu text'), tags: ['hr'] });
  await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Gnu budget ${run}`, body: md('gnu money'), tags: ['finance'] });
  // a large picture and a video of the media library
  const upload = (name, bytes, mime) => page.evaluate(async ([c, n, b, m]) => {
    const fd = new FormData();
    fd.append('meta', JSON.stringify({ category_id: c, title: n }));
    fd.append('file', new File([new Uint8Array(b)], n, { type: m }));
    const r = await __e2eFetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
    return (await r.json()).id;
  }, [cat, name, [...bytes], mime]);
  const png = await page.evaluate(async () => {
    const c = document.createElement('canvas'); c.width = 1600; c.height = 900;
    const x = c.getContext('2d'); x.fillStyle = '#3366aa'; x.fillRect(0, 0, 1600, 900);
    const b = await new Promise((r) => c.toBlob(r, 'image/png'));
    return [...new Uint8Array(await b.arrayBuffer())];
  });
  const img = await upload(`wide-${run}.png`, png, 'image/png');
  const fs = await import('node:fs');
  const vid = await upload(`loop-${run}.webm`, fs.readFileSync(new URL('./fixtures/background.webm', import.meta.url)), 'video/webm');
  assert.ok(img > 0 && vid > 0, 'uploaded');
  const opts = `padding_top:4 bg_video:/api/v1/media/${vid}/view overlay_color:#000000 overlay_opacity:70 text_color:#ffffff`;
  const src = `::news{layout=tiles columns=3 tag_buttons=true source="category:${cat}"}\n\n::searchbox{category=${cat} label="Find here"}\n\n![A wide picture](/api/v1/media/${img}/view)\n\n:::section{options="${opts}"}\nText on a video\n:::\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Tiles page ${run}`, body: md(src) });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // tiles with the filter by tag
  const tiles = page.locator('.ew-news--tiles');
  await tiles.waitFor();
  const filter = page.getByRole('group', { name: /^(filter by tag|nach schlagwort filtern)$/i });
  await filter.getByRole('button', { name: '#finance' }).click();
  assert.equal(await filter.getByRole('button', { name: '#finance' }).getAttribute('aria-pressed'), 'true');
  assert.ok(await tiles.getByRole('link', { name: `Gnu budget ${run}` }).isVisible());
  assert.ok(!(await tiles.getByRole('link', { name: `Gnu report ${run}` }).isVisible()), 'filtered out');
  await filter.getByRole('button', { name: /^(all|alle)$/i }).click();
  assert.ok(await tiles.getByRole('link', { name: `Gnu report ${run}` }).isVisible());
  // live results under the search box
  await page.getByLabel('Find here').fill('gnu');
  const live = page.locator('.ew-searchbox-live');
  await live.getByRole('link', { name: `Gnu budget ${run}` }).waitFor();
  await live.getByRole('link', { name: /^(show all results|alle treffer zeigen)$/i }).waitFor();
  await a11y('tiles, filter and live search');
  await page.getByLabel('Find here').press('Escape');
  assert.ok(await live.isHidden());
  // the picture is shown smaller than it is: a link opens it large
  const zoom = page.getByRole('link', { name: 'A wide picture' });
  await zoom.click();
  await page.locator('dialog[open] .ew-lightbox-img').waitFor();
  await page.keyboard.press('Escape');
  // the background video: under the overlay, with a button that stops it
  const frame = page.locator('.ew-bo--video');
  await frame.waitFor();
  assert.equal(await frame.locator('video').getAttribute('aria-hidden'), 'true');
  const btn = frame.getByRole('button', { name: /^(pause the video|video anhalten|play the video|video abspielen)$/i });
  await btn.waitFor();
  await btn.click();
  await a11y('a background video');
});

test('hero: heights per view; the text stays whole on a phone, also on a background video with an aspect ratio', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Heroes ${run}` } })).id;
  const vid = await page.evaluate(async ([c, n, b]) => {
    const fd = new FormData();
    fd.append('meta', JSON.stringify({ category_id: c, title: n }));
    fd.append('file', new File([new Uint8Array(b)], n, { type: 'video/webm' }));
    const r = await __e2eFetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
    return (await r.json()).id;
  }, [cat, `hero-${run}.webm`, [...(await import('node:fs')).readFileSync(new URL('./fixtures/background.webm', import.meta.url))]]);
  const long = Array.from({ length: 12 }, (_, i) => `Line ${i} of the introduction to the okapi house.`).join(' ');
  const opts = `aspect:16-9 bg_video:/api/v1/media/${vid}/view overlay_color:#000000 overlay_opacity:70 text_color:#ffffff`;
  const src = `:::hero{title="Okapi house ${run}" height=half phone_height=window background=dark}\nWelcome.\n:::\n\n:::hero{title="Video ${run}" options="${opts}"}\n${long} The last words.\n:::\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Heroes ${run}`, body: { format: 'markdown', source: src } });
  const size = page.viewportSize();
  try {
    for (const [w, hgt, name] of [[1280, 800, 'desktop'], [375, 700, 'phone']]) {
      await page.setViewportSize({ width: w, height: hgt });
      await page.goto(`${BASE}/e/${el.id}`);
      await idle();
      const m = await page.evaluate(() => {
        const [a, b] = [...document.querySelectorAll('.ewl-hero')];
        const last = [...b.querySelectorAll('p')].pop();
        const frame = b.closest('.ew-bo--video') ?? b;
        return { a: a.getBoundingClientRect().height, win: innerHeight, header: parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--ewd-header-h') || getComputedStyle(document.documentElement).getPropertyValue('--ew-header-h')) || 0,
          lastBottom: last.getBoundingClientRect().bottom, frameBottom: frame.getBoundingClientRect().bottom, video: !!b.closest('.ew-bo--video') || !!b.querySelector('video') };
      });
      if (name === 'desktop') assert.ok(m.a >= Math.min(m.win * 0.5, 34 * 16) - 2, `half the window on the desktop: ${JSON.stringify(m)}`);
      else assert.ok(m.a >= m.win - m.header - 2, `the window below the header on the phone: ${JSON.stringify(m)}`);
      // the text on the video is never cut by the aspect ratio
      assert.ok(m.lastBottom <= m.frameBottom + 1, `${name}: the last words are inside the hero: ${JSON.stringify(m)}`);
      await page.getByText('The last words.', { exact: false }).waitFor();
    }
    await a11y('heroes with heights');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('cards and tiles: each at least 12 rem wide in the room of the content, on every width', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Cards ${run}` } })).id;
  for (let i = 0; i < 4; i++) await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Tile ${i} ${run}`, body: { format: 'markdown', source: 'x' } });
  const cards = Array.from({ length: 4 }, (_, i) => `**Card ${i}**\n\nSome words about okapis.`).join('\n\n---\n\n');
  const src = `:::cards{columns=4}\n${cards}\n:::\n\n::news{layout=tiles columns=4 source="category:${cat}"}\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Cards ${run}`, body: { format: 'markdown', source: src } });
  const size = page.viewportSize();
  try {
    for (const [w, hgt] of [[1280, 800], [1024, 768], [800, 1000], [375, 760]]) {
      await page.setViewportSize({ width: w, height: hgt });
      await page.goto(`${BASE}/e/${el.id}`);
      await idle();
      await page.locator('.ewl-card').first().waitFor();
      await page.locator('.ew-news--tiles .ew-news-item').first().waitFor();
      const m = await page.evaluate(() => {
        const rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
        const widths = (sel) => [...document.querySelectorAll(sel)].map((e) => e.getBoundingClientRect().width);
        const box = (sel) => document.querySelector(sel).getBoundingClientRect().width;
        return { rem, cards: widths('.ewl-card'), tiles: widths('.ew-news--tiles .ew-news-item'), cardsBox: box('.ewl-cards'), tilesBox: box('.ew-news--tiles'),
          over: document.documentElement.scrollWidth - document.documentElement.clientWidth };
      });
      // each at least 12 rem, unless the whole area is narrower (then one column, full width)
      for (const [k, box] of [['cards', m.cardsBox], ['tiles', m.tilesBox]]) {
        for (const x of m[k]) assert.ok(x >= Math.min(12 * m.rem, box) - 1, `${w} px: ${k} ${Math.round(x)} px wide in ${Math.round(box)} px: ${JSON.stringify(m)}`);
      }
      assert.ok(m.over <= 0, `${w} px: the page scrolls sideways by ${m.over} px`);
    }
    await a11y('cards and tiles');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('own CSS of a template: switched off by default, checked while typing, used only while allowed', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Styled ${run}` } })).id;
  const tpl = await call('POST', '/api/v1/x/core.template/templates', { names: { en: `Styled ${run}` }, copy_of: 'core.template/example-wiki' });
  try {
    // switched off: the designer says so
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: `${await page.evaluate(() => document.documentElement.lang === 'de' ? 'Bearbeiten' : 'Edit')}: Styled ${run}` }).click();
    await page.locator('.ewd-structure').waitFor();
    await page.getByText(/own css is switched off|eigenes css ist abgeschaltet/i).first().waitFor();
    // switched on in the settings
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, custom_css: true } });
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): Styled ${run}$`, 'i') }).click();
    const css = page.getByLabel(/^css$/i);
    await css.waitFor();
    await css.fill('h1 { letter-spacing: 3px; }\n@import "https://evil.example/x.css";');
    await page.getByText(/^(line 2: @import is not allowed|zeile 2: @import ist nicht erlaubt)$/i).waitFor();
    await a11y('own CSS with a problem');
    await css.fill('h1 { letter-spacing: 3px; }');
    await css.dispatchEvent('change');
    await page.getByText(/^(no problems|keine probleme)/i).first().waitFor();
    const saved = page.waitForResponse((r) => r.url().endsWith(`/api/v1/x/core.template/templates/${tpl.key}`) && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    // used for a category: the wiki applies it
    await call('PUT', `/api/v1/categories/${cat}`, { template: tpl.id });
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    const spacing = () => page.locator('main h1').first().evaluate((x) => getComputedStyle(x).letterSpacing);
    assert.equal(await spacing(), '3px');
    // switched off: no longer used
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, custom_css: false } });
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    assert.notEqual(await spacing(), '3px');
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('PUT', `/api/v1/categories/${cat}`, { template: '' }).catch(() => {});
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}`).catch(() => {});
  }
});

test('options of a placed block: background with checked contrast, hidden on the phone, spacing; the wiki shows it', async () => {
  const before = await call('GET', '/api/v1/placements/site');
  try {
    await page.goto(`${BASE}/admin/placement`);
    await idle();
    await page.getByRole('button', { name: /^(options|optionen): (my favorites|meine favoriten)$/i }).first().click();
    const dlg = page.locator('dialog[open]');
    await dlg.getByText(/^(no problems|keine probleme)\.$/i).waitFor();
    // a background of its own needs a text color of its own, with enough contrast
    await dlg.locator('summary', { hasText: /^(background|hintergrund)$/i }).click();
    await dlg.getByLabel(/^(background colour: own colour|hintergrundfarbe: eigene farbe)$/i).check();
    await dlg.getByLabel(/^(background colour|hintergrundfarbe)$/i).fill('#102040');
    await dlg.locator('.ew-bo-problems').getByText(/(needs its own text colour|braucht eine eigene textfarbe)/i).waitFor();
    await dlg.locator('summary', { hasText: /^(text|schrift)$/i }).click();
    await dlg.getByLabel(/^(text colour: own colour|textfarbe: eigene farbe)$/i).check();
    await dlg.getByLabel(/^(text colour|textfarbe)$/i).fill('#3a4a6a');
    await dlg.locator('.ew-bo-problems').getByText(/(contrast of|einen kontrast von)/i).waitFor();
    await a11y('options of a block with a problem');
    await dlg.getByLabel(/^(text colour|textfarbe)$/i).fill('#ffffff');
    await dlg.getByText(/^(no problems|keine probleme)\.$/i).waitFor();
    // the sample shows the look at once
    assert.equal(await dlg.locator('.ew-bo[data-bo="sample"]').evaluate((el) => getComputedStyle(el).backgroundColor), 'rgb(16, 32, 64)');
    // hidden on the phone; more spacing at the top on the desktop
    await dlg.getByRole('group', { name: /^(hide on|ausblenden auf)$/i }).getByLabel(/^(phone|handy)$/i).check();
    await dlg.locator('summary', { hasText: /^(size and spacing|größe und abstand)$/i }).click();
    await dlg.getByLabel(/^(inner spacing top – desktop|innenabstand oben – desktop)$/i).selectOption({ label: '2 rem' });
    await a11y('options of a block');
    await dlg.getByRole('button', { name: /^(apply|übernehmen)$/i }).click();
    await page.getByRole('button', { name: /^(options \(set\)|optionen \(gesetzt\)): (my favorites|meine favoriten)$/i }).first().waitFor();
    const saved = page.waitForResponse((r) => r.url().endsWith('/api/v1/placements/site') && r.request().method() === 'PUT');
    await page.locator('.ew-placement').getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    // the wiki: the block in its frame with the options
    await page.goto(`${BASE}/`);
    await idle();
    const frame = page.locator('.ew-bo', { has: page.locator('.ewfav') }).first();
    await frame.waitFor();
    const look = await frame.evaluate((el) => { const c = getComputedStyle(el); return [c.backgroundColor, c.color, c.paddingTop]; });
    assert.deepEqual(look, ['rgb(16, 32, 64)', 'rgb(255, 255, 255)', '32px']);
    await a11y('a block with options in the wiki');
    const size = page.viewportSize();
    try {
      await page.setViewportSize({ width: 375, height: 800 });
      assert.equal(await frame.evaluate((el) => getComputedStyle(el).display), 'none');
    } finally {
      if (size) await page.setViewportSize(size);
    }
  } finally {
    await call('PUT', '/api/v1/placements/site', { placements: before.placements });
  }
});

test('template designer: visual and with the keyboard, room and contrast help, undo and redo, views by hand, live preview', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  let key = '';
  try {
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    const created = page.waitForResponse((r) => r.url().endsWith('/api/v1/x/core.template/templates') && r.request().method() === 'POST');
    await page.getByRole('button', { name: /^(duplicate|kopieren|duplizieren): (website \(data\)|website \(daten\))$/i }).click();
    key = (await (await created).json()).key;
    await page.locator('.ewd-structure').waitFor();
    await page.getByText(/^(no problems|keine probleme)/i).waitFor();
    // the preview is the wiki itself with the draft
    await page.waitForFunction(() => document.querySelector('iframe.ewd-frame')?.contentDocument?.body?.classList.contains('ewd-preview'));
    await a11y('the visual designer');
    // choose a block with the keyboard and give it more positions side by side than it has room for
    await page.getByRole('button', { name: /^tools \((area|bereich)\)$/i }).focus();
    await page.keyboard.press('Enter');
    const positions = page.getByLabel(/^(positions|positionen)$/i);
    await positions.fill('header-tools, tool-b, tool-c');
    await page.keyboard.press('Tab');
    await page.locator('.ewd-props').getByText(/(3 positions side by side need at least 13|3 positionen nebeneinander brauchen mindestens 13)/i).waitFor();
    const fit = page.getByLabel(/^(as wide as its content|so breit wie sein inhalt)$/i);
    if (await fit.isChecked()) { await fit.uncheck(); }
    const width = page.getByLabel(/^(width \(rem\)|breite \(rem\))$/i);
    await width.fill('10');
    await page.keyboard.press('Tab');
    await page.locator('.ewd-problems').getByText(/its 3 positions side by side need 13.0 rem, the area is 10 rem wide/).waitFor();
    await a11y('the visual designer with a problem');
    // undo step by step back to the shipped state, redo once
    const undo = page.getByRole('button', { name: /^(undo|rückgängig)/i });
    while (await undo.isEnabled()) await undo.click();
    await page.getByText(/^(no problems|keine probleme)/i).waitFor();
    await page.getByRole('button', { name: /^(redo|wiederholen)/i }).click();
    await page.getByRole('button', { name: /^tools \((area|bereich)\)$/i }).click();
    assert.match(await page.getByLabel(/^(positions|positionen)$/i).inputValue(), /tool-b/);
    while (await undo.isEnabled()) await undo.click();
    // contrast help: a text color that is too light, then the suggestion
    await page.getByLabel(/^text$/i).fill('#bbbbbb');
    const take = page.getByRole('button', { name: /^(text – background: take |text – hintergrund: )#[0-9a-f]{6}( übernehmen)?$/i });
    await take.waitFor();
    await take.click();
    // the text is also shown on the subtle background: take the suggestion there too
    const more = page.locator('.ewd-contrast .ewd-bad button');
    while (await more.count()) await more.first().click();
    assert.equal(await page.locator('.ewd-contrast .ewd-bad').count(), 0);
    await page.getByText(/^(no problems|keine probleme)/i).waitFor();
    // a color shows in the preview at once
    await page.getByLabel(/^(main colour|hauptfarbe)$/i).fill('#7a1f5c');
    await page.waitForFunction(() => getComputedStyle(document.querySelector('iframe.ewd-frame').contentDocument.body).getPropertyValue('--ew-primary').trim() === '#7a1f5c');
    // the phone: adjusted by hand, then automatic again
    await page.getByRole('button', { name: /^(row 4 \(body\)|zeile 4 \(body\))$/i }).click();
    await page.getByRole('button', { name: /^(adjust the phone by hand|handy von hand anpassen)$/i }).click();
    await page.getByText(/^(adjusted by hand\.|von hand angepasst\.)$/i).waitFor();
    await page.getByRole('button', { name: /^(automatic again|wieder automatisch)$/i }).click();
    await page.getByRole('button', { name: /^(adjust the phone by hand|handy von hand anpassen)$/i }).waitFor();
    // the preview as a phone
    await page.getByRole('button', { name: /^(phone|handy) \(375 px\)$/i }).click();
    await page.waitForFunction(() => {
      const f = document.querySelector('iframe.ewd-frame');
      return f?.getAttribute('width') === '375' && f.contentDocument?.body?.classList.contains('ewd-preview');
    });
    await page.locator('.ewd-hints h3', { hasText: /(phone|handy)/i }).waitFor();
    // the height of the sticky header, measured in the preview
    await page.locator('.ewd-hints li', { hasText: /(sticky header|klebende kopf) (is|ist) \d+ px/i }).waitFor();
    // dark colors: a first dark palette that passes every pair, its own
    // contrast list, and the preview in the dark scheme
    await page.getByLabel(/^(offer dark colours|dunkle farben anbieten)$/i).check();
    await page.getByRole('heading', { name: /^(contrast of the dark colours|kontrast der dunklen farben)$/i }).waitFor();
    assert.equal(await page.locator('.ewd-contrast .ewd-bad').count(), 0, 'the first dark palette passes every pair');
    await page.getByRole('group', { name: /^(colour scheme of the preview|farbschema der vorschau)$/i }).getByRole('button', { name: /^(dark|dunkel)$/i }).click();
    await page.waitForFunction(() => {
      const b = document.querySelector('iframe.ewd-frame')?.contentDocument?.body;
      return !!b && b.classList.contains('ewd-preview') && b.classList.contains('ewc-dark') && getComputedStyle(b).getPropertyValue('--ew-bg').trim() === '#1d2125';
    });
    await a11y('the designer with dark colours');
    await page.getByRole('group', { name: /^(colour scheme of the preview|farbschema der vorschau)$/i }).getByRole('button', { name: /^(light|hell)$/i }).click();
    await page.waitForFunction(() => {
      const b = document.querySelector('iframe.ewd-frame')?.contentDocument?.body;
      return !!b && b.classList.contains('ewd-preview') && !b.classList.contains('ewc-dark');
    });
    // the text shows the same design
    await page.getByRole('button', { name: /^(as text \(json\)|als text \(json\))$/i }).click();
    const design = JSON.parse(await page.getByLabel(/^(design \(text\)|design \(text\))$/i).inputValue());
    assert.equal(design.tokens.primary, '#7a1f5c');
    assert.equal(design.tokens.dark?.bg, '#1d2125');
    assert.equal(design.options.color_scheme, 'system');
    assert.equal(design.rows[1].mobile.drawer[0], 'menu');
    await page.getByRole('button', { name: /^(visual|visuell)$/i }).click();
    const saved = page.waitForResponse((r) => r.url().endsWith(`/api/v1/x/core.template/templates/${key}`) && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    await a11y('the visual designer, phone preview');
    // the website template of data (dark header) for the whole wiki: every
    // part has enough contrast there too, on the desktop and on a phone
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, template: `core.template/${key}` } });
    await page.goto(`${BASE}/`);
    await idle();
    assert.ok(await page.evaluate((k) => document.body.classList.contains(`ewd-${k}`), String(key)));
    await a11y('the wiki with the website template of data (desktop)');
    const size = page.viewportSize();
    try {
      await page.setViewportSize({ width: 375, height: 800 });
      await page.goto(`${BASE}/`);
      await idle();
      await a11y('the wiki with the website template of data (phone)');
    } finally {
      if (size) await page.setViewportSize(size);
    }
    // the same template in the dark scheme (its dark colors)
    await call('PUT', '/api/v1/scheme', { scheme: 'dark' });
    await page.goto(`${BASE}/`);
    await idle();
    assert.equal(await page.evaluate(() => getComputedStyle(document.body).getPropertyValue('--ew-bg').trim()), '#1d2125');
    await a11y('the wiki with the website template of data, dark');
  } finally {
    await call('PUT', '/api/v1/scheme', { scheme: '' }).catch(() => {});
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    if (key) {
      await call('DELETE', `/api/v1/x/core.template/templates/${key}`).catch(() => {});
      await call('DELETE', `/api/v1/x/core.template/templates/${key}?purge=1`).catch(() => {});
    }
  }
});

test('blocks B, part 1: progress bar, counter, timeline, dialog with a button, tooltip; with the keyboard and reduced motion', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Blocks B ${run}` } })).id;
  const src = [
    '::progress{label="Renovation" value=40}',
    '',
    '::counter{value=1234 label="Members" suffix="+"}',
    '',
    ':::timeline{label="History" direction=horizontal}',
    '### 2024',
    'Founded',
    '***',
    '### 2025',
    'Grown',
    ':::',
    '',
    ':::dialog{button="Opening hours" title="When we are open"}',
    'Monday to Friday, 8 to 17.',
    ':::',
    '',
    'Our :tooltip[API]{tip="Programming interface"} is documented.',
    '',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Blocks B ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // progress: a named bar with its value as text
  const bar = page.getByRole('progressbar', { name: 'Renovation' });
  assert.equal(await bar.getAttribute('aria-valuenow'), '40');
  assert.ok(await page.getByText('40 %').isVisible());
  // counter: counts up once in view and ends with the whole number
  const counter = page.locator('.ewl-counter');
  await counter.scrollIntoViewIfNeeded();
  await page.waitForFunction(() => /1[.,]234 \+$/.test(document.querySelector('.ewl-counter-shown')?.textContent ?? ''));
  assert.match(await counter.locator('.sr-only').textContent(), /1[.,]234 \+/);
  // timeline: entries in order; horizontal in a region the keyboard reaches
  const region = page.getByRole('region', { name: 'History' });
  assert.equal(await region.getAttribute('tabindex'), '0');
  assert.equal(await region.getByRole('listitem').count(), 2);
  // dialog: opens with the button, Escape closes, the focus returns
  const open = page.getByRole('button', { name: 'Opening hours' });
  await open.focus();
  await page.keyboard.press('Enter');
  const dlg = page.getByRole('dialog', { name: 'When we are open' });
  await dlg.getByText('Monday to Friday').waitFor();
  await a11y('a dialog opened by a button');
  await page.keyboard.press('Escape');
  await dlg.waitFor({ state: 'hidden' });
  assert.ok(await open.evaluate((b) => b === document.activeElement), 'the focus returns to the button');
  await open.click();
  await dlg.getByRole('button', { name: /^(close|schließen)$/i }).click();
  await dlg.waitFor({ state: 'hidden' });
  await page.waitForFunction(() => document.activeElement?.textContent === 'Opening hours');
  // tooltip: on focus and pointing, Escape hides it; read as the description of the word
  const word = page.getByRole('button', { name: 'API' });
  const tip = page.getByRole('tooltip');
  await word.focus();
  await tip.waitFor();
  assert.equal(await page.locator('.ewl-tip-bubble').textContent(), 'Programming interface');
  // the pointer leaving the word does not hide what the focus shows (found in CI)
  await word.hover();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(400);
  const state = () => page.evaluate(() => { const b = document.querySelector('.ewl-tip-bubble'); return JSON.stringify({ focus: document.activeElement?.className, hasFocus: document.hasFocus(), hidden: b?.hidden, dismissed: b?.dataset.dismissed, visible: b && getComputedStyle(b).display, fv: document.querySelector('.ewl-tip-button')?.matches(':focus-visible') }); });
  assert.ok(await tip.isVisible(), `stays while the word has the focus: ${await state()}`);
  assert.equal(await word.evaluate((b) => document.getElementById(b.getAttribute('aria-describedby'))?.textContent), 'Programming interface');
  await page.keyboard.press('Escape');
  await tip.waitFor({ state: 'hidden' });
  await page.locator('h1').first().focus();
  await word.hover();
  await tip.waitFor();
  await tip.hover(); // the pointer can move onto it (WCAG 1.4.13)
  await page.waitForTimeout(300);
  assert.ok(await tip.isVisible(), 'stays while the pointer is on it');
  await a11y('blocks B, part 1');
  // reduced motion: the counter shows the number at once, nothing moves
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.reload();
  await idle();
  await page.locator('.ewl-counter').scrollIntoViewIfNeeded();
  assert.equal(await page.locator('.ewl-counter-shown').count(), 0, 'no moving copy');
  assert.match(await page.locator('.ewl-counter-value').textContent(), /1[.,]234 \+/);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  // the phone: the horizontal timeline goes top to bottom, nothing scrolls sideways
  const size = page.viewportSize();
  await page.setViewportSize({ width: 375, height: 800 });
  try {
    await page.reload();
    await idle();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'no sideways scrolling');
    assert.equal(await page.locator('.ewl-timeline--horizontal').evaluate((x) => getComputedStyle(x).display), 'block');
    await a11y('blocks B, part 1 (phone)');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('blocks B, part 2: quote, logo bar with a picture of the media library, tag cloud, changes of a page, contributors', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `People ${run}` } })).id;
  const md = (source) => ({ format: 'markdown', source });
  const a = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Tagged ${run}`, body: md('x'), tags: [`walrus${run}`, 'common'] });
  await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Tagged two ${run}`, body: md('y'), tags: [`walrus${run}`] });
  await call('PUT', `/api/v1/elements/${a.id}`, { lang: 'en', title: `Tagged ${run}`, base_rev: 1, comment: 'second try', body: md('x2') });
  const logo = await page.evaluate(async ([c, n]) => {
    const cv = document.createElement('canvas'); cv.width = 200; cv.height = 60;
    const x = cv.getContext('2d'); x.fillStyle = '#204080'; x.fillRect(0, 0, 200, 60);
    const blob = await new Promise((r) => cv.toBlob(r, 'image/png'));
    const fd = new FormData();
    fd.append('meta', JSON.stringify({ category_id: c, title: n }));
    fd.append('file', new File([blob], n, { type: 'image/png' }));
    const r = await __e2eFetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
    return (await r.json()).id;
  }, [cat, `logo-${run}.png`]);
  const src = [
    ':::testimonial{name="Mira Example" role="Team lead"}',
    'New colleagues find their way in a few days.',
    ':::',
    '',
    ':::logos{label="Our partners" grey=true}',
    `[![Acme](/api/v1/media/${logo}/view)](https://acme.example)`,
    ':::',
    '',
    `::tagcloud{category=${cat}}`,
    '',
    `::pagechanges{page=${a.id}}`,
    '',
    `::contributors{page=${a.id}}`,
    '',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `People page ${run}`, body: md(src) });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // the quote with the name of the person
  await page.locator('figure.ewl-quote figcaption', { hasText: 'Mira Example' }).waitFor();
  // logos: a named list; the logo is a link named by its text alternative
  const partners = page.getByRole('list', { name: 'Our partners' });
  await partners.getByRole('link', { name: 'Acme' }).waitFor();
  // the tag cloud: the tags lead to their pages, the count is part of the name
  const cloud = page.getByRole('navigation', { name: 'Tags' });
  const walrus = cloud.getByRole('link', { name: new RegExp(`^#walrus${run} \\(2 pages\\)$`) });
  assert.match(await walrus.getAttribute('href'), new RegExp(`tags=walrus${run}`));
  // changes and contributors
  await page.locator('.ew-pagechanges-list li', { hasText: 'second try' }).waitFor();
  assert.equal(await page.locator('.ew-pagechanges a', { hasText: /whole history|ganze verlauf/i }).getAttribute('href'), `/e/${a.id}/history`);
  assert.ok(await page.locator('.ew-contributors li').count() >= 1);
  await a11y('blocks B, part 2');
  await walrus.click();
  await page.waitForURL(/\/search\?/);
  await page.getByRole('link', { name: `Tagged two ${run}` }).waitFor();
  // the phone: nothing scrolls sideways
  await page.goto(`${BASE}/e/${el.id}`);
  const size = page.viewportSize();
  await page.setViewportSize({ width: 375, height: 800 });
  try {
    await idle();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'no sideways scrolling');
    await a11y('blocks B, part 2 (phone)');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('a jump inside the page (#name) keeps the page as it is: no new drawing, tabs and closed sections open', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Jumps ${run}` } })).id;
  const src = [
    ':::tabs',
    '### First',
    '',
    'First tab',
    '',
    '***',
    '',
    '### Second',
    '',
    'Second tab',
    ':::',
    '',
    '[To the second tab](#ewl-tab-1-2) · [To the hidden part](#deep-part) · [To the end](#the-end)',
    '',
    ':::expand{title="More details"}',
    '::anchor{name=deep-part}',
    '',
    'The hidden part',
    ':::',
    '',
    ...Array.from({ length: 60 }, (_, i) => `Line ${i + 1}\n`),
    '::anchor{name=the-end}',
    '',
    'The end of the page',
    '',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Jumps ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  await page.evaluate(() => { window.__ewMark = document.querySelector('.ewl-tabs'); });
  const same = () => page.evaluate(() => !!window.__ewMark?.isConnected);
  // a link to a tab selects it
  await page.getByRole('link', { name: 'To the second tab' }).click();
  await page.getByRole('tab', { name: 'Second', selected: true }).waitFor();
  assert.ok(await page.getByText('Second tab', { exact: true }).isVisible());
  // a link into a closed section opens it
  await page.getByRole('link', { name: 'To the hidden part' }).click();
  await page.getByText('The hidden part', { exact: true }).waitFor();
  assert.equal(await page.locator('details', { hasText: 'The hidden part' }).evaluate((d) => d.open), true);
  // a link down the page scrolls there; the focus stays where the link sent it, not on the title
  const link = page.getByRole('link', { name: 'To the end' });
  await link.focus();
  await page.keyboard.press('Enter');
  await page.waitForFunction(() => location.hash === '#the-end');
  await page.waitForFunction(() => document.getElementById('the-end').getBoundingClientRect().top < window.innerHeight);
  assert.notEqual(await page.evaluate(() => document.activeElement?.tagName), 'H1', 'the focus does not jump to the title');
  // back: the same page, still not drawn anew
  await page.goBack();
  await page.waitForFunction(() => location.hash === '#deep-part');
  assert.ok(await same(), 'the page was not drawn anew');
  // another page is still a new page
  await page.getByRole('link', { name: `Jumps ${run}` }).first().click();
  await page.waitForFunction(() => !window.__ewMark?.isConnected);
});

test('blocks C, part 1: price table, picture with hotspots, before and after, countdown', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Offers ${run}` } })).id;
  const pics = [];
  for (const [i, color] of [['a', '#204080'], ['b', '#c06020']].entries()) {
    pics.push(await page.evaluate(async ([c, n, col]) => {
      const cv = document.createElement('canvas'); cv.width = 400; cv.height = 200;
      const x = cv.getContext('2d'); x.fillStyle = col; x.fillRect(0, 0, 400, 200);
      const blob = await new Promise((r) => cv.toBlob(r, 'image/png'));
      const fd = new FormData();
      fd.append('meta', JSON.stringify({ category_id: c, title: n }));
      fd.append('file', new File([blob], n, { type: 'image/png' }));
      const r = await __e2eFetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
      return (await r.json()).id;
    }, [cat, `offer-${i[1]}-${run}.png`, color]));
  }
  const src = [
    ':::pricing{featured=2 badge="Best choice"}',
    '### Starter',
    '',
    '**free**',
    '',
    '- One area',
    '',
    '***',
    '',
    '### Team',
    '',
    '**5 € per person**',
    '',
    '- All areas',
    ':::',
    '',
    `:::hotspots{image="/api/v1/media/${pics[0]}/view" alt="Plan of the office"}`,
    '- 20/30: The entrance',
    '- 80/70: The kitchen',
    ':::',
    '',
    `::compare{before="/api/v1/media/${pics[0]}/view" before_alt="Blue wall" after="/api/v1/media/${pics[1]}/view" after_alt="Orange wall" start=40}`,
    '',
    '::countdown{date=2099-12-31 time=23:00 label="Until the turn of the century" reached="Happy new century"}',
    '',
    '::countdown{date=2000-01-01 label="Long ago" reached="It is over"}',
    '',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Offers ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // price table: the highlighted plan says so in words
  const team = page.locator('.ewl-plan--featured');
  assert.equal(await team.locator('.ewl-plan-badge').textContent(), 'Best choice');
  await team.getByRole('heading', { name: 'Team' }).waitFor();
  // hotspots: a point is a link to its text below the picture
  const point = page.getByRole('link', { name: /^(point|punkt) 2$/i });
  await point.focus();
  await page.keyboard.press('Enter');
  await page.waitForFunction(() => location.hash.startsWith('#ewl-hs-'));
  assert.equal(await page.evaluate(() => document.querySelector(':target')?.textContent), 'The kitchen');
  // before and after: a slider with the keyboard
  const slider = page.getByRole('slider', { name: /^(border between|grenze zwischen) before and after$/i });
  assert.equal(await slider.inputValue(), '40');
  assert.match(await slider.getAttribute('aria-valuetext'), /^60 % After$/);
  await slider.focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  assert.equal(await slider.inputValue(), '42');
  assert.equal(await page.locator('.ewl-compare').evaluate((x) => x.style.getPropertyValue('--ewl-compare')), '42%');
  assert.equal(await page.getByRole('img', { name: 'Orange wall' }).count(), 1);
  // countdown: the time left, the date stays; reached: the text for afterwards
  const cd = page.locator('.ewl-countdown').first();
  await cd.locator('.ewl-countdown-unit', { hasText: /^(days|tage)$/i }).waitFor();
  assert.match(await cd.locator('time').getAttribute('datetime'), /^2099-12-31T23:00:00/);
  await cd.getByRole('button', { name: /^(pause|anhalten)$/i }).click();
  assert.equal(await cd.getByRole('button', { name: /^(play|abspielen)$/i }).getAttribute('aria-pressed'), 'true');
  const past = page.locator('.ewl-countdown').nth(1);
  await past.getByText('It is over').waitFor();
  assert.equal(await past.getByRole('button').count(), 0, 'no pause button once reached');
  await a11y('blocks C, part 1');
  // the phone: nothing scrolls sideways
  const size = page.viewportSize();
  await page.setViewportSize({ width: 375, height: 800 });
  try {
    await page.reload();
    await idle();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'no sideways scrolling');
    await a11y('blocks C, part 1 (phone)');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('blocks B, part 3: person card, changing words, ticker, slider; pause buttons and reduced motion', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Motion ${run}` } })).id;
  const src = [
    ':::personcard{name="Mira Example" role="Support" email="mira@example.com" phone="+43 1 234"}',
    'Answers questions about the wiki.',
    ':::',
    '',
    '::rotating{before="We build" words="wikis|sites|tools" after="together"}',
    '',
    ':::ticker{label="News"}',
    '- First news',
    '- Second news',
    ':::',
    '',
    ':::slider{label="Steps" auto=true}',
    'Step one',
    '***',
    'Step two',
    '***',
    'Step three',
    ':::',
    '',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Motion ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // person card
  await page.getByRole('link', { name: 'mira@example.com' }).waitFor();
  assert.equal(await page.getByRole('link', { name: '+43 1 234' }).getAttribute('href'), 'tel:+431234');
  // changing words: the heading keeps all words for screen readers; one shows; pause stops it
  const rot = page.locator('.ewl-rot');
  assert.equal(await page.getByRole('heading', { name: 'We build wikis, sites, tools together' }).count(), 1);
  const shown = () => rot.locator('.ewl-rot-word.is-active').textContent();
  const first = await shown();
  await page.mouse.move(0, 0);
  await page.waitForFunction((f) => document.querySelector('.ewl-rot-word.is-active')?.textContent !== f, first, { timeout: 6000 });
  const pause = rot.getByRole('button', { name: /^(pause|anhalten)$/i });
  await pause.click();
  assert.equal(await rot.getByRole('button', { name: /^(play|abspielen)$/i }).getAttribute('aria-pressed'), 'true');
  await page.mouse.move(0, 0);
  const now = await shown();
  await page.waitForTimeout(3000);
  assert.equal(await shown(), now, 'paused');
  // ticker: the list is there once for screen readers; it runs and can be paused
  const ticker = page.getByRole('region', { name: 'News' });
  assert.equal(await ticker.getByRole('listitem').count(), 2, 'the copy is hidden from screen readers');
  await ticker.getByRole('button', { name: /^(pause|anhalten)$/i }).click();
  assert.equal(await ticker.evaluate((x) => getComputedStyle(x.querySelector('.ewl-ticker-track')).animationPlayState), 'paused');
  // slider: one part at a time, next and a button per part, it changes by itself until paused
  const slider = page.getByRole('region', { name: 'Steps' });
  await slider.getByRole('button', { name: /^(pause|anhalten)$/i }).click();
  assert.ok(await slider.getByText('Step one').isVisible());
  assert.ok(!(await slider.getByText('Step two').isVisible()));
  await slider.getByRole('button', { name: /^(next|weiter)/i }).click();
  assert.ok(await slider.getByText('Step two').isVisible());
  assert.equal(await slider.getByRole('button', { name: /^(part 2 of 3|teil 2 von 3)$/i }).getAttribute('aria-current'), 'true');
  await slider.getByRole('button', { name: /^(part 3 of 3|teil 3 von 3)$/i }).click();
  assert.ok(await slider.getByText('Step three').isVisible());
  assert.equal(await slider.locator('.ewl-slides').getAttribute('aria-live'), 'polite', 'announced while paused');
  await a11y('blocks B, part 3');
  // reduced motion: nothing moves, nothing changes by itself, everything readable
  await page.emulateMedia({ reducedMotion: 'reduce' });
  try {
    await page.reload();
    await idle();
    assert.equal(await page.locator('.ewl-rot--ready').count(), 0, 'all words stand');
    assert.ok(await page.locator('.ewl-rot-word', { hasText: 'wikis' }).isVisible() && await page.locator('.ewl-rot-word', { hasText: 'tools' }).isVisible());
    assert.equal(await page.locator('.ewl-ticker--run').count(), 0, 'the ticker stands');
    assert.equal(await page.getByRole('region', { name: 'Steps' }).getByRole('button', { name: /^(pause|anhalten)$/i }).count(), 0, 'no change by itself');
    await a11y('blocks B, part 3, reduced motion');
  } finally {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
  }
  // the phone: nothing scrolls sideways
  const size = page.viewportSize();
  await page.setViewportSize({ width: 375, height: 800 });
  try {
    await page.reload();
    await idle();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'no sideways scrolling');
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('blocks B, part 4: a chart from a table, drawn by the server, with the table for everyone', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Charts ${run}` } })).id;
  const src = [
    ':::chart{title="Visits" type="line" unit="h"}',
    '| Month | Vienna | Graz |',
    '| --- | --- | --- |',
    '| Jan | 10 | 4,5 |',
    '| Feb | 12 | 6 |',
    '| Mar | 9 | 8 |',
    ':::',
    '',
    ':::chart{title="Shares" type="pie" table="open"}',
    '| Kind | Count |',
    '| --- | --- |',
    '| Pages | 3 |',
    '| Files | 1 |',
    ':::',
  ].join('\n');
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Chart ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const charts = page.locator('figure.ewl-chart');
  assert.equal(await charts.count(), 2);
  // one picture with a name for screen readers; the numbers stay as a table
  const svg = charts.first().locator('svg[role="img"]');
  assert.match(await svg.getAttribute('aria-label'), /^Visits\. (Line chart|Liniendiagramm)/);
  assert.equal(await charts.first().locator('polyline').count(), 2);
  assert.ok(await charts.first().locator('details table').isHidden(), 'the table is folded');
  await charts.first().locator('summary').click();
  assert.match(await charts.first().locator('details table').innerText(), /Graz[\s\S]*4[.,]5 h/);
  assert.ok(await charts.nth(1).locator('details table').isVisible(), 'open table');
  assert.equal(await charts.nth(1).locator('.ewl-chart-slice').count(), 2);
  assert.match(await charts.nth(1).locator('.ewl-chart-legend').innerText(), /Pages · 75/);
  assert.equal(await page.locator('.ew-macro-error').count(), 0);
  await a11y('charts');
});

test('blocks B, part 5: an external video loads only after consent, from an allowed service', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config.media ?? {};
  // the allowed service, answered here (the test runs without the internet)
  await page.route('https://video.example.com/**', (r) => r.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>player</title><p>player</p>' }));
  try {
    // an administrator allows the service in the settings (Media)
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    const allow = page.getByLabel(/^(services allowed for external videos and embeds|erlaubte dienste für externe videos und einbettungen)/i);
    await allow.fill('https://video.example.com');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).first().click();
    await page.locator('.ew-toasts', { hasText: /saved|gespeichert/i }).waitFor();
    assert.deepEqual((await call('GET', '/api/v1/admin/settings')).config.media.embed_allow, ['https://video.example.com']);
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Embeds ${run}` } })).id;
    const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Embed ${run}`,
      body: { format: 'markdown', source: '::embed{address="https://video.example.com/v/1" title="Intro video" caption="Part 1"}' } });
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const fig = page.locator('figure.ewl-embed');
    // first only the box: it names the service, nothing is loaded
    assert.equal(await fig.locator('iframe').count(), 0);
    assert.match(await fig.innerText(), /video\.example\.com/);
    await a11y('external video before consent');
    await fig.getByRole('checkbox', { name: /video\.example\.com/ }).check();
    await fig.getByRole('button', { name: /^(load content|inhalt laden)$/i }).click();
    const frame = fig.locator('iframe');
    await frame.waitFor();
    assert.equal(await frame.getAttribute('title'), 'Intro video');
    assert.equal(await frame.getAttribute('src'), 'https://video.example.com/v/1');
    assert.match(await frame.getAttribute('sandbox'), /allow-scripts/);
    assert.ok(!/allow-top-navigation/.test(await frame.getAttribute('sandbox')), 'no navigation of the page');
    await page.frameLocator('figure.ewl-embed iframe').getByText('player').waitFor();
    // remembered in this browser: the next visit loads it at once, and the consent can be taken back
    await page.reload();
    await idle();
    await frame.waitFor();
    await page.getByRole('button', { name: /video\.example\.com/ }).click();
    await page.getByRole('status').filter({ hasText: /video\.example\.com/ }).waitFor();
    await page.reload();
    await idle();
    await fig.getByRole('button', { name: /^(load content|inhalt laden)$/i }).waitFor();
    assert.equal(await fig.locator('iframe').count(), 0);
  } finally {
    await page.unroute('https://video.example.com/**');
    await call('PUT', '/api/v1/admin/settings', { media: { ...before, embed_allow: before.embed_allow ?? [] } });
  }
});

test('map: a place on OpenStreetMap, loaded only after consent, the place always in words', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config.media ?? {};
  // the map service, answered here (the test runs without the internet)
  await page.route('https://www.openstreetmap.org/**', (r) => r.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>map</title><p>the map</p>' }));
  try {
    await call('PUT', '/api/v1/admin/settings', { media: { ...before, embed_allow: ['https://www.openstreetmap.org'] } });
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Maps ${run}` } })).id;
    const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Map ${run}`,
      body: { format: 'markdown', source: '::map{place="Main entrance, Example Street 1" lat=46.6167 lon=14.2653 zoom=17}' } });
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const fig = page.locator('figure.ewl-map');
    // the place stands in words; nothing of the service is loaded yet
    await fig.locator('figcaption').getByText('Main entrance, Example Street 1').waitFor();
    assert.equal(await fig.locator('iframe').count(), 0);
    assert.match(await fig.innerText(), /www\.openstreetmap\.org/);
    assert.match(await fig.getByRole('link').getAttribute('href'), /^https:\/\/www\.openstreetmap\.org\/\?mlat=46\.616700&mlon=14\.265300#map=17\//);
    await a11y('map before consent');
    await fig.getByRole('button', { name: /^(load content|inhalt laden)$/i }).click();
    const frame = fig.locator('iframe');
    await frame.waitFor();
    assert.equal(await frame.getAttribute('title'), 'Map: Main entrance, Example Street 1');
    assert.match(await frame.getAttribute('src'), /^https:\/\/www\.openstreetmap\.org\/export\/embed\.html\?bbox=[0-9.,-]+&layer=mapnik&marker=46\.616700%2C14\.265300$/);
    await page.frameLocator('figure.ewl-map iframe').getByText('the map').waitFor();
    await fig.locator('figcaption').getByText('Main entrance, Example Street 1').waitFor();
    await a11y('map loaded');
  } finally {
    await page.unroute('https://www.openstreetmap.org/**');
    await call('PUT', '/api/v1/admin/settings', { media: { ...before, embed_allow: before.embed_allow ?? [] } });
  }
});

test('newsletter sign-up (external): the browser sends the address straight to an allowed provider', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config.media ?? {};
  // the allowed provider, answered here (the test runs without the internet)
  let sent = null;
  await page.route('https://news.example.com/**', async (r) => {
    sent = { method: r.request().method(), body: r.request().postData() ?? '' };
    await r.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><html lang="en"><title>Thanks</title><p>Thanks for subscribing</p></html>' });
  });
  try {
    // an administrator allows the provider in the settings (Media)
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    await page.getByLabel(/^(services allowed for newsletter sign-ups|erlaubte dienste für newsletter-anmeldungen)/i).fill('https://news.example.com');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).first().click();
    await page.locator('.ew-toasts', { hasText: /saved|gespeichert/i }).waitFor();
    assert.deepEqual((await call('GET', '/api/v1/admin/settings')).config.media.form_allow, ['https://news.example.com']);
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Newsletter ${run}` } })).id;
    const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Newsletter ${run}`,
      body: { format: 'markdown', source: ':::newsletter{address="https://news.example.com/subscribe" title="Our newsletter" hidden="list=7" name_field="FNAME" privacy="https://news.example.com/privacy"}\nOnce a month.\n:::\n' } });
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const box = page.getByRole('region', { name: 'Our newsletter' });
    // it says where the address goes before it goes there
    const email = box.getByRole('textbox', { name: /^(e-mail address|e-mail-adresse)$/i });
    assert.match(await email.getAttribute('aria-describedby').then((id) => page.locator(`#${id}`).innerText()), /news\.example\.com/);
    await a11y('newsletter sign-up');
    // the browser checks the address first
    await box.getByRole('button', { name: /^(subscribe|anmelden)$/i }).click();
    assert.equal(sent, null, 'nothing sent without an address');
    await box.getByRole('textbox', { name: /^name$/i }).fill('Anna Example');
    await email.fill('anna@example.com');
    await box.getByRole('button', { name: /^(subscribe|anmelden)$/i }).click();
    await page.getByText('Thanks for subscribing').waitFor();
    assert.equal(sent.method, 'POST');
    assert.deepEqual(Object.fromEntries(new URLSearchParams(sent.body)), { list: '7', FNAME: 'Anna Example', email: 'anna@example.com' });
  } finally {
    await page.unroute('https://news.example.com/**');
    // back to the wiki (the helpers call the API from the page)
    await page.goto(`${BASE}/`);
    await call('PUT', '/api/v1/admin/settings', { media: { ...before, form_allow: before.form_allow ?? [] } });
  }
});

test('blocks B, part 6: the sign-in box leads guests to the sign-in page and back, and signs out', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Members ${run}` } })).id;
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'guest', permissions: 1, inherit: true });
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Members ${run}`,
    body: { format: 'markdown', source: '::login{title="Members" text="Log in for more." member="Welcome back."}' } });
  const path = `/e/${el.id}`;
  // signed in: the name, the dashboard, Log out
  await page.goto(BASE + path);
  await idle();
  const box = page.getByRole('region', { name: 'Members' });
  await box.getByText(/^(signed in as|angemeldet als) /i).waitFor();
  await box.getByText('Welcome back.').waitFor();
  assert.equal(await box.getByText('Log in for more.').count(), 0, 'no guest text for members');
  assert.equal(await box.getByRole('link', { name: /^(my dashboard|mein dashboard)$/i }).getAttribute('href'), '/dashboard');
  await box.getByRole('button', { name: /^(log out|abmelden)$/i }).waitFor();
  await a11y('sign-in box, signed in');
  // a guest: through the box to the sign-in page and back to this page; then Log out there
  const guest = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  try {
    const g = await guest.newPage();
    await g.goto(BASE + path);
    const gbox = g.getByRole('region', { name: 'Members' });
    await gbox.getByText('Log in for more.').waitFor();
    await gbox.locator('a[href^="/login?next="]').waitFor(); // the script gave the way back
    const link = gbox.getByRole('link', { name: /^(log in|anmelden)$/i });
    const here = new URL(g.url()).pathname; // the address of the page (with its name)
    assert.equal(await link.getAttribute('href'), `/login?next=${encodeURIComponent(here)}`);
    assert.equal(await gbox.locator('input').count(), 0, 'the box takes no password itself');
    await link.click();
    await g.locator('input[name=username]').fill(USER);
    await g.locator('input[type=password]').fill(PASSWORD);
    await g.keyboard.press('Enter');
    await g.waitForURL((u) => u.pathname === here);
    await gbox.getByRole('button', { name: /^(log out|abmelden)$/i }).click();
    await g.waitForURL((u) => u.pathname === '/');
    await g.goto(BASE + path);
    await gbox.getByRole('link', { name: /^(log in|anmelden)$/i }).waitFor();
  } finally {
    await guest.close();
  }
  // the session of this test is still there
  assert.ok((await call('GET', '/api/v1/auth/me')).user?.id, 'own session kept');
});

test('web application firewall: shipped off, switched on, log only, then blocking from the settings page', async () => {
  const before = (await call('GET', '/api/v1/admin/plugins')).installed.find((p) => p.id === 'core.waf');
  assert.ok(before && !before.enabled && before.default_off, 'shipped but off');
  await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'core.waf': {} } });
  await call('POST', '/api/v1/admin/plugins/core.waf/enable');
  try {
    await page.goto(`${BASE}/admin/waf`);
    await idle();
    await page.getByRole('heading', { name: /^(web application firewall)$/i }).waitFor();
    await page.getByRole('status').filter({ hasText: /(log only|nur protokollieren)/i }).first().waitFor();
    await a11y('web application firewall');
    await page.getByLabel(/^(mode|betrieb)/i).selectOption('block');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.locator('.ew-toasts', { hasText: /saved|gespeichert/i }).waitFor();
    assert.equal((await call('GET', '/api/v1/x/core.waf/status')).mode, 'block');
    // an attack in the address is refused, the wiki's own pages still load
    const blocked = await page.evaluate(async () => (await __e2eFetch('/api/v1/elements?x=%3Cscript%3Ealert(document.cookie)%3C%2Fscript%3E')).status);
    assert.equal(blocked, 403);
    await page.goto(`${BASE}/`);
    await idle();
  } finally {
    await call('POST', '/api/v1/admin/plugins/core.waf/disable');
    await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'core.waf': {} } });
  }
});

test('template designer, part 3: a warning for standard positions, widths by dragging, header while scrolling, side column, footer on the phone, printing', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const tpl = await call('POST', '/api/v1/x/core.template/templates', { names: { en: `Behaviour ${run}` }, copy_of: 'core.template/example-wiki' });
  const size = page.viewportSize();
  try {
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): Behaviour ${run}$`, 'i') }).click();
    await page.locator('.ewd-structure').waitFor();
    await page.getByText(/^(no problems|keine probleme)/i).first().waitFor();
    // removing a standard position: the designer names the building blocks standing there
    await page.getByRole('button', { name: /^tools \((area|bereich)\)$/i }).click();
    const positions = page.getByLabel(/^(positions|positionen)$/i);
    await positions.fill('tool-x');
    await page.keyboard.press('Tab');
    const warning = page.getByRole('dialog');
    await warning.getByText(/\b(search|suche)\b/i).waitFor();
    await a11y('the warning for a standard position');
    await warning.getByRole('button', { name: /^(cancel|abbrechen)$/i }).click();
    // cancelled: the position stays
    await page.waitForFunction((id) => document.getElementById(id)?.value === 'header-tools', await positions.getAttribute('id'));
    // the widths of a row: a slider for each block, here with the keyboard
    await page.getByRole('button', { name: /^(row 3 \(body\)|zeile 3 \(body\))$/i }).click();
    const slider = page.getByRole('slider', { name: /^(width of pages|breite von seiten)$/i });
    assert.equal(await slider.getAttribute('aria-valuenow'), '16');
    await slider.focus();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('PageUp');
    await page.waitForFunction(() => document.querySelector('.ewd-widthbar-handle')?.getAttribute('aria-valuenow') === '21');
    // and with the mouse: dragging the edge to the right makes the block wider
    // (by about 3 rem: the bar shows the room of the row, so the content keeps enough)
    const box = await slider.boundingBox();
    const step = await page.locator('.ewd-widthbar').evaluate((x) => (x.clientWidth / 88) * 3);
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + step, box.y + box.height / 2, { steps: 5 });
    await page.mouse.up();
    await page.waitForFunction(() => Number(document.querySelector('.ewd-widthbar-handle')?.getAttribute('aria-valuenow')) > 21);
    const dragged = await page.locator('.ewd-widthbar-handle').getAttribute('aria-valuenow');
    await a11y('the widths of a row');
    // the side column: sticky and collapsible; the options of the header and of printing
    await page.getByRole('button', { name: /^(pages|seiten) \((area|bereich)\)$/i }).click();
    assert.equal(await page.getByLabel(/^(width \(rem\)|breite \(rem\))$/i).inputValue(), dragged);
    await page.getByLabel(/^(stays in view while scrolling|bleibt beim scrollen sichtbar)/i).check();
    await page.getByLabel(/^(can be hidden|lässt sich ausblenden)/i).check();
    await page.getByLabel(/^(the sticky header gets lower|der klebende kopf wird beim scrollen niedriger)/i).check();
    await page.getByLabel(/^(the sticky header goes away|der klebende kopf verschwindet)/i).check();
    await page.getByLabel(/^(print only the content|nur den inhalt drucken)$/i).check();
    await page.getByLabel(/^(boxed layout|gerahmtes layout)$/i).check();
    // a footer that folds on the phone needs a name for every area
    await page.getByRole('button', { name: /^(row 4 \(foot\)|zeile 4 \(foot\))$/i }).click();
    await page.getByLabel(/^(open and close on the phone|auf dem handy auf- und zuklappen)$/i).check();
    await page.locator('.ewd-problems').getByText(/needs a name for every area/).waitFor();
    await a11y('the designer with the behaviours');
    await page.getByLabel(/^(open and close on the phone|auf dem handy auf- und zuklappen)$/i).uncheck();
    await page.getByText(/^(no problems|keine probleme)/i).first().waitFor();
    // deleting an area that lies in a drawer on tablet and phone also takes
    // it out of these views (else the server refuses the design)
    await page.getByRole('button', { name: /^tools \((area|bereich)\)$/i }).click();
    await page.getByRole('button', { name: /^(delete area|bereich löschen)$/i }).click();
    await page.getByText(/^(no problems|keine probleme)/i).first().waitFor();
    const saved = page.waitForResponse((r) => r.url().endsWith(`/api/v1/x/core.template/templates/${tpl.key}`) && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    // the footer folds on the phone: give it a name and something to show (the tools of the header)
    const cur = (await call('GET', `/api/v1/x/core.template/templates/${tpl.key}`)).template;
    const d = cur.design;
    assert.deepEqual([d.options.header_shrink, d.options.header_hide, d.options.print_plain, d.options.boxed], [true, true, true, true]);
    const side = d.rows.find((r) => r.id === 'body').blocks.find((b) => b.id === 'side');
    assert.deepEqual([side.width, side.sticky, side.collapsible], [Number(dragged), true, true]);
    const head = d.rows.find((r) => r.id === 'head');
    head.blocks = head.blocks.filter((b) => b.id !== 'tools');
    const foot = d.rows.find((r) => r.id === 'foot');
    foot.collapse_mobile = true;
    foot.blocks[0].label = { en: 'Tools', de: 'Werkzeuge' };
    foot.blocks[0].positions = ['header-tools'];
    await call('PUT', `/api/v1/x/core.template/templates/${tpl.key}`, { version: cur.version, design: d });
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, template: `core.template/${tpl.key}` } });
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`${BASE}/`);
    await idle();
    await page.evaluate(() => { document.querySelector('.ewd-main').style.minHeight = '4000px'; });
    // boxed: the page is a box of the content width in the middle
    const app = await page.locator('.ewd-app').evaluate((x) => { const r = x.getBoundingClientRect(); return { boxed: x.classList.contains('ewd-boxed'), max: getComputedStyle(x).maxWidth, left: r.left, right: innerWidth - r.right }; });
    assert.ok(app.boxed && app.max !== 'none' && Math.abs(app.left - app.right) < 2, JSON.stringify(app));
    // the side column stays in view and can be hidden; the browser keeps that
    const aside = page.locator('aside#ewd-side');
    assert.equal(await aside.evaluate((x) => getComputedStyle(x).position), 'sticky');
    const hide = page.getByRole('button', { name: /^(hide pages|seiten ausblenden)$/i });
    assert.equal(await hide.getAttribute('aria-expanded'), 'true');
    await hide.click();
    assert.ok(!(await aside.isVisible()));
    const show = page.getByRole('button', { name: /^(show pages|seiten einblenden)$/i });
    assert.equal(await show.getAttribute('aria-expanded'), 'false');
    await page.reload();
    await idle();
    assert.ok(!(await aside.isVisible()), 'still hidden after loading again');
    await show.click();
    await aside.waitFor();
    // the header: lower once scrolled, away while scrolling down, back when scrolling up or with the focus
    const header = page.locator('header.ewd-landmark');
    await page.evaluate(() => { document.querySelector('.ewd-main').style.minHeight = '4000px'; });
    // the focus must not be in the header (it never hides what has the focus)
    await page.evaluate(() => document.activeElement?.blur());
    await page.evaluate(() => window.scrollTo(0, 600));
    await page.waitForFunction(() => document.querySelector('header.ewd-landmark').classList.contains('ewd-shrunk') && document.querySelector('header.ewd-landmark').classList.contains('ewd-away'));
    await page.evaluate(() => window.scrollTo(0, 300));
    await page.waitForFunction(() => !document.querySelector('header.ewd-landmark').classList.contains('ewd-away'));
    await page.evaluate(() => window.scrollTo(0, 900));
    await page.waitForFunction(() => document.querySelector('header.ewd-landmark').classList.contains('ewd-away'));
    await header.locator('a').first().focus();
    await page.waitForFunction(() => !document.querySelector('header.ewd-landmark').classList.contains('ewd-away'));
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.waitForFunction(() => !document.querySelector('header.ewd-landmark').classList.contains('ewd-shrunk'));
    await a11y('the wiki with a sticky side column (desktop)');
    // printing: only the content
    await page.emulateMedia({ media: 'print' });
    assert.ok(!(await header.isVisible()));
    assert.ok(!(await aside.isVisible()));
    assert.ok(await page.locator('main').isVisible());
    await page.emulateMedia({ media: 'screen' });
    // the phone: the footer folds, the side column neither sticks nor has its button
    await page.setViewportSize({ width: 375, height: 800 });
    await page.goto(`${BASE}/`);
    await idle();
    assert.ok(!(await page.getByRole('button', { name: /^(hide pages|seiten ausblenden)$/i }).isVisible()));
    const fold = page.getByRole('button', { name: /^(tools|werkzeuge)$/i });
    assert.equal(await fold.getAttribute('aria-expanded'), 'false');
    assert.ok(!(await page.locator('#ewd-footer-body').isVisible()));
    await fold.click();
    assert.equal(await fold.getAttribute('aria-expanded'), 'true');
    await page.locator('#ewd-footer-body').waitFor();
    await a11y('the wiki with a footer that folds (phone)');
  } finally {
    if (size) await page.setViewportSize(size);
    await page.emulateMedia({ media: 'screen' }).catch(() => {});
    await page.evaluate(() => { try { for (const k of Object.keys(localStorage)) if (k.startsWith('ewd-collapsed:')) localStorage.removeItem(k); } catch { /* nothing kept */ } }).catch(() => {});
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}`).catch(() => {});
  }
});

test('template designer, part 3c: own fonts, rights per template, trying a template only for oneself', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const tpl = await call('POST', '/api/v1/x/core.template/templates', { names: { en: `Fonts ${run}` }, copy_of: 'core.template/example-wiki' });
  let fontId = 0;
  try {
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    // upload a font: license and confirmation needed
    const fonts = page.getByRole('region', { name: /^(own fonts|eigene schriften)$/i });
    // two files of a family in the usual formats; weight and style come from their names
    await fonts.getByLabel(/^(font files|schriftdateien)/i).setInputFiles(['caladea-bold.ttf', 'caladea-italic.woff'].map((f) => new URL(`./fixtures/${f}`, import.meta.url).pathname));
    const bold = fonts.getByRole('group', { name: 'caladea-bold.ttf' });
    const italic = fonts.getByRole('group', { name: 'caladea-italic.woff' });
    assert.equal(await bold.getByLabel(/^(weight|stärke)$/i).inputValue(), '700');
    assert.equal(await bold.getByLabel(/^(style|stil)$/i).inputValue(), 'normal');
    assert.equal(await italic.getByLabel(/^(weight|stärke)$/i).inputValue(), '400');
    assert.equal(await italic.getByLabel(/^(style|stil)$/i).inputValue(), 'italic');
    const name = fonts.getByLabel(/^(name in the designer|name im designer)$/i);
    assert.equal(await name.inputValue(), 'caladea', 'the name comes from the file');
    await name.fill(`Inter ${run}`);
    const logged = errors.length;
    const refused = page.waitForResponse((r) => r.url().endsWith('/api/v1/x/core.template/fonts') && r.request().method() === 'POST');
    await fonts.getByRole('button', { name: /^(upload a font|schrift hochladen)$/i }).click();
    assert.equal((await refused).status(), 400, 'without the confirmation');
    await page.locator('.ew-toast', { hasText: /confirm/i }).first().waitFor();
    // refused on purpose: the browser logs the answer 400
    errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 400/.test(e)));
    await fonts.getByLabel(/^(i confirm|ich bestätige)/i).check();
    await fonts.getByRole('button', { name: /^(upload a font|schrift hochladen)$/i }).click();
    await page.getByRole('region', { name: /^(own fonts|eigene schriften)$/i }).getByText(`Inter ${run}`).waitFor();
    fontId = (await call('GET', '/api/v1/x/core.template/fonts')).find((f) => f.name === `Inter ${run}`).id;
    // each file can be downloaded, unchanged
    const dl = page.getByRole('link', { name: new RegExp(`^(download 700 \\(truetype\\) of Inter ${run}|700 \\(truetype\\) von Inter ${run} herunterladen)$`, 'i') });
    const file = await page.evaluate(async (u) => { const r = await __e2eFetch(u); return [r.status, r.headers.get('content-type'), (await r.arrayBuffer()).byteLength]; }, await dl.getAttribute('href'));
    assert.deepEqual(file, [200, 'font/ttf', 84492]);
    await a11y('own fonts');
    // the designer offers it; the wiki loads it from itself
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): Fonts ${run}$`, 'i') }).click();
    await page.locator('.ewd-structure').waitFor();
    await page.getByLabel(/^(font|schrift)$/i).selectOption({ label: `Inter ${run}` });
    const saved = page.waitForResponse((r) => r.url().endsWith(`/api/v1/x/core.template/templates/${tpl.key}`) && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    assert.equal((await call('GET', `/api/v1/x/core.template/templates/${tpl.key}`)).template.design.tokens.font, `own-${fontId}`);
    // a used font cannot be removed: the list says where it is used instead
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('region', { name: /^(own fonts|eigene schriften)$/i }).getByText(new RegExp(`(used by|verwendet von) .*Fonts ${run}`)).waitFor();
    // rights: all signed-in people may change it (the administrator manages it anyway)
    await page.getByRole('button', { name: new RegExp(`^(rights|rechte): Fonts ${run}$`, 'i') }).click();
    const dlg = page.getByRole('dialog');
    const edit = dlg.getByRole('group', { name: /^(may change it|darf es ändern)$/i });
    await edit.getByRole('button', { name: /^(add person, group or role|person, gruppe oder rolle hinzufügen)$/i }).click();
    await edit.getByLabel(/^(applies to|gilt für)$/i).selectOption('authenticated');
    await a11y('the rights of a template');
    await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await dlg.waitFor({ state: 'detached' });
    const got = (await call('GET', `/api/v1/x/core.template/templates/${tpl.key}`)).template;
    assert.deepEqual([got.edit_subjects, got.manage_subjects, got.may], [[{ type: 'authenticated' }], [], { edit: true, manage: true }]);
    // only for me: the wiki with this template in this browser, a bar says so
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(only for me|nur für mich): Fonts ${run}$`, 'i') }).click();
    await page.waitForURL(`${BASE}/`);
    await idle();
    const bar = page.getByRole('region', { name: /^(template only for you|template nur für sie)$/i });
    await bar.waitFor();
    assert.ok(await page.evaluate((k) => document.body.classList.contains(`ewd-${k}`), tpl.key), 'the tried template is shown');
    const loaded = await page.evaluate(async (id) => { await document.fonts.ready; return [...document.fonts].some((f) => f.family.includes(`ewd-own-${id}`) && f.status === 'loaded'); }, fontId);
    assert.ok(loaded, 'the own font is loaded from the wiki');
    await a11y('the wiki with a template tried only for oneself');
    // the site still has its template for everybody else
    assert.equal((await call('GET', '/api/v1/admin/settings')).config.ui.template, before.ui.template);
    await bar.getByRole('button', { name: /^(back to the template of the site|zurück zum template der website)$/i }).click();
    await page.waitForLoadState('load');
    await idle();
    assert.equal(await bar.count(), 0);
    assert.ok(!(await page.evaluate((k) => document.body.classList.contains(`ewd-${k}`), tpl.key)));
    // tried again, then used for everybody
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(only for me|nur für mich): Fonts ${run}$`, 'i') }).click();
    await page.waitForURL(`${BASE}/`);
    await bar.waitFor();
    await bar.getByRole('button', { name: /^(use for everybody|für alle verwenden)$/i }).click();
    await page.waitForFunction(() => !document.querySelector('.ew-try-bar') && document.readyState === 'complete', null, { timeout: 10000 });
    assert.equal((await call('GET', '/api/v1/admin/settings')).config.ui.template, tpl.id);
    // no template uses the font any more: it can be removed
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}`);
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}?purge=1`);
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(delete|löschen): Inter ${run}$`, 'i') }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await page.getByRole('region', { name: /^(own fonts|eigene schriften)$/i }).getByText(`Inter ${run}`).waitFor({ state: 'detached' });
    assert.ok(!(await call('GET', '/api/v1/x/core.template/fonts')).some((f) => f.id === fontId));
    fontId = 0;
  } finally {
    await page.evaluate(() => { try { for (const k of Object.keys(localStorage)) if (k.startsWith('ewiki:try-template:')) localStorage.removeItem(k); } catch { /* nothing kept */ } }).catch(() => {});
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}`).catch(() => {});
    await call('DELETE', `/api/v1/x/core.template/templates/${tpl.key}?purge=1`).catch(() => {});
    if (fontId) await call('DELETE', `/api/v1/x/core.template/fonts/${fontId}`).catch(() => {});
  }
});

test('templates of data: copy, check while typing, save, versions, use for the site, export, import, trash; desktop, tablet and phone without sideways scrolling, drawers, sticky header only when low enough', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const imports = [];
  let key = '';
  try {
    await page.goto(`${BASE}/admin/templates`);
    await idle();
    await a11y('administration of templates');
    // copy the shipped wiki template: the text of its design opens
    const created = page.waitForResponse((r) => r.url().endsWith('/api/v1/x/core.template/templates') && r.request().method() === 'POST');
    await page.getByRole('button', { name: /^(duplicate|kopieren|duplizieren): wiki \((data|daten)\)$/i }).click();
    const tpl = await (await created).json();
    key = tpl.key;
    // the designer opens visually; the text view shows the same design
    await page.locator('.ewd-structure').waitFor();
    await page.getByRole('button', { name: /^(as text \(json\)|als text \(json\))$/i }).click();
    const text = page.getByLabel(/^(design \(text\)|design \(text\))$/i);
    await text.waitFor();
    await page.getByText(/^(no problems|keine probleme)/i).waitFor();
    // a color with too little contrast is shown at once, saving is refused
    const design = JSON.parse(await text.inputValue());
    design.tokens.text = '#bbbbbb';
    await text.fill(JSON.stringify(design, null, 2));
    await page.locator('.ewd-problems').getByText(/contrast/i).first().waitFor();
    await a11y('a template with problems');
    design.tokens.text = '#102040';
    design.tokens.primary = '#7a1f5c';
    await text.fill(JSON.stringify(design, null, 2));
    await page.getByText(/^(no problems|keine probleme)/i).waitFor();
    // the limits, as the server checks them
    await page.locator('.ewd-limits summary').click();
    await page.locator('.ewd-limits').getByText(/36 rem/).waitFor();
    const saved = page.waitForResponse((r) => r.url().endsWith(`/api/v1/x/core.template/templates/${key}`) && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    // the versions: the saved one is current, the first can be taken back into the text
    await page.locator('.ewd-versions li', { hasText: /version 2/i }).getByText(/current|aktuell/i).waitFor();
    await page.getByRole('button', { name: /^(take into the text: version 1|in den text übernehmen: version 1)$/i }).click();
    assert.ok(!(await text.inputValue()).includes('#7a1f5c'), 'version 1 is in the text');
    // use it for the site (the page loads again); version 1 in the text is not saved, so leaving asks first
    await page.getByRole('button', { name: /^(← all templates|← alle templates)$/i }).click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(leave without saving|ohne speichern verlassen)$/i }).click();
    const reloaded = page.waitForEvent('load');
    await page.getByRole('button', { name: /^(use for the site|für die website verwenden): (copy of wiki \(data\)|kopie von wiki \(daten\))$/i }).click();
    await reloaded;
    await page.goto(`${BASE}/`);
    await idle();
    assert.ok(await page.evaluate((k) => document.body.classList.contains(`ewd-${k}`), String(key)), 'the template of data is used');
    assert.equal(await page.evaluate(() => getComputedStyle(document.body).getPropertyValue('--ew-primary').trim()), '#7a1f5c');
    await page.getByRole('banner').waitFor();
    // the footer is one landmark (empty here, so it takes no room)
    assert.equal(await page.locator('footer.ewd-landmark').count(), 1);
    await a11y('the wiki with a template of data (desktop)');
    const size = page.viewportSize();
    try {
      for (const [w, h, name] of [[800, 1000, 'tablet'], [375, 760, 'phone'], [320, 640, 'small phone']]) {
        await page.setViewportSize({ width: w, height: h });
        await page.goto(`${BASE}/`);
        await idle();
        const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert.ok(over <= 0, `${name}: the page scrolls sideways by ${over} px`);
        // the side column is a drawer: closed, opened by its button, closed with Escape
        const btn = page.getByRole('button', { name: /^(▸ )?(pages|seiten)$/i });
        assert.equal(await btn.getAttribute('aria-expanded'), 'false');
        await btn.click();
        assert.equal(await btn.getAttribute('aria-expanded'), 'true');
        await page.getByRole('complementary', { name: /^(pages|seiten)$/i }).waitFor();
        await a11y(`the wiki with a template of data (${name}, drawer open)`);
        await page.getByRole('complementary', { name: /^(pages|seiten)$/i }).getByRole('link').first().focus();
        await page.keyboard.press('Escape');
        assert.equal(await btn.getAttribute('aria-expanded'), 'false');
        assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-controls')), 'ewd-side', 'the focus returns to the button');
      }
      // a sticky header takes at most a quarter of the height of the window,
      // else it scrolls with the page (a phone held sideways, large text)
      const headerPosition = () => page.evaluate(() => getComputedStyle(document.querySelector('header.ewd-landmark')).position);
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto(`${BASE}/`);
      await idle();
      assert.equal(await headerPosition(), 'sticky');
      await page.setViewportSize({ width: 900, height: 180 });
      await page.waitForFunction(() => getComputedStyle(document.querySelector('header.ewd-landmark')).position === 'static');
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.waitForFunction(() => getComputedStyle(document.querySelector('header.ewd-landmark')).position === 'sticky');
      // a drawer open in a sticky header scrolls in itself: opened far down
      // the page, the header stays and the menu is in sight (the open menu
      // made the header too high before, it stopped sticking and the menu
      // was thousands of pixels above the window)
      {
        const c0 = (await call('GET', `/api/v1/x/core.template/templates/${key}`)).template;
        const d0 = structuredClone(c0.design);
        const head = d0.rows.find((r) => r.blocks.some((b) => b.id === 'menu'));
        const toolsId = head.blocks.find((b) => b.id !== 'menu' && b.kind !== 'logo' && b.id !== 'brand')?.id;
        head.mobile = { ...(head.mobile ?? {}), drawer: ['menu', ...(toolsId ? [toolsId] : [])] };
        d0.rows = d0.rows.filter((r) => r === head || r.role !== 'banner');
        await call('PUT', `/api/v1/x/core.template/templates/${key}`, { version: c0.version, design: d0 });
        const long = await call('POST', '/api/v1/elements', { category_id: await m6Category(), type: 'core.html', title: `Long ${run}`,
          body: { format: 'markdown', source: Array.from({ length: 150 }, (_, i) => `Paragraph ${i} of a long page.`).join('\n\n') } });
        await page.setViewportSize({ width: 375, height: 760 });
        await page.goto(`${BASE}/e/${long.id}`);
        await idle();
        await page.evaluate(() => window.scrollTo(0, 3000));
        await page.waitForFunction(() => getComputedStyle(document.querySelector('header.ewd-landmark')).position === 'sticky');
        await page.locator('[aria-controls="ewd-menu"]').click();
        const box = await page.evaluate(() => {
          const r = document.getElementById('ewd-menu').getBoundingClientRect();
          return { top: r.top, bottom: r.bottom, h: innerHeight, pos: getComputedStyle(document.querySelector('header.ewd-landmark')).position, y: scrollY };
        });
        assert.equal(box.pos, 'sticky', 'the header stays sticky with the menu open');
        assert.ok(box.top >= 0 && box.bottom <= box.h + 1 && box.y > 1000, `the open menu is in the window: ${JSON.stringify(box)}`);
        // the last entry of the first level (entries of closed submenus stay hidden)
        const lastEntry = page.locator('#ewd-menu a:visible, #ewd-menu button:visible').last();
        await lastEntry.focus();
        const lb = await lastEntry.boundingBox();
        assert.ok(lb && lb.y >= 0 && lb.y + lb.height <= box.h + 1, `the last entry is reached in the drawer: ${JSON.stringify(lb)}`);
        await a11y('a menu open in a sticky header on a phone');
        await page.keyboard.press('Escape');
        await call('PUT', `/api/v1/x/core.template/templates/${key}`, { version: c0.version + 1, design: c0.design });
        await page.setViewportSize({ width: 1280, height: 800 });
      }
      // the menu as a button with a drawer also on the desktop ("burger")
      const cur = (await call('GET', `/api/v1/x/core.template/templates/${key}`)).template;
      const dd = cur.design;
      const row = dd.rows.find((r) => r.blocks.some((b) => b.id === 'menu'));
      row.desktop = { drawer: ['menu'] };
      await call('PUT', `/api/v1/x/core.template/templates/${key}`, { version: cur.version, design: dd });
      await page.goto(`${BASE}/`);
      await idle();
      const burger = page.getByRole('button', { name: /^(☰ )?(main navigation|hauptnavigation)$/i });
      await burger.waitFor();
      assert.equal(await burger.getAttribute('aria-expanded'), 'false');
      const shown = () => page.evaluate(() => getComputedStyle(document.getElementById('ewd-menu')).display !== 'none');
      assert.ok(!(await shown()), 'the menu waits behind its button');
      await burger.click();
      assert.equal(await burger.getAttribute('aria-expanded'), 'true');
      assert.ok(await shown(), 'the open menu is shown');
      await a11y('a menu in a drawer on the desktop');
      await burger.click();
      assert.equal(await burger.getAttribute('aria-expanded'), 'false');
      // a drawer with nothing in it has no button (the rule of the stylesheet
      // in a real browser; :has() inside :has() made it void before)
      await page.evaluate(() => { for (const p of document.querySelectorAll('#ewd-menu .ew-pos')) p.replaceChildren(); });
      assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('[aria-controls="ewd-menu"]')).display), 'none', 'no button for an empty drawer');
    } finally {
      if (size) await page.setViewportSize(size);
    }
    // export a template, import the file, to the trash, restore, delete for good
    await page.goto(`${BASE}/admin/templates`);
    await page.locator('.ew-admin-panel h1').waitFor();
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: /^(export|exportieren): (website \(data\)|website \(daten\))$/i }).click();
    const exported = readFileSync(await (await download).path(), 'utf8');
    assert.match(exported, /"format": "ewiki-template"/);
    const imported = page.waitForResponse((r) => r.url().endsWith('/api/v1/x/core.template/templates/import') && r.request().method() === 'POST');
    await page.getByLabel(/^(template file \(json\)|template-datei \(json\))$/i).setInputFiles({ name: 'website.template.json', mimeType: 'application/json', buffer: Buffer.from(exported) });
    const importedKey = (await (await imported).json()).key;
    imports.push(importedKey);
    const del = page.getByRole('button', { name: /^(delete|löschen): (website \(data\)|website \(daten\))$/i });
    await del.click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await page.getByRole('button', { name: /^(restore|wiederherstellen): (website \(data\)|website \(daten\))$/i }).click();
    await del.click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    const purged = page.waitForResponse((r) => r.url().includes(`/templates/${importedKey}?purge=1`));
    await page.getByRole('button', { name: /^(delete permanently|endgültig löschen): (website \(data\)|website \(daten\))$/i }).click();
    await page.locator('dialog[open]').getByRole('button', { name: /^(delete permanently|endgültig löschen)$/i }).click();
    assert.equal((await purged).status(), 204);
    await page.getByRole('button', { name: /^(restore|wiederherstellen): (website \(data\)|website \(daten\))$/i }).waitFor({ state: 'detached' });
    imports.length = 0;
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    if (key) await call('DELETE', `/api/v1/x/core.template/templates/${key}`);
    for (const k of imports) {
      // to the trash first (it may be there already), then for good
      await call('DELETE', `/api/v1/x/core.template/templates/${k}`).catch(() => {});
      await call('DELETE', `/api/v1/x/core.template/templates/${k}?purge=1`).catch(() => {});
    }
  }
});

test('a hidden page: not listed for readers, marked for editors, reachable by its link', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Quiet ${run}`, body: { format: 'html', source: '<p>quiet</p>' } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(properties|eigenschaften)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByRole('checkbox', { name: /^(hidden|unsichtbar)/i }).check();
  await a11y('element properties with hidden');
  await dlg.locator('button[type=submit]').click();
  await page.getByRole('note').filter({ hasText: /this page is hidden|diese seite ist unsichtbar/i }).waitFor();
  assert.equal((await call('GET', `/api/v1/elements/${el.id}`)).hidden, true);
  await page.goto(`${BASE}/c/${cat}`);
  await idle();
  // the editor sees it in the category, marked
  const item = page.locator('.ew-list li', { hasText: `Quiet ${run}` });
  await item.getByText(/^(hidden|unsichtbar)$/i).waitFor();
  // readers: not listed (checked through the API of the category)
  assert.ok((await call('GET', `/api/v1/categories/${cat}`)).elements.some((x) => x.id === el.id && x.hidden), 'listed for editors, marked');
  // leave the edit mode as it was for the next tests
  const off = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await off.getAttribute('aria-pressed')) === 'true') { await off.click(); await idle(); }
});

test('rights of a page: own rights only, a rule, the note on the page', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Guarded ${run}`, body: { format: 'html', source: '<p>guarded</p>' } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(rights|rechte)$/i }).click();
  let dlg = page.locator('dialog[open]');
  await dlg.getByRole('checkbox', { name: /^(own rights only|nur eigene rechte)/i }).waitFor();
  await a11y('element rights');
  await dlg.getByRole('checkbox', { name: /^(own rights only|nur eigene rechte)/i }).check();
  await dlg.getByLabel(/^(classification of the page|einstufung der seite)/i).selectOption('1');
  await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.getByText(/^(applies|es gilt): (internal|intern)/i).waitFor();
  const acl = await call('GET', `/api/v1/elements/${el.id}/acl`);
  assert.equal(acl.own_rights_only, true);
  assert.equal(acl.classification, 1);
  // a rule for all signed-in people, view only
  dlg = page.locator('dialog[open]');
  await dlg.getByRole('button', { name: /^(add rule|regel hinzufügen)$/i }).click();
  const add = page.locator('dialog[open]').last();
  await add.getByLabel(/^(applies to|gilt für)/i).selectOption('authenticated');
  await a11y('element rights: add a rule');
  await add.locator('button[type=submit]').click();
  await page.locator('dialog[open] table').getByText(/logged-in users|angemeldeten benutzer/i).first().waitFor();
  const after = await call('GET', `/api/v1/elements/${el.id}/acl`);
  assert.equal(after.rules.length, 1);
  assert.equal(after.rules[0].subject_type, 'authenticated');
  // change the rule: also edit
  await page.locator('dialog[open]').last().getByRole('button', { name: /^(edit permissions of|rechte von .* bearbeiten)/i }).click();
  const ed = page.locator('dialog[open]').last();
  await ed.getByRole('checkbox', { name: /^(edit|bearbeiten)$/i }).check();
  await a11y('element rights: edit a rule');
  await ed.locator('button[type=submit]').click();
  await page.locator('dialog[open]').last().locator('table').getByText(/^(view, edit|ansehen, bearbeiten)$/i).waitFor();
  assert.equal((await call('GET', `/api/v1/elements/${el.id}/acl`)).rules[0].permissions, 3);
  // remove it
  await page.locator('dialog[open]').last().getByRole('button', { name: /^(remove rule for|regel für .* entfernen)/i }).click();
  await page.locator('dialog[open]').last().getByRole('button', { name: /^(remove|entfernen)$/i }).click();
  await page.locator('dialog[open]').last().locator('table').getByText(/^(no rules|keine regeln)/i).waitFor();
  assert.equal((await call('GET', `/api/v1/elements/${el.id}/acl`)).rules.length, 0);
  await page.keyboard.press('Escape');
  await page.reload();
  await idle();
  await page.getByRole('note').filter({ hasText: /only the page's own rights|nur ihre eigenen rechte/i }).waitFor();
  // the central overview lists the rule on the page
  await call('POST', `/api/v1/elements/${el.id}/acl`, { subject_type: 'authenticated', permissions: 1 });
  await page.goto(`${BASE}/admin/permissions?subject_type=authenticated`);
  await page.locator('tr.ew-perm-element').filter({ hasText: `Guarded ${run}` }).waitFor();
  await a11y('permission overview with a page rule');
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  const off = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await off.getAttribute('aria-pressed')) === 'true') { await off.click(); await idle(); }
});

test('FAQ: sections, questions, filter and quick add', async () => {
  const cat = await m6Category();
  const faqCat = await call('POST', '/api/v1/categories', { parent_id: cat, titles: { en: `Help ${run}` } });
  const faq = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.faq', title: `FAQ ${run}`,
    body: { category_id: faqCat.id, recursive: true, search: true, feed: true } });
  const sec = await call('POST', '/api/v1/categories', { via: faq.id, parent_id: faqCat.id, titles: { en: `Accounts ${run}` } });
  await call('POST', '/api/v1/elements', { via: faq.id, type: 'core.html', title: 'How do I reset my password?', category_id: sec.id, body: { format: 'markdown', source: 'Use the **reset** link.' } });
  await call('POST', '/api/v1/elements', { via: faq.id, category_id: faqCat.id, type: 'core.html', title: 'Who helps me?', body: { format: 'markdown', source: 'The help desk.' } });
  await page.goto(BASE + `/e/${faq.id}`);
  await idle();
  // on its own page the controls of the FAQ are blocks of the page
  await page.locator('.ew-pos--element-header .ewq-search').waitFor();
  await page.locator('.ew-pos--element-toolbar').getByRole('button', { name: /^(expand all|alle aufklappen)$/i }).waitFor();
  assert.equal(await page.locator('.ewq-toolbar').count(), 0, 'no toolbar of its own on the page of the FAQ');
  await a11y('faq');
  await page.getByRole('searchbox', { name: /search the faq|faq durchsuchen/i }).fill('password');
  await page.waitForFunction(() => /1/.test(document.querySelector('.ewq-status')?.textContent ?? ''));
  assert.ok(await page.getByText('Use the').isVisible(), 'matching answer opened');
  assert.equal(await page.locator('details.ewq-q:not([hidden])').count(), 1);
  await page.getByRole('searchbox', { name: /search the faq|faq durchsuchen/i }).fill('');
  await page.getByRole('button', { name: /^(new question|neue frage)$/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/^(question|frage)/i).fill(`Quick ${run}?`);
  await dlg.getByLabel(/^(answer|antwort)/i).fill('Yes.');
  await a11y('faq question dialog');
  await dlg.locator('button[type=submit]').click();
  await page.locator(`summary:has-text("Quick ${run}?")`).waitFor();
  // edit mode: edit and delete a question directly in the FAQ
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.locator('.ewq-edit').first().waitFor({ state: 'attached' });
  const quick = page.locator('details.ewq-q', { hasText: `Quick ${run}?` });
  await quick.evaluate((d) => { d.open = true; });
  await quick.getByRole('button', { name: /^(edit|bearbeiten):/i }).click();
  const ed = page.locator('dialog[open]');
  await ed.getByLabel(/^(question|frage)/i).fill(`Quicker ${run}?`);
  await a11y('faq edit question dialog');
  await ed.locator('button[type=submit]').click();
  await page.locator(`summary:has-text("Quicker ${run}?")`).waitFor();
  const quicker = page.locator('details.ewq-q', { hasText: `Quicker ${run}?` });
  // (the edited question is open again: its address is #q-ID)
  await quicker.locator('.ewq-edit').waitFor({ state: 'attached' });
  await quicker.evaluate((d) => { d.open = true; });
  await quicker.getByRole('button', { name: /^(delete|löschen):/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await page.locator(`summary:has-text("Quicker ${run}?")`).waitFor({ state: 'detached' });
  // sections: rename and delete
  const section = page.locator('details.ewq-section', { hasText: `Accounts ${run}` });
  await section.locator(':scope > .ewq-section-body > .ewq-edit').waitFor({ state: 'attached' });
  await section.evaluate((d) => { d.open = true; });
  await section.getByRole('button', { name: /^(edit section|abschnitt bearbeiten):/i }).click();
  await page.locator('dialog[open] input[lang="en"]').fill(`Logins ${run}`);
  await page.locator('dialog[open] button[type=submit]').click();
  const renamed = page.locator('details.ewq-section', { hasText: `Logins ${run}` });
  await renamed.locator(':scope > .ewq-section-body > .ewq-edit').waitFor({ state: 'attached' });
  await renamed.evaluate((d) => { d.open = true; });
  await renamed.getByRole('button', { name: /^(delete section|abschnitt löschen):/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await page.locator('details.ewq-section', { hasText: `Logins ${run}` }).waitFor({ state: 'detached' });
  await edit.click();
  await idle();
});

test('event calendar: views, details and registration', async () => {
  const cat = await m6Category();
  const cal = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.events', title: `Calendar ${run}`,
    body: { view: 'month', ical: true, categories: [{ label: 'Training', color: '#1f845a' }] } });
  const start = new Date();
  start.setDate(start.getDate() + 2);
  start.setHours(10, 0, 0, 0);
  await call('POST', `/api/v1/x/content.events/calendars/${cal.id}/events`, { title: `Workshop ${run}`, starts_at: start.toISOString(),
    ends_at: new Date(start.getTime() + 7200000).toISOString(), tz: 'Europe/Vienna', category: 'training', registration: true, capacity: 10, reminders: [1440], location: 'Room 1' });
  await call('POST', `/api/v1/x/content.events/calendars/${cal.id}/events`, { title: `Stand-up ${run}`, starts_at: start.toISOString(), tz: 'Europe/Vienna', rrule: 'FREQ=WEEKLY;COUNT=6' });
  await page.goto(BASE + `/e/${cal.id}`);
  await idle();
  await page.evaluate(() => sessionStorage.clear());
  await page.reload();
  await idle();
  await page.locator('.ewe-month').waitFor();
  await a11y('calendar month');
  await page.getByRole('button', { name: new RegExp(`Workshop ${run}`) }).first().click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByText('Room 1').waitFor();
  await a11y('event details');
  await dlg.getByRole('button', { name: /^(register|anmelden)$/i }).click();
  const reg = page.locator('dialog[open]').last();
  await reg.locator('button[type=submit]').click();
  await page.waitForFunction(() => !document.querySelector('dialog[open]'));
  await page.getByRole('button', { name: /^(week|woche)$/i }).click();
  await page.locator('.ewe-week').waitFor();
  await a11y('calendar week');
  await page.getByRole('button', { name: /^(list|liste)$/i }).click();
  await page.locator('.ewe-list').waitFor();
  assert.ok(await page.locator('.ewe-list .ew-badge--ok').count() >= 1, 'own registration shown');
  await a11y('calendar list');
  await page.getByRole('button', { name: /^(new event|neuer termin)$/i }).click();
  const form = page.locator('dialog[open]');
  await form.getByLabel(/^(title|titel)/i).fill(`New ${run}`);
  await form.getByLabel(/^(repeat|wiederholen)/i).selectOption('WEEKLY');
  await a11y('event form');
  await form.locator('button[type=submit]').click();
  await page.getByRole('button', { name: new RegExp(`New ${run}`) }).first().waitFor();

  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  // closes the open dialogs one after the other, as a person would
  const closeAll = async () => {
    await idle();
    while (await page.locator('dialog[open]').count()) {
      // a dialog may close itself meanwhile (after reloading): then the next one
      await page.locator('dialog[open]').last().getByRole('button', { name: /^(close|schließen)$/i }).first().click({ timeout: 3000 }).catch(() => undefined);
    }
  };
  const details = async (title) => {
    await page.getByRole('button', { name: new RegExp(title) }).first().click();
    const d = page.locator('dialog[open]').last();
    await d.waitFor();
    return d;
  };
  // the event as a file for the own calendar
  let d = await details(`Workshop ${run}`);
  const ics = page.waitForEvent('download');
  await d.getByRole('link', { name: /^(add to my calendar \(\.ics\)|in meinen kalender \(\.ics\))$/i }).click();
  assert.match(readFileSync(await (await ics).path(), 'utf8'), /BEGIN:VCALENDAR[\s\S]*Workshop/);
  // participants: the own registration, removed by the manager
  await d.getByRole('button', { name: /^(participants|teilnehmende)$/i }).click();
  const parts = page.locator('dialog[open]').last();
  await parts.getByRole('button', { name: /^(remove|entfernen)/i }).first().click();
  await page.getByRole('dialog').last().getByRole('button', { name: /^(remove|entfernen)$/i }).click();
  await toastOn(/^(removed|abgemeldet)$/i);
  // registered again, seen in "my registrations", then cancelled
  await closeAll();
  d = await details(`Workshop ${run}`);
  await d.getByRole('button', { name: /^(register|anmelden)$/i }).click();
  await page.locator('dialog[open]').last().locator('button[type=submit]').click();
  await closeAll();
  await page.getByRole('button', { name: /^(my registrations|meine anmeldungen)$/i }).click();
  const mine = page.locator('dialog[open]').last();
  await mine.getByText(`Workshop ${run}`).first().waitFor();
  await a11y('my registrations');
  await mine.getByRole('button', { name: /^(close|schließen)$/i }).last().click();
  d = await details(`Workshop ${run}`);
  await d.getByRole('button', { name: /^(cancel registration|abmelden)$/i }).click();
  await toastOn(/^(cancelled|abgemeldet)$/i);
  // edit the event
  await closeAll();
  d = await details(`Workshop ${run}`);
  await d.getByRole('button', { name: /^(edit|bearbeiten)$/i }).click();
  const ef = page.locator('dialog[open]').last();
  await ef.getByLabel(/^(location|ort)/i).fill('Room 2');
  await ef.locator('button[type=submit]').click();
  await toastOn(/event saved|termin gespeichert/i);
  // one date of the series changed, then like the series again
  await closeAll();
  d = await details(`Stand-up ${run}`);
  await d.getByRole('button', { name: /^(edit this occurrence only|nur diesen termin bearbeiten)$/i }).click();
  const of = page.locator('dialog[open]').last();
  await of.getByLabel(/^(title|titel)/i).fill(`Stand-up moved ${run}`);
  await of.locator('button[type=submit]').click();
  await page.getByRole('button', { name: new RegExp(`Stand-up moved ${run}`) }).first().waitFor();
  d = await details(`Stand-up moved ${run}`);
  await d.getByRole('button', { name: /^(reset changes|änderung zurücksetzen)$/i }).click();
  await page.getByRole('dialog').last().getByRole('button', { name: /^(reset changes|änderung zurücksetzen)$/i }).click();
  await toastOn(/follows the series again|entspricht wieder der serie/i);
  // the subscription address of the calendar answers with the calendar
  await closeAll();
  await page.getByRole('button', { name: /^(subscribe|abonnieren)$/i }).click();
  const feed = await page.locator('dialog[open]').last().getByLabel(/^(calendar address|adresse des kalenders)$/i).inputValue();
  await page.locator('dialog[open]').last().getByRole('button', { name: /^(close|schließen)$/i }).last().click();
  // opened in the browser, the address delivers the calendar as a file
  const sub = page.waitForEvent('download');
  await page.goto(feed).catch(() => undefined);
  assert.match(readFileSync(await (await sub).path(), 'utf8'), /BEGIN:VCALENDAR[\s\S]*Workshop/);
  // delete the event
  await page.goto(BASE + `/e/${cal.id}`);
  await idle();
  d = await details(`Workshop ${run}`);
  await d.getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await page.getByRole('dialog').last().getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await toastOn(/event deleted|termin gelöscht/i);
});

test('team page and business card', async () => {
  await page.goto(BASE + '/profile');
  await idle();
  await page.getByLabel(/^(job title|funktion)/i).fill('Wiki gardener');
  await page.locator('#card button[type=submit]').click();
  await page.waitForTimeout(300);
  await a11y('profile with business card');
  const cat = await m6Category();
  const g = await call('POST', '/api/v1/admin/groups', { name: `Team ${run}` });
  const me = (await call('GET', '/api/v1/auth/me')).user;
  await call('PUT', `/api/v1/admin/users/${me.id}`, { groups: [...(me.groups ?? []), g.id] });
  const team = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.team', title: `Team ${run}`,
    body: { groups: [g.id], fields: ['photo', 'job_title', 'email'], layout: 'cards' } });
  await page.goto(BASE + `/e/${team.id}`);
  await idle();
  await page.getByText('Wiki gardener').waitFor();
  await a11y('team page');
  // contact details for guests: a choice in the editor (the administrator may)
  await page.goto(`${BASE}/e/${team.id}/edit`);
  await idle();
  const pub = page.getByLabel(/^(show the contact details to guests too|kontaktdaten auch gästen zeigen)$/i);
  assert.equal(await pub.isChecked(), false, 'not public at first');
  await pub.check();
  await a11y('team editor');
  await page.getByRole('button', { name: /^(publish|veröffentlichen|save|speichern)$/i }).first().click();
  await page.waitForURL(new RegExp(`/e/${team.id}$`));
  const saved = await call('GET', `/api/v1/elements/${team.id}?edit=1`);
  assert.equal(saved.body?.public, true, 'saved as public');
});

test('people blocks: a profile card chosen by name and the people of a group', async () => {
  const g = await call('POST', '/api/v1/admin/groups', { name: `Crew ${run}` });
  const u = await call('POST', '/api/v1/admin/users', { username: `crew${run}`, password: 'Passw0rd!Passw0rd', display_name: `Crew Okapi ${run}`, email: `crew${run}@example.com`, groups: [g.id] });
  await call('PUT', `/api/v1/admin/users/${u.id}/profile`, { job_title: 'Deck officer' });
  await newPage('core.html', `People ${run}`);
  await page.locator('.ewe-prose').click();
  const dlg = await insertBlock('profilecard');
  const who = dlg.getByPlaceholder(/name or user name|name oder benutzername/i);
  const name = `Crew Okapi ${run}`;
  await who.fill(name);
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] datalist option')].some((o) => o.value.includes(n)), name);
  await who.fill(await page.evaluate((n) => [...document.querySelectorAll('dialog[open] datalist option')].find((o) => o.value.includes(n)).value, name));
  await a11y('profile card dialog');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="profilecard"]');
  const dlg2 = await insertBlock('people');
  await dlg2.locator('select').filter({ has: page.locator(`option[value="${g.id}"]`) }).selectOption(String(g.id));
  await dlg2.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="people"]');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ewt--card').getByText('Deck officer').waitFor();
  await page.locator('.ew-macro--people').getByText(name).waitFor();
  await a11y('people blocks');
});

test('decisions: a decision in a page, the report of decisions and the data for reports in the administration', async () => {
  const person = `Decider Lynx ${run}`;
  await call('POST', '/api/v1/admin/users', { username: `decider${run}`, password: 'Passw0rd!Passw0rd', display_name: person, email: `decider${run}@example.com` });
  await newPage('core.html', `Decisions ${run}`);
  await page.locator('.ewe-prose').click();
  const title = `Choose okapi ${run}`;
  const dlg2 = await insertBlock('decisions');
  await dlg2.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="decisions"]');
  const dlg = await insertBlock('decision');
  await dlg.getByLabel(/^(decision in short|entscheidung in kürze)/i).fill(title);
  await dlg.getByLabel(/^status/i).selectOption('decided');
  await dlg.getByLabel(/^(day|datum)/i).fill('2026-11-20');
  // the responsible person, chosen by name, and a text
  const who = dlg.getByPlaceholder(/name or user name|name oder benutzername/i);
  await who.fill(person);
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] datalist option')].some((o) => o.value.includes(n)), person);
  await who.fill(await page.evaluate((n) => [...document.querySelectorAll('dialog[open] datalist option')].find((o) => o.value.includes(n)).value, person));
  await dlg.getByLabel(/^(responsible \(text|verantwortlich \(text)/i).fill('Board');
  await a11y('decision dialog');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="decision"]');
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('.ew-decision-title', { hasText: title }).waitFor();
  await page.locator('.ew-decisions tr', { hasText: title }).getByText(/^(decided|entschieden)$/i).waitFor();
  await page.locator('.ew-decision-meta', { hasText: `${person}, Board` }).waitFor();
  await page.locator('.ew-decisions tr', { hasText: title }).getByText(`${person}, Board`).waitFor();
  await a11y('decision and report');
  await page.goto(BASE + '/admin/data');
  await idle();
  await page.getByRole('button', { name: /^(show entries|einträge zeigen): (decisions|entscheidungen)$/i }).click();
  await page.locator('.ew-admin-panel table tr', { hasText: title }).getByText(person, { exact: true }).waitFor();
  await a11y('data for reports');
  await page.getByRole('button', { name: /^(build again|neu aufbauen): (decisions|entscheidungen)$/i }).click();
  await page.getByRole('button', { name: /^(show entries|einträge zeigen): (decisions|entscheidungen)$/i }).waitFor();
  // "my decisions": only those I am the responsible person of
  const me = (await call('GET', '/api/v1/auth/me')).user;
  const mine = await call('POST', '/api/v1/elements', { category_id: await m6Category(), type: 'core.html', title: `My decisions ${run}`,
    body: { format: 'markdown', source: `:::decision{title="Mine okapi ${run}" status=open person=${me.id}}\n:::\n\n::decisions{who=mine}\n` } });
  await page.goto(`${BASE}/e/${mine.id}`);
  const myReport = page.getByRole('region', { name: /^(my decisions|meine entscheidungen)$/i });
  await myReport.locator('tr', { hasText: `Mine okapi ${run}` }).waitFor();
  assert.equal(await myReport.locator('tr', { hasText: title }).count(), 0, 'decisions of others are not listed');
  await a11y('my decisions');
});

test('tasks in pages: the report ticks a task (the page is saved), My tasks lists the own open tasks', async () => {
  const me = (await call('GET', '/api/v1/auth/me')).user;
  const src = `- [ ] Check okapi ${run} @${me.username} :date{value=2026-01-05}\n- [ ] Feed lynx ${run} @${me.username}\n- [ ] Nobody's walrus ${run}\n\n::tasks{}\n`;
  const pg = await call('POST', '/api/v1/elements', { category_id: await m6Category(), type: 'core.html', title: `Tasks ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${pg.id}`);
  await idle();
  const report = page.getByRole('region', { name: /^(tasks|aufgaben)$/i });
  const row = report.locator('tr', { hasText: `Check okapi ${run}` });
  await row.getByText(/^(overdue|überfällig)$/i).waitFor();
  assert.equal(await report.locator('tr', { hasText: `Nobody's walrus ${run}` }).count(), 0, 'a task without a person or a date is no task of the report');
  await a11y('report of tasks');
  // ticking saves the page: only this checkbox changes
  const box = row.getByRole('checkbox', { name: new RegExp(`^(done|erledigt): check okapi ${run}`, 'i') });
  const saved = page.waitForResponse((r) => /\/api\/v1\/x\/core\.html\/tasks\/\d+\/1$/.test(r.url()));
  await box.check();
  assert.equal((await saved).status(), 200);
  await report.getByRole('status').getByText(/^(task done|aufgabe erledigt)/i).waitFor();
  const after = await call('GET', `/api/v1/elements/${pg.id}?edit=1`);
  assert.equal(after.body.source, src.replace('- [ ] Check okapi', '- [x] Check okapi'));
  // the second task of the same page ticks from the new version
  const saved2 = page.waitForResponse((r) => /\/api\/v1\/x\/core\.html\/tasks\/\d+\/2$/.test(r.url()));
  await report.locator('tr', { hasText: `Feed lynx ${run}` }).getByRole('checkbox').check();
  assert.equal((await saved2).status(), 200);
  const saved3 = page.waitForResponse((r) => /\/api\/v1\/x\/core\.html\/tasks\/\d+\/2$/.test(r.url()));
  await report.locator('tr', { hasText: `Feed lynx ${run}` }).getByRole('checkbox').uncheck();
  assert.equal((await saved3).status(), 200);
  // My tasks: the own open tasks in pages
  await page.goto(`${BASE}/tasks`);
  await idle();
  await page.getByRole('heading', { level: 1, name: /^(my tasks|meine aufgaben)$/i }).waitFor();
  const section = page.getByRole('region', { name: /^(my open tasks in pages|meine offenen aufgaben in seiten)$/i });
  await section.locator('tr', { hasText: `Feed lynx ${run}` }).getByRole('checkbox').waitFor();
  assert.equal(await section.locator('tr', { hasText: `Check okapi ${run}` }).count(), 0, 'done tasks are not listed');
  await a11y('my tasks');
});

test('editor: suggestions of blocks while typing ("/" and ":name"), the task with person and date', async () => {
  const me = (await call('GET', '/api/v1/auth/me')).user;
  await newPage('core.html', `Suggest ${run}`);
  const ed = page.locator('.ewe-prose');
  await ed.click();
  // "/" at the start of a paragraph: the blocks, the task first
  await page.keyboard.type('/');
  const list = page.getByRole('listbox', { name: /^(blocks|bausteine)$/i });
  await list.waitFor();
  assert.match(await ed.getAttribute('aria-activedescendant') ?? '', /^ewe-suggest/);
  await a11y('suggestions of blocks');
  await page.keyboard.type('task');
  await list.getByRole('option', { name: /^(task with person and date|aufgabe mit person und datum)/i, selected: true }).waitFor();
  await page.keyboard.press('Enter');
  const dlg = page.locator('dialog[open]').last();
  await dlg.getByLabel(/^(task|aufgabe)/i).first().fill(`Water the okapi ${run}`);
  const who = dlg.getByPlaceholder(/name or user name|name oder benutzername/i);
  await who.fill(me.username);
  await page.waitForFunction(() => document.querySelectorAll('dialog[open] datalist option').length > 0);
  await who.fill(await page.evaluate(() => document.querySelector('dialog[open] datalist option').value));
  await dlg.getByLabel(/^(due|fällig)/i).fill('2026-12-24');
  await a11y('task dialog');
  await dlg.getByRole('button', { name: /^(insert|einfügen)$/i }).click();
  await ed.locator('li.ewe-task', { hasText: `Water the okapi ${run}` }).waitFor();
  assert.equal(await ed.locator('li.ewe-task', { hasText: `Water the okapi ${run}` }).locator('.ewe-chip, .ewe-macro-inline, [data-macro="date"]').count() > 0, true, 'the date is a block in the text');
  assert.equal(await ed.getByText('/task').count(), 0, 'the typed text is replaced');
  // ":name" in the text: Escape closes, the text stays
  await ed.locator('p').last().click();
  await page.keyboard.press('End');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Due :dat');
  await list.getByRole('option', { name: /^(date|datum)/i }).first().waitFor();
  await page.keyboard.press('Escape');
  await list.waitFor({ state: 'detached' });
  await ed.getByText('Due :dat').waitFor();
  // Markdown: "::" opens the dialog of the block
  await page.getByRole('button', { name: /^(markdown mode|markdown-modus)$/i }).click();
  const src = page.locator('textarea.ewe-markdown');
  assert.match(await src.inputValue(), new RegExp(`- \\[ \\] Water the okapi ${run} @${me.username} :date(\\[\\])?\\{value=2026-12-24\\}`));
  await src.press('Control+End');
  await src.pressSequentially('\n\n::tasks');
  await list.getByRole('option', { name: /^(report of tasks|aufgaben-bericht)/i }).waitFor();
  await src.press('Enter');
  const mdlg = page.locator('dialog[open]').last();
  await mdlg.locator('.ewe-macro-params').waitFor();
  await mdlg.locator('button[type=submit]').click();
  await page.waitForFunction(() => /\n::tasks\{?/.test(document.querySelector('textarea.ewe-markdown').value) && !document.querySelector('textarea.ewe-markdown').value.includes('::tasks\n::tasks'));
  assert.equal((await src.inputValue()).match(/::tasks/g).length, 1, 'the typed name is replaced by the block');
  await page.getByRole('button', { name: /^(visual mode|visueller modus)$/i }).click();
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.getByRole('region', { name: /^(tasks|aufgaben)$/i }).locator('tr', { hasText: `Water the okapi ${run}` }).waitFor();
});

test('dates in pages: an appointment with the date block, the report of dates, a calendar with the category as a source', async () => {
  const cat = await m6Category();
  const soon = new Date(Date.now() + 5 * 86400000).toISOString().slice(0, 10);
  // the date block: a title makes it an appointment, with start and end
  await newPage('core.html', `Dates ${run}`);
  await page.locator('.ewe-prose').click();
  await page.keyboard.type('We meet on ');
  const dlg = await insertBlock('date');
  await dlg.getByLabel(/^(date|datum)/i).first().fill(soon);
  await dlg.getByLabel(/^(title of the appointment|titel des termins)/i).fill(`Okapi meeting ${run}`);
  await dlg.getByLabel(/^(starts at|beginn)/i).fill('10:00');
  await dlg.getByLabel(/^(ends at|ende)/i).fill('11:30');
  await a11y('date dialog with an appointment');
  await dlg.locator('button[type=submit]').click();
  await page.locator('.ewe-prose').press('End');
  await page.keyboard.press('Enter');
  const dlg2 = await insertBlock('dates');
  await dlg2.locator('button[type=submit]').click();
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.locator('time.ew-date', { hasText: /10:00–11:30/ }).waitFor();
  const report = page.getByRole('region', { name: /^(dates|termine)$/i });
  await report.locator('tr', { hasText: `Okapi meeting ${run}` }).getByText('10:00–11:30').waitFor();
  await a11y('report of dates');
  // a calendar with the category as a source: the appointment, read-only, with a link to the page
  const found = (await call('GET', `/api/v1/elements?q=${encodeURIComponent(`Dates ${run}`)}&limit=5`)).items.find((x) => x.title === `Dates ${run}`);
  const pageId = found.id, pageCat = found.category_id;
  const cal = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.events', title: `Page dates ${run}`,
    body: { view: 'list', page_tasks: true, more_sources: `category:${pageCat}` } });
  await page.goto(`${BASE}/e/${cal.id}`);
  await idle();
  // the view chosen last is kept: the list shows the next weeks
  await page.getByRole('button', { name: /^(list|liste)$/i }).click();
  await page.locator('.ewe-list').waitFor();
  await page.getByRole('button', { name: new RegExp(`Okapi meeting ${run}`) }).first().click();
  const det = page.locator('dialog[open]').last();
  const link = det.getByRole('link', { name: `Dates ${run}` });
  await link.waitFor();
  assert.equal(await link.getAttribute('href'), `/e/${pageId}`);
  assert.equal(await det.getByRole('button', { name: /^(edit|bearbeiten)$/i }).count(), 0, 'read-only here');
  await a11y('appointment of a page in a calendar');
  await det.getByRole('button', { name: /^(close|schließen)$/i }).first().click();
  // the settings: pages are chosen by their title
  await page.goto(`${BASE}/e/${cal.id}/edit`);
  await idle();
  const sources = page.locator('fieldset.ew-source-builder').first();
  await sources.getByRole('button', { name: /^(add elements|elemente \(ids\) hinzufügen)/i }).click();
  await sources.getByRole('button', { name: /^(choose a page|seite wählen)$/i }).click();
  const pick = page.locator('dialog[open]').last();
  await pick.getByRole('searchbox').first().fill(`Dates ${run}`);
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] .ew-picker-list option')].some((o) => o.textContent.startsWith(`${n} –`)), `Dates ${run}`);
  const opt = await page.evaluate((n) => [...document.querySelectorAll('dialog[open] .ew-picker-list option')].find((o) => o.textContent.startsWith(`${n} –`))?.value, `Dates ${run}`);
  await pick.locator('.ew-picker-list').selectOption(opt);
  await a11y('choosing a page as a source');
  await pick.getByRole('button', { name: /^(add the page|seite hinzufügen)$/i }).click();
  assert.equal(await sources.getByRole('textbox', { name: /^(elements \(ids\)|elemente \(ids\))$/i }).inputValue(), String(pageId));
  // (measured at the top: a check box scrolled under the sticky header counts as too small)
  await page.evaluate(() => window.scrollTo(0, 0));
  await a11y('calendar settings with pages as a source');
  await sources.getByRole('button', { name: /^(remove elements|elemente \(ids\) entfernen)/i }).click();
});

test('a block readers may close (notice bar): closed it stays closed in this browser until its text changes', async () => {
  const cat = await m6Category();
  const body = (text) => ({ format: 'markdown', source: `:::panel{type=info options="closable:true"}\n### Move notice ${run}\n\n${text}\n:::\n\nAfter the notice: [the start page](/).\n` });
  const pg = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Notice ${run}`, body: body('The wiki moves on Friday.') });
  await page.goto(`${BASE}/e/${pg.id}`);
  await idle();
  const close = page.getByRole('button', { name: new RegExp(`^(close|schließen): move notice ${run}$`, 'i') });
  await close.waitFor();
  await a11y('a block readers may close');
  await close.click();
  await page.getByText('The wiki moves on Friday.').waitFor({ state: 'hidden' });
  assert.equal(await page.evaluate(() => document.activeElement?.textContent), 'the start page', 'the focus goes on to what follows');
  await page.reload();
  await idle();
  await page.getByText('After the notice:').waitFor();
  assert.equal(await page.getByText('The wiki moves on Friday.').isVisible(), false, 'closed after reloading');
  // a new text: shown again
  const cur = await call('GET', `/api/v1/elements/${pg.id}`);
  await call('PUT', `/api/v1/elements/${pg.id}`, { lang: cur.shown_lang, title: `Notice ${run}`, body: body('The move is postponed to Monday.') });
  await page.reload();
  await idle();
  await page.getByText('The move is postponed to Monday.').waitFor();
  await close.waitFor();
});

test('block options: a name for screen readers, scrolling in itself, sides and corners, underline, sticky', async () => {
  const cat = await m6Category();
  const lines = Array.from({ length: 30 }, (_, i) => `- Date ${i + 1}`).join('\n');
  const source = `:::panel{type=info options="label:Termin%C3%BCbersicht%20${run} scroll_height:6"}\n${lines}\n:::\n\n`
    + ':::panel{type=info options="border_style:solid border_width:3 border_color:#0b3a6b border_sides:start radius:12 radius_corners:end underline_style:wavy underline_color:#0b3a6b"}\nA [link to the start](/) here.\n:::\n\n'
    + `:::panel{type=info options="sticky:true"}\nStays in view ${run}\n:::\n`;
  const pg = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Options more ${run}`, body: { format: 'markdown', source } });
  const size = page.viewportSize();
  try {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`${BASE}/e/${pg.id}`);
    await idle();
    // scrolls in itself: a region with its name, reached with the keyboard
    const region = page.getByRole('region', { name: `Terminübersicht ${run}` });
    await region.waitFor();
    const r = await region.evaluate((el) => ({ tab: el.tabIndex, overflow: getComputedStyle(el).overflowY, scrolls: el.scrollHeight > el.clientHeight }));
    assert.deepEqual(r, { tab: 0, overflow: 'auto', scrolls: true });
    await region.focus();
    await page.keyboard.press('End');
    await page.waitForFunction((n) => { const el = [...document.querySelectorAll('[role=region]')].find((x) => x.getAttribute('aria-label') === n); return el && el.scrollTop > 0; }, `Terminübersicht ${run}`);
    // the border on the start of the line only, rounded at its end, links underlined wavy
    const b = await page.getByRole('link', { name: 'link to the start' }).evaluate((a) => {
      const frame = a.closest('.ew-bo');
      const cs = getComputedStyle(frame);
      return { start: cs.borderLeftWidth, top: cs.borderTopWidth, rTopEnd: cs.borderTopRightRadius, rTopStart: cs.borderTopLeftRadius, line: getComputedStyle(a).textDecorationLine, style: getComputedStyle(a).textDecorationStyle };
    });
    assert.deepEqual(b, { start: '3px', top: '0px', rTopEnd: '12px', rTopStart: '0px', line: 'underline', style: 'wavy' });
    // sticky: the last block of its area, it fits on the screen
    const sticky = page.getByText(`Stays in view ${run}`);
    await page.waitForFunction((n) => { const f = [...document.querySelectorAll('.ew-bo')].find((x) => x.textContent.includes(n)); return f && getComputedStyle(f).position === 'sticky'; }, `Stays in view ${run}`);
    assert.ok(await sticky.isVisible());
    await page.evaluate(() => window.scrollTo(0, 0));
    await a11y('blocks with a name, scrolling in itself, sides and corners');
    // on the phone it does not stick
    await page.setViewportSize({ width: 390, height: 800 });
    await page.waitForFunction((n) => { const f = [...document.querySelectorAll('.ew-bo')].find((x) => x.textContent.includes(n)); return f && getComputedStyle(f).position !== 'sticky'; }, `Stays in view ${run}`);
  } finally {
    if (size) await page.setViewportSize(size);
  }
});

test('template: the header lies on a hero banner below it, solid again once it is scrolled away', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const tpl = await call('POST', '/api/v1/x/core.template/templates', { names: { en: `Overlay ${run}` }, copy_of: 'core.template/example-cms' });
  const cur = (await call('GET', `/api/v1/x/core.template/templates/${tpl.key}`)).template;
  cur.design.options.header_overlay = true;
  cur.design.options.sticky_header = true;
  await call('PUT', `/api/v1/x/core.template/templates/${tpl.key}`, { version: cur.version, design: cur.design });
  const cat = await call('POST', '/api/v1/categories', { titles: { en: `Overlay ${run}` }, guest_visible: true });
  const blk = await call('POST', '/api/v1/elements', { category_id: cat.id, type: 'core.block', title: `Hero ${run}`,
    body: { format: 'markdown', source: `:::hero{title="Welcome okapi ${run}" background=dark}\nA website on the wiki.\n:::\n` } });
  await call('PUT', `/api/v1/placements/category?target=${cat.id}`, { placements: [{ block: `core.blocks/${blk.id}`, position: 'hero', sort: 1 }] });
  const size = page.viewportSize();
  try {
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, template: `core.template/${tpl.key}` } });
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`${BASE}/c/${cat.id}`);
    await idle();
    const header = page.locator('header.ewd-landmark');
    await page.locator('header.ewd-landmark.ewd-overlay').waitFor();
    await page.evaluate(() => { document.querySelector('.ewd-main').style.minHeight = '4000px'; });
    const look = () => page.evaluate(() => {
      const hd = document.querySelector('header.ewd-landmark');
      const hero = document.querySelector('.ewl-hero');
      const frame = hero.closest('.ew-bo') ?? hero;
      const row = hd.querySelector('.ewd-row:last-child');
      return { bg: getComputedStyle(row).backgroundColor, color: getComputedStyle(row).color, hero: getComputedStyle(hero).color,
        top: Math.round(frame.getBoundingClientRect().top - hd.getBoundingClientRect().top), left: Math.round(frame.getBoundingClientRect().left),
        width: Math.round(frame.getBoundingClientRect().width - hd.getBoundingClientRect().width), solid: hd.classList.contains('ewd-overlay--solid') };
    });
    let l = await look();
    // on the banner: no background of its own, the color of the banner's text, the banner from the top under it over the whole width
    assert.deepEqual([l.bg, l.color, l.top, l.width, l.solid], ['rgba(0, 0, 0, 0)', l.hero, 0, 0, false], JSON.stringify(l));
    await a11y('the header on a hero banner');
    // scrolled away: solid again
    await page.evaluate(() => window.scrollTo(0, 1500));
    await page.waitForFunction(() => document.querySelector('header.ewd-landmark').classList.contains('ewd-overlay--solid'));
    // (the background comes back with a short transition)
    await page.waitForFunction(() => getComputedStyle(document.querySelector('header.ewd-landmark .ewd-row:last-child')).backgroundColor !== 'rgba(0, 0, 0, 0)');
    await a11y('the header after the banner');
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.waitForFunction(() => !document.querySelector('header.ewd-landmark').classList.contains('ewd-overlay--solid'));
    // a page without a banner: the header as it is
    await page.goto(`${BASE}/`);
    await idle();
    await header.waitFor();
    assert.equal(await page.locator('header.ewd-landmark.ewd-overlay').count(), 0);
  } finally {
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
    await call('PUT', `/api/v1/placements/category?target=${cat.id}`, { placements: [] });
    if (size) await page.setViewportSize(size);
  }
});

test('template: the path switched off for a template in the placement (pages and categories), shown again for one category', async () => {
  const before = (await call('GET', '/api/v1/admin/settings')).config;
  const tpl = 'core.template/example-wiki';
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Path ${run}` } })).id;
  const pg = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Path ${run}`, body: { format: 'markdown', source: 'Text.' } });
  try {
    await call('PUT', '/api/v1/admin/settings', { ui: { ...before.ui, template: tpl } });
    await page.goto(`${BASE}/e/${pg.id}`);
    await idle();
    await page.locator('main .ew-breadcrumbs').waitFor();
    await call('PUT', `/api/v1/placements/template?target=${encodeURIComponent(tpl)}`, { placements: [{ block: 'core.element.path', position: '' }, { block: 'core.category.path', position: '' }] });
    await page.goto(`${BASE}/e/${pg.id}`);
    await idle();
    await page.getByRole('heading', { level: 1, name: `Path ${run}` }).waitFor();
    assert.equal(await page.locator('.ew-breadcrumbs').count(), 0, 'no path on a page');
    await page.goto(`${BASE}/c/${cat}`);
    await idle();
    assert.equal(await page.locator('.ew-breadcrumbs').count(), 0, 'no path on a category page');
    // one category shows it again
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [{ block: 'core.element.path', position: 'element-header', sort: 5 }] });
    await page.goto(`${BASE}/e/${pg.id}`);
    await idle();
    await page.locator('.ew-breadcrumbs').waitFor();
  } finally {
    await call('PUT', `/api/v1/placements/template?target=${encodeURIComponent(tpl)}`, { placements: [] });
    await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
    await call('PUT', '/api/v1/admin/settings', { ui: before.ui });
  }
});

test('theme blocks B: own text for "page not found", reading progress', async () => {
  const logged = errors.length;
  const cat = await m6Category();
  const own = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Lost ${run}`, body: { format: 'html', source: `<p>Lost okapi ${run}: try the search.</p>` } });
  // the administration chooses the page in the settings
  await page.goto(`${BASE}/admin/settings`);
  await idle();
  const box = page.getByLabel(/^(own text for “page not found”|eigener text für „seite nicht gefunden“)/i);
  await box.check();
  const form = page.locator('form', { has: box });
  const sub = form.locator('fieldset.ew-subfield', { has: page.locator('legend', { hasText: /^(page with the text for “page not found”|seite mit dem text für „seite nicht gefunden“)$/i }) });
  await sub.locator('input.ew-input[type=search]:not(.ew-picker-filter)').fill(`Lost ${run}`);
  await page.waitForFunction((n) => [...document.querySelectorAll('.ew-picker-list option')].some((o) => o.textContent.includes(n)), `Lost ${run}`);
  await pickOption(sub.locator('.ew-picker-list'), `Lost ${run}`);
  await a11y('settings with an own text for errors');
  const saved = page.waitForResponse((r) => r.url().endsWith('/api/v1/admin/settings') && r.request().method() === 'PUT');
  await form.getByRole('button', { name: /^(save|speichern)$/i }).last().click();
  assert.equal((await saved).status(), 200);
  assert.equal((await call('GET', '/api/v1/admin/settings')).config.ui.not_found_element, own.id);
  await page.goto(`${BASE}/e/99999999`);
  await page.locator('main').getByText(`Lost okapi ${run}: try the search.`).waitFor();
  await a11y('own text for "page not found"');
  const s = (await call('GET', '/api/v1/admin/settings')).config;
  await call('PUT', '/api/v1/admin/settings', { ui: { ...s.ui, not_found_element: 0 } });
  // reading progress placed above the content of a category with a long page
  await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [{ block: 'core.readprogress', position: 'content-top', sort: 1 }] });
  const long = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Long ${run}`, body: { format: 'html', source: Array.from({ length: 80 }, (_, i) => `<p>Paragraph ${i} of a long page.</p>`).join('') } });
  await page.goto(`${BASE}/e/${long.id}`);
  await idle();
  const bar = page.locator('.ew-readprogress');
  await bar.waitFor();
  assert.equal(await bar.getAttribute('aria-hidden'), 'true');
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await page.waitForFunction(() => Number(getComputedStyle(document.querySelector('.ew-readprogress')).getPropertyValue('--ew-read')) > 0.9);
  await call('PUT', `/api/v1/placements/category?target=${cat}`, { placements: [] });
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 404/.test(e)));
});

test('maintenance: read-only with a message, then closed; administrators keep working, guests see why', async () => {
  const logged = errors.length;
  try {
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Maintenance ${run}` } })).id;
    await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'guest', permissions: 1, inherit: false });
    const pg = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Moving ${run}`, body: { format: 'html', source: '<p>Lynx and okapi</p>' } });
    await page.goto(`${BASE}/admin/maintenance`);
    await idle();
    await page.getByLabel(/^(mode|modus)/i).selectOption('read_only');
    await page.getByLabel(/^(message|hinweis) \((english|englisch)\)/i).fill(`Moving the server ${run}.`);
    await a11y('maintenance settings');
    const saved = page.waitForResponse((r) => r.url().endsWith('/api/v1/admin/maintenance') && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    await page.waitForLoadState('load');
    await idle();
    // the administrator: a bar, and changes still work
    await page.getByRole('region', { name: /^(maintenance|wartung)$/i }).getByText(/as an administrator you can still change|als administrator können sie weiterhin/i).waitFor();
    await call('PUT', `/api/v1/elements/${pg.id}`, { lang: 'en', title: `Moving ${run}`, body: { format: 'html', source: '<p>Lynx and okapi, checked</p>' } });
    // a guest reads, sees the message, and cannot change anything
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    const guest = await ctx.newPage();
    await guest.goto(`${BASE}/e/${pg.id}`);
    await guest.getByRole('region', { name: /^(maintenance|wartung)$/i }).getByText(`Moving the server ${run}.`).waitFor();
    await guest.locator('main').getByText('Lynx and okapi, checked').waitFor();
    await a11y('read-only for maintenance (guest)', guest);
    // closed: guests see only the page that says so, with the way to sign in
    const m = await call('GET', '/api/v1/admin/maintenance');
    await call('PUT', '/api/v1/admin/maintenance', { ...m, mode: 'closed' });
    await guest.goto(`${BASE}/e/${pg.id}`);
    await guest.getByRole('heading', { level: 1, name: /^(maintenance|wartung)$/i }).waitFor();
    await guest.getByText(`Moving the server ${run}.`).waitFor();
    assert.equal(await guest.getByText('Lynx and okapi').count(), 0);
    await guest.getByRole('link', { name: /^(sign in \(administrators\)|anmelden \(administration\))$/i }).waitFor();
    await a11y('closed for maintenance (guest)', guest);
    await ctx.close();
    // off again
    await page.goto(`${BASE}/admin/maintenance`);
    await idle();
    await page.getByLabel(/^(mode|modus)/i).selectOption('');
    const off = page.waitForResponse((r) => r.url().endsWith('/api/v1/admin/maintenance') && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await off).status(), 200);
    assert.equal((await call('GET', '/api/v1/admin/maintenance')).mode, '');
  } finally {
    // never leave the wiki in maintenance (the jobs of the later tests would wait)
    await call('PUT', '/api/v1/admin/maintenance', { mode: '' }).catch(() => undefined);
  }
  errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of 503/.test(e)));
});

test('consent: privacy policy accepted before use, cookie banner with categories, proof for the administration', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Legal ${run}` } })).id;
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'guest', permissions: 1, inherit: false });
  const pol = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Privacy ${run}`, body: { format: 'html', source: `<p>We keep okapi data ${run}.</p>` } });
  const other = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Notes ${run}`, body: { format: 'html', source: '<p>Lynx notes</p>' } });
  const name = `cons${run}`.toLowerCase().slice(0, 30);
  const pw = 'Passw0rd!Passw0rd';
  await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Consent ${run}` });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  try {
    // the administration switches it on
    await page.goto(`${BASE}/admin/consent`);
    await idle();
    await page.getByLabel(/^(ask for consent to cookies|nach der zustimmung zu cookies fragen)/i).check();
    await page.getByLabel(/^(consent required|zustimmung zwingend)$/i).first().check();
    await page.getByLabel(/^(signed-in people accept the privacy policy|angemeldete personen stimmen der datenschutzerklärung zu)/i).check();
    const sub = page.locator('fieldset.ew-subfield', { has: page.locator('legend', { hasText: /^(page with the privacy policy|seite mit der datenschutzerklärung)$/i }) });
    await sub.locator('input.ew-input[type=search]:not(.ew-picker-filter)').fill(`Privacy ${run}`);
    await page.waitForFunction((n) => [...document.querySelectorAll('.ew-picker-list option')].some((o) => o.textContent.includes(n)), `Privacy ${run}`);
    await sub.locator('.ew-picker-list').selectOption(String(pol.id));
    await page.getByLabel(/^(guests|gäste)/i).selectOption('required');
    await a11y('consent settings');
    const saved = page.waitForResponse((r) => r.url().endsWith('/api/v1/admin/consent') && r.request().method() === 'PUT');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);

    // a person signs in: first the policy, the wiki only after accepting
    const p2 = await ctx.newPage();
    p2.on('pageerror', (e) => errors.push(`consent user: ${e.message}`));
    await p2.goto(`${BASE}/login`);
    // the cookie banner (required: modal) comes first, also on the sign-in page
    const cookies = p2.getByRole('dialog', { name: /^(cookies and similar storage|cookies und ähnliche speicher)$/i });
    await cookies.waitFor();
    await a11y('cookie banner', p2);
    await p2.keyboard.press('Escape');
    assert.ok(await cookies.isVisible(), 'Escape does not dismiss the required banner');
    await cookies.getByRole('button', { name: /^(only necessary|nur notwendige)$/i }).click();
    await cookies.waitFor({ state: 'detached' });
    await p2.getByLabel(/user ?name|benutzername/i).fill(name);
    await p2.getByLabel(/^(password|passwort)/i).fill(pw);
    await p2.keyboard.press('Enter');
    // signed in: the policy comes before the wiki
    const dlg = p2.getByRole('dialog', { name: /^(privacy policy|datenschutzerklärung)$/i });
    await dlg.getByText(`We keep okapi data ${run}.`).waitFor();
    assert.equal(await dlg.getByRole('button', { name: /^(later|später)/i }).count(), 0, 'only administrators may postpone');
    await a11y('privacy policy to accept', p2);
    await p2.keyboard.press('Escape');
    assert.ok(await dlg.isVisible(), 'Escape does not dismiss the policy');
    await dlg.getByRole('button', { name: /^(accept|zustimmen)$/i }).click();
    await dlg.waitFor({ state: 'detached' });
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    await p2.goto(`${BASE}/e/${other.id}`);
    await p2.locator('main').getByText('Lynx notes').waitFor();
    assert.equal(await p2.evaluate(() => JSON.parse(localStorage.getItem('ewiki.consent.cookies')).cats.length), 0);
    // the profile shows it and opens the choice again, with the categories
    await p2.goto(`${BASE}/profile`);
    await p2.getByText(/you accepted the privacy policy \(version 1\)|sie haben der datenschutzerklärung \(version 1\)/i).waitFor();
    await p2.getByRole('button', { name: /^(cookie settings|cookie-einstellungen)$/i }).click();
    const again = p2.getByRole('region', { name: /^(cookies and similar storage|cookies und ähnliche speicher)$/i });
    await again.getByRole('button', { name: /^(choose|auswählen)$/i }).click();
    await again.getByLabel(/^(external media|externe medien)$/i).check();
    await a11y('cookie settings with categories', p2);
    await again.getByRole('button', { name: /^(save choice|auswahl speichern)$/i }).click();
    assert.equal(await p2.evaluate(() => JSON.parse(localStorage.getItem('ewiki.consent.cookies')).cats.join()), 'external_media');
    await p2.close();

    // a guest: the policy is required as well (kept in the browser)
    const g = await ctx.browser().newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    const gp = await g.newPage();
    await gp.goto(`${BASE}/e/${other.id}`);
    await gp.getByRole('dialog', { name: /^(privacy policy|datenschutzerklärung)$/i }).getByRole('button', { name: /^(accept|zustimmen)$/i }).click();
    await gp.getByRole('dialog', { name: /^(cookies and similar storage|cookies und ähnliche speicher)$/i }).getByRole('button', { name: /^(only necessary|nur notwendige)$/i }).click();
    await gp.locator('main').getByText('Lynx notes').waitFor();
    await g.close();

    // the administration: asked as well, may postpone; the proof lists the person
    await page.goto(`${BASE}/admin/consent`);
    const adlg = page.getByRole('dialog', { name: /^(privacy policy|datenschutzerklärung)$/i });
    await adlg.getByRole('button', { name: /^(later|später)/i }).click();
    const cb = page.getByRole('dialog', { name: /^(cookies and similar storage|cookies und ähnliche speicher)$/i });
    await cb.getByRole('button', { name: /^(accept all|alle akzeptieren)$/i }).click();
    await idle();
    await page.getByRole('table', { name: /^(who accepted the privacy policy|wer der datenschutzerklärung zugestimmt hat)$/i }).getByRole('cell', { name: name, exact: true }).waitFor();
    await page.getByRole('link', { name: /^(download all as csv|alle als csv herunterladen)$/i }).waitFor();
    await a11y('consent proof');
  } finally {
    await ctx.close();
    await call('PUT', '/api/v1/admin/consent', { cookies: { enabled: false }, privacy: { enabled: false } }).catch(() => undefined);
    await page.evaluate(() => localStorage.removeItem('ewiki.consent.cookies'));
  }
});

test('editor: the menu of a block – right click, long press on a touch screen, context menu key; duplicate, move, delete, undo', async () => {
  const cat = await m6Category();
  const src = `:::panel{type=info}\nFirst okapi ${run}\n:::\n\n:::panel{type=warning}\nSecond lynx ${run}\n:::\n\nThe end.\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Menu ${run}`, body: { format: 'markdown', source: src } });
  await page.goto(`${BASE}/e/${el.id}/edit`);
  await idle();
  const blocks = page.locator('.ewe-prose > .ewe-macro');
  await blocks.nth(1).waitFor();
  const order = async () => (await blocks.allInnerTexts()).map((x) => (x.includes('okapi') ? 'okapi' : x.includes('lynx') ? 'lynx' : '?')).join(',');
  const menu = page.getByRole('menu', { name: /^(actions for|aktionen für) /i });
  // right click on the head of a block: the menu, the first entry has the focus
  await blocks.nth(0).locator('.ewe-macro-name').click({ button: 'right' });
  await menu.waitFor();
  assert.match(await page.evaluate(() => document.activeElement?.textContent ?? ''), /^(edit|bearbeiten)$/i);
  await a11y('the menu of a block');
  // arrow keys, then Duplicate
  await page.keyboard.press('ArrowDown');
  assert.match(await page.evaluate(() => document.activeElement?.textContent ?? ''), /^(move up|nach oben)$/i);
  await menu.getByRole('menuitem', { name: /^(duplicate|duplizieren)$/i }).click();
  assert.equal(await menu.count(), 0, 'closed after the choice');
  assert.equal(await order(), 'okapi,okapi,lynx');
  // right click in the text of a block: the browser's own menu, not ours
  await blocks.nth(0).locator('.ewe-macro-body p').first().click({ button: 'right' });
  assert.equal(await menu.count(), 0, 'no block menu in the text');
  await page.keyboard.press('Escape');
  // Escape closes and gives the focus back
  const editBtn = blocks.nth(2).getByRole('button', { name: /^(edit block|baustein bearbeiten)/i });
  await editBtn.focus();
  await page.keyboard.press('Shift+F10');
  await menu.waitFor();
  await page.keyboard.press('Escape');
  assert.equal(await menu.count(), 0);
  assert.ok(await editBtn.evaluate((b) => b === document.activeElement), 'the focus is back on the button');
  // the context menu key on a button of the head: Move up
  await page.keyboard.press('ContextMenu');
  await menu.waitFor();
  await menu.getByRole('menuitem', { name: /^(move up|nach oben)$/i }).click();
  assert.equal(await order(), 'okapi,lynx,okapi');
  // a long press on a touch screen: the menu; Delete, then undo
  const box = await blocks.nth(1).locator('.ewe-macro-name').boundingBox();
  await blocks.nth(1).locator('.ewe-macro-name').dispatchEvent('pointerdown', { pointerType: 'touch', isPrimary: true, clientX: box.x + 4, clientY: box.y + 4, bubbles: true });
  await menu.waitFor();
  await blocks.nth(1).locator('.ewe-macro-name').dispatchEvent('pointerup', { pointerType: 'touch', isPrimary: true, bubbles: true });
  await menu.getByRole('menuitem', { name: /^(delete|löschen)$/i }).click();
  assert.equal(await order(), 'okapi,okapi');
  await page.keyboard.press('Control+z');
  assert.equal(await order(), 'okapi,lynx,okapi');
  // leave without saving
  page.once('dialog', (d) => d.accept());
  await page.goto(`${BASE}/e/${el.id}`);
  const leave = page.locator('dialog[open]').getByRole('button', { name: /^(discard|verwerfen)/i });
  if (await leave.count()) await leave.click();
});

test('editor: blocks move with the mouse, the buttons and Alt+Arrow keys; their width and height with the handles and the keyboard', async () => {
  const cat = await m6Category();
  const src = `:::panel{type=info}\nFirst okapi ${run}\n:::\n\n:::panel{type=warning}\nSecond lynx ${run}\n:::\n\nThe end.\n`;
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Move ${run}`, body: { format: 'markdown', source: src } });
  // a page that starts with a block: no invalid selection when live editing starts
  const warned = [];
  const onWarn = (m) => { if (m.type() === 'warning' && /TextSelection endpoint/.test(m.text())) warned.push(m.text()); };
  page.on('console', onWarn);
  await page.goto(`${BASE}/e/${el.id}/edit`);
  await idle();
  const blocks = page.locator('.ewe-prose > .ewe-macro');
  await blocks.nth(1).waitFor();
  const order = async () => (await blocks.allInnerTexts()).map((x) => (x.includes('okapi') ? 'okapi' : x.includes('lynx') ? 'lynx' : '?')).join(',');
  assert.equal(await order(), 'okapi,lynx');
  // buttons
  await blocks.nth(1).getByRole('button', { name: /^(move up|nach oben)/i }).click();
  assert.equal(await order(), 'lynx,okapi');
  await blocks.nth(0).getByRole('button', { name: /^(move up|nach oben)/i }).click(); // already at the top: nothing
  assert.equal(await order(), 'lynx,okapi');
  // keyboard: Alt+ArrowDown with the cursor in the first block
  await blocks.nth(0).locator('.ewe-macro-body p').first().click();
  await page.keyboard.press('Alt+ArrowDown');
  assert.equal(await order(), 'okapi,lynx');
  // mouse: drag the head of the second block above the first
  const head = blocks.nth(1).locator('.ewe-macro-head');
  await head.dragTo(blocks.nth(0), { targetPosition: { x: 40, y: 4 } });
  assert.equal(await order(), 'lynx,okapi');
  // and onto the text below: after it (moved, never copied)
  await blocks.nth(0).locator('.ewe-macro-head').dragTo(page.locator('.ewe-prose > p').last(), { targetPosition: { x: 20, y: 18 } });
  assert.equal(await order(), 'okapi,lynx');
  await blocks.nth(1).locator('.ewe-macro-head').dragTo(blocks.nth(0), { targetPosition: { x: 40, y: 4 } });
  assert.equal(await order(), 'lynx,okapi');
  // width: keyboard on the handle (a slider), then the mouse
  const size = blocks.nth(0).getByRole('slider', { name: /^(width of|breite von)/i });
  await size.focus();
  await page.keyboard.press('Home');
  assert.match(await size.getAttribute('aria-valuetext'), /^16 rem$/);
  await page.keyboard.press('ArrowRight');
  assert.match(await size.getAttribute('aria-valuetext'), /^24 rem$/);
  await a11y('editor with move buttons and width handle');
  await page.keyboard.press('End');
  assert.match(await size.getAttribute('aria-valuetext'), /^(full width|volle breite)$/i);
  const box = await blocks.nth(0).boundingBox();
  const sb = await size.boundingBox();
  await page.mouse.move(sb.x + sb.width / 2, sb.y + sb.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 32 * 16 + 4, sb.y + sb.height / 2, { steps: 5 });
  await page.mouse.up();
  assert.match(await size.getAttribute('aria-valuetext'), /^32 rem$/);
  // least height: keyboard on the lower handle, then the mouse
  const hsize = blocks.nth(0).getByRole('slider', { name: /^(minimum height of|mindesthöhe von)/i });
  await hsize.focus();
  assert.match(await hsize.getAttribute('aria-valuetext'), /^(automatic|automatisch)$/i);
  await page.keyboard.press('ArrowUp');
  assert.match(await hsize.getAttribute('aria-valuetext'), /^4 rem$/);
  await page.keyboard.press('Home');
  assert.match(await hsize.getAttribute('aria-valuetext'), /^(automatic|automatisch)$/i);
  const hb = await hsize.boundingBox();
  const top = (await blocks.nth(0).boundingBox()).y;
  await page.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2);
  await page.mouse.down();
  await page.mouse.move(hb.x + hb.width / 2, top + 20 * 16 + 4, { steps: 5 });
  await page.mouse.up();
  assert.match(await hsize.getAttribute('aria-valuetext'), /^20 rem$/);
  const bh = (await blocks.nth(0).boundingBox()).height;
  assert.ok(bh >= 20 * 16 - 1, `block is at least 20 rem high (${bh})`);
  await page.getByRole('button', { name: /^(publish|veröffentlichen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  const md = (await call('GET', `/api/v1/elements/${el.id}?edit=1`)).body.source;
  assert.ok(md.indexOf('lynx') < md.indexOf('okapi'), 'saved in the new order');
  assert.match(md, /max_width:32/);
  assert.match(md, /min_height:20/);
  page.off('console', onWarn);
  assert.deepEqual(warned, []);
});

test('menu item pointing to an existing element; new blog from the menu dialog', async () => {
  const cat = await m6Category();
  await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Pickable ${run}`, body: { format: 'html', source: '<p>x</p>' } });
  await page.goto(BASE + '/admin/menus');
  await idle();
  await page.getByRole('button', { name: /^(add menu|menü hinzufügen)$/i }).click();
  const dlg = page.locator('dialog[open]').first();
  await dlg.getByLabel(/^(menu name|menüname)/i).fill(`Picker ${run}`);
  await dlg.locator('select').first().selectOption('sidebar-bottom');
  await dlg.getByRole('button', { name: /^(add item|eintrag hinzufügen)$/i }).click();
  const item = page.locator('dialog[open]').last();
  await item.getByLabel(/^(target|ziel)$/i).selectOption('element');
  // existing elements are listed without choosing a category first
  await item.getByLabel(/^(search element|element suchen)/i).fill(`Pickable ${run}`);
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] .ew-picker-list option')].some((o) => o.textContent.includes(n)), `Pickable ${run}`);
  await pickOption(item.locator('.ew-picker-list'), `Pickable ${run}`);
  await a11y('menu item dialog with element picker');
  // a blog can be created right here: without a source a category is created
  await item.getByRole('button', { name: /^(new element|neues element)$/i }).click();
  const nd = page.locator('dialog[open]').last();
  await nd.getByLabel(/^(element type|elementtyp)/i).selectOption('content.blog');
  await nd.getByLabel(/^(title|titel)/i).fill(`Quickblog ${run}`);
  await a11y('new element dialog');
  await nd.locator('button[type=submit]').click();
  await page.waitForFunction((n) => [...document.querySelectorAll('dialog[open] .ew-picker-list option:checked')].some((o) => o.textContent.includes(n)), `Quickblog ${run}`);
  await item.locator('button[type=submit]').click();
  await dlg.locator('.ewm-edit-title', { hasText: `Quickblog ${run}` }).waitFor();
  await dlg.locator('button[type=submit]').last().click();
  await page.waitForSelector(`.ew-pos--sidebar-bottom a:has-text("Quickblog ${run}")`);
});

test('short URL and the elements administration', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Shorty ${run}`, body: { format: 'html', source: '<p>short</p>' } });
  // record the element requests: if the search ever times out again, the message shows what happened
  const seen = [];
  // with the category filter and, for answers, how many rows came back
  const describe = (u) => { const p = new URL(u).searchParams; return `q=${p.get('q') ?? ''} category=${p.get('category') ?? ''}`; };
  const onReq = (r) => { if (r.url().includes('/api/v1/elements?')) seen.push(`request ${describe(r.url())}`); };
  const onRes = (r) => {
    if (!r.url().includes('/api/v1/elements?')) return;
    const i = seen.push(`response ${describe(r.url())} status=${r.status()}`) - 1;
    r.json().then((j) => { seen[i] += ` total=${j.total} rows=${j.items?.length} ids=${(j.items ?? []).map((x) => x.id).join(',')}`; }).catch(() => undefined);
  };
  page.on('request', onReq);
  page.on('response', onRes);
  try {
    await page.goto(BASE + '/admin/elements');
    await idle();
    await page.getByRole('searchbox', { name: /search element|element suchen/i }).fill(`Shorty ${run}`);
    await page.waitForFunction((n) => document.querySelectorAll('.ew-admin-results tbody tr').length === 1 && document.querySelector('.ew-admin-results tbody')?.textContent?.includes(n), `Shorty ${run}`);
  } catch (e) {
    const state = await page.evaluate(() => ({ url: location.href, rows: document.querySelectorAll('.ew-admin-results tbody tr').length,
      search: [...document.querySelectorAll('input[type=search]')].map((x) => x.value), status: document.querySelector('.ew-admin-panel [role=status]')?.textContent,
      filters: [...document.querySelectorAll('.ew-filters select, .ew-filters input:not([type=search])')].map((x) => `${x.name || x.id}=${x.value}`),
      titles: [...document.querySelectorAll('.ew-admin-results tbody th')].map((x) => x.textContent) })).catch(() => null);
    throw new Error(`${e.message}\npage: ${JSON.stringify(state)}\nrequests: ${seen.join(' | ')}`);
  } finally {
    page.off('request', onReq);
    page.off('response', onRes);
  }
  await a11y('elements administration');
  await page.getByRole('button', { name: new RegExp(`^(properties|eigenschaften): Shorty ${run}`, 'i') }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/^(short url|kurz-url)/i).fill(`Kurz ${run}`);
  await dlg.getByLabel(/^(preview image|vorschaubild)/i).fill('/brand/mark.webp');
  await a11y('element properties');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector(`.ew-admin-results code:has-text("/kurz-${run}")`);
  await page.goto(BASE + `/kurz-${run}`);
  await idle();
  await page.locator('h1', { hasText: `Shorty ${run}` }).waitFor();
  assert.equal(new URL(page.url()).pathname, `/kurz-${run}`);
  assert.ok(el.id > 0);
  // preview image for link previews (set in the properties dialog above)
  const saved = await call('GET', `/api/v1/elements/${el.id}`);
  assert.equal(saved.preview_image, '/brand/mark.webp');
});

test('guest cache administration', async () => {
  await page.goto(BASE + '/admin/cache');
  await idle();
  await page.getByRole('button', { name: /clear cache|cache leeren/i }).waitFor();
  await a11y('cache administration');
});

test('translate the interface: plural forms, filters, import and export', async () => {
  await page.goto(`${BASE}/admin/languages?edit=de&filter=all&q=inbox.new`);
  await idle();
  await page.locator('.ew-tr-table tbody tr').first().waitFor();
  await a11y('translation editor');
  const one = page.locator('.ew-tr-table input').first();
  await one.fill(`{n} neue Nachricht ${run}`);
  // wait for the save itself (navigating away earlier would cancel it)
  await Promise.all([
    page.waitForResponse((r) => r.url().includes('/api/v1/admin/i18n/de') && r.request().method() === 'PUT'),
    page.getByRole('button', { name: /^(save|speichern)$/i }).first().click(),
  ]);
  await idle();
  await page.goto(`${BASE}/admin/languages?edit=de&filter=custom`);
  await idle();
  assert.equal(await page.locator('.ew-tr-table input').first().inputValue(), `{n} neue Nachricht ${run}`);
  // import and export
  await page.locator('details.ew-panel summary').click();
  const href = await page.getByRole('link', { name: /^(export|exportieren)$/i }).getAttribute('href');
  assert.match(href, /\/api\/v1\/admin\/i18n\/de\/export\?format=xliff/);
  await a11y('translation import and export');
  const msgs = await call('GET', '/api/v1/i18n/de');
  assert.equal(msgs['inbox.new_one'], `{n} neue Nachricht ${run}`);
  await call('PUT', '/api/v1/admin/i18n/de', { 'inbox.new_one': '' });
});

test('translate a category', async () => {
  const cat = await m6Category();
  await page.goto(`${BASE}/c/${cat}`);
  await idle();
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(translations|übersetzungen)/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.locator('table').waitFor();
  await a11y('category translations');
  await dlg.getByRole('button', { name: /^(translate|übersetzen):/i }).first().click();
  const tr = page.locator('dialog[open]');
  await tr.getByLabel(/^(title|titel)/i).fill(`M6 übersetzt ${run}`);
  await a11y('translate category dialog');
  await tr.locator('button[type=submit]').click();
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  // the save may still be on its way: poll the API
  let c;
  for (let i = 0; i < 20; i++) {
    c = await call('GET', `/api/v1/categories/${cat}`);
    if (c.category.translations.every((x) => x.status === 'original' || x.status === 'current')) break;
    await page.waitForTimeout(250);
  }
  assert.ok(c.category.translations.every((x) => x.status === 'original' || x.status === 'current'), JSON.stringify(c.category.translations));
});

test('reactions, favorites and page properties', async () => {
  const cat = await m6Category();
  const el = await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `M11 page ${run}`, body: { format: 'html', source: '<p>m11</p>' } });
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  // the address bar shows the readable path
  await page.waitForFunction(() => location.pathname.startsWith('/wiki/'));
  // reactions: picker, keyboard, toggle
  await page.getByRole('button', { name: /^(add reaction|reaktion hinzufügen)/i }).first().click();
  const picker = page.locator('.ewre-picker');
  await picker.waitFor();
  await a11y('reaction picker');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('Enter');
  await page.locator('.ewre-btn[aria-pressed="true"]').first().waitFor();
  // favorites: star in the actions, list in the sidebar
  await page.getByRole('button', { name: /^(☆|★)?\s*(favorite|favorit)$/i }).click();
  await page.locator('.ewfav a', { hasText: `M11 page ${run}` }).waitFor();
  // properties: quick editing in edit mode with a free property
  const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
  if ((await edit.getAttribute('aria-pressed')) !== 'true') { await edit.click(); await idle(); }
  await page.getByRole('button', { name: /^(add properties|eigenschaften hinzufügen)/i }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByRole('button', { name: /^(add property|eigenschaft hinzufügen)$/i }).click();
  const add = page.locator('dialog[open]').last();
  await add.getByLabel(/^(name)/i).fill('Owner');
  await a11y('add a free property');
  await add.locator('button[type=submit]').click();
  await dlg.getByLabel(/^owner/i).fill(`Ada ${run}`);
  await a11y('properties dialog');
  await dlg.locator('button[type=submit]').click();
  await page.locator('.ewprop-list', { hasText: `Ada ${run}` }).waitFor();
  await a11y('page with reactions, favorites and properties');
  // definitions in the category settings: the dialog to define one
  await page.goto(`${BASE}/c/${cat}/settings?tab=properties`);
  await idle();
  await a11y('property definitions');
  await page.getByRole('button', { name: /^(define a property|eigenschaft definieren)$/i }).click();
  const def = page.locator('dialog[open]');
  await def.getByLabel(/^(key|schlüssel)/i).fill(`status_${run}`.toLowerCase().replace(/[^a-z0-9_]/g, '_'));
  await def.getByLabel(/^name/i).first().fill('Status');
  await def.getByLabel(/^(type|typ)$/i).selectOption('status');
  await def.getByRole('button', { name: /^(add value|wert hinzufügen)$/i }).click();
  await def.getByLabel(/^(value|wert)$/i).first().fill('Open');
  await a11y('define a property');
  await def.locator('details summary').click();
  await a11y('define a property: more settings');
  await def.locator('button[type=submit]').click();
  await def.waitFor({ state: 'detached', timeout: 5000 }).catch(async () => { throw new Error(`definition dialog: ${await def.locator('.ew-form-alert').innerText()}`); });
  await page.locator('.ew-table code', { hasText: `status_${run}`.toLowerCase().replace(/[^a-z0-9_]/g, '_') }).waitFor();
  await page.goto(`${BASE}/e/${el.id}`);
  await idle();
  await page.getByRole('button', { name: /^(☆|★)?\s*(favorite|favorit)$/i }).click(); // remove again
});

test('carousel and mega menu', async () => {
  const cat = await m6Category();
  const c = await call('POST', '/api/v1/x/core.carousel/carousels', { name: `News ${run}`, position: 'content-top', autoplay: 5, enabled: true });
  await call('POST', `/api/v1/x/core.carousel/carousels/${c.id}/slides`, { title: `Slide A ${run}`, text: 'First', link: '/search' });
  await call('POST', `/api/v1/x/core.carousel/carousels/${c.id}/slides`, { title: `Slide B ${run}`, text: 'Second' });
  const m = await call('POST', '/api/v1/x/core.menu/menus', { name: `Mega ${run}`, location: 'header-nav', style: 'mega', items: [
    { label: { en: `Mega ${run}`, de: `Mega ${run}` }, type: 'heading', children: [
      { label: { en: 'Column', de: 'Spalte' }, type: 'heading', children: [{ type: 'category', category_id: cat, description: { en: 'Where to start', de: 'Hier anfangen' } }] }] }] });
  await page.goto(BASE + '/');
  await idle();
  const car = page.locator('.ewcar', { hasText: `Slide A ${run}` });
  await car.waitFor();
  await car.getByRole('button', { name: /^(next slide|nächste folie)$/i }).click();
  await car.locator('.ewcar-slide:not([hidden])', { hasText: `Slide B ${run}` }).waitFor();
  await a11y('carousel');
  const btn = page.getByRole('button', { name: `Mega ${run}` });
  await btn.click();
  await page.locator('.ewmm-panel:not([hidden])').waitFor();
  assert.equal(await btn.getAttribute('aria-expanded'), 'true');
  await a11y('mega menu');
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => !document.querySelector('.ewmm-panel:not([hidden])'));
  await call('DELETE', `/api/v1/x/core.menu/menus/${m.id}`);
  await call('DELETE', `/api/v1/x/core.carousel/carousels/${c.id}`);
  // administration pages
  for (const section of ['carousels', 'reactions']) {
    await page.goto(`${BASE}/admin/${section}`);
    await idle();
    await page.waitForSelector('.ew-admin-panel h1');
    await a11y(`admin/${section}`);
  }
});

test('page not found: suggestions', async () => {
  await page.goto(`${BASE}/wiki/nowhere/m11-page-${run}`);
  await idle();
  await page.locator('#ew-nf-sugg').waitFor();
  await a11y('page not found with suggestions');
});

test('switch language', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-lang select').selectOption('de');
  await page.waitForFunction(() => document.documentElement.lang === 'de');
  assert.equal(await page.locator('a.ew-skip').textContent(), 'Zum Inhalt springen');
  await page.getByRole('button', { name: 'Suchen' }).waitFor({ timeout: 5000 }).catch(() => assert.fail('header widgets translated'));
  // focus returns to the language selector once the header has been re-rendered
  await page.waitForFunction(() => document.activeElement?.closest('.ew-lang') !== null, null, { timeout: 5000 })
    .catch(() => assert.fail('focus stays on the language selector'));
});

test('a further site on its own host name: separate content, shared account', async () => {
  const name = `e2e${run}`.replace(/[^a-z0-9]/g, '');
  const host = `${name}.localhost`;
  const other = new URL(BASE);
  other.hostname = host;
  await page.goto(`${BASE}/admin/sites`);
  await idle();
  await page.getByRole('button', { name: /^(New site|Neue Site)$/ }).click();
  const dlg = page.getByRole('dialog');
  await dlg.getByLabel(/^Name/).fill(name);
  await dlg.getByLabel(/^(Title|Titel)/).fill(`E2E ${run}`);
  await dlg.getByLabel(/^(Host names|Hostnamen)/).fill(host);
  await a11y('new site dialog');
  await dlg.getByRole('button', { name: /^(Save|Speichern)$/ }).click();
  await page.getByRole('cell', { name: host }).waitFor();
  // the new site answers on its host name, as a separate wiki (no session yet)
  await page.goto(other.origin + '/login');
  await idle();
  await page.getByLabel(/user ?name|benutzername/i).fill(USER);
  await page.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
  await page.locator('form.ew-login button[type=submit]').click();
  await page.waitForSelector('.ew-user-menu');
  const site = await page.evaluate(async () => (await __e2eFetch('/api/v1/site')).json());
  assert.equal(site.site, name, 'site of the host name');
  // pages of the main site do not exist here
  const hits = async (origin) => page.evaluate(async (u) => (await (await __e2eFetch(u, { credentials: 'include' })).json()).elements?.length ?? 0, `${origin}/api/v1/search?q=quokka${run}`);
  assert.equal(await hits(other.origin), 0, 'content of another site is not visible');
  await a11y('further site');
  // back on the main site: delete it
  await page.goto(`${BASE}/admin/sites`);
  await idle();
  await page.getByRole('button', { name: new RegExp(`^(Delete|Löschen): E2E ${run}$`) }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^(Delete|Löschen)$/ }).click();
  await page.getByRole('cell', { name: host }).waitFor({ state: 'detached' });
});

test('media library: upload with rights options, details, image editor', async () => {
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAgAAAAGCAIAAABxZ0isAAAAXUlEQVR4nAXBMQEAMAgEsVfCjIiKYK6SV3IzIiqCGUVNJBHiiCssEE+skJJITnITJyQv2UQqojjFLVxQvGILyYQ55hobzDNrpCaa09zGDc1rtpGGGM5wBw8Mb9jhA/3OQpEMoO8GAAAAAElFTkSuQmCC', 'base64');
  await page.goto(`${BASE}/media`);
  await idle();
  await a11y('media library');
  await page.locator('.ew-media-toolbar').getByRole('button', { name: /^(Upload|Hochladen)$/ }).click();
  const dlg = page.getByRole('dialog');
  await dlg.locator('input[type=file]').setInputFiles({ name: `pic-${run}.png`, mimeType: 'image/png', buffer: png });
  // first category the user may add files to
  const cat = dlg.locator('select').first();
  const values = await cat.locator('option').evaluateAll((os) => os.map((o) => o.value).filter((v) => v && v !== '0'));
  await cat.selectOption(values[0]);
  await dlg.getByLabel(/^(Title|Titel)/).fill(`Pic ${run}`);
  await dlg.getByLabel(/^(Alternative text|Alternativtext)/).fill('Coloured test pattern');
  await dlg.getByLabel(/^(Watermark|Wasserzeichen)$/).selectOption('viewer_view');
  await a11y('upload dialog');
  await dlg.locator('form').getByRole('button', { name: /^(Upload|Hochladen)$/ }).click();
  const tile = page.getByRole('button', { name: new RegExp(`^Pic ${run}`) });
  await tile.waitFor();
  await tile.click();
  const details = page.getByRole('dialog', { name: `Pic ${run}` });
  await details.getByRole('button', { name: /^(Edit image|Bild bearbeiten)$/ }).waitFor();
  await a11y('media details');
  await details.getByRole('button', { name: /^(Edit image|Bild bearbeiten)$/ }).click();
  const editor = page.getByRole('dialog', { name: /(Edit image|Bild bearbeiten):/ });
  await editor.getByRole('button', { name: /^(Rotate right|Nach rechts drehen)$/ }).click();
  await a11y('image editor');
  await editor.getByRole('button', { name: /^(Save as new version|Als neue Version speichern)$/ }).click();
  await editor.waitFor({ state: 'detached' });
  // version 2, now 6×8
  await details.getByText(/6×8/).first().waitFor();
  const inner = () => page.getByRole('dialog').last();
  // texts
  await details.getByRole('button', { name: /^(edit texts|texte bearbeiten)$/i }).click();
  await inner().getByLabel(/^(title|titel)/i).fill(`Pic renamed ${run}`);
  await inner().getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.locator('.ew-toast', { hasText: /^(saved\.|gespeichert\.)$/i }).first().waitFor();
  // the dialog is named after the new title (heading and accessible name)
  const det = page.getByRole('dialog', { name: `Pic renamed ${run}` });
  await det.waitFor();
  // version 1 back (as a new version): 8×6 again
  await det.getByRole('button', { name: /^(restore|wiederherstellen)$/i }).last().click();
  await page.locator('.ew-toast', { hasText: /version restored|version wiederhergestellt/i }).first().waitFor();
  await det.getByText(/8×6/).first().waitFor();
  // a new version uploaded
  await det.getByRole('button', { name: /^(upload a new version|neue version hochladen)$/i }).click();
  await inner().locator('input[type=file]').setInputFiles({ name: `pic-${run}.png`, mimeType: 'image/png', buffer: png });
  await inner().locator('form').getByRole('button', { name: /^(upload|hochladen)$/i }).click();
  await page.locator('.ew-toast', { hasText: /saved as version 4|als version 4 gespeichert/i }).first().waitFor();
  // own rights for this file, then shown again in the settings
  await det.getByRole('button', { name: /^(rights and protection|rechte und schutz)$/i }).click();
  await inner().getByRole('checkbox', { name: /^(own rights for this file|eigene rechte für diese datei)/i }).check();
  await inner().getByRole('button', { name: /^(save|speichern)$/i }).click();
  await det.getByRole('button', { name: /^(rights and protection|rechte und schutz)$/i }).click();
  await inner().getByRole('checkbox', { name: /^(own rights for this file|eigene rechte für diese datei)/i }).waitFor();
  assert.equal(await inner().getByRole('checkbox', { name: /^(own rights for this file|eigene rechte für diese datei)/i }).isChecked(), true);
  await a11y('media rights');
  await inner().getByRole('button', { name: /^(cancel|abbrechen)$/i }).click();
  // make an area unreadable, then delete the older versions that still show it
  await det.getByRole('button', { name: /^(Edit image|Bild bearbeiten)$/ }).click();
  const ed2 = page.getByRole('dialog', { name: /(Edit image|Bild bearbeiten):/ });
  await ed2.getByLabel('X', { exact: true }).fill('0');
  await ed2.getByLabel('Y', { exact: true }).fill('0');
  await ed2.getByLabel(/^(Width|Breite)$/).fill('4');
  await ed2.getByLabel(/^(Height|Höhe)$/).fill('3');
  await ed2.getByLabel(/^(How|Wie)$/).selectOption('fill');
  await ed2.getByRole('button', { name: /^(Make the area unreadable|Bereich unkenntlich machen)$/ }).click();
  await ed2.getByRole('listitem').filter({ hasText: /^(Make unreadable|Unkenntlich machen)$/ }).waitFor();
  await a11y('image editor: make unreadable');
  await ed2.getByRole('button', { name: /^(Save as new version|Als neue Version speichern)$/ }).click();
  // right after saving: delete the older versions (they still show the original)
  const ask = page.getByRole('dialog').last();
  await ask.getByText(/still show what you made unreadable|zeigen noch, was Sie unkenntlich gemacht haben/).waitFor();
  await ask.getByRole('button', { name: /^(Delete older versions|Ältere Versionen löschen)$/ }).click();
  await page.locator('.ew-toast', { hasText: /^(4 older versions deleted\.|4 ältere Versionen gelöscht\.)/ }).first().waitFor();
  const det2 = page.getByRole('dialog', { name: `Pic renamed ${run}` });
  await det2.getByRole('button', { name: /^(restore|wiederherstellen)$/i }).first().waitFor({ state: 'detached' });
  assert.equal(await det2.getByRole('button', { name: /^(Delete older versions|Ältere Versionen löschen)$/ }).count(), 0);
  // download
  const dl = page.waitForEvent('download');
  await det2.getByRole('link', { name: /^(download|herunterladen)$/i }).click();
  assert.equal(readFileSync(await (await dl).path()).subarray(1, 4).toString(), 'PNG');
  // delete
  await det2.getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await page.getByRole('dialog').last().getByRole('button', { name: /^(delete|löschen)$/i }).click();
  await page.locator('.ew-toast', { hasText: /file deleted|datei gelöscht/i }).first().waitFor();
  await page.getByRole('button', { name: new RegExp(`^Pic renamed ${run}`) }).waitFor({ state: 'detached' });
});

test('office programs: default viewer setting, refusal without a configured server', async () => {
  await page.goto(`${BASE}/admin/settings`);
  await idle();
  const viewer = page.getByLabel(/^(Default viewer for files|Standard-Anzeige für Dateien)/);
  await viewer.waitFor();
  const options = await viewer.locator('option').evaluateAll((os) => os.map((o) => o.value));
  assert.ok(options.includes('internal') && options.includes('office.collabora'), `viewer options: ${options}`);
  // "always open externally" needs an office program as default
  const ext = page.getByLabel(/^(Always open files directly|Dateien immer direkt)/);
  assert.equal(await ext.isDisabled(), true);
  await viewer.selectOption('office.collabora');
  assert.equal(await ext.isDisabled(), false);
  await viewer.selectOption('internal');
  await a11y('settings with media');
  // a document; Collabora is active but has no server: the office page refuses with a message
  const id = await page.evaluate(async (name) => {
    const cats = await (await __e2eFetch('/api/v1/categories')).json();
    const fd = new FormData();
    fd.append('meta', JSON.stringify({ category_id: cats[0].id, title: name }));
    fd.append('file', new File(['PK not really a docx'], `${name}.docx`));
    const r = await __e2eFetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
    return (await r.json()).id;
  }, `doc-${run}`);
  assert.ok(id > 0);
  await page.goto(`${BASE}/office/${id}?viewer=office.collabora`);
  await idle();
  await page.getByRole('alert').filter({ hasText: /./ }).first().waitFor();
  await a11y('office page refusal');
  await page.getByRole('button', { name: /^(Back|Zurück)$/ }).click();
});

test('demo content: create, browse accessibly, remove', async () => {
  await page.goto(`${BASE}/admin/demo`);
  await idle();
  await a11y('demo administration');
  await page.getByRole('button', { name: /^(Create demo|Demo anlegen)$/ }).click();
  await page.waitForURL(/\/demo$/, { timeout: 60000 });
  await idle();
  // the first page of the demo: the two areas
  await page.locator('main .ewl-iconbox a[href^="/c/"]').nth(1).waitFor();
  await a11y('demo home page');
  // the start page of the site is now the demo's page for visitors: what
  // the wiki can do, no path, the way into the tour
  await page.goto(`${BASE}/`);
  await idle();
  await page.locator('.ewl-iconbox').nth(11).waitFor();
  assert.equal(await page.locator('.ew-breadcrumbs').count(), 0);
  assert.equal(await page.locator('main h1').count(), 1);
  await a11y('demo start page');
  // … on the phone too
  const wide = page.viewportSize();
  await page.setViewportSize({ width: 390, height: 844 });
  try {
    await idle();
    await a11y('demo start page on the phone');
  } finally {
    await page.setViewportSize(wide);
  }
  // the first button leads into the example company
  await page.locator('main a.ewl-cta[href^="/c/"]').first().click();
  await page.waitForURL(/\/c\/\d+$/);
  await idle();
  const company = new URL(page.url()).pathname;
  await a11y('demo company');
  // the menu of the demo
  for (const name of [/^(Tour|Rundgang)$/, /^(News|Neuigkeiten)$/]) {
    await page.goto(`${BASE}/demo`);
    await idle();
    await page.getByRole('navigation').getByRole('link', { name }).first().click();
    await idle();
    await a11y(`demo ${name}`);
  }
  // as administrator, every page of the demo opens without an error, is
  // accessible, and its editor opens (no "access denied" anywhere)
  const failed = [];
  const seen = new Set();
  const queue = ['/demo'];
  const editors = new Set();
  const inDemo = (p) => /^\/(demo|wiki\/demo)(\/|$|\?)|^\/(e|c)\/\d+$/.test(p);
  const errorTitle = /^(access denied|not found|something went wrong|zugriff verweigert|nicht gefunden|etwas ist schiefgelaufen)/i;
  while (queue.length && seen.size < 130) {
    const p = queue.shift();
    if (seen.has(p)) continue;
    seen.add(p);
    await page.goto(BASE + p);
    await idle();
    const h1 = (await page.locator('main h1').first().textContent().catch(() => '')) ?? '';
    if (errorTitle.test(h1.trim())) { failed.push(`${p}: ${h1}`); continue; }
    await a11y(`demo page ${p}`);
    const links = await page.$$eval('main a[href^="/"], header nav a[href^="/"]', (as) => as.map((a) => a.getAttribute('href') ?? ''));
    for (const l of links) {
      const path = l.split('#')[0];
      if (/\/edit(\?|$)/.test(path)) editors.add(path);
      else if (inDemo(path) && !seen.has(path)) queue.push(path);
    }
  }
  assert.ok(seen.size > 60, `demo pages visited: ${seen.size}`);
  // the property database always (the crawl above stops after 130 pages, in tree order)
  const db = (await call('GET', '/api/v1/elements?type=content.propdb&limit=5')).items[0];
  assert.ok(db, 'the demo has a property database');
  await page.goto(`${BASE}/e/${db.id}`);
  await idle();
  await a11y('demo property database');
  for (const p of [...editors].slice(0, 25)) {
    await page.goto(BASE + p);
    await idle();
    const h1 = (await page.locator('main h1').first().textContent().catch(() => '')) ?? '';
    if (errorTitle.test(h1.trim())) failed.push(`${p}: ${h1}`);
  }
  assert.deepEqual(failed, [], 'demo pages with errors');
  // the same pages in the dark colour scheme (template "Wiki (dark)", and the
  // choice "dark" for the categories with their own template, e.g. the CMS
  // ones): every component takes its colours from the variables, axe checks
  // the contrast
  const uiBefore = (await call('GET', '/api/v1/admin/settings')).config.ui;
  await call('PUT', '/api/v1/admin/settings', { ui: { ...uiBefore, template: 'theme.wiki.dark' } });
  await call('PUT', '/api/v1/scheme', { scheme: 'dark' });
  try {
    let dark = 0;
    let cms = 0;
    // (the start page of the site has the website template: a CMS page)
    for (const p of [...seen, '/', `/e/${db.id}`, ...[...editors].slice(0, 3)]) {
      await page.goto(BASE + p);
      await idle();
      // a template of the designer has light colours only; those pages stay light
      if (!(await page.evaluate(() => document.body.classList.contains('ewc-dark')))) continue;
      dark++;
      if (await page.locator('.ewcms-app').count()) cms++;
      await a11y(`dark: demo page ${p}`);
    }
    assert.ok(dark > 10, `demo pages checked in the dark scheme: ${dark}`);
    assert.ok(cms > 0, 'the CMS pages of the demo are checked in the dark scheme too');
  } finally {
    await call('PUT', '/api/v1/scheme', { scheme: '' });
    await call('PUT', '/api/v1/admin/settings', { ui: uiBefore });
  }
  // the carousel of the demo is placed for the start page of the company
  // only; elsewhere (the demo home, the tour) there is none
  await page.goto(`${BASE}/demo`);
  await idle();
  assert.equal(await page.locator('.ew-pos--content-top .ewcar-pos').count(), 0, 'no carousel on the tour');
  // one arrow per toggle in the tree of the side column
  const arrow = page.locator('.ew-sidebar-tree .ew-tree-toggle, aside .ew-tree-toggle').first();
  if (await arrow.count()) {
    assert.equal(await arrow.evaluate((b) => getComputedStyle(b, '::before').content), 'none', 'a single arrow in the tree');
  }
  await page.goto(BASE + company);
  await idle();
  await page.locator('.ew-pos--content-top .ewcar-pos').waitFor();
  await a11y('demo carousel');
  // the tour: from the first station to the last with "Next", each with
  // "Previous" (but the first), the station "Menus, placement and carousel"
  // explains where the carousel is
  await page.goto(`${BASE}/demo`);
  await idle();
  await page.locator('main .ewl-iconbox a[href^="/c/"]').nth(1).click();
  await idle();
  let stations = 1;
  let explained = false;
  assert.equal(await page.locator('.ew-pagenav-prev').count(), 0, 'the first station has no previous one');
  for (;;) {
    if (await page.locator('main').getByText(/content-top/).count()) explained = true;
    const next = page.locator('.ew-pagenav-next');
    if (!(await next.count())) break;
    await next.click();
    await idle();
    stations++;
    assert.equal(await page.locator('.ew-pagenav-prev').count(), 1, `station ${stations} has a previous one`);
    assert.ok(stations < 60, 'the tour ends');
  }
  assert.ok(stations > 20, `stations walked through: ${stations}`);
  assert.ok(explained, 'a station explains where the carousel is');
  await a11y('demo last station');
  await page.goto(`${BASE}/admin/demo`);
  await idle();
  await page.getByRole('button', { name: /^(Remove demo|Demo entfernen)$/ }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^(Remove demo|Demo entfernen)$/ }).click();
  await page.getByRole('button', { name: /^(Create demo|Demo anlegen)$/ }).waitFor();
  const res = await page.evaluate(async () => (await __e2eFetch('/api/v1/resolve?path=/demo')).status);
  assert.equal(res, 404);
});

test('light and dark: block, profile, guest cookie, no flash', async () => {
  await page.goto(BASE + '/');
  await idle();
  const before = (await call('GET', '/api/v1/placements/site')).placements;
  const cat = await call('POST', '/api/v1/categories', { titles: { en: `Schemes ${run}` } });
  await call('POST', `/api/v1/categories/${cat.id}/acl`, { subject_type: 'guest', permissions: 1, inherit: false });
  const el = await call('POST', '/api/v1/elements', { category_id: cat.id, type: 'core.html', title: `Scheme page ${run}`, body: { format: 'markdown', source: 'Light and dark.\n' } });
  await call('PUT', '/api/v1/placements/site', { placements: [...before, { block: 'core.scheme', position: 'footer', sort: 99 }] });
  const group = (pg) => pg.getByRole('group', { name: /^(colour scheme|farbschema)$/i });
  const shown = (pg) => pg.evaluate(() => [document.documentElement.dataset.shown, document.body.classList.contains('ewc-dark')]);
  try {
    // a person: the default of the template (as the device is set; the browser is light)
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const g = group(page);
    assert.equal(await g.getByRole('button', { name: /^auto$/i }).getAttribute('aria-pressed'), 'true', 'default: as the system');
    assert.deepEqual(await shown(page), ['light', false]);
    await g.getByRole('button', { name: /^(dark|dunkel)$/i }).click();
    await page.waitForFunction(() => document.body.classList.contains('ewc-dark'));
    assert.equal(await g.getByRole('button', { name: /^(dark|dunkel)$/i }).getAttribute('aria-pressed'), 'true');
    assert.equal((await call('GET', '/api/v1/me/prefs')).prefs.scheme, 'dark', 'kept in the personal settings');
    await a11y('scheme block, dark');
    // the page the server sends is already dark (no light flash while it loads)
    const html = await page.evaluate(async () => (await __e2eFetch('/')).text());
    assert.match(html, /<html[^>]* data-scheme="dark"/);
    await page.reload();
    await idle();
    assert.deepEqual(await shown(page), ['dark', true], 'dark after reloading');
    // the profile: back to the default of the template
    await page.goto(`${BASE}/profile`);
    await idle();
    const sel = page.getByRole('combobox', { name: /^(colour scheme|farbschema)$/i });
    assert.equal(await sel.inputValue(), 'dark');
    await a11y('profile with the colour scheme, dark');
    await sel.selectOption('');
    await page.getByRole('button', { name: /^(save|speichern)$/i }).first().click();
    await page.locator('.ew-toast', { hasText: /^(saved|gespeichert)/i }).first().waitFor();
    assert.deepEqual(await shown(page), ['light', false], 'the default again');
    assert.equal((await call('GET', '/api/v1/me/prefs')).prefs.scheme, undefined);

    // a guest whose device is dark: dark by default; the choice is kept in a cookie
    const guest = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'dark', bypassCSP: true });
    try {
      const gp = await guest.newPage();
      await gp.goto(`${BASE}/e/${el.id}`);
      await group(gp).waitFor();
      assert.deepEqual(await shown(gp), ['dark', true], 'guest: as the device is set');
      await a11y('guest page, dark device', gp);
      await group(gp).getByRole('button', { name: /^(light|hell)$/i }).click();
      await gp.waitForFunction(() => !document.body.classList.contains('ewc-dark'));
      const cookie = (await guest.cookies()).find((c) => c.name === 'ewiki_scheme');
      assert.ok(cookie && cookie.value === 'light' && cookie.httpOnly && cookie.sameSite === 'Lax', `guest cookie: ${JSON.stringify(cookie)}`);
      await gp.reload();
      await group(gp).waitFor();
      assert.deepEqual(await shown(gp), ['light', false], 'guest: light after reloading');
      assert.equal(await gp.evaluate(() => document.documentElement.dataset.schemeChoice), 'light');
    } finally {
      await guest.close();
    }
  } finally {
    await call('PUT', '/api/v1/scheme', { scheme: '' });
    await call('PUT', '/api/v1/placements/site', { placements: before });
  }
});

test('marks of the product: favicon, the logo in the header, powered by, no seal without a license', async () => {
  await page.goto(BASE + '/');
  await idle();
  for (const [p, type] of [['/favicon.ico', /icon/], ['/apple-touch-icon.png', /png/], ['/brand/mark.webp', /webp/], ['/brand/seal.webp', /webp/], ['/brand/logo-ink.webp', /webp/]]) {
    const r = await page.request.get(BASE + p);
    assert.equal(r.status(), 200, p);
    assert.match(r.headers()['content-type'] ?? '', type, p);
    assert.match(r.headers()['cache-control'] ?? '', /max-age=86400/, p);
  }
  assert.equal(await page.locator('link[rel="icon"]').getAttribute('href'), '/favicon.ico');
  // the logo in the header (no own logo set): named "Enterprise Wiki", or
  // decorative with the name of the site next to it
  const brandLink = page.locator('header a[href="/"]:has(img[src="/brand/logo.webp"])').first();
  const logo = brandLink.locator('img');
  assert.ok(await logo.evaluate((i) => i.complete && i.naturalWidth > 0), 'the logo is loaded');
  const siteName = (await call('GET', '/api/v1/site')).name;
  const linkName = siteName === 'Enterprise Wiki' ? await logo.getAttribute('alt') : await brandLink.innerText();
  assert.equal(linkName.trim(), siteName, 'the link to the start page is named after the site');
  assert.equal(await logo.getAttribute('alt'), siteName === 'Enterprise Wiki' ? 'Enterprise Wiki' : '');
  // powered by: the "e" and the text; the seal only with a valid license (none here)
  const powered = page.locator('.ew-powered').first();
  assert.match(await powered.innerText(), /enterprise wiki/i);
  assert.equal(await powered.locator('img[src="/brand/mark.webp"]').count(), 1);
  assert.equal(await page.locator('img[src="/brand/seal.webp"]').count(), 0, 'no seal without a license');
  assert.equal((await call('GET', '/api/v1/site')).licensed, false);
  // on paper: the grey logo (with the name of the site); on screen hidden
  const pb = page.locator('.ew-print-brand');
  assert.equal(await pb.isVisible(), false);
  await page.emulateMedia({ media: 'print' });
  try {
    assert.equal(await pb.isVisible(), true, 'printed brand');
    assert.equal(await pb.locator('img').getAttribute('src'), '/brand/logo-ink.webp');
  } finally {
    await page.emulateMedia({ media: 'screen' });
  }
  // administration: the active item carries the "e"; no seal without a license
  await page.goto(BASE + '/admin/license');
  await idle();
  const cur = page.locator('.ew-subnav a[aria-current="page"]');
  assert.match(await cur.evaluate((a) => getComputedStyle(a).backgroundImage), /brand\/mark\.webp/);
  assert.equal(await page.locator('.ew-admin-seal, .ew-license-seal').count(), 0);
  await a11y('administration with the marks');
});

test('spaces: settings, own navigation, /s/KEY, directory, archive', async () => {
  await page.goto(BASE + '/');
  await idle();
  const key = `S${run.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 10)}`;
  const top = await call('POST', '/api/v1/categories', { titles: { en: `Space ${run}`, de: `Bereich ${run}` } });
  await call('POST', '/api/v1/categories', { parent_id: top.id, titles: { en: 'Inside', de: 'Innen' } });
  await page.goto(`${BASE}/c/${top.id}/settings?tab=space`);
  await idle();
  await page.getByLabel(/^(space key|bereichsschlüssel)/i).fill(key);
  await page.getByRole('button', { name: /^(save|speichern)$/i }).first().click();
  await page.locator('.ew-toast', { hasText: /^(saved\.|gespeichert\.)$/i }).first().waitFor();
  await a11y('space settings');
  // appearance and content (every category): own navigation with a shortcut
  await page.goto(`${BASE}/c/${top.id}/settings?tab=appearance`);
  await idle();
  await page.getByLabel(/^(own navigation|eigene navigation)/i).check();
  await page.getByRole('button', { name: /^(add shortcut|verknüpfung hinzufügen)$/i }).click();
  await page.getByLabel(/^(label of shortcut 1|bezeichnung der verknüpfung 1)$/i).fill('Search');
  await page.getByLabel(/^(address of shortcut 1|adresse der verknüpfung 1)$/i).fill('/search');
  await page.evaluate(() => document.querySelectorAll('.ew-toast').forEach((e) => e.remove()));
  await page.getByRole('button', { name: /^(save|speichern)$/i }).first().click();
  await page.locator('.ew-toast', { hasText: /^(saved\.|gespeichert\.)$/i }).first().waitFor();
  await a11y('appearance and content of a category');
  assert.equal((await call('GET', `/api/v1/categories/${top.id}/settings`)).effective.own_tree, true, 'own navigation saved');
  await page.goto(`${BASE}/s/${key}`);
  await idle();
  assert.match(page.url(), new RegExp(`/(c/${top.id}|wiki/)`));
  const nav = page.locator('.ew-space-nav');
  await nav.waitFor();
  assert.ok(await nav.getByRole('link', { name: 'Search' }).count(), 'shortcut in the space navigation');
  assert.ok(await nav.getByRole('link', { name: /^(.*Inside|.*Innen)$/ }).count(), 'subcategory in the space navigation');
  await page.goto(BASE + '/spaces');
  await idle();
  await page.getByLabel(/^(filter spaces|bereiche filtern)/i).fill(key);
  assert.equal(await page.locator('.ew-space-card').count(), 1);
  await a11y('space directory');
  // archive and restore
  await page.goto(`${BASE}/c/${top.id}/settings`);
  await idle();
  await page.getByRole('button', { name: /^(archive|archivieren)$/i }).click();
  await page.locator('dialog[open]').getByRole('button', { name: /^(archive|archivieren)$/i }).click();
  await page.getByRole('button', { name: /^(restore|wiederherstellen)$/i }).waitFor();
  await page.goto(`${BASE}/c/${top.id}`);
  await idle();
  await page.locator('.ew-archived').waitFor();
  await a11y('archived space');
  await call('POST', `/api/v1/categories/${top.id}/archive`, { archived: false });
});

test('webhooks: create with a signing key, signed test delivery, deliveries, send again, edit, delete', async () => {
  // a receiver of the test: internal addresses need webhooks.allow_private,
  // set on the server for the browser tests (ci.yml); the wiki refuses them otherwise
  const got = [];
  const server = http.createServer((req, res) => {
    let body = '';
    req.on('data', (c) => { body += c; });
    req.on('end', () => { got.push({ headers: req.headers, body }); res.writeHead(204).end(); });
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const target = `http://127.0.0.1:${server.address().port}/hook`;
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const dialog = () => page.getByRole('dialog');
  const name = `Hook ${run}`;
  const delivered = async (n) => {
    const deadline = Date.now() + 20000;
    while (got.length < n && Date.now() < deadline) await page.waitForTimeout(100);
    assert.ok(got.length >= n, `delivery ${n} arrived`);
    return got[n - 1];
  };
  try {
    await page.goto(`${BASE}/admin/webhooks`);
    await idle();
    await a11y('webhooks');
    await page.getByRole('button', { name: /^(new webhook|neuer webhook)$/i }).click();
    await dialog().getByLabel(/^name/i).fill(name);
    await dialog().getByLabel(/^(address \(url\)|adresse \(url\))/i).fill(target);
    // a signing key is offered for the JSON format
    await dialog().getByLabel(/^format/i).selectOption('json');
    await dialog().getByLabel(/^(signing key|signaturschlüssel)/i).selectOption('generate');
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
    await toastOn(/webhook saved|webhook gespeichert/i);
    // the key is shown once
    const shown = await page.getByRole('dialog').locator('code').innerText();
    const key = /([0-9a-f]{32,})\s*$/.exec(shown)?.[1];
    assert.ok(key, `signing key shown once: ${shown}`);
    await page.getByRole('dialog').getByRole('button', { name: /^(close|schließen)$/i }).first().click();
    await page.getByRole('rowheader', { name: new RegExp(`^${name}\\b`) }).waitFor();
    assert.equal((await page.content()).includes(key), false, 'the key is not shown again');

    // test delivery, signed with the key
    await page.getByRole('button', { name: new RegExp(`^(send test|test senden): ${name}$`, 'i') }).click();
    await toastOn(/sending a test message|testnachricht wird gesendet/i);
    const first = await delivered(1);
    const sig = createHmac('sha256', key).update(first.body).digest('hex');
    assert.equal(first.headers['x-ewiki-signature'], `sha256=${sig}`, 'signature of the body');
    assert.ok(first.headers['x-ewiki-delivery'], 'delivery id');

    // the deliveries, one sent again
    await page.waitForTimeout(500);
    await page.getByRole('button', { name: new RegExp(`^(deliveries|zustellungen): ${name}$`, 'i') }).click();
    const retry = dialog().getByRole('button', { name: /^(send again|erneut senden)/i }).first();
    await retry.waitFor();
    await a11y('webhook deliveries');
    await retry.click();
    await toastOn(/sending again|wird erneut gesendet/i);
    const again = await delivered(2);
    assert.equal(again.body, first.body, 'the same message again');

    // edit: no longer active
    await page.goto(`${BASE}/admin/webhooks`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): ${name}$`, 'i') }).click();
    await dialog().getByRole('checkbox', { name: /^(active|aktiv)$/i }).uncheck();
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
    await toastOn(/webhook saved|webhook gespeichert/i);
    const hooks = (await call('GET', '/api/v1/admin/webhooks')).webhooks;
    assert.equal(hooks.find((h) => h.name === name).enabled, false);

    // account events for webhooks with all events: switched on and off again
    const accounts = page.getByRole('checkbox', { name: /also send changes of accounts|änderungen an konten auch/i });
    const was = await accounts.isChecked();
    await accounts.setChecked(!was);
    await toastOn(/^(saved\.|gespeichert\.)$/i);
    await page.locator('.ew-toast').evaluateAll((els) => els.forEach((e) => e.remove()));
    await accounts.setChecked(was);
    await toastOn(/^(saved\.|gespeichert\.)$/i);

    // delete
    await page.getByRole('button', { name: new RegExp(`^(delete|löschen): ${name}$`, 'i') }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await toastOn(/webhook deleted|webhook gelöscht/i);
    await page.getByRole('rowheader', { name: new RegExp(`^${name}\\b`) }).waitFor({ state: 'detached' });
  } finally {
    server.close();
  }
});

test('expert settings: a limit, the limit of an API key, secrets stored again; cache; trash restore and delete permanently', async () => {
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const key = await call('POST', '/api/v1/me/api-keys', { name: `Limited ${run}`, permissions: 1, allow_admin: false, category_id: null,
    expires_at: new Date(Date.now() + 864e5).toISOString(), rate_per_second: null });
  try {
    await page.goto(`${BASE}/admin/limits`);
    await idle();
    // a limit: changed, then back to the default
    const days = page.getByLabel(/^(period of the text search|zeitraum der textsuche)/i);
    const before = Number(await days.inputValue());
    await days.fill(String(before + 1));
    await page.locator('form.ew-form').getByRole('button', { name: /^(save|speichern)$/i }).last().click();
    await toastOn(/limits saved|grenzwerte gespeichert/i);
    const value = async () => (await call('GET', '/api/v1/admin/limits')).limits.find((l) => l.key === 'core.audit.search_days').value;
    assert.equal(await value(), before + 1);
    await idle();
    await page.getByRole('button', { name: /^(set the default|vorgabe einsetzen): (period of the text search|zeitraum der textsuche)$/i }).click();
    // the toast of the first save may still be there: remove it, so that the next one belongs to this save
    await page.evaluate(() => document.querySelectorAll('.ew-toast').forEach((e) => e.remove()));
    await page.locator('form.ew-form').getByRole('button', { name: /^(save|speichern)$/i }).last().click();
    await toastOn(/limits saved|grenzwerte gespeichert/i);
    assert.equal(await value(), before);

    // the own limit of an API key, set by the administration (on the page as
    // saved: saving the limits draws the page anew)
    await page.goto(`${BASE}/admin/limits`);
    await idle();
    await page.locator('.ew-toast').evaluateAll((els) => els.forEach((e) => e.remove()));
    const label = new RegExp(`Limited ${run}`);
    await page.getByRole('spinbutton', { name: label }).fill('3');
    await page.getByRole('button', { name: new RegExp(`^(save|speichern): .*Limited ${run}`, 'i') }).click();
    await toastOn(/^(limit saved\.|grenze gespeichert\.)$/i);
    assert.equal((await call('GET', '/api/v1/admin/api-keys')).find((k) => k.id === key.id).rate_per_second, 3);

    // secrets in the database stored again in the current mode
    await page.getByRole('button', { name: /^(store all values in the current mode|alle werte im aktuellen modus speichern)$/i }).click();
    await page.getByRole('status').filter({ hasText: /values stored again|werte neu gespeichert/i }).waitFor();
    await a11y('expert settings after saving');

    // cache
    await page.goto(`${BASE}/admin/cache`);
    await idle();
    await page.getByRole('button', { name: /^(clear cache|cache leeren)$/i }).click();
    await toastOn(/cache cleared|cache geleert/i);

    // trash: one page restored, one deleted for good
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Trash ${run}` } })).id;
    const back = (await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Back ${run}`, body: { format: 'markdown', source: 'x' } })).id;
    const gone = (await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `Gone ${run}`, body: { format: 'markdown', source: 'x' } })).id;
    await call('DELETE', `/api/v1/elements/${back}`);
    await call('DELETE', `/api/v1/elements/${gone}`);
    await page.goto(`${BASE}/trash`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(restore Back ${run}|Back ${run} wiederherstellen)$`, 'i') }).click();
    await toastOn(/^(restored\.|wiederhergestellt\.)$/i);
    assert.equal((await call('GET', `/api/v1/elements/${back}`)).title, `Back ${run}`);
    await page.getByRole('button', { name: new RegExp(`^(delete permanently|endgültig löschen): Gone ${run}$`, 'i') }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(delete permanently|endgültig löschen)/i }).click();
    await toastOn(/deleted permanently|endgültig gelöscht/i);
    assert.equal(await page.evaluate(async (id) => (await __e2eFetch(`/api/v1/elements/${id}`)).status, gone), 404);
    assert.equal((await call('GET', '/api/v1/trash')).some((x) => x.id === gone), false, 'gone from the trash too');
  } finally {
    await call('DELETE', `/api/v1/me/api-keys/${key.id}`);
  }
});

test('mail outbox: a failed message sent again, details, test message, delete, run a job', async () => {
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const smtp = await smtpFake();
  // a port nobody listens on: sending fails first
  const closed = await new Promise((r) => { const sv = net.createServer().listen(0, '127.0.0.1', () => { const port = sv.address().port; sv.close(() => r(port)); }); });
  const prevMail = await call('GET', '/api/v1/admin/settings');
  const smtpAt = (port) => call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: prevMail.config.mail.from || 'wiki@example.invalid',
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port, security: 'none', username: '', password: '' } } });
  const addr = `outbox${run}@example.invalid`;
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  // notifications without the quiet period (expert setting)
  await call('PUT', '/api/v1/admin/limits', { values: { 'core.watch.quiet_seconds': 0 } });
  try {
    await smtpAt(closed);
    // a notification goes through the outbox: a person with a confirmed address
    // watches a category, a page is created there
    const u = await call('POST', '/api/v1/admin/users', { username: `outbox${run}`, password: 'Outbox-Passw0rd-1!', display_name: `Outbox ${run}` });
    await call('POST', `/api/v1/admin/users/${u.id}/emails`, { email: addr, verified: true });
    const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Watched ${run}` } })).id;
    await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'user', subject_id: u.id, permissions: 1, inherit: true });
    const g = await ctx2.newPage();
    await g.goto(BASE + '/login?local=1');
    await g.locator('input[name=username]').fill(`outbox${run}`);
    await g.locator('input[type=password]').fill('Outbox-Passw0rd-1!');
    await g.keyboard.press('Enter');
    await g.waitForURL((x) => !x.pathname.startsWith('/login'));
    assert.ok((await g.evaluate(async (id) => (await __e2eFetch('/api/v1/me/watches', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' },
      body: JSON.stringify({ target_type: 'category', target_id: id, mode: 'link', frequency: 'instant' }) })).status, cat)) < 300);
    await call('POST', '/api/v1/elements', { category_id: cat, type: 'core.html', title: `News ${run}`, body: { format: 'markdown', source: 'x' } });
    // the watch job collects the change (every 30 s; here run by hand)
    await page.goto(`${BASE}/admin/mail`);
    await idle();
    let entry;
    for (const deadline = Date.now() + 20000; !entry && Date.now() < deadline; await page.waitForTimeout(500)) {
      await page.locator('.ew-toast').evaluateAll((els) => els.forEach((e) => e.remove()));
      await page.getByRole('button', { name: /^(run now|jetzt ausführen): watch$/i }).click();
      await toastOn(/watch (finished|ausgeführt)/i);
      entry = (await call('GET', '/api/v1/admin/mail/outbox?limit=100')).find((o) => JSON.stringify(o).includes(addr) || o.subject.includes(`Watched ${run}`) || o.subject.includes(`News ${run}`));
    }
    assert.ok(entry, 'message in the outbox');
    const subject = entry.subject;
    const esc = subject.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

    await page.goto(`${BASE}/admin/mail?q=${encodeURIComponent(addr)}`);
    await idle();
    await page.getByRole('button', { name: /^(send queued messages now|wartende nachrichten jetzt senden)$/i }).click();
    await toastOn(/finished|ausgeführt/i);
    // failed on the closed port: send again after the server is fixed
    await smtpAt(smtp.port);
    await page.goto(`${BASE}/admin/mail?q=${encodeURIComponent(addr)}`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(send again|erneut senden): ${esc}$`, 'i') }).click();
    await toastOn(/sent again|erneut gesendet/i);
    await page.getByRole('button', { name: /^(send queued messages now|wartende nachrichten jetzt senden)$/i }).click();
    for (const deadline = Date.now() + 15000; !smtp.mails.some((m) => m.to.includes(addr)) && Date.now() < deadline;) await page.waitForTimeout(100);
    assert.ok(smtp.mails.some((m) => m.to.includes(addr)), 'the message arrived after sending it again');

    // details of the message
    await page.goto(`${BASE}/admin/mail?q=${encodeURIComponent(addr)}`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^${esc}$`) }).first().click();
    await page.getByRole('dialog', { name: subject }).waitFor();
    await a11y('outbox message', page);
    await page.getByRole('dialog').getByRole('button', { name: /^(close|schließen)$/i }).first().click();

    // a test message
    await page.getByLabel(/^(send a test message to|testnachricht senden an)/i).fill(`test${run}@example.invalid`);
    await page.getByRole('button', { name: /^(send test|test senden)$/i }).click();
    await toastOn(/test message sent|testnachricht an/i);
    assert.ok(smtp.mails.some((m) => m.to.includes(`test${run}@example.invalid`)), 'test message arrived');

    // delete the message from the outbox
    await page.getByRole('button', { name: new RegExp(`^(delete|löschen): ${esc}$`, 'i') }).first().click();
    await page.getByRole('dialog').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await page.getByRole('button', { name: new RegExp(`^(delete|löschen): ${esc}$`, 'i') }).first().waitFor({ state: 'detached' });

  } finally {
    await ctx2.close();
    await call('PUT', '/api/v1/admin/limits', { values: { 'core.watch.quiet_seconds': null } });
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail });
    smtp.close();
  }
});

test('property definitions: define, edit, only defined properties in a category, remove the definition', async () => {
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const dialog = () => page.getByRole('dialog');
  const name = `Cost ${run}`;
  await page.goto(`${BASE}/admin/properties`);
  await idle();
  await page.getByRole('button', { name: /^(define a property|eigenschaft definieren)$/i }).click();
  await dialog().getByLabel(/^(key|schlüssel)/i).fill(`cost_${run}`);
  await dialog().locator('input[lang="en"]').first().fill(name);
  await dialog().getByRole('button', { name: /^(define|definieren)$/i }).click();
  await toastOn(/property saved|eigenschaft gespeichert/i);
  await page.getByRole('rowheader', { name: new RegExp(`^${name}`) }).waitFor();
  await a11y('property definitions');
  // edit: another name
  await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): ${name}$`, 'i') }).click();
  await dialog().locator('input[lang="en"]').first().fill(`${name} total`);
  await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
  await toastOn(/property saved|eigenschaft gespeichert/i);
  await page.getByRole('rowheader', { name: new RegExp(`^${name} total`) }).waitFor();

  // a category that only takes defined properties
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Props ${run}` } })).id;
  await page.goto(`${BASE}/c/${cat}/settings?tab=properties`);
  await idle();
  await page.locator('.ew-toast').evaluateAll((els) => els.forEach((e) => e.remove()));
  await page.getByLabel(/^(free properties|freie eigenschaften)/i).selectOption('deny');
  await toastOn(/^(saved\.|gespeichert\.)$/i);
  await a11y('properties of a category');

  // remove the definition (the values stay as free properties)
  await page.goto(`${BASE}/admin/properties`);
  await idle();
  await page.getByRole('button', { name: new RegExp(`^(delete|löschen): ${name} total$`, 'i') }).click();
  await dialog().getByRole('button', { name: /^(remove definition|definition entfernen)$/i }).click();
  await toastOn(/definition removed|definition entfernt/i);
  await page.getByRole('rowheader', { name: new RegExp(`^${name} total`) }).waitFor({ state: 'detached' });
});

test('forms: a person sends a form with a file; submissions, file, CSV and Excel export, delete', async () => {
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `Forms ${run}` } })).id;
  const form = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.forms', title: `Contact ${run}`, body: {
    intro: 'Write to us.', submit_label: 'Send', success_message: `Thanks ${run}!`,
    fields: [{ id: 'name', type: 'text', label: 'Your name', required: true }, { id: 'doc', type: 'file', label: 'Document', accept: ['.txt'], max_files: 1, max_size_mb: 1 }],
    targets: { store: true } } });
  const name = `sender${run}`;
  const pw = 'Sender-Passw0rd-1!';
  const u = await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Sender ${run}` });
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'user', subject_id: u.id, permissions: 1, inherit: true });
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`form sender: ${e.message}`));
  const dir = mkdtempSync(join(tmpdir(), 'ewform-'));
  try {
    writeFileSync(join(dir, 'note.txt'), `attached ${run}\n`);
    await p2.goto(BASE + '/login?local=1');
    await p2.locator('input[name=username]').fill(name);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((x) => !x.pathname.startsWith('/login'));
    await p2.goto(`${BASE}/e/${form.id}`);
    await p2.getByLabel(/^your name/i).waitFor();
    const opened = Date.now();
    await a11y('form for a person', p2);
    // required field missing: the form says so
    await p2.getByRole('button', { name: /^send$/i }).click();
    await p2.locator('[aria-invalid="true"]').first().waitFor();
    await p2.getByLabel(/^your name/i).fill(`Ann ${run}`);
    await p2.locator('input[type=file]').setInputFiles(join(dir, 'note.txt'));
    // the form refuses what is sent faster than a person can (2 s, against bots)
    await p2.waitForTimeout(Math.max(0, 2500 - (Date.now() - opened)));
    await p2.getByRole('button', { name: /^send$/i }).click();
    await p2.getByRole('status').filter({ hasText: `Thanks ${run}!` }).waitFor();

    // the submissions on the page of the form
    await page.goto(`${BASE}/e/${form.id}`);
    await idle();
    const subs = page.locator('table', { has: page.getByRole('columnheader', { name: 'Your name' }) });
    await subs.getByRole('cell', { name: `Ann ${run}` }).waitFor();
    await a11y('form submissions');
    for (const [link, check] of [[page.getByRole('link', { name: 'note.txt' }), (b) => b.includes(`attached ${run}`)],
      [page.getByRole('link', { name: /^(export csv|csv exportieren)$/i }), (b) => b.includes(`Ann ${run}`)],
      [page.getByRole('link', { name: /^(export excel|excel exportieren)$/i }), (b) => b.startsWith('PK')]]) {
      const dl = page.waitForEvent('download');
      await link.click();
      const file = await (await dl).path();
      assert.ok(check(readFileSync(file, 'latin1')), `download of ${await link.innerText()}`);
    }
    await subs.getByRole('button', { name: /^(delete submission|einsendung löschen)/i }).first().click();
    await page.getByRole('dialog').getByRole('button', { name: /^(confirm|bestätigen)$/i }).click();
    await page.getByText(/no submissions|keine einsendungen/i).waitFor();
  } finally {
    await ctx2.close();
    rmSync(dir, { recursive: true, force: true });
  }
});

test('carousel: create, edit, slides added, ordered, edited and removed, a feed fetched, delete', async () => {
  // a feed of the test on this machine (internal addresses need the switch of the carousel)
  const rss = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/rss+xml' }).end(`<?xml version="1.0"?><rss version="2.0"><channel><title>Feed ${run}</title><link>https://example.com/</link><description>x</description>
<item><title>Feed news ${run}</title><link>https://example.com/news</link><description>From the feed</description><pubDate>${new Date().toUTCString()}</pubDate></item></channel></rss>`);
  });
  await new Promise((r) => rss.listen(0, '127.0.0.1', r));
  const feed = `http://127.0.0.1:${rss.address().port}/feed.xml`;
  const prev = (await call('GET', '/api/v1/admin/settings')).config.plugin_settings?.['core.carousel'] ?? {};
  await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'core.carousel': { ...prev, allow_private: true } } });
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const dialog = () => page.getByRole('dialog').last();
  const name = `Teaser ${run}`;
  try {
    await page.goto(`${BASE}/admin/carousels`);
    await idle();
    await page.getByRole('button', { name: /^(new carousel|neues karussell)$/i }).click();
    await dialog().getByLabel(/^name/i).fill(name);
    await dialog().getByLabel(/^(external feeds|externe feeds)/i).fill(feed);
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.getByRole('rowheader', { name }).waitFor();
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): ${name}$`, 'i') }).click();
    await dialog().getByLabel(/^name/i).fill(`${name} 2`);
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.getByRole('rowheader', { name: `${name} 2` }).waitFor();

    // slides
    await page.getByRole('button', { name: new RegExp(`^(slides|folien): ${name} 2$`, 'i') }).click();
    for (const title of [`Slide A ${run}`, `Slide B ${run}`]) {
      await page.getByRole('button', { name: /^(add slide|folie hinzufügen)$/i }).click();
      await dialog().getByLabel(/^(title|titel)/i).fill(title);
      await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
      await page.getByRole('rowheader', { name: title }).waitFor();
    }
    await a11y('carousel slides');
    await page.getByRole('button', { name: new RegExp(`^(up|nach oben): Slide B ${run}$`, 'i') }).click();
    await page.waitForFunction((b) => document.querySelector('table tbody tr th')?.textContent === b, `Slide B ${run}`);
    await page.getByRole('button', { name: new RegExp(`^(edit|bearbeiten): Slide A ${run}$`, 'i') }).click();
    await dialog().getByLabel(/^(title|titel)/i).fill(`Slide A2 ${run}`);
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click();
    await page.getByRole('rowheader', { name: `Slide A2 ${run}` }).waitFor();
    await page.getByRole('button', { name: new RegExp(`^(remove|entfernen): Slide A2 ${run}$`, 'i') }).click();
    await page.getByRole('dialog').last().getByRole('button', { name: /^(remove|entfernen)$/i }).click();
    await page.getByRole('rowheader', { name: `Slide A2 ${run}` }).waitFor({ state: 'detached' });

    // the feed, fetched now
    await page.getByRole('button', { name: /^(fetch feeds now|feeds jetzt abrufen)$/i }).click();
    await toastOn(/feeds fetched|feeds abgerufen/i);
    const slides = await call('GET', `/api/v1/x/core.carousel/carousels/${(await call('GET', '/api/v1/x/core.carousel/carousels')).find((c) => c.name === `${name} 2`).id}/slides`);
    assert.ok(slides.feeds.some((f) => f.feed === feed && !f.error && f.items >= 1), `feed fetched: ${JSON.stringify(slides.feeds)}`);

    // back and delete
    await page.getByRole('button', { name: /^(back to carousels|zurück zu den karussells)$/i }).click();
    await page.getByRole('button', { name: new RegExp(`^(delete|löschen): ${name} 2$`, 'i') }).click();
    await page.getByRole('dialog').last().getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await page.getByRole('rowheader', { name: `${name} 2` }).waitFor({ state: 'detached' });
  } finally {
    await call('PUT', '/api/v1/admin/settings', { plugin_settings: { 'core.carousel': prev } });
    rss.close();
  }
});

test('languages: add, export and import translations, remove', async () => {
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const had = (await call('GET', '/api/v1/site')).languages ?? [];
  assert.equal(had.includes('it'), false, 'Italian not there yet');
  await page.goto(`${BASE}/admin/languages`);
  await idle();
  await page.getByLabel(/^(language code|sprachcode)/i).fill('it');
  // the page reloads itself shortly after adding or removing a language: wait for that reload
  let reloaded = page.waitForEvent('load');
  await page.getByRole('button', { name: /^(add language|sprache hinzufügen)$/i }).click();
  await toastOn(/language added|sprache hinzugefügt/i);
  await reloaded;
  await page.goto(`${BASE}/admin/languages?edit=it`);
  await idle();
  const exchange = page.locator('details', { has: page.getByText(/^(import and export|import und export)$/i) });
  await exchange.locator('summary').click();
  await exchange.getByLabel(/^format/i).first().selectOption('json');
  const dl = page.waitForEvent('download');
  await exchange.getByRole('link', { name: /^(export|exportieren)$/i }).click();
  const file = await (await dl).path();
  assert.ok(Object.keys(JSON.parse(readFileSync(file, 'utf8'))).length > 0 || readFileSync(file, 'utf8').length > 2, 'export has texts');
  // the same file back: nothing unknown
  const dir = mkdtempSync(join(tmpdir(), 'ewlang-'));
  try {
    writeFileSync(join(dir, 'it.json'), readFileSync(file));
    await exchange.locator('input[type=file]').setInputFiles(join(dir, 'it.json'));
    await exchange.getByRole('button', { name: /^(import|importieren)$/i }).click();
    await exchange.getByRole('status').filter({ hasText: /0 unknown keys skipped|0 unbekannte schlüssel übersprungen/i }).waitFor();
    await a11y('translations of a language');
  } finally { rmSync(dir, { recursive: true, force: true }); }
  // remove it again
  await page.goto(`${BASE}/admin/languages`);
  await idle();
  await page.getByRole('button', { name: /^(remove|entfernen) (italian|italienisch|italiano)$/i }).click();
  reloaded = page.waitForEvent('load');
  await page.getByRole('dialog').last().getByRole('button', { name: /^(remove|entfernen)$/i }).click();
  await reloaded;
  for (const deadline = Date.now() + 10000; (await call('GET', '/api/v1/site')).languages.includes('it') && Date.now() < deadline;) await page.waitForTimeout(200);
  assert.equal((await call('GET', '/api/v1/site')).languages.includes('it'), false, 'removed');
});

test('users, groups and roles: created in the administration, the permissions take effect', async () => {
  const name = `adm${run}`;
  const pw = 'Adm-Passw0rd-1!';
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`administered user: ${e.message}`));
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const clearToasts = () => page.evaluate(() => document.querySelectorAll('.ew-toast').forEach((e) => e.remove()));
  const audit = () => p2.evaluate(async () => (await __e2eFetch('/api/v1/admin/audit/search')).status);
  const signIn = async (p = p2) => {
    await p.goto(BASE + '/login?local=1');
    await p.locator('input[name=username]').fill(name);
    await p.locator('input[type=password]').fill(pw);
    await p.keyboard.press('Enter');
    await p.waitForURL((u) => !u.pathname.startsWith('/login'));
  };
  const dialog = () => page.getByRole('dialog');
  const save = async () => { await clearToasts(); await dialog().getByRole('button', { name: /^(save|speichern)$/i }).click(); await toastOn(/^(saved\.|gespeichert\.)$/i); };
  try {
    // a role with one global permission
    await page.goto(`${BASE}/admin/roles`);
    await idle();
    await page.getByRole('button', { name: /^(new role|neue rolle)$/i }).click();
    await dialog().getByLabel(/^name/i).fill(`Auditors ${run}`);
    await dialog().getByRole('checkbox', { name: /^(view the audit log|protokoll ansehen)/i }).check();
    await save();
    await page.getByRole('rowheader', { name: `Auditors ${run}`, exact: true }).waitFor();
    await a11y('roles');
    // a group with that role
    await page.goto(`${BASE}/admin/groups`);
    await idle();
    await page.getByRole('button', { name: /^(new group|neue gruppe)$/i }).click();
    await dialog().getByLabel(/^name/i).fill(`Audit team ${run}`);
    await dialog().getByRole('checkbox', { name: `Auditors ${run}` }).check();
    await save();
    await page.getByRole('rowheader', { name: `Audit team ${run}`, exact: true }).waitFor();

    // a person, created in the administration
    await page.goto(`${BASE}/admin/users`);
    await idle();
    await page.getByRole('button', { name: /^(new user|neuer benutzer)$/i }).click();
    await dialog().getByLabel(/^(username|benutzername)/i).fill(name);
    await dialog().getByLabel(/^(display name|anzeigename)/i).fill(`Administered ${run}`);
    await dialog().getByLabel(/^(password|passwort)/i).fill(pw);
    await save();
    await signIn();
    assert.equal(await audit(), 403, 'no permission yet');

    // member of the group: the permission of its role applies
    await page.goto(`${BASE}/admin/groups`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(members of|mitglieder von) Audit team ${run}$`, 'i') }).click();
    await dialog().getByLabel(/^(add to the group|zur gruppe hinzufügen)/i).selectOption({ label: `${name}@local` });
    await dialog().getByRole('button', { name: /^(add to the group|zur gruppe hinzufügen)$/i }).click();
    await dialog().getByRole('button', { name: new RegExp(`^(remove ${name}@local from the group|${name}@local aus der gruppe entfernen)$`, 'i') }).waitFor();
    await a11y('group members');
    assert.equal(await audit(), 200, 'the permission of the group role applies');
    const removed = page.waitForResponse((r) => r.request().method() === 'DELETE' && r.url().includes('/members/'));
    await dialog().getByRole('button', { name: new RegExp(`^(remove ${name}@local from the group|${name}@local aus der gruppe entfernen)$`, 'i') }).click();
    assert.ok((await removed).ok(), 'removed');
    assert.equal(await audit(), 403, 'removed from the group: the permission ends');
    await dialog().getByRole('button', { name: /^(close|schließen)$/i }).last().click();

    // edit the person: name, user name and the role directly
    await page.goto(`${BASE}/admin/users${'?q=' + name}`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(edit ${name}@local|${name}@local bearbeiten)$`, 'i') }).click();
    await dialog().getByLabel(/^(display name|anzeigename)/i).fill(`Administered renamed ${run}`);
    await dialog().getByLabel(/^(user name|benutzername)/i).fill(`admalias${run}`);
    await dialog().getByRole('group', { name: /^(roles|rollen)$/i }).getByRole('checkbox', { name: `Auditors ${run}` }).check();
    await save();
    assert.equal(await audit(), 200, 'the role given directly applies');
    const me = await p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user);
    assert.equal(me.display_name, `Administered renamed ${run}`);

    // business card of the person
    await page.getByRole('button', { name: new RegExp(`^(business card|visitenkarte): ${name}@local$`, 'i') }).click();
    await dialog().getByRole('button', { name: /^(save|speichern)$/i }).waitFor();
    await save();
    await dialog().getByRole('button', { name: /^(close|schließen)$/i }).first().click();

    // e-mail addresses and sessions of the person
    await page.getByRole('button', { name: new RegExp(`^(e-mail & sign-ins|e-mail & anmeldungen): ${name}@local$`, 'i') }).click();
    const id = dialog();
    const addMail = async (a) => {
      await id.getByLabel(/^(add an address|adresse hinzufügen)/i).fill(a);
      await clearToasts();
      await id.getByRole('button', { name: /^(add|hinzufügen)$/i }).click();
      await toastOn(/address added and confirmed|adresse hinzugefügt und bestätigt/i);
    };
    await addMail(`adm${run}@example.invalid`);
    await addMail(`adm${run}-2@example.invalid`);
    await id.getByRole('button', { name: new RegExp(`^(make primary|als primär festlegen): adm${run}-2@example.invalid$`, 'i') }).click();
    await toastOn(/primary address changed|primäre adresse geändert/i);
    await id.getByRole('button', { name: new RegExp(`^(remove|entfernen)`, 'i') }).first().click();
    await page.getByRole('dialog').filter({ hasText: `adm${run}@example.invalid` }).getByRole('button', { name: /^(remove|entfernen)$/i }).last().click();
    await toastOn(/address removed|adresse entfernt/i);
    await a11y('e-mail and sign-ins of a person');
    // a single session, then all
    await id.getByRole('button', { name: /^(sign out|abmelden):/i }).first().click();
    await page.getByRole('dialog').getByRole('button', { name: /^(sign out|abmelden)$/i }).last().click();
    await toastOn(/session ended|sitzung beendet/i);
    assert.equal(await p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user), null, 'the session was ended');
    await signIn();
    const ctx3 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    const p3 = await ctx3.newPage();
    await signIn(p3); // two sessions
    await page.getByRole('dialog').getByRole('button', { name: /^(close|schließen)$/i }).first().click();
    await page.getByRole('button', { name: new RegExp(`^(e-mail & sign-ins|e-mail & anmeldungen): ${name}@local$`, 'i') }).click();
    await dialog().getByRole('button', { name: /^(end all sessions|alle sitzungen beenden)$/i }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(end all sessions|alle sitzungen beenden)$/i }).last().click();
    await toastOn(/sessions ended|sitzungen beendet/i);
    for (const p of [p2, p3]) assert.equal(await p.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user), null, 'all sessions ended');
    await ctx3.close();
    await dialog().getByRole('button', { name: /^(close|schließen)$/i }).first().click();

    // overview of the permissions of the person
    await page.goto(`${BASE}/admin/permissions`);
    await idle();
    await a11y('permission overview');

    // the defaults of all signed-in users: changed and set back
    await page.goto(`${BASE}/admin/roles`);
    await idle();
    await page.getByRole('button', { name: /^(edit|bearbeiten): (permissions of all signed-in users|rechte aller angemeldeten benutzer)$/i }).click();
    await save();

    // delete person, group and role
    await page.goto(`${BASE}/admin/users${'?q=' + name}`);
    await idle();
    await page.getByRole('button', { name: new RegExp(`^(delete ${name}@local|${name}@local löschen)$`, 'i') }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(delete|löschen)$/i }).click();
    await toastOn(/^(deleted\.|gelöscht\.)$/i);
    for (const [path, label] of [['groups', `Audit team ${run}`], ['roles', `Auditors ${run}`]]) {
      await page.goto(`${BASE}/admin/${path}`);
      await idle();
      await page.getByRole('button', { name: new RegExp(`^(delete ${label}|${label} löschen)$`, 'i') }).click();
      await page.getByRole('dialog').getByRole('button', { name: /^(delete|löschen)$/i }).click();
      await page.getByRole('rowheader', { name: label, exact: true }).waitFor({ state: 'detached' });
    }
  } finally {
    await ctx2.close();
  }
});

test('category administration: a permission for a person takes effect and ends, order, move, duplicate, history, rollback of texts and of the structure, insights', async () => {
  const name = `catu${run}`;
  const pw = 'Catu-Passw0rd-1!';
  await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Cat User ${run}` });
  const top = (await call('POST', '/api/v1/categories', { titles: { en: `Admin ${run}` } })).id;
  const one = (await call('POST', '/api/v1/categories', { titles: { en: `One ${run}` }, parent_id: top })).id;
  const two = (await call('POST', '/api/v1/categories', { titles: { en: `Two ${run}` }, parent_id: top })).id;
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`category user: ${e.message}`));
  const toastOn = (re) => page.locator('.ew-toast', { hasText: re }).first().waitFor();
  const sees = async (id) => p2.evaluate(async (id) => (await __e2eFetch(`/api/v1/categories/${id}`)).status, id);
  try {
    await p2.goto(BASE + '/login?local=1');
    await p2.locator('input[name=username]').fill(name);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    assert.notEqual(await sees(top), 200, 'no access before the rule');

    // a rule for the person, added in the interface
    await page.goto(`${BASE}/c/${top}/settings?tab=permissions`);
    await idle();
    await page.getByRole('button', { name: /^(add permission|berechtigung hinzufügen)$/i }).click();
    let dlg = page.getByRole('dialog');
    await dlg.getByLabel(/^(applies to|gilt für)/i).selectOption('user');
    const lookup = page.waitForResponse((r) => r.url().includes('/api/v1/users/lookup') && r.ok());
    await dlg.getByLabel(/^(user|benutzer)$/i).fill(`${name}@local`);
    await lookup;
    await dlg.getByRole('button', { name: /^(add permission|berechtigung hinzufügen)$/i }).click();
    await toastOn(/permission added|berechtigung hinzugefügt/i);
    // the rule names the person, not only an id
    const subject = `Cat User ${run} (${name}@local)`;
    await page.getByRole('rowheader', { name: new RegExp(subject.replace(/[()]/g, '\\$&')) }).waitFor();
    await a11y('category permissions');
    assert.equal(await sees(top), 200, 'the rule gives access');
    assert.equal(await sees(one), 200, 'inherited by subcategories');
    // change the rule (here: no longer for subcategories)
    await page.getByRole('button', { name: new RegExp(`^(edit permissions of|rechte von) .*${name}`, 'i') }).click();
    dlg = page.getByRole('dialog');
    await dlg.getByRole('checkbox', { name: /also applies to all subcategories|gilt auch für alle unterkategorien/i }).uncheck();
    await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await toastOn(/^(saved\.|gespeichert\.)$/i);
    assert.equal(await sees(top), 200, 'still on the category itself');
    assert.notEqual(await sees(one), 200, 'no longer below it');
    // remove it: the access ends at once
    await page.getByRole('button', { name: new RegExp(`^(remove rule for|regel für) .*${name}`, 'i') }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(remove|entfernen)$/i }).click();
    await toastOn(/permission removed|berechtigung entfernt/i);
    assert.notEqual(await sees(top), 200, 'access ends with the rule');

    // insights of the category
    const insights = page.waitForResponse((r) => r.url().includes(`/api/v1/categories/${top}/insights`) && r.ok());
    await page.goto(`${BASE}/c/${top}/settings?tab=insights`);
    await insights;
    await idle();
    await a11y('category insights');

    // order of the subcategories (edit mode)
    await page.goto(`${BASE}/c/${top}`);
    await idle();
    const edit = page.locator('.ew-pos--header-tools button.ew-switch-btn[aria-pressed]');
    if ((await edit.getAttribute('aria-pressed')) !== 'true') await edit.click();
    await page.getByRole('button', { name: new RegExp(`^(move Two ${run} up|Two ${run} nach oben)$`, 'i') }).click();
    await toastOn(/order changed|reihenfolge geändert/i);
    const order = (await call('GET', `/api/v1/categories?parent=${top}`)).map((x) => x.id);
    assert.deepEqual(order, [two, one], 'new order saved');

    // a point in time to go back to, then a change
    const before = await page.evaluate(() => { const d = new Date(Date.now() + 1000); const p = (n) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`; });
    await page.waitForTimeout(1500); // the change below must be later than this point (seconds)
    await call('PUT', `/api/v1/categories/${one}`, { titles: { en: `One changed ${run}` } });

    // move Two into One, duplicate One to the top level
    await page.goto(`${BASE}/c/${two}`);
    await idle();
    await page.getByRole('button', { name: /^(move|verschieben)$/i }).click();
    dlg = page.getByRole('dialog');
    await dlg.locator('select').selectOption(String(one));
    await dlg.getByRole('button', { name: /^(move|verschieben)$/i }).click();
    await toastOn(/category moved|kategorie verschoben/i);
    assert.equal((await call('GET', `/api/v1/categories/${two}`)).category.parent_id, one);
    await page.goto(`${BASE}/c/${one}`);
    await idle();
    await page.getByRole('button', { name: /^(duplicate|duplizieren)$/i }).click();
    dlg = page.getByRole('dialog');
    await dlg.locator('select').selectOption(String(top));
    const duplicated = page.waitForResponse((r) => r.url().endsWith(`/api/v1/categories/${one}/duplicate`) && r.status() < 300);
    await dlg.getByRole('button', { name: /^(duplicate|duplizieren)$/i }).click();
    await toastOn(/category duplicated|kategorie dupliziert/i);
    const copy = (await (await duplicated).json()).id;
    assert.equal((await call('GET', `/api/v1/categories/${copy}`)).category.parent_id, top);

    // history and rollback to the point in time: the title of One is back
    await page.goto(`${BASE}/c/${top}/settings?tab=history`);
    await idle();
    await page.getByLabel(/^(point in time|zeitpunkt)/i).fill(before);
    await page.getByRole('button', { name: /^(reset to this point in time|auf diesen zeitpunkt zurücksetzen)$/i }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(reset to this point in time|auf diesen zeitpunkt zurücksetzen)$/i }).click();
    await toastOn(/rollback done|zurückgesetzt/i);
    assert.equal((await call('GET', `/api/v1/categories/${one}`)).category.titles.en, `One ${run}`, 'title as before');
    // without the structure, moved or new categories stay
    assert.equal((await call('GET', `/api/v1/categories/${two}`)).category.parent_id, one, 'moves stay');

    // with the structure: only together with all subcategories
    await page.goto(`${BASE}/c/${top}/settings?tab=history`);
    await idle();
    const structure = page.getByRole('checkbox', { name: /include moved and new categories|verschobene und neue kategorien einbeziehen/i });
    const recursive = page.getByRole('checkbox', { name: /include all subcategories|alle unterkategorien einbeziehen/i });
    await recursive.uncheck();
    assert.equal(await structure.isDisabled(), true, 'structure needs all subcategories');
    await recursive.check();
    await structure.check();
    await a11y('category history');
    await page.getByLabel(/^(point in time|zeitpunkt)/i).fill(before);
    await page.getByRole('button', { name: /^(reset to this point in time|auf diesen zeitpunkt zurücksetzen)$/i }).click();
    await page.getByRole('dialog').getByRole('button', { name: /^(reset to this point in time|auf diesen zeitpunkt zurücksetzen)$/i }).click();
    await toastOn(/1 newer categories moved to the trash|1 neuere kategorien in den papierkorb/i);
    assert.equal((await call('GET', `/api/v1/categories/${two}`)).category.parent_id, top, 'the move is undone');
    assert.deepEqual((await call('GET', `/api/v1/categories?parent=${top}`)).map((x) => x.id), [two, one], 'place and order as then');
    assert.equal((await page.evaluate(async (id) => (await __e2eFetch(`/api/v1/categories/${id}`)).status, copy)), 404, 'the later copy is in the trash');
  } finally {
    await ctx2.close();
  }
});

test('help texts: explanations hidden by default, shown with "?" or in the profile, notes to fill in always there', async () => {
  await page.goto(`${BASE}/profile`);
  await idle();
  const editorHelp = page.locator('.ew-help', { hasText: /^(both editors save markdown|beide editoren speichern markdown)/i });
  const rules = page.locator('.ew-help', { hasText: /^(at least 12 characters|mindestens 12 zeichen)/i });
  // off by default: the explanation is gone, also for screen readers; the
  // rules for the password stay
  await editorHelp.waitFor({ state: 'attached' });
  assert.equal(await editorHelp.isVisible(), false);
  const editorSel = page.getByLabel(/^(editor for pages|editor für seiten)$/i).first();
  const helpId = await editorHelp.getAttribute('id');
  assert.ok(!((await editorSel.getAttribute('aria-describedby')) ?? '').split(' ').includes(helpId), 'a hidden text does not describe its field');
  if (await rules.count()) assert.equal(await rules.first().isVisible(), true);
  const box = page.getByRole('checkbox', { name: /^(show help texts|hilfetexte anzeigen)$/i });
  assert.equal(await box.isChecked(), false);
  // the "?" button switches it on, for the person (it stays after loading)
  const toggle = page.getByRole('button', { name: /^(help texts|hilfetexte)$/i });
  assert.equal(await toggle.getAttribute('aria-pressed'), 'false');
  await toggle.click();
  await editorHelp.waitFor();
  assert.equal(await toggle.getAttribute('aria-pressed'), 'true');
  await page.waitForFunction((id) => (document.querySelector('[aria-describedby*="' + id + '"]') !== null), helpId);
  await a11y('help texts shown');
  await page.reload();
  await idle();
  await page.locator('.ew-help', { hasText: /^(both editors save markdown|beide editoren speichern markdown)/i }).waitFor();
  assert.equal(await page.getByRole('checkbox', { name: /^(show help texts|hilfetexte anzeigen)$/i }).isChecked(), true);
  // off again in the profile
  await page.getByRole('checkbox', { name: /^(show help texts|hilfetexte anzeigen)$/i }).uncheck();
  await page.locator('form.ew-form').first().getByRole('button', { name: /^(save|speichern)$/i }).click();
  await page.locator('.ew-help', { hasText: /^(both editors save markdown|beide editoren speichern markdown)/i }).waitFor({ state: 'hidden' });
  assert.equal(await page.getByRole('button', { name: /^(help texts|hilfetexte)$/i }).getAttribute('aria-pressed'), 'false');
});

test('own profile: data, e-mail addresses, notifications, watches, feed key, API keys, SSH keys, sessions, password', async () => {
  const name = `prof${run}`;
  const pw = 'Prof-Passw0rd-1!';
  const pw2 = 'Prof-Passw0rd-2!';
  await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: `Profile ${run}` });
  const smtp = await smtpFake();
  const prevMail = await call('GET', '/api/v1/admin/settings');
  await call('PUT', '/api/v1/admin/settings', { mail: { ...prevMail.config.mail, enabled: true, transport: 'smtp', from: prevMail.config.mail.from || 'wiki@example.invalid',
    smtp: { ...prevMail.config.mail.smtp, host: '127.0.0.1', port: smtp.port, security: 'none', username: '', password: '' } } });
  const login = async (p, password) => {
    await p.goto(BASE + '/login?local=1');
    await p.locator('input[name=username]').fill(name);
    await p.locator('input[type=password]').fill(password);
    await p.keyboard.press('Enter');
    await p.waitForURL((u) => !u.pathname.startsWith('/login'));
  };
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const ctx3 = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const p2 = await ctx2.newPage();
  const p3 = await ctx3.newPage();
  p2.on('pageerror', (e) => errors.push(`profile: ${e.message}`));
  const logged = errors.length;
  // one toast of its own; old toasts are cleared first where the text repeats
  const toastOn = (p, re) => p.locator('.ew-toast', { hasText: re }).first().waitFor();
  const clearToasts = (p) => p.evaluate(() => document.querySelectorAll('.ew-toast').forEach((e) => e.remove()));
  const confirm = (p, re) => p.getByRole('dialog').getByRole('button', { name: re }).click();
  try {
    await login(p2, pw);
    await login(p3, pw); // a second session, ended from the profile below
    // a watch to remove later (set where the category is shown)
    const cat = (await call('GET', '/api/v1/categories'))[0];
    await p2.evaluate(async (id) => (await __e2eFetch('/api/v1/me/watches', { method: 'PUT', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': 'application/json' },
      body: JSON.stringify({ target_type: 'category', target_id: id, mode: 'link', frequency: 'weekly' }) })).status, cat.id);
    await p2.goto(BASE + '/profile');
    await p2.locator('#ak-h').waitFor();
    await idle();

    // data and user name
    const data = p2.locator('section[aria-labelledby="pr-h"]');
    await data.getByLabel(/^(display name|anzeigename)/i).fill(`Profile renamed ${run}`);
    await data.getByLabel(/^(user name|benutzername)/i).fill(`alias${run}`);
    await clearToasts(p2);
    await data.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await toastOn(p2, /^(saved\.|gespeichert\.)/i);
    const me = await p2.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user);
    assert.equal(me.display_name, `Profile renamed ${run}`);
    assert.equal(me.alias, `alias${run}`);

    // a further e-mail address: confirmation link, then primary, then the old one is removed
    const addr = `prof${run}@example.invalid`;
    const addr2 = `prof${run}-2@example.invalid`;
    const emails = p2.locator('section', { has: p2.getByRole('heading', { name: /^(e-mail addresses|e-mail-adressen)$/i }) });
    const addMail = async (a) => {
      await emails.getByLabel(/^(add an address|adresse hinzufügen)/i).fill(a);
      await emails.getByRole('button', { name: /^(add|hinzufügen)$/i }).click();
      await toastOn(p2, new RegExp(a.replace(/[.+]/g, '\\$&')));
    };
    const linkFor = async (a, after = 0) => {
      const deadline = Date.now() + 15000;
      let mail;
      while (!(mail = smtp.mails.slice(after).find((m) => m.to.includes(a))) && Date.now() < deadline) await p2.waitForTimeout(50);
      const link = /\/verify-email\?token=[A-Za-z0-9_-]+/.exec(mail?.text ?? '')?.[0];
      assert.ok(link, `confirmation link in the e-mail: ${mail?.text}`);
      return link;
    };
    await addMail(addr);
    // send the confirmation again: a second mail with a fresh link
    const before = smtp.mails.length;
    await emails.getByRole('button', { name: new RegExp(`^(send confirmation again|bestätigung erneut senden): ${addr}$`, 'i') }).click();
    await toastOn(p2, new RegExp(addr.replace(/[.+]/g, '\\$&')));
    await p2.goto(BASE + await linkFor(addr, before));
    await p2.locator('.ew-notice--ok', { hasText: addr }).waitFor();
    await a11y('e-mail address confirmed', p2);
    await p2.goto(BASE + '/profile');
    await p2.locator('#ak-h').waitFor();
    await addMail(addr2);
    await p2.goto(BASE + await linkFor(addr2));
    await p2.locator('.ew-notice--ok', { hasText: addr2 }).waitFor();
    await p2.goto(BASE + '/profile');
    await p2.locator('#ak-h').waitFor();
    const row = (a) => emails.getByRole('row').filter({ hasText: a });
    await row(addr2).getByRole('button', { name: /^(make primary|als primär festlegen)/i }).click();
    await toastOn(p2, /primary address changed|primäre adresse geändert/i);
    await row(addr).getByRole('button', { name: /^(remove|entfernen)/i }).click();
    await confirm(p2, /^(remove|entfernen)$/i);
    await toastOn(p2, /address removed|adresse entfernt/i);
    await row(addr).waitFor({ state: 'detached' });

    // e-mail notifications
    const notify = p2.locator('section[aria-labelledby="nt-h"]');
    const box = notify.getByRole('checkbox').first();
    const was = await box.isChecked();
    await box.setChecked(!was);
    await clearToasts(p2);
    await notify.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await toastOn(p2, /^(saved\.|gespeichert\.)/i);

    // stop watching
    const watches = p2.locator('section[aria-labelledby="wt-h"]');
    await watches.getByRole('button', { name: /^(stop watching|nicht mehr beobachten):/i }).first().click();
    await toastOn(p2, /watch removed|beobachtung beendet/i);

    // an action that fails says so (regression: the feed buttons swallowed errors)
    await p2.route('**/api/v1/me/feed-token', (route) => route.fulfill({ status: 422, contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'e2e', message: `E2E refused ${run}` } }) }), { times: 1 });
    await p2.getByRole('button', { name: /^(renew key|schlüssel erneuern)$/i }).first().click();
    await confirm(p2, /^(renew key|schlüssel erneuern)$/i);
    await toastOn(p2, new RegExp(`E2E refused ${run}`));

    // the personal feed key: renew and delete
    await p2.getByRole('button', { name: /^(renew key|schlüssel erneuern)$/i }).first().click();
    await confirm(p2, /^(renew key|schlüssel erneuern)$/i);
    await toastOn(p2, /new key created|neuer schlüssel erzeugt/i);
    await p2.getByRole('button', { name: /^(delete key|schlüssel löschen)$/i }).first().click();
    await confirm(p2, /^(delete key|schlüssel löschen)$/i);
    await toastOn(p2, /key deleted|schlüssel gelöscht/i);

    // API key: create, show once, change its limit, revoke
    await p2.getByRole('button', { name: /^(create api key|api-key erstellen)$/i }).click();
    let dlg = p2.getByRole('dialog');
    await dlg.getByLabel(/^name/i).fill(`Key ${run}`);
    await dlg.getByLabel(/^(requests per second|anfragen pro sekunde)/i).fill('5');
    await dlg.getByRole('button', { name: /^(create|erstellen)$/i }).click();
    dlg = p2.getByRole('dialog', { name: /api key created|api-key erstellt/i });
    const token = (await dlg.locator('pre').textContent()).trim();
    assert.ok(token.length > 20, 'the token is shown once');
    await a11y('API key created', p2);
    await dlg.getByRole('button', { name: /^(close|schließen)$/i }).last().click();
    await p2.getByRole('rowheader', { name: `Key ${run}`, exact: true }).waitFor();
    assert.equal((await p2.content()).includes(token), false, 'the token is not shown again');
    await p2.getByRole('button', { name: new RegExp(`^(change the limit of|grenze von) .Key ${run}`, 'i') }).click();
    dlg = p2.getByRole('dialog');
    await dlg.getByLabel(/^(requests per second|anfragen pro sekunde)/i).fill('7');
    await dlg.getByRole('button', { name: /^(save|speichern)$/i }).click();
    await p2.getByRole('row').filter({ has: p2.getByRole('rowheader', { name: `Key ${run}`, exact: true }) }).filter({ hasText: /(^|[^0-9])7\/s/ }).waitFor();
    await p2.getByRole('button', { name: new RegExp(`^(revoke Key ${run}|Key ${run} widerrufen)$`, 'i') }).click();
    await confirm(p2, /^(revoke|widerrufen)$/i);
    await toastOn(p2, /^(revoked|widerrufen)$/i);

    // SSH key: add and remove
    const { publicKey } = generateKeyPairSync('ed25519');
    const raw = Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url');
    const str = (b) => { const l = Buffer.alloc(4); l.writeUInt32BE(b.length); return Buffer.concat([l, b]); };
    const sshKey = `ssh-ed25519 ${Buffer.concat([str(Buffer.from('ssh-ed25519')), str(raw)]).toString('base64')} e2e`;
    await p2.getByRole('button', { name: /^(add ssh key|ssh-schlüssel hinzufügen)$/i }).click();
    dlg = p2.getByRole('dialog');
    await dlg.getByLabel(/^name/i).fill(`Laptop ${run}`);
    await dlg.getByLabel(/^(public key|öffentlicher schlüssel)/i).fill(sshKey);
    await dlg.getByRole('button', { name: /^(add ssh key|ssh-schlüssel hinzufügen)$/i }).click();
    await toastOn(p2, /ssh key added|ssh-schlüssel hinzugefügt/i);
    await p2.getByRole('button', { name: new RegExp(`^(remove Laptop ${run}|Laptop ${run} entfernen)$`, 'i') }).click();
    await confirm(p2, /^(remove|entfernen)$/i);
    await p2.getByRole('rowheader', { name: `Laptop ${run}`, exact: true }).waitFor({ state: 'detached' });

    // end the other session: that browser is signed out
    const sessions = p2.locator('section', { has: p2.getByRole('heading', { name: /^(sessions|sitzungen)$/i }) });
    await sessions.getByRole('button', { name: /^(sign out|abmelden):/i }).first().click();
    await confirm(p2, /^(sign out|abmelden)$/i);
    await toastOn(p2, /session ended|sitzung beendet/i);
    assert.equal(await p3.evaluate(async () => (await (await __e2eFetch('/api/v1/auth/me')).json()).user), null, 'the ended session is signed out');
    assert.equal(await p3.evaluate(async () => (await __e2eFetch('/api/v1/me/sessions')).status), 401, 'the ended session gets no data');

    // password: wrong repetition refused in the form, then changed
    const pwf = p2.locator('section[aria-labelledby="pw-h"]');
    await pwf.getByLabel(/^(current password|aktuelles passwort)/i).fill(pw);
    await pwf.getByLabel(/^(new password|neues passwort)$/i).fill(pw2);
    await pwf.getByLabel(/^(repeat new password|neues passwort wiederholen)/i).fill(pw2);
    await pwf.getByRole('button', { name: /^(change password|passwort ändern)$/i }).click();
    await toastOn(p2, /password changed|passwort geändert/i);
    await a11y('own profile', p2);
    await login(p3, pw2);
  } finally {
    await ctx2.close();
    await ctx3.close();
    await call('PUT', '/api/v1/admin/settings', { mail: prevMail.config.mail });
    smtp.close();
    // ended sessions answer 401 on purpose
    errors.splice(logged, errors.length - logged, ...errors.slice(logged).filter((e) => !/status of (401|422)/.test(e)));
  }
});

test('administration overview: tiles with links, update commands, narrow and wide, light and dark', async () => {
  // the start page of the administration is the overview
  await page.goto(`${BASE}/admin`);
  await idle();
  await page.waitForSelector('.ew-overview');
  assert.match((await page.locator('.ew-admin-panel h1').textContent()) ?? '', /^(overview|übersicht)$/i);
  assert.equal(await page.locator('.ew-admin nav.ew-subnav a[aria-current="page"]').getAttribute('href'), '/admin/overview');
  for (const id of ['health', 'updates', 'security', 'plugins', 'users', 'license']) {
    assert.equal(await page.locator(`.ew-overview section[aria-labelledby="ov-${id}"]`).count(), 1, `tile ${id}`);
  }
  // the update tile: filled after the check (or its failure), with the
  // commands to install an update; the wiki offers no button for the core
  const updates = page.locator('section[aria-labelledby="ov-updates"]');
  await page.waitForFunction(() => document.querySelector('section[aria-labelledby="ov-updates"] [aria-busy="false"]'));
  assert.ok(/docker compose|scripts\/update\.sh/.test((await updates.textContent()) ?? ''), 'update commands');
  assert.equal(await updates.locator('button').count(), 0, 'no update button for the core');
  // users: a deactivated account is counted
  const users = page.locator('section[aria-labelledby="ov-users"] dd');
  const disabledBefore = Number(await users.nth(1).textContent());
  const u = await call('POST', '/api/v1/admin/users', { username: `ov${run}`, password: 'Ov-Passw0rd-123!' });
  await call('PUT', `/api/v1/admin/users/${u.id}`, { disabled: true });
  await page.reload();
  await idle();
  assert.equal(Number(await page.locator('section[aria-labelledby="ov-users"] dd').nth(1).textContent()), disabledBefore + 1, 'deactivated accounts');
  await a11y('administration overview');
  // narrow: one column, nothing wider than the screen; wide: several columns
  const size = page.viewportSize();
  try {
    await page.setViewportSize({ width: 390, height: 800 });
    await idle();
    // what sticks out of its tile (named for the report)
    const overflow = await page.evaluate(() => [...document.querySelectorAll('.ew-overview > section')].flatMap((sec) => {
      const right = sec.getBoundingClientRect().right;
      return [...sec.querySelectorAll('*')].filter((el) => el.getBoundingClientRect().right > right + 1)
        .map((el) => `${sec.getAttribute('aria-labelledby')}: <${el.tagName.toLowerCase()} class="${el.className}"> ${(el.textContent ?? '').slice(0, 60)}`);
    }));
    assert.deepEqual(overflow, [], 'overview wider than a narrow screen');
    assert.ok(await page.evaluate(() => document.querySelector('.ew-overview').scrollWidth <= document.querySelector('.ew-overview').clientWidth + 1), 'overview scrolls sideways');
    await a11y('administration overview (narrow)');
    await page.setViewportSize({ width: 1400, height: 900 });
    await idle();
    const tops = await page.locator('.ew-overview > section').evaluateAll((els) => new Set(els.map((el) => Math.round(el.getBoundingClientRect().top))).size);
    assert.ok(tops < 6, 'tiles side by side on a wide screen');
  } finally {
    if (size) await page.setViewportSize(size);
  }
  // dark colour scheme
  const uiBefore = (await call('GET', '/api/v1/admin/settings')).config.ui;
  await call('PUT', '/api/v1/admin/settings', { ui: { ...uiBefore, template: 'theme.wiki.dark' } });
  await call('PUT', '/api/v1/scheme', { scheme: 'dark' });
  try {
    await page.goto(`${BASE}/admin/overview`);
    await idle();
    await page.waitForFunction(() => document.querySelector('section[aria-labelledby="ov-updates"] [aria-busy="false"]'));
    await a11y('administration overview (dark)');
  } finally {
    await call('PUT', '/api/v1/scheme', { scheme: '' });
    await call('PUT', '/api/v1/admin/settings', { ui: uiBefore });
  }
  // the heading of a tile leads to its page
  await page.goto(`${BASE}/admin`);
  await idle();
  await page.locator('#ov-users a').click();
  await page.waitForURL(/\/admin\/users$/);
});

test('pop-up notification: shown once as a dialog, then in the bell', async () => {
  await page.goto(BASE + '/');
  await idle();
  const cat = await call('POST', '/api/v1/categories', { titles: { en: `Pop-ups ${run}` } });
  const wf = await call('POST', '/api/v1/elements', { category_id: cat.id, type: 'workflow.process', title: `Pop-up ${run}`, body: {
    fields: [{ id: 'name', label: 'Name', type: 'text', required: true }],
    steps: [{ id: 'tell', name: 'Tell', type: 'notify', next: 'done', notify: { to: 'starter', channel: 'inbox', popup: true, subject: 'Answer for {{field.name}}', body: 'Line one\nLine two' } },
      { id: 'done', name: 'Done', type: 'end', outcome: 'success' }],
  } });
  const popups = async (n) => {
    // the message step runs in the background (at most half a minute)
    for (let i = 0; i < 120; i++) {
      const res = await call('GET', '/api/v1/me/inbox/popups');
      if (res.items.length >= n) return res.items;
      await page.waitForTimeout(500);
    }
    throw new Error('no pop-up arrived');
  };
  try {
    for (const scheme of ['light', 'dark']) {
      await call('POST', `/api/v1/x/workflow.process/elements/${wf.id}/instances`, { data: { name: `${scheme} ${run}` } });
      await popups(1);
      await call('PUT', '/api/v1/scheme', { scheme });
      await page.goto(BASE + '/');
      const dlg = page.getByRole('dialog', { name: `Answer for ${scheme} ${run}` });
      await dlg.waitFor();
      await dlg.getByText('Line two').waitFor();
      // the focus is inside the dialog, on its first button
      assert.ok(await page.evaluate(() => !!document.activeElement?.closest('dialog')), 'focus in the pop-up');
      await a11y(`pop-up notification (${scheme})`);
      await page.keyboard.press('Escape');
      await dlg.waitFor({ state: 'detached' });
      // shown once: not again after a reload, but read in the bell
      await page.reload();
      await idle();
      assert.equal(await page.getByRole('dialog').count(), 0, 'the pop-up comes once');
      const inbox = await call('GET', '/api/v1/me/inbox?limit=5');
      const n = inbox.items.find((x) => x.title === `Answer for ${scheme} ${run}`);
      assert.ok(n && n.popup && n.read_at, 'the pop-up stays, read, in the bell');
    }
  } finally {
    await call('PUT', '/api/v1/scheme', { scheme: '' });
  }
});

test('people: the menu on names and the person page – mouse, keyboard, touch, e-mail rules, guests, narrow and wide, light and dark', async () => {
  const name = `pm${run}`.toLowerCase().slice(0, 30);
  const pw = 'Pers-Passw0rd-1!';
  const display = `Person Menu ${run}`;
  const g = await call('POST', '/api/v1/admin/groups', { name: `Menu ${run}` });
  const u = await call('POST', '/api/v1/admin/users', { username: name, password: pw, display_name: display, email: `${name}@example.com`, groups: [g.id] });
  await call('PUT', `/api/v1/admin/users/${u.id}/profile`, { job_title: 'Keeper of okapis', phone: '+43 1 234 567' });
  const cat = (await call('POST', '/api/v1/categories', { titles: { en: `People ${run}` } })).id;
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'guest', permissions: 1, inherit: true });
  await call('POST', `/api/v1/categories/${cat}/acl`, { subject_type: 'authenticated', permissions: 15, inherit: true });
  // the person on a phone (touch, 390 px)
  const ctx2 = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, bypassCSP: true });
  const p2 = await ctx2.newPage();
  p2.on('pageerror', (e) => errors.push(`people: ${e.message}`));
  const guestCtx = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const size = page.viewportSize();
  const nameButton = (pg, scope) => pg.locator(scope).getByRole('button', { name: new RegExp(`^${display}`) });
  const menuOf = (pg) => pg.getByRole('menu', { name: new RegExp(display) });
  const focused = (pg) => pg.evaluate(() => document.activeElement?.textContent ?? '');
  try {
    await p2.goto(BASE + '/login?local=1');
    await p2.locator('input[name=username]').fill(name);
    await p2.locator('input[type=password]').fill(pw);
    await p2.keyboard.press('Enter');
    await p2.waitForURL((x) => !x.pathname.startsWith('/login'));
    const res = await p2.request.post(`${BASE}/api/v1/elements`, { headers: { 'X-Requested-With': 'ewiki' },
      data: { category_id: cat, type: 'core.html', title: `Okapi notes ${run}`, body: { format: 'html', source: '<p>Okapis</p>' } } });
    assert.equal(res.status(), 201);
    const el = await res.json();

    // the administrator may find everybody: the name is a button with a menu
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    const who = nameButton(page, 'footer.ew-meta');
    assert.equal(await who.getAttribute('aria-haspopup'), 'menu');
    assert.equal(await who.getAttribute('aria-expanded'), 'false');
    await who.click();
    const menu = menuOf(page);
    await menu.waitFor();
    assert.equal(await who.getAttribute('aria-expanded'), 'true');
    const mail = menu.getByRole('menuitem', { name: /^(send e-mail|e-mail senden)$/i });
    assert.equal(await mail.getAttribute('href'), `mailto:${name}@example.com`);
    await a11y('menu on a name');
    // keyboard: the first entry has the focus, the arrows move, Escape closes, the focus returns
    assert.match(await focused(page), /^(open profile|profil öffnen)$/i);
    await page.keyboard.press('ArrowDown');
    assert.match(await focused(page), /^(send e-mail|e-mail senden)$/i);
    await page.keyboard.press('ArrowDown');
    assert.match(await focused(page), /^(open profile|profil öffnen)$/i, 'the arrows wrap around');
    await page.keyboard.press('End');
    assert.match(await focused(page), /^(send e-mail|e-mail senden)$/i);
    await page.keyboard.press('Escape');
    await menu.waitFor({ state: 'detached' });
    assert.equal(await page.evaluate(() => document.activeElement?.classList.contains('ew-person')), true, 'the focus is back on the name');
    assert.equal(await who.getAttribute('aria-expanded'), 'false');
    // Enter opens the menu, Enter on "Open profile" opens the person page
    await page.keyboard.press('Enter');
    await menu.waitFor();
    await page.keyboard.press('Enter');
    await page.waitForURL(new RegExp(`/people/${u.id}$`));
    await idle();
    await page.getByRole('heading', { level: 1, name: display }).waitFor();
    await page.getByText('Keeper of okapis').waitFor();
    assert.equal(await page.getByRole('link', { name: '+43 1 234 567' }).getAttribute('href'), 'tel:+431234567');
    await page.getByRole('link', { name: `${name}@example.com` }).waitFor();
    await a11y('person page');
    // the history names the author with the menu too
    await page.goto(`${BASE}/e/${el.id}/history`);
    await idle();
    await nameButton(page, 'main table').waitFor();

    // the person, on a phone: the own name opens the menu with a tap, inside the window
    await p2.goto(`${BASE}/e/${el.id}`);
    await p2.waitForLoadState('networkidle');
    const own = nameButton(p2, 'footer.ew-meta');
    await own.tap();
    const ownMenu = menuOf(p2);
    await ownMenu.waitFor();
    const box = await ownMenu.boundingBox();
    assert.ok(box && box.x >= 0 && box.x + box.width <= 390, `the menu fits the narrow window: ${JSON.stringify(box)}`);
    await a11y('menu on a name (narrow, touch)', p2);
    await p2.keyboard.press('Escape');
    await ownMenu.waitFor({ state: 'detached' });
    // the person hides the address in the profile
    await p2.goto(`${BASE}/profile`);
    await p2.waitForLoadState('networkidle');
    const card = p2.locator('form.ew-card-form');
    await card.getByRole('checkbox', { name: /^(hide my e-mail address|meine e-mail-adresse verbergen)$/i }).check();
    const saved = p2.waitForResponse((r) => r.url().endsWith('/api/v1/me/profile') && r.request().method() === 'PUT');
    await card.getByRole('button', { name: /^(save|speichern)$/i }).click();
    assert.equal((await saved).status(), 200);
    // nobody else gets the address any more: no "Send e-mail"
    await page.goto(`${BASE}/people/${u.id}`);
    await idle();
    await page.getByRole('heading', { level: 1, name: display }).waitFor();
    assert.equal(await page.locator('a[href^="mailto:"]').count(), 0);
    await page.goto(`${BASE}/e/${el.id}`);
    await idle();
    await nameButton(page, 'footer.ew-meta').click();
    await menuOf(page).waitFor();
    assert.equal(await menuOf(page).getByRole('menuitem').count(), 1, 'only "Open profile"');
    await page.keyboard.press('Escape');

    // dark and wide
    const uiBefore = (await call('GET', '/api/v1/admin/settings')).config.ui;
    await call('PUT', '/api/v1/admin/settings', { ui: { ...uiBefore, template: 'theme.wiki.dark' } });
    await call('PUT', '/api/v1/scheme', { scheme: 'dark' });
    try {
      await page.setViewportSize({ width: 1400, height: 900 });
      await page.goto(`${BASE}/e/${el.id}`);
      await idle();
      await nameButton(page, 'footer.ew-meta').click();
      await menuOf(page).waitFor();
      await a11y('menu on a name (dark)');
      await page.keyboard.press('Escape');
      await page.goto(`${BASE}/people/${u.id}`);
      await idle();
      await a11y('person page (dark)');
    } finally {
      await call('PUT', '/api/v1/scheme', { scheme: '' });
      await call('PUT', '/api/v1/admin/settings', { ui: uiBefore });
    }

    // a guest sees the name as text and no person
    const guest = await guestCtx.newPage();
    await guest.goto(`${BASE}/e/${el.id}`);
    await guest.waitForLoadState('networkidle');
    await guest.locator('footer.ew-meta').getByText(display).waitFor();
    assert.equal(await guest.locator('.ew-person').count(), 0);
    await guest.goto(`${BASE}/people/${u.id}`);
    await guest.getByRole('heading', { level: 1, name: /^(please log in|bitte anmelden)$/i }).waitFor();
    assert.equal(await guest.getByText('Keeper of okapis').count(), 0);

    // the settings offer who sees addresses
    await page.goto(`${BASE}/admin/settings`);
    await idle();
    assert.equal(await page.getByLabel(/^(who sees e-mail addresses of people|wer e-mail-adressen von personen sieht)/i).inputValue(), 'users');
    assert.equal(await page.getByRole('checkbox', { name: /^(people may hide their own address|personen dürfen ihre eigene adresse verbergen)/i }).isChecked(), true);
  } finally {
    if (size) await page.setViewportSize(size);
    await ctx2.close();
    await guestCtx.close();
  }
});

test('no JavaScript errors occurred', () => {
  assert.deepEqual(errors, []);
});
