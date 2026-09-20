import type { KeysCopy } from "./types";

export const defaultCopy: KeysCopy = {
  title: "Tes clés",
  description:
    "Une clé laisse ton code appeler ce produit en ton nom. Donne-lui un nom qui te dise où elle sert.",
  labelPlaceholder: "prod",
  create: "Créer une clé",
  creating: "Création…",
  empty: "Tu n'as pas encore de clé.",
  columnLabel: "Nom",
  columnId: "Identifiant",
  columnScopes: "Droits",
  columnCreated: "Créée le",
  revoke: "Révoquer",
  revoking: "Révocation…",
  confirmRevoke: (label) =>
    `Révoquer « ${label} » ? Le code qui s'en sert cessera d'être accepté, et cette clé ne peut pas être rétablie.`,
  secretTitle: "Voici ta clé",
  secretWarning:
    "Copie-la maintenant. Elle ne sera plus jamais affichée, et personne ne peut la retrouver.",
  copy: "Copier",
  copied: "Copiée",
  dismiss: "J'ai copié la clé",
  errors: {
    unauthorized: "Ta session a expiré. Reconnecte-toi.",
    forbidden: "Tu n'as pas le droit de créer une clé pour ce produit.",
    invalid: "Donne un nom à cette clé.",
    // A revocation can answer this after refusing to delete anything, so the
    // wording must not read as "it worked" for either call.
    unavailable:
      "Ça n'a pas abouti. Rien n'a changé — réessaie dans un instant.",
  },
};
