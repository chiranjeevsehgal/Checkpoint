import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import { deviceAdminInvocation } from '../deviceAdmin';
import { badRequest, unavailable } from '../httpError';
import { runCapture } from '../process';
import { isUuid } from '../validation';

export function registerUserRoutes(app: FastifyInstance, context: AppContext): void {
  const { config, kratos } = context;

  const requireIdentityId = (value: string): string => {
    if (!isUuid(value)) throw badRequest('invalid identity id');
    return value;
  };

  app.get('/api/users', async () => ({ users: await kratos.listIdentities() }));

  app.get('/api/users/:id/sessions', async (request) => {
    const { id } = request.params as { id: string };
    return { sessions: await kratos.listSessions(requireIdentityId(id)) };
  });

  app.post('/api/users/:id/sessions/revoke', async (request) => {
    const { id } = request.params as { id: string };
    return { revoked: await kratos.revokeSessions(requireIdentityId(id)) };
  });

  // Queues an account-deletion tombstone; the ingestion worker performs the
  // purge, device quarantine and identity removal asynchronously.
  app.post('/api/users/:id/delete', async (request) => {
    const { id } = request.params as { id: string };
    const invocation = deviceAdminInvocation(config, ['delete-account', '-user', requireIdentityId(id)]);
    const result = await runCapture(invocation.command, invocation.options);
    if (result.code !== 0) {
      throw unavailable(result.stderr.trim() || `device-admin exited ${result.code}`);
    }
    return { output: result.stdout.trim() };
  });
}
