import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import { badRequest } from '../httpError';

interface OpenBody {
  path?: string;
  baudRate?: number;
}

interface SendBody {
  command?: string;
}

export function registerSerialRoutes(app: FastifyInstance, context: AppContext): void {
  app.get('/api/serial', () => ({ open: context.serial.isOpen }));

  app.get('/api/serial/ports', async () => ({ ports: await context.serial.listPorts() }));

  app.post('/api/serial/open', async (request) => {
    const body = (request.body ?? {}) as OpenBody;
    const path = (body.path ?? '').trim();
    if (!path) throw badRequest('path is required');
    await context.serial.open(path, body.baudRate);
    return { open: true, path };
  });

  app.post('/api/serial/close', async () => {
    await context.serial.close();
    return { open: false };
  });

  app.post('/api/serial/send', async (request) => {
    const body = (request.body ?? {}) as SendBody;
    const command = (body.command ?? '').trim();
    if (!command) throw badRequest('command is required');
    await context.serial.send(command);
    return { sent: command };
  });
}
