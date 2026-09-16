import { test, beforeEach } from "node:test"
import assert from "node:assert/strict"
import {
  provisionIdentity,
  resetProvisioningTokenCache,
  type IdentityProvisioningConfig,
} from "./identity-provisioning.ts"

const CONFIG: IdentityProvisioningConfig = {
  issuer: "https://id.urbangate.dev",
  clientId: "spore-provisioner",
  clientSecret: "secret",
  role: "spore:user",
  product: "spore",
}

function stub(
  identityResponse: { status: number; body: unknown },
  tokenResponse: { status: number; body: unknown } = {
    status: 200,
    body: { access_token: "tok", expires_in: 900 },
  },
): { fetch: typeof fetch; calls: Array<{ url: string; init?: RequestInit }> } {
  const calls: Array<{ url: string; init?: RequestInit }> = []
  const impl = (async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const next = url.includes("/oauth2/token") ? tokenResponse : identityResponse
    return {
      ok: next.status >= 200 && next.status < 300,
      status: next.status,
      json: async () => next.body,
    } as Response
  }) as unknown as typeof fetch
  return { fetch: impl, calls }
}

beforeEach(() => {
  resetProvisioningTokenCache()
})

test("a created identity is returned with created true", async () => {
  const { fetch: impl } = stub({
    status: 200,
    body: { identity_id: "id-1", created: true },
  })
  const outcome = await provisionIdentity(
    CONFIG,
    { email: "Jean@Perso.fr", name: "Jean" },
    impl,
  )
  assert.deepEqual(outcome, {
    status: "provisioned",
    identityId: "id-1",
    created: true,
  })
})

test("an existing identity is joined, not an error", async () => {
  const { fetch: impl } = stub({
    status: 200,
    body: { identity_id: "id-1", created: false },
  })
  const outcome = await provisionIdentity(CONFIG, { email: "jean@perso.fr" }, impl)
  assert.deepEqual(outcome, {
    status: "provisioned",
    identityId: "id-1",
    created: false,
  })
})

test("the address is normalised and the role sent as configured", async () => {
  const { fetch: impl, calls } = stub({
    status: 200,
    body: { identity_id: "id-2", created: true },
  })
  await provisionIdentity(CONFIG, { email: "  Jean@Perso.FR " }, impl)
  const body = JSON.parse(String(calls.at(-1)?.init?.body))
  assert.equal(body.email, "jean@perso.fr")
  assert.equal(body.email_verified, true)
  assert.equal(body.role, "spore:user")
  assert.equal(body.product, "spore")
})

test("the inconclusive-scan 503 is unavailable, so the repair retries", async () => {
  const { fetch: impl } = stub({ status: 503, body: { error: "unavailable" } })
  const outcome = await provisionIdentity(CONFIG, { email: "jean@perso.fr" }, impl)
  assert.deepEqual(outcome, { status: "unavailable" })
})

test("a rejection carries its reason and is not retried as unavailable", async () => {
  const { fetch: impl } = stub({ status: 403, body: { error: "role_mismatch" } })
  const outcome = await provisionIdentity(CONFIG, { email: "jean@perso.fr" }, impl)
  assert.deepEqual(outcome, { status: "rejected", reason: "role_mismatch" })
})

test("a token that cannot be minted is unavailable", async () => {
  const { fetch: impl } = stub(
    { status: 200, body: { identity_id: "id-3" } },
    { status: 401, body: {} },
  )
  const outcome = await provisionIdentity(CONFIG, { email: "jean@perso.fr" }, impl)
  assert.deepEqual(outcome, { status: "unavailable" })
})

test("the token is reused across calls", async () => {
  const { fetch: impl, calls } = stub({
    status: 200,
    body: { identity_id: "id-4", created: true },
  })
  await provisionIdentity(CONFIG, { email: "a@b.fr" }, impl)
  await provisionIdentity(CONFIG, { email: "c@d.fr" }, impl)
  const tokenCalls = calls.filter((c) => c.url.includes("/oauth2/token"))
  assert.equal(tokenCalls.length, 1)
})

test("the token request names the audience and scope", async () => {
  const { fetch: impl, calls } = stub({
    status: 200,
    body: { identity_id: "id-5", created: true },
  })
  await provisionIdentity(CONFIG, { email: "a@b.fr" }, impl)
  const token = calls.find((c) => c.url.includes("/oauth2/token"))
  const body = String(token?.init?.body)
  assert.match(body, /audience=urbangate/)
  assert.match(body, /urbangate%3Aidentities%3Aprovision/)
})

test("an unreachable endpoint is unavailable", async () => {
  const failing = (async () => {
    throw new Error("ECONNREFUSED")
  }) as unknown as typeof fetch
  const outcome = await provisionIdentity(CONFIG, { email: "jean@perso.fr" }, failing)
  assert.deepEqual(outcome, { status: "unavailable" })
})
