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
  for (const section of ['users', 'groups', 'roles', 'plugins', 'updates', 'settings', 'languages', 'audit', 'menus', 'ldap', 'oidc', 'shibboleth', 'shell', 'mail', 'search', 'blocks']) {
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
  let dlg = page.locator('dialog[open]');
  await dlg.waitFor();
  await dlg.getByLabel(/^(menu name|menüname)/i).fill(`Menu ${run}`);
  await dlg.locator('select').first().selectOption('sidebar-bottom');
  await dlg.locator('.ewm-labels summary').first().click();
  await dlg.locator('fieldset input[lang]').first().fill(`Item ${run}`);
  await a11y('menu dialog');
  await dlg.locator('button[type=submit]').click();
  // a target is required
  await page.waitForFunction(() => /target|ziel/i.test(document.querySelector('dialog[open] .ew-form-alert')?.textContent ?? ''));
  await dlg.locator('.ewm-target select').first().selectOption({ index: 1 });
  await dlg.locator('button[type=submit]').click();
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
  } finally {
    await page.setViewportSize({ width: 1280, height: 900 });
    await set('theme.confluence');
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

test('guest cache administration', async () => {
  await page.goto(BASE + '/admin/cache');
  await idle();
  await page.getByRole('button', { name: /clear cache|cache leeren/i }).waitFor();
  await a11y('cache administration');
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

test('no JavaScript errors occurred', () => {
  assert.deepEqual(errors, []);
});
