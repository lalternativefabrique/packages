import { test } from "node:test";
import assert from "node:assert/strict";
import { clearCookie, readCookie, serializeCookie } from "./cookies.ts";

test("readCookie finds a value among several and decodes it", () => {
  const headers = new Headers({ cookie: "a=1; tornad_session=ory%3Dst; b=2" });
  assert.equal(readCookie(headers, "tornad_session"), "ory=st");
  assert.equal(readCookie(headers, "missing"), undefined);
});

test("serializeCookie is httpOnly, Lax, secure on demand, and a clear expires it", () => {
  assert.equal(
    serializeCookie("s", "v", { maxAge: 10, secure: true }),
    "s=v; Path=/; HttpOnly; SameSite=Lax; Max-Age=10; Secure",
  );
  assert.equal(
    clearCookie("s", false),
    "s=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0",
  );
});

test("a domain shares the cookie across sub-domains, on set and on clear", () => {
  assert.equal(
    serializeCookie("s", "v", { maxAge: 10, secure: true, domain: ".messag.eco" }),
    "s=v; Path=/; HttpOnly; SameSite=Lax; Max-Age=10; Domain=.messag.eco; Secure",
  );
  assert.equal(
    serializeCookie("s", "v", { maxAge: 10, secure: true, domain: "messag.eco" }),
    "s=v; Path=/; HttpOnly; SameSite=Lax; Max-Age=10; Domain=messag.eco; Secure",
  );
  assert.equal(
    clearCookie("s", true, ".messag.eco"),
    "s=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0; Domain=.messag.eco; Secure",
  );
});

test("a malformed domain is refused rather than written into the header", () => {
  // Attributes smuggled after the domain, and a CRLF-injected second header:
  // the domain is concatenated raw, so an unchecked value would land verbatim.
  for (const domain of [
    "messag.eco; Domain=attacker.example",
    "messag.eco\r\nSet-Cookie: pwned=1",
    "messag.eco Path=/",
    "not a domain",
    "localhost",
    "",
  ]) {
    assert.throws(
      () => serializeCookie("s", "v", { maxAge: 10, secure: true, domain }),
      /invalid cookie domain/,
      `expected ${JSON.stringify(domain)} to be refused`,
    );
    assert.throws(() => clearCookie("s", true, domain), /invalid cookie domain/);
  }
});
