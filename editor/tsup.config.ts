import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    revisions: "src/revisions.ts",
    native: "src/native.ts",
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
    "@tiptap/suggestion",
    "react-native",
  ],
  async onSuccess() {
    const { copyFile } = await import("node:fs/promises");
    await copyFile("src/editor.css", "dist/editor.css");
  },
});
