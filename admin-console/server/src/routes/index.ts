import type { FastifyInstance } from 'fastify';

import type { AppContext } from '../context';
import { registerConfigRoutes } from './config';
import { registerDeviceRoutes } from './devices';
import { registerEventRoutes } from './events';
import { registerFirmwareRoutes } from './firmware';
import { registerInfraRoutes } from './infra';
import { registerSerialRoutes } from './serial';
import { registerSettingsRoutes } from './settings';
import { registerUserRoutes } from './users';

export function registerRoutes(app: FastifyInstance, context: AppContext): void {
  registerConfigRoutes(app, context);
  registerSerialRoutes(app, context);
  registerFirmwareRoutes(app, context);
  registerDeviceRoutes(app, context);
  registerUserRoutes(app, context);
  registerInfraRoutes(app, context);
  registerSettingsRoutes(app, context);
  registerEventRoutes(app, context);
}
