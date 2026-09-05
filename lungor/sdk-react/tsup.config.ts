import { defineConfig } from 'tsup';

export default defineConfig({
  entry: ['src/index.tsx'],
  format: ['esm'],
  platform: 'browser',
  dts: true,
  clean: true,
  sourcemap: true,
  external: ['react'],
});
