import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import type { Plugin } from "vite"

// The first deletion fails on the data step, the retry finishes it: the
// screen can be walked through both of its outcomes.
function demoAccountDeletion(): Plugin {
  let attempts = 0
  return {
    name: "demo-account-deletion",
    configureServer(server) {
      server.middlewares.use("/demo/delete-account", (_req, res) => {
        attempts += 1
        const done = attempts % 2 === 0
        setTimeout(() => {
          res.setHeader("Content-Type", "application/json")
          res.end(
            JSON.stringify({
              deleted: done,
              steps: [
                { id: "billing", status: "done" },
                { id: "data", status: done ? "done" : "failed" },
              ],
            }),
          )
        }, 900)
      })
    },
  }
}

// Rooted at playground/ but importing straight from ../src: the screens render
// from source, so a save shows up without tsup in the loop at all.
export default defineConfig({
  root: __dirname,
  plugins: [react(), tailwindcss(), demoAccountDeletion()],
  server: { port: 5199, host: "0.0.0.0" },
})
