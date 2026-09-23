import { test } from "node:test";
import assert from "node:assert/strict";
import { Exchange, decodeToken } from "./exchange.ts";

const config = {
  product: "tornad",
  issuerUrl: "https://id.urbangate.dev",
  provisioner: { clientId: "tornad-provisioner", clientSecret: "p" },
  admin: { clientId: "tornad-admin", clientSecret: "a" },
};

function jwt(payload: Record<string, unknown>) {
  return (
    "h." + Buffer.from(JSON.stringify(payload)).toString("base64url") + ".s"
  );
}

function stub(answers: Record<string, (init: RequestInit) => Response>) {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  const fetchImpl = (async (url: URL | string, init: RequestInit = {}) => {
    const path = new URL(String(url)).pathname;
    calls.push({ path, init });
    const grant =
      init.body instanceof URLSearchParams ? init.body.get("grant_type") : "";
    const key = path === "/oauth2/token" ? `${path}:${grant}` : path;
    return answers[key]?.(init) ?? new Response("{}", { status: 500 });
  }) as typeof fetch;
  return { fetchImpl, calls };
}

test("an exchange takes three calls and yields the person's token", async () => {
  const access = jwt({
    sub: "8f3a",
    roles: ["tornad:user"],
    exp: Math.floor(Date.now() / 1000) + 900,
  });
  const { fetchImpl, calls } = stub({
    "/oauth2/token:client_credentials": () =>
      Response.json({ access_token: "machine", expires_in: 3600 }),
    "/api/machine/sessions/exchange": (init) => {
      assert.equal(
        new Headers(init.headers).get("authorization"),
        "Bearer machine",
      );
      return Response.json({
        assertion: "a.b.c",
        identity_id: "8f3a",
        roles: ["tornad:user"],
      });
    },
    "/oauth2/token:urn:ietf:params:oauth:grant-type:jwt-bearer": (init) => {
      const form = init.body as URLSearchParams;
      assert.equal(form.get("assertion"), "a.b.c");
      assert.equal(form.get("audience"), "tornad");
      assert.equal(form.get("client_id"), "tornad-admin");
      assert.equal(form.get("client_secret"), "a");
      assert.equal(new Headers(init.headers).get("authorization"), null);
      return Response.json({ access_token: access, expires_in: 900 });
    },
  });
  const exchange = new Exchange(config, fetchImpl);
  const outcome = await exchange.exchange("ory_st");
  assert.equal(outcome.status, "ok");
  if (outcome.status !== "ok") return;
  assert.equal(outcome.token.accessToken, access);
  assert.deepEqual(outcome.token.roles, ["tornad:user"]);
  await exchange.exchange("ory_st");
  assert.equal(
    calls.filter(
      (c) =>
        c.path === "/oauth2/token" &&
        (c.init.body as URLSearchParams).get("grant_type") ===
          "client_credentials",
    ).length,
    1,
  );
});

test("a session urbangate no longer knows, an inactive identity, and an outage are told apart", async () => {
  const machine = {
    "/oauth2/token:client_credentials": () =>
      Response.json({ access_token: "m", expires_in: 3600 }),
  };
  const gone = new Exchange(
    config,
    stub({
      ...machine,
      "/api/machine/sessions/exchange": () =>
        Response.json({ error: "session" }, { status: 401 }),
    }).fetchImpl,
  );
  assert.deepEqual(await gone.exchange("t"), { status: "session_gone" });
  const inactive = new Exchange(
    config,
    stub({
      ...machine,
      "/api/machine/sessions/exchange": () =>
        Response.json({ error: "inactive" }, { status: 403 }),
    }).fetchImpl,
  );
  assert.deepEqual(await inactive.exchange("t"), { status: "inactive" });
  const down = new Exchange(
    config,
    stub({
      ...machine,
      "/api/machine/sessions/exchange": () =>
        Response.json({ error: "unavailable" }, { status: 503 }),
    }).fetchImpl,
  );
  assert.equal((await down.exchange("t")).status, "unavailable");
});

test("needsRefresh and decodeToken agree on the token's end", () => {
  const exp = Math.floor(Date.now() / 1000) + 30;
  const decoded = decodeToken(jwt({ sub: "8f3a", exp }));
  assert.ok(decoded);
  assert.equal(
    new Exchange(config).needsRefresh({ accessToken: "x", ...decoded! }),
    true,
  );
  assert.equal(new Exchange(config).needsRefresh(null), true);
  assert.equal(decodeToken("not-a-jwt"), null);
});
