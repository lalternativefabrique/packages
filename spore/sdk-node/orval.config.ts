import { defineConfig } from "orval";

export default defineConfig({
  spore: {
    input: "../openapi.json",
    output: {
      mode: "split",
      target: "./src/generated/spore.ts",
      schemas: "./src/generated/model",
      client: "axios",
      clean: true,
      prettier: false,
      override: {
        mutator: {
          path: "./src/http-client.ts",
          name: "sporeHttp",
        },
      },
    },
  },
});
