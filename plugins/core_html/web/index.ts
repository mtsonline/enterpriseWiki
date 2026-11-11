// Editors for core.html. Pages are stored as Markdown (with macro
// directives); two editors are available (site default, own choice in the
// profile):
//   visual  the ProseMirror editor of the core (loaded on first use)
//   simple  a minimal contenteditable editor (HTML, converted on save)
// The server sanitizes everything on save and render, so the editors are not
// a security boundary; HTML for editing comes sanitized from the server.
import { api, button, editorKind, state, errorText, formatAttrs, macroDialog, field, formDialog, h, isImage, loadMarkdownEditor, loading, mediaURL, pickMedium, registerEditor, select, t, textInput, toast, trustedHTML, type EditorContext, type EditorInstance, type MarkdownEditor } from '@ewiki/sdk';
import { macroAt, macroHTML, parseParams } from './macros';
import './style.css';

interface Body { format: 'html' | 'markdown'; source: string }

interface Tool { cmd: string; arg?: string; key: string; icon: string; state?: boolean }

const TOOLS: (Tool | '|')[] = [
  { cmd: 'formatBlock', arg: 'p', key: 'core.html.paragraph', icon: '¶' },
  { cmd: 'formatBlock', arg: 'h2', key: 'core.html.h2', icon: 'H2' },
  { cmd: 'formatBlock', arg: 'h3', key: 'core.html.h3', icon: 'H3' },
  '|',
  { cmd: 'bold', key: 'core.html.bold', icon: 'B', state: true },
  { cmd: 'italic', key: 'core.html.italic', icon: 'I', state: true },
  { cmd: 'underline', key: 'core.html.underline', icon: 'U', state: true },
  { cmd: 'strikeThrough', key: 'core.html.strike', icon: 'S', state: true },
  '|',
  { cmd: 'insertUnorderedList', key: 'core.html.bullet', icon: '•', state: true },
  { cmd: 'insertOrderedList', key: 'core.html.ordered', icon: '1.', state: true },
  { cmd: 'formatBlock', arg: 'blockquote', key: 'core.html.quote', icon: '❝' },
  { cmd: 'formatBlock', arg: 'pre', key: 'core.html.code', icon: '</>' },
  '|',
  { cmd: 'link', key: 'core.html.link', icon: '🔗' },
  { cmd: 'table', key: 'core.html.table', icon: '▦' },
  { cmd: 'image', key: 'core.html.image', icon: '🖼' },
  { cmd: 'insertHorizontalRule', key: 'core.html.hr', icon: '—' },
  { cmd: 'macro', key: 'core.html.macro', icon: '⚙' },
  '|',
  { cmd: 'undo', key: 'core.html.undo', icon: '↶' },
  { cmd: 'redo', key: 'core.html.redo', icon: '↷' },
];

function decode(body: unknown): Body {
  const b = (body ?? {}) as Partial<Body>;
  return { format: b.format === 'markdown' ? 'markdown' : 'html', source: typeof b.source === 'string' ? b.source : '' };
}

const BASE = '/api/v1/x/core.html/';

/** Markdown -> HTML with macro placeholders (sanitized, no macro executed). */
async function editHTML(markdown: string): Promise<string> {
  if (!markdown.trim()) return '';
  return (await api.post<{ html: string }>(BASE + 'edit-html', { source: markdown })).html;
}

/** HTML (with macro placeholders) -> Markdown. */
async function toMarkdown(html: string): Promise<string> {
  if (!html.trim()) return '';
  return (await api.post<{ markdown: string }>(BASE + 'to-markdown', { source: html })).markdown;
}

type Ctx = Pick<EditorContext, 'lang' | 'type' | 'labelId' | 'elementId' | 'onRemoteSave'>;

/** Creates the core.html editor (the user's choice). Exported so other plugins can reuse it. */
export function createHtmlEditor(container: HTMLElement, body: unknown, ctx: Ctx): EditorInstance {
  return editorKind() === 'simple' ? createSimpleEditor(container, body, ctx) : createVisualEditor(container, body, ctx);
}

