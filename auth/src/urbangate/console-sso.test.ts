import { test } from "node:test";
import assert from "node:assert/strict";
import { createUrbangateAuth } from "./server.ts";
import { decodePending, encodePending, localPath } from "./sso.ts";

const rsa = { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" };

async function signer(kid: string) {
  const pair = (await crypto.subtle.generateKey(
    { ...rsa, modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]) },
    true,
    ["sign", "verify"],
  )) as CryptoKeyPair;
  const jwk = {
    ...(await crypto.subtle.exportKey("jwk", pair.publicKey)),
    kid,
    use: "sig",
  };
  const sign = async (payload: Record<string, unknown>) => {
    const part = (o: unknown) =>
      Buffer.from(JSON.stringify(o)).toString("base64url");
    const head = `${part({ alg: "RS256", kid, typ: "JWT" })}.${part(payload)}`;
    const sig = await crypto.subtle.sign(
      rsa,
      pair.privateKey,
      new TextEncoder().encode(head),
    );
    return `${head}.${Buffer.from(sig).toString("base64url")}`;
  };
  return { jwk, sign };
}

const hydra = await signer("k1");
const stranger = await signer("k1");
const claims = (roles: Array<string>) => ({
  iss: "https://id.urbangate.dev",
  aud: ["https://id.urbangate.dev/oauth2/token", "partage"],
  sub: "8f3a",
  roles,
  exp: Math.floor(Date.now() / 1000) + 900,
});
const adminToken = await hydra.sign(claims(["partage:admin"]));
const userToken = await hydra.sign(claims(["partage:user"]));
const forgedToken = await stranger.sign(claims(["partage:admin"]));

function hydraStub(
  overrides: Record<string, (init: RequestInit) => Response> = {},
) {
  const calls: Array<{ key: string; body: URLSearchParams | null }> = [];
  const fetchImpl = (async (url: URL | string, init: RequestInit = {}) => {
    const u = new URL(String(url));
    const key = `${init.method ?? "GET"} ${u.pathname}`;
    calls.push({
      key,
      body: init.body instanceof URLSearchParams ? init.body : null,
    });
    if (overrides[key]) return overrides[key](init);
    if (key === "POST /oauth2/token")
      return Response.json({
        access_token: adminToken,
        refresh_token: "rt2",
        expires_in: 900,
      });
    if (key === "GET /userinfo")
      return Response.json({
        sub: "8f3a",
        email: "ana@example",
        email_verified: true,
        name: "Ana",
      });
    if (key === "GET /.well-known/jwks.json")
      return Response.json({ keys: [hydra.jwk] });
    if (key === "POST /oauth2/revoke")
      return new Response(null, { status: 200 });
    if (key === "DELETE /self-service/logout/api")
      return new Response(null, { status: 204 });
    return new Response("{}", { status: 500 });
  }) as typeof fetch;
  return { fetchImpl, calls };
}

function auth(fetchImpl: typeof fetch, sso = true) {
  return createUrbangateAuth({
    product: "partage",
    kratosUrl: "https://id.urbangate.dev",
    urbangate: {
      issuerUrl: "https://id.urbangate.dev",
      provisioner: { clientId: "partage-provisioner", clientSecret: "p" },
      admin: { clientId: "partage-admin", clientSecret: "a" },
    },
    ...(sso ? { sso: { appUrl: "https://app.partagg.fr/" } } : {}),
    fetch: fetchImpl,
  });
}

const get = (path: string, cookie = "") =>
  new Request(`https://app.partagg.fr/api/auth/${path}`, {
    headers: cookie ? { cookie } : {},
  });

function pendingCookie(state = "st", landing = "/admin/users") {
  return `partage_sso=${encodePending({ state, verifier: "v", landing })}`;
}

test("the console sign-in is not mounted without sso", async () => {
  const { fetchImpl } = hydraStub();
  const res = await auth(fetchImpl, false).handler(get("sign-in/urbangate"));
  assert.equal(res.status, 404);
});

