// API client. The frontend uses exactly the public REST API.

export class ApiError extends Error {
  /** detail: e.g. the token of a sign-in that waits for linking */
  constructor(public status: number, public code: string, message: string, public detail = '') {
    super(message);
  }
}

// Responses embedded by the guest cache into the page (<script id="ew-preload">):
// used once each, only right after the page was loaded.
let preload: Record<string, unknown> | null = null;
const loadedAt = Date.now();
try {
  const el = typeof document !== 'undefined' ? document.getElementById('ew-preload') : null;
  if (el?.textContent) preload = JSON.parse(el.textContent) as Record<string, unknown>;
  el?.remove();
} catch {
  preload = null;
}

function fromPreload(url: string): { hit: boolean; data?: unknown } {
  if (!preload || Date.now() - loadedAt > 15000) {
    preload = null;
    return { hit: false };
  }
  const u = new URL(url, location.origin);
  const key = u.pathname + u.search;
  if (!(key in preload)) return { hit: false };
  const data = preload[key];
  delete preload[key];
  return { hit: true, data };
}

/**
 * A change that gets no answer: after waitNotice seconds the user is asked
 * whether to keep waiting or to cancel. Nothing typed is lost either way:
 * the request keeps running while waiting, and cancelling leaves the page
 * (and its editor) as it is. The dialog is provided by the UI (setWaitPrompt),
 * so this module stays free of markup.
 */
export interface WaitPrompt { close: () => void }
let waitNotice = 5;
let waitPrompt: ((keepWaiting: () => void, cancel: () => void) => WaitPrompt) | null = null;
export function setWaitNotice(seconds: number | undefined): void { if (seconds && seconds > 0) waitNotice = seconds; }
export function setWaitPrompt(fn: typeof waitPrompt): void { waitPrompt = fn; }

/** fetch for changes: asks the user after waitNotice seconds without an answer. */
async function patientFetch(url: string, init: RequestInit): Promise<Response> {
  if (!waitPrompt || init.method === 'GET') return fetch(url, init);
  const ctl = new AbortController();
  let prompt: WaitPrompt | null = null;
  let timer = 0;
  const arm = () => {
    timer = window.setTimeout(() => {
      prompt = waitPrompt!(() => { prompt = null; arm(); }, () => { prompt = null; ctl.abort(); });
    }, waitNotice * 1000);
  };
  arm();
  try {
    return await fetch(url, { ...init, signal: ctl.signal });
  } finally {
    window.clearTimeout(timer);
    (prompt as WaitPrompt | null)?.close();
  }
}

async function request<T>(method: string, path: string, body?: unknown, background = false): Promise<T> {
  const url = path.startsWith('/') ? path : `/api/v1/${path}`;
  if (method === 'GET') {
    const p = fromPreload(url);
    if (p.hit) return p.data as T;
  }
  const headers: Record<string, string> = { 'X-Requested-With': 'ewiki', Accept: 'application/json' };
  // automatic requests (polling) do not count as activity for the idle timeout
  if (background) headers['X-Ewiki-Background'] = '1';
  const init: RequestInit = { method, headers, credentials: 'same-origin' };
  if (body instanceof Blob) {
    headers['Content-Type'] = body.type || 'application/octet-stream';
    init.body = body; // e.g. an import package as is
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await patientFetch(url, init);
  } catch (e) {
    if ((e as Error)?.name === 'AbortError') throw new ApiError(0, 'cancelled', 'cancelled');
    throw new ApiError(0, 'network', 'network error');
  }
  if (res.status === 204) return undefined as T;
  let data: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; detail?: string } } | null)?.error;
    throw new ApiError(res.status, err?.code ?? 'error', err?.message ?? res.statusText, err?.detail ?? '');
  }
  return data as T;
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  /** GET that the server does not count as activity of the user (polling). */
  poll: <T>(p: string) => request<T>('GET', p, undefined, true),
  post: <T>(p: string, b: unknown = {}) => request<T>('POST', p, b),
  put: <T>(p: string, b: unknown) => request<T>('PUT', p, b),
  patch: <T>(p: string, b: unknown) => request<T>('PATCH', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
};

/** Builds a query string, skipping empty values. */
export function qs(params: Record<string, string | number | undefined | null>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== '') u.set(k, String(v));
  const s = u.toString();
  return s ? `?${s}` : '';
}

export interface UploadedFile {
  hash: string;
  name: string;
  mime: string;
  size: number;
}

/** Uploads a file (multipart) to /api/v1/files. */
export async function uploadFile(file: File): Promise<UploadedFile> {
  const fd = new FormData();
  fd.append('file', file, file.name);
  let res: Response;
  try {
    res = await fetch('/api/v1/files', { method: 'POST', body: fd, credentials: 'same-origin', headers: { 'X-Requested-With': 'ewiki' } });
  } catch {
    throw new ApiError(0, 'network', 'network error');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, data?.error?.code ?? 'error', data?.error?.message ?? res.statusText);
  return data as UploadedFile;
}

/** Sends a file as the raw request body (e.g. translation imports); returns the JSON answer. */
export async function postRaw<T>(path: string, body: Blob, contentType = 'application/octet-stream'): Promise<T> {
  const url = path.startsWith('/') ? path : `/api/v1/${path}`;
  let res: Response;
  try {
    res = await fetch(url, { method: 'POST', body, credentials: 'same-origin', headers: { 'X-Requested-With': 'ewiki', 'Content-Type': contentType, Accept: 'application/json' } });
  } catch {
    throw new ApiError(0, 'network', 'network error');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, data?.error?.code ?? 'error', data?.error?.message ?? res.statusText);
  return data as T;
}

/** Formats a byte count. */
export function humanSize(n: number): string {
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${i ? v.toFixed(1) : v} ${u[i]}`;
}
