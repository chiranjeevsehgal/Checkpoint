import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

import fastifyStatic from '@fastify/static';
import Fastify from 'fastify';

import { loadConfig } from './config';
import { createContext } from './context';
import { HttpError } from './httpError';
import { registerRoutes } from './routes';
import { TaskBusyError } from './task';

const webDist = resolve(__dirname, '..', '..', 'web', 'dist', 'web', 'browser');

async function main(): Promise<void> {
  const config = loadConfig();
  const context = createContext(config);
  const app = Fastify({ logger: false });

  app.setErrorHandler((error, _request, reply) => {
    if (error instanceof HttpError) {
      return reply.code(error.status).send({ error: error.message });
    }
    if (error instanceof TaskBusyError) {
      return reply.code(409).send({ error: error.message });
    }
    const message = error instanceof Error ? error.message : 'unexpected error';
    return reply.code(500).send({ error: message });
  });

  registerRoutes(app, context);

  if (existsSync(webDist)) {
    await app.register(fastifyStatic, { root: webDist, prefix: '/', index: ['index.html'] });
  }

  app.setNotFoundHandler((request, reply) => {
    const url = request.raw.url ?? '';
    if (url.startsWith('/api') || url.startsWith('/events')) {
      return reply.code(404).send({ error: 'not found' });
    }
    if (existsSync(webDist)) {
      return reply.sendFile('index.html');
    }
    return reply
      .code(200)
      .type('text/html')
      .send(
        '<h1>Admin console not built</h1><p>Run <code>npm run build</code>, or <code>npm run dev</code> and open the Angular dev server on :4200.</p>',
      );
  });

  await app.listen({ host: config.host, port: config.port });
  console.log(`admin-console agent listening on http://${config.host}:${config.port}`);
}

void main().catch((error: unknown) => {
  console.error(error);
  process.exit(1);
});
