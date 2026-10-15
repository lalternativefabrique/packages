import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  codeOf,
  currentChoice,
  isStored,
  linkRequest,
  statusOf,
} from './model'
import type {
  Choice,
  ConnectClient,
  Connections as ConnectionsData,
  Entity,
  Outcome,
  ProductConnection,
  Resource,
} from './model'
import type { Texts } from './texts'

export type Classes = {
  root: string
  intro: string
  notice: string
  noticeError: string
  card: string
  cardHeader: string
  title: string
  meta: string
  body: string
  text: string
  muted: string
  link: string
  button: string
  primary: string
  select: string
  confirm: string
  list: string
  item: string
}

export const defaultClasses: Classes = {
  root: 'max-w-3xl space-y-6 px-6 py-8',
  intro: 'text-sm opacity-80',
  notice: 'rounded-md border px-3 py-2 text-sm',
  noticeError: 'rounded-md border border-red-500 px-3 py-2 text-sm',
  card: 'rounded-lg border',
  cardHeader: 'flex flex-wrap items-center gap-3 border-b px-4 py-3',
  title: 'text-base font-semibold',
  meta: 'flex-1 text-xs opacity-60',
  body: 'px-4 py-3',
  text: 'text-sm opacity-80',
  muted: 'text-xs opacity-60',
  link: 'underline underline-offset-4',
  button: 'inline-flex h-8 items-center rounded-md border px-3 text-sm disabled:opacity-50',
  primary: 'inline-flex h-8 items-center rounded-md border px-3 text-sm font-medium disabled:opacity-50',
  select: 'h-8 rounded-md border bg-transparent px-2 text-sm',
  confirm: 'basis-full space-y-2 rounded-md border border-red-500 px-3 py-2',
  list: 'divide-y',
  item: 'flex flex-wrap items-center gap-3 py-2',
}

export type ConnectionsProps = {
  client: ConnectClient
  entities: Entity[]
  texts: Texts
  intro?: ReactNode
  outcome?: Outcome
  product?: string
  formatDate?: (iso: string) => string
  classes?: Partial<Classes>
  navigate?: (url: string) => void
}

type Async<T> =
  | { state: 'pending' }
  | { state: 'error'; error: unknown }
  | { state: 'ready'; data: T }

function useAsync<T>(load: () => Promise<T>, enabled = true) {
  const [value, setValue] = useState<Async<T>>({ state: 'pending' })
  const [round, setRound] = useState(0)
  useEffect(() => {
    if (!enabled) return
    let live = true
    setValue({ state: 'pending' })
    load().then(
      (data) => live && setValue({ state: 'ready', data }),
      (error: unknown) => live && setValue({ state: 'error', error }),
    )
    return () => {
      live = false
    }
  }, [load, enabled, round])
  const reload = useCallback(() => setRound((r) => r + 1), [])
  return [value, reload] as const
}

const defaultDate = (iso: string) =>
  new Date(iso).toLocaleDateString('fr-FR', { day: 'numeric', month: 'short', year: 'numeric' })

export function Connections({
  client,
  entities,
  texts,
  intro,
  outcome,
  product,
  formatDate = defaultDate,
  classes,
  navigate = (url) => window.location.assign(url),
}: ConnectionsProps) {
  const c = { ...defaultClasses, ...classes }
  const list = useCallback(() => client.list(), [client])
  const [connections, reload] = useAsync<ConnectionsData>(list)
  const label =
    (connections.state === 'ready' &&
      connections.data.products.find((p) => p.product === product)?.label) ||
    product ||
    ''

  return (
    <div className={c.root}>
      {intro && <div className={c.intro}>{intro}</div>}
      {outcome && (
        <p role="status" className={outcome === 'connected' ? c.notice : c.noticeError}>
          {texts.outcome(outcome, label)}
        </p>
      )}
      {connections.state === 'pending' ? null : connections.state === 'error' ? (
        <p role="alert" className={c.text}>
          {texts.loadFailed}{' '}
          <button type="button" className={c.link} onClick={reload}>
            {texts.retry}
          </button>
        </p>
      ) : !connections.data.available ? (
        <p className={c.text}>{texts.unavailable}</p>
      ) : (
        connections.data.products.map((p) => (
          <ProductCard
            key={p.product}
            connection={p}
            client={client}
            entities={entities}
            texts={texts}
            c={c}
            formatDate={formatDate}
            navigate={navigate}
            onChange={reload}
          />
        ))
      )}
    </div>
  )
}

