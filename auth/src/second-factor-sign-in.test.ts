import assert from "node:assert/strict"
import { test } from "node:test"
import { isSecondFactorRequired, secondFactorOutcome } from "./second-factor-sign-in.ts"

test("a sign-in answered second_factor_required asks for the code", () => {
  assert.equal(isSecondFactorRequired({ code: "second_factor_required" }), true)
})

test("other refusals and no error do not ask for the code", () => {
  assert.equal(isSecondFactorRequired({ code: "invalid_credentials" }), false)
  assert.equal(isSecondFactorRequired(null), false)
})

test("a wrong code stays on the code step", () => {
  assert.deepEqual(secondFactorOutcome({ code: "invalid_code" }), { kind: "invalid" })
})

test("an expired flow goes back to the first step", () => {
  assert.deepEqual(secondFactorOutcome({ code: "flow_expired" }), { kind: "expired" })
})

test("any other refusal keeps its message", () => {
  assert.deepEqual(secondFactorOutcome({ code: "unavailable", message: "down" }), {
    kind: "refused",
    message: "down",
  })
})
