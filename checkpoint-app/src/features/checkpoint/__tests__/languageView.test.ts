import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { filterLanguages, sameSelection, selectedFirst } from '../languageView.ts';

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

describe('selectedFirst', () => {
  it('puts selected languages first and keeps catalog order within groups', () => {
    assert.deepEqual(
      selectedFirst(CATALOG, ['zho']).map((language) => language.code),
      ['zho', 'eng', 'hin'],
    );
  });

  it('keeps the catalog order when nothing is selected', () => {
    assert.deepEqual(
      selectedFirst(CATALOG, []).map((language) => language.code),
      ['eng', 'hin', 'zho'],
    );
  });

  it('orders multiple selected languages by catalog order', () => {
    assert.deepEqual(
      selectedFirst(CATALOG, ['zho', 'hin']).map((language) => language.code),
      ['hin', 'zho', 'eng'],
    );
  });
});

describe('sameSelection', () => {
  it('ignores order', () => {
    assert.equal(sameSelection(['eng', 'hin'], ['hin', 'eng']), true);
  });

  it('detects different length or content', () => {
    assert.equal(sameSelection(['eng'], ['eng', 'hin']), false);
    assert.equal(sameSelection(['eng'], ['hin']), false);
  });

  it('treats two empty selections as equal', () => {
    assert.equal(sameSelection([], []), true);
  });
});
