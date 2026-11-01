// Editor for core.html: accessible WYSIWYG (contenteditable) or Markdown.
// The server sanitizes everything on save and render, so the editor does
// not need to be a security boundary.
import { api, button, field, formDialog, h, registerEditor, select, t, textInput, toast, trustedHTML, uploadFile, type EditorInstance } from '@ewiki/sdk';
import { macroAt, macroDialog, macroHTML, parseParams } from './macros';
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

/** Creates the core.html editor. Exported so other plugins can reuse it. */
export function createHtmlEditor(container: HTMLElement, body: unknown, ctx: { lang: string; type: string; labelId: string }): EditorInstance {
  let cur = decode(body);
  const formatSel = select([{ value: 'html', label: t('core.html.format.html') }, { value: 'markdown', label: t('core.html.format.markdown') }], cur.format);
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
    if (tool.cmd === 'image') return imageDialog();
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

  // Images: upload, then insert with a mandatory alternative text (WCAG 1.1.1).
  const imageDialog = () => {
    const sel = document.getSelection();
    const range = sel && sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null;
    const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/gif,image/webp', class: 'ew-input' });
    const alt = textInput({ maxlength: 300 });
    formDialog(t('core.html.image'), [field(t('core.html.image_file'), file, { required: true }),
      field(t('core.html.image_alt'), alt, { required: true, help: t('core.html.image_alt_help') })], t('common.apply'), async () => {
      const f = file.files?.[0];
      if (!f) throw new Error(t('core.html.image_file'));
      const up = await uploadFile(f);
      if (!up.mime.startsWith('image/')) throw new Error(t('core.html.image_type'));
      const esc = (x: string) => x.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
      afterDialog(range, () => document.execCommand('insertHTML', false, `<img src="/api/v1/files/${up.hash}" alt="${esc(alt.value.trim())}">`));
    });
  };

  // Macros: insert at the cursor, or edit the macro the cursor is in.
  const openMacro = async () => {
    const sel = document.getSelection();
    const range = sel && sel.rangeCount ? sel.getRangeAt(0).cloneRange() : null;
    const existing = editable ? macroAt(sel?.anchorNode ?? null, editable) : null;
    try {
      await macroDialog(existing ? { name: existing.dataset.macro!, params: parseParams(existing) } : null, (c) => {
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
      });
    } catch (e) {
      toast(String((e as Error).message ?? e), 'error');
    }
  };

  const insertMarkdownMacro = async () => {
    const ta = textarea!;
    const start = ta.selectionStart, end = ta.selectionEnd;
    try {
      await macroDialog(null, (c) => {
        const inner = ta.value.slice(start, end) || t('core.html.macro_body');
        const snippet = c.hasBody
          ? `\n${macroHTML(c, '').replace('</div>', '')}\n\n${inner}\n\n</div>\n`
          : `\n${macroHTML(c, '')}\n`;
        setTimeout(() => { ta.setRangeText(snippet, start, end, 'end'); ta.focus(); }, 0);
      });
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
    if (formatSel.value === 'markdown') {
      // Markdown may contain HTML, so the HTML source is kept as is.
      cur = { format: 'markdown', source: before.source };
      mountMarkdown(cur.source);
    } else {
      let html = before.source;
      try {
        html = (await api.post<{ html: string }>('elements/preview', { type: 'core.html', lang: ctx.lang, body: before })).html;
      } catch { /* keep source */ }
      cur = { format: 'html', source: html };
      mountHtml(html);
    }
  });

  if (cur.format === 'markdown') mountMarkdown(cur.source);
  else mountHtml(cur.source);

  return {
    getBody: () => current(),
    destroy: () => { if (onSel) document.removeEventListener('selectionchange', onSel); },
  };
}

export function register(): void {
  registerEditor('core.html', createHtmlEditor);
}
