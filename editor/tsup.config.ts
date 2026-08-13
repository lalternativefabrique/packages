import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    revisions: "src/revisions.ts",
  },
  format: ["esm"],
  dts: true,
  clean: true,
  sourcemap: true,
  external: [
    "react",
    "react-dom",
    "@tiptap/core",
    "@tiptap/pm",
    "@tiptap/react",
    "@tiptap/starter-kit",
  ],
  async onSuccess() {
    const { copyFile } = await import("node:fs/promises");
    await copyFile("src/editor.css", "dist/editor.css");
  },
});
