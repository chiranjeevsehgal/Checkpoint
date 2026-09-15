// Package language normalizes provider language codes to the canonical
// ISO-639-3 codes the ingestion catalog stores.
package language

import "strings"

// pair links a canonical ISO-639-3 code with the ISO-639-1 code providers
// use. Languages without a 2-letter code are absent.
type pair struct {
	iso3 string
	iso1 string
}

var pairs = []pair{
	{"afr", "af"}, {"amh", "am"}, {"ara", "ar"}, {"hye", "hy"}, {"asm", "as"},
	{"aze", "az"}, {"bel", "be"}, {"ben", "bn"}, {"bos", "bs"}, {"bul", "bg"},
	{"mya", "my"}, {"cat", "ca"}, {"nya", "ny"}, {"hrv", "hr"}, {"ces", "cs"},
	{"dan", "da"}, {"nld", "nl"}, {"eng", "en"}, {"est", "et"}, {"fil", "tl"},
	{"fin", "fi"}, {"fra", "fr"}, {"ful", "ff"}, {"glg", "gl"}, {"lug", "lg"},
	{"kat", "ka"}, {"deu", "de"}, {"ell", "el"}, {"guj", "gu"}, {"hau", "ha"},
	{"heb", "he"}, {"hin", "hi"}, {"hun", "hu"}, {"isl", "is"}, {"ibo", "ig"},
	{"ind", "id"}, {"gle", "ga"}, {"ita", "it"}, {"jpn", "ja"}, {"jav", "jv"},
	{"kan", "kn"}, {"kaz", "kk"}, {"khm", "km"}, {"kor", "ko"}, {"kur", "ku"},
	{"kir", "ky"}, {"lao", "lo"}, {"lav", "lv"}, {"lin", "ln"}, {"lit", "lt"},
	{"ltz", "lb"}, {"mkd", "mk"}, {"msa", "ms"}, {"mal", "ml"}, {"mlt", "mt"},
	{"zho", "zh"}, {"mri", "mi"}, {"mar", "mr"}, {"mon", "mn"}, {"nep", "ne"},
	{"nor", "no"}, {"oci", "oc"}, {"ori", "or"}, {"pus", "ps"}, {"fas", "fa"},
	{"pol", "pl"}, {"por", "pt"}, {"pan", "pa"}, {"ron", "ro"}, {"rus", "ru"},
	{"srp", "sr"}, {"sna", "sn"}, {"snd", "sd"}, {"sin", "si"}, {"slk", "sk"},
	{"slv", "sl"}, {"som", "so"}, {"spa", "es"}, {"swa", "sw"}, {"swe", "sv"},
	{"tgk", "tg"}, {"tam", "ta"}, {"tel", "te"}, {"tha", "th"}, {"tur", "tr"},
	{"ukr", "uk"}, {"urd", "ur"}, {"uzb", "uz"}, {"vie", "vi"}, {"cym", "cy"},
	{"wol", "wo"}, {"xho", "xh"}, {"zul", "zu"},
}

var (
	iso1ToIso3 = make(map[string]string, len(pairs))
	iso3ToIso1 = make(map[string]string, len(pairs))
)

func init() {
	for _, p := range pairs {
		iso1ToIso3[p.iso1] = p.iso3
		iso3ToIso1[p.iso3] = p.iso1
	}
}

// Normalize maps a provider code to a canonical ISO-639-3 code: it lowercases,
// drops any region/script suffix and upgrades a known ISO-639-1 code. Unknown
// 3-letter codes pass through; unknown 2-letter codes become "".
func Normalize(code string) string {
	trimmed := strings.ToLower(strings.TrimSpace(code))
	if base, _, found := strings.Cut(trimmed, "-"); found {
		trimmed = base
	}
	if len(trimmed) == 2 {
		return iso1ToIso3[trimmed]
	}
	return trimmed
}

// ISO1 returns the ISO-639-1 code providers expect for a canonical code, or
// "" when the language has none.
func ISO1(canonical string) string {
	return iso3ToIso1[strings.ToLower(strings.TrimSpace(canonical))]
}

// Allowed reports whether a detected code is in the selected set. An empty
// selection disables filtering.
func Allowed(detected string, selected []string) bool {
	if len(selected) == 0 {
		return true
	}
	normalized := Normalize(detected)
	if normalized == "" {
		return false
	}
	for _, code := range selected {
		if strings.EqualFold(strings.TrimSpace(code), normalized) {
			return true
		}
	}
	return false
}

// OmitReason explains why a transcript should be dropped, or "" to keep it.
func OmitReason(text, detected string, selected []string) string {
	if strings.TrimSpace(text) == "" {
		return "empty transcript"
	}
	if !Allowed(detected, selected) {
		return "language " + detected + " not in selection"
	}
	return ""
}
