export interface AppKey {
  id: string
  label: string
  audience: Array<string>
  scopes: Array<string>
  createdAt: string | null
}

// The secret exists in one response and is never stored, so it travels apart
// from the row: a key the caller holds has just been created, a key in the
// list never carries one.
export interface CreatedAppKey extends AppKey {
  secret: string
}

export type KeysError =
  | { kind: 'unauthorized' }
  | { kind: 'forbidden' }
  | { kind: 'invalid'; field: string }
  | { kind: 'unavailable' }

export interface KeysTransport {
  list: () => Promise<Array<AppKey>>
  create: (input: { label: string }) => Promise<CreatedAppKey>
  revoke: (id: string) => Promise<void>
}

export interface KeysCopy {
  title: string
  description: string
  labelPlaceholder: string
  create: string
  creating: string
  empty: string
  columnLabel: string
  columnId: string
  columnScopes: string
  columnCreated: string
  revoke: string
  revoking: string
  confirmRevoke: (label: string) => string
  secretTitle: string
  secretWarning: string
  copy: string
  copied: string
  dismiss: string
  errors: Record<KeysError['kind'], string>
}
