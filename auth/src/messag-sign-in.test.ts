import { test } from "node:test"
import assert from "node:assert/strict"
import {
  isInvitationRequired,
  isMessagAddress,
  messagRegisterUrl,
  messagSignInUrl,
} from "./messag-sign-in.ts"

test("the messag sign-in starts on the product's own route", () => {
  assert.equal(messagSignInUrl(), "/api/auth/sign-in/messag")
})

test("the messag sign-in carries where to land", () => {
  assert.equal(
    messagSignInUrl("/settings?tab=messag"),
    "/api/auth/sign-in/messag?callbackURL=%2Fsettings%3Ftab%3Dmessag",
  )
})

test("a messag address is spotted whatever the case or the spaces", () => {
  assert.equal(isMessagAddress(" Ana@Messag.ECO "), true)
  assert.equal(isMessagAddress("ana@messag.eco.evil"), false)
  assert.equal(isMessagAddress("ana@gmail.com"), false)
  assert.equal(isMessagAddress("ana@mail.test", "mail.test"), true)
})

test("messag's sign-up gets told where to send the person back", () => {
  assert.equal(
    messagRegisterUrl("https://synthiz.com/register?x=1"),
    "https://messag.eco/register?return=https%3A%2F%2Fsynthiz.com%2Fregister%3Fx%3D1",
  )
})

test("only urbangate's code reads as an invitation refusal", () => {
  assert.equal(isInvitationRequired({ code: "invitation_required" }), true)
  assert.equal(isInvitationRequired({ code: "invalid_credentials" }), false)
  assert.equal(isInvitationRequired(null), false)
})
