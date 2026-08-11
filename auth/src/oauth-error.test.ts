import assert from "node:assert/strict"
import { test } from "node:test"
import { oauthErrorMessage, type OAuthErrorLabels } from "./oauth-error.ts"

const labels: OAuthErrorLabels = {
  accountNotLinked: "already a password account",
  socialCancelled: "cancelled",
  socialFailed: "failed",
}

test("account_not_linked names the refusal", () => {
  assert.equal(
    oauthErrorMessage("account_not_linked", labels),
    labels.accountNotLinked,
  )
})

test("access_denied reads as a cancellation, not a failure", () => {
  assert.equal(oauthErrorMessage("access_denied", labels), labels.socialCancelled)
})

test("anything else falls back to a retryable failure", () => {
  assert.equal(oauthErrorMessage("server_error", labels), labels.socialFailed)
  assert.equal(oauthErrorMessage(null, labels), labels.socialFailed)
  assert.equal(oauthErrorMessage(undefined, labels), labels.socialFailed)
})
