export type Status = 'none' | 'active' | 'revoked'

export type ProductConnection = {
  product: string
  label: string
  status: Status
  scopes: string[]
  connected_at: string | null
}

export type Connections = {
  available: boolean
  products: ProductConnection[]
}

export type Outcome = 'connected' | 'refused' | 'invalid' | 'unavailable'

export const OUTCOMES: readonly Outcome[] = ['connected', 'refused', 'invalid', 'unavailable']

export type Link = { state: 'none' | 'linked' | 'ignored'; app: string }

export type Resource = {
  id: string
  type: string
  label: string
  url: string
  parent: string
  depth: number
  mappable: boolean
  suggested: string
  link: Link
}

export type Resources = {
  product: string
  scopes: { name: string; label: string }[]
  resources: Resource[]
}

export type SetLink =
  | { resource_id: string; choice: 'ignore' | 'create' | 'clear' }
  | { resource_id: string; choice: 'link'; app: string }

export type Entity = { id: string; name: string }

export type ConnectClient = {
  list: () => Promise<Connections>
  start: (product: string) => Promise<{ authorize_url: string }>
  disconnect: (product: string) => Promise<void>
  resources: (product: string) => Promise<Resources>
  setLink: (product: string, request: SetLink) => Promise<void>
}

export type Choice = '' | 'ignore' | 'create' | `link:${string}`

export function readOutcome(value: unknown): Outcome | undefined {
  return OUTCOMES.find((o) => o === value)
}

export function currentChoice(r: Resource): Choice {
  if (r.link.state === 'ignored') return 'ignore'
  if (r.link.state === 'linked') return `link:${r.link.app}`
  return r.suggested ? `link:${r.suggested}` : ''
}

export function isStored(r: Resource, choice: Choice): boolean {
  if (r.link.state === 'ignored') return choice === 'ignore'
  if (r.link.state === 'linked') return choice === `link:${r.link.app}`
  return false
}

export function linkRequest(resourceId: string, choice: Exclude<Choice, ''>): SetLink {
  if (choice === 'ignore' || choice === 'create') return { resource_id: resourceId, choice }
  return { resource_id: resourceId, choice: 'link', app: choice.slice(5) }
}

export function statusOf(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const e = error as { status?: unknown; response?: { status?: unknown } }
  if (typeof e.status === 'number') return e.status
  if (typeof e.response?.status === 'number') return e.response.status
  return undefined
}

export function codeOf(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const e = error as { message?: unknown; code?: unknown }
  if (typeof e.code === 'string') return e.code
  if (typeof e.message === 'string') return e.message
  return undefined
}
