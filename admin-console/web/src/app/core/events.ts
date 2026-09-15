import { Injectable, signal } from '@angular/core';

import type { SerialEvent, TaskEvent } from './models';

const MAX_TASK_EVENTS = 500;
const MAX_SERIAL_EVENTS = 1000;

export function isTaskBusy(events: readonly TaskEvent[]): boolean {
  const last = events.filter((event) => ['start', 'exit', 'failed'].includes(event.type)).at(-1);
  return last?.type === 'start';
}

@Injectable({ providedIn: 'root' })
export class EventsService {
  readonly taskEvents = signal<TaskEvent[]>([]);
  readonly serialEvents = signal<SerialEvent[]>([]);

  private taskSource?: EventSource;
  private serialSource?: EventSource;

  connectTasks(): void {
    if (this.taskSource || typeof EventSource === 'undefined') return;
    this.taskSource = new EventSource('/events?channel=tasks');
    this.taskSource.onmessage = (message: MessageEvent<string>) => {
      const event = JSON.parse(message.data) as TaskEvent;
      this.taskEvents.update((events) => appendBounded(events, event, MAX_TASK_EVENTS));
    };
  }

  connectSerial(): void {
    if (this.serialSource || typeof EventSource === 'undefined') return;
    this.serialSource = new EventSource('/events?channel=serial');
    this.serialSource.onmessage = (message: MessageEvent<string>) => {
      const event = JSON.parse(message.data) as SerialEvent;
      this.serialEvents.update((events) => appendBounded(events, event, MAX_SERIAL_EVENTS));
    };
  }

  disconnectSerial(): void {
    this.serialSource?.close();
    this.serialSource = undefined;
  }

  clearTasks(): void {
    this.taskEvents.set([]);
  }

  clearSerial(): void {
    this.serialEvents.set([]);
  }
}

function appendBounded<T>(events: T[], event: T, max: number): T[] {
  const next = [...events, event];
  return next.length > max ? next.slice(next.length - max) : next;
}
