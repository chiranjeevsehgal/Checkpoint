export const COMPILE_UPLOAD_SPEED = 921600;
export const UPLOAD_SPEED = 512000;

export const FQBN_BASE =
  'esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,' +
  'PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio';

export const FQBN_COMPILE = `${FQBN_BASE},UploadSpeed=${COMPILE_UPLOAD_SPEED}`;

// Flashing runs slower than compiling for a reliable upload.
export function toUploadFqbn(fqbn: string): string {
  return fqbn.replace(/UploadSpeed=\d+/, `UploadSpeed=${UPLOAD_SPEED}`);
}

export function buildCompile(
  arduinoCli: string,
  fqbn: string,
  sketchDir: string,
  outputDir: string,
): string[] {
  return [arduinoCli, 'compile', '--fqbn', fqbn, sketchDir, '--output-dir', outputDir];
}

export function buildUpload(
  arduinoCli: string,
  fqbn: string,
  port: string,
  inputDir: string,
): string[] {
  return [arduinoCli, 'upload', '-p', port, '--fqbn', fqbn, '--input-dir', inputDir];
}
