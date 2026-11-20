// Weight and style of an uploaded font file from its name (changeable in the form).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { guessFace } from '../../../plugins/core_template/web/fonts';

test('weight and style from file names', () => {
  const cases: [string, string, string][] = [
    ['Caladea-Regular.ttf', '400', 'normal'], ['Caladea-Bold.ttf', '700', 'normal'], ['Caladea-BoldItalic.ttf', '700', 'italic'],
    ['Inter-Italic.woff2', '400', 'italic'], ['Inter[wght].woff2', '100 900', 'normal'], ['Inter-Italic[wght].woff2', '100 900', 'italic'],
    ['Roboto-SemiBold.otf', '600', 'normal'], ['Roboto-ExtraBold.woff', '800', 'normal'], ['Roboto-Light.woff', '300', 'normal'],
    ['Roboto-ExtraLight.woff', '200', 'normal'], ['Roboto-Thin.woff', '100', 'normal'], ['Roboto-Black.woff', '900', 'normal'],
    ['Roboto-Medium.woff', '500', 'normal'], ['SourceSans3-VF.ttf', '100 900', 'normal'],
  ];
  for (const [name, weight, style] of cases) assert.deepEqual(guessFace(name), { weight, style }, name);
});
