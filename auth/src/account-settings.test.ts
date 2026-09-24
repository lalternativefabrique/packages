import { test } from "node:test"
import assert from "node:assert/strict"
import {
  emailChangeProblem,
  hasPasswordAccount,
  passwordChangeProblem,
} from "./account-settings.ts"

test("a password change needs both passwords", () => {
  assert.equal(passwordChangeProblem({ current: "", next: "abcdefgh", confirm: "abcdefgh" }), "missing")
  assert.equal(passwordChangeProblem({ current: "old-pass", next: "", confirm: "" }), "missing")
})

test("a new password shorter than eight characters is refused", () => {
  assert.equal(passwordChangeProblem({ current: "old-pass", next: "short", confirm: "short" }), "too_short")
})

test("the confirmation must match the new password", () => {
  assert.equal(passwordChangeProblem({ current: "old-pass", next: "new-pass1", confirm: "new-pass2" }), "mismatch")
})

test("the new password must differ from the current one", () => {
  assert.equal(passwordChangeProblem({ current: "same-pass", next: "same-pass", confirm: "same-pass" }), "unchanged")
})

test("a valid password change has no problem", () => {
  assert.equal(passwordChangeProblem({ current: "old-pass", next: "new-pass1", confirm: "new-pass1" }), undefined)
})

test("an address without a domain is invalid", () => {
  assert.equal(emailChangeProblem("a@b.fr", "camille@"), "invalid")
  assert.equal(emailChangeProblem("a@b.fr", "camille"), "invalid")
})

test("the same address, whatever its case, is unchanged", () => {
  assert.equal(emailChangeProblem("Camille@Exemple.fr", " camille@exemple.fr "), "unchanged")
})

test("a new valid address has no problem", () => {
  assert.equal(emailChangeProblem("a@b.fr", "camille@exemple.fr"), undefined)
})

test("a password is held by the credential account", () => {
  assert.equal(hasPasswordAccount([{ providerId: "google" }, { providerId: "credential" }]), true)
  assert.equal(hasPasswordAccount([{ providerId: "google" }]), false)
  assert.equal(hasPasswordAccount(null), undefined)
})
