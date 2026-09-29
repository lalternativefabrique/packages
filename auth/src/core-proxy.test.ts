import { test } from "node:test"
import assert from "node:assert/strict"
import { forwardHeaders } from "./core-proxy.ts"

test("forwardHeaders drops the cookie, the browser's authorization and hop-by-hop headers", () => {
  const out = forwardHeaders(
    new Headers({ cookie: "s=1", authorization: "Bearer stolen", connection: "close", accept: "application/json" }),
  )
  assert.equal(out.get("cookie"), null)
  assert.equal(out.get("authorization"), null)
  assert.equal(out.get("connection"), null)
  assert.equal(out.get("accept"), "application/json")
})
