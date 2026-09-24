import { test } from "node:test";
import assert from "node:assert/strict";
import { coreRefusal, createUrbangateAuth } from "./server.ts";
import type { UrbangateAuthConfig } from "./server.ts";
import { resetProvisioningTokenCache } from "../identity-provisioning.ts";
import { signer, unsigned } from "./test-keys.ts";

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

const hydra = await signer();

function jwt(payload: Record<string, unknown>) {
  return hydra.sign({
    iss: "https://id.urbangate.dev",
    aud: ["https://id.urbangate.dev/oauth2/token", "tornad"],
    ...payload,
  });
}

function parseBody(body: RequestInit["body"]) {
  if (typeof body !== "string") return body;
  try {
    return JSON.parse(body);
  } catch {
    return body;
  }
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
      body: parseBody(init.body),
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
    if (key === "GET /.well-known/jwks.json")
      return Response.json({ keys: [hydra.jwk] });
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
  const token = await jwt({
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
  const fresh = await jwt({
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

function deletionStub(deletions: () => Response) {
  resetProvisioningTokenCache();
  return kratosStub({
    "POST /oauth2/token": () =>
      Response.json({ access_token: "machine", expires_in: 900 }),
    "POST /api/v1/machine/accounts/deletions": deletions,
  });
}

function deletionAuth(
  fetchImpl: typeof fetch,
  steps: { cancelBilling?: () => Promise<void>; deleteData: () => Promise<void> },
) {
  return createUrbangateAuth({
    product: "tornad",
    kratosUrl: "http://kratos:4433",
    urbangate: {
      issuerUrl: "https://id.urbangate.dev",
      provisioner: { clientId: "tornad-provisioner", clientSecret: "p" },
      admin: { clientId: "tornad-admin", clientSecret: "a" },
    },
    fetch: fetchImpl,
    accountDeletion: () => steps,
  });
}

const signedIn = async () =>
  `tornad_session=ory_st; tornad_token=${await jwt({
    sub: "8f3a",
    roles: ["tornad:user"],
    exp: Math.floor(Date.now() / 1000) + 900,
  })}`;

test("delete-account runs billing then data, drops the role, and signs out", async () => {
  const { fetchImpl, calls } = deletionStub(() =>
    Response.json({ event_id: "e1" }, { status: 202 }),
  );
  const order: Array<string> = [];
  const res = await deletionAuth(fetchImpl, {
    cancelBilling: async () => void order.push("billing"),
    deleteData: async () => void order.push("data"),
  }).handler(post("delete-account", {}, await signedIn()));

  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), {
    deleted: true,
    steps: [
      { id: "billing", status: "done" },
      { id: "data", status: "done" },
    ],
  });
  assert.deepEqual(order, ["billing", "data"]);
  const drop = calls.find(
    (c) => c.key === "POST /api/v1/machine/accounts/deletions",
  );
  assert.deepEqual(drop?.body, { identity_id: "8f3a" });
  assert.ok(calls.some((c) => c.key === "DELETE /self-service/logout/api"));
  assert.equal(
    res.headers.getSetCookie().filter((c) => c.includes("Max-Age=0")).length,
    3,
  );
});

test("a failed billing step stops before the data and keeps the session", async () => {
  const { fetchImpl, calls } = deletionStub(() =>
    Response.json({ event_id: "e1" }, { status: 202 }),
  );
  let purged = false;
  const res = await deletionAuth(fetchImpl, {
    cancelBilling: async () => {
      throw new Error("lungor down");
    },
    deleteData: async () => {
      purged = true;
    },
  }).handler(post("delete-account", {}, await signedIn()));

  assert.equal(res.status, 502);
  assert.deepEqual(await res.json(), {
    deleted: false,
    steps: [
      { id: "billing", status: "failed" },
      { id: "data", status: "pending" },
    ],
  });
  assert.equal(purged, false);
  assert.ok(!calls.some((c) => c.key === "DELETE /self-service/logout/api"));
});

test("urbangate unreachable fails the data step so the person retries", async () => {
  const { fetchImpl } = deletionStub(
    () => new Response("", { status: 503 }),
  );
  const res = await deletionAuth(fetchImpl, {
    deleteData: async () => {},
  }).handler(post("delete-account", {}, await signedIn()));

  assert.equal(res.status, 502);
  assert.deepEqual(await res.json(), {
    deleted: false,
    steps: [{ id: "data", status: "failed" }],
  });
});

test("delete-account needs a session, and is 501 when the product wired none", async () => {
  const { fetchImpl } = deletionStub(() => Response.json({}));
  const unsigned = await deletionAuth(fetchImpl, {
    deleteData: async () => {},
  }).handler(post("delete-account", {}));
  assert.equal(unsigned.status, 401);
  const unwired = await auth(fetchImpl).handler(
    post("delete-account", {}, await signedIn()),
  );
  assert.equal(unwired.status, 501);
});

const signedInCookie = "tornad_session=ory_st";

test("update-user submits the profile with the other traits kept", async () => {
  const { fetchImpl, calls } = kratosStub({
    "GET /self-service/settings/api": () => Response.json({ id: "S" }),
    "POST /self-service/settings?flow=S": () => Response.json({ id: "S" }),
  });
  const res = await auth(fetchImpl).handler(
    post("update-user", { name: " Ana B " }, signedInCookie),
  );
  assert.equal(res.status, 200);
  const submitted = calls.find(
    (c) => c.key === "POST /self-service/settings?flow=S",
  );
  assert.deepEqual(submitted?.body, {
    method: "profile",
    traits: { email: "ana@example", name: "Ana B" },
    transient_payload: { product: "tornad", product_name: "Tornad" },
  });
  assert.equal(submitted?.headers.get("x-session-token"), "ory_st");
});

test("update-user needs a session and a name", async () => {
  const { fetchImpl } = kratosStub();
  const a = auth(fetchImpl);
  assert.equal((await a.handler(post("update-user", { name: "Ana" }))).status, 401);
  assert.equal(
    (await a.handler(post("update-user", { name: " " }, signedInCookie))).status,
    400,
  );
});

test("change-password re-proves the current one, sets the new one, and revokes the others", async () => {
  const { fetchImpl, calls } = kratosStub({
    "GET /self-service/login/api?refresh=true": () => Response.json({ id: "L" }),
    "GET /self-service/settings/api": () => Response.json({ id: "S" }),
    "POST /self-service/settings?flow=S": () => Response.json({ id: "S" }),
    "DELETE /sessions": () => Response.json({ count: 2 }),
  });
  const res = await auth(fetchImpl).handler(
    post(
      "change-password",
      { currentPassword: "old", newPassword: "new-pass1", revokeOtherSessions: true },
      signedInCookie,
    ),
  );
  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), { status: true, othersRevoked: true });
  const keys = calls.map((c) => c.key);
  assert.deepEqual(
    keys.filter((k) => k !== "GET /sessions/whoami"),
    [
      "GET /self-service/login/api?refresh=true",
      "POST /self-service/login?flow=L",
      "GET /self-service/settings/api",
      "POST /self-service/settings?flow=S",
      "DELETE /sessions",
    ],
  );
  const login = calls.find((c) => c.key === "POST /self-service/login?flow=L");
  assert.deepEqual(login?.body, {
    method: "password",
    identifier: "ana@example",
    password: "old",
    transient_payload: { product: "tornad", product_name: "Tornad" },
  });
  const settings = calls.find((c) => c.key === "POST /self-service/settings?flow=S");
  assert.equal((settings?.body as { password: string }).password, "new-pass1");
});

test("a wrong current password is 401 and the password is left alone", async () => {
  const { fetchImpl, calls } = kratosStub({
    "GET /self-service/login/api?refresh=true": () => Response.json({ id: "L" }),
    "POST /self-service/login?flow=L": () =>
      Response.json(
        { ui: { messages: [{ id: 4000006, type: "error" }] } },
        { status: 400 },
      ),
  });
  const res = await auth(fetchImpl).handler(
    post("change-password", { currentPassword: "bad", newPassword: "new-pass1" }, signedInCookie),
  );
  assert.equal(res.status, 401);
  assert.ok(!calls.some((c) => c.key.startsWith("GET /self-service/settings")));
});

test("change-password needs a session and both passwords", async () => {
  const { fetchImpl } = kratosStub();
  const a = auth(fetchImpl);
  assert.equal(
    (await a.handler(post("change-password", { currentPassword: "a", newPassword: "b" }))).status,
    401,
  );
  assert.equal(
    (await a.handler(post("change-password", { currentPassword: "a" }, signedInCookie))).status,
    400,
  );
});

test("an old session is asked to sign in again before a settings change", async () => {
  const { fetchImpl } = kratosStub({
    "GET /self-service/settings/api": () => Response.json({ id: "S" }),
    "POST /self-service/settings?flow=S": () =>
      Response.json(
        { error: { id: "session_refresh_required", code: 403 } },
        { status: 403 },
      ),
  });
  const res = await auth(fetchImpl).handler(
    post("update-user", { name: "Ana" }, "tornad_session=ory_st"),
  );
  assert.equal(res.status, 403);
  assert.deepEqual(await res.json(), {
    error: { code: "session_refresh_required", status: 403 },
  });
});

async function exchangeStub(roles: Array<string>) {
  const fresh = await jwt({
    sub: "8f3a",
    roles,
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const stub = kratosStub({
    "POST /oauth2/token": (init) => {
      const form = init.body as URLSearchParams;
      return form.get("grant_type") === "client_credentials"
        ? Response.json({ access_token: "m", expires_in: 3600 })
        : Response.json({ access_token: fresh, expires_in: 900 });
    },
    "POST /api/machine/sessions/exchange": () =>
      Response.json({ assertion: "a.b.c", identity_id: "8f3a", roles }),
  });
  return { ...stub, fresh };
}

test("get-session exchanges an expired token so the role is the current one, and keeps the new token", async () => {
  resetProvisioningTokenCache();
  const { fetchImpl } = await exchangeStub(["tornad:admin"]);
  const expired = await jwt({
    sub: "8f3a",
    roles: [],
    exp: Math.floor(Date.now() / 1000) - 60,
  });
  const res = await auth(fetchImpl).handler(
    new Request("https://tornad.dev/api/auth/get-session", {
      headers: { cookie: `tornad_session=ory_st; tornad_token=${expired}` },
    }),
  );
  const body = (await res.json()) as { user: { role: string } };
  assert.equal(body.user.role, "admin");
  assert.match(res.headers.getSetCookie().join(";"), /tornad_token=/);
});

test("get-session reads no role while urbangate cannot be reached", async () => {
  resetProvisioningTokenCache();
  const { fetchImpl } = kratosStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const s = await auth(fetchImpl).getSession(
    new Headers({ cookie: "tornad_session=ory_st" }),
  );
  assert.equal(s?.user.role, "user");
  assert.equal(s?.user.identityId, "8f3a");
});

async function throughProxy(
  proxy: (request: Request) => Promise<Response>,
  request: Request,
) {
  const seen: Array<Headers> = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (_url: URL | string, init: RequestInit = {}) => {
    seen.push(new Headers(init.headers));
    return Response.json({ ok: true });
  }) as typeof fetch;
  try {
    return { res: await proxy(request), seen };
  } finally {
    globalThis.fetch = original;
  }
}

test("coreProxy refuses a request without a session by default", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = auth(fetchImpl).coreProxy({ coreUrl: "http://core:8080" });
  const { res, seen } = await throughProxy(
    proxy,
    new Request("https://tornad.dev/api/v1/me"),
  );
  assert.equal(res.status, 401);
  assert.equal(seen.length, 0);
});

test("coreProxy forwards an anonymous request as it came when asked to", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = auth(fetchImpl).coreProxy({
    coreUrl: "http://core:8080",
    anonymous: "forward",
  });
  const { res, seen } = await throughProxy(
    proxy,
    new Request("https://tornad.dev/api/v1/invitations/claim", {
      headers: { cookie: "other=1" },
    }),
  );
  assert.equal(res.status, 200);
  assert.equal(seen.length, 1);
  assert.equal(seen[0].get("authorization"), null);
  assert.equal(seen[0].get("cookie"), null);
});

