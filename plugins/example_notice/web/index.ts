// Frontend of example.notice: reuses the core.html editor (inheritance on the
// frontend side) and adds a tone selector.
import { api, field, getEditor, h, registerEditor, select, t } from '@ewiki/sdk';

export function register(): void {
  registerEditor('example.notice', (container, body, ctx) => {
    const b = (body ?? {}) as { tone?: string };
    const tone = select(['info', 'success', 'warning', 'danger'].map((x) => ({ value: x, label: t(`example.notice.tone.${x}`) })), b.tone ?? 'info');
    // Demonstrates calling the plugin's own API route.
    api.get<string[]>('/api/v1/x/example.notice/tones').then((tones) => {
      tone.replaceChildren(...tones.map((x) => h('option', { value: x, selected: x === (b.tone ?? 'info') }, t(`example.notice.tone.${x}`))));
    }).catch(() => undefined);
    const inner = h('div');
    container.append(field(t('example.notice.tone'), tone), inner);
    const base = getEditor('core.html');
    if (!base) throw new Error('core.html editor missing');
    const editor = base(inner, body, ctx);
    return {
      getBody: () => ({ ...(editor.getBody() as object), tone: tone.value }),
      validate: () => editor.validate?.() ?? null,
      destroy: () => editor.destroy?.(),
    };
  });
}
