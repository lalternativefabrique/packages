import assert from "node:assert/strict"
import { test } from "node:test"
import {
  messagAddress,
  messagLocalPart,
  messagRegisterUrl,
} from "./messag-sign-in.ts"

test("a local part becomes an address on the Messag domain", () => {
  assert.equal(messagAddress("  Ana.B ", "messag.eco"), "ana.b@messag.eco")
})

test("a full Messag address is accepted as typed", () => {
  assert.equal(messagAddress("ana@Messag.eco", "messag.eco"), "ana@messag.eco")
})

test("an address on another domain is refused", () => {
  assert.equal(messagAddress("ana@gmail.com", "messag.eco"), null)
})

test("an empty or malformed local part is refused", () => {
  assert.equal(messagAddress("", "messag.eco"), null)
  assert.equal(messagAddress("ana..b", "messag.eco"), null)
  assert.equal(messagAddress(".ana", "messag.eco"), null)
})

test("the local part is read back from a Messag address only", () => {
  assert.equal(messagLocalPart("ana@messag.eco", "messag.eco"), "ana")
  assert.equal(messagLocalPart("ana@gmail.com", "messag.eco"), undefined)
  assert.equal(messagLocalPart(undefined, "messag.eco"), undefined)
})

test("the sign-up link carries the page to come back to", () => {
  assert.equal(
    messagRegisterUrl("https://messag.eco/", "https://lalter.fr/login?redirect=%2Fa"),
    "https://messag.eco/register?return=https%3A%2F%2Flalter.fr%2Flogin%3Fredirect%3D%252Fa",
  )
})

test("an address handed back earlier is not sent round again", () => {
  assert.equal(
    messagRegisterUrl("https://messag.eco", "https://lalter.fr/login?email=a%40messag.eco"),
    "https://messag.eco/register?return=https%3A%2F%2Flalter.fr%2Flogin",
  )
})

test("without a page to come back to, the sign-up stands alone", () => {
  assert.equal(messagRegisterUrl("https://messag.eco"), "https://messag.eco/register")
})
