import { describe, expect, it } from 'vitest';

import {
  FQBN_COMPILE,
  UPLOAD_SPEED,
  buildCompile,
  buildUpload,
  toUploadFqbn,
} from '../src/arduino';

describe('fqbn', () => {
  it('compiles with the fast upload speed', () => {
    expect(FQBN_COMPILE).toContain('UploadSpeed=921600');
  });

  it('slows the upload speed for flashing', () => {
    expect(toUploadFqbn(FQBN_COMPILE)).toContain(`UploadSpeed=${UPLOAD_SPEED}`);
  });
});

describe('command builders', () => {
  it('builds a compile command', () => {
    expect(
      buildCompile('arduino-cli', FQBN_COMPILE, 'firmware/checkpoint', 'firmware/checkpoint/build'),
    ).toEqual([
      'arduino-cli',
      'compile',
      '--fqbn',
      FQBN_COMPILE,
      'firmware/checkpoint',
      '--output-dir',
      'firmware/checkpoint/build',
    ]);
  });

  it('builds an upload command', () => {
    expect(buildUpload('arduino-cli', FQBN_COMPILE, 'COM3', 'out')).toEqual([
      'arduino-cli',
      'upload',
      '-p',
      'COM3',
      '--fqbn',
      FQBN_COMPILE,
      '--input-dir',
      'out',
    ]);
  });
});
