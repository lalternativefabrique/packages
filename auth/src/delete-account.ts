import type {
  DeletionOutcome,
  IdentityProvisioningConfig,
} from "./identity-provisioning"

export interface DeleteAccountConfig {
  /**
   * Ends the person's subscription. Called first and its failure is fatal:
   * an account that is deleted but keeps being charged is far worse than a
   * deletion its owner has to retry. Must be idempotent — no subscription is
   * a success, not an error, because a retried deletion reaches it again.
   *
   * Omit on a product that bills nobody.
   */
  cancelBilling?: (userId: string) => Promise<void>
  /**
   * Purges the product's own data: the domain tables keyed on the user, the
   * object storage prefix, whatever else only this product knows about.
   * Fatal, and called before anything that cannot be undone, so a failure
   * leaves an account its owner can still sign into and delete again.
   *
   * Must be idempotent: `DELETE ... WHERE user_id` on already-purged rows
   * affects none, which is what makes a retry safe.
   */
  purgeDomain?: (userId: string) => Promise<void>
  /**
   * Revokes what third parties still hold: OAuth grants on the person's
   * Google or Bluesky account, API keys, webhook endpoints. Best-effort —
   * the tokens die with the rows anyway, and a right to erasure cannot
   * depend on another company's uptime.
   */
  revokeExternal?: (userId: string) => Promise<void>
  /**
   * Drops this product's role at urbangate. Omit to leave the identity
   * untouched, which is right for a product that does not enrol one.
   */
  identity?: IdentityProvisioningConfig
  /** Reports a step that failed without stopping the deletion. */
  onWarning?: (step: string, error: unknown) => void
  /**
   * The call that asks urbangate to drop the role. Defaults to
   * `requestAccountDeletion`; an app overrides it only in a test.
   */
  requestDeletion?: (
    identity: IdentityProvisioningConfig,
    request: { identityId: string; userId?: string },
  ) => Promise<DeletionOutcome>
}

export interface DeleteAccountRequest {
  userId: string
  /** The `identityId` on the local user row; absent on accounts that predate urbangate. */
  identityId?: string | null
}

export type DeleteAccountResult =
  | { status: "deleted"; warnings: Array<string> }
  | { status: "failed"; step: DeleteAccountStep; cause: unknown }

export type DeleteAccountStep =
  | "cancel_billing"
  | "purge_domain"
  | "drop_identity_role"
  | "delete_login"

/**
 * Deletes one product's account, in the order that leaves the least damage
 * when a step fails.
 *
 * No step can be rolled back once the next one has run, and no transaction
 * spans a payment provider, a domain database and an identity provider. What
 * the order buys is that a failure is always recoverable by retrying: billing
 * stops first because a charge that outlives the account is the one outcome
 * nobody notices, the domain data goes next while the account still exists to
 * try again from, and the login row goes last because it is what the person
 * would need to come back.
 *
 * Every step is idempotent, so the retry is safe.
 */
export async function deleteAccount(
  config: DeleteAccountConfig,
  request: DeleteAccountRequest,
  deleteLogin: (userId: string) => Promise<void>,
): Promise<DeleteAccountResult> {
  const warnings: Array<string> = []
  const warn = (step: string, error: unknown) => {
    warnings.push(step)
    config.onWarning?.(step, error)
  }

  if (config.cancelBilling) {
    try {
      await config.cancelBilling(request.userId)
    } catch (cause) {
      return { status: "failed", step: "cancel_billing", cause }
    }
  }

  if (config.purgeDomain) {
    try {
      await config.purgeDomain(request.userId)
    } catch (cause) {
      return { status: "failed", step: "purge_domain", cause }
    }
  }

  // Before the login row goes: the tokens live on it, and a row that is gone
  // takes with it the only way to find what to revoke.
  if (config.revokeExternal) {
    try {
      await config.revokeExternal(request.userId)
    } catch (cause) {
      warn("revoke_external", cause)
    }
  }

  if (config.identity && request.identityId) {
    // Imported here rather than at the top so this module carries no runtime
    // dependency on the provisioning client: a caller that injects its own
    // never loads it.
    const call =
      config.requestDeletion ??
      (await import("./identity-provisioning")).requestAccountDeletion
    const outcome = await call(config.identity, {
      identityId: request.identityId,
      userId: request.userId,
    })
    if (outcome.status === "unavailable") {
      return {
        status: "failed",
        step: "drop_identity_role",
        cause: new Error("urbangate unavailable"),
      }
    }
    // A rejection is urbangate refusing this request, not a transient fault:
    // retrying sends the same one. The role survives, and the warning is what
    // says so.
    if (outcome.status === "rejected") {
      warn("drop_identity_role", new Error(outcome.reason))
    }
  }

  try {
    await deleteLogin(request.userId)
  } catch (cause) {
    return { status: "failed", step: "delete_login", cause }
  }

  return { status: "deleted", warnings }
}