/** The visual editor of the core (separate file, loaded on first use). */
export function createVisualEditor(container: HTMLElement, body: unknown, ctx: Ctx): EditorInstance {
  const b = decode(body);
  const box = h('div', { class: 'ewh-host' }, loading());
  container.append(box);
  let ed: MarkdownEditor | null = null;
  let gone = false;
  let fallback: EditorInstance | null = null;
  (async () => {
    const md = b.format === 'html' ? await toMarkdown(b.source) : b.source;
    const mod = await loadMarkdownEditor();
    if (gone) return;
    box.replaceChildren();
    // live editing of existing pages (if the site allows it)
    const collab = ctx.elementId && state.user && state.site.collaboration !== false
      ? { elementId: ctx.elementId, userId: state.user.id, onSaved: (rev: number, by?: { name: string }) => ctx.onRemoteSave?.(rev, by?.name) }
      : undefined;
    ed = mod.createMarkdownEditor(box, { markdown: md, lang: ctx.lang, labelId: ctx.labelId, collab });
  })().catch((e: unknown) => {
    if (gone) return;
    // e.g. the editor file could not be loaded: the simple editor still works
    box.replaceChildren(h('p', { class: 'ew-form-alert', role: 'alert' }, t('core.html.editor_failed', { error: errorText(e) })),
      button(t('core.html.use_simple'), () => { box.replaceChildren(); fallback = createSimpleEditor(box, body, ctx); }));
  });
  return {
    getBody: () => (ed ? { format: 'markdown', source: ed.getMarkdown() } : fallback ? fallback.getBody() : b),
    saved: (rev: number) => ed?.saved(rev),
    destroy: () => { gone = true; ed?.destroy(); fallback?.destroy?.(); },
  };
}