test("sign-in/urbangate redirects to Hydra with PKCE on the admin client", async () => {
  const { fetchImpl } = hydraStub();
  const res = await auth(fetchImpl).handler(
    get("sign-in/urbangate?callbackURL=/admin/users"),
  );
  assert.equal(res.status, 302);
  const location = new URL(res.headers.get("location") ?? "");
  assert.equal(
    location.origin + location.pathname,
    "https://id.urbangate.dev/oauth2/auth",
  );
  const q = location.searchParams;
  assert.equal(q.get("client_id"), "partage-admin");
  assert.equal(
    q.get("redirect_uri"),
    "https://app.partagg.fr/api/auth/callback/urbangate",
  );
  assert.equal(q.get("audience"), "partage");
  assert.equal(q.get("code_challenge_method"), "S256");
  assert.match(q.get("scope") ?? "", /offline_access/);
  const set = res.headers
    .getSetCookie()
    .find((c) => c.startsWith("partage_sso="));
  const pending = decodePending(
    set?.split(";")[0].slice("partage_sso=".length),
  );
  assert.equal(pending?.state, q.get("state"));
  assert.equal(pending?.landing, "/admin/users");
  assert.notEqual(q.get("code_challenge"), pending?.verifier);
});

test("the landing path stays on the product", () => {
  assert.equal(localPath("/admin", "/x"), "/admin");
  assert.equal(localPath("//evil.example", "/x"), "/x");
  assert.equal(localPath("https://evil.example", "/x"), "/x");
  assert.equal(localPath("/\\evil.example", "/x"), "/x");
  assert.equal(localPath(null, "/x"), "/x");
});

test("the callback trades the code, keeps both tokens, and lands where asked", async () => {
  const { fetchImpl, calls } = hydraStub();
  const res = await auth(fetchImpl).handler(
    get(
      "callback/urbangate?code=c1&state=st",
      `${pendingCookie()}; partage_session=ory_st`,
    ),
  );
  assert.equal(res.status, 302);
  assert.equal(res.headers.get("location"), "/admin/users");
  const token = calls.find((c) => c.key === "POST /oauth2/token")?.body;
  assert.equal(token?.get("grant_type"), "authorization_code");
  assert.equal(token?.get("code"), "c1");
  assert.equal(token?.get("code_verifier"), "v");
  assert.equal(token?.get("client_secret"), "a");
  const cookies = res.headers.getSetCookie();
  assert.ok(cookies.some((c) => c.startsWith("partage_admin=rt2")));
  assert.ok(
    cookies.some((c) =>
      c.startsWith(`partage_token=${encodeURIComponent(adminToken)}`),
    ),
  );
  assert.ok(cookies.some((c) => /^partage_session=;.*Max-Age=0/.test(c)));
  assert.ok(calls.some((c) => c.key === "DELETE /self-service/logout/api"));
});

test("a callback whose state does not match is refused before any token call", async () => {
  const { fetchImpl, calls } = hydraStub();
  const res = await auth(fetchImpl).handler(
    get("callback/urbangate?code=c1&state=other", pendingCookie()),
  );
  assert.equal(res.headers.get("location"), "/admin/login?error=sso_state");
  assert.equal(calls.length, 0);
  const none = await auth(fetchImpl).handler(
    get("callback/urbangate?code=c1&state=st"),
  );
  assert.equal(none.headers.get("location"), "/admin/login?error=sso_state");
});

test("someone without the admin role is sent back and their token revoked", async () => {
  const { fetchImpl, calls } = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({
        access_token: userToken,
        refresh_token: "rt2",
        expires_in: 900,
      }),
  });
  const res = await auth(fetchImpl).handler(
    get("callback/urbangate?code=c1&state=st", pendingCookie()),
  );
  assert.equal(res.headers.get("location"), "/admin/login?error=not_admin");
  assert.ok(
    !res.headers.getSetCookie().some((c) => c.startsWith("partage_admin=rt2")),
  );
  assert.equal(
    calls.find((c) => c.key === "POST /oauth2/revoke")?.body?.get("token"),
    "rt2",
  );
});

test("a refused code and an unreachable Hydra are told apart", async () => {
  const refused = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({ error: "invalid_grant" }, { status: 400 }),
  });
  const r1 = await auth(refused.fetchImpl).handler(
    get("callback/urbangate?code=c1&state=st", pendingCookie()),
  );
  assert.equal(r1.headers.get("location"), "/admin/login?error=sso_refused");
  const down = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({ error: "invalid_client" }, { status: 401 }),
  });
  const r2 = await auth(down.fetchImpl).handler(
    get("callback/urbangate?code=c1&state=st", pendingCookie()),
  );
  assert.equal(r2.headers.get("location"), "/admin/login?error=unavailable");
});

