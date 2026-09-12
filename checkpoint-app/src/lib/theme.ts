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
  secondary: string;
  secondaryForeground: string;
  muted: string;
  mutedForeground: string;
  accent: string;
  accentForeground: string;
  destructive: string;
  destructiveForeground: string;
  border: string;
  divider: string;
  ring: string;
}

export const THEME: Record<'light' | 'dark', Palette> = {
  light: {
    background: '#f3f2f2',
    foreground: '#201e1d',
    card: '#eae9e9',
    cardForeground: '#201e1d',
    popover: '#eae9e9',
    popoverForeground: '#201e1d',
    surface: '#eae9e9',
    inputBg: '#eae9e9',
    primary: '#ec3013',
    primaryForeground: '#f3f2f2',
    secondary: '#eae9e9',
    secondaryForeground: '#201e1d',
    muted: '#eae9e9',
    mutedForeground: '#7f7d7d',
    accent: '#eae9e9',
    accentForeground: '#201e1d',
    destructive: '#ae1800',
    destructiveForeground: '#f3f2f2',
    border: '#9f9d9d',
    divider: '#9f9d9d',
    ring: '#ec3013',
  },
  dark: {
    background: '#1b1918',
    foreground: '#f3f2f2',
    card: '#262322',
    cardForeground: '#f3f2f2',
    popover: '#262322',
    popoverForeground: '#f3f2f2',
    surface: '#262322',
    inputBg: '#2f2c2b',
    primary: '#ff6b52',
    primaryForeground: '#1b1918',
    secondary: '#2f2c2b',
    secondaryForeground: '#f3f2f2',
    muted: '#2f2c2b',
    mutedForeground: '#929090',
    accent: '#2f2c2b',
    accentForeground: '#f3f2f2',
    destructive: '#ff6b52',
    destructiveForeground: '#1b1918',
    border: '#575655',
    divider: '#575655',
    ring: '#ff6b52',
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
