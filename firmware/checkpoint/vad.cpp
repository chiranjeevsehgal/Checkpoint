// See vad.h for the design note. Self-contained: no Arduino, no DSP lib.
#include "vad.h"
#include <math.h>
#include <string.h>

#ifndef VAD_DEFAULT_MODE
#define VAD_DEFAULT_MODE 2
#endif

// Mode thresholds on the weighted band-SNR score (dB). Higher = pickier.
// Calibrated so quiet-room speech (~-35..-20 dBFS, formant-structured)
// triggers on modes 0..3, while flat white/pink noise at the same RMS does
// not trigger on modes 2..3.
static const float kScoreThresh[4] = {2.5f, 4.0f, 5.5f, 7.0f};
// Absolute floors per mode (dBFS): below this, force unvoiced even if the
// SNR score is high (prevents amplifying mic self-noise into triggers).
static const float kAbsFloor[4] = {-55.0f, -52.0f, -50.0f, -47.0f};

// One-pole lowpass coefficients for 16kHz: a = 1 - exp(-2*pi*fc/fs).
#define VAD_A500 0.1789f // ~500Hz  (speech fundamental band edge)
#define VAD_A2000 0.5441f // ~2kHz   (formant band edge)

struct VadInst {
  struct VadConfig cfg;
  // Filter + noise state (preserved across frames).
  float lp500;
  float lp2000;
  float dc;
  float n_low;
  float n_mid;
  float n_high;
  int noise_init;
  // Level + machine state.
  float level_dbfs;
  enum VadState st;
  unsigned voiced_run;  // consecutive voiced (onset needs >=2 = 40ms)
  unsigned silence_run; // consecutive silent (hangover debouncer)
  unsigned sess_silence; // silence since last voiced while SPEECH/PAUSE
  unsigned utt_frames;  // frames in current utterance (voiced + hangover)
  unsigned utterances;
  // Onset density window: trailing voiced flags. A lip smack is 1-3 voiced
  // frames; real speech sustains. Requiring ~50% density over 200ms plus a
  // short consecutive run rejects isolated transients while tolerating
  // intra-word gaps.
#define VAD_WIN_MAX 50 // 1000ms @20ms — onset windows clamp to this
  uint8_t win[VAD_WIN_MAX];
  unsigned win_head;  // next write slot
  unsigned win_count; // valid entries (<= win_frames())
  unsigned win_voiced; // voiced flags currently in window
  // Energy-modulation ring: per-frame log mid-band energy for the hum gate.
  // Speech wobbles at syllabic rate (range 6-15dB over 400ms); a hum sits
  // within ~1-2dB. One float/frame; doubles as the 2s watchdog history.
#define VAD_E_MAX 100 // 2000ms @20ms
  float ering[VAD_E_MAX];
  unsigned ering_head;
  unsigned ering_count;
  unsigned sess_frames; // frames since ONSET (watchdog validity)
  // Utterance-scoped tonal portrait: counts voiced frames sitting on a
  // sustained whistle/tone shelf (R~1.2-2.5 with ZCR~0.18-0.35). Human
  // whistles live there for seconds (field: R 1.79-1.87, z 0.247-0.269);
  // speech vowels sweep through it only transiently (field voice R 0.07-0.4,
  // z 0.01-0.11). Reset at each ONSET; judged at session end.
  unsigned u_tonal_cnt;
  unsigned u_cnt;
};

static struct VadInst s_v;

static int clamp_mode(int m) { return m < 0 ? 0 : (m > 3 ? 3 : m); }

// Fresh utterance portrait (tonal-frame counter).
static void portrait_clear(void) {
  s_v.u_tonal_cnt = 0;
  s_v.u_cnt = 0;
}

// Sustained-tone verdict: >=70% of the utterance's voiced frames on the
// whistle shelf with >=8 voiced frames of evidence. Short words may brush
// the shelf for a frame or two; only a real whistle dwells on it.
static int utterance_is_sustained_tone(void) {
  if (s_v.u_cnt < 8) return 0; // too little voiced audio to judge
  return (s_v.u_tonal_cnt * 10 >= s_v.u_cnt * 7);
}

static void cfg_defaults(struct VadConfig *c, int mode) {
  c->mode = clamp_mode(mode);
  c->onset_ms = 200;
  c->hangover_ms = 1500;
  c->session_extend_ms = 2000;
  c->min_speech_ms = 120;
  c->preroll_ms = 750;
  c->abs_floor_dbfs = 0.0f; // 0 = use per-mode table
}

