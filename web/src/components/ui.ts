// Accessible UI building blocks: buttons, labelled form fields, dialogs.
import { ApiError } from '../core/api';
import { h, uid, type Child } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';

type Variant = 'primary' | 'secondary' | 'danger' | 'link' | 'subtle';

/** A button. Icon-only buttons must pass ariaLabel. */
export function button(label: Child, onClick: (ev: MouseEvent) => void, opts: { variant?: Variant; ariaLabel?: string; type?: 'button' | 'submit'; pressed?: boolean; disabled?: boolean; id?: string; describedBy?: string } = {}): HTMLButtonElement {
  return h('button', {
    type: opts.type ?? 'button',
    class: ['ew-btn', `ew-btn--${opts.variant ?? 'secondary'}`],
    'aria-label': opts.ariaLabel,
    'aria-pressed': opts.pressed === undefined ? undefined : String(opts.pressed),
    'aria-describedby': opts.describedBy,
    disabled: opts.disabled,
    id: opts.id,
    onclick: onClick,
  }, label);
}

/** Wraps a control with a label, optional help text and error slot. */
export function field(label: string, control: HTMLElement, opts: { help?: string; required?: boolean; inline?: boolean } = {}): HTMLElement {
  const id = control.id || uid('f');
  control.id = id;
  const helpId = opts.help ? `${id}-help` : undefined;
  const errId = `${id}-err`;
  const described = [helpId, errId].filter(Boolean).join(' ');
  control.setAttribute('aria-describedby', described);
  if (opts.required) control.setAttribute('aria-required', 'true');
  return h('div', { class: ['ew-field', opts.inline && 'ew-field--inline'] },
    h('label', { for: id }, label, opts.required ? h('span', { class: 'ew-req', 'aria-hidden': 'true' }, ' *') : null),
    control,
    opts.help ? h('p', { class: 'ew-help', id: helpId }, opts.help) : null,
    h('p', { class: 'ew-field-error', id: errId }),
  );
}

/** Marks a field as invalid (or valid when msg is empty). */
export function setFieldError(control: HTMLElement, msg: string): void {
  const err = document.getElementById(`${control.id}-err`);
  if (err) err.textContent = msg;
  if (msg) control.setAttribute('aria-invalid', 'true');
  else control.removeAttribute('aria-invalid');
}

export function textInput(opts: { value?: string; name?: string; type?: string; required?: boolean; autocomplete?: string; placeholder?: string; maxlength?: number; lang?: string; id?: string } = {}): HTMLInputElement {
  return h('input', {
    class: 'ew-input', type: opts.type ?? 'text', name: opts.name, value: opts.value ?? '', required: opts.required,
    autocomplete: opts.autocomplete, placeholder: opts.placeholder, maxlength: opts.maxlength, lang: opts.lang, id: opts.id,
  });
}

export function textArea(opts: { value?: string; name?: string; rows?: number; lang?: string } = {}): HTMLTextAreaElement {
  const ta = h('textarea', { class: 'ew-input', name: opts.name, rows: opts.rows ?? 4, lang: opts.lang });
  ta.value = opts.value ?? '';
  return ta;
}

export function select(options: { value: string; label: string }[], value?: string, name?: string): HTMLSelectElement {
  return h('select', { class: 'ew-input', name }, ...options.map((o) => h('option', { value: o.value, selected: o.value === value }, o.label)));
}

export function checkbox(label: string, checked = false, opts: { name?: string; help?: string } = {}): { el: HTMLElement; input: HTMLInputElement } {
  const input = h('input', { type: 'checkbox', name: opts.name, checked, id: uid('cb') });
  const help = opts.help ? h('span', { class: 'ew-help', id: `${input.id}-help` }, opts.help) : null;
  if (help) input.setAttribute('aria-describedby', help.id);
  return { el: h('div', { class: 'ew-check' }, input, h('label', { for: input.id }, label), help), input };
}

/** Renders an error message for an exception. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return t('error.network');
    return e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

export function loading(): HTMLElement {
  return h('p', { class: 'ew-loading', role: 'status' }, t('common.loading'));
}

/** Page heading with optional actions. */
export function pageHeader(title: string, ...actions: Child[]): HTMLElement {
  return h('div', { class: 'ew-page-header' }, h('h1', null, title), actions.length ? h('div', { class: 'ew-actions' }, ...actions) : null);
}

