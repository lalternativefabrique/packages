import { test } from "node:test";
import assert from "node:assert/strict";
import { createUrbangateAuth } from "./server.ts";

const identity = {
  id: "8f3a",
  traits: { email: "ana@example", name: "Ana" },
  verifiable_addresses: [{ value: "ana@example", verified: true }],
};
const session = {
  id: "s1",
  active: true,
  expires_at: "2027-01-01T00:00:00Z",
  identity,
};

function jwt(payload: Record<string, unknown>) {
  return (
    "h." + Buffer.from(JSON.stringify(payload)).toString("base64url") + ".s"
  );
}

function kratosStub(
  overrides: Record<string, (init: RequestInit) => Response> = {},
) {
  const calls: Array<{ key: string; body: unknown; headers: Headers }> = [];
  const fetchImpl = (async (url: URL | string, init: RequestInit = {}) => {
    const u = new URL(String(url));
    const key = `${init.method ?? "GET"} ${u.pathname}${u.search}`;
    calls.push({
      key,
      body: typeof init.body === "string" ? JSON.parse(init.body) : init.body,
      headers: new Headers(init.headers),
    });
    if (overrides[key]) return overrides[key](init);
    if (key === "GET /self-service/login/api")
      return Response.json({ id: "L" });
    if (key === "GET /self-service/registration/api")
      return Response.json({ id: "R" });
    if (key === "GET /self-service/verification/api")
      return Response.json({ id: "V" });
    if (key === "POST /self-service/login?flow=L")
      return Response.json({ session_token: "ory_st", session });
    if (key === "POST /self-service/registration?flow=R") {
      return Response.json({
        session_token: "ory_st",
        session,
        continue_with: [{ action: "show_verification_ui", flow: { id: "V" } }],
      });
    }
    if (key === "POST /self-service/verification?flow=V")
      return Response.json({ id: "V", state: "passed_challenge" });
    if (key === "GET /sessions/whoami") {
      return new Headers(init.headers).get("x-session-token") === "ory_st"
        ? Response.json(session)
        : new Response("", { status: 401 });
    }
    if (key === "DELETE /self-service/logout/api")
      return new Response("", { status: 204 });
    return new Response("{}", { status: 500 });
  }) as typeof fetch;
  return { fetchImpl, calls };
}

function auth(fetchImpl: typeof fetch) {
  return createUrbangateAuth({
    product: "tornad",
    kratosUrl: "http://kratos:4433",
    urbangate: {
      issuerUrl: "https://id.urbangate.dev",
      provisioner: { clientId: "tornad-provisioner", clientSecret: "p" },
      admin: { clientId: "tornad-admin", clientSecret: "a" },
    },
    fetch: fetchImpl,
  });
}

const post = (path: string, body: unknown, cookie = "") =>
  new Request(`https://tornad.dev/api/auth/${path}`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...(cookie ? { cookie } : {}),
    },
    body: JSON.stringify(body),
  });

test("sign-in by password sets the session cookie", async () => {
  const { fetchImpl, calls } = kratosStub();
  const res = await auth(fetchImpl).handler(
    post("sign-in/email", { email: "ana@example", password: "pw" }),
  );
  assert.equal(res.status, 200);
  assert.match(
    res.headers.get("set-cookie") ?? "",
    /tornad_session=ory_st; Path=\/; HttpOnly/,
  );
  const submitted = calls.find(
    (c) => c.key === "POST /self-service/login?flow=L",
  );
  assert.deepEqual(submitted?.body, {
    method: "password",
    identifier: "ana@example",
    password: "pw",
  });
  const body = (await res.json()) as { user: { email: string; role: string } };
  assert.equal(body.user.email, "ana@example");
  assert.equal(body.user.role, "user");
});

