// Business card (profile fields shown on team pages): form used in the
// profile and by administrators.
import { api, uploadFile } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { toast } from '../core/announce';
import { button, errorText, field, select, textArea, textInput } from './ui';

const TEXT = ['job_title', 'department', 'phone', 'mobile', 'office'];

/** Builds the form; path is "me/profile" or "admin/users/ID/profile". */
export async function profileCardForm(path: string, name: string): Promise<HTMLElement> {
  const data = await api.get<{ profile: Record<string, string>; photo_policy?: { default: string; max: string; fixed: boolean } }>(path);
  const pol = data.photo_policy ?? { default: 'users', max: 'public', fixed: false };
  const order = ['private', 'users', 'public'];
  const vis = select([{ value: '', label: t('photo.vis.default', { level: t(`photo.vis.${pol.default}`) }) },
    ...order.filter((v) => order.indexOf(v) <= order.indexOf(pol.max)).map((v) => ({ value: v, label: t(`photo.vis.${v}`) }))], data.profile.photo_visibility ?? '');
  vis.disabled = pol.fixed;
  const p = { ...data.profile };
  const inputs = TEXT.map((f) => ({ f, input: textInput({ value: p[f] ?? '', maxlength: 200, type: f === 'phone' || f === 'mobile' ? 'tel' : 'text', autocomplete: f === 'job_title' ? 'organization-title' : f === 'phone' || f === 'mobile' ? 'tel' : undefined }) }));
  const about = textArea({ value: p.about ?? '', rows: 4 });
  const preview = h('div', { class: 'ew-card-photo' });
  const drawPhoto = () => preview.replaceChildren(p.photo
    ? h('img', { src: `/api/v1/files/${p.photo}`, alt: t('profile.photo_of', { name }) })
    : h('span', { class: 'ew-avatar ew-avatar--large', 'aria-hidden': 'true' }, (name || '?').slice(0, 1).toUpperCase()));
  drawPhoto();
  const fileId = uid('photo');
  const file = h('input', { type: 'file', id: fileId, accept: 'image/png,image/jpeg,image/gif,image/webp', class: 'sr-only' }) as HTMLInputElement;
  file.addEventListener('change', async () => {
    const f = file.files?.[0];
    if (!f) return;
    try {
      const up = await uploadFile(f);
      p.photo = up.hash;
      drawPhoto();
      toast(t('profile.photo_uploaded'));
    } catch (e) { toast(errorText(e), 'error'); }
  });
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const form = h('form', { class: 'ew-form ew-card-form', novalidate: true }, alert,
    h('div', { class: 'ew-card-photo-row' }, preview,
      h('div', null, h('label', { for: fileId, class: 'ew-btn ew-btn--secondary' }, t('profile.photo_choose')), file,
        button(t('profile.photo_remove'), () => { p.photo = ''; drawPhoto(); }, { variant: 'link' }),
        h('p', { class: 'ew-help' }, t('profile.photo_help')),
        field(t('photo.vis.label'), vis, { help: pol.fixed ? t('photo.vis.fixed', { level: t(`photo.vis.${pol.default}`) }) : t('photo.vis.help') }))),
    h('div', { class: 'ew-card-grid' }, ...inputs.map((x) => field(t(`profile.card_${x.f}`), x.input))),
    field(t('profile.card_about'), about, { help: t('profile.card_about_help') }),
    h('div', { class: 'ew-form-actions' }, button(t('common.save'), () => undefined, { type: 'submit', variant: 'primary' })));
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    try {
      await api.put(path, { ...Object.fromEntries(inputs.map((x) => [x.f, x.input.value])), about: about.value, photo: p.photo ?? '', ...(pol.fixed ? {} : { photo_visibility: vis.value }) });
      toast(t('common.saved'));
    } catch (e) { alert.textContent = errorText(e); }
  });
  return form;
}
