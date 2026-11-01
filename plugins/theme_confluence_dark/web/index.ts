// theme.confluence.dark: example of template inheritance. It reuses the
// layout of theme.confluence (no layout function here) and only adds a CSS
// class that overrides the color variables.
import { registerTemplate } from '@ewiki/sdk';
import './style.css';

export function register(): void {
  registerTemplate({ id: 'theme.confluence.dark', extends: 'theme.confluence', className: 'ewc-dark' });
}
