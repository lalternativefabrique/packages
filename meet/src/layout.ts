export type Layout = 'grid' | 'stage' | 'screens'

export type Stage = {
  pinnedTrack?: string
  pinnedBy?: string
}

export type Limits = {
  screensPerParticipant: number
  screensPerRoom: number
}

export const defaultLimits: Limits = { screensPerParticipant: 2, screensPerRoom: 4 }

export type Tile = {
  id: string
  participant: string
  kind: 'camera' | 'screen'
  speaking?: boolean
}

export type Arrangement = {
  main: Tile[]
  strip: Tile[]
}

export function parseStage(metadata: string | undefined): Stage {
  if (!metadata) return {}
  try {
    const value: unknown = JSON.parse(metadata)
    if (!value || typeof value !== 'object') return {}
    const { pinnedTrack, pinnedBy } = value as Record<string, unknown>
    return {
      pinnedTrack: typeof pinnedTrack === 'string' ? pinnedTrack : undefined,
      pinnedBy: typeof pinnedBy === 'string' ? pinnedBy : undefined,
    }
  } catch {
    return {}
  }
}

export function canShareScreen(own: number, total: number, limits: Limits = defaultLimits): boolean {
  return own < limits.screensPerParticipant && total < limits.screensPerRoom
}

export function arrange(tiles: Tile[], layout: Layout, stage: Stage, localFocus?: string): Arrangement {
  const screens = tiles.filter((t) => t.kind === 'screen')
  const cameras = tiles.filter((t) => t.kind === 'camera')

  if (layout === 'grid') return { main: [...screens, ...cameras], strip: [] }

  if (layout === 'screens') {
    if (screens.length === 0) return { main: cameras, strip: [] }
    return { main: screens, strip: cameras }
  }

  const focusId = localFocus ?? stage.pinnedTrack
  const focus =
    tiles.find((t) => t.id === focusId) ??
    screens[0] ??
    cameras.find((t) => t.speaking) ??
    cameras[0]
  if (!focus) return { main: [], strip: [] }
  return { main: [focus], strip: tiles.filter((t) => t.id !== focus.id) }
}

export function defaultLayout(tiles: Tile[]): Layout {
  const screens = tiles.filter((t) => t.kind === 'screen').length
  if (screens > 1) return 'screens'
  if (screens === 1) return 'stage'
  return 'grid'
}
