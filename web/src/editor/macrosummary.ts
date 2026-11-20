// The parameters of a macro in a few readable words, for the head of the
// macro in the visual editor. Without DOM, so it can be tested on its own.
import type { MacroInfo } from '../core/types';

const pick = (m: Record<string, string> | undefined, lang: string): string => (m ? m[lang] || m.en || Object.values(m)[0] || '' : '');

/**
 * A short, readable summary of the parameters of a macro: "Title: Careful ·
 * Only one open at a time" instead of "title=Careful, one=true,
 * first=false". Switched off options, values as by default and the block
 * options are left out; unknown parameters show as name=value.
 */
export function paramSummary(info: MacroInfo | undefined, params: Record<string, string>, lang: string): string {
  const out: string[] = [];
  for (const [k, v] of Object.entries(params)) {
    if (v === '' || k === 'options') continue;
    const p = info?.params?.find((x) => x.name === k);
    if (!p) { out.push(`${k}=${v}`); continue; }
    if (p.default !== undefined && v === p.default) continue; // as it is anyway
    const name = pick(p.labels, lang) || k;
    if (p.type === 'bool') { if (v === 'true') out.push(name); continue; }
    if (p.type === 'select') { out.push(`${name}: ${pick(p.option_labels?.[v], lang) || v}`); continue; }
    if (p.type.startsWith('custom:')) { out.push(name); continue; }
    out.push(`${name}: ${v}`);
  }
  const s = out.join(' · ');
  return s.length > 100 ? s.slice(0, 99) + '…' : s;
}
