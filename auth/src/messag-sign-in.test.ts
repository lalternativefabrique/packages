import { test } from "node:test"
import assert from "node:assert/strict"
import { messagSignInUrl } from "./messag-sign-in.ts"

test("the messag sign-in starts on the product's own route", () => {
  assert.equal(messagSignInUrl(), "/api/auth/sign-in/messag")
})

test("the messag sign-in carries where to land", () => {
  assert.equal(
    messagSignInUrl("/settings?tab=messag"),
    "/api/auth/sign-in/messag?callbackURL=%2Fsettings%3Ftab%3Dmessag",
  )
})