test("a wrong password answers 401 invalid_credentials", async () => {
  const { fetchImpl } = kratosStub({
    "POST /self-service/login?flow=L": () =>
      Response.json(
        { ui: { messages: [{ id: 4000006, type: "error" }] } },
        { status: 400 },
      ),
  });
  const res = await auth(fetchImpl).handler(
    post("sign-in/email", { email: "ana@example", password: "x" }),
  );
  assert.equal(res.status, 401);
  assert.deepEqual(await res.json(), {
    error: { code: "invalid_credentials", status: 401 },
  });
});

test("sign-up opens the session, keeps the verification flow, and the code verifies it", async () => {
  const { fetchImpl, calls } = kratosStub();
  const a = auth(fetchImpl);
  const res = await a.handler(
    post("sign-up/email", {
      email: "ana@example",
      password: "pw",
      name: "Ana",
    }),
  );
  assert.equal(res.status, 200);
  const cookies = res.headers.getSetCookie();
  assert.ok(cookies.some((c) => c.startsWith("tornad_session=ory_st")));
  assert.ok(cookies.some((c) => c.startsWith("tornad_flow=verification%3AV")));
  assert.deepEqual(
    calls.find((c) => c.key === "POST /self-service/registration?flow=R")?.body,
    {
      method: "password",
      traits: { email: "ana@example", name: "Ana" },
      password: "pw",
    },
  );
  const verified = await a.handler(
    post(
      "email-otp/verify-email",
      { email: "ana@example", otp: "123456" },
      "tornad_flow=verification%3AV",
    ),
  );
  assert.equal(verified.status, 200);
  assert.deepEqual(
    calls.find((c) => c.key === "POST /self-service/verification?flow=V")?.body,
    { method: "code", code: "123456" },
  );
});

test("get-session reads whoami and the role off the token cookie; sign-out clears everything", async () => {
  const { fetchImpl } = kratosStub();
  const a = auth(fetchImpl);
  const token = jwt({
    sub: "8f3a",
    roles: ["tornad:admin"],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const res = await a.handler(
    new Request("https://tornad.dev/api/auth/get-session", {
      headers: { cookie: `tornad_session=ory_st; tornad_token=${token}` },
    }),
  );
  const body = (await res.json()) as {
    user: { role: string; identityId: string };
  };
  assert.equal(body.user.role, "admin");
  assert.equal(body.user.identityId, "8f3a");
  const none = await a.handler(
    new Request("https://tornad.dev/api/auth/get-session"),
  );
  assert.equal(await none.json(), null);
  const out = await a.handler(post("sign-out", {}, "tornad_session=ory_st"));
  assert.equal(
    out.headers.getSetCookie().filter((c) => c.includes("Max-Age=0")).length,
    3,
  );
});

test("accessToken exchanges when the cookie is missing or stale, and keeps a fresh one", async () => {
  const fresh = jwt({
    sub: "8f3a",
    roles: [],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const { fetchImpl } = kratosStub({
    "POST /oauth2/token": (init) => {
      const form = init.body as URLSearchParams;
      return form.get("grant_type") === "client_credentials"
        ? Response.json({ access_token: "m", expires_in: 3600 })
        : Response.json({ access_token: fresh, expires_in: 900 });
    },
    "POST /api/machine/sessions/exchange": () =>
      Response.json({ assertion: "a.b.c", identity_id: "8f3a", roles: [] }),
  });
  const a = auth(fetchImpl);
  const got = await a.accessToken(
    new Headers({ cookie: "tornad_session=ory_st" }),
  );
  assert.equal(got?.token, fresh);
  assert.match(got?.setCookie ?? "", /^tornad_token=/);
  const kept = await a.accessToken(
    new Headers({ cookie: `tornad_session=ory_st; tornad_token=${fresh}` }),
  );
  assert.deepEqual(kept, { token: fresh });
  assert.equal(await a.accessToken(new Headers()), null);
});

test("an unknown route is 404 and the social sign-in is 501", async () => {
  const a = auth(kratosStub().fetchImpl);
  assert.equal((await a.handler(post("nope", {}))).status, 404);
  assert.equal((await a.handler(post("sign-in/social", {}))).status, 501);
});
