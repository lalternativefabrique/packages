import { useEffect, useId, useState, type FormEvent, type ReactNode } from "react"
import type { AuthClientDataResult, AuthClientResult } from "../types"
import {
  MIN_PASSWORD_LENGTH,
  emailChangeProblem,
  hasPasswordAccount,
  passwordChangeProblem,
  type EmailChangeProblem,
  type PasswordChangeProblem,
} from "../account-settings"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import {
  DeleteAccountSteps,
  type DeleteAccountStepsProps,
} from "./delete-account-steps"

/** The slice of a Better Auth client the page calls; createPlatformAuthClient fits it. */
export interface AccountSettingsClient {
  updateUser(input: { name: string }): Promise<AuthClientResult>
  changePassword(input: {
    currentPassword: string
    newPassword: string
    revokeOtherSessions?: boolean
  }): Promise<AuthClientResult>
  changeEmail?(input: {
    newEmail: string
    callbackURL?: string
  }): Promise<AuthClientResult>
  listAccounts?(): Promise<AuthClientDataResult<Array<{ providerId?: string }>>>
}

export interface AccountSettingsSection {
  id: string
  label: string
  content: ReactNode
}

export interface AccountSettingsLabels {
  title: string
  profileTab: string
  billingTab: string
  nameHeading: string
  nameLabel: string
  save: string
  saving: string
  nameSaved: string
  emailHeading: string
  emailChange: string
  emailNewLabel: string
  emailSend: string
  emailSent: string
  emailProblems: Record<EmailChangeProblem, string>
  passwordHeading: string
  passwordChange: string
  passwordCurrentLabel: string
  passwordNewLabel: string
  passwordNewDescription: string
  passwordConfirmLabel: string
  passwordRevokeOthers: string
  passwordSaved: string
  passwordProblems: Record<PasswordChangeProblem, string>
  passwordNone: string
  cancel: string
  failed: string
  dangerHeading: string
}

export interface AccountSettingsProps {
  client: AccountSettingsClient
  user: { name?: string | null; email: string }
  /** The billing tab, second after the profile; omitted for a product that bills nobody. */
  billing?: ReactNode
  /** Tabs the product adds after profile and billing. */
  sections?: AccountSettingsSection[]
  /** Controlled tab, e.g. mirrored in the URL. Uncontrolled when omitted. */
  tab?: string
  onTabChange?: (id: string) => void
  /**
   * Offers the address change. Off by default: it needs the server's
   * `user.changeEmail` and, with Kratos passwords, the identity kept in step.
   */
  emailChange?: { callbackURL?: string } | boolean
  /** Overrides the detection through `client.listAccounts`. */
  hasPassword?: boolean
  /** Where a person without a password goes to set one. */
  setPasswordHref?: string
  /** The danger zone under the profile; `false` hides it. */
  deleteAccount?: DeleteAccountStepsProps | false
  onUpdated?: () => void | Promise<void>
  labels?: Partial<AccountSettingsLabels>
}

const DEFAULTS: AccountSettingsLabels = {
  title: "Paramètres",
  profileTab: "Profil",
  billingTab: "Abonnement",
  nameHeading: "Nom",
  nameLabel: "Nom affiché",
  save: "Enregistrer",
  saving: "Enregistrement…",
  nameSaved: "Nom enregistré.",
  emailHeading: "Adresse e-mail",
  emailChange: "Modifier",
  emailNewLabel: "Nouvelle adresse",
  emailSend: "Envoyer le lien de confirmation",
  emailSent:
    "Un lien de confirmation a été envoyé. L'adresse change dès que tu l'as ouvert.",
  emailProblems: {
    invalid: "Cette adresse n'est pas valide.",
    unchanged: "C'est déjà ton adresse.",
  },
  passwordHeading: "Mot de passe",
  passwordChange: "Modifier",
  passwordCurrentLabel: "Mot de passe actuel",
  passwordNewLabel: "Nouveau mot de passe",
  passwordNewDescription: `Au moins ${MIN_PASSWORD_LENGTH} caractères.`,
  passwordConfirmLabel: "Confirmer le nouveau mot de passe",
  passwordRevokeOthers: "Déconnecter mes autres appareils",
  passwordSaved: "Mot de passe modifié.",
  passwordProblems: {
    missing: "Renseigne ton mot de passe actuel et le nouveau.",
    too_short: `Le nouveau mot de passe doit faire au moins ${MIN_PASSWORD_LENGTH} caractères.`,
    mismatch: "La confirmation ne correspond pas au nouveau mot de passe.",
    unchanged: "Le nouveau mot de passe est identique à l'actuel.",
  },
  passwordNone:
    "Tu te connectes sans mot de passe (Google, GitHub ou lien magique).",
  cancel: "Annuler",
  failed: "La modification n'a pas pu être enregistrée. Réessaie dans un instant.",
  dangerHeading: "Supprimer mon compte",
}