void vad_configure(const struct VadConfig *cfg) {
  if (!cfg) {
    cfg_defaults(&s_v.cfg, VAD_DEFAULT_MODE);
  } else {
    s_v.cfg = *cfg;
    s_v.cfg.mode = clamp_mode(s_v.cfg.mode);
    if (s_v.cfg.onset_ms < 20) s_v.cfg.onset_ms = 20;
    if (s_v.cfg.hangover_ms < 20) s_v.cfg.hangover_ms = 20;
    if (s_v.cfg.session_extend_ms < 0) s_v.cfg.session_extend_ms = 0;
    if (s_v.cfg.min_speech_ms < 0) s_v.cfg.min_speech_ms = 0;
    if (s_v.cfg.preroll_ms < 0) s_v.cfg.preroll_ms = 0;
  }
  vad_reset();
}

void vad_init(int mode) {
  struct VadConfig c;
  cfg_defaults(&c, mode);
  vad_configure(&c);
}

void vad_reset(void) {
  s_v.lp500 = 0;
  s_v.lp2000 = 0;
  s_v.dc = 0;
  s_v.n_low = 1e-9f;
  s_v.n_mid = 1e-9f;
  s_v.n_high = 1e-9f;
  s_v.noise_init = 0;
  s_v.level_dbfs = -90.0f;
  s_v.st = VAD_ST_IDLE;
  s_v.voiced_run = 0;
  s_v.silence_run = 0;
  s_v.sess_silence = 0;
  s_v.utt_frames = 0;
  s_v.utterances = 0;
  memset(s_v.win, 0, sizeof(s_v.win));
  s_v.win_head = 0;
  s_v.win_count = 0;
  s_v.win_voiced = 0;
  memset(s_v.ering, 0, sizeof(s_v.ering));
  s_v.ering_head = 0;
  s_v.ering_count = 0;
  s_v.sess_frames = 0;
  portrait_clear();
}

static float vad_abs_floor(void) {
  if (s_v.cfg.abs_floor_dbfs < -90.0f || s_v.cfg.abs_floor_dbfs > -10.0f)
    return kAbsFloor[s_v.cfg.mode];
  if (s_v.cfg.abs_floor_dbfs == 0.0f) return kAbsFloor[s_v.cfg.mode];
  return s_v.cfg.abs_floor_dbfs;
}

