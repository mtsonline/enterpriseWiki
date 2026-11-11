// Browser end-to-end tests: real user flows in Chromium plus automated WCAG
// 2.2 AA checks with axe-core.
//
//   EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=... npm run test:e2e
//
// The server must have been set up with "ewiki setup" (welcome space).
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createHmac } from 'node:crypto';

const BASE = process.env.EWIKI_E2E_URL ?? 'http://localhost:8080';
const USER = process.env.EWIKI_E2E_USER ?? 'admin';
const PASSWORD = process.env.EWIKI_E2E_PASSWORD ?? 'Admin-Passw0rd!';
const REQUIRE_AXE = process.env.EWIKI_E2E_REQUIRE_AXE === '1';
const run = Date.now().toString(36);

let axeSource = null;
try {
  axeSource = (await import('axe-core')).default.source;
} catch {
  if (REQUIRE_AXE) throw new Error('axe-core is required (npm install)');
}

let browser, page;
const errors = [];

before(async () => {
  browser = await chromium.launch();
  // bypassCSP: the strict CSP of the wiki (script-src 'self') would block injecting axe-core
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error' && !/status of 40[134]/.test(m.text())) errors.push(m.text()); });
});
after(async () => { await browser?.close(); });

async function a11y(name, pg = page) {
  if (!axeSource) return;
  // transient toasts (fixed, bottom right) may cover controls: wait until they are gone
  await pg.waitForFunction(() => !document.querySelector('.ew-toasts > *'), null, { timeout: 15000 }).catch(() => undefined);
  // Playwright scrolls controls into view without scroll-padding, which can leave
  // content half below the sticky header in the snapshot: check from the top
  await pg.evaluate(() => window.scrollTo(0, 0));
  await pg.addScriptTag({ content: axeSource });
  const res = await pg.evaluate(async () => {
    const r = await globalThis.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] });
    // details for the report: what axe says and which element is on top of the node
    for (const v of r.violations) for (const n of v.nodes) {
      const el = document.querySelector(n.target.join(' '));
      const b = el?.getBoundingClientRect();
      const top = b ? document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2) : null;
      n.detail = `${(n.failureSummary ?? '').replace(/\s+/g, ' ').slice(0, 300)}${b ? ` [box ${Math.round(b.x)},${Math.round(b.y)} ${Math.round(b.width)}x${Math.round(b.height)}, on top: ${top?.outerHTML.slice(0, 120) ?? '-'}]` : ''}`;
    }
    return r;
  });
  const v = res.violations.map((x) => `${x.id} (${x.impact}): ${x.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(', ')}`);
  const details = res.violations.flatMap((x) => x.nodes.slice(0, 3).map((n) => `  ${n.target.join(' ')}: ${n.detail}`));
  assert.deepEqual(v, [], `accessibility violations on ${name}:\n${v.join('\n')}\n${details.join('\n')}`);
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
});

