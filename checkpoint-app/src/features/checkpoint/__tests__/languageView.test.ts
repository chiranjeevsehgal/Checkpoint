import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { filterLanguages } from '../languageView.ts';

const CATALOG = [
  { code: 'eng', name: 'English' },
  { code: 'hin', name: 'Hindi' },
  { code: 'zho', name: 'Mandarin Chinese' },
];

describe('filterLanguages', () => {
  it('returns everything for an empty query', () => {
    assert.equal(filterLanguages(CATALOG, '   ').length, 3);
  });

  it('matches by name case-insensitively', () => {
    assert.deepEqual(
      filterLanguages(CATALOG, 'MAN').map((language) => language.code),
      ['zho'],
    );
  });

  it('matches by code', () => {
    assert.deepEqual(
      filterLanguages(CATALOG, 'ZHO').map((language) => language.code),
      ['zho'],
    );
  });

  it('returns nothing when no language matches', () => {
    assert.equal(filterLanguages(CATALOG, 'zzz').length, 0);
  });
});
