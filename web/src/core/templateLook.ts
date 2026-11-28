// The look of a template from its settings: the server computes the CSS
// variables of the values changed from their defaults (tplsettings.LookOf),
// for both colour schemes; this module writes them into one style element
// for the template shown. The values were checked on the server and are
// checked here again by their form, so nothing but a colour, a number with
// a unit or a list of font names can reach the CSS.

/** The CSS variables of a template's settings (see tplsettings.Look). */
export interface TemplateLook {
  all?: Record<string, string>;
  light?: Record<string, string>;
  dark?: Record<string, string>;
  /** ids of own fonts whose @font-face rules are loaded */
  fonts?: string[];
}

const NAME = /^--[a-z][a-z0-9-]{0,62}$/;
const VALUE = [
  /^#[0-9a-f]{6}$/i,
  /^-?[0-9]+(\.[0-9]+)?(px|rem|em|%)?$/,
  /^color-mix\(in srgb, #[0-9a-f]{6} 82%, (black|white)\)$/i,
  // a list of font names (FontStacks of the server, or an own font)
  /^[A-Za-z0-9 ",.-]{1,300}$/,
];
const ROOT = /^(\.[a-z][a-z0-9-]{0,40})?$/;
const FONT_ID = /^[1-9][0-9]{0,17}$/;

/** The CSS of a look: the variables on <body> (root "") or on the element
 * of the template (".ewcms-app", doubled so it wins over the rules of the
 * template and its variants), light, dark and both. */
export function lookCSS(look: TemplateLook, root: string): string {
  if (!ROOT.test(root)) return '';
  const el = root ? ` ${root}${root}` : '';
  const block = (sel: string, vars: Record<string, string> | undefined) => {
    const body = Object.entries(vars ?? {}).filter(([k, v]) => NAME.test(k) && VALUE.some((re) => re.test(v))).map(([k, v]) => `${k}: ${v};`).join(' ');
    return body ? `${sel}${el} { ${body} }` : '';
  };
  return [block('html body', look.all), block('html body:not(.ewc-dark)', look.light), block('html body.ewc-dark', look.dark)].filter(Boolean).join('\n');
}

/** Writes the look of the template shown (replacing the one before). */
export function applyLook(look: TemplateLook | undefined, root: string): void {
  let style = document.getElementById('ew-template-look') as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement('style');
    style.id = 'ew-template-look';
    document.head.append(style);
  }
  style.textContent = look ? lookCSS(look, root) : '';
  // the @font-face rules of own fonts (the server shows them while a
  // setting chooses them)
  const want = new Set((look?.fonts ?? []).filter((f) => FONT_ID.test(f)));
  for (const link of document.querySelectorAll<HTMLLinkElement>('link[data-ew-look-font]')) {
    if (!want.delete(link.dataset.ewLookFont ?? '')) link.remove();
  }
  for (const id of want) {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = `/api/v1/x/core.template/fonts/${id}/css`;
    link.dataset.ewLookFont = id;
    document.head.append(link);
  }
}
