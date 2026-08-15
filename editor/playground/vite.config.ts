import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// Rooted at playground/ but importing straight from ../src: the screens render
// from source, so a save shows up without tsup in the loop at all.
export default defineConfig({
  root: __dirname,
  plugins: [react()],
  server: { port: 5198, host: "0.0.0.0" },
})
