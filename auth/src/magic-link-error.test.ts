import assert from "node:assert/strict"
import { test } from "node:test"
import {
  initialMagicLinkError,
  isMagicLinkError,
  magicLinkErrorCallback,
  magicLinkErrorMessage,
  MAGIC_LINK_ERROR_DEFAULTS,
  type MagicLinkErrorLabels,
} from "./magic-link-error.ts"

const labels: MagicLinkErrorLabels = {
  invalidToken: "expired or already used",
  signUpDisabled: "no account for this address",
  failed: "failed",
}

test("INVALID_TOKEN covers both expiry and reuse", () => {
  assert.equal(magicLinkErrorMessage("INVALID_TOKEN", labels), labels.invalidToken)
})

test("new_user_signup_disabled names the missing account", () => {
  assert.equal(
    magicLinkErrorMessage("new_user_signup_disabled", labels),
    labels.signUpDisabled,
  )
})

test("anything else falls back to a retryable failure", () => {
  assert.equal(magicLinkErrorMessage("failed_to_create_session", labels), labels.failed)
  assert.equal(magicLinkErrorMessage(null, labels), labels.failed)
  assert.equal(magicLinkErrorMessage(undefined, labels), labels.failed)
})

test("labels are optional, and override one at a time", () => {
  assert.equal(
    magicLinkErrorMessage("INVALID_TOKEN"),
    MAGIC_LINK_ERROR_DEFAULTS.invalidToken,
  )
  assert.equal(
    magicLinkErrorMessage("new_user_signup_disabled", { invalidToken: "x" }),
    MAGIC_LINK_ERROR_DEFAULTS.signUpDisabled,
  )
})

// A page offering both flows reads one `?error=` against two vocabularies, so
// the predicate has to refuse OAuth's codes rather than claim every failure.
test("isMagicLinkError tells the flow's own failures from OAuth's", () => {
  assert.equal(isMagicLinkError("INVALID_TOKEN"), true)
  assert.equal(isMagicLinkError("new_user_signup_disabled"), true)
  assert.equal(isMagicLinkError("failed_to_create_user"), true)
  assert.equal(isMagicLinkError("account_not_linked"), false)
  assert.equal(isMagicLinkError("access_denied"), false)
  assert.equal(isMagicLinkError(null), false)
})

// The bug this replaced: with no errorCallbackURL, Better Auth falls back to
// the SUCCESS callback, which is a signed-in destination. An auth guard bounces
// the visitor and drops the ?error= on the way, so an expired link reads as
// nothing having happened.
test("a failed link comes back to the page that can send another one", () => {
  assert.equal(magicLinkErrorCallback(undefined, "/login", "/magic-link"), "/magic-link")
})

test("an explicit destination outranks the current page", () => {
  assert.equal(magicLinkErrorCallback("/help", "/login", "/magic-link"), "/help")
})

test("the fallback covers the server render, where there is no current page", () => {
  assert.equal(magicLinkErrorCallback(undefined, "/login", undefined), "/login")
})

test("initialMagicLinkError reads nothing off a server render", () => {
  assert.equal(globalThis.window, undefined)
  assert.equal(initialMagicLinkError(labels), undefined)
})

test("initialMagicLinkError leaves an OAuth failure to its own handler", () => {
  const restore = globalThis.window
  try {
    // @ts-expect-error minimal stand-in — the function reads only .location.search
    globalThis.window = { location: { search: "?error=account_not_linked" } }
    assert.equal(initialMagicLinkError(labels), undefined)

    // @ts-expect-error same
    globalThis.window = { location: { search: "?error=INVALID_TOKEN" } }
    assert.equal(initialMagicLinkError(labels), labels.invalidToken)
  } finally {
    globalThis.window = restore
  }
})
