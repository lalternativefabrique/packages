import { test } from "node:test"
import assert from "node:assert/strict"
import { mapSsoProfile, roleFromIdToken } from "./sso-profile.ts"

test("the product's admin role makes the user an admin", () => {
  const u = mapSsoProfile(
    { email: "A@Example.com", email_verified: true, name: "Ada", roles: ["tornade:admin", "spore:admin"] },
    "tornade:admin",
  )
  assert.equal(u.role, "admin")
  assert.equal(u.email, "a@example.com")
  assert.equal(u.name, "Ada")
  assert.equal(u.emailVerified, true)
})

test("another product's admin role does not", () => {
  const u = mapSsoProfile({ email: "a@example.com", roles: ["spore:admin"] }, "tornade:admin")
  assert.equal(u.role, "user")
})

test("a missing or malformed roles claim is no role", () => {
  assert.equal(mapSsoProfile({ email: "a@example.com" }, "tornade:admin").role, "user")
  assert.equal(mapSsoProfile({ email: "a@example.com", roles: "tornade:admin" }, "tornade:admin").role, "user")
})

test("a missing name falls back to the mailbox", () => {
  assert.equal(mapSsoProfile({ email: "ada@example.com" }, "x").name, "ada")
})

function jwt(payload: Record<string, unknown>): string {
  const b64 = (o: unknown) => Buffer.from(JSON.stringify(o)).toString("base64url")
  return `${b64({ alg: "RS256", kid: "k" })}.${b64(payload)}.sig`
}

test("the ID token's roles claim decides the role", () => {
  assert.equal(roleFromIdToken(jwt({ sub: "u", roles: ["www:admin"] }), "www:admin"), "admin")
  assert.equal(roleFromIdToken(jwt({ sub: "u", roles: ["spore:admin"] }), "www:admin"), "user")
  assert.equal(roleFromIdToken(jwt({ sub: "u" }), "www:admin"), "user")
})

test("roles nested under Hydra's ext claim count too", () => {
  assert.equal(roleFromIdToken(jwt({ sub: "u", ext: { roles: ["www:admin"] } }), "www:admin"), "admin")
})

test("an unreadable token leaves the role alone", () => {
  assert.equal(roleFromIdToken(undefined, "www:admin"), undefined)
  assert.equal(roleFromIdToken("not-a-jwt", "www:admin"), undefined)
  assert.equal(roleFromIdToken("a.!!!.c", "www:admin"), undefined)
})