test("coreProxy keeps a caller's own credential when forwarding anonymous requests", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = auth(fetchImpl).coreProxy({
    coreUrl: "http://core:8080",
    anonymous: "forward",
  });
  const { seen } = await throughProxy(
    proxy,
    new Request("https://tornad.dev/api/v1/customers", {
      headers: { authorization: "Bearer app-key" },
    }),
  );
  assert.equal(seen[0].get("authorization"), "Bearer app-key");
});

test("coreProxy attaches the person's token when there is a session, in either mode", async () => {
  resetProvisioningTokenCache();
  const { fetchImpl, fresh } = await exchangeStub(["tornad:user"]);
  const proxy = auth(fetchImpl).coreProxy({
    coreUrl: "http://core:8080",
    anonymous: "forward",
  });
  const { res, seen } = await throughProxy(
    proxy,
    new Request("https://tornad.dev/api/v1/me", {
      headers: { cookie: "tornad_session=ory_st" },
    }),
  );
  assert.equal(seen[0].get("authorization"), `Bearer ${fresh}`);
  assert.match(res.headers.getSetCookie().join(";"), /tornad_token=/);
});

test("coreProxy still turns a signed-in non-admin away from an admin-only core", async () => {
  resetProvisioningTokenCache();
  const { fetchImpl } = await exchangeStub(["tornad:user"]);
  const proxy = auth(fetchImpl).coreProxy({
    coreUrl: "http://core:8080",
    adminOnly: true,
    anonymous: "forward",
  });
  const { res, seen } = await throughProxy(
    proxy,
    new Request("https://tornad.dev/api/v1/me", {
      headers: { cookie: "tornad_session=ory_st" },
    }),
  );
  assert.equal(res.status, 403);
  assert.equal(seen.length, 0);
});

