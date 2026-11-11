// Live editing: a Yjs document shared over the WebSocket relay of the
// server (/api/v1/collab/elements/{id}/{lang}, y-protocols messages).
//
// The server keeps no document: the first editor of a page fills the shared
// document from the saved Markdown, later editors get it from the others.
import * as decoding from 'lib0/decoding';
import * as encoding from 'lib0/encoding';
import { Awareness, applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates } from 'y-protocols/awareness';
import * as syncProtocol from 'y-protocols/sync';
import * as Y from 'yjs';

const MSG_SYNC = 0;
const MSG_AWARENESS = 1;
const MSG_SAVED = 3;

export interface Peer { id: number; name: string }

export interface CollabEvents {
  /** The other editors changed (leader: this editor joined first). */
  onPeers?(peers: Peer[], leader: boolean): void;
  /** Another editor saved revision rev. */
  onSaved?(rev: number, by: Peer | undefined): void;
  /** The connection is lost for good (e.g. no longer allowed to edit). */
  onClosed?(reason: string): void;
}

/** How the document got its content: from this editor, or from the others. */
export type Start = 'first' | 'synced';

/** Accessible colors for the cursors of others (white text >= 4.5:1). */
const COLORS = ['#0b5cad', '#a61f17', '#17602d', '#6b2fb3', '#8f3f00', '#005f73', '#7a1f5c', '#3b4048'];
export const colorOf = (id: number): string => COLORS[Math.abs(id) % COLORS.length];

export class Collab {
  readonly doc = new Y.Doc();
  readonly fragment = this.doc.getXmlFragment('prosemirror');
  readonly awareness = new Awareness(this.doc);
  peers: Peer[] = [];
  leader = false;
  /** Resolves once the document is ready to edit (or rejects: no live editing). */
  readonly ready: Promise<Start>;
  private ws: WebSocket | null = null;
  private resolve!: (s: Start) => void;
  private reject!: (e: Error) => void;
  private started = false;
  private closed = false;
  private retry = 0;
  private timer = 0;

  constructor(private url: string, private ev: CollabEvents = {}) {
    this.ready = new Promise<Start>((res, rej) => { this.resolve = res; this.reject = rej; });
    this.doc.on('update', (update: Uint8Array, origin: unknown) => {
      if (origin === this) return;
      const e = encoding.createEncoder();
      encoding.writeVarUint(e, MSG_SYNC);
      syncProtocol.writeUpdate(e, update);
      this.send(encoding.toUint8Array(e));
    });
    this.awareness.on('update', ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }, origin: unknown) => {
      if (origin === this) return;
      const changed = [...added, ...updated, ...removed].filter((c) => c === this.doc.clientID);
      if (!changed.length) return;
      const e = encoding.createEncoder();
      encoding.writeVarUint(e, MSG_AWARENESS);
      encoding.writeVarUint8Array(e, encodeAwarenessUpdate(this.awareness, changed));
      this.send(encoding.toUint8Array(e));
    });
    this.connect();
    // no answer in time: edit without live editing
    window.setTimeout(() => { if (!this.started) this.fail(new Error('timeout')); }, 8000);
  }

  private connect(): void {
    if (this.closed) return;
    const ws = new WebSocket(this.url);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onopen = () => {
      this.retry = 0;
      const e = encoding.createEncoder();
      encoding.writeVarUint(e, MSG_SYNC);
      syncProtocol.writeSyncStep1(e, this.doc);
      this.send(encoding.toUint8Array(e));
      if (this.awareness.getLocalState()) {
        const a = encoding.createEncoder();
        encoding.writeVarUint(a, MSG_AWARENESS);
        encoding.writeVarUint8Array(a, encodeAwarenessUpdate(this.awareness, [this.doc.clientID]));
        this.send(encoding.toUint8Array(a));
      }
    };
    ws.onmessage = (m) => {
      if (typeof m.data === 'string') this.control(m.data);
      else this.message(new Uint8Array(m.data as ArrayBuffer));
    };
    ws.onclose = (c) => {
      this.ws = null;
      removeAwarenessStates(this.awareness, Array.from(this.awareness.getStates().keys()).filter((k) => k !== this.doc.clientID), this);
      if (this.closed) return;
      if (c.code === 1008 || c.code === 1013 || (!this.started && this.retry >= 2)) {
        // refused (rights, too many editors) or not available
        this.fail(new Error(c.reason || 'closed'));
        return;
      }
      this.retry++;
      this.timer = window.setTimeout(() => this.connect(), Math.min(30000, 500 * 2 ** this.retry));
    };
  }

  private fail(e: Error): void {
    if (!this.started) {
      this.started = true;
      this.reject(e);
    } else {
      this.ev.onClosed?.(e.message);
    }
    this.destroy();
  }

  private control(text: string): void {
    let c: { type?: string; peers?: number; leader?: boolean; first?: boolean; users?: Peer[] };
    try { c = JSON.parse(text) as typeof c; } catch { return; }
    if (c.type !== 'peers') return;
    this.peers = c.users ?? [];
    this.leader = !!c.leader;
    this.ev.onPeers?.(this.peers, this.leader);
    if (!this.started && c.first) {
      this.started = true;
      this.resolve('first');
    }
  }

  private message(data: Uint8Array): void {
    const d = decoding.createDecoder(data);
    const type = decoding.readVarUint(d);
    if (type === MSG_SYNC) {
      const e = encoding.createEncoder();
      encoding.writeVarUint(e, MSG_SYNC);
      const kind = syncProtocol.readSyncMessage(d, e, this.doc, this);
      if (encoding.length(e) > 1) this.send(encoding.toUint8Array(e));
      if (kind === syncProtocol.messageYjsSyncStep2 && !this.started) {
        this.started = true;
        this.resolve('synced');
      }
    } else if (type === MSG_AWARENESS) {
      applyAwarenessUpdate(this.awareness, decoding.readVarUint8Array(d), this);
    } else if (type === MSG_SAVED) {
      const rev = decoding.readVarUint(d);
      const by = decoding.hasContent(d) ? decoding.readVarUint(d) : 0;
      this.ev.onSaved?.(rev, this.peers.find((p) => p.id === by));
    }
  }

  private send(b: Uint8Array): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(b as Uint8Array<ArrayBuffer>);
  }

  /** Tells the others that revision rev was saved (by user id). */
  saved(rev: number, by: number): void {
    const e = encoding.createEncoder();
    encoding.writeVarUint(e, MSG_SAVED);
    encoding.writeVarUint(e, rev);
    encoding.writeVarUint(e, by);
    this.send(encoding.toUint8Array(e));
  }

  destroy(): void {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.timer);
    removeAwarenessStates(this.awareness, [this.doc.clientID], 'local');
    this.ws?.close(1000);
    this.ws = null;
    this.awareness.destroy();
  }
}

/** The address of the live editing connection of an element. */
export function collabURL(elementId: number, lang: string): string {
  return `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/collab/elements/${elementId}/${encodeURIComponent(lang)}`;
}
