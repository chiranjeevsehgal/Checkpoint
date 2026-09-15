package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidLanguage means a code is empty or absent from SupportedLanguages.
var ErrInvalidLanguage = errors.New("unsupported language code")

// Language is one STT language the providers accept, keyed by ISO-639-3.
type Language struct {
	Code string
	Name string
}

// SupportedLanguages is the ElevenLabs Scribe v2 language catalog. It is the
// single source of truth for what a user may select.
var SupportedLanguages = []Language{
	{Code: "afr", Name: "Afrikaans"},
	{Code: "amh", Name: "Amharic"},
	{Code: "ara", Name: "Arabic"},
	{Code: "hye", Name: "Armenian"},
	{Code: "asm", Name: "Assamese"},
	{Code: "ast", Name: "Asturian"},
	{Code: "aze", Name: "Azerbaijani"},
	{Code: "bel", Name: "Belarusian"},
	{Code: "ben", Name: "Bengali"},
	{Code: "bos", Name: "Bosnian"},
	{Code: "bul", Name: "Bulgarian"},
	{Code: "mya", Name: "Burmese"},
	{Code: "yue", Name: "Cantonese"},
	{Code: "cat", Name: "Catalan"},
	{Code: "ceb", Name: "Cebuano"},
	{Code: "nya", Name: "Chichewa"},
	{Code: "hrv", Name: "Croatian"},
	{Code: "ces", Name: "Czech"},
	{Code: "dan", Name: "Danish"},
	{Code: "nld", Name: "Dutch"},
	{Code: "eng", Name: "English"},
	{Code: "est", Name: "Estonian"},
	{Code: "fil", Name: "Filipino"},
	{Code: "fin", Name: "Finnish"},
	{Code: "fra", Name: "French"},
	{Code: "ful", Name: "Fulah"},
	{Code: "glg", Name: "Galician"},
	{Code: "lug", Name: "Ganda"},
	{Code: "kat", Name: "Georgian"},
	{Code: "deu", Name: "German"},
	{Code: "ell", Name: "Greek"},
	{Code: "guj", Name: "Gujarati"},
	{Code: "hau", Name: "Hausa"},
	{Code: "heb", Name: "Hebrew"},
	{Code: "hin", Name: "Hindi"},
	{Code: "hun", Name: "Hungarian"},
	{Code: "isl", Name: "Icelandic"},
	{Code: "ibo", Name: "Igbo"},
	{Code: "ind", Name: "Indonesian"},
	{Code: "gle", Name: "Irish"},
	{Code: "ita", Name: "Italian"},
	{Code: "jpn", Name: "Japanese"},
	{Code: "jav", Name: "Javanese"},
	{Code: "kea", Name: "Kabuverdianu"},
	{Code: "kan", Name: "Kannada"},
	{Code: "kaz", Name: "Kazakh"},
	{Code: "khm", Name: "Khmer"},
	{Code: "kor", Name: "Korean"},
	{Code: "kur", Name: "Kurdish"},
	{Code: "kir", Name: "Kyrgyz"},
	{Code: "lao", Name: "Lao"},
	{Code: "lav", Name: "Latvian"},
	{Code: "lin", Name: "Lingala"},
	{Code: "lit", Name: "Lithuanian"},
	{Code: "luo", Name: "Luo"},
	{Code: "ltz", Name: "Luxembourgish"},
	{Code: "mkd", Name: "Macedonian"},
	{Code: "msa", Name: "Malay"},
	{Code: "mal", Name: "Malayalam"},
	{Code: "mlt", Name: "Maltese"},
	{Code: "zho", Name: "Mandarin Chinese"},
	{Code: "mri", Name: "Māori"},
	{Code: "mar", Name: "Marathi"},
	{Code: "mon", Name: "Mongolian"},
	{Code: "nep", Name: "Nepali"},
	{Code: "nso", Name: "Northern Sotho"},
	{Code: "nor", Name: "Norwegian"},
	{Code: "oci", Name: "Occitan"},
	{Code: "ori", Name: "Odia"},
	{Code: "pus", Name: "Pashto"},
	{Code: "fas", Name: "Persian"},
	{Code: "pol", Name: "Polish"},
	{Code: "por", Name: "Portuguese"},
	{Code: "pan", Name: "Punjabi"},
	{Code: "ron", Name: "Romanian"},
	{Code: "rus", Name: "Russian"},
	{Code: "srp", Name: "Serbian"},
	{Code: "sna", Name: "Shona"},
	{Code: "snd", Name: "Sindhi"},
	{Code: "sin", Name: "Sinhala"},
	{Code: "slk", Name: "Slovak"},
	{Code: "slv", Name: "Slovenian"},
	{Code: "som", Name: "Somali"},
	{Code: "spa", Name: "Spanish"},
	{Code: "swa", Name: "Swahili"},
	{Code: "swe", Name: "Swedish"},
	{Code: "tgk", Name: "Tajik"},
	{Code: "tam", Name: "Tamil"},
	{Code: "tel", Name: "Telugu"},
	{Code: "tha", Name: "Thai"},
	{Code: "tur", Name: "Turkish"},
	{Code: "ukr", Name: "Ukrainian"},
	{Code: "umb", Name: "Umbundu"},
	{Code: "urd", Name: "Urdu"},
	{Code: "uzb", Name: "Uzbek"},
	{Code: "vie", Name: "Vietnamese"},
	{Code: "cym", Name: "Welsh"},
	{Code: "wol", Name: "Wolof"},
	{Code: "xho", Name: "Xhosa"},
	{Code: "zul", Name: "Zulu"},
}

var supportedLanguageCodes = func() map[string]struct{} {
	codes := make(map[string]struct{}, len(SupportedLanguages))
	for _, language := range SupportedLanguages {
		codes[language.Code] = struct{}{}
	}
	return codes
}()

// ValidateLanguages lowercases, de-duplicates and validates selected codes,
// preserving their order. An empty selection is valid (it means no filtering).
func ValidateLanguages(codes []string) ([]string, error) {
	normalized := make([]string, 0, len(codes))
	seen := make(map[string]struct{}, len(codes))
	for _, raw := range codes {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			return nil, ErrInvalidLanguage
		}
		if _, ok := supportedLanguageCodes[code]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidLanguage, raw)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, code)
	}
	return normalized, nil
}