test("coreProxy answers 502 when the core cannot be reached", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = auth(fetchImpl).coreProxy({
    coreUrl: "http://core:8080",
    anonymous: "forward",
  });
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    throw new TypeError("fetch failed");
  }) as typeof fetch;
  try {
    const res = await proxy(new Request("https://tornad.dev/api/v1/plans"));
    assert.equal(res.status, 502);
  } finally {
    globalThis.fetch = original;
  }
});

test("a token cookie Hydra did not sign is never read: it is exchanged again", async () => {
  const stranger = await signer();
  const claims = {
    iss: "https://id.urbangate.dev",
    aud: ["https://id.urbangate.dev/oauth2/token", "tornad"],
    sub: "8f3a",
    roles: ["tornad:admin"],
    exp: Math.floor(Date.now() / 1000) + 900,
  };
  const forged = [
    await stranger.sign(claims),
    unsigned("none", claims),
    unsigned("HS256", claims),
    await jwt({ ...claims, aud: ["other-product"] }),
  ];
  for (const token of forged) {
    resetProvisioningTokenCache();
    const { fetchImpl, calls } = await exchangeStub(["tornad:user"]);
    const res = await auth(fetchImpl).handler(
      new Request("https://tornad.dev/api/auth/get-session", {
        headers: { cookie: `tornad_session=ory_st; tornad_token=${token}` },
      }),
    );
    const body = (await res.json()) as { user: { role: string } };
    assert.equal(body.user.role, "user");
    assert.ok(calls.some((c) => c.key === "POST /api/machine/sessions/exchange"));
  }
});

