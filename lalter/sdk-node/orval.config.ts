import { defineConfig } from "orval";

export default defineConfig({
  lalter: {
    input: {
      // Pre-filtered by scripts/filter-openapi.mjs (run via `pnpm generate`,
      // see package.json) from openapi.full.json — orval's own `filters.tags`
      // only prunes paths, not schemas with no surviving path, so it would
      // otherwise still emit AppKeyDTO, NoteDTO and every other context's
      // types alongside tasks and chat. See ../sdk-go/README.md for why an
      // allow list is the right choice here.
      target: "./openapi.json",
      filters: {
        mode: "include",
        tags: ["tasks", "chat"],
      },
    },
    output: {
      mode: "split",
      target: "./src/generated/lalter.ts",
      schemas: "./src/generated/model",
      client: "axios",
      clean: true,
      prettier: false,
      override: {
        mutator: {
          path: "./src/http-client.ts",
          name: "lalterHttp",
        },
      },
    },
  },
});
