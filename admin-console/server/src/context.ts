import type { AppConfig } from './config';
import { EventHub } from './events';
import { KratosAdminClient } from './kratos';
import { SerialManager } from './serial';
import { TaskRunner } from './task';

export interface AppContext {
  config: AppConfig;
  events: EventHub;
  tasks: TaskRunner;
  serial: SerialManager;
  kratos: KratosAdminClient;
}

export function createContext(config: AppConfig): AppContext {
  const events = new EventHub();
  return {
    config,
    events,
    tasks: new TaskRunner(events),
    serial: new SerialManager(events),
    kratos: new KratosAdminClient(config.kratosAdminUrl),
  };
}
