import { test } from "node:test";
import assert from "node:assert/strict";
import { createDevAuth } from "./server.ts";

const issuer = "http://web:5273";

function auth(extra: Partial<Parameters<typeof createDevAuth>[0]> = {}) {
  return createDevAuth({ product: "messag", issuer, coreUrl: "http://core:4100", ...extra });
}

function post(path: string, body: unknown, cookie?: string) {
  return new Request(`http://localhost:5273/api/auth/${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(cookie ? { cookie } : {}) },
    body: JSON.stringify(body),
  });
}

function cookieOf(res: Response) {
  return res.headers.getSetCookie()[0].split(";")[0];
}

function claimsOf(token: string) {
  const body = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(body));
}

async function signIn(a = auth(), email = "Ana@Example.org") {
  const res = await a.handler(post("sign-in/email", { email, password: "anything" }));
  assert.equal(res.status, 200);
  return cookieOf(res);
}

async function withCore<T>(
  answer: (url: string, init: RequestInit) => Response,
  run: (calls: Array<{ url: string; init: RequestInit }>) => Promise<T>,
) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (url: URL | string, init: RequestInit = {}) => {
    calls.push({ url: String(url), init });
    return answer(String(url), init);
  }) as typeof fetch;
  try {
    return await run(calls);
  } finally {
    globalThis.fetch = original;
  }
}

test("any address and password open a session, read back by get-session", async () => {
  const a = auth();
  const cookie = await signIn(a);
  assert.match(cookie, /^messag_session=/);
  const res = await a.handler(
    new Request("http://localhost:5273/api/auth/get-session", { headers: { cookie } }),
  );
  const s = await res.json();
  assert.equal(s.user.email, "ana@example.org");
  assert.equal(s.user.role, "user");
  assert.equal(s.user.id, s.user.identityId);
});

test("the same address is the same identity across sign-ins", async () => {
  const a = auth();
  const one = await a.getSession(new Headers({ cookie: await signIn(a) }));
  const two = await a.getSession(new Headers({ cookie: await signIn(a, "ana@example.org") }));
  assert.equal(one?.user.identityId, two?.user.identityId);
});

test("a sign-in without a password is refused", async () => {
  const res = await auth().handler(post("sign-in/email", { email: "ana@example.org" }));
  assert.equal(res.status, 400);
});

test("a forged session cookie is no session", async () => {
  const a = auth();
  const [head, , sig] = (await signIn(a)).split("=")[1].split(".");
  const body = btoa(JSON.stringify({ iss: issuer, identityId: "x", email: "eve@x", exp: 9e9 }))
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  assert.equal(
    await a.getSession(new Headers({ cookie: `messag_session=${head}.${body}.${sig}` })),
    null,
  );
});

test("a session signed for another issuer is no session", async () => {
  const cookie = await signIn(auth({ issuer: "http://other:1" }));
  assert.equal(await auth().getSession(new Headers({ cookie })), null);
});

test("the listed addresses sign in as admins", async () => {
  const a = auth({ admins: ["Ana@example.org"] });
  const s = await a.getSession(new Headers({ cookie: await signIn(a) }));
  assert.equal(s?.user.role, "admin");
  const guarded = await a.requireAdmin(new Headers({ cookie: await signIn(a, "bob@example.org") }));
  assert.ok("response" in guarded && guarded.response.status === 403);
});

test("the token the core gets verifies against the published key set", async () => {
  const a = auth();
  const token = (await a.accessToken(new Headers({ cookie: await signIn(a) })))!.token;
  const jwks = await (await a.handler(new Request("http://localhost:5273/api/auth/jwks"))).json();
  const key = await crypto.subtle.importKey("jwk", jwks.keys[0], { name: "Ed25519" }, false, ["verify"]);
  const [head, body, sig] = token.split(".");
  const b = sig.replace(/-/g, "+").replace(/_/g, "/");
  const valid = await crypto.subtle.verify(
    { name: "Ed25519" },
    key,
    Uint8Array.from(atob(b), (c) => c.charCodeAt(0)),
    new TextEncoder().encode(`${head}.${body}`),
  );
  assert.ok(valid);
  const claims = claimsOf(token);
  assert.equal(claims.iss, issuer);
  assert.equal(claims.aud, "messag");
  assert.equal(claims.sub, claims.identityId);
  assert.equal(jwks.keys[0].d, undefined);
  assert.ok(claims.exp - claims.iat <= 15 * 60);
});

test("the proxy forwards to the core with the person's token, cookies stripped", async () => {
  const a = auth();
  const cookie = await signIn(a);
  await withCore(
    () => new Response("{}", { status: 200 }),
    async (calls) => {
      const res = await a.coreProxy({ stripPrefix: "/api/core" })(
        new Request("http://localhost:5273/api/core/me?x=1", { headers: { cookie } }),
      );
      assert.equal(res.status, 200);
      assert.equal(calls[0].url, "http://core:4100/me?x=1");
      const h = new Headers(calls[0].init.headers);
      assert.equal(h.get("cookie"), null);
      assert.equal(claimsOf(h.get("authorization")!.slice(7)).email, "ana@example.org");
    },
  );
});

test("the proxy refuses a signed-out request unless anonymous requests are forwarded", async () => {
  const a = auth();
  await withCore(
    () => new Response("{}", { status: 401 }),
    async (calls) => {
      const refused = await a.coreProxy()(new Request("http://localhost:5273/api/v1/me"));
      assert.equal(refused.status, 401);
      assert.equal(calls.length, 0);
      const forwarded = await a.coreProxy({ anonymous: "forward" })(
        new Request("http://localhost:5273/api/v1/me", { headers: { authorization: "Bearer app-key" } }),
      );
      assert.equal(forwarded.status, 401);
      assert.equal(new Headers(calls[0].init.headers).get("authorization"), "Bearer app-key");
    },
  );
});

test("adminOnly keeps a signed-in user away from the core", async () => {
  const a = auth();
  const cookie = await signIn(a);
  await withCore(
    () => new Response("{}"),
    async (calls) => {
      const res = await a.coreProxy({ adminOnly: true })(
        new Request("http://localhost:5273/api/v1/admin", { headers: { cookie } }),
      );
      assert.equal(res.status, 403);
      assert.equal(calls.length, 0);
    },
  );
});

test("sign-up runs onAccountOpened with the new session", async () => {
  let seen: Headers | undefined;
  const a = auth({
    onAccountOpened: ({ headers }) => {
      seen = headers;
      return ["extra=1; Path=/"];
    },
  });
  const res = await a.handler(post("sign-up/email", { email: "new@example.org", password: "p" }));
  assert.equal(res.headers.getSetCookie().length, 2);
  assert.equal((await a.getSession(seen!))?.user.email, "new@example.org");
});

test("sign-out clears the session cookie", async () => {
  const res = await auth().handler(post("sign-out", {}));
  assert.match(res.headers.getSetCookie()[0], /^messag_session=;.*Max-Age=0/);
});
