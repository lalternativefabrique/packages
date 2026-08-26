#!/usr/bin/env node
// Trims lalter's full OpenAPI document down to the tasks/chat surface this
// SDK exposes, before handing it to orval.
//
// orval's `filters.tags` (see orval.config.ts) only prunes PATHS — schemas
// with no path left referencing them are still emitted, because orval's
// split mode generates one file per schema in the document regardless of
// whether any surviving operation uses it. Left alone, this SDK would export
// AppKeyDTO, NoteDTO, VoiceTranscribeResponse and every other context's types
// alongside tasks and chat — exactly what the allow list in
// oapi-codegen.yaml (the Go side) exists to prevent. This script is that same
// allow list, applied to the document orval reads.
import { readFileSync, writeFileSync } from "node:fs";

const ALLOWED_TAGS = new Set(["tasks", "chat"]);

const [, , inPath, outPath] = process.argv;
if (!inPath || !outPath) {
  console.error("usage: filter-openapi.mjs <in.json> <out.json>");
  process.exit(1);
}

const doc = JSON.parse(readFileSync(inPath, "utf8"));

const keptPaths = {};
for (const [path, methods] of Object.entries(doc.paths ?? {})) {
  const keptMethods = {};
  for (const [method, op] of Object.entries(methods)) {
    if ((op.tags ?? []).some((t) => ALLOWED_TAGS.has(t))) {
      keptMethods[method] = op;
    }
  }
  if (Object.keys(keptMethods).length > 0) {
    keptPaths[path] = keptMethods;
  }
}

// Walk every $ref reachable from a kept operation, transitively, so a schema
// another kept schema embeds is not dropped for having no path of its own.
const schemas = doc.components?.schemas ?? {};
const keptSchemas = new Set();
const queue = [];
collectRefs(keptPaths, queue);
while (queue.length > 0) {
  const name = queue.pop();
  if (keptSchemas.has(name)) continue;
  keptSchemas.add(name);
  collectRefs(schemas[name], queue);
}

function collectRefs(node, out) {
  if (Array.isArray(node)) {
    for (const item of node) collectRefs(item, out);
  } else if (node && typeof node === "object") {
    for (const [key, value] of Object.entries(node)) {
      if (key === "$ref" && typeof value === "string") {
        const match = value.match(/^#\/components\/schemas\/(.+)$/);
        if (match) out.push(match[1]);
        continue;
      }
      collectRefs(value, out);
    }
  }
}

const filtered = {
  ...doc,
  paths: keptPaths,
  components: {
    ...doc.components,
    schemas: Object.fromEntries(
      Object.entries(schemas).filter(([name]) => keptSchemas.has(name)),
    ),
  },
};

writeFileSync(outPath, JSON.stringify(filtered, null, 2) + "\n");
console.log(
  `wrote ${outPath}: ${Object.keys(keptPaths).length} paths, ${keptSchemas.size} schemas`,
);
