import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import type { VoiceSource } from './speak'
import { createVoicePlayer, type VoicePlayer, type VoiceSnapshot } from './voicePlayer'

export type { VoicePlaybackState } from './voicePlayer'

export type VoicePlayback = VoiceSnapshot & Omit<VoicePlayer, 'getSnapshot' | 'subscribe'>

export function useVoicePlayback(resolve: () => Promise<VoiceSource>): VoicePlayback {
  const latest = useRef(resolve)
  latest.current = resolve
  const [player] = useState(() => createVoicePlayer(() => latest.current()))
  const snapshot = useSyncExternalStore(player.subscribe, player.getSnapshot, player.getSnapshot)

  useEffect(() => () => player.stop(), [player])

  const { toggle, stop, seek, skip, setRate } = player
  return { ...snapshot, toggle, stop, seek, skip, setRate }
}
