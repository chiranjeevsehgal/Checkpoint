import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map, startWith } from 'rxjs';

import { EventsService, isTaskBusy } from './core/events';
import { ApiService } from './core/api';
import { setAgentToken } from './core/auth-token';

const THEME_KEY = 'ck-admin-theme';

interface NavItem {
  path: string;
  label: string;
  title: string;
}

const NAV_ITEMS: NavItem[] = [
  { path: '/', label: 'Dashboard', title: 'Dashboard' },
  { path: '/devices', label: 'Devices', title: 'Devices' },
  { path: '/firmware', label: 'Firmware', title: 'Firmware' },
  { path: '/serial', label: 'Serial', title: 'Serial console' },
  { path: '/users', label: 'Users', title: 'Users' },
  { path: '/deletions', label: 'Deletions', title: 'Account deletions' },
  { path: '/settings', label: 'Settings', title: 'Settings' },
];

@Component({
  selector: 'ck-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  templateUrl: './app.html',
  styleUrl: './app.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App {
  private readonly events = inject(EventsService);
  private readonly router = inject(Router);
  private readonly api = inject(ApiService);

  protected readonly navItems = NAV_ITEMS;
  protected readonly dark = signal(readStoredTheme());
  protected readonly busy = computed(() => isTaskBusy(this.events.taskEvents()));
  protected readonly locked = signal(false);
  protected readonly unlockError = signal('');
  protected readonly tokenInput = signal('');

  protected readonly title = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map(() => this.currentTitle()),
      startWith(this.currentTitle()),
    ),
    { initialValue: 'Dashboard' },
  );

  constructor() {
    this.events.connectTasks();
    applyTheme(this.dark());
    void this.checkAuth();
  }

  protected toggleTheme(): void {
    this.dark.update((value) => !value);
    applyTheme(this.dark());
  }

  protected unlock(): void {
    setAgentToken(this.tokenInput().trim());
    this.tokenInput.set('');
    this.unlockError.set('');
    void this.checkAuth();
  }

  private async checkAuth(): Promise<void> {
    try {
      await this.api.config();
      if (this.locked()) this.events.reconnectTasks();
      this.locked.set(false);
    } catch (error) {
      if (error instanceof Error && error.message === 'unauthorized') {
        this.locked.set(true);
        this.unlockError.set('The agent needs its token (ADMIN_TOKEN).');
      }
    }
  }

  private currentTitle(): string {
    const path = this.router.url.split(/[?#]/)[0];
    return NAV_ITEMS.find((item) => item.path === path)?.title ?? 'Checkpoint Admin';
  }
}

function readStoredTheme(): boolean {
  return typeof localStorage !== 'undefined' && localStorage.getItem(THEME_KEY) === 'dark';
}

function applyTheme(dark: boolean): void {
  if (typeof document !== 'undefined') {
    document.documentElement.classList.toggle('dark', dark);
  }
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(THEME_KEY, dark ? 'dark' : 'light');
  }
}
