/**
 * Fills in the display name of an account signed up without one.
 *
 * The sign-up form treats the name as optional and omits the key when it is
 * left blank, but Better Auth's user schema requires it and rejects the
 * request with `[body.name] Invalid input`. Naming the account is the server's
 * call, so the default is applied here rather than invented by the client.
 *
 * The local part of the address is the closest thing to a name the person has
 * actually given us. It is only a starting label: they can change it later,
 * and nothing keys off it.
 */
export function withSignUpName<T extends { email?: unknown; name?: unknown }>(
  body: T,
): T & { name: string } {
  const name = typeof body.name === "string" ? body.name.trim() : ""
  if (name) return { ...body, name }

  const email = typeof body.email === "string" ? body.email.trim() : ""
  const at = email.lastIndexOf("@")
  const localPart = at > 0 ? email.slice(0, at) : email

  return { ...body, name: localPart }
}
