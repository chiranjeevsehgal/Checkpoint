#pragma once
#include <Arduino.h>
#include "config.h"

#define OGG_SERIAL 0x12345678

static inline uint32_t ogg_crc32_update(uint32_t crc, const uint8_t *buf, size_t len) {
  for (size_t i = 0; i < len; i++) {
    crc ^= ((uint32_t)buf[i] << 24);
    for (int b = 0; b < 8; b++) {
      if (crc & 0x80000000) crc = (crc << 1) ^ 0x04C11DB7;
      else crc <<= 1;
    }
  }
  return crc;
}

static inline size_t ogg_build_page(uint8_t *out, uint64_t granule, uint32_t seq, uint8_t hdr_type,
                                    const uint8_t *packets[], const size_t lens[], int pkt_count,
                                    uint32_t serial) {
  uint8_t lacing[255];
  size_t lacing_len = 0;
  for (int p=0;p<pkt_count;p++) {
    size_t remaining = lens[p];
    while (remaining >= 255) { lacing[lacing_len++]=255; remaining-=255; if(lacing_len>=255) break; }
    lacing[lacing_len++] = (uint8_t)remaining;
  }
  size_t hdr = 27 + lacing_len;
  memcpy(out, "OggS",4);
  out[4]=0; out[5]=hdr_type;
  for(int i=0;i<8;i++) out[6+i]=(granule>>(i*8))&0xFF;
  for(int i=0;i<4;i++) out[14+i]=(serial>>(i*8))&0xFF;
  for(int i=0;i<4;i++) out[18+i]=(seq>>(i*8))&0xFF;
  out[22]=0; out[23]=0; out[24]=0; out[25]=0;
  out[26]=(uint8_t)lacing_len;
  memcpy(out+27, lacing, lacing_len);
  size_t off = hdr;
  for(int p=0;p<pkt_count;p++) { memcpy(out+off, packets[p], lens[p]); off+=lens[p]; }
  uint32_t crc = ogg_crc32_update(0, out, off);
  out[22]=crc&0xFF; out[23]=(crc>>8)&0xFF; out[24]=(crc>>16)&0xFF; out[25]=(crc>>24)&0xFF;
  return off;
}

static inline void ogg_opus_head(uint8_t *out) {
  memcpy(out, "OpusHead",8);
  out[8]=1; out[9]=1;
  // pre-skip 312 LE = Opus 16k VOIP lookahead in 48k ticks (was 6000 = 125ms gap)
  out[10]=0x38; out[11]=0x01;
  // input sample rate 16000 LE (was 48000 0xBB80 -> naive players played 3x fast)
  out[12]=0x80; out[13]=0x3E; out[14]=0x00; out[15]=0x00;
  out[16]=0x00; out[17]=0x00; out[18]=0x00;
}
static inline size_t ogg_opus_tags(uint8_t *out) {
  const char *vendor = "Checkpoint16k";
  size_t vl = strlen(vendor);
  memcpy(out, "OpusTags",8);
  out[8]=vl&0xFF; out[9]=(vl>>8)&0xFF; out[10]=(vl>>16)&0xFF; out[11]=(vl>>24)&0xFF;
  memcpy(out+12, vendor, vl);
  size_t off=12+vl;
  out[off]=0; out[off+1]=0; out[off+2]=0; out[off+3]=0;
  return off+4;
}

static inline size_t ogg_write_bos(File &f, uint32_t serial) {
  uint8_t head[19]; ogg_opus_head(head);
  uint8_t tags[64]; size_t tlen = ogg_opus_tags(tags);
  const uint8_t *pkts[2]={head, tags};
  size_t lens[2]={19, tlen};
  uint8_t page[300];
  size_t n = ogg_build_page(page, 0, 0, 0x02, pkts, lens, 2, serial);
  return f.write(page, n);
}
static inline size_t ogg_write_audio_frame(File &f, const uint8_t *opus, size_t olen, uint64_t granule, uint32_t seq, uint32_t serial) {
  const uint8_t *pkts[1]={opus};
  size_t lens[1]={olen};
  uint8_t page[350];
  size_t n = ogg_build_page(page, granule, seq, 0x00, pkts, lens, 1, serial);
  return f.write(page, n);
}
static inline size_t ogg_write_eos(File &f, uint64_t granule, uint32_t seq, uint32_t serial) {
  uint8_t page[64];
  memcpy(page, "OggS",4); page[4]=0; page[5]=0x04;
  for(int i=0;i<8;i++) page[6+i]=(granule>>(i*8))&0xFF;
  for(int i=0;i<4;i++) page[14+i]=(serial>>(i*8))&0xFF;
  for(int i=0;i<4;i++) page[18+i]=(seq>>(i*8))&0xFF;
  page[22]=0;page[23]=0;page[24]=0;page[25]=0; page[26]=0;
  uint32_t crc=ogg_crc32_update(0, page, 27);
  page[22]=crc&0xFF; page[23]=(crc>>8)&0xFF; page[24]=(crc>>16)&0xFF; page[25]=(crc>>24)&0xFF;
  return f.write(page, 27);
}

