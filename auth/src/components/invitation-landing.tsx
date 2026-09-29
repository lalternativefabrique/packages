import type { InvitationLandingLabels, InvitationLandingProps } from "../types"
import { AuthHeading } from "./auth-heading"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"

const DEFAULTS: Required<InvitationLandingLabels> = {
  invited: (app, email) => `Tu es invité·e sur ${app} avec ${email}`,
  subtitle:
    "Connecte-toi si tu as déjà un mot de passe, sinon définis-en un pour cette adresse.",
  login: "Se connecter",
  setPassword: "Définir un mot de passe",
}

const PRIMARY_CLASS =
  "inline-flex h-11 w-full items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"

export function InvitationLanding({
  app,
  email,
  loginUrl,
  setPasswordUrl,
  labels,
  submitClassName,
  linkComponent,
}: InvitationLandingProps) {
  const t = { ...DEFAULTS, ...labels }

  return (
    <div className="space-y-8">
      <AuthHeading title={t.invited(app, email)} subtitle={t.subtitle} />
      <div className="space-y-4 text-center">
        <AuthLink
          to={loginUrl}
          as={linkComponent}
          className={submitClassName ?? PRIMARY_CLASS}
        >
          {t.login}
        </AuthLink>
        <p className="text-sm">
          <AuthLink
            to={setPasswordUrl}
            as={linkComponent}
            className={AUTH_LINK_CLASS}
          >
            {t.setPassword}
          </AuthLink>
        </p>
      </div>
    </div>
  )
}