const PROFILE_TAB = "profil"
const BILLING_TAB = "abonnement"

const primaryButton =
  "inline-flex h-10 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-55"
const quietButton =
  "inline-flex h-10 items-center justify-center rounded-md px-4 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
const outlineButton =
  "inline-flex h-9 items-center justify-center rounded-md border border-border px-3 text-sm font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"

export function AccountSettings({
  client,
  user,
  billing,
  sections = [],
  tab,
  onTabChange,
  emailChange = false,
  hasPassword,
  setPasswordHref,
  deleteAccount,
  onUpdated,
  labels,
}: AccountSettingsProps) {
  const t: AccountSettingsLabels = {
    ...DEFAULTS,
    ...labels,
    emailProblems: { ...DEFAULTS.emailProblems, ...labels?.emailProblems },
    passwordProblems: { ...DEFAULTS.passwordProblems, ...labels?.passwordProblems },
  }
  const tabs = [
    { id: PROFILE_TAB, label: t.profileTab },
    ...(billing ? [{ id: BILLING_TAB, label: t.billingTab }] : []),
    ...sections.map(({ id, label }) => ({ id, label })),
  ]
  const [localTab, setLocalTab] = useState(PROFILE_TAB)
  const requested = tab ?? localTab
  const active = tabs.some((x) => x.id === requested) ? requested : PROFILE_TAB
  const select = (id: string) => {
    setLocalTab(id)
    onTabChange?.(id)
  }
  const baseId = useId()

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">{t.title}</h1>

      {tabs.length > 1 ? (
        <div
          role="tablist"
          aria-label={t.title}
          className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-border [scrollbar-width:none]"
          onKeyDown={(e) => {
            if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return
            const i = tabs.findIndex((x) => x.id === active)
            const step = e.key === "ArrowRight" ? 1 : -1
            const next = tabs[(i + step + tabs.length) % tabs.length]!
            select(next.id)
            document.getElementById(`${baseId}-tab-${next.id}`)?.focus()
          }}
        >
          {tabs.map((x) => {
            const selected = x.id === active
            return (
              <button
                key={x.id}
                id={`${baseId}-tab-${x.id}`}
                type="button"
                role="tab"
                aria-selected={selected}
                aria-controls={`${baseId}-panel-${x.id}`}
                tabIndex={selected ? 0 : -1}
                onClick={() => select(x.id)}
                className={[
                  "-mb-px shrink-0 border-b-2 px-3 py-2 text-sm font-medium transition-colors",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  selected
                    ? "border-foreground text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground",
                ].join(" ")}
              >
                {x.label}
              </button>
            )
          })}
        </div>
      ) : null}

      <div
        role={tabs.length > 1 ? "tabpanel" : undefined}
        id={`${baseId}-panel-${active}`}
        aria-labelledby={tabs.length > 1 ? `${baseId}-tab-${active}` : undefined}
        className="space-y-6"
      >
        {active === PROFILE_TAB ? (
          <>
            <div className="divide-y divide-border rounded-xl border border-border">
              <NameSection client={client} user={user} t={t} onUpdated={onUpdated} />
              <EmailSection
                client={client}
                user={user}
                t={t}
                enabled={Boolean(emailChange) && Boolean(client.changeEmail)}
                callbackURL={typeof emailChange === "object" ? emailChange.callbackURL : undefined}
              />
              <PasswordSection
                client={client}
                t={t}
                hasPassword={hasPassword}
                setPasswordHref={setPasswordHref}
              />
            </div>
            {deleteAccount !== false ? (
              <section className="space-y-3 rounded-xl border border-destructive/30 p-6">
                <h2 className="text-base font-semibold">{t.dangerHeading}</h2>
                <DeleteAccountSteps billing={Boolean(billing)} {...deleteAccount} />
              </section>
            ) : null}
          </>
        ) : active === BILLING_TAB ? (
          billing
        ) : (
          sections.find((s) => s.id === active)?.content
        )}
      </div>
    </div>
  )
}

