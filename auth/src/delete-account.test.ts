import { test } from "node:test"
import assert from "node:assert/strict"
import { deleteAccount, type DeleteAccountConfig } from "./delete-account.ts"
import type {
  DeletionOutcome,
  IdentityProvisioningConfig,
} from "./identity-provisioning.ts"

const IDENTITY: IdentityProvisioningConfig = {
  issuer: "https://id.urbangate.dev",
  clientId: "partage-provisioner",
  clientSecret: "secret",
  role: "partage:user",
  product: "partage",
}

function urbangate(outcome: DeletionOutcome): {
  call: DeleteAccountConfig["requestDeletion"]
  calls: Array<{ identityId: string; userId?: string }>
} {
  const calls: Array<{ identityId: string; userId?: string }> = []
  return {
    call: async (_identity, request) => {
      calls.push(request)
      return outcome
    },
    calls,
  }
}

test("runs the steps in order and deletes the login last", async () => {
  const order: Array<string> = []
  const config: DeleteAccountConfig = {
    cancelBilling: async () => {
      order.push("billing")
    },
    purgeDomain: async () => {
      order.push("domain")
    },
    revokeExternal: async () => {
      order.push("external")
    },
  }

  const result = await deleteAccount(config, { userId: "u1" }, async () => {
    order.push("login")
  })

  assert.deepEqual(result, { status: "deleted", warnings: [] })
  assert.deepEqual(order, ["billing", "domain", "external", "login"])
})

// An account that keeps being charged is the one outcome nobody notices, so
// billing failing has to stop everything with nothing yet destroyed.
test("stops on a billing failure without touching anything else", async () => {
  let purged = false
  let loginDeleted = false

  const result = await deleteAccount(
    {
      cancelBilling: async () => {
        throw new Error("psp down")
      },
      purgeDomain: async () => {
        purged = true
      },
    },
    { userId: "u1" },
    async () => {
      loginDeleted = true
    },
  )

  assert.equal(result.status, "failed")
  assert.equal(result.status === "failed" && result.step, "cancel_billing")
  assert.equal(purged, false)
  assert.equal(loginDeleted, false)
})

test("stops on a domain purge failure, leaving an account to retry from", async () => {
  let loginDeleted = false

  const result = await deleteAccount(
    {
      purgeDomain: async () => {
        throw new Error("postgres unreachable")
      },
    },
    { userId: "u1" },
    async () => {
      loginDeleted = true
    },
  )

  assert.equal(result.status, "failed")
  assert.equal(result.status === "failed" && result.step, "purge_domain")
  assert.equal(loginDeleted, false)
})

// A right to erasure cannot depend on Google answering.
test("carries on when an external revocation fails, and says so", async () => {
  const warnings: Array<string> = []

  const result = await deleteAccount(
    {
      revokeExternal: async () => {
        throw new Error("google 503")
      },
      onWarning: (step) => warnings.push(step),
    },
    { userId: "u1" },
    async () => {},
  )

  assert.deepEqual(result, { status: "deleted", warnings: ["revoke_external"] })
  assert.deepEqual(warnings, ["revoke_external"])
})

test("asks urbangate to drop the role, naming no product", async () => {
  const stub = urbangate({ status: "requested", eventId: "e1" })

  const result = await deleteAccount(
    { identity: IDENTITY, requestDeletion: stub.call },
    { userId: "u1", identityId: "ident-1" },
    async () => {},
  )

  assert.equal(result.status, "deleted")
  // The product comes from the token's client_id: sending it would let a
  // token ask for an account it does not own.
  assert.deepEqual(stub.calls, [{ identityId: "ident-1", userId: "u1" }])
})

test("stops when urbangate is unreachable, before the login goes", async () => {
  const stub = urbangate({ status: "unavailable" })
  let loginDeleted = false

  const result = await deleteAccount(
    { identity: IDENTITY, requestDeletion: stub.call },
    { userId: "u1", identityId: "ident-1" },
    async () => {
      loginDeleted = true
    },
  )

  assert.equal(result.status, "failed")
  assert.equal(result.status === "failed" && result.step, "drop_identity_role")
  assert.equal(loginDeleted, false)
})

// urbangate refusing this request is not a transient fault: retrying sends the
// same one. The deletion carries on and the warning says the role survived.
test("warns but carries on when urbangate refuses", async () => {
  const stub = urbangate({ status: "rejected", reason: "not_a_provisioner" })
  const warnings: Array<string> = []

  const result = await deleteAccount(
    {
      identity: IDENTITY,
      requestDeletion: stub.call,
      onWarning: (step) => warnings.push(step),
    },
    { userId: "u1", identityId: "ident-1" },
    async () => {},
  )

  assert.deepEqual(result, {
    status: "deleted",
    warnings: ["drop_identity_role"],
  })
  assert.deepEqual(warnings, ["drop_identity_role"])
})

test("skips urbangate for an account that has no identity", async () => {
  const stub = urbangate({ status: "requested", eventId: "e1" })

  const result = await deleteAccount(
    { identity: IDENTITY, requestDeletion: stub.call },
    { userId: "u1", identityId: null },
    async () => {},
  )

  assert.equal(result.status, "deleted")
  assert.equal(stub.calls.length, 0)
})

test("reports a login row that could not be deleted", async () => {
  const result = await deleteAccount({}, { userId: "u1" }, async () => {
    throw new Error("constraint violation")
  })

  assert.equal(result.status, "failed")
  assert.equal(result.status === "failed" && result.step, "delete_login")
})
