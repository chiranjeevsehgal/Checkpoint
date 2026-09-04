#pragma once
#include <Arduino.h>
#include <SPI.h>
#include "config.h"

#if SD_USE_SDMMC
#include <SD_MMC.h>
#define FS_SD SD_MMC
#ifndef SD
#define SD SD_MMC
#endif
#else
#include <SD.h>
#define FS_SD SD
#endif

bool sd_present();
bool sd_begin();
void sd_end();
bool sd_mounted();
bool sd_ensure_rec_dir();
bool sd_list_files(String *out_names, size_t *out_count, size_t max_count);
bool sd_file_size(const String &path, uint32_t *out_size);
uint32_t sd_file_crc32(const String &path);
bool sd_file_crc32_cooperative(const String &path, uint32_t *out_crc, size_t chunk_bytes = 4096);
bool sd_safe_delete_after_ack(const String &path);
bool sd_rename_atomic(const String &from, const String &to);
bool sd_write_atomic(const String &path, const uint8_t *data, size_t len);
bool sd_patch_wav_header(const String &path);
bool sd_patch_wav_header_locked(const String &path);
bool sd_lock(uint32_t timeout_ms = 2000);
void sd_unlock();
