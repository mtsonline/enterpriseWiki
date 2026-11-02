// theme.cms.modern: neutral, modern website look. It inherits layout,
// positions and settings from theme.cms and only adds a CSS class.
import { registerTemplate } from '@ewiki/sdk';
import './style.css';

export function register(): void {
  registerTemplate({ id: 'theme.cms.modern', extends: 'theme.cms', className: 'ewcms-modern' });
}
