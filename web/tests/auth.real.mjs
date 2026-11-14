// Browser test of the sign-in with a real identity provider: a person opens
// the sign-in page of the wiki, chooses "Sign in with Keycloak", signs in at
// Keycloak and later signs out again (single logout). With axe checks of the
// pages of the wiki. With OpenLDAP also the directory sync on the admin
// page: preview, sync, the list of runs.
//
//   eval "$(tests/auth/start.sh)"
//   cd web && EWIKI_E2E_URL=http://localhost:8080 npm run test:auth
//
// The wiki must have been set up with "ewiki setup" and use EWIKI_E2E_URL as
// its base URL. The test changes its OIDC settings.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { chromium } from 'playwright';

const BASE = process.env.EWIKI_E2E_URL ?? 'http://localhost:8080';
const ADMIN = process.env.EWIKI_E2E_USER ?? 'admin';
const ADMIN_PW = process.env.EWIKI_E2E_PASSWORD ?? 'Admin-Passw0rd!';
const KC = process.env.EWIKI_TEST_KEYCLOAK_URL;
if (!KC || !process.env.EWIKI_TEST_KEYCLOAK_PASSWORD) throw new Error('start the sign-in systems first: eval "$(tests/auth/start.sh)"');
const axeSource = (await import('axe-core')).default.source;
const realm = `ewiki-ui-${randomBytes(4).toString('hex')}`;
const userPW = randomBytes(12).toString('hex');

let browser, page, kcToken;
const errors = [];

async function kc(method, path, body) {
  const res = await fetch(KC + path, {
    method, headers: { Authorization: `Bearer ${kcToken}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`Keycloak ${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}

async function a11y(name) {
  await page.addScriptTag({ content: axeSource });
  const res = await page.evaluate(() => globalThis.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] }));
  const v = res.violations.map((x) => `${x.id}: ${x.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(', ')}`);
  assert.deepEqual(v, [], `accessibility violations on ${name}`);
}

before(async () => {
  const tok = await fetch(`${KC}/realms/master/protocol/openid-connect/token`, {
    method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ grant_type: 'password', client_id: 'admin-cli', username: process.env.EWIKI_TEST_KEYCLOAK_ADMIN, password: process.env.EWIKI_TEST_KEYCLOAK_PASSWORD }),
  }).then((r) => r.json());
  kcToken = tok.access_token;
  assert.ok(kcToken, 'Keycloak admin token');
  await kc('POST', '/admin/realms', { realm, enabled: true });
  await kc('POST', `/admin/realms/${realm}/users`, {
    username: 'frida', email: 'frida@example.com', emailVerified: true, enabled: true, firstName: 'Frida', lastName: 'Example',
    credentials: [{ type: 'password', value: userPW, temporary: false }],
  });
  const secret = randomBytes(16).toString('hex');
  await kc('POST', `/admin/realms/${realm}/clients`, {
    clientId: 'wiki-ui', protocol: 'openid-connect', publicClient: false, secret, standardFlowEnabled: true,
    redirectUris: [`${BASE}/api/v1/auth/sso/oidc/callback`],
    attributes: { 'post.logout.redirect.uris': `${BASE}/*`, 'backchannel.logout.url': `${BASE}/api/v1/auth/sso/oidc/logout` },
  });

  browser = await chromium.launch();
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  page.on('pageerror', (e) => errors.push(e.message));

  // the administration connects the wiki to the realm (as a person would on the settings page)
  await page.goto(BASE + '/');
  const api = (method, path, body) => page.evaluate(async ([method, path, body]) => {
    const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: body === undefined ? undefined : JSON.stringify(body) });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
    return text ? JSON.parse(text) : null;
  }, [method, path, body]);
  await api('POST', '/api/v1/auth/login', { username: ADMIN, password: ADMIN_PW });
  const st = await api('GET', '/api/v1/admin/settings');
  const settings = st.config.plugin_settings ?? {};
  settings['auth.oidc'] = { name: 'Keycloak', issuer: `${KC}/realms/${realm}`, client_id: 'wiki-ui', client_secret: secret };
  await api('PUT', '/api/v1/admin/settings', { plugin_settings: settings });
  await api('POST', '/api/v1/auth/logout');
  await page.context().clearCookies();
});

after(async () => {
  await browser?.close();
  if (kcToken) await kc('DELETE', `/admin/realms/${realm}`).catch(() => undefined);
});