test("a forged admin token cookie reads as user while urbangate cannot be reached", async () => {
  resetProvisioningTokenCache();
  const stranger = await signer();
  const forged = await stranger.sign({
    iss: "https://id.urbangate.dev",
    aud: ["tornad"],
    sub: "8f3a",
    roles: ["tornad:admin"],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const { fetchImpl } = kratosStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
    "GET /.well-known/jwks.json": () => new Response("", { status: 503 }),
  });
  const s = await auth(fetchImpl).getSession(
    new Headers({ cookie: `tornad_session=ory_st; tornad_token=${forged}` }),
  );
  assert.equal(s?.user.role, "user");
});

test("someone else's valid token cookie beside my session is exchanged, not read", async () => {
  resetProvisioningTokenCache();
  const theirs = await jwt({
    sub: "someone-else",
    roles: ["tornad:admin"],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const { fetchImpl, calls } = await exchangeStub(["tornad:user"]);
  const res = await auth(fetchImpl).handler(
    new Request("https://tornad.dev/api/auth/get-session", {
      headers: { cookie: `tornad_session=ory_st; tornad_token=${theirs}` },
    }),
  );
  const body = (await res.json()) as { user: { role: string; identityId: string } };
  assert.equal(body.user.identityId, "8f3a");
  assert.equal(body.user.role, "user");
  assert.ok(calls.some((c) => c.key === "POST /api/machine/sessions/exchange"));
});

function authWith(
  fetchImpl: typeof fetch,
  extra: Partial<UrbangateAuthConfig> = {},
) {
  return createUrbangateAuth({
    product: "tornad",
    kratosUrl: "http://kratos:4433",
    urbangate: {
      issuerUrl: "https://id.urbangate.dev",
      provisioner: { clientId: "tornad-provisioner", clientSecret: "p" },
      admin: { clientId: "tornad-admin", clientSecret: "a" },
    },
    coreUrl: "http://core:4100",
    fetch: fetchImpl,
    ...extra,
  });
}

async function withCore(
  answer: (url: string, init: RequestInit) => Response,
  run: () => Promise<unknown>,
) {
  const seen: Array<{ url: string; headers: Headers }> = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (url: URL | string, init: RequestInit = {}) => {
    seen.push({ url: String(url), headers: new Headers(init.headers) });
    return answer(String(url), init);
  }) as typeof fetch;
  try {
    return { result: await run(), seen };
  } finally {
    globalThis.fetch = original;
  }
}

const signedInAs = async (roles: Array<string>) =>
  `tornad_session=ory_st; tornad_token=${await jwt({
    sub: "8f3a",
    roles,
    exp: Math.floor(Date.now() / 1000) + 900,
  })}`;

test("coreProxy strips its prefix and reaches the auth's own coreUrl", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = authWith(fetchImpl).coreProxy({ stripPrefix: "/api/core" });
  const cookie = await signedInAs(["tornad:user"]);
  const { seen } = await withCore(
    () => Response.json({ ok: true }),
    () =>
      proxy(
        new Request("https://tornad.dev/api/core/chat/send?x=1", {
          headers: { cookie },
        }),
      ),
  );
  assert.equal(seen[0].url, "http://core:4100/chat/send?x=1");
});