/** Inputs for a multi-language text (one input per configured language). */
export function langFields(label: string, values: Record<string, string>, opts: { multiline?: boolean; required?: boolean } = {}): { el: HTMLElement; get: () => Record<string, string> } {
  const inputs: Record<string, HTMLInputElement | HTMLTextAreaElement> = {};
  const rows = state.site.languages.map((lang) => {
    const ctl = opts.multiline ? textArea({ value: values[lang] ?? '', lang, rows: 2 }) : textInput({ value: values[lang] ?? '', lang, maxlength: 200 });
    inputs[lang] = ctl;
    const langName = t(`lang.${lang}`) === `lang.${lang}` ? lang.toUpperCase() : t(`lang.${lang}`);
    return field(`${label} (${langName})`, ctl, { required: opts.required && lang === state.site.default_language });
  });
  return {
    el: h('fieldset', { class: 'ew-langfields' }, h('legend', null, label), ...rows),
    get: () => Object.fromEntries(Object.entries(inputs).map(([k, v]) => [k, v.value.trim()]).filter(([, v]) => v)),
  };
}

// ---------------------------------------------------------------- dialogs

export interface DialogHandle {
  dialog: HTMLDialogElement;
  close: () => void;
}

/** Opens a modal dialog (native <dialog>: focus trap, Escape to close). */
export function openDialog(title: string, content: Child, actions: Child[] = [], opts: { onClose?: () => void; wide?: boolean } = {}): DialogHandle {
  const opener = document.activeElement as HTMLElement | null;
  const titleId = uid('dlg');
  const dialog = h('dialog', { class: ['ew-dialog', opts.wide && 'ew-dialog--wide'], 'aria-labelledby': titleId },
    h('div', { class: 'ew-dialog-head' },
      h('h2', { id: titleId }, title),
      button(h('span', { 'aria-hidden': 'true' }, '×'), () => close(), { variant: 'subtle', ariaLabel: t('common.close') })),
    h('div', { class: 'ew-dialog-body' }, content),
    actions.length ? h('div', { class: 'ew-dialog-actions' }, ...actions) : null,
  );
  const close = () => {
    if (dialog.open) dialog.close();
  };
  dialog.addEventListener('close', () => {
    dialog.remove();
    opts.onClose?.();
    opener?.focus?.();
  });
  document.body.append(dialog);
  dialog.showModal();
  const first = dialog.querySelector<HTMLElement>('.ew-dialog-body input, .ew-dialog-body select, .ew-dialog-body textarea, .ew-dialog-body button, .ew-dialog-actions button');
  first?.focus();
  return { dialog, close };
}

/** Asks for confirmation. */
export function confirmDialog(message: string, confirmLabel = t('common.confirm'), danger = false): Promise<boolean> {
  return new Promise((resolve) => {
    let result = false;
    const d = openDialog(t('common.confirm_title'), h('p', null, message), [
      button(t('common.cancel'), () => d.close()),
      button(confirmLabel, () => { result = true; d.close(); }, { variant: danger ? 'danger' : 'primary' }),
    ], { onClose: () => resolve(result) });
  });
}

/**
 * A dialog with a form. onSubmit may throw; the error is shown inside the
 * dialog (role=alert) and the dialog stays open.
 */
export function formDialog(title: string, fields: Child, submitLabel: string, onSubmit: (form: HTMLFormElement) => Promise<void>, opts: { wide?: boolean } = {}): DialogHandle {
  const alert = h('div', { class: 'ew-form-alert', role: 'alert' });
  const submit = button(submitLabel, () => undefined, { variant: 'primary', type: 'submit' });
  const form = h('form', { class: 'ew-form', novalidate: true }, alert, fields, h('div', { class: 'ew-dialog-actions' }, button(t('common.cancel'), () => handle.close()), submit));
  const handle = openDialog(title, form, [], opts);
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alert.textContent = '';
    for (const el of Array.from(form.querySelectorAll<HTMLInputElement>('[aria-required="true"]'))) {
      if (!el.value.trim()) {
        setFieldError(el, t('error.required'));
        el.focus();
        return;
      }
      setFieldError(el, '');
    }
    submit.disabled = true;
    try {
      await onSubmit(form);
      handle.close();
    } catch (e) {
      alert.textContent = errorText(e);
    } finally {
      submit.disabled = false;
    }
  });
  return handle;
}
