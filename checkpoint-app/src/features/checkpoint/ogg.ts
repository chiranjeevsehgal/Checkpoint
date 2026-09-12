const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

export function isOggOpus(data: Uint8Array): boolean {
  return (
    data.length >= 4 && data[0] === 0x4f && data[1] === 0x67 && data[2] === 0x67 && data[3] === 0x53
  );
}

export function oggCrc(data: Uint8Array): number {
  let crc = 0;
  for (const byte of data) {
    crc ^= byte << 24;
    for (let i = 0; i < 8; i++) {
      if (crc & 0x80000000) {
        crc = ((crc << 1) & 0xffffffff) ^ 0x04c11db7;
      } else {
        crc = (crc << 1) & 0xffffffff;
      }
    }
  }
  return crc >>> 0;
}

export function makeOggPage(
  headerType: number,
  granulePosition: bigint,
  serial: number,
  sequence: number,
  segments: number[],
  payload: Uint8Array,
): Uint8Array {
  const page = new Uint8Array(27 + segments.length + payload.length);
  const view = new DataView(page.buffer);
  page.set(textEncoder.encode('OggS'), 0);
  page[4] = 0;
  page[5] = headerType & 0xff;
  view.setBigUint64(6, granulePosition, true);
  view.setUint32(14, serial >>> 0, true);
  view.setUint32(18, sequence >>> 0, true);
  page[26] = segments.length & 0xff;
  for (let i = 0; i < segments.length; i++) page[27 + i] = segments[i]! & 0xff;
  page.set(payload, 27 + segments.length);
  view.setUint32(22, oggCrc(page), true);
  return page;
}

export function parseOggPages(data: Uint8Array): Uint8Array[] {
  const pages: Uint8Array[] = [];
  let pos = 0;
  while (pos < data.length) {
    if (
      data[pos] !== 0x4f ||
      data[pos + 1] !== 0x67 ||
      data[pos + 2] !== 0x67 ||
      data[pos + 3] !== 0x53
    ) {
      throw new Error(`Invalid OGG page at byte ${pos}`);
    }
    const segmentCount = data[pos + 26]!;
    const tableEnd = pos + 27 + segmentCount;
    let bodyLen = 0;
    for (let i = 0; i < segmentCount; i++) bodyLen += data[pos + 27 + i]!;
    pages.push(data.slice(pos, tableEnd + bodyLen));
    pos = tableEnd + bodyLen;
  }
  return pages;
}

function startsWith(data: Uint8Array, text: string): boolean {
  const prefix = textEncoder.encode(text);
  if (data.length < prefix.length) return false;
  for (let i = 0; i < prefix.length; i++) {
    if (data[i] !== prefix[i]) return false;
  }
  return true;
}

export function repairOpusOgg(data: Uint8Array): Uint8Array {
  const pages = parseOggPages(data);
  const first = pages[0];
  if (!first) throw new Error('No OGG pages found');
  const view = new DataView(first.buffer, first.byteOffset, first.byteLength);
  const serial = view.getUint32(14, true);
  const segmentCount = first[26]!;
  const segments: number[] = [];
  for (let i = 0; i < segmentCount; i++) segments.push(first[27 + i]!);
  const payload = first.slice(27 + segmentCount);
  if (startsWith(payload, 'OpusHead') && segments.length === 1) return data;
  if (segments.length < 2) {
    throw new Error(`Unexpected OGG layout: ${segments.join(',')}`);
  }
  const headSize = segments[0]!;
  const tagsSize = segments[1]!;
  const opusHead = payload.slice(0, headSize);
  const opusTags = payload.slice(headSize, headSize + tagsSize);
  if (!startsWith(opusHead, 'OpusHead')) throw new Error('OpusHead not found');
  if (!startsWith(opusTags, 'OpusTags')) throw new Error('OpusTags not found');
  const fixed: Uint8Array[] = [
    makeOggPage(0x02, BigInt(0), serial, 0, [headSize], opusHead),
    makeOggPage(0x00, BigInt(0), serial, 1, [tagsSize], opusTags),
  ];
  for (const original of pages.slice(1)) {
    const page = Uint8Array.from(original);
    const pageView = new DataView(page.buffer);
    if (pageView.getUint32(14, true) === serial) {
      const oldSeq = pageView.getUint32(18, true);
      pageView.setUint32(18, (oldSeq + 1) >>> 0, true);
      pageView.setUint32(22, 0, true);
      pageView.setUint32(22, oggCrc(page), true);
    }
    fixed.push(page);
  }
  const total = fixed.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const page of fixed) {
    out.set(page, offset);
    offset += page.length;
  }
  return out;
}

export function decodeUtf8(data: Uint8Array): string {
  return textDecoder.decode(data);
}
