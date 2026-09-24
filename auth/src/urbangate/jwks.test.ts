import { test } from "node:test";
import assert from "node:assert/strict";
import { Jwks } from "./jwks.ts";
import { signer } from "./test-keys.ts";

const issuer = "https://id.urbangate.dev";
const hydra = await signer();

function jwksStub(statuses: Array<number>) {
  let calls = 0;
  const fetchImpl = (async () => {
    calls++;
    const status = statuses.shift() ?? 200;
    return status === 200
      ? Response.json({ keys: [hydra.jwk] })
      : new Response("", { status });
  }) as typeof fetch;
  return { fetchImpl, calls: () => calls };
}

const claims = (extra: Record<string, unknown> = {}) => ({
  iss: issuer,
  aud: ["tornad"],
  sub: "8f3a",
  exp: Math.floor(Date.now() / 1000) + 900,
  ...extra,
});

test("a failed JWKS fetch is not retried inside the cooldown", async () => {
  const { fetchImpl, calls } = jwksStub([503, 200]);
  const jwks = new Jwks(issuer, "tornad", fetchImpl);
  const token = await hydra.sign(claims({ roles: ["tornad:user"] }));
  const t0 = Date.now();
  await assert.rejects(jwks.verify(token, t0));
  await assert.rejects(jwks.verify(token, t0 + 1_000));
  await assert.rejects(jwks.verify(token, t0 + 29_000));
  assert.equal(calls(), 1);
  assert.equal((await jwks.verify(token, t0 + 31_000))?.identityId, "8f3a");
  assert.equal(calls(), 2);
});

test("cached keys keep serving while the issuer is down", async () => {
  const { fetchImpl, calls } = jwksStub([200, 503]);
  const jwks = new Jwks(issuer, "tornad", fetchImpl);
  const token = await hydra.sign(claims());
  const t0 = Date.now();
  assert.ok(await jwks.verify(token, t0));
  const later = t0 + 11 * 60_000;
  assert.ok(await jwks.verify(token, later));
  assert.ok(await jwks.verify(token, later + 1_000));
  assert.equal(calls(), 2);
});

test("roles fall back to ext.roles, as svcauth reads them", async () => {
  const { fetchImpl } = jwksStub([200]);
  const jwks = new Jwks(issuer, "tornad", fetchImpl);
  const token = await hydra.sign(claims({ ext: { roles: ["tornad:admin"] } }));
  assert.deepEqual((await jwks.verify(token))?.roles, ["tornad:admin"]);
});
