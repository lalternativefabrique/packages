import { afterEach, describe, expect, it, vi } from "vitest";
import { KeysRequestError, httpTransport, keyOf } from "./transport";

function answer(status: number, body: unknown) {
  return Promise.resolve(
    new Response(body === undefined ? null : JSON.stringify(body), { status }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("keyOf", () => {
  it("reads a row", () => {
    expect(
      keyOf({
        id: "ak_7f3a",
        label: "prod",
        audience: ["tornad"],
        scopes: ["tornad:search"],
        createdAt: "2026-09-20T08:00:00Z",
      }),
    ).toEqual({
      id: "ak_7f3a",
      label: "prod",
      audience: ["tornad"],
      scopes: ["tornad:search"],
      createdAt: "2026-09-20T08:00:00Z",
      expiresAt: null,
    });
  });

  // A key issued before keys were signed has no lifetime of its own, so a
  // missing expiry is "does not expire" rather than a value still to come.
  it("reads an expiry, and leaves a key without one at null", () => {
    expect(keyOf({ expiresAt: "2027-09-20T08:00:00Z" }).expiresAt).toBe(
      "2027-09-20T08:00:00Z",
    );
    expect(keyOf({}).expiresAt).toBeNull();
    expect(keyOf({ expiresAt: 0 }).expiresAt).toBeNull();
  });

  it("answers a usable row rather than throwing on junk", () => {
    expect(keyOf(null)).toEqual({
      id: "",
      label: "",
      audience: [],
      scopes: [],
      createdAt: null,
      expiresAt: null,
    });
    expect(keyOf({ scopes: [1, "ok"], createdAt: 42 })).toEqual({
      id: "",
      label: "",
      audience: [],
      scopes: ["ok"],
      createdAt: null,
      expiresAt: null,
    });
  });
});

describe("httpTransport", () => {
  it("lists the keys a product answers", async () => {
    vi.stubGlobal("fetch", () =>
      answer(200, { keys: [{ id: "ak_1", label: "prod" }] }),
    );
    const keys = await httpTransport("/api/keys").list();
    expect(keys).toHaveLength(1);
    expect(keys[0]?.id).toBe("ak_1");
  });

  it("answers an empty list when the body carries none", async () => {
    vi.stubGlobal("fetch", () => answer(200, {}));
    await expect(httpTransport("/api/keys").list()).resolves.toEqual([]);
  });

  it("returns the secret exactly once, on creation", async () => {
    vi.stubGlobal("fetch", () =>
      answer(200, { id: "ak_1", label: "prod", secret: "vv_live_abc" }),
    );
    const created = await httpTransport("/api/keys").create({ label: "prod" });
    expect(created.secret).toBe("vv_live_abc");
  });

  // A creation whose answer carries no secret is a failure, not a key: the
  // secret is never fetched again, so accepting the row would leave a key
  // nobody can use and no way to notice.
  it("refuses a creation that answers without a secret", async () => {
    vi.stubGlobal("fetch", () => answer(200, { id: "ak_1", label: "prod" }));
    await expect(
      httpTransport("/api/keys").create({ label: "prod" }),
    ).rejects.toThrow(KeysRequestError);
  });

  it("names what the status means", async () => {
    const cases: Array<[number, string]> = [
      [401, "unauthorized"],
      [403, "forbidden"],
      [422, "invalid"],
      [503, "unavailable"],
    ];
    for (const [status, kind] of cases) {
      vi.stubGlobal("fetch", () => answer(status, { error: "label" }));
      await expect(httpTransport("/api/keys").list()).rejects.toMatchObject({
        error: { kind },
      });
    }
  });

  it("reads a network failure and a non-JSON body as unavailable", async () => {
    vi.stubGlobal("fetch", () => Promise.reject(new Error("offline")));
    await expect(httpTransport("/api/keys").list()).rejects.toMatchObject({
      error: { kind: "unavailable" },
    });

    vi.stubGlobal("fetch", () =>
      Promise.resolve(new Response("<html>", { status: 200 })),
    );
    await expect(httpTransport("/api/keys").list()).rejects.toMatchObject({
      error: { kind: "unavailable" },
    });
  });

  it("escapes the id it revokes", async () => {
    const seen: Array<string> = [];
    vi.stubGlobal("fetch", (url: string) => {
      seen.push(url);
      return answer(204, undefined);
    });
    await httpTransport("/api/keys/").revoke("ak/../admin");
    expect(seen[0]).toBe("/api/keys/ak%2F..%2Fadmin");
  });
});