type CardProps = {
  connection: ProductConnection
  client: ConnectClient
  entities: Entity[]
  texts: Texts
  c: Classes
  formatDate: (iso: string) => string
  navigate: (url: string) => void
  onChange: () => void
}

function ProductCard({ connection, client, entities, texts, c, formatDate, navigate, onChange }: CardProps) {
  const active = connection.status === 'active'
  const load = useCallback(() => client.resources(connection.product), [client, connection.product])
  const [resources, reload] = useAsync(load, active)
  const removed =
    connection.status === 'revoked' ||
    (resources.state === 'error' && statusOf(resources.error) === 409)

  return (
    <section aria-labelledby={`connection-${connection.product}`} className={c.card}>
      <header className={c.cardHeader}>
        <h2 id={`connection-${connection.product}`} className={c.title}>
          {connection.label}
        </h2>
        <span className={c.meta}>
          {removed
            ? texts.removed
            : active && connection.connected_at
              ? texts.connectedOn(formatDate(connection.connected_at))
              : texts.notConnected}
        </span>
        {active && !removed ? (
          <DisconnectButton connection={connection} client={client} texts={texts} c={c} onDone={onChange} />
        ) : (
          <ConnectButton
            connection={connection}
            client={client}
            texts={texts}
            c={c}
            navigate={navigate}
            label={removed ? texts.reconnect : texts.connect(connection.label)}
          />
        )}
      </header>
      <div className={c.body}>
        {removed ? (
          <p className={c.text}>{texts.removedHint(connection.label)}</p>
        ) : !active ? (
          <p className={c.text}>{texts.notConnectedHint(connection.label)}</p>
        ) : resources.state === 'pending' ? null : resources.state === 'error' ? (
          <p role="alert" className={c.text}>
            {texts.productDown(connection.label)}{' '}
            <button type="button" className={c.link} onClick={reload}>
              {texts.retry}
            </button>
          </p>
        ) : resources.data.resources.length === 0 ? (
          <p className={c.text}>{texts.nothingShared(connection.label)}</p>
        ) : (
          <ul className={c.list}>
            {resources.data.resources.map((r) => (
              <li key={r.id} className={c.item} style={{ paddingLeft: `${r.depth * 20}px` }}>
                <span style={{ minWidth: 0, flex: 1 }}>
                  <span style={{ display: 'block' }}>{r.label}</span>
                  {r.url && <span className={c.muted}>{r.url}</span>}
                </span>
                {r.mappable ? (
                  <ResourceChoice
                    key={`${r.link.state}:${r.link.app}`}
                    product={connection.product}
                    resource={r}
                    entities={entities}
                    client={client}
                    texts={texts}
                    c={c}
                    onSaved={reload}
                  />
                ) : (
                  <span className={c.muted}>{texts.groups}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

function ConnectButton({
  connection,
  client,
  texts,
  c,
  navigate,
  label,
}: {
  connection: ProductConnection
  client: ConnectClient
  texts: Texts
  c: Classes
  navigate: (url: string) => void
  label: string
}) {
  const [state, setState] = useState<'idle' | 'busy' | 'error'>('idle')
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
      {state === 'error' && (
        <span role="alert" className={c.muted}>
          {texts.startFailed}
        </span>
      )}
      <button
        type="button"
        className={c.primary}
        disabled={state === 'busy'}
        onClick={() => {
          setState('busy')
          client.start(connection.product).then(
            (r) => navigate(r.authorize_url),
            () => setState('error'),
          )
        }}
      >
        {label}
      </button>
    </span>
  )
}

function DisconnectButton({
  connection,
  client,
  texts,
  c,
  onDone,
}: {
  connection: ProductConnection
  client: ConnectClient
  texts: Texts
  c: Classes
  onDone: () => void
}) {
  const [step, setStep] = useState<'idle' | 'confirm' | 'busy' | 'error'>('idle')
  if (step === 'idle')
    return (
      <button type="button" className={c.button} onClick={() => setStep('confirm')}>
        {texts.disconnect}
      </button>
    )
  return (
    <div role="alertdialog" aria-label={texts.disconnectProduct(connection.label)} className={c.confirm}>
      <p className={c.text}>{texts.disconnectWarning(connection.label)}</p>
      {step === 'error' && (
        <p role="alert" className={c.muted}>
          {texts.disconnectFailed}
        </p>
      )}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <button
          type="button"
          className={c.primary}
          disabled={step === 'busy'}
          onClick={() => {
            setStep('busy')
            client.disconnect(connection.product).then(onDone, () => setStep('error'))
          }}
        >
          {texts.disconnectProduct(connection.label)}
        </button>
        <button type="button" className={c.button} onClick={() => setStep('idle')}>
          {texts.cancel}
        </button>
      </div>
    </div>
  )
}

function linkState(r: Resource, entities: Entity[], texts: Texts) {
  if (r.link.state === 'ignored') return texts.ignored
  if (r.link.state === 'linked')
    return texts.linkedTo(entities.find((e) => e.id === r.link.app)?.name ?? r.link.app)
  return r.suggested ? texts.suggested : texts.notLinked
}

function saveError(error: unknown, texts: Texts) {
  if (statusOf(error) !== 409) return texts.saveFailed
  return codeOf(error) === 'linked_elsewhere' ? texts.linkedElsewhere : texts.nameTaken
}

function ResourceChoice({
  product,
  resource,
  entities,
  client,
  texts,
  c,
  onSaved,
}: {
  product: string
  resource: Resource
  entities: Entity[]
  client: ConnectClient
  texts: Texts
  c: Classes
  onSaved: () => void
}) {
  const [choice, setChoice] = useState<Choice>(() => currentChoice(resource))
  const [save, setSave] = useState<{ state: 'idle' | 'busy' } | { state: 'error'; error: unknown }>({
    state: 'idle',
  })
  const stored = isStored(resource, choice)

  return (
    <form
      style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '0.5rem' }}
      onSubmit={(e) => {
        e.preventDefault()
        if (choice === '' || stored) return
        setSave({ state: 'busy' })
        client.setLink(product, linkRequest(resource.id, choice)).then(onSaved, (error: unknown) =>
          setSave({ state: 'error', error }),
        )
      }}
    >
      <span className={c.muted}>
        {save.state === 'error' ? saveError(save.error, texts) : linkState(resource, entities, texts)}
      </span>
      <select
        aria-label={texts.choiceLabel(resource.label)}
        value={choice}
        onChange={(e) => setChoice(e.target.value as Choice)}
        className={c.select}
      >
        <option value="" disabled>
          {texts.choose}
        </option>
        {entities.length > 0 && (
          <optgroup label={texts.linkTo}>
            {entities.map((e) => (
              <option key={e.id} value={`link:${e.id}`}>
                {e.name}
              </option>
            ))}
          </optgroup>
        )}
        <option value="create">{texts.create(resource.label)}</option>
        <option value="ignore">{texts.ignore}</option>
      </select>
      <button type="submit" className={c.button} disabled={choice === '' || stored || save.state === 'busy'}>
        {texts.save}
      </button>
    </form>
  )
}
