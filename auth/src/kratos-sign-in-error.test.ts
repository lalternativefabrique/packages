import { test } from "node:test"
import assert from "node:assert/strict"
import {
  isAccountDisabled,
  isIdentityProviderUnavailable,
  needsPasswordRecovery,
  needsSecondFactor,
} from "./kratos-sign-in-error.ts"

test("a missing provider password is told apart from a wrong one", () => {
  assert.equal(needsPasswordRecovery({ code: "IDENTITY_HAS_NO_PASSWORD", status: 403 }), true)
  assert.equal(needsPasswordRecovery({ code: "INVALID_EMAIL_OR_PASSWORD", status: 401 }), false)
  assert.equal(needsPasswordRecovery(null), false)
})

test("an outage is recognised by code or by status", () => {
  assert.equal(
    isIdentityProviderUnavailable({ code: "IDENTITY_PROVIDER_UNAVAILABLE", status: 503 }),
    true,
  )
  assert.equal(isIdentityProviderUnavailable({ code: "SOMETHING", status: 503 }), true)
  assert.equal(isIdentityProviderUnavailable({ code: "INVALID_EMAIL_OR_PASSWORD", status: 401 }), false)
})

test("an outage never reads as a wrong password", () => {
  const outage = { code: "IDENTITY_PROVIDER_UNAVAILABLE", status: 503 }
  assert.equal(needsPasswordRecovery(outage), false)
  assert.equal(isIdentityProviderUnavailable(outage), true)
})

test("the second factor and the disabled account have their own predicates", () => {
  assert.equal(needsSecondFactor({ code: "SECOND_FACTOR_REQUIRED", status: 403 }), true)
  assert.equal(isAccountDisabled({ code: "ACCOUNT_DISABLED", status: 403 }), true)
  assert.equal(needsSecondFactor({ code: "ACCOUNT_DISABLED", status: 403 }), false)
})
