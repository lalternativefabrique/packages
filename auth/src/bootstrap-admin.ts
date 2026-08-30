import type { PlatformAuth } from "./server"

/**
 * Minimal Postgres pool contract this module needs — kept structural so the
 * package doesn't pull in `pg` as a dependency; any `pg.Pool` satisfies it.
 */
export interface BootstrapAdminPool {
  connect(): Promise<BootstrapAdminClient>
}

export interface BootstrapAdminClient {
  query(text: string, values?: unknown[]): Promise<{ rowCount: number | null }>
  release(): void
}

export interface BootstrapFirstAdminInput {
  email: string
  password: string
  name: string
}

export type BootstrapFirstAdminResult =
  | { ok: true }
  | { ok: false; error: "already_completed" }

// The admin() plugin's createUser endpoint isn't in PlatformAuth's published
// type (see the widening note on createPlatformAuth in ./server), even though
// it's mounted at runtime by every app that enables admin().
interface AuthWithAdminCreateUser {
  api: {
    createUser: (input: {
      body: {
        email: string
        password: string
        name: string
        role: string
        data?: Record<string, unknown>
      }
    }) => Promise<unknown>
  }
}

/**
 * Creates the very first admin for an app, before any admin exists — the one
 * case the admin() plugin's own `createUser` endpoint can't cover, since it
 * requires an already-authenticated admin session to call.
 *
 * Delegates the actual user/account creation to `auth.api.createUser` (called
 * server-side, with no request/session, so the plugin's own auth check is
 * skipped the same way a trusted server script would be) instead of inserting
 * `user`/`account` rows by hand — that keeps this in sync with whatever
 * Better Auth's internal adapter does (account.issuer, password hashing,
 * future schema changes) rather than re-deriving it and letting the two
 * drift apart.
 *
 * Serializes concurrent callers with a Postgres advisory lock: a plain
 * `WHERE NOT EXISTS` check on an empty `user` table lets two racing requests
 * both pass before either has inserted, minting two admins.
 *
 * Callers are expected to have already applied their own access gate (setup
 * token, allowed-emails list, etc.) — this function only enforces "at most
 * one admin, ever".
 */
export async function bootstrapFirstAdmin(
  auth: PlatformAuth,
  pool: BootstrapAdminPool,
  input: BootstrapFirstAdminInput,
): Promise<BootstrapFirstAdminResult> {
  const client = await pool.connect()
  try {
    await client.query("SELECT pg_advisory_lock(hashtext($1))", ["admin-setup"])

    const existing = await client.query(
      `SELECT 1 FROM "user" WHERE role = 'admin'`,
    )
    if (existing.rowCount && existing.rowCount > 0) {
      return { ok: false, error: "already_completed" }
    }

    const adminApi = (auth as unknown as AuthWithAdminCreateUser).api
    await adminApi.createUser({
      body: {
        email: input.email,
        password: input.password,
        name: input.name,
        role: "admin",
        data: { emailVerified: true },
      },
    })

    return { ok: true }
  } finally {
    await client.query("SELECT pg_advisory_unlock(hashtext($1))", ["admin-setup"])
    client.release()
  }
}