test('create a subcategory: the current category is preselected as parent', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  for (const section of ['users', 'groups', 'roles', 'plugins', 'updates', 'settings', 'languages', 'audit', 'permissions', 'watches', 'menus', 'ldap', 'oidc', 'shibboleth', 'shell', 'mail', 'search', 'blocks', 'sites']) {
    await page.goto(`${BASE}/admin/${section}`);
    await idle();
    await page.waitForSelector('.ew-admin-panel h1');
    await a11y(`admin/${section}`);
  }
  await page.goto(`${BASE}/profile`);
  await idle();
  await a11y('profile');
  await page.goto(`${BASE}/trash`);
  await idle();
  await a11y('trash');
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

async function newPage(type, title) {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-tree a').first().click();
  await idle();
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  await page.getByRole('button', { name: /^(insert macro|makro einfügen)$/i }).click();
  const dialog = page.locator('dialog[open]');
  await dialog.waitFor();
  await dialog.locator('select').first().selectOption('panel');
  await dialog.locator('select').nth(1).selectOption('warning');
  await dialog.getByLabel(/^(title|titel)/i).fill('Careful');
  await a11y('macro dialog');
  await dialog.locator('button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="panel"]');
  // the placeholder text of the body is selected: typing replaces it
  await page.keyboard.type('Panel body');
  // the edit button of the macro opens the dialog for this macro
  await page.locator('.ewe-macro[data-macro="panel"]').getByRole('button', { name: /^(edit macro|makro bearbeiten)/i }).click();
  assert.ok(await page.locator('dialog[open] select').first().isDisabled(), 'existing macro is edited');
  await page.locator('dialog[open]').getByLabel(/^(title|titel)/i).fill('Very careful');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  // below the panel: insert a second macro
  await page.locator('.ewe-prose > p').first().click();
  await page.keyboard.press('Control+End');
  await page.getByRole('button', { name: /^(insert macro|makro einfügen)$/i }).click();
  await page.locator('dialog[open] select').first().selectOption('toc');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.waitForSelector('.ewe-prose .ewe-macro[data-macro="toc"]');
  // the property report with its own controls (sources, filters, columns)
  await page.keyboard.press('Control+End');
  await page.getByRole('button', { name: /^(insert macro|makro einfügen)$/i }).click();
  await page.locator('dialog[open] select').first().selectOption('properties');
  await page.locator('dialog[open]').getByRole('button', { name: /^(add filter|filter hinzufügen)$/i }).waitFor();
  await page.locator('dialog[open]').getByRole('button', { name: /^(add source|quelle hinzufügen)$/i }).click();
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
  await page.getByRole('button', { name: /^markdown$/i }).click();
  const src = page.locator('textarea.ewe-markdown');
  const md = await src.inputValue();
  assert.match(md, /## Section/);
  assert.match(md, /- \[x\] open task\n- \[ \] second/);
  assert.match(md, /\| Head \| Second \|/);
  await src.fill(md + '\nAdded in *Markdown*\n');
  await a11y('editor in Markdown mode');
  await page.getByRole('button', { name: /^markdown$/i }).click();
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
    await page.getByRole('button', { name: /^markdown$/i }).click();
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
    await p2.getByRole('button', { name: /^markdown$/i }).click();
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
    const me2 = await p2.evaluate(async () => (await (await fetch('/api/v1/auth/me')).json()).user);
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
      const r = await fetch('/api/v1/me/mfa/totp/begin', { method: 'POST', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': 'application/json' }, body: '{}' });
      return (await r.json()).secret;
    });
    const enabled = await p2.evaluate(async (code) => (await fetch('/api/v1/me/mfa/totp/finish', { method: 'POST', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) })).status, totpCode(secret));
    assert.equal(enabled, 204);
    // a passkey with a virtual authenticator of the browser
    const cdp = await ctx2.newCDPSession(p2);
    await cdp.send('WebAuthn.enable');
    await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
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
    await p2.evaluate(() => fetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-Requested-With': 'ewiki' } }));
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
    // the passkey: sign-in without password
    await p2.evaluate(() => fetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-Requested-With': 'ewiki' } }));
    await p2.goto(BASE + '/login');
    await p2.getByRole('button', { name: /^(sign in with a passkey|mit passkey anmelden)$/i }).click();
    await p2.waitForURL((u) => !u.pathname.startsWith('/login'));
    const me = await p2.evaluate(async () => (await (await fetch('/api/v1/auth/me')).json()).user?.username);
    assert.equal(me, name);
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
  await dlg.getByRole('checkbox', { name: 'hr' }).check();
  await dlg.getByRole('button', { name: /^(check recipients|empfänger prüfen)$/i }).click();
  await dlg.getByText(/recipient|empfänger/i).first().waitFor();
  await a11y('share dialog');
  await dlg.getByRole('button', { name: /close|schließen/i }).first().click();
  // the bell opens the notification panel
  await page.getByRole('button', { name: /^(notifications|benachrichtigungen)/i }).click();
  await page.locator('.ew-inbox').waitFor();
  await a11y('notification panel');
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
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  // block in the footer
  await page.goto(BASE + '/admin/blocks');
  await idle();
  await page.getByRole('button', { name: /^(add block|block hinzufügen)$/i }).click();
  dlg = page.locator('dialog[open]');
  await dlg.waitFor();
  await dlg.getByLabel(/^name/i).fill(`Contact ${run}`);
  await dlg.locator('select').first().selectOption('footer');
  await dlg.locator('textarea').first().fill(`**Contact** block${run}`);
  await a11y('block dialog');
  await dlg.locator('button[type=submit]').click();
  await page.waitForSelector(`.ew-pos--footer strong:has-text("Contact")`);
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
    const cur = await (await fetch('/api/v1/admin/settings', { headers: h })).json();
    const res = await fetch('/api/v1/admin/settings', { method: 'PUT', headers: h, body: JSON.stringify({ ui: { ...cur.config.ui, template: tpl } }) });
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
  const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: body === undefined ? undefined : JSON.stringify(body) });
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
    await call('POST', `/api/v1/x/content.blog/elements/${blog.id}/posts`, { title: `Post ${n} ${run}`, body: { format: 'html', source: `<p>About giraffes ${n}</p>` }, tags: [`tag${n % 2}`] });
  }
  await page.goto(BASE + `/e/${blog.id}`);
  await idle();
  await page.locator('.ewb-toolbar').waitFor();
  assert.equal(await page.locator('.ewb-post').count(), 2);
  await a11y('blog');
  await page.getByRole('button', { name: /older posts|ältere beiträge/i }).click();
  await page.waitForFunction(() => document.querySelectorAll('.ewb-post').length === 1);
  await page.getByRole('searchbox', { name: /search the blog|im blog suchen/i }).fill('giraffes');
  await page.locator('.ewb-search button[type=submit]').click();
  await page.waitForFunction(() => document.querySelectorAll('.ewb-hit').length === 3);
  await page.getByRole('button', { name: /^(new post|neuer beitrag)$/i }).click();
  await page.waitForURL(/\/c\/\d+\/new\?type=core\.html&return=/);
});