test("get-session verifies the console token, reads userinfo once, then keeps the profile", async () => {
  const { fetchImpl, calls } = hydraStub();
  const a = auth(fetchImpl);
  const first = await a.handler(
    get("get-session", `partage_admin=rt1; partage_token=${adminToken}`),
  );
  const body = (await first.json()) as { user: Record<string, unknown> };
  assert.deepEqual(body.user, {
    id: "8f3a",
    identityId: "8f3a",
    email: "ana@example",
    emailVerified: true,
    name: "Ana",
    role: "admin",
  });
  const jar = first.headers
    .getSetCookie()
    .map((c) => c.split(";")[0])
    .join("; ");
  assert.match(jar, /partage_profile=/);
  assert.match(jar, /partage_seal=/);
  const again = await a.handler(get("get-session", jar));
  assert.equal(
    ((await again.json()) as { user: { name: string } }).user.name,
    "Ana",
  );
  assert.equal(calls.filter((c) => c.key === "GET /userinfo").length, 1);
});

test("a token cookie Hydra did not sign is replaced by the refresh token's own", async () => {
  const { fetchImpl, calls } = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({
        access_token: userToken,
        refresh_token: "rt2",
        expires_in: 900,
      }),
  });
  const res = await auth(fetchImpl).handler(
    get("get-session", `partage_admin=rt1; partage_token=${forgedToken}`),
  );
  const body = (await res.json()) as { user: { role: string } };
  assert.equal(body.user.role, "user");
  assert.equal(
    calls.find((c) => c.key === "POST /oauth2/token")?.body?.get("grant_type"),
    "refresh_token",
  );
  const none = await auth(fetchImpl).handler(
    get("get-session", `partage_token=${forgedToken}`),
  );
  assert.equal(await none.json(), null);
});

test("a console session answers 503 while urbangate cannot answer, never a signed-out 200", async () => {
  const jwksAndHydraDown = hydraStub({
    "GET /.well-known/jwks.json": () => new Response("", { status: 502 }),
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const r1 = await auth(jwksAndHydraDown.fetchImpl).handler(
    get("get-session", `partage_admin=rt1; partage_token=${adminToken}`),
  );
  assert.equal(r1.status, 503);
  const userinfoDown = hydraStub({
    "GET /userinfo": () => new Response("", { status: 502 }),
  });
  const r2 = await auth(userinfoDown.fetchImpl).handler(
    get("get-session", `partage_admin=rt1; partage_token=${adminToken}`),
  );
  assert.equal(r2.status, 503);
  const refreshDown = hydraStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const r3 = await auth(refreshDown.fetchImpl).handler(
    get("get-session", "partage_admin=rt1"),
  );
  assert.equal(r3.status, 503);
});

test("core-token renews every cookie, is 401 signed out and 503 in an outage", async () => {
  const { fetchImpl } = hydraStub();
  const ok = await auth(fetchImpl).handler(
    get("core-token", "partage_admin=rt1"),
  );
  assert.equal(ok.status, 200);
  const set = ok.headers.getSetCookie();
  assert.ok(set.some((c) => c.startsWith("partage_admin=rt2")));
  assert.ok(set.some((c) => c.startsWith("partage_token=")));
  const none = await auth(fetchImpl).handler(get("core-token"));
  assert.equal(none.status, 401);
  const down = hydraStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const out = await auth(down.fetchImpl).handler(
    get("core-token", "partage_admin=rt1"),
  );
  assert.equal(out.status, 503);
});

test("profile answers the shape the admin package reads", async () => {
  const { fetchImpl } = hydraStub();
  const res = await auth(fetchImpl).handler(
    get("profile", `partage_admin=rt1; partage_token=${adminToken}`),
  );
  assert.deepEqual(await res.json(), {
    user_id: "8f3a",
    email: "ana@example",
    name: "Ana",
    avatar_url: "",
    roles: ["admin"],
  });
  const none = await auth(fetchImpl).handler(get("profile"));
  assert.equal(none.status, 401);
});

test("an expired console token is refreshed once, and both cookies are renewed", async () => {
  const { fetchImpl, calls } = hydraStub();
  const a = auth(fetchImpl);
  const headers = new Headers({ cookie: "partage_admin=rt1" });
  const [t1, t2] = await Promise.all([
    a.accessToken(headers),
    a.accessToken(headers),
  ]);
  assert.equal(t1?.token, adminToken);
  assert.equal(t2?.token, adminToken);
  const refreshes = calls.filter((c) => c.key === "POST /oauth2/token");
  assert.equal(refreshes.length, 1);
  assert.equal(refreshes[0].body?.get("grant_type"), "refresh_token");
  assert.equal(refreshes[0].body?.get("refresh_token"), "rt1");
  assert.ok(t1?.setCookies?.some((c) => c.startsWith("partage_admin=rt2")));
  assert.ok(t1?.setCookies?.some((c) => c.startsWith("partage_token=")));
});

test("a spent refresh token signs out; an unreachable Hydra is an outage", async () => {
  const spent = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({ error: "invalid_grant" }, { status: 400 }),
  });
  assert.equal(
    await auth(spent.fetchImpl).accessToken(
      new Headers({ cookie: "partage_admin=rt1" }),
    ),
    null,
  );
  const down = hydraStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  await assert.rejects(
    auth(down.fetchImpl).accessToken(
      new Headers({ cookie: "partage_admin=rt1" }),
    ),
  );
});

