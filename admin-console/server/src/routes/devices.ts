import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import { deviceAdminInvocation, parseProvision } from '../deviceAdmin';
import { badRequest, conflict, unprocessable, unavailable } from '../httpError';
import { delay, runCapture } from '../process';
import { isClaimHash, isDeviceId } from '../validation';

const READ_IDS_DELAY_MS = 1500;

interface DeviceBody {
  deviceId?: string;
}

interface ProvisionBody extends DeviceBody {
  claimHash?: string;
}

export function registerDeviceRoutes(app: FastifyInstance, context: AppContext): void {
  const { config, serial, events } = context;

  const run = async (args: string[]): Promise<string> => {
    const invocation = deviceAdminInvocation(config, args);
    const result = await runCapture(invocation.command, invocation.options);
    if (result.code !== 0) {
      throw unavailable(result.stderr.trim() || `device-admin exited ${result.code}`);
    }
    return result.stdout.trim();
  };

  const requireDeviceId = (value: string | undefined): string => {
    const deviceId = (value ?? '').trim();
    if (!isDeviceId(deviceId)) throw badRequest('deviceId must be 32 lowercase hex characters');
    return deviceId;
  };

  app.get('/api/devices', async () => {
    const output = await run(['list', '-json']);
    return { devices: JSON.parse(output) as unknown[] };
  });

  app.get('/api/deletions', async () => {
    const output = await run(['deletions', '-json']);
    return { deletions: JSON.parse(output) as unknown[] };
  });

  app.post('/api/devices/read-ids', async () => {
    if (!serial.isOpen) throw conflict('Open the serial port first');
    const lines: string[] = [];
    const unsubscribe = events.subscribe('serial', (event) => {
      if (event.type === 'line' && typeof event.line === 'string') lines.push(event.line);
    });
    try {
      await serial.send('auth provision');
      await delay(READ_IDS_DELAY_MS);
    } finally {
      unsubscribe();
    }
    const parsed = parseProvision(lines.join('\n'));
    if (!parsed) throw unprocessable('No device/cloud-sha256 in the pendant response');
    return parsed;
  });

  app.post('/api/devices/provision', async (request) => {
    const body = (request.body ?? {}) as ProvisionBody;
    const deviceId = requireDeviceId(body.deviceId);
    const claimHash = (body.claimHash ?? '').trim();
    if (!isClaimHash(claimHash)) throw badRequest('claimHash must be 64 lowercase hex characters');
    return { output: await run(['provision', '-device', deviceId, '-claim-hash', claimHash]) };
  });

  app.post('/api/devices/status', async (request) => {
    const body = (request.body ?? {}) as DeviceBody;
    return { output: await run(['status', '-device', requireDeviceId(body.deviceId)]) };
  });

  app.post('/api/devices/unquarantine', async (request) => {
    const body = (request.body ?? {}) as DeviceBody;
    return { output: await run(['unquarantine', '-device', requireDeviceId(body.deviceId)]) };
  });
}
