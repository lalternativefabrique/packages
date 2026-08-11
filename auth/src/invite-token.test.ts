import assert from "node:assert/strict"
import { test } from "node:test"
import { normalizeInviteToken, withInviteToken } from "./invite-token.ts"

test("normalizeInviteToken keeps a plain token", () => {
  assert.equal(normalizeInviteToken("abc123"), "abc123")
  assert.equal(normalizeInviteToken("  abc123  "), "abc123")
})

test("normalizeInviteToken drops what is not one token", () => {
  assert.equal(normalizeInviteToken(undefined), undefined)
  assert.equal(normalizeInviteToken(""), undefined)
  assert.equal(normalizeInviteToken("   "), undefined)
  assert.equal(normalizeInviteToken(42), undefined)
  // A repeated ?invite= param arrives as an array.
  assert.equal(normalizeInviteToken(["a", "b"]), undefined)
  assert.equal(normalizeInviteToken("x".repeat(129)), undefined)
})

test("withInviteToken carries the offer to the sibling screen", () => {
  assert.equal(withInviteToken("/login", "tok"), "/login?invite=tok")
  assert.equal(
    withInviteToken("/login?mode=signup", "tok"),
    "/login?mode=signup&invite=tok",
  )
})

test("withInviteToken leaves the href alone without a token", () => {
  assert.equal(withInviteToken("/login", undefined), "/login")
  assert.equal(withInviteToken("/login", ""), "/login")
})

test("withInviteToken escapes a token that would break the query", () => {
  assert.equal(withInviteToken("/login", "a&b=c"), "/login?invite=a%26b%3Dc")
})
