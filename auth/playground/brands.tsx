import { AuthWordmark } from "../src/components"
import { BrandPanel } from "./brand-panel"

export interface Brand {
  /** Set as the screen's title — the app's name is what someone checks first */
  name: string
  /** Optional mark beside the name */
  logo: React.ReactNode
  panel: React.ReactNode
  /** Replaces the submit button's colour utilities */
  submitClassName: string
}

/** One short line under the app name, per screen. */
export const SUBTITLES = {
  login: "Connecte-toi à ton compte",
  register: "Crée ton compte",
  verify: "Vérifie ton adresse",
  forgot: "Récupère ton accès",
  reset: "Choisis un nouveau mot de passe",
} as const

const dot = (className: string) => (
  <span className={`size-2 rounded-full ${className}`} />
)

/**
 * The apps consuming this package, each with the accent its own stylesheet
 * defines. Switching between them in the playground is how a change gets
 * checked against every surface it will land on rather than against one.
 */
export const BRANDS: Record<string, Brand> = {
  partage: {
    name: "Partage",
    logo: <AuthWordmark mark={dot("bg-zinc-900")}>partage</AuthWordmark>,
    panel: (
      <BrandPanel
        gradient="from-zinc-600 via-zinc-800 to-zinc-950"
        title="Écris une fois. Publie partout."
        tagline="Un brouillon, réécrit pour chaque plateforme, programmé à l'heure que tu choisis."
      />
    ),
    submitClassName: "bg-zinc-900 text-white hover:bg-zinc-800",
  },
  synthiz: {
    name: "Synthiz",
    logo: <AuthWordmark mark={dot("bg-violet-500")}>synthiz</AuthWordmark>,
    panel: (
      <BrandPanel
        gradient="from-violet-400 via-violet-500 to-purple-600"
        title="Tes sources, synthétisées."
        tagline="Transcris, résume et relie ce que tu lis en une timeline éditoriale."
      />
    ),
    submitClassName: "bg-violet-500 text-white hover:bg-violet-600",
  },
  spore: {
    name: "Spore",
    logo: <AuthWordmark mark={dot("bg-emerald-500")}>spore</AuthWordmark>,
    panel: (
      <BrandPanel
        gradient="from-emerald-400 via-emerald-500 to-teal-600"
        title="L'e-mail qui part vraiment."
        tagline="Transactionnel, files d'attente et suivi de remise, sans le fournisseur."
      />
    ),
    submitClassName: "bg-emerald-600 text-white hover:bg-emerald-700",
  },
  lungor: {
    name: "Lungor",
    logo: <AuthWordmark mark={dot("bg-[#ffd400]")}>lungor</AuthWordmark>,
    panel: (
      <BrandPanel
        gradient="from-[#fff08a] via-[#ffd400] to-[#ffb800]"
        tone="dark"
        title="Facture ce que tes clients consomment."
        tagline="Plans, compteurs et quotas, branchés sur ton produit par un SDK."
      />
    ),
    submitClassName: "bg-neutral-900 text-white hover:bg-neutral-800",
  },
}

export type BrandKey = keyof typeof BRANDS
