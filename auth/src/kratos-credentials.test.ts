import { test } from "node:test"
import assert from "node:assert/strict"
import {
  KRATOS_SENTINEL_HASH,
  isKratosSentinel,
  verifyAgainstKratos,
} from "./kratos-credentials.ts"

function fetchStub(
  responses: Array<{ status: number; body: unknown; ok?: boolean }>,
): typeof fetch {
  let call = 0
  return (async () => {
    const next = responses[Math.min(call, responses.length - 1)]
    call += 1
    return {
      ok: next.ok ?? (next.status >= 200 && next.status < 300),
      status: next.status,
      json: async () => next.body,
    } as Response
  }) as unknown as typeof fetch
}

const FLOW = { status: 200, body: { id: "flow-1", ui: { nodes: [] } } }

test("the sentinel is not a hash and is recognised", () => {
  assert.equal(isKratosSentinel(KRATOS_SENTINEL_HASH), true)
  assert.equal(isKratosSentinel("$2b$10$abcdefghijklmnopqrstuv"), false)
  assert.equal(isKratosSentinel(null), false)
  assert.equal(isKratosSentinel(""), false)
  assert.match(KRATOS_SENTINEL_HASH, /^kratos:/)
})

test("valid credentials yield the identity id", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "Jean@Perso.fr", password: "right" },
    fetchStub([
      FLOW,
      { status: 200, body: { session: { identity: { id: "id-7", state: "active" } } } },
    ]),
  )
  assert.deepEqual(outcome, { status: "valid", identityId: "id-7" })
})

test("a refused password is invalid_credentials, not unavailable", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "wrong" },
    fetchStub([FLOW, { status: 400, body: { ui: { messages: [{ id: 4000006 }] } } }]),
  )
  assert.deepEqual(outcome, { status: "invalid_credentials" })
})

test("an inactive identity is account_disabled", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    fetchStub([FLOW, { status: 400, body: { ui: { messages: [{ id: 4000010 }] } } }]),
  )
  assert.deepEqual(outcome, { status: "account_disabled" })
})

test("an aal2 requirement is second_factor_required", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    fetchStub([FLOW, { status: 400, body: { error: { id: "session_aal2_required" } } }]),
  )
  assert.deepEqual(outcome, { status: "second_factor_required" })
})

test("an unverified address is email_not_verified", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    fetchStub([
      FLOW,
      { status: 403, body: { error: { id: "session_verified_address_required" } } },
    ]),
  )
  assert.deepEqual(outcome, { status: "email_not_verified" })
})

test("a 5xx from Kratos never reads as a wrong password", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    fetchStub([FLOW, { status: 502, body: {} }]),
  )
  assert.deepEqual(outcome, { status: "unavailable" })
})

test("an unreachable Kratos is unavailable, not a refusal", async () => {
  const failing = (async () => {
    throw new Error("ECONNREFUSED")
  }) as unknown as typeof fetch
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    failing,
  )
  assert.deepEqual(outcome, { status: "unavailable" })
})

test("a flow that cannot be started is unavailable", async () => {
  const outcome = await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    fetchStub([{ status: 500, body: {} }]),
  )
  assert.deepEqual(outcome, { status: "unavailable" })
})

test("the csrf token of the flow is submitted back", async () => {
  const seen: Array<Record<string, unknown>> = []
  const capturing = (async (_url: string, init?: RequestInit) => {
    if (!init || init.method !== "POST") {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          id: "flow-2",
          ui: { nodes: [{ attributes: { name: "csrf_token", value: "tok-9" } }] },
        }),
      } as Response
    }
    seen.push(JSON.parse(String(init.body)))
    return {
      ok: true,
      status: 200,
      json: async () => ({ session: { identity: { id: "id-9", state: "active" } } }),
    } as Response
  }) as unknown as typeof fetch

  await verifyAgainstKratos(
    "https://id.urbangate.dev",
    { email: "jean@perso.fr", password: "right" },
    capturing,
  )
  assert.equal(seen[0]?.csrf_token, "tok-9")
  assert.equal(seen[0]?.identifier, "jean@perso.fr")
})
