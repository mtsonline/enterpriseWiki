// Editor for core.attachment: upload a file (each upload becomes a new
// version when saved) and describe it.
import { errorText, field, h, humanSize, registerEditor, t, textArea, uploadFile, type UploadedFile } from '@ewiki/sdk';
import './style.css';

interface Body { file: UploadedFile | null; description: string }

export function register(): void {
  registerEditor('core.attachment', (container, body, ctx) => {
    const b = (body ?? {}) as Partial<Body>;
    let file: UploadedFile | null = b.file ?? null;
    const status = h('p', { class: 'ewa-status', role: 'status' });
    const showStatus = () => {
      status.replaceChildren(file ? t('core.attachment.current', { name: file.name, size: humanSize(file.size) }) : t('core.attachment.none'));
      if (file && ctx.elementId) {
        status.append(' ', h('a', { href: `/api/v1/x/core.attachment/elements/${ctx.elementId}/file?hash=${file.hash}`, download: '' }, t('core.attachment.download')));
      }
    };
    const input = h('input', { type: 'file', class: 'ew-input' });
    input.setAttribute('aria-labelledby', ctx.labelId);
    const err = h('p', { class: 'ew-field-error', role: 'alert' });
    input.addEventListener('change', async () => {
      const f = input.files?.[0];
      if (!f) return;
      err.textContent = '';
      status.textContent = t('core.attachment.uploading');
      input.disabled = true;
      try {
        file = await uploadFile(f);
        status.textContent = t('core.attachment.uploaded', { name: file.name });
      } catch (e) {
        err.textContent = errorText(e);
        showStatus();
      } finally {
        input.disabled = false;
      }
    });
    const desc = textArea({ value: b.description ?? '', rows: 4, lang: ctx.lang });
    container.append(h('div', { class: 'ewa-editor' },
      h('p', { class: 'ew-help' }, t('core.attachment.replace_help')), input, status, err,
      field(t('core.attachment.description'), desc, { help: t('core.attachment.description_help') })));
    showStatus();
    return {
      getBody: () => ({ file: file ? { hash: file.hash, name: file.name } : null, description: desc.value }),
      validate: () => (file ? null : t('core.attachment.missing')),
    };
  });
}
