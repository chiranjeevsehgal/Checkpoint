import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import { composeServices, probe } from '../docker';

export function registerInfraRoutes(app: FastifyInstance, context: AppContext): void {
  const { config } = context;

  app.get('/api/infra', async () => {
    const services = await composeServices(config).catch((error: unknown) => ({
      error: error instanceof Error ? error.message : String(error),
    }));
    const [ingestion, kratos] = await Promise.all([
      probe(`${config.ingestionUrl}/health/ready`),
      probe(`${config.kratosPublicUrl}/health/ready`),
    ]);
    return { services, health: { ingestion, kratos } };
  });
}
