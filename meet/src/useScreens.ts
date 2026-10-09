import { useCallback, useState } from 'react'
import { useLocalParticipant } from '@livekit/components-react'
import { createLocalScreenTracks, ScreenSharePresets, TrackEvent, type LocalTrack } from 'livekit-client'
import { canShareScreen, type Limits } from './layout'

export function useScreens(totalScreens: number, limits: Limits) {
  const { localParticipant } = useLocalParticipant()
  const [own, setOwn] = useState<LocalTrack[][]>([])
  const [error, setError] = useState<string>()

  const allowed = canShareScreen(own.length, totalScreens, limits)

  const stop = useCallback(
    async (group: LocalTrack[]) => {
      setOwn((current) => current.filter((g) => g !== group))
      await Promise.all(group.map((t) => localParticipant.unpublishTrack(t, true)))
    },
    [localParticipant],
  )

  const share = useCallback(async () => {
    setError(undefined)
    let group: LocalTrack[]
    try {
      group = await createLocalScreenTracks({
        audio: true,
        contentHint: 'detail',
        resolution: ScreenSharePresets.h1080fps15.resolution,
        selfBrowserSurface: 'exclude',
      })
    } catch (e) {
      if (e instanceof Error && e.name !== 'NotAllowedError') setError(e.message)
      return
    }
    for (const track of group) {
      track.once(TrackEvent.Ended, () => void stop(group))
      await localParticipant.publishTrack(track, { simulcast: false })
    }
    setOwn((current) => [...current, group])
  }, [localParticipant, stop])

  return { own, allowed, share, stop, error }
}
