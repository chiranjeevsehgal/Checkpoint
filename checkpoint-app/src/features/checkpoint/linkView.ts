export interface LinkView {
  label: string;
  sub: string;
  openSettings: boolean;
}

const VIEWS: Record<string, LinkView> = {
  idle: {
    label: 'Not connected',
    sub: 'Tap Connect to look for your pendant.',
    openSettings: false,
  },
  connecting: {
    label: 'Connecting…',
    sub: 'Looking for your pendant',
    openSettings: false,
  },
  listening: {
    label: 'Connected',
    sub: 'Linked to your pendant',
    openSettings: false,
  },
  'needs permission': {
    label: 'Permission needed',
    sub: 'Checkpoint needs Bluetooth and location access to scan for your pendant.',
    openSettings: true,
  },
  'permission denied': {
    label: 'Permission denied',
    sub: 'Bluetooth access was denied. Enable it in system settings to connect.',
    openSettings: true,
  },
  'bluetooth off': {
    label: 'Bluetooth is off',
    sub: 'Turn on Bluetooth on this phone, then try again.',
    openSettings: false,
  },
  'bluetooth unavailable': {
    label: 'Bluetooth unavailable',
    sub: "This device doesn't support Bluetooth.",
    openSettings: false,
  },
};

export function linkView(state: string, deviceName: string): LinkView {
  const view = VIEWS[state] ?? VIEWS.idle!;
  if (state === 'connecting') return { ...view, sub: `Looking for "${deviceName}" Pendant` };
  if (state === 'listening') return { ...view, sub: `Linked to ${deviceName} Pendant` };
  return view;
}
