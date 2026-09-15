import type { FastifyInstance } from 'fastify';

import { redactDatabaseUrl } from '../config';
import type { AppContext } from '../context';

export function registerConfigRoutes(app: FastifyInstance, context: AppContext): void {
  const { config } = context;

  app.get('/api/health', () => ({ status: 'ok' }));

  app.get('/api/config', () => ({
    host: config.host,
    port: config.port,
    repoRoot: config.repoRoot,
    databaseUrl: redactDatabaseUrl(config.databaseUrl),
    kratosAdminUrl: config.kratosAdminUrl,
    kratosPublicUrl: config.kratosPublicUrl,
    ingestionUrl: config.ingestionUrl,
    arduinoCli: config.arduinoCli,
    sketchDir: config.sketchDir,
    buildDir: config.buildDir,
    fqbn: config.fqbn,
    dockerHost: config.dockerHost,
  }));
}
