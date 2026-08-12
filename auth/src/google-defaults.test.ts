import assert from "node:assert/strict"
import { test } from "node:test"
import { withGoogleDefaults } from "./google-defaults.ts"

test("a bare config gets the platform defaults", () => {
  const out = withGoogleDefaults({ clientId: "id", clientSecret: "secret" })
  assert.deepEqual(out, {
    clientId: "id",
    clientSecret: "secret",
    accessType: "offline",
    prompt: "select_account consent",
  })
})

test("what the app set wins", () => {
  const out = withGoogleDefaults({
    clientId: "id",
    clientSecret: "secret",
    prompt: "none",
    accessType: "online",
  })
  assert.equal(out.prompt, "none")
  assert.equal(out.accessType, "online")
})

test("everything else is carried through", () => {
  const mapProfileToUser = () => ({ name: "x" })
  const out = withGoogleDefaults({
    clientId: "id",
    clientSecret: "secret",
    scope: ["drive.readonly"],
    mapProfileToUser,
  })
  assert.deepEqual(out.scope, ["drive.readonly"])
  assert.equal(out.mapProfileToUser, mapProfileToUser)
})

test("a function config is left alone", () => {
  const fn = () => ({ clientId: "id", clientSecret: "secret" })
  assert.equal(withGoogleDefaults(fn), fn)
})
