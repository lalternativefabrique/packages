import { useEffect, useState } from "react"
import { createRoot } from "react-dom/client"
import { EditorContent, useEditor } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"

import { PluginKey } from "@tiptap/pm/state"

import { SelectionToolbar } from "../src/SelectionToolbar"
import { useSelectionToolbar } from "../src/useSelectionToolbar"
import { blockFormats } from "../src/formats"
import { SlashCommands } from "../src/SlashCommands"
import { defaultSlashItems } from "../src/slash"
import { EditorScreen, type SaveState } from "../src/EditorScreen"
import { KEYBOARD_HEIGHT, KeyboardOverlay, useSimulatedKeyboard } from "./keyboard"
import { SAMPLE } from "./sample"
import "./styles.css"

const SLASH_ITEMS = defaultSlashItems({
  descriptions: {
    heading1: "Grand titre de section",
    heading2: "Sous-titre",
    bulletList: "Liste non ordonnée",
    orderedList: "Liste ordonnée",
    blockquote: "Bloc de citation",
    codeBlock: "Code monospace",
    horizontalRule: "Ligne horizontale",
  },
})

const slashCommands = SlashCommands.configure({
  items: () => SLASH_ITEMS,
  pluginKey: new PluginKey("playgroundSlash"),
})

function Playground() {
  const [phone, setPhone] = useState(true)
  const [preferBelow, setPreferBelow] = useState(true)
  const [title, setTitle] = useState("Pourquoi l'installation de Revol…")
  const [save, setSave] = useState<SaveState>("saved")
  const keyboard = useSimulatedKeyboard()

  const editor = useEditor({
    extensions: [StarterKit, slashCommands],
    content: SAMPLE,
    autofocus: false,
  })

  const selection = useSelectionToolbar(editor)

  return (
    <>
      <Controls
        phone={phone}
        onPhone={setPhone}
        preferBelow={preferBelow}
        onPreferBelow={setPreferBelow}
        keyboardOpen={keyboard.open}
        onKeyboard={keyboard.setOpen}
        hasSelection={selection !== null}
      />

      <div className={`pg-stage${phone ? " pg-force-touch" : ""}`}>
        <div className={`pg-screen${phone ? " pg-screen--phone" : ""}`}>
          <EditorScreen
            fill
            title={title}
            onTitleChange={(next) => {
              setTitle(next)
              setSave("dirty")
              window.setTimeout(() => setSave("saving"), 400)
              window.setTimeout(() => setSave("saved"), 900)
            }}
            saveState={save}
            lead={<button type="button" className="pg-btn">‹</button>}
            status={`${editor?.storage.characterCount?.characters?.() ?? 0}`}
            actions={
              <>
                <button type="button" className="pg-btn pg-btn--primary">
                  Programmer
                </button>
                <button type="button" className="pg-btn" aria-label="Plus">
                  ⋯
                </button>
              </>
            }
            subnav={
              <div className="pg-tabs">
                <button type="button" className="pg-tab pg-tab--active">
                  Ton texte
                </button>
                <button type="button" className="pg-tab">LinkedIn</button>
                <button type="button" className="pg-tab">Article</button>
              </div>
            }
          >
            <EditorContent editor={editor} />
          </EditorScreen>
        </div>
      </div>

      {selection && (
        <SelectionToolbar
          selectedText={selection.text}
          selection={selection.rect}
          preferBelow={preferBelow}
          actions={[
            {
              id: "revise",
              label: "Corriger",
              icon: <span aria-hidden>✦</span>,
              onSelect: () => undefined,
            },
            {
              id: "bold",
              label: "B",
              onSelect: () => editor?.chain().focus().toggleBold().run(),
            },
            {
              id: "italic",
              label: "I",
              onSelect: () => editor?.chain().focus().toggleItalic().run(),
            },
          ]}
          formats={blockFormats(editor)}
          formatsLabel="Format"
        />
      )}

      <KeyboardOverlay open={keyboard.open} />
    </>
  )
}

// Every control keeps the focus where it is: the toolbar only exists while the
// editor holds a selection, and a button that stole focus would dismiss the
// thing being inspected the moment it is toggled.
function keepFocus(e: React.MouseEvent) {
  e.preventDefault()
}