test("with sso, a session opened on the product's screens is never admin", async () => {
  const session = {
    active: true,
    expires_at: "2027-01-01T00:00:00Z",
    identity: { id: "8f3a", traits: { email: "ana@example" } },
  };
  const { fetchImpl } = hydraStub({
    "GET /sessions/whoami": () => Response.json(session),
  });
  const withSso = await auth(fetchImpl).getSession(
    new Headers({
      cookie: `partage_session=ory_st; partage_token=${adminToken}`,
    }),
  );
  assert.equal(withSso?.user.role, "user");
  const without = await auth(fetchImpl, false).getSession(
    new Headers({
      cookie: `partage_session=ory_st; partage_token=${adminToken}`,
    }),
  );
  assert.equal(without?.user.role, "admin");
});

test("sign-out revokes the console refresh token and clears its cookie", async () => {
  const { fetchImpl, calls } = hydraStub();
  const res = await auth(fetchImpl).handler(
    new Request("https://app.partagg.fr/api/auth/sign-out", {
      method: "POST",
      headers: { cookie: "partage_admin=rt1" },
    }),
  );
  assert.equal(res.status, 200);
  assert.equal(
    calls.find((c) => c.key === "POST /oauth2/revoke")?.body?.get("token"),
    "rt1",
  );
  assert.ok(
    res.headers
      .getSetCookie()
      .some((c) => /^partage_admin=;.*Max-Age=0/.test(c)),
  );
});

test("coreProxy carries the console token and renews both cookies", async () => {
  const { fetchImpl } = hydraStub();
  const a = auth(fetchImpl);
  const seen: Array<string | null> = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async (_url: URL | string, init: RequestInit = {}) => {
    seen.push(new Headers(init.headers).get("authorization"));
    return new Response("ok");
  }) as typeof fetch;
  try {
    const res = await a.coreProxy({
      coreUrl: "http://core:4100",
      adminOnly: true,
    })(
      new Request("https://app.partagg.fr/api/v1/admin/stats", {
        headers: { cookie: "partage_admin=rt1" },
      }),
    );
    assert.equal(res.status, 200);
    assert.deepEqual(seen, [`Bearer ${adminToken}`]);
    assert.ok(
      res.headers.getSetCookie().some((c) => c.startsWith("partage_admin=rt2")),
    );
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("an exchanged token beside a junk console cookie is no console session", async () => {
  const { fetchImpl, calls } = hydraStub({
    "POST /oauth2/token": () =>
      Response.json({ error: "invalid_grant" }, { status: 400 }),
  });
  const res = await auth(fetchImpl).handler(
    get("get-session", `partage_admin=junk; partage_token=${adminToken}`),
  );
  assert.equal(await res.json(), null);
  assert.equal(
    calls
      .find((c) => c.key === "POST /oauth2/token")
      ?.body?.get("refresh_token"),
    "junk",
  );
});

test("a profile cookie rewritten by the browser is read again from Hydra", async () => {
  const { fetchImpl, calls } = hydraStub();
  const a = auth(fetchImpl);
  const first = await a.handler(get("get-session", "partage_admin=rt1"));
  const cookies = first.headers.getSetCookie().map((c) => c.split(";")[0]);
  const forged = cookies.map((c) => {
    if (!c.startsWith("partage_profile=")) return c;
    const p = JSON.parse(
      decodeURIComponent(c.slice("partage_profile=".length)),
    );
    return `partage_profile=${encodeURIComponent(JSON.stringify({ ...p, email: "boss@example" }))}`;
  });
  const again = await a.handler(get("get-session", forged.join("; ")));
  const body = (await again.json()) as { user: { email: string } };
  assert.equal(body.user.email, "ana@example");
  assert.equal(calls.filter((c) => c.key === "GET /userinfo").length, 2);
});
