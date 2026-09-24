import { test } from "node:test";
import assert from "node:assert/strict";
import { createUrbangateAuthClient } from "./client.ts";

function stubFetch(answers: Record<string, Array<number>>) {
  const calls: Array<string> = [];
  const real = globalThis.fetch;
  globalThis.fetch = (async (input: string | URL) => {
    const path = new URL(String(input)).pathname;
    calls.push(path);
    const status = answers[path]?.shift() ?? 500;
    return new Response(status === 204 ? null : "{}", { status });
  }) as typeof fetch;
  return { calls, restore: () => (globalThis.fetch = real) };
}

test("a 401 from the core renews the token once and replays the call", async () => {
  const s = stubFetch({
    "/api/v1/writings": [401, 401, 200, 200],
    "/api/auth/core-token": [200],
  });
  try {
    const client = createUrbangateAuthClient({
      baseURL: "https://app.example",
    });
    const [a, b] = await Promise.all([
      client.fetch("https://app.example/api/v1/writings"),
      client.fetch("https://app.example/api/v1/writings"),
    ]);
    assert.equal(a.status, 200);
    assert.equal(b.status, 200);
    assert.equal(s.calls.filter((c) => c === "/api/auth/core-token").length, 1);
  } finally {
    s.restore();
  }
});

test("a renewal refused signs out and hands back the core's 401", async () => {
  const s = stubFetch({
    "/api/v1/writings": [401],
    "/api/auth/core-token": [401],
  });
  let signedOut = 0;
  try {
    const client = createUrbangateAuthClient({
      baseURL: "https://app.example",
      onSignedOut: () => signedOut++,
    });
    const res = await client.fetch("https://app.example/api/v1/writings");
    assert.equal(res.status, 401);
    assert.equal(signedOut, 1);
  } finally {
    s.restore();
  }
});

test("an outage at urbangate comes back as 503, not as signed out", async () => {
  const s = stubFetch({
    "/api/v1/writings": [401],
    "/api/auth/core-token": [503],
  });
  let signedOut = 0;
  try {
    const client = createUrbangateAuthClient({
      baseURL: "https://app.example",
      onSignedOut: () => signedOut++,
    });
    const res = await client.fetch("https://app.example/api/v1/writings");
    assert.equal(res.status, 503);
    assert.equal(signedOut, 0);
  } finally {
    s.restore();
  }
});
