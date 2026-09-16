export interface SsoEndpoints {
  discoveryUrl: string
  accountIssuer: string
  authorizationUrl: string
  tokenUrl: string
  userInfoUrl: string
}

/**
 * Hydra's endpoints, derived from the issuer so that the provider comes up
 * without a discovery fetch: Better Auth refuses to initialise a provider
 * whose discovery failed unless it already knows the account issuer and the
 * endpoints, and that failure would take the whole app down at boot.
 */
export function ssoEndpoints(issuer: string): SsoEndpoints {
  const base = issuer.replace(/\/$/, "")
  return {
    discoveryUrl: `${base}/.well-known/openid-configuration`,
    accountIssuer: base,
    authorizationUrl: `${base}/oauth2/auth`,
    tokenUrl: `${base}/oauth2/token`,
    userInfoUrl: `${base}/userinfo`,
  }
}
