import { test } from "node:test"
import assert from "node:assert/strict"
import { runAccountDeletion } from "./account-deletion.ts"

test("a product that bills nobody shows only the data step", async () => {
  const report = await runAccountDeletion({ deleteData: async () => {} })
  assert.deepEqual(report, {
    deleted: true,
    steps: [{ id: "data", status: "done" }],
  })
})

test("a failed data step reports billing done and hands the error over", async () => {
  const seen: Array<string> = []
  const report = await runAccountDeletion({
    cancelBilling: async () => {},
    deleteData: async () => {
      throw new Error("core down")
    },
    onError: (step) => void seen.push(step),
  })
  assert.deepEqual(report, {
    deleted: false,
    steps: [
      { id: "billing", status: "done" },
      { id: "data", status: "failed" },
    ],
  })
  assert.deepEqual(seen, ["data"])
})