test('sign in with a real identity provider and sign out again', async () => {
  await page.goto(BASE + '/login?next=/search');
  const sso = page.getByRole('link', { name: /sign in with keycloak|anmelden mit keycloak/i })
    .or(page.getByRole('button', { name: /sign in with keycloak|anmelden mit keycloak/i }));
  await sso.waitFor();
  await a11y('sign-in page with an identity provider');
  await sso.click();

  // Keycloak's own sign-in page
  await page.waitForURL((u) => u.href.startsWith(KC));
  await page.locator('#username').fill('frida');
  await page.locator('#password').fill(userPW);
  await page.locator('#kc-login').click();

  // back in the wiki, signed in, at the page asked for
  await page.waitForURL((u) => u.href.startsWith(BASE) && new URL(u).pathname === '/search');
  await page.getByRole('button', { name: /frida/i }).waitFor();
  const me = await page.evaluate(() => fetch('/api/v1/auth/me').then((r) => r.json()));
  assert.equal(me.user?.username, 'frida');
  assert.equal(me.user?.provider, 'oidc');
  await a11y('wiki after the sign-in');

  // signing out ends the session in the wiki and at Keycloak
  await page.getByRole('button', { name: /frida/i }).click();
  await page.getByRole('button', { name: /^(log out|abmelden)$/i }).click();
  await page.waitForURL((u) => u.href.startsWith(BASE));
  await page.getByRole('link', { name: /^(log in|anmelden)$/i }).first().waitFor();
  const after = await page.evaluate(() => fetch('/api/v1/auth/me').then((r) => r.json()));
  assert.equal(after.user, null);
  const users = await kc('GET', `/admin/realms/${realm}/users?username=frida&exact=true`);
  const sessions = await kc('GET', `/admin/realms/${realm}/users/${users[0].id}/sessions`);
  assert.deepEqual(sessions, [], 'the session at Keycloak must end too');

  // signing in again needs the password again (no silent sign-in after single logout)
  await page.goto(BASE + '/login');
  await sso.click();
  await page.waitForURL((u) => u.href.startsWith(KC));
  await page.locator('#password').waitFor();
  assert.deepEqual(errors, []);
});

test('directory sync on the admin page (OpenLDAP)', { skip: !process.env.EWIKI_TEST_LDAP_URL && 'no OpenLDAP' }, async () => {
  await page.context().clearCookies();
  await page.goto(BASE + '/');
  const api = (method, path, body) => page.evaluate(async ([method, path, body]) => {
    const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: body === undefined ? undefined : JSON.stringify(body) });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
    return text ? JSON.parse(text) : null;
  }, [method, path, body]);
  await api('POST', '/api/v1/auth/login', { username: ADMIN, password: ADMIN_PW });
  const st = await api('GET', '/api/v1/admin/settings');
  const settings = st.config.plugin_settings ?? {};
  settings['auth.ldap'] = {
    name: 'Directory', url: process.env.EWIKI_TEST_LDAP_URL, bind_dn: process.env.EWIKI_TEST_LDAP_BIND_DN,
    bind_password: process.env.EWIKI_TEST_LDAP_BIND_PASSWORD, base_dn: 'ou=people,dc=example,dc=com', user_filter: '(uid={username})',
    attr_subject: 'entryUUID', sync_max_disable_percent: 100,
  };
  await api('PUT', '/api/v1/admin/settings', { plugin_settings: settings });

  await page.goto(BASE + '/admin/ldap');
  const panel = page.getByRole('region', { name: /directory sync|verzeichnis-abgleich/i });
  await panel.waitFor();
  // the preview shows what would happen, without changing anything
  await panel.getByRole('button', { name: /^(preview|vorschau)$/i }).click();
  const result = panel.getByRole('status');
  await result.getByText(/nothing was changed|es wurde nichts geändert/i).waitFor();
  await panel.getByRole('table', { name: /^(changes|änderungen)$/i }).getByRole('cell', { name: 'alice', exact: true }).first().waitFor();
  const before = await api('GET', '/api/v1/admin/users?q=alice');
  assert.ok(!JSON.stringify(before).includes('"provider":"ldap"'), 'the preview created an account');
  await a11y('directory sync preview');

  // the real run; afterwards the runs list it
  await panel.getByRole('button', { name: /^(sync now|jetzt abgleichen)$/i }).click();
  await result.getByText(/sync finished|abgleich abgeschlossen/i).waitFor();
  await panel.getByRole('table', { name: /recent syncs|letzte abgleiche/i }).getByRole('cell', { name: /^(sync|abgleich)$/i }).first().waitFor();
  const users = await api('GET', '/api/v1/admin/users?q=alice');
  assert.ok(JSON.stringify(users).includes('"provider":"ldap"'), 'the sync created no account for alice');
  await a11y('directory sync result');
  assert.deepEqual(errors, []);
});
