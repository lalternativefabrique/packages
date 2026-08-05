import type { InvitationNoticeProps } from "../types"

/**
 * Every app is reachable at contact@ its own apex domain, so the address is
 * derived rather than configured. Sub-domains are stripped because the app is
 * routinely served from app./admin. while the mailbox lives on the apex;
 * multi-part public suffixes (.co.uk) would need a real suffix list and no app
 * using this is on one.
 */
function defaultSupportEmail(): string | undefined {
  if (typeof window === "undefined") return undefined
  const host = window.location.hostname
  if (!host || host === "localhost" || /^[\d.]+$/.test(host)) return undefined
  const apex = host.split(".").slice(-2).join(".")
  return `contact@${apex}`
}

const REASON_MESSAGE: Record<string, string> = {
  expired: "Cette invitation a expiré.",
  claimed: "Cette invitation a déjà été utilisée.",
  unknown: "Ce lien d'invitation n'est pas valide.",
}

/**
 * What an invitee sees when their link does not work.
 *
 * It names the reason instead of merging the cases into one message: "expired"
 * tells someone their link was real and that asking for another one is worth
 * it, which "invalid" does not. Short TTLs make that distinction routine.
 *
 * The only way forward offered is a mailto, deliberately. A self-service
 * "request a new invitation" form would mean a public endpoint accepting dead
 * tokens, a queue to moderate, and a way to probe which tokens once existed —
 * for a flow where the operator already knows the person by name.
 */
export function InvitationNotice({
  reason = "unknown",
  supportEmail,
  title = "Invitation indisponible",
  action,
}: InvitationNoticeProps) {
  const contact = supportEmail ?? defaultSupportEmail()

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {REASON_MESSAGE[reason] ?? REASON_MESSAGE.unknown}
        </p>
      </div>

      {contact ? (
        <p className="text-sm text-muted-foreground">
          Écrivez-nous à{" "}
          <a
            href={`mailto:${contact}`}
            className="font-medium text-foreground underline underline-offset-4 hover:text-foreground/80"
          >
            {contact}
          </a>{" "}
          pour en recevoir une nouvelle.
        </p>
      ) : null}

      {action}
    </div>
  )
}
