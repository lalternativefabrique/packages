import { useMemo, useState } from 'react'
import {
  DisconnectButton,
  LiveKitRoom,
  ParticipantTile,
  RoomAudioRenderer,
  TrackToggle,
  useLocalParticipant,
  useRoomInfo,
  useTracks,
  type TrackReferenceOrPlaceholder,
} from '@livekit/components-react'
import { Track, VideoPresets, type RoomOptions } from 'livekit-client'
import { arrange, defaultLayout, defaultLimits, parseStage, type Layout, type Limits, type Tile } from './layout'
import { frLabels, type Labels } from './labels'
import { useScreens } from './useScreens'

export type MeetRoomProps = {
  serverUrl: string
  token: string
  onLeave?: () => void
  onPin?: (trackSid: string | null) => void | Promise<void>
  limits?: Limits
  labels?: Labels
  className?: string
}

const roomOptions: RoomOptions = {
  adaptiveStream: true,
  dynacast: true,
  videoCaptureDefaults: { resolution: VideoPresets.h720.resolution },
  publishDefaults: {
    simulcast: true,
    videoSimulcastLayers: [VideoPresets.h180, VideoPresets.h360],
    screenShareEncoding: { maxBitrate: 2_500_000, maxFramerate: 15 },
  },
}

export function MeetRoom({ serverUrl, token, onLeave, onPin, limits = defaultLimits, labels = frLabels, className }: MeetRoomProps) {
  return (
    <LiveKitRoom
      serverUrl={serverUrl}
      token={token}
      connect
      audio
      video
      options={roomOptions}
      onDisconnected={onLeave}
      className={['lam-room', className].filter(Boolean).join(' ')}
      data-lk-theme="default"
    >
      <CallView onPin={onPin} limits={limits} labels={labels} />
      <RoomAudioRenderer />
    </LiveKitRoom>
  )
}

function tileId(ref: TrackReferenceOrPlaceholder): string {
  return ref.publication?.trackSid ?? `${ref.participant.identity}:${ref.source}`
}

function CallView({ onPin, limits, labels }: { onPin?: MeetRoomProps['onPin']; limits: Limits; labels: Labels }) {
  const refs = useTracks(
    [
      { source: Track.Source.Camera, withPlaceholder: true },
      { source: Track.Source.ScreenShare, withPlaceholder: false },
    ],
    { onlySubscribed: false },
  )
  const { metadata } = useRoomInfo()
  const { localParticipant } = useLocalParticipant()
  const stage = parseStage(metadata)
  const isHost = localParticipant.attributes.role === 'host'
  const canPublish = localParticipant.permissions?.canPublish ?? true

  const byId = useMemo(() => new Map(refs.map((r) => [tileId(r), r])), [refs])
  const tiles: Tile[] = refs.map((r) => ({
    id: tileId(r),
    participant: r.participant.identity,
    kind: r.source === Track.Source.ScreenShare ? 'screen' : 'camera',
    speaking: r.participant.isSpeaking,
  }))
  const totalScreens = tiles.filter((t) => t.kind === 'screen').length

  const [chosen, setChosen] = useState<Layout>()
  const [focus, setFocus] = useState<string>()
  const layout = chosen ?? defaultLayout(tiles)
  const { main, strip } = arrange(tiles, layout, stage, focus && byId.has(focus) ? focus : undefined)
  const screens = useScreens(totalScreens, limits)

  const renderTile = (tile: Tile) => {
    const ref = byId.get(tile.id)
    if (!ref) return null
    const pinned = stage.pinnedTrack === tile.id
    return (
      <div key={tile.id} className="lam-tile" data-kind={tile.kind} data-pinned={pinned || undefined}>
        <ParticipantTile trackRef={ref} onParticipantClick={() => setFocus(focus === tile.id ? undefined : tile.id)} />
        {tile.kind === 'screen' && <span className="lam-tile-label">{labels.screenOf(ref.participant.name || ref.participant.identity)}</span>}
        {pinned && <span className="lam-badge">{labels.pinnedForAll}</span>}
        {isHost && onPin && ref.publication && (
          <button type="button" className="lam-pin" onClick={() => void onPin(pinned ? null : tile.id)}>
            {pinned ? labels.unpin : labels.pinForAll}
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="lam-call" data-layout={layout}>
      <div className="lam-main" data-count={main.length}>
        {main.map(renderTile)}
      </div>
      {strip.length > 0 && <div className="lam-strip">{strip.map(renderTile)}</div>}

      <div className="lam-controls">
        {canPublish && (
          <>
            <TrackToggle source={Track.Source.Microphone}>{labels.microphone}</TrackToggle>
            <TrackToggle source={Track.Source.Camera}>{labels.camera}</TrackToggle>
            <button
              type="button"
              className="lam-button"
              disabled={!screens.allowed}
              title={screens.allowed ? undefined : labels.screenLimit}
              onClick={() => void screens.share()}
            >
              {labels.shareScreen}
            </button>
            {screens.own.map((group, i) => (
              <button key={group[0]?.sid ?? i} type="button" className="lam-button lam-stop" onClick={() => void screens.stop(group)}>
                {labels.stopScreen} {i + 1}
              </button>
            ))}
          </>
        )}
        <div className="lam-layouts" role="group">
          {(['grid', 'stage', 'screens'] as const).map((l) => (
            <button key={l} type="button" className="lam-button" aria-pressed={layout === l} onClick={() => setChosen(l)}>
              {labels[l]}
            </button>
          ))}
        </div>
        <DisconnectButton>{labels.leave}</DisconnectButton>
      </div>
      {screens.error && <p className="lam-error">{screens.error}</p>}
    </div>
  )
}
