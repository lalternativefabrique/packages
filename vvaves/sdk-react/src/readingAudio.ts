import { speakSource, type SignedReading } from './speak'
import { createVoicePlayer, type VoicePlayer } from './voicePlayer'

export type ReadingRequest = () => Promise<SignedReading>

export function readingAudio(request: ReadingRequest): VoicePlayer {
  return createVoicePlayer(async () => speakSource(await request()))
}
