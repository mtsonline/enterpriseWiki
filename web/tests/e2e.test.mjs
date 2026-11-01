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
  for (const section of ['users', 'groups', 'roles', 'plugins', 'settings', 'languages', 'audit', 'menus']) {
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
