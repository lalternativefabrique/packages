import assert from "node:assert/strict"
import { test } from "node:test"
import { isEmailNotVerified } from "./email-not-verified.ts"

test("the documented code names the refusal", () => {
  assert.equal(isEmailNotVerified({ code: "EMAIL_NOT_VERIFIED" }), true)
})

test("a bare 403 is the same refusal from a build without error codes", () => {
  assert.equal(isEmailNotVerified({ status: 403 }), true)
})

test("wrong credentials are not an unverified address", () => {
  assert.equal(
    isEmailNotVerified({ code: "INVALID_EMAIL_OR_PASSWORD", status: 401 }),
    false,
  )
})

test("no error is not a refusal", () => {
  assert.equal(isEmailNotVerified(null), false)
  assert.equal(isEmailNotVerified(undefined), false)
})
