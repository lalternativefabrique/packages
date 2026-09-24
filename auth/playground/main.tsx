import { useState, type ReactNode } from "react"
import { createRoot } from "react-dom/client"
import {
  AccountSettings,
  AuthLayout,
  ForgotPasswordForm,
  LoginForm,
  RegisterForm,
  ResetPasswordForm,
  VerifyEmailForm,
} from "../src/components"
import { BRANDS, SUBTITLES, type BrandKey } from "./brands"
import "./styles.css"

// Every call resolves to a rejection after a beat: the screens can then be
// driven into their pending and error states, which is where the spacing and
// the focus behaviour are hardest to get right.
const authClient = {
  signIn: {
    email: () => delay({ error: { message: "Adresse e-mail ou mot de passe incorrect" } }),
    social: () => delay({}),
  },
  signUp: { email: () => delay({}) },
  emailOtp: {
    sendVerificationOtp: () => delay({}),
    verifyEmail: () => delay({ error: { message: "Code invalide. Réessaie." } }),
  },
}

const accountClient = {
  updateUser: () => delay({}),
  changeEmail: () => delay({}),
  changePassword: ({ currentPassword }: { currentPassword: string }) =>
    delay(
      currentPassword === "faux"
        ? { error: { message: "Mot de passe actuel incorrect" } }
        : {},
    ),
}

function delay<T>(value: T): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), 900))
}

interface Opts {
  social: boolean
  brand: (typeof BRANDS)[BrandKey]
  panel?: ReactNode
}

const SCREENS = {
  login: {
    label: "Connexion",
    render: ({ social, panel, brand }: Opts) => (
      <AuthLayout
        panel={panel}
        title={brand.name}
        subtitle={SUBTITLES.login}
      >
        <LoginForm
          authClient={authClient}
          submitClassName={brand.submitClassName}
          coreTokenUrl={null}
          socialProviders={social ? ["google", "github"] : []}
        />
      </AuthLayout>
    ),
  },
  register: {
    label: "Inscription",
    render: ({ social, panel, brand }: Opts) => (
      <AuthLayout
        panel={panel}
        title={brand.name}
        subtitle={SUBTITLES.register}
      >
        <RegisterForm
          authClient={authClient}
          submitClassName={brand.submitClassName}
          socialProviders={social ? ["google", "github"] : []}
        />
      </AuthLayout>
    ),
  },
  verify: {
    label: "Vérification",
    render: ({ panel, brand }: Opts) => (
      <AuthLayout
        panel={panel}
        title={brand.name}
        subtitle={SUBTITLES.verify}
      >
        <VerifyEmailForm
          authClient={authClient}
          submitClassName={brand.submitClassName}
          email="toi@exemple.fr"
        />
      </AuthLayout>
    ),
  },
  forgot: {
    label: "Mot de passe oublié",
    render: ({ panel, brand }: Opts) => (
      <AuthLayout
        panel={panel}
        title={brand.name}
        subtitle={SUBTITLES.forgot}
      >
        <ForgotPasswordForm
          authClient={authClient}
          submitClassName={brand.submitClassName}
        />
      </AuthLayout>
    ),
  },
  reset: {
    label: "Réinitialisation",
    render: ({ panel, brand }: Opts) => (
      <AuthLayout
        panel={panel}
        title={brand.name}
        subtitle={SUBTITLES.reset}
      >
        <ResetPasswordForm
          authClient={authClient}
          submitClassName={brand.submitClassName}
          email="toi@exemple.fr"
        />
      </AuthLayout>
    ),
  },
  account: {
    label: "Paramètres",
    render: ({ social }: Opts) => (
      <div className="min-h-dvh bg-background px-4 py-10 pb-40 sm:px-8">
        <AccountSettings
          client={accountClient}
          user={{ name: "Camille Durand", email: "camille@exemple.fr" }}
          emailChange
          hasPassword={!social}
          setPasswordHref="#forgot"
          deleteAccount={{ endpoint: "/demo/delete-account" }}
          billing={
            <DemoSlot>Le produit monte ici sa page de facturation.</DemoSlot>
          }
          sections={[
            {
              id: "produit",
              label: "Onglet du produit",
              content: (
                <DemoSlot>
                  Chaque app ajoute ici ses propres onglets via sections.
                </DemoSlot>
              ),
            },
          ]}
        />
      </div>
    ),
  },
} as const

function DemoSlot({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-xl border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
      {children}
    </div>
  )
}

type ScreenKey = keyof typeof SCREENS

function Playground() {
  const [screen, setScreen] = useState<ScreenKey>("login")
  const [social, setSocial] = useState(false)
  const [branded, setBranded] = useState(false)
  const [brandKey, setBrandKey] = useState<BrandKey>("partage")
  const brand = BRANDS[brandKey]

  return (
    <>
      {/* Fixed rather than in flow: the screens are min-h-dvh, and a toolbar
          above them would push every one down and misreport its vertical
          rhythm — which is the thing being judged here. Anchored at the bottom
          because the top is where the screens put their own heading. */}
      <div className="fixed bottom-[4.25rem] left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-full border border-foreground/10 bg-card/90 p-1 shadow-lg backdrop-blur">
        {(Object.keys(BRANDS) as BrandKey[]).map((key) => (
          <button
            key={key}
            onClick={() => setBrandKey(key)}
            className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
              brandKey === key
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {BRANDS[key].name}
          </button>
        ))}
      </div>

      <div className="fixed bottom-5 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-full border border-foreground/10 bg-card/90 p-1 shadow-lg backdrop-blur">
        {(Object.keys(SCREENS) as ScreenKey[]).map((key) => (
          <button
            key={key}
            onClick={() => setScreen(key)}
            className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
              screen === key
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {SCREENS[key].label}
          </button>
        ))}
        <span className="mx-1 h-4 w-px bg-border" />
        <button
          onClick={() => setSocial((v) => !v)}
          className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
            social
              ? "bg-foreground text-background"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          Social
        </button>
        <button
          onClick={() => setBranded((v) => !v)}
          className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
            branded
              ? "bg-foreground text-background"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          Panneau
        </button>
      </div>

      {SCREENS[screen].render({
        social,
        brand,
        panel: branded ? brand.panel : undefined,
      })}
    </>
  )
}

createRoot(document.getElementById("root")!).render(<Playground />)
