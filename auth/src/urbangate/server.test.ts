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
    productName: "Tornad",
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
    transient_payload: { product: "tornad", product_name: "Tornad" },
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
      transient_payload: { product: "tornad", product_name: "Tornad" },
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
    {
      method: "code",
      code: "123456",
      transient_payload: { product: "tornad", product_name: "Tornad" },
    },
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

test("a code for an unknown address becomes a sign-up by code", async () => {
  const { fetchImpl, calls } = kratosStub({
    "POST /self-service/login?flow=L": () =>
      Response.json(
        {
          id: "L",
          ui: {
            messages: [
              {
                id: 4000035,
                type: "error",
                text: "This account does not exist or has not setup sign in with code.",
              },
            ],
          },
        },
        { status: 400 },
      ),
    "POST /self-service/registration?flow=R": (init) => {
      const b = JSON.parse(init.body as string) as { code?: string };
      if (!b.code) return Response.json({ id: "R", state: "sent_email" });
      return Response.json({ session_token: "ory_st", session });
    },
  });
  const a = auth(fetchImpl);
  const sent = await a.handler(
    post("email-otp/send-verification-otp", {
      email: "ana@example",
      type: "sign-in",
    }),
  );
  assert.equal(sent.status, 200);
  const flow = sent.headers.get("set-cookie") ?? "";
  assert.match(flow, /tornad_flow=registration%3AR/);
  const signedIn = await a.handler(
    post(
      "sign-in/email-otp",
      { email: "ana@example", otp: "123456" },
      flow.split(";")[0],
    ),
  );
  assert.equal(signedIn.status, 200);
  assert.match(
    signedIn.headers.get("set-cookie") ?? "",
    /tornad_session=ory_st/,
  );
  assert.ok(
    calls.some((c) => c.key === "POST /self-service/registration?flow=R"),
  );
});

test("every submit carries the product for urbangate's mail templates", async () => {
  const { fetchImpl, calls } = kratosStub({
    "POST /self-service/login?flow=L": (init) => {
      const b = JSON.parse(init.body as string) as { code?: string };
      return b.code
        ? Response.json({ session_token: "ory_st", session })
        : Response.json({ id: "L", state: "sent_email" });
    },
  });
  const a = auth(fetchImpl);
  const sent = await a.handler(
    post("email-otp/send-verification-otp", {
      email: "ana@example",
      type: "sign-in",
    }),
  );
  const flow = (sent.headers.get("set-cookie") ?? "").split(";")[0];
  await a.handler(
    post("sign-in/email-otp", { email: "ana@example", otp: "123456" }, flow),
  );
  await a.handler(
    post("sign-in/email", { email: "ana@example", password: "pw" }),
  );
  const submits = calls.filter((c) => c.key.startsWith("POST /self-service/"));
  assert.ok(submits.length >= 3);
  for (const c of submits) {
    assert.deepEqual(
      (c.body as { transient_payload?: unknown }).transient_payload,
      { product: "tornad", product_name: "Tornad" },
      c.key,
    );
  }
});

function recoveryStub(settings: (init: RequestInit) => Response) {
  return kratosStub({
    "POST /self-service/recovery?flow=RC": () =>
      Response.json(
        {
          id: "RC",
          continue_with: [
            { action: "set_ory_session_token", ory_session_token: "ory_st" },
            { action: "show_settings_ui", flow: { id: "S" } },
          ],
        },
        { status: 422 },
      ),
    "POST /self-service/settings?flow=S": settings,
  });
}

const aal2Refusal = () =>
  Response.json(
    { error: { id: "session_aal2_required", code: 403 } },
    { status: 403 },
  );

test("a reset refused for a second factor keeps the session and the settings flow", async () => {
  const { fetchImpl } = recoveryStub(aal2Refusal);
  const res = await auth(fetchImpl).handler(
    post(
      "email-otp/reset-password",
      { email: "ana@example", otp: "123456", password: "a-new-password" },
      "tornad_flow=recovery%3ARC",
    ),
  );
  assert.equal(res.status, 403);
  assert.equal(
    ((await res.json()) as { error: { code: string } }).error.code,
    "second_factor_required",
  );
  const cookies = res.headers.getSetCookie();
  assert.ok(cookies.some((c) => c.startsWith("tornad_session=ory_st")));
  assert.ok(cookies.some((c) => c.startsWith("tornad_flow=settings2fa%3AS")));
});

