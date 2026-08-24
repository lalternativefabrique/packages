import assert from "node:assert/strict"
import { test } from "node:test"
import { resolveRateLimit } from "./rate-limit.ts"

test("the limiter is on when the app says nothing", () => {
  const out = resolveRateLimit(undefined)
  assert.equal(out.enabled, true)
})

test("sign-in is capped tighter than the global rule", () => {
  const out = resolveRateLimit()
  assert.deepEqual(out.customRules["/sign-in/email"], { window: 60, max: 5 })
  assert.equal(out.max, 100)
})

test("OTP sending is the tightest rule", () => {
  const out = resolveRateLimit()
  assert.deepEqual(out.customRules["/email-otp/send-verification-otp"], {
    window: 60,
    max: 3,
  })
})

test("every two-factor verification path is capped", () => {
  const rules = resolveRateLimit().customRules
  for (const path of [
    "/two-factor/verify-totp",
    "/two-factor/verify-otp",
    "/two-factor/verify-backup-code",
  ]) {
    assert.deepEqual(rules[path], { window: 60, max: 5 }, path)
  }
})

test("an app can tighten a platform rule", () => {
  const out = resolveRateLimit({
    customRules: { "/sign-in/email": { window: 300, max: 3 } },
  })
  assert.deepEqual(out.customRules["/sign-in/email"], { window: 300, max: 3 })
  assert.deepEqual(out.customRules["/forget-password"], { window: 60, max: 3 })
})

test("an app that runs replicas can move the counters to the database", () => {
  const out = resolveRateLimit({ storage: "database", modelName: "rateLimit" })
  assert.equal(out.storage, "database")
  assert.equal(out.modelName, "rateLimit")
})

test("storage is left unset so Better Auth picks its own default", () => {
  assert.equal("storage" in resolveRateLimit(), false)
})

test("an app can still switch it off explicitly", () => {
  assert.equal(resolveRateLimit({ enabled: false }).enabled, false)
})
