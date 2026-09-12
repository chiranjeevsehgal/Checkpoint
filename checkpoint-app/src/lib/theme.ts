import { DarkTheme, DefaultTheme, type Theme } from 'expo-router';

export interface Palette {
  background: string;
  foreground: string;
  card: string;
  cardForeground: string;
  popover: string;
  popoverForeground: string;
  surface: string;
  inputBg: string;
  primary: string;
  primaryForeground: string;
  primaryPressed: string;
  primaryText: string;
  secondary: string;
  secondaryForeground: string;
  muted: string;
  mutedForeground: string;
  subtleForeground: string;
  accent: string;
  accentForeground: string;
  success: string;
  warning: string;
  destructive: string;
  destructiveForeground: string;
  border: string;
  borderStrong: string;
  divider: string;
  switchThumb: string;
  ring: string;
}

export const THEME: Record<'light' | 'dark', Palette> = {
  light: {
    background: '#f7f6f3',
    foreground: '#1d1b1a',
    card: '#ffffff',
    cardForeground: '#1d1b1a',
    popover: '#f0ede9',
    popoverForeground: '#1d1b1a',
    surface: '#ffffff',
    inputBg: '#faf9f7',
    primary: '#ff6b57',
    primaryForeground: '#1d1b1a',
    primaryPressed: '#e95a47',
    primaryText: '#c83d2d',
    secondary: '#f0ede9',
    secondaryForeground: '#1d1b1a',
    muted: '#f0ede9',
    mutedForeground: '#625e59',
    subtleForeground: '#77716b',
    accent: '#f0ede9',
    accentForeground: '#1d1b1a',
    success: '#247a50',
    warning: '#9a6700',
    destructive: '#c63e3e',
    destructiveForeground: '#f7f5f2',
    border: '#d8d3cd',
    borderStrong: '#bdb7b0',
    divider: '#d8d3cd',
    switchThumb: '#f7f5f2',
    ring: '#ff6b57',
  },
  dark: {
    background: '#151514',
    foreground: '#f7f5f2',
    card: '#211e1d',
    cardForeground: '#f7f5f2',
    popover: '#292524',
    popoverForeground: '#f7f5f2',
    surface: '#211e1d',
    inputBg: '#1b1918',
    primary: '#ff6b57',
    primaryForeground: '#1d1b1a',
    primaryPressed: '#e95a47',
    primaryText: '#ff8272',
    secondary: '#292524',
    secondaryForeground: '#f7f5f2',
    muted: '#292524',
    mutedForeground: '#b7b2ad',
    subtleForeground: '#88837f',
    accent: '#292524',
    accentForeground: '#f7f5f2',
    success: '#55c58a',
    warning: '#e5b85c',
    destructive: '#f06464',
    destructiveForeground: '#1d1b1a',
    border: '#3d3937',
    borderStrong: '#57514d',
    divider: '#3d3937',
    switchThumb: '#f7f5f2',
    ring: '#ff6b57',
  },
};

export const PALETTE = THEME;

export const NAV_THEME: Record<'light' | 'dark', Theme> = {
  light: {
    ...DefaultTheme,
    colors: {
      background: THEME.light.background,
      border: THEME.light.border,
      card: THEME.light.card,
      notification: THEME.light.destructive,
      primary: THEME.light.primary,
      text: THEME.light.foreground,
    },
  },
  dark: {
    ...DarkTheme,
    colors: {
      background: THEME.dark.background,
      border: THEME.dark.border,
      card: THEME.dark.card,
      notification: THEME.dark.destructive,
      primary: THEME.dark.primary,
      text: THEME.dark.foreground,
    },
  },
};