test("the second factor steps the session up, then sets the password", async () => {
  let stepped = false;
  const { fetchImpl, calls } = recoveryStub(() =>
    stepped ? Response.json({ id: "S", state: "success" }) : aal2Refusal(),
  );
  const withLogin = (async (url: URL | string, init: RequestInit = {}) => {
    const u = new URL(String(url));
    if (u.pathname === "/self-service/login/api" && u.searchParams.get("aal"))
      return Response.json({ id: "L2" });
    if (
      u.pathname === "/self-service/login" &&
      u.searchParams.get("flow") === "L2"
    ) {
      const b = JSON.parse(init.body as string) as { totp_code?: string };
      if (b.totp_code !== "123456")
        return Response.json(
          { id: "L2", ui: { messages: [{ id: 4000008, type: "error" }] } },
          { status: 400 },
        );
      stepped = true;
      return Response.json({ session });
    }
    return fetchImpl(url, init);
  }) as typeof fetch;
  const a = auth(withLogin);
  const cookie = "tornad_session=ory_st; tornad_flow=settings2fa%3AS";
  const wrong = await a.handler(
    post("second-factor/verify", { code: "000000", password: "p" }, cookie),
  );
  assert.equal(wrong.status, 400);
  assert.equal(
    ((await wrong.json()) as { error: { code: string } }).error.code,
    "invalid_code",
  );
  const ok = await a.handler(
    post(
      "second-factor/verify",
      { code: "123 456", password: "a-new-password" },
      cookie,
    ),
  );
  assert.equal(ok.status, 200);
  const settings = calls.filter(
    (c) => c.key === "POST /self-service/settings?flow=S",
  );
  assert.equal(settings.at(-1)?.headers.get("x-session-token"), "ory_st");
  assert.equal(
    (settings.at(-1)?.body as { password?: string }).password,
    "a-new-password",
  );
});

test("a backup code goes through lookup_secret", async () => {
  const bodies: Array<unknown> = [];
  const f = (async (url: URL | string, init: RequestInit = {}) => {
    const u = new URL(String(url));
    if (u.pathname === "/self-service/login/api")
      return Response.json({ id: "L2" });
    if (u.pathname === "/self-service/login") {
      bodies.push(JSON.parse(init.body as string));
      return Response.json({ session });
    }
    return Response.json({ id: "S", state: "success" });
  }) as typeof fetch;
  const res = await auth(f).handler(
    post(
      "second-factor/verify",
      { code: "ab12cd34", password: "p" },
      "tornad_session=ory_st; tornad_flow=settings2fa%3AS",
    ),
  );
  assert.equal(res.status, 200);
  assert.deepEqual(bodies[0], {
    method: "lookup_secret",
    lookup_secret: "ab12cd34",
  });
});

test("a second sign-up with a known address answers already_registered", async () => {
  const { fetchImpl } = kratosStub({
    "POST /self-service/registration?flow=R": () =>
      Response.json(
        { id: "R", ui: { messages: [{ id: 4000007, type: "error" }] } },
        { status: 400 },
      ),
  });
  const res = await auth(fetchImpl).handler(
    post("sign-up/email", { email: "ana@example", password: "pw" }),
  );
  assert.equal(res.status, 409);
  assert.equal(
    ((await res.json()) as { error: { code: string } }).error.code,
    "already_registered",
  );
});

test("an admin is an admin from the sign-in on, before any call to the core", async () => {
  const admin = jwt({
    sub: "8f3a",
    roles: ["tornad:admin"],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const { fetchImpl } = kratosStub({
    "POST /oauth2/token": (init) => {
      const form = init.body as URLSearchParams;
      return form.get("grant_type") === "client_credentials"
        ? Response.json({ access_token: "m", expires_in: 3600 })
        : Response.json({ access_token: admin, expires_in: 900 });
    },
    "POST /api/machine/sessions/exchange": () =>
      Response.json({
        assertion: "a.b.c",
        identity_id: "8f3a",
        roles: ["tornad:admin"],
      }),
  });
  const a = auth(fetchImpl);
  const signedIn = await a.handler(
    post("sign-in/email", { email: "ana@example", password: "long-enough-pw" }),
  );
  const body = (await signedIn.json()) as { user: { role: string } };
  assert.equal(body.user.role, "admin");
  const cookies = signedIn.headers.getSetCookie().join("\n");
  assert.match(cookies, /tornad_session=ory_st/);
  assert.match(cookies, /tornad_token=/);

  const read = await a.handler(
    new Request("https://tornad.dev/api/auth/get-session", {
      headers: { cookie: "tornad_session=ory_st" },
    }),
  );
  const session = (await read.json()) as { user: { role: string } };
  assert.equal(session.user.role, "admin");
  assert.match(read.headers.get("set-cookie") ?? "", /^tornad_token=/);
  assert.equal(
    (await a.getSession(new Headers({ cookie: "tornad_session=ory_st" })))?.user
      .role,
    "admin",
  );
});
