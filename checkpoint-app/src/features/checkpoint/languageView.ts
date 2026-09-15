import type { LanguageOption } from '@/lib/api/settings-api';

/** Filters the catalog by name or code; an empty query keeps everything. */
export function filterLanguages(languages: LanguageOption[], query: string): LanguageOption[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return languages;
  return languages.filter(
    (language) =>
      language.name.toLowerCase().includes(needle) || language.code.toLowerCase().includes(needle),
  );
}
