export type StreamChannel = 'tasks' | 'serial';

export interface StreamEvent {
  type: string;
  [key: string]: unknown;
}

type Listener = (event: StreamEvent) => void;

export class EventHub {
  private readonly listeners = new Map<StreamChannel, Set<Listener>>();

  subscribe(channel: StreamChannel, listener: Listener): () => void {
    const listeners = this.listeners.get(channel) ?? new Set<Listener>();
    listeners.add(listener);
    this.listeners.set(channel, listeners);
    return () => listeners.delete(listener);
  }

  publish(channel: StreamChannel, event: StreamEvent): void {
    for (const listener of this.listeners.get(channel) ?? []) {
      listener(event);
    }
  }
}
