import { useCallback } from 'react'
import { speakSource, type SignedReading } from './speak'
import { useVoicePlayback, type VoicePlayback } from './useVoicePlayback'

export type ReadingRequest = () => Promise<SignedReading>

export function readingAudio(request: ReadingRequest): { useReading: () => VoicePlayback } {
  return {
    useReading() {
      const resolve = useCallback(async () => speakSource(await request()), [])
      return useVoicePlayback(resolve)
    },
  }
}
