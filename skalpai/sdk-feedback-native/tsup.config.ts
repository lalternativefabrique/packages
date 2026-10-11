import { defineConfig } from 'tsup';

export default defineConfig({
  entry: ['src/index.ts', 'src/expo-image-picker.ts', 'src/view-shot.ts'],
  format: ['esm'],
  platform: 'neutral',
  target: 'es2020',
  dts: true,
  clean: true,
  sourcemap: true,
  external: [
    'react',
    'react/jsx-runtime',
    'react-native',
    'react-native-safe-area-context',
    'expo-image-picker',
    'react-native-view-shot',
  ],
});
