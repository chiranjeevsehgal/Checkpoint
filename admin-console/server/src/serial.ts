import type { EventHub } from './events';

export const SERIAL_BAUD = 115200;

type SerialModule = typeof import('serialport');
type SerialPortInstance = InstanceType<SerialModule['SerialPort']>;

let modulePromise: Promise<SerialModule | null> | null = null;

async function loadSerialModule(): Promise<SerialModule | null> {
  if (!modulePromise) {
    modulePromise = import('serialport').catch(() => null);
  }
  return modulePromise;
}

export class SerialManager {
  private port: SerialPortInstance | null = null;
  private buffer = '';

  constructor(private readonly events: EventHub) {}

  get isOpen(): boolean {
    return this.port?.isOpen ?? false;
  }

  async listPorts(): Promise<string[]> {
    const module = await loadSerialModule();
    if (!module) return [];
    const ports = await module.SerialPort.list();
    return ports.map((port) => port.path);
  }

  async open(path: string, baudRate = SERIAL_BAUD): Promise<void> {
    const module = await loadSerialModule();
    if (!module) throw new Error('serialport is not available');
    await this.close();
    const port = new module.SerialPort({ path, baudRate, autoOpen: false });
    await new Promise<void>((resolve, reject) => {
      port.open((error) => (error ? reject(error) : resolve()));
    });
    port.on('data', (chunk: Buffer) => this.consume(chunk));
    port.on('error', (error: Error) => {
      this.events.publish('serial', { type: 'error', message: error.message });
    });
    this.port = port;
    this.events.publish('serial', { type: 'status', open: true, path, baudRate });
  }

  async send(command: string): Promise<void> {
    const port = this.port;
    if (!port?.isOpen) throw new Error('serial port is not open');
    const line = command.trim();
    await new Promise<void>((resolve, reject) => {
      port.write(`${line}\n`, (error) => (error ? reject(error) : resolve()));
    });
    this.events.publish('serial', { type: 'sent', line });
  }

  async close(): Promise<void> {
    const port = this.port;
    this.port = null;
    this.buffer = '';
    if (!port) return;
    await new Promise<void>((resolve) => {
      if (!port.isOpen) {
        resolve();
        return;
      }
      port.close(() => resolve());
    });
    this.events.publish('serial', { type: 'status', open: false });
  }

  private consume(chunk: Buffer): void {
    this.buffer += chunk.toString('utf8');
    let index = this.buffer.indexOf('\n');
    while (index >= 0) {
      const line = this.buffer.slice(0, index).replace(/\r$/, '');
      this.buffer = this.buffer.slice(index + 1);
      if (line.length > 0) this.events.publish('serial', { type: 'line', line });
      index = this.buffer.indexOf('\n');
    }
  }
}
