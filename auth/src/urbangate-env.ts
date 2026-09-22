import type { PlatformKratosPasswordConfig, PlatformSsoConfig } from "./types"

export const URBANGATE_ISSUER = "https://id.urbangate.dev"

/** What single sign-on reads. The secret is the switch: unset, SSO stays off. */
export interface SsoEnv {
  /** Hydra's issuer. Defaults to the suite's. */
  URBANGATE_ISSUER_URL?: string
  /** The back-office's OIDC client. Defaults to `<product>-admin`. */
  URBANGATE_CLIENT_ID?: string
  URBANGATE_CLIENT_SECRET?: string
}

/** What enrolment at the provider reads. The secret is the switch: unset, nobody is enrolled. */
export interface ProvisionerEnv {
  /** Hydra's issuer. Defaults to the suite's. */
  URBANGATE_ISSUER_URL?: string
  /** Kratos' public URL, when the front reaches it by another address than the issuer. Defaults to the issuer. */
  URBANGATE_PUBLIC_URL?: string
  /** The client that enrols customers and issues their keys. Defaults to `<product>-provisioner`. */
  URBANGATE_PROVISIONER_CLIENT_ID?: string
  URBANGATE_PROVISIONER_CLIENT_SECRET?: string
}

export type UrbangateEnv = SsoEnv & ProvisionerEnv

export function ssoFromEnv(product: string, env: SsoEnv): PlatformSsoConfig | undefined {
  if (!env.URBANGATE_CLIENT_SECRET) return undefined
  return {
    issuer: env.URBANGATE_ISSUER_URL || URBANGATE_ISSUER,
    clientId: env.URBANGATE_CLIENT_ID || `${product}-admin`,
    clientSecret: env.URBANGATE_CLIENT_SECRET,
    adminRole: `${product}:admin`,
  }
}

export function kratosPasswordsFromEnv(
  product: string,
  env: ProvisionerEnv,
  options: Pick<PlatformKratosPasswordConfig, "onProvisioningDeferred"> = {},
): PlatformKratosPasswordConfig | undefined {
  if (!env.URBANGATE_PROVISIONER_CLIENT_SECRET) return undefined
  const issuer = env.URBANGATE_ISSUER_URL || URBANGATE_ISSUER
  return {
    issuer,
    publicUrl: env.URBANGATE_PUBLIC_URL || issuer,
    clientId: env.URBANGATE_PROVISIONER_CLIENT_ID || `${product}-provisioner`,
    clientSecret: env.URBANGATE_PROVISIONER_CLIENT_SECRET,
    role: `${product}:user`,
    product,
    ...options,
  }
}