/** The minimal editor: contenteditable (or the Markdown source). */
export function createSimpleEditor(container: HTMLElement, body: unknown, ctx: Ctx): EditorInstance {
  const stored = decode(body);
  let cur: Body = { format: 'html', source: '' };
  let ready = false;
  const formatSel = select([{ value: 'html', label: t('core.html.format.html') }, { value: 'markdown', label: t('core.html.format.markdown') }], 'html');
  const surface = h('div', { class: 'ewh-surface' });
  container.append(h('div', { class: 'ewh-editor' }, field(t('core.html.format'), formatSel, { inline: true, help: t('core.html.convert_warning') }), surface));

  let editable: HTMLElement | null = null;
  let textarea: HTMLTextAreaElement | null = null;
  let onSel: (() => void) | null = null;

  const mountHtml = (html: string) => {
    const toolbar = h('div', { class: 'ewh-toolbar', role: 'toolbar', 'aria-label': t('core.html.toolbar') });
    editable = h('div', {
      class: 'ewh-editable ew-content', contenteditable: 'true', role: 'textbox', 'aria-multiline': 'true',
      'aria-labelledby': ctx.labelId, lang: ctx.lang, spellcheck: 'true',
    });
    editable.innerHTML = html || '<p><br></p>';
    const stateButtons: { tool: Tool; btn: HTMLButtonElement }[] = [];
    for (const tool of TOOLS) {
      if (tool === '|') {
        toolbar.append(h('span', { class: 'ewh-sep', 'aria-hidden': 'true' }));
        continue;
      }
      // WCAG 2.5.3 (label in name): visible letters/digits must be part of the accessible name.
      const name = /[A-Za-z0-9]/.test(tool.icon) && tool.icon.length > 1 ? `${tool.icon} ${t(tool.key)}` : t(tool.key);
      const btn = h('button', { type: 'button', class: 'ewh-tool', 'aria-label': name, title: name, 'aria-pressed': tool.state ? 'false' : undefined },
        h('span', { 'aria-hidden': 'true' }, tool.icon));
      btn.addEventListener('mousedown', (ev) => ev.preventDefault()); // keep selection
      btn.addEventListener('click', () => run(tool));
      if (tool.state) stateButtons.push({ tool, btn });
      toolbar.append(btn);
    }
    surface.replaceChildren(toolbar, editable);
    onSel = () => {
      if (!editable || !editable.contains(document.getSelection()?.anchorNode ?? null)) return;
      for (const { tool, btn } of stateButtons) {
        let on = false;
        try { on = document.queryCommandState(tool.cmd); } catch { /* unsupported */ }
        btn.setAttribute('aria-pressed', String(on));
      }
    };
    document.addEventListener('selectionchange', onSel);
  };

  const run = (tool: Tool) => {
    if (!editable) return;
    editable.focus();
    if (tool.cmd === 'link') return linkDialog();
    if (tool.cmd === 'image') return void imageDialog();
    if (tool.cmd === 'macro') return void openMacro();
    if (tool.cmd === 'table') {
      const cells = (tag: string) => `<tr>${[1, 2, 3].map(() => `<${tag}><br></${tag}>`).join('')}</tr>`;
      document.execCommand('insertHTML', false, `<table><thead>${cells('th')}</thead><tbody>${cells('td')}${cells('td')}</tbody></table><p><br></p>`);
      return;
    }
    document.execCommand(tool.cmd, false, tool.arg ? `<${tool.arg}>` : undefined);
    onSel?.();
  };

  // Dialogs are modal: the page behind them is inert, so changes to the
  // editing surface are applied after the dialog has closed.
  const afterDialog = (range: Range | null, fn: () => void) => {
    setTimeout(() => {
      if (!editable) return;
      editable.focus();
      if (range) {
        const s = document.getSelection()!;
        s.removeAllRanges();
        s.addRange(range);
      }
      fn();
    }, 0);
  };

  const linkDialog = () => {
    const sel = document.getSelection();
    const range = sel && sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null;
    const existing = (sel?.anchorNode?.parentElement?.closest('a') as HTMLAnchorElement | null)?.getAttribute('href') ?? '';
    const url = textInput({ value: existing, type: 'url', placeholder: 'https://' });
    formDialog(t('core.html.link'), field(t('core.html.link_prompt'), url), t('common.apply'), async () => {
      const v = url.value.trim();
      if (v && !/^(https?:|mailto:|\/)/i.test(v)) throw new Error(t('core.html.link_invalid'));
      afterDialog(range, () => {
        if (!v) document.execCommand('unlink');
        else if (range && range.collapsed) document.execCommand('insertHTML', false, `<a href="${v.replace(/"/g, '&quot;')}">${v.replace(/</g, '&lt;')}</a>`);
        else document.execCommand('createLink', false, v);
      });
    });
  };

  // Images: chosen from (or uploaded to) the media library, inserted with a
  // mandatory alternative text (WCAG 1.1.1). Other files are inserted as link.
  const imageDialog = async () => {
    const sel = document.getSelection();
    const range = sel && sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null;
    const m = await pickMedium();
    if (!m) return;
    const esc = (x: string) => x.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
    if (!isImage(m)) {
      afterDialog(range, () => document.execCommand('insertHTML', false, `<a href="/api/v1/media/${m.id}/view">${esc(m.title || m.name)}</a>`));
      return;
    }
    const alt = textInput({ maxlength: 300, value: m.alt });
    formDialog(t('core.html.image'), [field(t('core.html.image_alt'), alt, { required: true, help: t('core.html.image_alt_help') })], t('common.apply'), async () => {
      afterDialog(range, () => document.execCommand('insertHTML', false, `<img src="${mediaURL(m)}" alt="${esc(alt.value.trim())}">`));
    });
  };

  // Macros: insert at the cursor, or edit the macro the cursor is in.
  const openMacro = async () => {
    const sel = document.getSelection();
    const range = sel && sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null;
    const existing = editable ? macroAt(sel?.anchorNode ?? null, editable) : null;
    try {
      const c = await macroDialog(existing ? { name: existing.dataset.macro!, params: parseParams(existing) } : null);
      if (c) {
        if (existing) {
          existing.dataset.params = JSON.stringify(c.params);
          return;
        }
        // Inserted as a block next to the current paragraph (insertHTML would
        // merge the placeholder into the paragraph).
        afterDialog(range, () => {
          const tpl = document.createElement('template');
          tpl.innerHTML = macroHTML(c, `<p>${t('core.html.macro_body')}</p>`);
          const node = tpl.content.firstElementChild!;
          let block: Node | null = range?.startContainer ?? null;
          while (block && block.parentNode !== editable) block = block.parentNode;
          if (block instanceof Element && !block.textContent?.trim() && !block.querySelector('img, [data-macro]')) block.replaceWith(node);
          else if (block) (block as ChildNode).after(node);
          else editable!.append(node);
          if (!node.nextSibling) node.after(h('p', null, h('br')));
          const sel = document.getSelection()!;
          sel.removeAllRanges();
          const r = document.createRange();
          r.selectNodeContents(node.querySelector('p') ?? node.nextSibling ?? node);
          sel.addRange(r);
          editable!.dispatchEvent(new Event('input', { bubbles: true }));
        });
      }
    } catch (e) {
      toast(String((e as Error).message ?? e), 'error');
    }
  };

  const insertMarkdownMacro = async () => {
    const ta = textarea!;
    const start = ta.selectionStart, end = ta.selectionEnd;
    try {
      const c = await macroDialog(null);
      if (!c) return;
      const inner = ta.value.slice(start, end).trim() || t('core.html.macro_body');
      const head = c.name + formatAttrs(c.params);
      const snippet = c.hasBody ? `\n:::${head}\n${inner}\n:::\n` : `\n::${head}\n`;
      ta.setRangeText(snippet, start, end, 'end');
      ta.focus();
    } catch (e) {
      toast(String((e as Error).message ?? e), 'error');
    }
  };

  const mountMarkdown = (src: string) => {
    if (onSel) document.removeEventListener('selectionchange', onSel);
    editable = null;
    textarea = h('textarea', { class: 'ew-input ewh-markdown', rows: 18, 'aria-labelledby': ctx.labelId, lang: ctx.lang, spellcheck: 'true' });
    textarea.value = src;
    const preview = h('div', { class: 'ewh-preview', 'aria-live': 'polite' });
    surface.replaceChildren(h('p', { class: 'ew-help' }, t('core.html.markdown_help')),
      h('div', { class: 'ewh-toolbar' }, button(t('core.html.macro_insert'), () => void insertMarkdownMacro())), textarea,
      button(t('core.html.preview'), async () => {
        try {
          const res = await api.post<{ html: string }>('elements/preview', { type: ctx.type, lang: ctx.lang, body: { format: 'markdown', source: textarea!.value } });
          preview.replaceChildren(h('h3', null, t('core.html.preview')), trustedHTML(res.html));
        } catch (e) { preview.textContent = String(e); }
      }), preview);
  };

  const current = (): Body => (cur.format === 'markdown' ? { format: 'markdown', source: textarea?.value ?? '' } : { format: 'html', source: editable?.innerHTML ?? '' });

  formatSel.addEventListener('change', async () => {
    const before = current();
    formatSel.disabled = true;
    try {
      if (formatSel.value === 'markdown') {
        cur = { format: 'markdown', source: before.source && before.format === 'html' ? await toMarkdown(before.source) : before.source };
        mountMarkdown(cur.source);
      } else {
        cur = { format: 'html', source: await editHTML(before.source) };
        mountHtml(cur.source);
      }
    } catch (e) {
      formatSel.value = before.format;
      toast(errorText(e), 'error');
    } finally {
      formatSel.disabled = false;
    }
  });

  surface.replaceChildren(loading());
  formatSel.disabled = true;
  (async () => {
    const html = stored.format === 'html' ? stored.source : await editHTML(stored.source);
    cur = { format: 'html', source: html };
    mountHtml(html);
    ready = true;
    formatSel.disabled = false;
  })().catch((e: unknown) => {
    // fall back to the Markdown source
    cur = { format: 'markdown', source: stored.source };
    formatSel.value = 'markdown';
    mountMarkdown(stored.source);
    ready = true;
    formatSel.disabled = false;
    toast(errorText(e), 'error');
  });

  return {
    getBody: () => (ready ? current() : stored),
    destroy: () => { if (onSel) document.removeEventListener('selectionchange', onSel); },
  };
}

export function register(): void {
  registerEditor('core.html', createHtmlEditor);
}