test("coreProxy forwards only the cookies the core reads itself", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = authWith(fetchImpl).coreProxy({
    forwardCookies: ["txl_trial"],
  });
  const cookie = `${await signedInAs(["tornad:user"])}; txl_trial=trial-xyz; other=1`;
  const { seen } = await withCore(
    () => Response.json({ ok: true }),
    () =>
      proxy(
        new Request("https://tornad.dev/api/v1/me", { headers: { cookie } }),
      ),
  );
  assert.equal(seen[0].headers.get("cookie"), "txl_trial=trial-xyz");
  assert.match(seen[0].headers.get("authorization") ?? "", /^Bearer /);
});

test("coreProxy relays an event stream unbuffered, every Set-Cookie, and no stale encoding", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = authWith(fetchImpl).coreProxy();
  const cookie = await signedInAs(["tornad:user"]);
  const { result } = await withCore(
    () => {
      const h = new Headers({
        "content-type": "text/event-stream",
        "content-encoding": "gzip",
      });
      h.append("set-cookie", "a=1; Path=/");
      h.append("set-cookie", "b=2; Path=/");
      return new Response("data: hi\n\n", { headers: h });
    },
    () =>
      proxy(
        new Request("https://tornad.dev/api/v1/stream", {
          headers: { cookie },
        }),
      ),
  );
  const res = result as Response;
  assert.equal(res.headers.get("x-accel-buffering"), "no");
  assert.equal(res.headers.get("content-encoding"), null);
  assert.deepEqual(res.headers.getSetCookie(), ["a=1; Path=/", "b=2; Path=/"]);
});

test("coreProxy's adminOnly is a 503, not a 403, while the roles cannot be read", async () => {
  resetProvisioningTokenCache();
  const { fetchImpl } = kratosStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const res = await authWith(fetchImpl).coreProxy({ adminOnly: true })(
    new Request("https://tornad.dev/api/v1/admin", {
      headers: { cookie: "tornad_session=ory_st" },
    }),
  );
  assert.equal(res.status, 503);
});

