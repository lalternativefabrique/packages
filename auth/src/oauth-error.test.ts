import assert from "node:assert/strict"
import { test } from "node:test"
import {
  oauthErrorCallback,
  oauthErrorMessage,
  type OAuthErrorLabels,
} from "./oauth-error.ts"

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

// The bug this replaced: with no errorCallbackURL, Better Auth keeps the
// browser on its own error route — the app root — where nothing reads the
// `?error=` it appended, so a refused provider button reads as broken.
test("a refused sign-in comes back to the screen that shows the error", () => {
  assert.equal(oauthErrorCallback(undefined, "/login", "/register"), "/register")
})

test("an explicit destination outranks the current page", () => {
  assert.equal(oauthErrorCallback("/help", "/login", "/register"), "/help")
})

test("the fallback covers the server render, where there is no current page", () => {
  assert.equal(oauthErrorCallback(undefined, "/login", undefined), "/login")
})
