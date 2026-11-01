// Browser end-to-end tests: real user flows in Chromium plus automated WCAG
// 2.2 AA checks with axe-core.
//
//   EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=... npm run test:e2e
//
// The server must have been set up with "ewiki setup" (welcome space).
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

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

async function a11y(name) {
  if (!axeSource) return;
  await page.addScriptTag({ content: axeSource });
  const res = await page.evaluate(async () => await globalThis.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] }));
  const v = res.violations.map((x) => `${x.id} (${x.impact}): ${x.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(', ')}`);
  assert.deepEqual(v, [], `accessibility violations on ${name}:\n${v.join('\n')}`);
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
  const ed = page.locator('.ewh-editable');
  await ed.click();
  await page.keyboard.type(`Hello quokka${run} world`);
  await page.getByRole('button', { name: /^bold$|^fett$/i }).click();
  await page.keyboard.type(' bold');
  await a11y('page editor');
  await page.getByRole('button', { name: /^(create|erstellen)$/i }).click();
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
  await page.locator('.ewh-editable').fill(`Hallo quokka${run} Welt`);
  await page.getByRole('button', { name: /^(save|speichern)$/i }).click();
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
  for (const section of ['users', 'groups', 'roles', 'plugins', 'updates', 'settings', 'languages', 'audit', 'menus', 'ldap', 'oidc', 'shibboleth', 'shell']) {
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
  await page.getByRole('button', { name: /^(create|erstellen)$/i }).click();
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
  await page.getByRole('button', { name: /set up two-factor|zwei-faktor-authentifizierung einrichten/i }).click();
  await page.locator('dialog[open]').waitFor();
  assert.match(await page.locator('dialog[open] .ew-secret').innerText(), /^[A-Z2-7 ]{20,}$/);
  await a11y('2FA setup dialog');
  await page.keyboard.press('Escape');
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
  const ed = page.locator('.ewh-editable');
  await ed.click();
  await page.keyboard.type('Intro text');
  await page.getByRole('button', { name: /^(macro|makro)$/i }).click();
  const dialog = page.locator('dialog[open]');
  await dialog.waitFor();
  await dialog.locator('select').first().selectOption('panel');
  await dialog.locator('select').nth(1).selectOption('warning');
  await dialog.getByLabel(/^(title|titel)/i).fill('Careful');
  await a11y('macro dialog');
  await dialog.locator('button[type=submit]').click();
  await page.waitForSelector('.ewh-editable [data-macro="panel"]');
  // with the cursor inside the panel, the dialog edits the panel
  await page.getByRole('button', { name: /^(macro|makro)$/i }).click();
  assert.ok(await page.locator('dialog[open] select').first().isDisabled(), 'existing macro is edited');
  await page.locator('dialog[open]').getByLabel(/^(title|titel)/i).fill('Very careful');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.locator('dialog[open]').waitFor({ state: 'detached' });
  // below the panel: insert a second macro
  await page.locator('.ewh-editable').click();
  await page.keyboard.press('Control+End');
  await page.getByRole('button', { name: /^(macro|makro)$/i }).click();
  await page.locator('dialog[open] select').first().selectOption('toc');
  await page.locator('dialog[open] button[type=submit]').click();
  await page.waitForSelector('.ewh-editable [data-macro="toc"]');
  await page.getByRole('button', { name: /^(create|erstellen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  assert.ok(await page.locator('.ew-content .ew-macro-panel--warning').count() === 1, 'panel rendered');
  assert.match(await page.locator('.ew-macro-panel').innerText(), /Very careful/);
  assert.ok(await page.locator('.ew-content .ew-toc, .ew-content .ew-macro--toc').count() === 1, 'toc rendered');
  await a11y('page with macros');
});

test('workflow: HR application from template, start and handle a case', async () => {
  await newPage('workflow.process', `Application ${run}`);
  await page.locator('.ewf-template select').selectOption('hr');
  await page.getByRole('button', { name: /use template|vorlage verwenden/i }).click();
  await page.waitForFunction(() => document.querySelectorAll('.ewf-step-row').length >= 4);
  await a11y('workflow editor');
  await page.getByRole('button', { name: /^(create|erstellen)$/i }).click();
  await page.waitForURL(/\/e\/\d+$/);
  await idle();
  await page.waitForSelector('.ewf-cases');
  await a11y('workflow page');
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
  } finally {
    await page.setViewportSize({ width: 1280, height: 900 });
    await set('theme.confluence');
  }
});

test('switch language', async () => {
  await page.goto(BASE + '/');
  await idle();
  await page.locator('.ew-lang select').selectOption('de');
  await page.waitForFunction(() => document.documentElement.lang === 'de');
  assert.equal(await page.locator('a.ew-skip').textContent(), 'Zum Inhalt springen');
  assert.ok(await page.getByRole('button', { name: 'Suchen' }).count() > 0, 'header widgets translated');
  assert.equal(await page.evaluate(() => document.activeElement?.closest('.ew-lang') !== null), true, 'focus stays on the language selector');
});

test('no JavaScript errors occurred', () => {
  assert.deepEqual(errors, []);
});
