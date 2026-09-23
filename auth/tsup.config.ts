import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    server: "src/server.ts",
    client: "src/client.ts",
    urbangate: "src/urbangate/server.ts",
    "urbangate-client": "src/urbangate/client.ts",
  },
  format: ["esm"],
  dts: { resolve: [/better-auth/, /zod/] },
  clean: true,
  sourcemap: true,
  external: ["react", "react-dom", "better-auth"],
});
