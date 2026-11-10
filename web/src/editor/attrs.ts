// Parameters of macro directives: {key=value key2="quoted value"}
// (the same rules as internal/markdown on the server).

const KEY = /[A-Za-z0-9_.-]/;

/** Parses the inside of {...}; null if it is not valid. */
export function parseAttrs(s: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  let i = 0;
  for (;;) {
    while (i < s.length && (s[i] === ' ' || s[i] === '\t')) i++;
    if (i >= s.length) return out;
    let j = i;
    while (j < s.length && KEY.test(s[j])) j++;
    if (j === i || s[j] !== '=') return null;
    const key = s.slice(i, j);
    j++;
    let val = '';
    if (s[j] === '"') {
      j++;
      let closed = false;
      while (j < s.length) {
        const c = s[j];
        if (c === '\\' && j + 1 < s.length) { val += s[j + 1]; j += 2; continue; }
        if (c === '"') { closed = true; j++; break; }
        val += c;
        j++;
      }
      if (!closed) return null;
    } else {
      while (j < s.length && s[j] !== ' ' && s[j] !== '\t') {
        if (s[j] === '"' || s[j] === '}') return null;
        val += s[j];
        j++;
      }
    }
    out[key] = val;
    i = j;
  }
}

/** Writes params as {key=value ...} (sorted; '' for none). */
export function formatAttrs(params: Record<string, string>): string {
  const keys = Object.keys(params).filter((k) => /^[A-Za-z0-9_.-]+$/.test(k)).sort();
  if (!keys.length) return '';
  return '{' + keys.map((k) => {
    const v = params[k];
    return v !== '' && !/[\s"}\\]/.test(v) ? `${k}=${v}` : `${k}="${v.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\n/g, ' ')}"`;
  }).join(' ') + '}';
}

/** Index of the brace closing {...} at s[0] (quotes respected), -1 if none. */
export function attrEnd(s: string): number {
  let q = false;
  for (let i = 1; i < s.length; i++) {
    if (q && s[i] === '\\') { i++; continue; }
    if (s[i] === '"') q = !q;
    else if (!q && s[i] === '}') return i;
  }
  return -1;
}

export const NAME = /^[a-z][a-z0-9_.-]{0,63}/;