int vad_process_frame(const int16_t *pcm, size_t n) {
  if (!pcm || n == 0) return -1;
  // Accept multiples of 20ms too (e.g. drained 1024B chunks split upstream);
  // score the first 320 samples so one call == one 20ms decision.
  // Callers (opus_encode_task) always pass exactly 320.
  if (n < VAD_FRAME_SAMPLES) return -1;
  n = VAD_FRAME_SAMPLES;

  double e_low = 0, e_mid = 0, e_high = 0, e_tot = 0;
  long zc = 0;
  float prev = 0;
  float peak = 0.0f;
  for (size_t i = 0; i < n; i++) {
    float x = (float)pcm[i] / 32768.0f;
    // DC blocker (running mean, fast enough for mic bias drift).
    s_v.dc += 0.001f * (x - s_v.dc);
    x -= s_v.dc;

    float ax = fabsf(x);
    if (ax > peak)
      peak = ax;
    // Band split: lp500 (fundamental), bp 500-2k (formants), hp 2k+ (fricatives).
    s_v.lp500 += VAD_A500 * (x - s_v.lp500);
    s_v.lp2000 += VAD_A2000 * (x - s_v.lp2000);
    float low = s_v.lp500;
    float mid = s_v.lp2000 - s_v.lp500;
    float high = x - s_v.lp2000;
    e_low += (double)low * low;
    e_mid += (double)mid * mid;
    e_high += (double)high * high;
    e_tot += (double)x * x;
    float s = x >= 0 ? 1.0f : -1.0f;
    if (i && s != prev) zc++;
    prev = s;
  }
  e_low /= n;
  e_mid /= n;
  e_high /= n;
  e_tot /= n;

  float level = e_tot > 1e-12f ? 10.0f * log10f((float)e_tot) : -90.0f;
  s_v.level_dbfs = level;
  float zcr = (float)zc / (float)n;
  // Modulation history: log mid-band energy per frame. Pushed for every
  // analyzed frame (voiced or not) so onset/watchdog see the true contour.
  {
    float edb = 10.0f * log10f((float)(e_mid + 1e-10));
    s_v.ering[s_v.ering_head] = edb;
    s_v.ering_head = (s_v.ering_head + 1) % VAD_E_MAX;
    if (s_v.ering_count < VAD_E_MAX) s_v.ering_count++;
  }

  const float eps = 1e-10f;
  if (!s_v.noise_init) {
    s_v.n_low = (float)e_low + eps;
    s_v.n_mid = (float)e_mid + eps;
    s_v.n_high = (float)e_high + eps;
    s_v.noise_init = 1;
  }

  float snr_low = 10.0f * log10f((float)(e_low + eps) / (s_v.n_low + eps));
  float snr_mid = 10.0f * log10f((float)(e_mid + eps) / (s_v.n_mid + eps));
  float snr_high = 10.0f * log10f((float)(e_high + eps) / (s_v.n_high + eps));
  // Formant band dominates: voiced speech concentrates 500Hz-2kHz.
  // Flat noise spreads evenly and scores lower on this weighting.
  float score = 0.2f * snr_low + 0.5f * snr_mid + 0.3f * snr_high;
  // Formant-dominance ratio up front: R ~3-4 for textbook vowels, ~0.8-0.9
  // for white noise through this filterbank, lower for hiss/fan/thumps.
  // NOTE: real close-mic voices can sit R~0.1-0.4 (low-heavy) and still be
  // speech — R is used for vetoes, never as a voicing requirement, and no
  // low-R penalty is applied (density already rejects isolated thumps).
  float r_dom = (float)(e_mid / (0.5 * (e_low + e_high) + eps));

  int voiced = score > kScoreThresh[s_v.cfg.mode] ? 1 : 0;
  // Absolute floor: never trigger on mic self-noise.
  if (level < vad_abs_floor()) voiced = 0;
  // Formant guard (the core speech-vs-noise discriminator): voiced speech
  // concentrates energy in the 500Hz-2kHz formant band (dominance ratio R =
  // e_mid / mean(e_low, e_high) ~3-4). White noise through this filterbank
  // measures R ~0.8-0.9, hiss/fan lower. Veto broadband-hissy frames even
  // when loud, otherwise any loud noise would pass on raw band-SNR.
  // (Fricatives like /s/ also fail R, but vowels carry the onset and the
  // 1000ms hangover bridges intra-word fricatives.)
  if (zcr > 0.30f && r_dom < 1.5f) voiced = 0;
  // Loud + formant-heavy fast path: don't let a stale noise floor after a
  // gain jump suppress obvious speech.
  if (level > -25.0f && e_mid > 2.0f * (e_low + e_high + eps)) voiced = 1;

  // Sharp impulse/click/tap rejection: a click has a huge peak but little
  // sustained RMS (high crest factor); voice spreads energy across the
  // frame. Apply only while trying to START speech — once speaking, sharp
  // consonants must not veto. Loosened 8 -> 10: loud plosive onsets
  // ("p"/"t"/"k") also spike crest and must still trigger.
  {
    float rms = sqrtf((float)e_tot + 1e-12f);
    float crest = peak / (rms + 1e-6f);
    if (s_v.st == VAD_ST_IDLE && crest > 10.0f) {
      voiced = 0;
    }
  }

  // Utterance portrait: count voiced frames dwelling on the whistle shelf
  // while capturing (resumed-after-pause frames count too — same file).
  if (voiced && (s_v.st == VAD_ST_SPEECH || s_v.st == VAD_ST_PAUSE)) {
    if (r_dom > 1.2f && r_dom < 2.5f && zcr > 0.18f && zcr < 0.35f)
      s_v.u_tonal_cnt++;
    s_v.u_cnt++;
  }

  // Adapt noise floors: track silence quickly, drift slowly during speech
  // so sustained vowels don't drag the floor up and self-cancel.
  float a = voiced ? 0.005f : 0.10f;
  s_v.n_low += a * ((float)e_low + eps - s_v.n_low);
  s_v.n_mid += a * ((float)e_mid + eps - s_v.n_mid);
  s_v.n_high += a * ((float)e_high + eps - s_v.n_high);

  return voiced;
}

static unsigned frames_for(int ms) {
  unsigned f = (unsigned)(ms / VAD_FRAME_MS);
  return f < 1 ? 1 : f;
}

// Onset window: trailing N decisions, fires at ~50% voiced density.
// N derives from onset_ms (clamped 5..50 frames = 100..1000ms).
static unsigned win_frames(void) {
  unsigned f = frames_for(s_v.cfg.onset_ms);
  if (f < 5) f = 5;
  if (f > VAD_WIN_MAX) f = VAD_WIN_MAX;
  return f;
}