interface ControlsProps {
  phone: boolean
  onPhone: (v: boolean) => void
  preferBelow: boolean
  onPreferBelow: (v: boolean) => void
  keyboardOpen: boolean
  onKeyboard: (v: boolean) => void
  hasSelection: boolean
}

function Controls({
  phone,
  onPhone,
  preferBelow,
  onPreferBelow,
  keyboardOpen,
  onKeyboard,
  hasSelection,
}: ControlsProps) {
  const metrics = useViewportMetrics()
  const bar = useToolbarPosition(hasSelection)

  const covered = metrics.inner - metrics.visual
  // Read off the rendered bar rather than deduced: the claim is that it is
  // visible, and only its own box says so.
  const hidden = bar !== null && bar.bottom > metrics.visual + 1

  return (
    <div className="pg-controls">
      <div className="pg-row">
        <span className="pg-label">Écran</span>
        <button
          className="pg-btn"
          aria-pressed={phone}
          onMouseDown={keepFocus}
          onClick={() => onPhone(true)}
        >
          Téléphone
        </button>
        <button
          className="pg-btn"
          aria-pressed={!phone}
          onMouseDown={keepFocus}
          onClick={() => onPhone(false)}
        >
          Bureau
        </button>
      </div>

      <div className="pg-row">
        <span className="pg-label">Clavier</span>
        <button
          className="pg-btn"
          aria-pressed={keyboardOpen}
          onMouseDown={keepFocus}
          onClick={() => onKeyboard(!keyboardOpen)}
        >
          {keyboardOpen ? `Ouvert (${KEYBOARD_HEIGHT}px)` : "Fermé"}
        </button>
      </div>

      <div className="pg-row">
        <span className="pg-label">Callout iOS</span>
        <button
          className="pg-btn"
          aria-pressed={preferBelow}
          onMouseDown={keepFocus}
          onClick={() => onPreferBelow(!preferBelow)}
        >
          {preferBelow ? "Barre dessous" : "Barre dessus"}
        </button>
      </div>

      <div className="pg-readout">
        <span>window.innerHeight</span>
        <b>{metrics.inner}px</b>
        <span>visualViewport.height</span>
        <b>{metrics.visual}px</b>
        <span>caché par le clavier</span>
        <b>{covered}px</b>
        {bar && (
          <>
            <span>bas de la barre</span>
            <b>{Math.round(bar.bottom)}px</b>
          </>
        )}
      </div>

      {bar ? (
        <div className={`pg-verdict pg-verdict--${hidden ? "bad" : "good"}`}>
          {hidden
            ? `Barre cachée — ${Math.round(bar.bottom - metrics.visual)}px sous le clavier`
            : "Barre visible"}
        </div>
      ) : (
        <div className="pg-readout" style={{ borderTop: "none", paddingTop: 0 }}>
          <span style={{ gridColumn: "1 / -1" }}>
            {hasSelection ? "Barre absente" : "Sélectionne du texte"}
          </span>
        </div>
      )}
    </div>
  )
}

function useToolbarPosition(hasSelection: boolean) {
  const [box, setBox] = useState<{ top: number; bottom: number } | null>(null)

  useEffect(() => {
    const read = () => {
      const el = document.querySelector(".lalt-toolbar")
      if (!el) {
        setBox(null)
        return
      }
      const rect = el.getBoundingClientRect()
      setBox({ top: rect.top, bottom: rect.bottom })
    }
    read()
    const timer = window.setInterval(read, 150)
    return () => window.clearInterval(timer)
  }, [hasSelection])

  return box
}

function useViewportMetrics() {
  const read = () => ({
    inner: window.innerHeight,
    visual: Math.round(window.visualViewport?.height ?? window.innerHeight),
  })

  const [metrics, setMetrics] = useState(read)

  useEffect(() => {
    const update = () => setMetrics(read())
    update()
    window.addEventListener("resize", update)
    const vv = window.visualViewport
    vv?.addEventListener("resize", update)
    vv?.addEventListener("scroll", update)
    // The simulator swaps the object itself, so a poll is what notices; the
    // listeners above are bound to whichever instance was current at mount.
    const timer = window.setInterval(update, 200)
    return () => {
      window.removeEventListener("resize", update)
      vv?.removeEventListener("resize", update)
      vv?.removeEventListener("scroll", update)
      window.clearInterval(timer)
    }
  }, [])

  return metrics
}

createRoot(document.getElementById("root")!).render(<Playground />)
