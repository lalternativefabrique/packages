import { useEffect, useState } from "react"

/**
 * A stand-in for the on-screen keyboard, so the toolbar's behaviour under one
 * can be judged on a desktop browser.
 *
 * The simulation is the mechanism, not a picture of it: a real keyboard is
 * visible to a page only as a shrunken `visualViewport`, and that is exactly
 * what this replaces the API with. Code that reads `window.innerHeight` sees
 * nothing change here — which is the bug being demonstrated.
 */

export const KEYBOARD_HEIGHT = 300

interface FakeViewport extends EventTarget {
  height: number
  width: number
  offsetTop: number
  offsetLeft: number
  pageTop: number
  pageLeft: number
  scale: number
  onresize: null
  onscroll: null
}

let fake: FakeViewport | null = null
let real: VisualViewport | null | undefined

function ensureFake(): FakeViewport {
  if (fake) return fake
  const target = new EventTarget() as FakeViewport
  target.width = window.innerWidth
  target.height = window.innerHeight
  target.offsetTop = 0
  target.offsetLeft = 0
  target.pageTop = 0
  target.pageLeft = 0
  target.scale = 1
  target.onresize = null
  target.onscroll = null
  fake = target
  return target
}

export function setSimulatedKeyboard(open: boolean) {
  const target = ensureFake()

  if (real === undefined) real = window.visualViewport

  target.width = window.innerWidth
  target.height = window.innerHeight - (open ? KEYBOARD_HEIGHT : 0)

  Object.defineProperty(window, "visualViewport", {
    configurable: true,
    get: () => target,
  })

  target.dispatchEvent(new Event("resize"))
}

export function useSimulatedKeyboard() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    setSimulatedKeyboard(open)
    const onResize = () => setSimulatedKeyboard(open)
    window.addEventListener("resize", onResize)
    return () => window.removeEventListener("resize", onResize)
  }, [open])

  return { open, setOpen }
}

/** Drawn where the keyboard would be, over the page, as the real one is. */
export function KeyboardOverlay({ open }: { open: boolean }) {
  if (!open) return null

  const rows = ["AZERTYUIOP", "QSDFGHJKLM", "WXCVBN"]

  return (
    <div
      aria-hidden
      style={{
        position: "fixed",
        left: 0,
        right: 0,
        bottom: 0,
        height: KEYBOARD_HEIGHT,
        zIndex: 200,
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        gap: 8,
        padding: 12,
        background: "#2c2c2e",
        boxShadow: "0 -1px 0 rgb(255 255 255 / 0.12)",
      }}
    >
      {rows.map((row) => (
        <div key={row} style={{ display: "flex", justifyContent: "center", gap: 6 }}>
          {[...row].map((key) => (
            <span
              key={key}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                width: 30,
                height: 40,
                borderRadius: 5,
                background: "#4a4a4c",
                color: "#f2f2f7",
                fontSize: 15,
                fontFamily: "system-ui, sans-serif",
              }}
            >
              {key}
            </span>
          ))}
        </div>
      ))}
      <div style={{ display: "flex", justifyContent: "center" }}>
        <span
          style={{
            width: 200,
            height: 40,
            borderRadius: 5,
            background: "#4a4a4c",
          }}
        />
      </div>
    </div>
  )
}
