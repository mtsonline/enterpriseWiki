// Formulas in content: TeX set as MathML with Temml (MIT), loaded only when a
// page has a formula. Browsers show MathML themselves and screen readers
// read it as mathematics. The server writes the TeX as text; nothing of the
// output of Temml reaches the page as it is: a new MathML tree is built
// from a fixed list of elements and attributes (no links, no styles, no
// HTML). Without script, or if the TeX is not valid, the TeX stays readable.

const NS = 'http://www.w3.org/1998/Math/MathML';

/** The MathML elements a formula may have (MathML Core and menclose). */
export const ELEMENTS = new Set([
  'math', 'semantics', 'annotation', 'mrow', 'mi', 'mn', 'mo', 'ms', 'mspace', 'mtext', 'mfrac', 'msqrt', 'mroot', 'mstyle',
  'merror', 'mpadded', 'mphantom', 'msub', 'msup', 'msubsup', 'munder', 'mover', 'munderover', 'mmultiscripts', 'mprescripts',
  'none', 'mtable', 'mtr', 'mtd', 'mlabeledtr', 'menclose',
]);

/** Attributes with the values they may have. */
const WORD = /^[A-Za-z0-9 .:+-]{0,40}$/;
export const ATTRS: Record<string, RegExp> = {
  display: /^(block|inline)$/,
  displaystyle: /^(true|false)$/,
  mathvariant: /^[a-z-]{1,30}$/,
  scriptlevel: /^[+-]?\d{1,2}$/,
  stretchy: /^(true|false)$/,
  symmetric: /^(true|false)$/,
  largeop: /^(true|false)$/,
  movablelimits: /^(true|false)$/,
  accent: /^(true|false)$/,
  accentunder: /^(true|false)$/,
  fence: /^(true|false)$/,
  separator: /^(true|false)$/,
  form: /^(prefix|infix|postfix)$/,
  lspace: WORD, rspace: WORD, minsize: WORD, maxsize: WORD, width: WORD, height: WORD, depth: WORD, voffset: WORD,
  linethickness: WORD, columnalign: WORD, rowalign: WORD, columnlines: WORD, rowlines: WORD, columnspacing: WORD, rowspacing: WORD,
  columnspan: /^\d{1,2}$/, rowspan: /^\d{1,2}$/, notation: WORD,
  mathcolor: /^(#[0-9A-Fa-f]{3,8}|[a-z]{1,20})$/,
  mathbackground: /^(#[0-9A-Fa-f]{3,8}|[a-z]{1,20})$/,
  class: /^[a-z0-9 _-]{0,80}$/i,
  encoding: /^application\/x-tex$/,
};

/** Builds a new MathML tree from the allowed parts of src (null if src is not math). */
export function cleanMathML(src: Element, doc: Document = document): Element | null {
  const name = src.localName;
  if (!ELEMENTS.has(name)) return null;
  const out = doc.createElementNS(NS, name);
  for (const a of Array.from(src.attributes)) {
    const re = ATTRS[a.name];
    if (re && re.test(a.value)) out.setAttribute(a.name, a.value);
  }
  for (const c of Array.from(src.childNodes)) {
    if (c.nodeType === Node.TEXT_NODE) {
      out.append(doc.createTextNode(c.textContent ?? ''));
    } else if (c.nodeType === Node.ELEMENT_NODE) {
      const x = cleanMathML(c as Element, doc);
      // an element that is not allowed keeps only its text
      out.append(x ?? doc.createTextNode(c.textContent ?? ''));
    }
  }
  return out;
}

type Temml = typeof import('temml').default;
let temml: Promise<Temml> | null = null;

/** Sets tex as MathML (null if it is not valid TeX). */
export async function texToMathML(tex: string, block: boolean, doc: Document = document): Promise<Element | null> {
  temml ??= import('temml').then((m) => m.default);
  const tm = await temml;
  let s: string;
  try {
    s = tm.renderToString(tex, { displayMode: block, throwOnError: true, trust: false, strict: false, maxExpand: 500, maxSize: [20, 20], annotate: false });
  } catch {
    return null;
  }
  // parsed in a document of its own: nothing in it runs or loads
  const parsed = new DOMParser().parseFromString(`<!doctype html><body>${s}`, 'text/html');
  const m = parsed.body.querySelector('math');
  return m ? cleanMathML(m, doc) : null;
}

/** Sets the formulas in root (the macro and ```math code blocks). */
export function hydrateMath(root: ParentNode): void {
  const items: { el: HTMLElement; tex: string; block: boolean }[] = [];
  root.querySelectorAll<HTMLElement>('.ew-math:not([data-ew-math])').forEach((el) => {
    const code = el.querySelector('code.ew-math-tex');
    if (code) items.push({ el, tex: code.textContent ?? '', block: el.dataset.display === 'block' });
  });
  root.querySelectorAll<HTMLElement>('pre:not([data-ew-math]) > code.language-math').forEach((code) => {
    items.push({ el: code.parentElement as HTMLElement, tex: code.textContent ?? '', block: true });
  });
  for (const it of items) {
    it.el.dataset.ewMath = '1';
    void texToMathML(it.tex.trim(), it.block).then((m) => {
      if (!m) {
        it.el.classList.add('ew-math--error');
        return;
      }
      if (it.el.localName === 'pre') {
        const box = document.createElement('div');
        box.className = 'ew-math ew-math--block';
        box.dataset.ewMath = '1';
        box.append(m);
        it.el.replaceWith(box);
      } else {
        it.el.replaceChildren(m);
      }
    }).catch(() => it.el.classList.add('ew-math--error'));
  }
}