test("coreFetch calls the core as the person and tells a refusal from an outage", async () => {
  const { fetchImpl } = kratosStub();
  const a = authWith(fetchImpl);
  const cookie = await signedInAs(["tornad:user"]);
  const { result, seen } = await withCore(
    () => Response.json({ tenantId: "t1" }),
    () => a.coreFetch(new Headers({ cookie }), "/me"),
  );
  const call = result as Awaited<ReturnType<typeof a.coreFetch>>;
  assert.equal(call.status, "ok");
  assert.equal(seen[0].url, "http://core:4100/me");
  assert.match(seen[0].headers.get("authorization") ?? "", /^Bearer /);

  const out = await a.coreFetch(new Headers(), "/me");
  assert.deepEqual(out, { status: "signed_out" });
  assert.equal(coreRefusal(out as never).status, 401);

  const down = await withCore(
    () => {
      throw new Error("refused");
    },
    () => a.coreFetch(new Headers({ cookie }), "/me"),
  );
  assert.deepEqual(down.result, { status: "unavailable", cause: "core" });
  assert.equal(coreRefusal(down.result as never).status, 502);

  resetProvisioningTokenCache();
  const idpDown = kratosStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const noIdp = await authWith(idpDown.fetchImpl).coreFetch(
    new Headers({ cookie: "tornad_session=ory_st" }),
    "/me",
  );
  assert.deepEqual(noIdp, {
    status: "unavailable",
    cause: "identity_provider",
  });
  assert.equal(coreRefusal(noIdp as never).status, 503);
});

test("requireAdmin: admin passes, user is 403, unknown roles are 503, signed out is 401", async () => {
  const { fetchImpl } = kratosStub();
  const a = authWith(fetchImpl);
  const ok = await a.requireAdmin(
    new Headers({ cookie: await signedInAs(["tornad:admin"]) }),
  );
  assert.ok("session" in ok && ok.session.user.role === "admin");
  const user = await a.requireAdmin(
    new Headers({ cookie: await signedInAs(["tornad:user"]) }),
  );
  assert.equal("response" in user && user.response.status, 403);
  const session = await a.requireSession(
    new Headers({ cookie: await signedInAs(["tornad:user"]) }),
  );
  assert.ok("session" in session);
  const none = await a.requireAdmin(new Headers());
  assert.equal("response" in none && none.response.status, 401);

  resetProvisioningTokenCache();
  const down = kratosStub({
    "POST /oauth2/token": () => new Response("", { status: 503 }),
  });
  const unknown = await authWith(down.fetchImpl).requireAdmin(
    new Headers({ cookie: "tornad_session=ory_st" }),
  );
  assert.equal("response" in unknown && unknown.response.status, 503);

  const kratosDown = kratosStub({
    "GET /sessions/whoami": () => new Response("", { status: 502 }),
  });
  const noKratos = await authWith(kratosDown.fetchImpl).requireSession(
    new Headers({ cookie: "tornad_session=ory_st" }),
  );
  assert.equal("response" in noKratos && noKratos.response.status, 503);
});

test("onAccountOpened waits for the address: not at a password sign-up, at its verification", async () => {
  const { fetchImpl } = kratosStub();
  const opened: Array<{ email: string; verified: boolean; cookie: string | null }> = [];
  const a = authWith(fetchImpl, {
    onAccountOpened: ({ user, headers }) => {
      opened.push({
        email: user.email,
        verified: user.emailVerified,
        cookie: headers.get("cookie"),
      });
      return ["tornad_invite=; Max-Age=0; Path=/"];
    },
  });
  const signUp = await a.handler(
    post("sign-up/email", { email: "ana@example", password: "pw" }),
  );
  assert.equal(signUp.status, 200);
  assert.equal(opened.length, 0);
  const flow = signUp.headers
    .getSetCookie()
    .filter((c) => c.startsWith("tornad_flow=") && !c.includes("Max-Age=0"))
    .at(-1)
    ?.split(";")[0];
  const verified = await a.handler(
    post("email-otp/verify-email", { email: "ana@example", otp: "123456" }, `tornad_session=ory_st; ${flow}`),
  );
  assert.equal(verified.status, 200);
  assert.deepEqual(opened, [
    { email: "ana@example", verified: true, cookie: "tornad_session=ory_st" },
  ]);
  assert.ok(verified.headers.getSetCookie().some((c) => c.startsWith("tornad_invite=")));
});

