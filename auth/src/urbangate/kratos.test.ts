import { test } from "node:test";
import assert from "node:assert/strict";
import { KratosFlows, codeWasSent, failureOf } from "./kratos.ts";

test("failureOf reads Kratos' message ids", () => {
  assert.deepEqual(
    failureOf(400, { ui: { messages: [{ id: 4000006, type: "error" }] } }),
    {
      status: "invalid_credentials",
    },
  );
  assert.deepEqual(
    failureOf(400, {
      ui: { nodes: [{ messages: [{ id: 4010008, type: "error" }] }] },
    }),
    { status: "invalid_code" },
  );
  assert.deepEqual(
    failureOf(400, { ui: { messages: [{ id: 4000007, type: "error" }] } }),
    {
      status: "already_registered",
    },
  );
  assert.deepEqual(
    failureOf(400, {
      ui: {
        nodes: [
          { messages: [{ id: 4000005, type: "error", text: "too short" }] },
        ],
      },
    }),
    { status: "password_refused", message: "too short" },
  );
  assert.deepEqual(failureOf(410, {}), { status: "flow_expired" });
  assert.deepEqual(failureOf(403, { error: { id: "session_aal2_required" } }), {
    status: "second_factor_required",
  });
  assert.deepEqual(failureOf(502, {}), { status: "unavailable" });
  assert.deepEqual(
    failureOf(400, { ui: { messages: [{ id: 1, type: "error", text: "x" }] } }),
    {
      status: "invalid_input",
      message: "x",
    },
  );
});

test("codeWasSent recognises the sent-code message", () => {
  assert.equal(
    codeWasSent({ id: "f", ui: { messages: [{ id: 1010014, type: "info" }] } }),
    true,
  );
  assert.equal(codeWasSent({ id: "f" }), false);
});

test("a submitted flow answered 400 becomes a KratosError, a 422 with continue_with is a success", async () => {
  const fetchImpl = (async (url: URL | string) => {
    if (String(url).includes("/self-service/login/api")) {
      return Response.json({ id: "flow-1" });
    }
    if (String(url).includes("/self-service/login?flow=flow-1")) {
      return Response.json(
        { ui: { messages: [{ id: 4000006, type: "error" }] } },
        { status: 400 },
      );
    }
    return Response.json(
      { continue_with: [{ action: "show_settings_ui", flow: { id: "s" } }] },
      { status: 422 },
    );
  }) as typeof fetch;
  const kratos = new KratosFlows("http://kratos:4433", fetchImpl);
  const flow = await kratos.start("login");
  assert.equal(flow.id, "flow-1");
  await assert.rejects(
    kratos.submit("login", "flow-1", { method: "password" }),
    (e: Error & { failure?: unknown }) => {
      assert.deepEqual(e.failure, { status: "invalid_credentials" });
      return true;
    },
  );
  const recovered = await kratos.submit<{
    continue_with: Array<{ action: string }>;
  }>("recovery", "r", {});
  assert.equal(recovered.continue_with[0].action, "show_settings_ui");
});

test("whoami answers null for 401 and throws unavailable when Kratos is out", async () => {
  const gone = new KratosFlows(
    "http://kratos:4433",
    (async () => new Response("", { status: 401 })) as typeof fetch,
  );
  assert.equal(await gone.whoami("t"), null);
  const down = new KratosFlows("http://kratos:4433", (async () => {
    throw new Error("ECONNREFUSED");
  }) as typeof fetch);
  await assert.rejects(
    down.whoami("t"),
    (e: Error & { failure?: { status: string } }) =>
      e.failure?.status === "unavailable",
  );
});
