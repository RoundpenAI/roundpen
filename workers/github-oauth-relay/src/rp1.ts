/** rp1 install tokens — same contract as internal/installid and docs/free-llm-token.md */

export type Rp1 = {
  installId: string
  expUnix: number
  sig: Uint8Array
}

export function parseRp1(token: string): Rp1 {
  const parts = token.split('.')
  if (parts.length !== 4 || parts[0] !== 'rp1') {
    throw new Error('install token is malformed')
  }
  const expUnix = Number(parts[2])
  if (!Number.isInteger(expUnix) || expUnix <= 0) {
    throw new Error('install token has a bad expiry')
  }
  const sig = b64urlDecode(parts[3])
  if (sig.byteLength !== 64) {
    throw new Error('install token has a bad signature')
  }
  return { installId: parts[1], expUnix, sig }
}

export function signMessage(installId: string, expUnix: number): Uint8Array {
  return new TextEncoder().encode(`rp1|${installId}|${expUnix}`)
}

export async function verifyRp1(
  token: string,
  publicKeyHex: string,
  nowUnix: number,
): Promise<Rp1> {
  const parsed = parseRp1(token)
  if (nowUnix > parsed.expUnix) {
    throw new Error('install token expired')
  }
  const pub = hexToBytes(publicKeyHex)
  if (pub.byteLength !== 32) {
    throw new Error('install public key must be 32-byte hex')
  }
  const key = await crypto.subtle.importKey('raw', pub, { name: 'Ed25519' }, false, ['verify'])
  const ok = await crypto.subtle.verify('Ed25519', key, parsed.sig, signMessage(parsed.installId, parsed.expUnix))
  if (!ok) {
    throw new Error('install token signature rejected')
  }
  return parsed
}

export function hexToBytes(hex: string): Uint8Array {
  const h = hex.trim().toLowerCase()
  if (h.length % 2 !== 0 || !/^[0-9a-f]+$/.test(h)) {
    throw new Error('invalid hex')
  }
  const out = new Uint8Array(h.length / 2)
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(h.slice(i * 2, i * 2 + 2), 16)
  }
  return out
}

export function b64urlDecode(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4))
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + pad
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export function b64urlEncode(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
