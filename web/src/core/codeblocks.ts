// Code blocks in content: syntax highlighting (highlight.js, BSD-3-Clause,
// loaded only when a page has code in a known language), a button to copy
// the code and, for blocks marked with "lines", line numbers. Without
// script the code stays plain and readable.
import { h } from './dom';
import { t } from './i18n';
import { announce } from './announce';

import type { HLJSApi, LanguageFn } from 'highlight.js';

type LangModule = { default: LanguageFn };

/** The languages offered (name -> module); aliases point to a name. */
export const CODE_LANGUAGES: Record<string, () => Promise<LangModule>> = {
  bash: () => import('highlight.js/lib/languages/bash'),
  c: () => import('highlight.js/lib/languages/c'),
  cpp: () => import('highlight.js/lib/languages/cpp'),
  csharp: () => import('highlight.js/lib/languages/csharp'),
  css: () => import('highlight.js/lib/languages/css'),
  diff: () => import('highlight.js/lib/languages/diff'),
  dockerfile: () => import('highlight.js/lib/languages/dockerfile'),
  go: () => import('highlight.js/lib/languages/go'),
  graphql: () => import('highlight.js/lib/languages/graphql'),
  ini: () => import('highlight.js/lib/languages/ini'),
  java: () => import('highlight.js/lib/languages/java'),
  javascript: () => import('highlight.js/lib/languages/javascript'),
  json: () => import('highlight.js/lib/languages/json'),
  kotlin: () => import('highlight.js/lib/languages/kotlin'),
  makefile: () => import('highlight.js/lib/languages/makefile'),
  markdown: () => import('highlight.js/lib/languages/markdown'),
  nginx: () => import('highlight.js/lib/languages/nginx'),
  php: () => import('highlight.js/lib/languages/php'),
  powershell: () => import('highlight.js/lib/languages/powershell'),
  python: () => import('highlight.js/lib/languages/python'),
  ruby: () => import('highlight.js/lib/languages/ruby'),
  rust: () => import('highlight.js/lib/languages/rust'),
  scss: () => import('highlight.js/lib/languages/scss'),
  sql: () => import('highlight.js/lib/languages/sql'),
  swift: () => import('highlight.js/lib/languages/swift'),
  typescript: () => import('highlight.js/lib/languages/typescript'),
  xml: () => import('highlight.js/lib/languages/xml'),
  yaml: () => import('highlight.js/lib/languages/yaml'),
};

const ALIASES: Record<string, string> = {
  sh: 'bash', shell: 'bash', zsh: 'bash', 'c++': 'cpp', cs: 'csharp', 'c#': 'csharp', golang: 'go', toml: 'ini',
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', ts: 'typescript', tsx: 'typescript', kt: 'kotlin', md: 'markdown',
  ps: 'powershell', ps1: 'powershell', py: 'python', rb: 'ruby', rs: 'rust', html: 'xml', svg: 'xml', yml: 'yaml', patch: 'diff',
  docker: 'dockerfile', make: 'makefile',
};

/** Names shown for the languages (the button and the label). */
const NAMES: Record<string, string> = {
  bash: 'Bash', c: 'C', cpp: 'C++', csharp: 'C#', css: 'CSS', diff: 'Diff', dockerfile: 'Dockerfile', go: 'Go', graphql: 'GraphQL',
  ini: 'INI/TOML', java: 'Java', javascript: 'JavaScript', json: 'JSON', kotlin: 'Kotlin', makefile: 'Makefile', markdown: 'Markdown',
  nginx: 'Nginx', php: 'PHP', powershell: 'PowerShell', python: 'Python', ruby: 'Ruby', rust: 'Rust', scss: 'SCSS', sql: 'SQL',
  swift: 'Swift', typescript: 'TypeScript', xml: 'HTML/XML', yaml: 'YAML',
};

