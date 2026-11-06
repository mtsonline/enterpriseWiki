// Browser test against real office servers (scripts/test-docker-office.sh):
// open a document in Collabora Online and in OnlyOffice Docs, edit, save,
// and check that the wiki stored a new version.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const BASE = process.env.EWIKI_OFFICE_URL ?? 'http://wiki.test:8080';
const USER = process.env.EWIKI_OFFICE_USER ?? 'admin';
const PASSWORD = process.env.EWIKI_OFFICE_PASSWORD ?? '';

let browser, page;
const errors = [];

// ---------------------------------------------------------------- a minimal .docx (stored zip)
const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crc32 = (b) => { let c = 0xffffffff; for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
function zip(files) {
  const parts = []; const central = []; let offset = 0;
  for (const [name, text] of Object.entries(files)) {
    const data = Buffer.from(text); const n = Buffer.from(name); const crc = crc32(data);
    const local = Buffer.alloc(30); local.writeUInt32LE(0x04034b50, 0); local.writeUInt16LE(20, 4);
    local.writeUInt32LE(crc, 14); local.writeUInt32LE(data.length, 18); local.writeUInt32LE(data.length, 22); local.writeUInt16LE(n.length, 26);
    parts.push(local, n, data);
    const c = Buffer.alloc(46); c.writeUInt32LE(0x02014b50, 0); c.writeUInt16LE(20, 4); c.writeUInt16LE(20, 6);
    c.writeUInt32LE(crc, 16); c.writeUInt32LE(data.length, 20); c.writeUInt32LE(data.length, 24); c.writeUInt16LE(n.length, 28); c.writeUInt32LE(offset, 42);
    central.push(c, n);
    offset += 30 + n.length + data.length;
  }
  const cd = Buffer.concat(central);
  const end = Buffer.alloc(22); end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(central.length / 2, 8); end.writeUInt16LE(central.length / 2, 10);
  end.writeUInt32LE(cd.length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...parts, cd, end]);
}
const docx = (text) => zip({
  '[Content_Types].xml': '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
  '_rels/.rels': '<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
  'word/document.xml': `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>${text}</w:t></w:r></w:p></w:body></w:document>`,
});

// ---------------------------------------------------------------- helpers
const api = async (method, path, body) => {
  const r = await page.request.fetch(BASE + path, { method, data: body, headers: { 'X-Requested-With': 'ewiki' } });
  if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`);
  return r.json();
};
async function upload(name) {
  const cats = await api('GET', '/api/v1/categories');
  const r = await page.request.post(BASE + '/api/v1/media', {
    headers: { 'X-Requested-With': 'ewiki' },
    multipart: {
      meta: JSON.stringify({ category_id: cats[0].id, title: name }),
      file: { name: `${name}.docx`, mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', buffer: docx(`Hello from ${name}`) },
    },
  });
  assert.equal(r.status(), 201, await r.text());
  return (await r.json()).id;
}
async function waitVersion(id, want, seconds) {
  for (let i = 0; i < seconds; i += 2) {
    const m = await api('GET', `/api/v1/media/${id}`);
    if (m.version >= want) return m;
    await page.waitForTimeout(2000);
  }
  throw new Error(`medium ${id} has no version ${want} after ${seconds}s`);
}

// on failure: a screenshot for the CI artifacts
async function shot(name, fn) {
  try {
    await fn();
  } catch (e) {
    await page.screenshot({ path: `/tmp/office-${name}.png`, fullPage: true }).catch(() => undefined);
    throw e;
  }
}

before(async () => {
  browser = await chromium.launch({ args: ['--host-resolver-rules=MAP wiki.test 127.0.0.1, MAP office.test 127.0.0.1, MAP docs.test 127.0.0.1'] });
  page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(BASE + '/login');
  await page.getByLabel(/user ?name|benutzername/i).fill(USER);
  await page.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
  await page.locator('form.ew-login button[type=submit]').click();
  await page.waitForSelector('.ew-user-menu');
});
after(async () => { await browser?.close(); });

test('connection tests of both plugins', async () => {
  const c = await api('POST', '/api/v1/x/office.collabora/test');
  assert.ok(c.ok && c.proof_key && c.edit.includes('docx'), JSON.stringify(c));
  const o = await api('POST', '/api/v1/x/office.onlyoffice/test');
  assert.ok(o.ok && o.secret, JSON.stringify(o));
});

test('Collabora: open, save through WOPI (proof keys), new version', async () => {
  const id = await upload('collabora');
  // the page reports Collabora's PostMessage status to the test
  await page.goto(`${BASE}/office/${id}?viewer=office.collabora&mode=edit`);
  const frame = page.locator('iframe.ew-office-frame');
  await frame.waitFor();
  await shot('collabora', () => page.locator('html[data-office-ready]').waitFor({ state: 'attached', timeout: 120000 }));
  // save as the host page would (PostMessage API), also without changes
  await page.evaluate(() => {
    const f = document.querySelector('iframe.ew-office-frame');
    // test only: the frame's origin is office.test (any origin is fine here)
    f.contentWindow.postMessage(JSON.stringify({ MessageId: 'Action_Save', SendTime: Date.now(), Values: { DontTerminateEdit: true, DontSaveIfUnmodified: false, Notify: true } }), '*');
  });
  const m = await waitVersion(id, 2, 90);
  assert.equal(m.version, 2);
  const vers = await api('GET', `/api/v1/media/${id}/versions`);
  assert.equal(vers[0].note, 'Collabora Online');
});

test('OnlyOffice: open with the API (JWT), edit, close, saved by the callback', async () => {
  const id = await upload('onlyoffice');
  await page.goto(`${BASE}/office/${id}?viewer=office.onlyoffice&mode=edit`);
  await shot('onlyoffice', () => page.locator('html[data-office-ready]').waitFor({ state: 'attached', timeout: 240000 }));
  // type into the document: click into the editor, then write
  const box = await page.locator('iframe[name=frameEditor]').boundingBox();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.waitForTimeout(1000);
  await page.keyboard.type(' edited in OnlyOffice');
  await page.waitForTimeout(5000);
  // leaving the editor: the Document Server saves ~10 s after the last user left
  await page.goto(`${BASE}/media`);
  const m = await waitVersion(id, 2, 120);
  assert.equal(m.version, 2);
});

test('no JavaScript errors in the wiki', () => {
  assert.deepEqual(errors, []);
});
