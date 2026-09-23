import { test } from "node:test"
import assert from "node:assert/strict"
import { coreProxy, forwardHeaders } from "./core-proxy.ts"

function fakeAuth(session: unknown, token: string | Error | null) {
  return {
    api: {
      getSession: async () => session,
      getAccessToken: async () => {
        if (token instanceof Error) throw token
        return token ? { accessToken: token } : null
      },
    },
  }
}

function withFetch(fn: (calls: Array<{ url: string; init: RequestInit }>) => Promise<void>) {
  const calls: Array<{ url: string; init: RequestInit }> = []
  const original = globalThis.fetch
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    calls.push({ url, init })
    return new Response("{}", { status: 200, headers: { "content-type": "application/json" } })
  }) as typeof fetch
  return fn(calls).finally(() => {
    globalThis.fetch = original
  })
}

test("forwardHeaders drops the cookie, the browser's authorization and hop-by-hop headers", () => {
  const out = forwardHeaders(
    new Headers({ cookie: "s=1", authorization: "Bearer stolen", connection: "close", accept: "application/json" }),
  )
  assert.equal(out.get("cookie"), null)
  assert.equal(out.get("authorization"), null)
  assert.equal(out.get("connection"), null)
  assert.equal(out.get("accept"), "application/json")
})

test("a session with an urbangate token reaches the core with that token", async () => {
  await withFetch(async (calls) => {
    const proxy = coreProxy(fakeAuth({ user: { role: "user" } }, "at-1"), { coreUrl: "http://app:8080/" })
    const res = await proxy(new Request("https://tornad.dev/api/keys?x=1", { headers: { cookie: "s=1" } }))
    assert.equal(res.status, 200)
    assert.equal(calls[0].url, "http://app:8080/api/keys?x=1")
    assert.equal((calls[0].init.headers as Headers).get("authorization"), "Bearer at-1")
    assert.equal((calls[0].init.headers as Headers).get("cookie"), null)
  })
})

test("no session is 401, and a plain user on an admin route is 403", async () => {
  const anon = coreProxy(fakeAuth(null, "at-1"), { coreUrl: "http://app:8080" })
  assert.equal((await anon(new Request("https://x/api/v1/apps"))).status, 401)
  const user = coreProxy(fakeAuth({ user: { role: "user" } }, "at-1"), { coreUrl: "http://app:8080", adminOnly: true })
  assert.equal((await user(new Request("https://x/api/v1/apps"))).status, 403)
})

test("a session without an urbangate token is asked to sign in, unless a fallback signs for it", async () => {
  const bare = coreProxy(fakeAuth({ user: { role: "user" } }, new Error("no account")), { coreUrl: "http://app:8080" })
  const res = await bare(new Request("https://x/api/keys"))
  assert.equal(res.status, 401)
  assert.deepEqual(await res.json(), { error: "sign_in_required" })

  await withFetch(async (calls) => {
    const legacy = coreProxy(fakeAuth({ user: { role: "user" } }, null), {
      coreUrl: "http://app:8080",
      fallbackToken: async () => "web-signed",
    })
    await legacy(new Request("https://x/api/keys"))
    assert.equal((calls[0].init.headers as Headers).get("authorization"), "Bearer web-signed")
  })
})
