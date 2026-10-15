import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
  },
  format: ["esm"],
  dts: true,
  clean: true,
  sourcemap: true,
  external: ["react", "react-dom"],
  async onSuccess() {
    const { copyFile } = await import("node:fs/promises");
    await copyFile("src/assistant.css", "dist/assistant.css");
  },
});