static void win_push(int voiced) {
  unsigned wf = win_frames();
  // Advancing ring: new flag lands at head; the entry falling out of the
  // wf-wide window sits wf slots back. (wf only changes via vad_configure,
  // which clears the window, so no shrink/grow mid-stream.)
  if (s_v.win_count >= wf) {
    unsigned drop = (s_v.win_head + VAD_WIN_MAX - wf) % VAD_WIN_MAX;
    s_v.win_voiced -= s_v.win[drop];
  } else {
    s_v.win_count++;
  }
  s_v.win[s_v.win_head] = voiced ? 1 : 0;
  s_v.win_voiced += voiced ? 1 : 0;
  s_v.win_head = (s_v.win_head + 1) % VAD_WIN_MAX;
}

static void win_clear(void) {
  memset(s_v.win, 0, sizeof(s_v.win));
  s_v.win_head = 0;
  s_v.win_count = 0;
  s_v.win_voiced = 0;
}

// Trailing-n range (max-min, dB) of the energy ring. n clamps to valid count.
static float erange_last(unsigned n) {
  if (s_v.ering_count == 0) return 0.0f;
  if (n > s_v.ering_count) n = s_v.ering_count;
  float mn = 1e30f, mx = -1e30f;
  for (unsigned k = 0; k < n; k++) {
    unsigned idx = (s_v.ering_head + VAD_E_MAX - 1 - k) % VAD_E_MAX;
    float e = s_v.ering[idx];
    if (e < mn) mn = e;
    if (e > mx) mx = e;
  }
  return mx - mn;
}

// Trailing-n mean of the energy ring (dB). Same clamping as erange_last.
static float emean_last(unsigned n) {
  if (s_v.ering_count == 0) return -90.0f;
  if (n > s_v.ering_count) n = s_v.ering_count;
  double acc = 0;
  for (unsigned k = 0; k < n; k++) {
    unsigned idx = (s_v.ering_head + VAD_E_MAX - 1 - k) % VAD_E_MAX;
    acc += s_v.ering[idx];
  }
  return (float)(acc / n);
}

// IAC (Important Amplitude Changes): hysteresis-based direction flips of the
// energy contour over the trailing n frames. A 6dB-wide sliding interval
// chases the contour; tiny estimator jitter never flips it, but syllabic
// swings do. Speech flips it several times per 400ms; a hum holds it still
// (0 flips); an isolated click flips it once (up-down) but fails density.
// (Radioengineering 2012 speech/hum segmentation: ~6dB WIDTH is optimal.)
static unsigned iac_flips_last(unsigned n) {
  if (n > s_v.ering_count) n = s_v.ering_count;
  if (n < 2) return 0;
  const float W = 3.0f; // half-width: 6dB interval
  float center = s_v.ering[(s_v.ering_head + VAD_E_MAX - n) % VAD_E_MAX];
  int dir = 0;
  unsigned flips = 0;
  for (unsigned k = 0; k < n; k++) {
    float e = s_v.ering[(s_v.ering_head + VAD_E_MAX - n + k) % VAD_E_MAX];
    int d = dir;
    if (e > center + W) { center = e - W; d = 1; }
    else if (e < center - W) { center = e + W; d = -1; }
    if (d != 0 && dir != 0 && d != dir) flips++;
    if (d != 0) dir = d;
  }
  return flips;
}

// Onset modulation gate: needs a real syllabic swing in the trailing 400ms
// (>=1 IAC flip AND >=3.5dB range). Deliberately light: density over 200ms
// + 2-frame run + crest veto already reject clicks/taps; the gate only has
// to reject flat hums (~1-2dB), and short words ("yes"/"no"/"okay") must
// not be gated out. No cold-start bypass: until the ring holds 12 frames
// there is not enough evidence to start, so gate fails.
static int mod_gate_pass(void) {
  if (s_v.ering_count < 12) return 0;
  return (iac_flips_last(20) >= 1) && (erange_last(20) >= 3.5f);
}

enum VadEvent vad_update(int voiced) {
  unsigned hangover_need = frames_for(s_v.cfg.hangover_ms);
  unsigned session_need = hangover_need;
  if (s_v.cfg.session_extend_ms > 0)
    session_need += frames_for(s_v.cfg.session_extend_ms);
  // Density onset: ~50% voiced over the trailing onset window (5/10 at
  // 200ms). Clicks/pops contribute 1-3 frames and never reach density;
  // sustained speech does. Plus a short consecutive run (2 frames = 40ms)
  // so isolated transients cannot trip density alone. Recall-first:
  // residual false starts are rejected afterward by MIN_SPEECH on true
  // voiced frames, so onset need not be near-perfect.
  unsigned wf = win_frames();
  unsigned onset_need = (wf + 1) / 2;
  if (onset_need < 1) onset_need = 1;

