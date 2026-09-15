import { describe, expect, it } from 'vitest';

import { parseComposeOutput } from '../src/docker';

describe('parseComposeOutput', () => {
  it('parses newline-delimited JSON objects', () => {
    const output = '{"Service":"postgres","State":"running"}\n{"Service":"kafka","State":"running"}\n';
    expect(parseComposeOutput(output)).toEqual([
      { Service: 'postgres', State: 'running' },
      { Service: 'kafka', State: 'running' },
    ]);
  });

  it('parses a JSON array and empty output', () => {
    expect(parseComposeOutput('[{"Service":"minio"}]')).toEqual([{ Service: 'minio' }]);
    expect(parseComposeOutput('   ')).toEqual([]);
  });
});
