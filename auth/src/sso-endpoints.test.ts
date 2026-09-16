import { describe, it } from "node:test"
import assert from "node:assert/strict"
import { ssoEndpoints } from "./sso-endpoints.ts"

describe("ssoEndpoints", () => {
  it("derives Hydra's endpoints and the account issuer from the issuer", () => {
    assert.deepEqual(ssoEndpoints("https://id.urbangate.dev"), {
      discoveryUrl: "https://id.urbangate.dev/.well-known/openid-configuration",
      accountIssuer: "https://id.urbangate.dev",
      authorizationUrl: "https://id.urbangate.dev/oauth2/auth",
      tokenUrl: "https://id.urbangate.dev/oauth2/token",
      userInfoUrl: "https://id.urbangate.dev/userinfo",
    })
  })

  it("drops a trailing slash so the issuer matches the tokens' iss claim", () => {
    assert.equal(ssoEndpoints("http://localhost:4444/").accountIssuer, "http://localhost:4444")
  })
})
