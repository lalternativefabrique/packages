import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    urbangate: "src/urbangate/server.ts",
    "urbangate-client": "src/urbangate/client.ts",
    "urbangate-native": "src/urbangate/native.ts",
  },
  format: ["esm"],
  dts: true,
  clean: true,
  sourcemap: true,
  external: ["react", "react-dom"],
});
