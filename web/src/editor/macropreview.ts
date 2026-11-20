// Live pictures of macros in the visual editor: a macro without a body (a
// button group, a counter, a page tree …) shows what the page will show,
// rendered by the server with the rights of the editor (POST
// /api/v1/elements/preview). The macros waiting for a picture are rendered
// together in one request; equal macros are rendered once per page load.
import { api } from '../core/api';
import { uiLang } from '../core/i18n';

type Done = (html: string | null) => void;

const cache = new Map<string, string>();
const waiting = new Map<string, Done[]>();
let timer: ReturnType<typeof setTimeout> | null = null;
/** at most this many macros in one request (the server limits macros per page) */
const BATCH = 40;

/**
 * Asks for the picture of a macro given as Markdown (::name{…}). done gets
 * the sanitized HTML of the server, or null if there is none (an error, or
 * the server is not reachable); it may be called right away from the cache.
 */
export function macroPicture(src: string, done: Done): void {
  const hit = cache.get(src);
  if (hit !== undefined) { done(hit); return; }
  const list = waiting.get(src);
  if (list) { list.push(done); return; }
  waiting.set(src, [done]);
  timer ??= setTimeout(flush, 250);
}

/** a paragraph between the macros that tells their pictures apart */
const SEP = 'ewe-picture-separator-7f3a';

async function flush(): Promise<void> {
  timer = null;
  const batch = [...waiting.entries()].slice(0, BATCH);
  for (const [src] of batch) waiting.delete(src);
  if (waiting.size) timer = setTimeout(flush, 250);
  const source = batch.map(([src], i) => `${SEP}${i}\n\n${src.trim()}`).join('\n\n') + `\n\n${SEP}${batch.length}\n`;
  let parts: string[] | null = null;
  try {
    const res = await api.post<{ html: string }>('elements/preview', { type: 'core.html', lang: uiLang(), body: { format: 'markdown', source } });
    parts = split(res.html, batch.length);
  } catch { /* no picture: the editor shows the name of the macro */ }
  batch.forEach(([src, dones], i) => {
    const html = parts?.[i] ?? null;
    if (html !== null) cache.set(src, html);
    for (const d of dones) d(html);
  });
}

/** Splits the rendered page at the separator paragraphs (n parts, or null). */
function split(html: string, n: number): string[] | null {
  const tpl = document.createElement('template');
  tpl.innerHTML = html; // inert: a template's content is not rendered and runs no scripts
  const out: string[] = [];
  let cur: Node[] | null = null;
  for (const node of [...tpl.content.childNodes]) {
    const m = node instanceof HTMLParagraphElement ? new RegExp(`^${SEP}(\\d+)$`).exec(node.textContent?.trim() ?? '') : null;
    if (m) {
      if (cur) out.push(serializeNodes(cur));
      cur = Number(m[1]) < n ? [] : null;
      continue;
    }
    cur?.push(node);
  }
  return out.length === n ? out : null;
}

function serializeNodes(nodes: Node[]): string {
  const box = document.createElement('div');
  box.append(...nodes);
  return box.innerHTML;
}
