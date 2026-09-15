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

/** Puts selected languages first, preserving catalog order within each group. */
export function selectedFirst(languages: LanguageOption[], selected: string[]): LanguageOption[] {
  const chosen = new Set(selected);
  const picked: LanguageOption[] = [];
  const rest: LanguageOption[] = [];
  for (const language of languages) {
    (chosen.has(language.code) ? picked : rest).push(language);
  }
  return [...picked, ...rest];
}

/** Order-insensitive comparison, so re-adding a language is not a change. */
export function sameSelection(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(b);
  return a.every((code) => set.has(code));
}