  win_push(voiced);

  if (voiced) {
    s_v.voiced_run++;
    s_v.silence_run = 0;
    s_v.sess_silence = 0;
  } else {
    s_v.silence_run++;
    s_v.voiced_run = 0;
    if (s_v.st != VAD_ST_IDLE) s_v.sess_silence++;
  }
  // Session age (voiced + silent frames alike) — watchdog validity.
  if (s_v.st != VAD_ST_IDLE) s_v.sess_frames++;

  switch (s_v.st) {
    case VAD_ST_IDLE:
      if (s_v.win_count >= wf && s_v.win_voiced >= onset_need &&
          s_v.voiced_run >= 2 && mod_gate_pass()) {
        s_v.st = VAD_ST_SPEECH;
        s_v.utt_frames = s_v.win_voiced;
        s_v.sess_silence = 0;
        s_v.silence_run = 0;
        s_v.sess_frames = 0;
        win_clear(); // fresh window for the *next* onset; session uses hangover
        portrait_clear(); // fresh spectral portrait for this utterance
        return VAD_EV_ONSET;
      }
      return VAD_EV_SILENCE;

    case VAD_ST_SPEECH:
      s_v.utt_frames++;
      // Flatness watchdog: 2s of voiced-but-spectrally-frozen audio is a hum
      // or tune, not speech — vowels/consonants always break 2.5dB over 2s.
      // Returns DISCARD (purge + drop file), not OFFSET (which would keep it).
      if (s_v.sess_frames >= VAD_E_MAX && erange_last(VAD_E_MAX) < 2.5f) {
        s_v.st = VAD_ST_IDLE;
        s_v.utt_frames = 0;
        s_v.sess_silence = 0;
        s_v.silence_run = 0;
        s_v.sess_frames = 0;
        win_clear();
        return VAD_EV_DISCARD;
      }
      if (!voiced && s_v.silence_run >= hangover_need) {
        if (session_need <= hangover_need) {
          // No session extension configured: close immediately.
          s_v.st = VAD_ST_IDLE;
          s_v.utterances++;
          s_v.utt_frames = 0;
          return VAD_EV_OFFSET;
        }
        s_v.st = VAD_ST_PAUSE;
        return VAD_EV_PAUSE;
      }
      return VAD_EV_SPEECH;

    case VAD_ST_PAUSE:
      if (voiced) {
        s_v.st = VAD_ST_SPEECH;
        s_v.utt_frames++;
        s_v.silence_run = 0;
        return VAD_EV_SPEECH; // resumed same utterance/file
      }
      if (s_v.sess_silence >= session_need) {
        // Session end, two discard paths:
        // (a) trailing 2s flat AND loud = adapted-out hum still droning;
        // (b) sustained whistle shelf = utterance dwelled on the tone band
        //     (field whistles sit R~1.8/z~0.26 for seconds; speech only
        //     brushes it transiently). True pauses are quiet -> OFFSET keeps.
        int flat_tail = (s_v.sess_frames >= VAD_E_MAX &&
                         erange_last(VAD_E_MAX) < 2.5f &&
                         emean_last(VAD_E_MAX) > -38.0f);
        if (flat_tail || utterance_is_sustained_tone()) {
          s_v.st = VAD_ST_IDLE;
          s_v.utt_frames = 0;
          s_v.sess_silence = 0;
          s_v.silence_run = 0;
          s_v.sess_frames = 0;
          win_clear();
          return VAD_EV_DISCARD;
        }
        s_v.st = VAD_ST_IDLE;
        s_v.utterances++;
        s_v.utt_frames = 0;
        s_v.sess_silence = 0;
        s_v.silence_run = 0;
        return VAD_EV_OFFSET;
      }
      return VAD_EV_SILENCE; // file open but writes suspended
  }
  return VAD_EV_SILENCE;
}

enum VadState vad_state(void) { return s_v.st; }
int vad_should_record(void) { return s_v.st != VAD_ST_IDLE; }
int vad_is_speech(void) { return s_v.st == VAD_ST_SPEECH; }
float vad_level_dbfs(void) { return s_v.level_dbfs; }
float vad_mod_db(void) { return erange_last(20); }
unsigned vad_utterances(void) { return s_v.utterances; }
unsigned vad_utterance_frames(void) { return s_v.utt_frames; }
int vad_in_session(void) { return s_v.st != VAD_ST_IDLE; }
