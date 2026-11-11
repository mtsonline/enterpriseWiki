// WebAuthn in the browser: the server sends the options as JSON (binary
// values base64url encoded); the browser API wants ArrayBuffers, and the
// answer goes back as JSON again.

const b64urlToBuf = (s: string): ArrayBuffer => {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(s.length / 4) * 4, '=');
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
};

const bufToB64url = (b: ArrayBuffer | ArrayBufferView): string => {
  const bytes = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  let bin = '';
  for (const x of bytes) bin += String.fromCharCode(x);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

type Json = Record<string, unknown>;

/** True if the browser can use security keys and passkeys. */
export function webauthnSupported(): boolean {
  return typeof window.PublicKeyCredential === 'function' && !!navigator.credentials;
}

const descriptors = (list: unknown): PublicKeyCredentialDescriptor[] | undefined =>
  Array.isArray(list) ? list.map((d: Json) => ({ ...(d as object), id: b64urlToBuf(String(d.id)) }) as PublicKeyCredentialDescriptor) : undefined;

/** Registers a new credential with the options of the server ({publicKey}). */
export async function webauthnCreate(options: Json): Promise<Json> {
  const pk = (options.publicKey ?? options) as Json;
  const user = pk.user as Json;
  const cred = await navigator.credentials.create({
    publicKey: {
      ...(pk as object),
      challenge: b64urlToBuf(String(pk.challenge)),
      user: { ...(user as object), id: b64urlToBuf(String(user.id)) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    } as PublicKeyCredentialCreationOptions,
  }) as PublicKeyCredential | null;
  if (!cred) throw new DOMException('no credential', 'NotAllowedError');
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id, rawId: bufToB64url(cred.rawId), type: cred.type, authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: bufToB64url(r.clientDataJSON), attestationObject: bufToB64url(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : undefined,
    },
  };
}

/** Signs a challenge with the options of the server ({publicKey}). */
export async function webauthnGet(options: Json): Promise<Json> {
  const pk = (options.publicKey ?? options) as Json;
  const cred = await navigator.credentials.get({
    publicKey: { ...(pk as object), challenge: b64urlToBuf(String(pk.challenge)), allowCredentials: descriptors(pk.allowCredentials) } as PublicKeyCredentialRequestOptions,
  }) as PublicKeyCredential | null;
  if (!cred) throw new DOMException('no credential', 'NotAllowedError');
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id, rawId: bufToB64url(cred.rawId), type: cred.type, authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: bufToB64url(r.clientDataJSON), authenticatorData: bufToB64url(r.authenticatorData),
      signature: bufToB64url(r.signature), userHandle: r.userHandle ? bufToB64url(r.userHandle) : undefined,
    },
  };
}

/** True for the errors of a cancelled or timed-out WebAuthn ceremony. */
export function webauthnCancelled(e: unknown): boolean {
  return e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError');
}
