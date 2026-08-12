import assert from "node:assert/strict"
import { test } from "node:test"
import { withSignUpName } from "./signup-name.ts"

test("a name that was given is kept", () => {
  const out = withSignUpName({ email: "ada@example.com", name: "Ada Lovelace" })
  assert.equal(out.name, "Ada Lovelace")
})

test("a missing name falls back to the local part of the address", () => {
  const out = withSignUpName({ email: "ada@example.com" })
  assert.equal(out.name, "ada")
})

test("a blank name is treated as missing", () => {
  const out = withSignUpName({ email: "ada@example.com", name: "   " })
  assert.equal(out.name, "ada")
})

test("a given name is trimmed", () => {
  const out = withSignUpName({ email: "ada@example.com", name: "  Ada  " })
  assert.equal(out.name, "Ada")
})

test("only the last @ separates the local part", () => {
  const out = withSignUpName({ email: '"a@b"@example.com' })
  assert.equal(out.name, '"a@b"')
})

test("an address with no @ is used whole", () => {
  const out = withSignUpName({ email: "ada" })
  assert.equal(out.name, "ada")
})

test("the rest of the body is carried through untouched", () => {
  const out = withSignUpName({
    email: "ada@example.com",
    password: "hunter22",
    inviteToken: "tok",
  })
  assert.equal(out.password, "hunter22")
  assert.equal(out.inviteToken, "tok")
})

test("a non-string name is treated as missing", () => {
  const out = withSignUpName({ email: "ada@example.com", name: null })
  assert.equal(out.name, "ada")
})

// The magic-link plugin creates the account itself with `name: name || ""`,
// so it arrives as an empty string rather than an absent key.
test("the empty name a magic-link signup creates is filled in", () => {
  const out = withSignUpName({ email: "ada@example.com", name: "" })
  assert.equal(out.name, "ada")
})
