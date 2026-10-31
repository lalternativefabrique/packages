import { useCallback, useRef, type RefObject } from 'react'
import { speakSource, type SignedReading } from './speak'
import { useReadAlong } from './useReadAlong'
import { useVoicePlayback, type VoicePlayback } from './useVoicePlayback'

export type ReadingRequest = () => Promise<SignedReading>

export function readingAudio(request: ReadingRequest): {
  useReading: (article?: RefObject<HTMLElement | null>) => VoicePlayback
} {
  return {
    useReading(article) {
      const resolve = useCallback(async () => speakSource(await request()), [])
      const playback = useVoicePlayback(resolve)
      const none = useRef<HTMLElement | null>(null)
      useReadAlong(playback, article ?? none, { name: 'lalt-article-reading' })
      return playback
    },
  }
}
