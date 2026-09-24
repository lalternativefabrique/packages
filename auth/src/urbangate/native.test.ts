import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createUrbangateNativeClient,
  parseSetCookie,
  splitSetCookie,
  type NativeStorage,
} from "./native.ts";

function memoryStorage(): NativeStorage & { items: Map<string, string> } {
  const items = new Map<string, string>();
  return {
    items,
    getItem: async (k) => items.get(k) ?? null,
    setItem: async (k, v) => {
      items.set(k, v);
    },
    deleteItem: async (k) => {
      items.delete(k);
    },
  };
}

type Seen = { url: string; init: RequestInit };

function server(answer: (seen: Seen) => Response) {
  const seen: Array<Seen> = [];
  const fetchImpl = (async (url: string, init: RequestInit) => {
    const call = { url, init };
    seen.push(call);
    return answer(call);
  }) as unknown as typeof fetch;
  return { seen, fetchImpl };
}

function answer(status: number, body: unknown, cookies: Array<string> = []) {
  const headers = new Headers({ "content-type": "application/json" });
  for (const c of cookies) headers.append("set-cookie", c);
  return new Response(JSON.stringify(body), { status, headers });
}

function cookieOf(seen: Seen): string | null {
  return new Headers(seen.init.headers).get("cookie");
}

test("splitSetCookie keeps an Expires date whole", () => {
  assert.deepEqual(
    splitSetCookie(
      "a=1; Expires=Wed, 21 Oct 2026 07:28:00 GMT; Path=/, b=2; Max-Age=0",
    ),
    ["a=1; Expires=Wed, 21 Oct 2026 07:28:00 GMT; Path=/", "b=2; Max-Age=0"],
  );
});

test("parseSetCookie decodes the value and reads a clear as deleted", () => {
  assert.deepEqual(parseSetCookie("s=ory%3Dst; Path=/; Max-Age=10"), {
    name: "s",
    value: "ory=st",
    deleted: false,
  });
  assert.equal(parseSetCookie("s=; Path=/; Max-Age=0").deleted, true);
  assert.equal(
    parseSetCookie("s=v; Expires=Thu, 01 Jan 1970 00:00:00 GMT").deleted,
    true,
  );
});

test("a sign-in keeps the session and replays it without the OS cookie jar", async () => {
  const storage = memoryStorage();
  const { seen, fetchImpl } = server(({ url }) =>
    url.endsWith("sign-in/email")
      ? answer(200, { ok: true }, [
          "lalter_session=ory_st_1; Path=/; HttpOnly; Max-Age=2592000",
          "lalter_flow=; Path=/; HttpOnly; Max-Age=0",
          "other=x; Path=/",
        ])
      : answer(200, null),
  );
  const client = createUrbangateNativeClient({
    baseURL: "https://lalter.fr/",
    product: "lalter",
    storage,
    fetch: fetchImpl,
  });

  const signIn = await client.signIn.email({ email: "a@b.c", password: "pw" });
  assert.equal(signIn.error, null);
  assert.deepEqual([...storage.items], [["lalter_session", "ory_st_1"]]);

  await client.getSession();
  const [first, second] = seen;
  assert.equal(first?.url, "https://lalter.fr/api/auth/sign-in/email");
  assert.equal(cookieOf(first!), null);
  assert.equal(second?.url, "https://lalter.fr/api/auth/get-session");
  assert.equal(second?.init.credentials, "omit");
  assert.equal(cookieOf(second!), "lalter_session=ory_st_1");
});

test("the flow a code was sent for comes back on the code step", async () => {
  const storage = memoryStorage();
  const { seen, fetchImpl } = server(({ url }) =>
    url.endsWith("send-verification-otp")
      ? answer(200, { ok: true }, ["lalter_flow=login%3Aabc; Max-Age=600"])
      : answer(200, { ok: true }),
  );
  const client = createUrbangateNativeClient({
    baseURL: "https://lalter.fr",
    product: "lalter",
    storage,
    fetch: fetchImpl,
  });

  await client.emailOtp.sendVerificationOtp({
    email: "a@b.c",
    type: "sign-in",
  });
  await client.signIn.emailOtp({ email: "a@b.c", otp: "123456" });
  assert.equal(cookieOf(seen[1]!), "lalter_flow=login%3Aabc");
});

test("a refusal comes back as the server's error", async () => {
  const { fetchImpl } = server(() =>
    answer(401, { error: { code: "invalid_credentials", status: 401 } }),
  );
  const client = createUrbangateNativeClient({
    baseURL: "https://lalter.fr",
    product: "lalter",
    storage: memoryStorage(),
    fetch: fetchImpl,
  });

  const { error } = await client.signIn.email({ email: "a@b.c", password: "x" });
  assert.deepEqual(error, { code: "invalid_credentials", status: 401 });
});

test("fetch carries the session to the core proxy and keeps the renewed token", async () => {
  const storage = memoryStorage();
  await storage.setItem("lalter_session", "ory_st_1");
  const { seen, fetchImpl } = server(() =>
    answer(200, [], ["lalter_token=jwt.2; Max-Age=900"]),
  );
  const client = createUrbangateNativeClient({
    baseURL: "https://lalter.fr",
    product: "lalter",
    storage,
    fetch: fetchImpl,
  });

  await client.fetch("/api/core/api/v1/chat/conversations", {
    headers: { accept: "application/json" },
  });
  const call = seen[0]!;
  assert.equal(call.url, "https://lalter.fr/api/core/api/v1/chat/conversations");
  assert.equal(new Headers(call.init.headers).get("accept"), "application/json");
  assert.equal(cookieOf(call), "lalter_session=ory_st_1");
  assert.equal(storage.items.get("lalter_token"), "jwt.2");
});

test("sign-out forgets everything even when the server cannot answer", async () => {
  const storage = memoryStorage();
  await storage.setItem("lalter_session", "ory_st_1");
  await storage.setItem("lalter_token", "jwt.1");
  const { fetchImpl } = server(() => {
    throw new TypeError("Network request failed");
  });
  const client = createUrbangateNativeClient({
    baseURL: "https://lalter.fr",
    product: "lalter",
    storage,
    fetch: fetchImpl,
  });

  const { error } = await client.signOut();
  assert.equal(error?.status, 0);
  assert.equal(storage.items.size, 0);
});
