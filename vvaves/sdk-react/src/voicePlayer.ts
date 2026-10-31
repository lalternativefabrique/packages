import { FRAMES_CONTENT_TYPE, readFrames } from './frames.ts'
import type { VoiceSource } from './speak'
import {
  appendPiece,
  bufferedSeconds,
  clampRate,
  clampSeek,
  planFrom,
  positionAt,
  type Anchor,
  type Piece,
} from './timeline.ts'

export type VoicePlaybackState = 'idle' | 'loading' | 'playing' | 'paused' | 'unavailable'

export type VoiceSnapshot = {
  state: VoicePlaybackState
  position: number
  buffered: number
  duration: number | null
  rate: number
  text: string | null
}

export type VoicePlayer = {
  getSnapshot: () => VoiceSnapshot
  subscribe: (listener: () => void) => () => void
  toggle: () => void
  stop: () => void
  seek: (seconds: number) => void
  skip: (delta: number) => void
  setRate: (rate: number) => void
}

type Session = {
  context: AudioContext
  controller: AbortController
  cancelled: boolean
  mode: 'loading' | 'playing' | 'paused'
  buffers: AudioBuffer[]
  pieces: Piece[]
  streamDone: boolean
  sources: Set<AudioBufferSourceNode>
  anchor: Anchor
  pausedAt: number
  nextStartAt: number
  ticker: ReturnType<typeof setInterval> | null
}

const TICK_MS = 250

/**
 * Plays a reading aloud piece by piece as vvaves streams it, so listening
 * starts on the first piece instead of after the whole synthesis.
 *
 * Decoded with Web Audio rather than MediaSource: mp3 in a SourceBuffer is
 * inconsistent across desktop browsers, and MediaSource is unavailable on
 * iOS Safari and in a Tauri webview, while decodeAudioData works everywhere.
 *
 * iOS hands back a context that is suspended until a gesture resumes it, and
 * only a gesture does: resuming after the first await is already too late,
 * and the reading then plays to nobody with nothing reported. Pausing stops
 * the sources rather than suspending the context, so the context never has
 * to be resumed again outside a gesture.
 *
 * resolve is called on each press rather than once: the URL it returns is
 * signed and expires, so one resolved when the text was rendered would have
 * gone stale by the time someone presses play.
 */
