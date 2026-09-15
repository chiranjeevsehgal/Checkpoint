import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./features/dashboard/dashboard').then((module) => module.Dashboard),
  },
  {
    path: 'devices',
    loadComponent: () => import('./features/devices/devices').then((module) => module.Devices),
  },
  {
    path: 'firmware',
    loadComponent: () => import('./features/firmware/firmware').then((module) => module.Firmware),
  },
  {
    path: 'serial',
    loadComponent: () => import('./features/serial/serial').then((module) => module.Serial),
  },
  {
    path: 'users',
    loadComponent: () => import('./features/users/users').then((module) => module.Users),
  },
  {
    path: 'deletions',
    loadComponent: () => import('./features/deletions/deletions').then((module) => module.Deletions),
  },
  { path: '**', redirectTo: '' },
];