test('FAQ: sections, questions, filter and quick add', async () => {
  const cat = await m6Category();
  const faqCat = await call('POST', '/api/v1/categories', { parent_id: cat, titles: { en: `Help ${run}` } });
  const faq = await call('POST', '/api/v1/elements', { category_id: cat, type: 'content.faq', title: `FAQ ${run}`,
    body: { category_id: faqCat.id, recursive: true, search: true, feed: true } });
  const sec = await call('POST', `/api/v1/x/content.faq/elements/${faq.id}/categories`, { titles: { en: `Accounts ${run}` } });
  await call('POST', `/api/v1/x/content.faq/elements/${faq.id}/questions`, { title: 'How do I reset my password?', category_id: sec.id, body: { format: 'markdown', source: 'Use the **reset** link.' } });
  await call('POST', `/api/v1/x/content.faq/elements/${faq.id}/questions`, { title: 'Who helps me?', body: { format: 'markdown', source: 'The help desk.' } });
  await page.goto(BASE + `/e/${faq.id}`);
  await idle();
  await page.locator('.ewq-toolbar').waitFor();
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
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  await item.locator('.ew-picker-list').selectOption({ index: 0 });
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
  await page.goto(BASE + '/admin/elements');
  await idle();
  await page.getByRole('searchbox', { name: /search element|element suchen/i }).fill(`Shorty ${run}`);
  await page.waitForFunction((n) => document.querySelectorAll('.ew-admin-results tbody tr').length === 1 && document.querySelector('.ew-admin-results tbody')?.textContent?.includes(n), `Shorty ${run}`);
  await a11y('elements administration');
  await page.getByRole('button', { name: new RegExp(`^(properties|eigenschaften): Shorty ${run}`, 'i') }).click();
  const dlg = page.locator('dialog[open]');
  await dlg.getByLabel(/^(short url|kurz-url)/i).fill(`Kurz ${run}`);
  await dlg.getByLabel(/^(preview image|vorschaubild)/i).fill('/favicon.svg');
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
  assert.equal(saved.preview_image, '/favicon.svg');
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
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  const edit = page.locator('.ew-pos--header-tools button[aria-pressed]');
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
  for (const section of ['carousels', 'reactions', 'favorites']) {
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
  const site = await page.evaluate(async () => (await fetch('/api/v1/site')).json());
  assert.equal(site.site, name, 'site of the host name');
  // pages of the main site do not exist here
  const hits = async (origin) => page.evaluate(async (u) => (await (await fetch(u, { credentials: 'include' })).json()).elements?.length ?? 0, `${origin}/api/v1/search?q=quokka${run}`);
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
  await details.getByRole('button', { name: /^(Close|Schließen)$/ }).first().click();
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
    const cats = await (await fetch('/api/v1/categories')).json();
    const fd = new FormData();
    fd.append('meta', JSON.stringify({ category_id: cats[0].id, title: name }));
    fd.append('file', new File(['PK not really a docx'], `${name}.docx`));
    const r = await fetch('/api/v1/media', { method: 'POST', body: fd, headers: { 'X-Requested-With': 'ewiki' } });
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
  await page.locator('.ew-macro--toc').first().waitFor();
  await a11y('demo tour page');
  // the overview of the demo area and the team page
  for (const name of [/^(Team)$/, /^(News|Neuigkeiten)$/]) {
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
  while (queue.length && seen.size < 60) {
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
  assert.ok(seen.size > 10, `demo pages visited: ${seen.size}`);
  for (const p of [...editors].slice(0, 25)) {
    await page.goto(BASE + p);
    await idle();
    const h1 = (await page.locator('main h1').first().textContent().catch(() => '')) ?? '';
    if (errorTitle.test(h1.trim())) failed.push(`${p}: ${h1}`);
  }
  assert.deepEqual(failed, [], 'demo pages with errors');
  await page.goto(`${BASE}/admin/demo`);
  await idle();
  await page.getByRole('button', { name: /^(Remove demo|Demo entfernen)$/ }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^(Remove demo|Demo entfernen)$/ }).click();
  await page.getByRole('button', { name: /^(Create demo|Demo anlegen)$/ }).waitFor();
  const res = await page.evaluate(async () => (await fetch('/api/v1/resolve?path=/demo')).status);
  assert.equal(res, 404);
});

test('no JavaScript errors occurred', () => {
  assert.deepEqual(errors, []);
});
