import { useCallback, useEffect, useRef, useState } from 'react'
import { FRAMES_CONTENT_TYPE, readFrames } from './frames'
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
} from './timeline'

export type VoicePlaybackState = 'idle' | 'loading' | 'playing' | 'paused' | 'unavailable'

export type VoicePlayback = {
  state: VoicePlaybackState
  toggle: () => void
  stop: () => void
  position: number
  buffered: number
  duration: number | null
  seek: (seconds: number) => void
  skip: (delta: number) => void
  rate: number
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
export function useVoicePlayback(resolve: () => Promise<VoiceSource>): VoicePlayback {
  const [state, setState] = useState<VoicePlaybackState>('idle')
  const [position, setPosition] = useState(0)
  const [buffered, setBuffered] = useState(0)
  const [duration, setDuration] = useState<number | null>(null)
  const [rate, setRateState] = useState(1)
  const sessionRef = useRef<Session | null>(null)
  const rateRef = useRef(1)

  const currentPosition = useCallback((s: Session) => {
    if (s.mode !== 'playing') return s.pausedAt
    return positionAt(s.anchor, s.context.currentTime, rateRef.current, bufferedSeconds(s.pieces))
  }, [])

  const silence = useCallback((s: Session) => {
    for (const source of s.sources) {
      source.onended = null
      source.stop()
    }
    s.sources.clear()
    if (s.ticker !== null) clearInterval(s.ticker)
    s.ticker = null
  }, [])

  const teardown = useCallback(
    (s: Session) => {
      s.cancelled = true
      silence(s)
      s.controller.abort()
      s.context.close().catch(() => {})
      if (sessionRef.current === s) sessionRef.current = null
      setPosition(0)
      setBuffered(0)
      setDuration(null)
    },
    [silence],
  )

  const stop = useCallback(() => {
    const s = sessionRef.current
    if (s) teardown(s)
    setState((prev) => (prev === 'unavailable' ? prev : 'idle'))
  }, [teardown])

  const finishIfDone = useCallback(
    (s: Session) => {
      if (s.cancelled || !s.streamDone || s.mode !== 'playing' || s.sources.size > 0) return
      teardown(s)
      setState('idle')
    },
    [teardown],
  )

  const startPiece = useCallback(
    (s: Session, index: number, when: number, offset: number) => {
      const source = s.context.createBufferSource()
      source.buffer = s.buffers[index]
      source.playbackRate.value = rateRef.current
      source.connect(s.context.destination)
      source.onended = () => {
        s.sources.delete(source)
        finishIfDone(s)
      }
      s.sources.add(source)
      source.start(when, offset)
    },
    [finishIfDone],
  )

  const playFrom = useCallback(
    (s: Session, offset: number) => {
      silence(s)
      s.mode = 'playing'
      const now = s.context.currentTime
      s.anchor = { offset, contextTime: now }
      const plan = planFrom(s.pieces, offset, now, rateRef.current)
      for (const p of plan.scheduled) startPiece(s, p.index, p.when, p.offset)
      s.nextStartAt = plan.nextStartAt
      s.ticker = setInterval(() => setPosition(currentPosition(s)), TICK_MS)
      setPosition(offset)
      setState('playing')
      finishIfDone(s)
    },
    [silence, startPiece, currentPosition, finishIfDone],
  )

  const onDecoded = useCallback(
    (s: Session, buffer: AudioBuffer) => {
      s.buffers.push(buffer)
      const piece = appendPiece(s.pieces, buffer.duration)
      setBuffered(bufferedSeconds(s.pieces))
      if (s.mode === 'loading') {
        playFrom(s, 0)
        return
      }
      if (s.mode !== 'playing') return
      const now = s.context.currentTime
      const startAt = Math.max(s.nextStartAt, now)
      if (startAt > s.nextStartAt) s.anchor = { offset: piece.start, contextTime: startAt }
      startPiece(s, s.buffers.length - 1, startAt, 0)
      s.nextStartAt = startAt + buffer.duration / rateRef.current
    },
    [playFrom, startPiece],
  )

  const play = useCallback(() => {
    setState('loading')
    const Ctor: typeof AudioContext | undefined =
      typeof AudioContext !== 'undefined'
        ? AudioContext
        : (globalThis as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) {
      setState('unavailable')
      return
    }
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
    sessionRef.current = s

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
        setDuration(bufferedSeconds(s.pieces))
        finishIfDone(s)
      } catch {
        if (s.cancelled) return
        teardown(s)
        setState('unavailable')
      }
    })()
  }, [resolve, onDecoded, finishIfDone, teardown])

  const pause = useCallback(
    (s: Session) => {
      s.pausedAt = currentPosition(s)
      silence(s)
      s.mode = 'paused'
      setPosition(s.pausedAt)
      setState('paused')
    },
    [currentPosition, silence],
  )

  const toggle = useCallback(() => {
    const s = sessionRef.current
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
  }, [play, stop, pause, playFrom])

  const seek = useCallback(
    (seconds: number) => {
      const s = sessionRef.current
      if (!s || s.mode === 'loading') return
      const target = clampSeek(seconds, bufferedSeconds(s.pieces))
      if (s.mode === 'playing') {
        playFrom(s, target)
        return
      }
      s.pausedAt = target
      setPosition(target)
    },
    [playFrom],
  )

  const skip = useCallback(
    (delta: number) => {
      const s = sessionRef.current
      if (!s) return
      seek(currentPosition(s) + delta)
    },
    [seek, currentPosition],
  )

  const setRate = useCallback(
    (next: number) => {
      const clamped = clampRate(next)
      const s = sessionRef.current
      const at = s && s.mode === 'playing' ? currentPosition(s) : null
      rateRef.current = clamped
      setRateState(clamped)
      if (s && at !== null) playFrom(s, at)
    },
    [currentPosition, playFrom],
  )

  useEffect(
    () => () => {
      const s = sessionRef.current
      if (s) teardown(s)
    },
    [teardown],
  )

  return { state, toggle, stop, position, buffered, duration, seek, skip, rate, setRate }
}