/** The languages for a select, by name. */
export const codeLanguageOptions = (): { value: string; label: string }[] =>
  Object.keys(CODE_LANGUAGES).map((k) => ({ value: k, label: NAMES[k] })).sort((a, b) => a.label.localeCompare(b.label));

/** The language of a fence info ("go lines" -> go), or '' if unknown. */
export function codeLanguage(info: string): string {
  const w = info.trim().split(/\s+/)[0]?.toLowerCase() ?? '';
  const l = ALIASES[w] ?? w;
  return Object.hasOwn(CODE_LANGUAGES, l) ? l : '';
}

/** Whether the fence info asks for line numbers ("go lines"). */
export const codeLines = (info: string): boolean => info.trim().split(/\s+/).slice(1).includes('lines');

let core: Promise<HLJSApi> | null = null;
const loaded = new Map<string, Promise<void>>();

async function highlighter(lang: string) {
  core ??= import('highlight.js/lib/core').then((m) => m.default);
  const hl = await core;
  if (!loaded.has(lang)) loaded.set(lang, CODE_LANGUAGES[lang]().then((m) => hl.registerLanguage(lang, m.default)));
  await loaded.get(lang);
  return hl;
}

/** The fence info of a rendered block: data-lang of the pre, else the class language-x of the code. */
function infoOf(pre: HTMLElement, code: HTMLElement): string {
  const d = pre.dataset.lang;
  if (d) return d;
  const c = [...code.classList].find((x) => x.startsWith('language-'));
  return c ? c.slice(9) + (pre.hasAttribute('data-lines') ? ' lines' : '') : (pre.hasAttribute('data-lines') ? ' lines' : '');
}

/** Activates the code blocks below root (each once). */
export function hydrateCode(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('.ew-content pre, .ew-macro pre').forEach((pre) => {
    if (pre.dataset.ewCode || pre.closest('.ProseMirror, .ewe-prose')) return;
    const code = pre.querySelector<HTMLElement>(':scope > code');
    if (!code) return;
    pre.dataset.ewCode = '1';
    const info = infoOf(pre, code);
    const lang = codeLanguage(info);
    const text = code.textContent ?? '';
    const bar = h('div', { class: 'ew-code-bar' },
      lang ? h('span', { class: 'ew-code-lang' }, NAMES[lang]) : null,
      h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-code-copy' }, t('code.copy')));
    const copy = bar.querySelector('button')!;
    copy.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(text.replace(/\n$/, ''));
        announce(t('code.copied'));
        copy.textContent = t('code.copied');
        setTimeout(() => { copy.textContent = t('code.copy'); }, 2000);
      } catch { announce(t('code.copy_failed')); }
    });
    const wrap = h('div', { class: 'ew-codeblock' + (codeLines(info) ? ' ew-codeblock--lines' : '') });
    pre.replaceWith(wrap);
    wrap.append(bar, pre);
    if (codeLines(info)) {
      const n = text.replace(/\n$/, '').split('\n').length;
      // the numbers are for the eye only: screen readers and copying get the code alone
      const gutter = h('span', { class: 'ew-code-gutter', 'aria-hidden': 'true' }, Array.from({ length: n }, (_, i) => String(i + 1)).join('\n'));
      pre.prepend(gutter);
    }
    // a block wider than its column scrolls; it must be reachable with the keyboard
    const scroll = () => {
      if (pre.scrollWidth > pre.clientWidth + 1) {
        pre.tabIndex = 0;
        pre.setAttribute('role', 'region');
        pre.setAttribute('aria-label', lang ? t('code.region_lang', { lang: NAMES[lang] }) : t('code.region'));
      }
    };
    requestAnimationFrame(scroll);
    if (!lang) return;
    void highlighter(lang).then((hl) => {
      // highlight.js escapes the text it is given; the result holds only its own spans
      code.innerHTML = hl.highlight(text, { language: lang, ignoreIllegals: true }).value;
      code.classList.add('hljs');
    }).catch(() => { /* stays plain */ });
  });
}
