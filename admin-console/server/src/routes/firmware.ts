import type { FastifyInstance } from 'fastify';

import { buildCompile, buildUpload, toUploadFqbn } from '../arduino';
import type { AppContext } from '../context';
import { badRequest } from '../httpError';

interface UploadBody {
  port?: string;
}

export function registerFirmwareRoutes(app: FastifyInstance, context: AppContext): void {
  const { config, tasks, serial } = context;

  const compileCommand = (): string[] =>
    buildCompile(config.arduinoCli, config.fqbn, config.sketchDir, config.buildDir);

  const uploadCommand = (port: string): string[] =>
    buildUpload(config.arduinoCli, toUploadFqbn(config.fqbn), port, config.buildDir);

  app.post('/api/firmware/compile', async () => {
    await serial.close();
    tasks.start('compile', [compileCommand()]);
    return { started: true };
  });

  app.post('/api/firmware/upload', async (request) => {
    const body = (request.body ?? {}) as UploadBody;
    const port = (body.port ?? '').trim();
    if (!port) throw badRequest('port is required');
    await serial.close();
    tasks.start('upload', [uploadCommand(port)]);
    return { started: true };
  });

  app.post('/api/firmware/compile-upload', async (request) => {
    const body = (request.body ?? {}) as UploadBody;
    const port = (body.port ?? '').trim();
    if (!port) throw badRequest('port is required');
    await serial.close();
    tasks.start('compile-upload', [compileCommand(), uploadCommand(port)]);
    return { started: true };
  });
}