export function createVoicePlayer(resolve: () => Promise<VoiceSource>): VoicePlayer {
  let snapshot: VoiceSnapshot = { state: 'idle', position: 0, buffered: 0, duration: null, rate: 1, text: null }
  let session: Session | null = null
  let rate = 1
  const listeners = new Set<() => void>()

  const set = (patch: Partial<VoiceSnapshot>) => {
    snapshot = { ...snapshot, ...patch }
    for (const listener of listeners) listener()
  }

  const currentPosition = (s: Session) => {
    if (s.mode !== 'playing') return s.pausedAt
    return positionAt(s.anchor, s.context.currentTime, rate, bufferedSeconds(s.pieces))
  }

  const silence = (s: Session) => {
    for (const source of s.sources) {
      source.onended = null
      source.stop()
    }
    s.sources.clear()
    if (s.ticker !== null) clearInterval(s.ticker)
    s.ticker = null
  }

  const teardown = (s: Session) => {
    s.cancelled = true
    silence(s)
    s.controller.abort()
    s.context.close().catch(() => {})
    if (session === s) session = null
    set({ position: 0, buffered: 0, duration: null, text: null })
  }

  const stop = () => {
    const s = session
    if (s) teardown(s)
    if (snapshot.state !== 'unavailable') set({ state: 'idle' })
  }

  const finishIfDone = (s: Session) => {
    if (s.cancelled || !s.streamDone || s.mode !== 'playing' || s.sources.size > 0) return
    teardown(s)
    set({ state: 'idle' })
  }

  const startPiece = (s: Session, index: number, when: number, offset: number) => {
    const source = s.context.createBufferSource()
    source.buffer = s.buffers[index]
    source.playbackRate.value = rate
    source.connect(s.context.destination)
    source.onended = () => {
      s.sources.delete(source)
      finishIfDone(s)
    }
    s.sources.add(source)
    source.start(when, offset)
  }

  const playFrom = (s: Session, offset: number) => {
    silence(s)
    s.mode = 'playing'
    const now = s.context.currentTime
    s.anchor = { offset, contextTime: now }
    const plan = planFrom(s.pieces, offset, now, rate)
    for (const p of plan.scheduled) startPiece(s, p.index, p.when, p.offset)
    s.nextStartAt = plan.nextStartAt
    s.ticker = setInterval(() => set({ position: currentPosition(s) }), TICK_MS)
    set({ position: offset })
    set({ state: 'playing' })
    finishIfDone(s)
  }

  const onDecoded = (s: Session, buffer: AudioBuffer) => {
    s.buffers.push(buffer)
    const piece = appendPiece(s.pieces, buffer.duration)
    set({ buffered: bufferedSeconds(s.pieces) })
    if (s.mode === 'loading') {
      playFrom(s, 0)
      return
    }
    if (s.mode !== 'playing') return
    const now = s.context.currentTime
    const startAt = Math.max(s.nextStartAt, now)
    if (startAt > s.nextStartAt) s.anchor = { offset: piece.start, contextTime: startAt }
    startPiece(s, s.buffers.length - 1, startAt, 0)
    s.nextStartAt = startAt + buffer.duration / rate
  }

  const play = () => {
    set({ state: 'loading' })
    const Ctor: typeof AudioContext | undefined =
      typeof AudioContext !== 'undefined'
        ? AudioContext
        : (globalThis as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) {
      set({ state: 'unavailable' })
      return
    }
    const { audioSession } = navigator as { audioSession?: { type: string } }
    // iOS mutes Web Audio on the speaker under the silent switch unless the session is media playback.
    if (audioSession) audioSession.type = 'playback'
    const context = new Ctor()
    // Still inside the gesture that called play(): a resume() issued after
    // the first await is refused on iOS, and every source then plays silent.
    const resumed = context.resume().catch(() => {})

    const s: Session = {
      context,
      controller: new AbortController(),
      cancelled: false,
      mode: 'loading',
      buffers: [],
      pieces: [],
      streamDone: false,
      sources: new Set(),
      anchor: { offset: 0, contextTime: 0 },
      pausedAt: 0,
      nextStartAt: 0,
      ticker: null,
    }
    session = s

    let decoding: Promise<void> = Promise.resolve()
    const decode = (bytes: ArrayBuffer) => {
      decoding = decoding.then(async () => {
        let buffer: AudioBuffer
        try {
          buffer = await context.decodeAudioData(bytes)
        } catch {
          return
        }
        if (!s.cancelled) onDecoded(s, buffer)
      })
    }

    ;(async () => {
      try {
        await resumed
        const reading = await resolve()
        if (!s.cancelled) set({ text: reading.text ?? null })
        // No credentials: the audio comes from another origin, and what
        // authorises the request is the signature already in the URL.
        const res = await fetch(reading.url, {
          method: 'POST',
          signal: s.controller.signal,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(reading.body),
        })
        if (!res.ok || !res.body) {
          throw new Error(`unexpected response (${res.status})`)
        }
        const contentType = res.headers.get('Content-Type') ?? ''
        if (contentType === FRAMES_CONTENT_TYPE) {
          await readFrames(
            res.body.getReader(),
            // decodeAudioData detaches the buffer it is given, which would
            // corrupt later frames sharing the same backing ArrayBuffer.
            (frame) => decode(frame.slice().buffer),
            s.controller.signal,
          )
        } else {
          // A reading vvaves already keeps comes back whole, as one audio
          // file with a Content-Length, whatever the request asked for.
          if (!contentType.startsWith('audio/')) {
            throw new Error(`unexpected response (${res.status}, ${contentType})`)
          }
          decode(await res.arrayBuffer())
        }
        await decoding
        if (s.cancelled) return
        if (s.buffers.length === 0) throw new Error('no playable audio')
        s.streamDone = true
        set({ duration: bufferedSeconds(s.pieces) })
        finishIfDone(s)
      } catch {
        if (s.cancelled) return
        teardown(s)
        set({ state: 'unavailable' })
      }
    })()
  }

  const pause = (s: Session) => {
    s.pausedAt = currentPosition(s)
    silence(s)
    s.mode = 'paused'
    set({ position: s.pausedAt })
    set({ state: 'paused' })
  }

  const toggle = () => {
    const s = session
    if (!s) {
      play()
      return
    }
    if (s.mode === 'loading') {
      stop()
      return
    }
    if (s.mode === 'playing') {
      pause(s)
      return
    }
    s.context.resume().catch(() => {})
    playFrom(s, s.pausedAt)
  }

  const seek = (seconds: number) => {
    const s = session
    if (!s || s.mode === 'loading') return
    const target = clampSeek(seconds, bufferedSeconds(s.pieces))
    if (s.mode === 'playing') {
      playFrom(s, target)
      return
    }
    s.pausedAt = target
    set({ position: target })
  }

  const skip = (delta: number) => {
    const s = session
    if (!s) return
    seek(currentPosition(s) + delta)
  }

  const setRate = (next: number) => {
    const clamped = clampRate(next)
    const s = session
    const at = s && s.mode === 'playing' ? currentPosition(s) : null
    rate = clamped
    set({ rate: clamped })
        if (s && at !== null) playFrom(s, at)
  }

  return {
    getSnapshot: () => snapshot,
    subscribe(listener) {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    toggle,
    stop,
    seek,
    skip,
    setRate,
  }
}