interface SectionProps {
  client: AccountSettingsClient
  t: AccountSettingsLabels
}

function Row({
  heading,
  value,
  action,
  children,
}: {
  heading: string
  value?: ReactNode
  action?: ReactNode
  children?: ReactNode
}) {
  return (
    <section className="space-y-4 p-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <h2 className="text-base font-semibold">{heading}</h2>
          {value ? <div className="truncate text-sm text-muted-foreground">{value}</div> : null}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function NameSection({
  client,
  user,
  t,
  onUpdated,
}: SectionProps & { user: AccountSettingsProps["user"]; onUpdated?: () => void | Promise<void> }) {
  const saved = user.name ?? ""
  const [name, setName] = useState(saved)
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState<{ tone: "error" | "success"; text: string }>()

  useEffect(() => setName(saved), [saved])

  const trimmed = name.trim()
  const dirty = trimmed !== saved.trim()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!dirty || !trimmed) return
    setPending(true)
    setMessage(undefined)
    try {
      const res = await client.updateUser({ name: trimmed })
      if (res.error) {
        setMessage({ tone: "error", text: res.error.message || t.failed })
        return
      }
      setMessage({ tone: "success", text: t.nameSaved })
      await onUpdated?.()
    } catch {
      setMessage({ tone: "error", text: t.failed })
    } finally {
      setPending(false)
    }
  }

  return (
    <Row heading={t.nameHeading}>
      <form onSubmit={submit} className="space-y-3">
        <AuthField
          label={t.nameLabel}
          value={name}
          autoComplete="name"
          maxLength={100}
          onChange={(e) => {
            setName(e.target.value)
            setMessage(undefined)
          }}
        />
        <AuthAlert tone={message?.tone}>{message?.text}</AuthAlert>
        <button type="submit" disabled={!dirty || !trimmed || pending} className={primaryButton}>
          {pending ? t.saving : t.save}
        </button>
      </form>
    </Row>
  )
}

function EmailSection({
  client,
  user,
  t,
  enabled,
  callbackURL,
}: SectionProps & {
  user: AccountSettingsProps["user"]
  enabled: boolean
  callbackURL?: string
}) {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState("")
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState<{ tone: "error" | "success"; text: string }>()

  const close = () => {
    setOpen(false)
    setEmail("")
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const problem = emailChangeProblem(user.email, email)
    if (problem) {
      setMessage({ tone: "error", text: t.emailProblems[problem] })
      return
    }
    setPending(true)
    setMessage(undefined)
    try {
      const res = await client.changeEmail!({
        newEmail: email.trim().toLowerCase(),
        ...(callbackURL ? { callbackURL } : {}),
      })
      if (res.error) {
        setMessage({ tone: "error", text: res.error.message || t.failed })
        return
      }
      close()
      setMessage({ tone: "success", text: t.emailSent })
    } catch {
      setMessage({ tone: "error", text: t.failed })
    } finally {
      setPending(false)
    }
  }

  return (
    <Row
      heading={t.emailHeading}
      value={user.email}
      action={
        enabled && !open ? (
          <button
            type="button"
            className={outlineButton}
            onClick={() => {
              setOpen(true)
              setMessage(undefined)
            }}
          >
            {t.emailChange}
          </button>
        ) : null
      }
    >
      {open ? (
        <form onSubmit={submit} className="space-y-3">
          <AuthField
            label={t.emailNewLabel}
            type="email"
            value={email}
            autoComplete="email"
            autoFocus
            onChange={(e) => {
              setEmail(e.target.value)
              setMessage(undefined)
            }}
          />
          <AuthAlert tone={message?.tone}>{message?.text}</AuthAlert>
          <div className="flex flex-wrap gap-2">
            <button type="submit" disabled={pending || !email} className={primaryButton}>
              {pending ? t.saving : t.emailSend}
            </button>
            <button type="button" onClick={close} className={quietButton}>
              {t.cancel}
            </button>
          </div>
        </form>
      ) : (
        <AuthAlert tone={message?.tone}>{message?.text}</AuthAlert>
      )}
    </Row>
  )
}

