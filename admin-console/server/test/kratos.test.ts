import { describe, expect, it } from 'vitest';

import { nextPageToken } from '../src/kratos';

describe('nextPageToken', () => {
  it('extracts the token from a rel="next" link', () => {
    expect(
      nextPageToken('<http://kratos:4434/admin/identities?page_size=250&page_token=abc>; rel="next"'),
    ).toBe('abc');
  });

  it('returns empty when there is no next link', () => {
    expect(nextPageToken('<http://kratos/admin/identities?page_token=abc>; rel="prev"')).toBe('');
    expect(nextPageToken(null)).toBe('');
  });
});
