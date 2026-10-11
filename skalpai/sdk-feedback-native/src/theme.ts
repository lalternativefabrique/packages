export type Palette = {
  bg: string;
  fg: string;
  panel: string;
  input: string;
  border: string;
  muted: string;
  mutedSoft: string;
  accent: string;
  accentFg: string;
  disabled: string;
  disabledFg: string;
  danger: string;
  backdrop: string;
};

export const LIGHT: Palette = {
  bg: '#ffffff',
  fg: '#111111',
  panel: '#ffffff',
  input: '#ffffff',
  border: 'rgba(0,0,0,0.1)',
  muted: 'rgba(0,0,0,0.4)',
  mutedSoft: 'rgba(0,0,0,0.06)',
  accent: '#7c3aed',
  accentFg: '#ffffff',
  disabled: 'rgba(0,0,0,0.08)',
  disabledFg: 'rgba(0,0,0,0.35)',
  danger: '#dc2626',
  backdrop: 'rgba(0,0,0,0.35)',
};

export const DARK: Palette = {
  bg: '#1a1a1a',
  fg: '#f5f5f5',
  panel: '#1f1f1f',
  input: '#2a2a2a',
  border: 'rgba(255,255,255,0.08)',
  muted: 'rgba(255,255,255,0.45)',
  mutedSoft: 'rgba(255,255,255,0.06)',
  accent: '#7c3aed',
  accentFg: '#ffffff',
  disabled: 'rgba(255,255,255,0.08)',
  disabledFg: 'rgba(255,255,255,0.35)',
  danger: '#f87171',
  backdrop: 'rgba(0,0,0,0.55)',
};