test("verifying an address later, outside a sign-up, opens no account", async () => {
  let calls = 0;
  const { fetchImpl } = kratosStub();
  await authWith(fetchImpl, { onAccountOpened: () => void calls++ }).handler(
    post("email-otp/verify-email", { email: "ana@example", otp: "123456" }, "tornad_session=ory_st; tornad_flow=verification%3AV"),
  );
  assert.equal(calls, 0);
});

test("a code sent again during a sign-up keeps the sign-up's mark", async () => {
  const { fetchImpl } = kratosStub({
    "POST /self-service/verification?flow=V": () =>
      Response.json({ id: "V", state: "sent_email" }),
  });
  const res = await authWith(fetchImpl).handler(
    post("email-otp/send-verification-otp", { email: "ana@example", type: "email-verification" }, "tornad_flow=verification%3AV0%3Aopened"),
  );
  assert.ok(res.headers.getSetCookie().some((c) => c.startsWith("tornad_flow=verification%3AV%3Aopened")));
});

test("onAccountOpened runs for a code that signs an address up, not for a sign-in", async () => {
  let calls = 0;
  const hook = { onAccountOpened: () => void calls++ };
  const login = kratosStub({
    "POST /self-service/login?flow=L": () =>
      Response.json({ session_token: "ory_st", session }),
  });
  await authWith(login.fetchImpl, hook).handler(
    post(
      "sign-in/email-otp",
      { email: "ana@example", otp: "123456" },
      "tornad_flow=login%3AL",
    ),
  );
  assert.equal(calls, 0);
  const registration = kratosStub({
    "POST /self-service/registration?flow=R": () =>
      Response.json({ session_token: "ory_st", session }),
  });
  await authWith(registration.fetchImpl, hook).handler(
    post(
      "sign-in/email-otp",
      { email: "ana@example", otp: "123456" },
      "tornad_flow=registration%3AR",
    ),
  );
  assert.equal(calls, 1);
});

test("a failing onAccountOpened never fails the sign-up", async () => {
  const { fetchImpl } = kratosStub();
  const warn = console.warn;
  console.warn = () => {};
  try {
    const res = await authWith(fetchImpl, {
      onAccountOpened: () => {
        throw new Error("lungor down");
      },
    }).handler(post("sign-up/email", { email: "ana@example", password: "pw" }));
    assert.equal(res.status, 200);
    assert.ok(
      res.headers
        .getSetCookie()
        .some((c) => c.startsWith("tornad_session=ory_st")),
    );
  } finally {
    console.warn = warn;
  }
});

test("coreProxy never lets the path choose the host", async () => {
  const { fetchImpl } = kratosStub();
  const proxy = authWith(fetchImpl, { coreUrl: "http://lungor-core" }).coreProxy({
    stripPrefix: "/api/core",
  });
  const cookie = await signedInAs(["tornad:user"]);
  for (const path of ["/api/core.evil.example/x", "/api/coreX", "/api/core//evil.example/x"]) {
    const { result, seen } = await withCore(
      () => Response.json({ ok: true }),
      () => proxy(new Request(`https://tornad.dev${path}`, { headers: { cookie } })),
    );
    assert.equal((result as Response).status, 404, path);
    assert.equal(seen.length, 0, path);
  }
  const plain = authWith(fetchImpl, { coreUrl: "http://lungor-core" }).coreProxy();
  const { result, seen } = await withCore(
    () => Response.json({ ok: true }),
    () => plain(new Request("https://tornad.dev//evil.example/x", { headers: { cookie } })),
  );
  assert.equal((result as Response).status, 404);
  assert.equal(seen.length, 0);
});

test("coreFetch refuses a path that would leave the core", async () => {
  const { fetchImpl } = kratosStub();
  const a = authWith(fetchImpl, { coreUrl: "http://lungor-core" });
  await assert.rejects(a.coreFetch(new Headers(), ".evil.example/x"));
  await assert.rejects(a.coreFetch(new Headers(), "//evil.example/x"));
});

test("forwardCookies may not name the auth's own cookies", () => {
  const { fetchImpl } = kratosStub();
  assert.throws(() => authWith(fetchImpl).coreProxy({ forwardCookies: ["tornad_session"] }));
  assert.throws(() => authWith(fetchImpl).coreProxy({ forwardCookies: ["tornad_admin"] }));
});
