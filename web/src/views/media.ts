// /media: the media library of the site.
import { h, mount } from '../core/dom';
import { t } from '../core/i18n';
import type { RouteCtx } from '../core/router';
import { state } from '../core/state';
import { mediaBrowser } from '../components/media';
import { pageHeader } from '../components/ui';

export async function mediaView({ main, query }: RouteCtx): Promise<void> {
  document.title = `${t('media.library')} – ${state.site.name}`;
  const cat = Number(query.get('category')) || undefined;
  mount(main, pageHeader(t('media.library')), h('p', { class: 'ew-help' }, t('media.library_help')), await mediaBrowser({ categoryId: cat }));
}
