import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';

import { EventsService, isTaskBusy } from './core/events';

const THEME_KEY = 'ck-admin-theme';

@Component({
  selector: 'ck-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  templateUrl: './app.html',
  styleUrl: './app.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App {
  private readonly events = inject(EventsService);

  protected readonly navItems = [
    { path: '/', label: 'Dashboard' },
    { path: '/devices', label: 'Devices' },
    { path: '/firmware', label: 'Firmware' },
    { path: '/serial', label: 'Serial' },
    { path: '/users', label: 'Users' },
    { path: '/deletions', label: 'Deletions' },
  ];

  protected readonly dark = signal(readStoredTheme());
  protected readonly busy = computed(() => isTaskBusy(this.events.taskEvents()));

  constructor() {
    this.events.connectTasks();
    applyTheme(this.dark());
  }

  protected toggleTheme(): void {
    this.dark.update((value) => !value);
    applyTheme(this.dark());
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