function PasswordSection({
  client,
  t,
  hasPassword,
  setPasswordHref,
}: SectionProps & { hasPassword?: boolean; setPasswordHref?: string }) {
  const detected = useDetectedPassword(client, hasPassword)
  const [open, setOpen] = useState(false)
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [revokeOthers, setRevokeOthers] = useState(true)
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState<{ tone: "error" | "success"; text: string }>()

  const close = () => {
    setOpen(false)
    setCurrent("")
    setNext("")
    setConfirm("")
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const problem = passwordChangeProblem({ current, next, confirm })
    if (problem) {
      setMessage({ tone: "error", text: t.passwordProblems[problem] })
      return
    }
    setPending(true)
    setMessage(undefined)
    try {
      const res = await client.changePassword({
        currentPassword: current,
        newPassword: next,
        revokeOtherSessions: revokeOthers,
      })
      if (res.error) {
        setMessage({ tone: "error", text: res.error.message || t.failed })
        return
      }
      close()
      setMessage({ tone: "success", text: t.passwordSaved })
    } catch {
      setMessage({ tone: "error", text: t.failed })
    } finally {
      setPending(false)
    }
  }

  if (detected === false) {
    return (
      <Row
        heading={t.passwordHeading}
        value={t.passwordNone}
        action={
          setPasswordHref ? (
            <a href={setPasswordHref} className={outlineButton}>
              {t.passwordChange}
            </a>
          ) : null
        }
      />
    )
  }

  return (
    <Row
      heading={t.passwordHeading}
      value={open ? undefined : "••••••••"}
      action={
        !open && detected !== undefined ? (
          <button
            type="button"
            className={outlineButton}
            onClick={() => {
              setOpen(true)
              setMessage(undefined)
            }}
          >
            {t.passwordChange}
          </button>
        ) : null
      }
    >
      {open ? (
        <form onSubmit={submit} className="space-y-3">
          <AuthField
            label={t.passwordCurrentLabel}
            type="password"
            value={current}
            autoComplete="current-password"
            autoFocus
            onChange={(e) => {
              setCurrent(e.target.value)
              setMessage(undefined)
            }}
          />
          <AuthField
            label={t.passwordNewLabel}
            type="password"
            value={next}
            autoComplete="new-password"
            description={t.passwordNewDescription}
            onChange={(e) => {
              setNext(e.target.value)
              setMessage(undefined)
            }}
          />
          <AuthField
            label={t.passwordConfirmLabel}
            type="password"
            value={confirm}
            autoComplete="new-password"
            onChange={(e) => {
              setConfirm(e.target.value)
              setMessage(undefined)
            }}
          />
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={revokeOthers}
              onChange={(e) => setRevokeOthers(e.target.checked)}
              className="size-4 accent-primary"
            />
            {t.passwordRevokeOthers}
          </label>
          <AuthAlert tone={message?.tone}>{message?.text}</AuthAlert>
          <div className="flex flex-wrap gap-2">
            <button type="submit" disabled={pending} className={primaryButton}>
              {pending ? t.saving : t.save}
            </button>
            <button type="button" onClick={close} className={quietButton}>
              {t.cancel}
            </button>
          </div>
        </form>
      ) : (
        <AuthAlert tone={message?.tone}>{message?.text}</AuthAlert>
      )}
    </Row>
  )
}

function useDetectedPassword(
  client: AccountSettingsClient,
  override: boolean | undefined,
): boolean | undefined {
  const [detected, setDetected] = useState<boolean | undefined>(
    override ?? (client.listAccounts ? undefined : true),
  )
  useEffect(() => {
    if (override !== undefined) {
      setDetected(override)
      return
    }
    if (!client.listAccounts) return
    let live = true
    client
      .listAccounts()
      .then((res) => {
        if (live) setDetected(hasPasswordAccount(res.data) ?? true)
      })
      .catch(() => {
        if (live) setDetected(true)
      })
    return () => {
      live = false
    }
  }, [client, override])
  return detected
}
