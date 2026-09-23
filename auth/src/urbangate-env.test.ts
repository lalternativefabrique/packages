import { test } from "node:test"
import assert from "node:assert/strict"
import { kratosPasswordsFromEnv, ssoFromEnv } from "./urbangate-env.ts"

test("sso is off without its client secret", () => {
  assert.equal(ssoFromEnv("tornad", { URBANGATE_CLIENT_ID: "tornad-admin" }), undefined)
})

test("sso derives the client and the admin role from the product", () => {
  assert.deepEqual(ssoFromEnv("tornad", { URBANGATE_CLIENT_SECRET: "s" }), {
    issuer: "https://id.urbangate.dev",
    clientId: "tornad-admin",
    clientSecret: "s",
    adminRole: "tornad:admin",
    audience: "tornad",
  })
})

test("sso takes the issuer and client it is given", () => {
  const c = ssoFromEnv("tornad", {
    URBANGATE_ISSUER_URL: "http://localhost:4444",
    URBANGATE_CLIENT_ID: "tornad-dev",
    URBANGATE_CLIENT_SECRET: "s",
  })
  assert.equal(c?.issuer, "http://localhost:4444")
  assert.equal(c?.clientId, "tornad-dev")
})

test("kratos passwords are off without the provisioner secret", () => {
  assert.equal(kratosPasswordsFromEnv("spore", { URBANGATE_CLIENT_SECRET: "s" }), undefined)
})

test("kratos passwords derive the provisioner, the role and the product", () => {
  assert.deepEqual(kratosPasswordsFromEnv("spore", { URBANGATE_PROVISIONER_CLIENT_SECRET: "p" }), {
    issuer: "https://id.urbangate.dev",
    publicUrl: "https://id.urbangate.dev",
    clientId: "spore-provisioner",
    clientSecret: "p",
    role: "spore:user",
    product: "spore",
  })
})

test("the public URL stands apart from the issuer when both are given", () => {
  const c = kratosPasswordsFromEnv("spore", {
    URBANGATE_ISSUER_URL: "http://hydra:4444",
    URBANGATE_PUBLIC_URL: "http://localhost:4455",
    URBANGATE_PROVISIONER_CLIENT_ID: "spore-dev",
    URBANGATE_PROVISIONER_CLIENT_SECRET: "p",
  })
  assert.equal(c?.issuer, "http://hydra:4444")
  assert.equal(c?.publicUrl, "http://localhost:4455")
  assert.equal(c?.clientId, "spore-dev")
})

test("an empty variable counts as unset", () => {
  assert.equal(ssoFromEnv("tornad", { URBANGATE_CLIENT_SECRET: "" }), undefined)
  assert.equal(
    kratosPasswordsFromEnv("tornad", { URBANGATE_ISSUER_URL: "", URBANGATE_PROVISIONER_CLIENT_SECRET: "p" })?.issuer,
    "https://id.urbangate.dev",
  )
})

test("the deferred-provisioning hook rides along", () => {
  const hook = async () => {}
  const c = kratosPasswordsFromEnv("spore", { URBANGATE_PROVISIONER_CLIENT_SECRET: "p" }, { onProvisioningDeferred: hook })
  assert.equal(c?.onProvisioningDeferred, hook)
})
