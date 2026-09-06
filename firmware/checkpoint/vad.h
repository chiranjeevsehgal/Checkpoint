#pragma once
// Voice Activity Detection — WebRTC-style spectral VAD + debounced state machine.
//
// Design: per 20ms frame (320 samples @16k mono, the native Opus frame size),
// splits energy into 3 sub-bands with stateful one-pole filters, tracks an
// adaptive noise floor per band (min-statistics), and scores band-SNR with a
// zero-crossing guard. Aggressiveness modes 0..3 map to WebRTC VAD modes
// (0 permissive .. 3 aggressive). This is speech detection, not a fixed
// loudness threshold: flat-spectrum noise at the same RMS scores lower than
// formant-structured speech.
//
// State machine (frame-counted, all derives from ms config):
//   IDLE --[~50% voiced density over trailing 200ms onset window (5/10)
//           AND >=2 consecutive voiced frames (40ms)
//           AND syllabic energy swing (IAC: 6dB-hysteresis flips >=1 + >=3.5dB
//           range over 400ms) AND IDLE-only impulse veto (crest <= 10)]-->
//           ONSET -> SPEECH
//     (recall-first: onset is moderately easy, then MIN_SPEECH on true
//      voiced frames discards blips afterward — a false clip can be
//      deleted, a missed word cannot be recovered. NOTE: the 750ms
//      pre-roll preserves audio from before the trigger but cannot cause
//      one — onset still needs ~100ms of voiced evidence, so isolated
//      sub-100ms clicks cannot trigger even though they would be
//      preserved if one did.)
//   SPEECH --[HANGOVER_MS silent]--> PAUSE (file stays open, writes suspended)
//   SPEECH --[2s voiced-but-flat]--> DISCARD (hum/tune: purge, drop file)
//   PAUSE --[voiced]--> SPEECH (resume same file)
//   PAUSE --[session end + trailing 2s flat AND loud]--> DISCARD (adapted hum)
//   PAUSE --[session end + >=70% voiced frames on whistle shelf]--> DISCARD
//     (human whistle: R~1.8 + ZCR~0.26 sustained; speech only brushes it)
//   PAUSE --[session end, normal]--> OFFSET -> IDLE (close chunk, keep file)
//
// Pure DSP + counters. No Arduino deps (host-testable with g++).
// Threading: call vad_process_frame+vad_update from ONE task (opus_encode_task).
// recorder_task only reads vad_should_record()/polls event flags.
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// Native frame geometry. WebRTC VAD requires 10/20/30ms; we fix 20ms @16k
// because that is exactly REC_OPUS_SAMPLES_PER_FRAME (320).
#define VAD_SAMPLE_RATE 16000
#define VAD_FRAME_MS 20
#define VAD_FRAME_SAMPLES 320

enum VadEvent {
  VAD_EV_SILENCE = 0, // still idle / still paused, nothing to do
  VAD_EV_SPEECH = 1,  // still in speech, keep recording
  VAD_EV_ONSET = 2,   // speech started -> open chunk, flush pre-roll
  VAD_EV_OFFSET = 3,  // utterance over -> close chunk
  VAD_EV_PAUSE = 4,   // hangover expired -> suspend writes, keep file open
  VAD_EV_DISCARD = 5  // sustained flat non-speech (hum) -> purge + drop file
};

enum VadState {
  VAD_ST_IDLE = 0,
  VAD_ST_SPEECH = 1,
  VAD_ST_PAUSE = 2
};

struct VadConfig {
  int mode;              // 0..3 aggressiveness (default 2, WebRTC mapping)
  int onset_ms;          // trailing density window; fires at ~50% voiced (default 200)
  int hangover_ms;       // silence to suspend writes (default 1500)
  int session_extend_ms; // extra silence to keep file open (default 2000)
  int min_speech_ms;     // true voiced frames below this are discarded (default 120)
  int preroll_ms;        // pre-roll to flush on onset (default 750)
  float abs_floor_dbfs;  // below this level force unvoiced (default -50)
};

void vad_configure(const struct VadConfig *cfg);
void vad_init(int mode); // shorthand: default timings + mode
void vad_reset(void);

// Returns 1 voiced, 0 silence, -1 error (null/bad length).
int vad_process_frame(const int16_t *pcm, size_t n);

// Feed the per-frame voiced flag through the debounce machine.
// Returns the event for this frame.
enum VadEvent vad_update(int voiced);

enum VadState vad_state(void);
// True while file should stay open (SPEECH or PAUSE-session).
int vad_should_record(void);
int vad_is_speech(void);
float vad_level_dbfs(void); // last frame level, dBFS
float vad_mod_db(void); // trailing-400ms mid-band energy range (hum ~1-2, speech 6-15)
unsigned vad_utterances(void); // completed utterances since init
unsigned vad_utterance_frames(void); // voiced+hangover frames in current utterance
int vad_in_session(void); // 1 while SPEECH or PAUSE

#ifdef __cplusplus
}
#endif
