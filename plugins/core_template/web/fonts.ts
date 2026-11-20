// Uploaded fonts: helpers without the rest of the interface (unit tested).

/** Weight and style from a file name (Bold, Italic, Variable …); changeable. */
export function guessFace(file: string): { weight: string; style: string } {
  const n = file.toLowerCase();
  const style = /italic|oblique|kursiv/.test(n) ? 'italic' : 'normal';
  if (/variable|\[wght|wght\]|-vf\b/.test(n)) return { weight: '100 900', style };
  const names: [RegExp, string][] = [[/thin|hairline/, '100'], [/extra-?light|ultra-?light/, '200'], [/light/, '300'], [/medium/, '500'],
    [/semi-?bold|demi-?bold/, '600'], [/extra-?bold|ultra-?bold/, '800'], [/black|heavy/, '900'], [/bold/, '700']];
  for (const [re, w] of names) if (re.test(n)) return { weight: w, style };
  return { weight: '400', style };
}
