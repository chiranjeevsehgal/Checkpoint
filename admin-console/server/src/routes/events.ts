import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import type { StreamChannel } from '../events';

const KEEP_ALIVE_MS = 15000;

export function registerEventRoutes(app: FastifyInstance, context: AppContext): void {
  app.get('/events', (request, reply) => {
    const channel = ((request.query as { channel?: string }).channel ?? 'tasks') as StreamChannel;
    reply.raw.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache, no-transform',
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no',
    });
    reply.raw.write(': connected\n\n');

    const unsubscribe = context.events.subscribe(channel, (event) => {
      reply.raw.write(`data: ${JSON.stringify(event)}\n\n`);
    });
    const keepAlive = setInterval(() => reply.raw.write(': keep-alive\n\n'), KEEP_ALIVE_MS);

    request.raw.on('close', () => {
      clearInterval(keepAlive);
      unsubscribe();
    });
  });
}
