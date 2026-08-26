import { createServer, type Server } from "node:http";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { configureLalterClient } from "./http-client";
import { getCoreAPI } from "./generated/lalter";

// The README documents getCoreAPI() as the entry point for every generated
// call — a wrapper factory, not top-level exports like sendChatMessage. This
// test exists because that shape is easy to get wrong when hand-writing
// usage examples: orval's split mode wraps every generated function in
// `export const getCoreAPI = () => { ... return {...} }`, so
// `import { createTask } from "@lalternative/lalter-sdk"` compiles to
// nothing — there is no such top-level export.
describe("getCoreAPI", () => {
  let server: Server;
  let baseURL: string;
  let received: { path?: string; body?: string } = {};

  beforeEach(async () => {
    received = {};
    server = createServer((req, res) => {
      received.path = req.url;
      const chunks: Buffer[] = [];
      req.on("data", (c) => chunks.push(c));
      req.on("end", () => {
        received.body = Buffer.concat(chunks).toString("utf8");
        res.writeHead(202, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ id: "t-1", status: "queued" }));
      });
    });
    await new Promise<void>((resolve) => server.listen(0, resolve));
    const addr = server.address();
    if (typeof addr !== "object" || addr === null) throw new Error("no address");
    baseURL = `http://127.0.0.1:${addr.port}`;
    configureLalterClient({ baseURL, apiKey: "lalter_sk_x" });
  });

  afterEach(() => new Promise<void>((resolve) => server.close(() => resolve())));

  it("sends mcp_servers on createTask", async () => {
    const api = getCoreAPI();
    await api.createTask({
      kind: "fix",
      prompt: "p",
      repo_url: "https://example.test/r.git",
      mcp_servers: ["skalpai-logs"],
    });

    expect(received.path).toBe("/api/v1/tasks");
    const body = JSON.parse(received.body ?? "{}");
    expect(body.mcp_servers).toEqual(["skalpai-logs"]);
  });
});
