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
