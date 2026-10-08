import { useCallback, useRef, useState } from 'react'
import { FRAMES_CONTENT_TYPE, readFrames } from './frames'
import type { VoiceSource } from './speak'

export type VoicePlaybackState = 'idle' | 'loading' | 'playing' | 'unavailable'

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
 * and the reading then plays to nobody with nothing reported.
 *
 * resolve is called on each press rather than once: the URL it returns is
 * signed and expires, so one resolved when the text was rendered would have
 * gone stale by the time someone presses play.
 */
export function useVoicePlayback(resolve: () => Promise<VoiceSource>) {
  const [state, setState] = useState<VoicePlaybackState>('idle')
  const contextRef = useRef<AudioContext | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const stopRef = useRef<(() => void) | null>(null)

  const stop = useCallback(() => {
    stopRef.current?.()
    stopRef.current = null
    abortRef.current?.abort()
    abortRef.current = null
    setState((s) => (s === 'unavailable' ? s : 'idle'))
  }, [])

  const play = useCallback(() => {
    setState('loading')
    const controller = new AbortController()
    abortRef.current = controller

    const Ctor: typeof AudioContext | undefined =
      typeof AudioContext !== 'undefined'
        ? AudioContext
        : (globalThis as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) {
      setState('unavailable')
      return
    }
    const audioContext = new Ctor()
    contextRef.current = audioContext
    // Still inside the gesture that called play(): a resume() issued after
    // the first await is refused on iOS, and every source then plays silent.
    const resumed = audioContext.resume().catch(() => {})

    let cancelled = false
    let nextStartAt = 0
    let scheduled = 0
    let sourcesDone = 0
    let streamDone = false

    const finishIfDone = () => {
      if (streamDone && scheduled === sourcesDone) {
        setState((s) => (s === 'playing' ? 'idle' : s))
      }
    }

    stopRef.current = () => {
      cancelled = true
      audioContext.close().catch(() => {})
    }

    ;(async () => {
      try {
        await resumed
        const reading = await resolve()
        // No credentials: the audio comes from another origin, and what
        // authorises the request is the signature already in the URL.
        const res = await fetch(reading.url, {
          method: 'POST',
          signal: controller.signal,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(reading.body),
        })
        if (!res.ok || !res.body) {
          throw new Error(`unexpected response (${res.status})`)
        }
        const contentType = res.headers.get('Content-Type') ?? ''
        if (contentType !== FRAMES_CONTENT_TYPE) {
          // A reading vvaves already keeps comes back whole, as one audio
          // file with a Content-Length, whatever the request asked for.
          if (!contentType.startsWith('audio/')) {
            throw new Error(`unexpected response (${res.status}, ${contentType})`)
          }
          const whole = await res.arrayBuffer()
          if (cancelled) return
          scheduled = 1
          const buffer = await audioContext.decodeAudioData(whole)
          if (cancelled) return
          const source = audioContext.createBufferSource()
          source.buffer = buffer
          source.connect(audioContext.destination)
          source.start()
          source.onended = () => {
            sourcesDone += 1
            finishIfDone()
          }
          streamDone = true
          setState('playing')
          return
        }

        await readFrames(
          res.body.getReader(),
          (frame) => {
            if (cancelled) return
            scheduled += 1
            // decodeAudioData detaches the buffer it is given, which would
            // corrupt later frames sharing the same backing ArrayBuffer.
            const bytes = frame.slice().buffer
            audioContext
              .decodeAudioData(bytes)
              .then((buffer) => {
                if (cancelled) return
                const source = audioContext.createBufferSource()
                source.buffer = buffer
                source.connect(audioContext.destination)
                const startAt = Math.max(nextStartAt, audioContext.currentTime)
                source.start(startAt)
                nextStartAt = startAt + buffer.duration
                source.onended = () => {
                  sourcesDone += 1
                  finishIfDone()
                }
                if (scheduled === 1) setState('playing')
              })
              .catch(() => {
                sourcesDone += 1
                finishIfDone()
              })
          },
          controller.signal,
        )
        streamDone = true
        finishIfDone()
        if (scheduled === 0) setState('unavailable')
      } catch {
        if (!cancelled) setState('unavailable')
      }
    })()
  }, [resolve])

  const toggle = useCallback(() => {
    if (state === 'playing' || state === 'loading') {
      stop()
      return
    }
    play()
  }, [state, play, stop])

  return { state, toggle, stop }
}
