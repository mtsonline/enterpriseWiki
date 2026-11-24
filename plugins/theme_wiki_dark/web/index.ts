// theme.wiki.dark: example of template inheritance. It reuses the layout of
// theme.wiki (no layout function here). Its plugin.json sets the dark scheme
// as the default (color_scheme); the colours are the dark ones of the core,
// and a person may still choose light.
import { registerTemplate } from '@ewiki/coreplugin';
import './style.css';

export function register(): void {
  registerTemplate({ id: 'theme.wiki.dark', extends: 'theme.wiki' });
}
