const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

const REVERSE: Record<string, number> = {};
for (let i = 0; i < ALPHABET.length; i++) REVERSE[ALPHABET[i]!] = i;

export function base64Encode(bytes: Uint8Array): string {
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const a = bytes[i]!;
    const b = i + 1 < bytes.length ? bytes[i + 1]! : 0;
    const c = i + 2 < bytes.length ? bytes[i + 2]! : 0;
    const triple = (a << 16) | (b << 8) | c;
    out += ALPHABET[(triple >>> 18) & 0x3f]!;
    out += ALPHABET[(triple >>> 12) & 0x3f]!;
    out += i + 1 < bytes.length ? ALPHABET[(triple >>> 6) & 0x3f]! : '=';
    out += i + 2 < bytes.length ? ALPHABET[triple & 0x3f]! : '=';
  }
  return out;
}

export function base64Decode(text: string): Uint8Array {
  const clean = text.replace(/\s/g, '');
  if (clean.length % 4 !== 0) throw new Error('Invalid base64 length');
  const pad = clean.endsWith('==') ? 2 : clean.endsWith('=') ? 1 : 0;
  const out = new Uint8Array((clean.length / 4) * 3 - pad);
  let pos = 0;
  for (let i = 0; i < clean.length; i += 4) {
    const sextets = [0, 0, 0, 0];
    for (let j = 0; j < 4; j++) {
      const ch = clean[i + j]!;
      if (ch === '=') {
        sextets[j] = 0;
      } else {
        const value = REVERSE[ch];
        if (value === undefined) throw new Error(`Invalid base64 char: ${ch}`);
        sextets[j] = value;
      }
    }
    const triple = (sextets[0]! << 18) | (sextets[1]! << 12) | (sextets[2]! << 6) | sextets[3]!;
    if (pos < out.length) out[pos++] = (triple >>> 16) & 0xff;
    if (pos < out.length) out[pos++] = (triple >>> 8) & 0xff;
    if (pos < out.length) out[pos++] = triple & 0xff;
  }
  return out;
}
