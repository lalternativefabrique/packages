export interface ReviewLabels {
  before: string;
  after: string;
  accept: string;
  reject: string;
  rework: string;
  undo: string;
  send: string;
  cancel: string;
  instructionPlaceholder: string;
  reworking: string;
  reworked: string;
  acceptAll: string;
  rejectAll: string;
  nextPending: string;
  apply: string;
  discard: string;
  pending: (n: number) => string;
  accepted: (n: number) => string;
  rejected: (n: number) => string;
  noChange: string;
  reworkFailed: string;
  keptOriginal: string;
}

export const defaultLabels: ReviewLabels = {
  before: "Avant",
  after: "Après",
  accept: "Accepter",
  reject: "Rejeter",
  rework: "Retravailler",
  undo: "Revenir",
  send: "Envoyer",
  cancel: "Annuler",
  instructionPlaceholder: "Ce qui ne va pas dans ce passage…",
  reworking: "Retravail en cours…",
  reworked: "Retravaillé",
  acceptAll: "Tout accepter",
  rejectAll: "Tout rejeter",
  nextPending: "Passage suivant",
  apply: "Appliquer",
  discard: "Abandonner",
  pending: (n) => `${n} à décider`,
  accepted: (n) => `${n} accepté${n > 1 ? "s" : ""}`,
  rejected: (n) => `${n} rejeté${n > 1 ? "s" : ""}`,
  noChange: "Aucun changement.",
  reworkFailed: "Le retravail a échoué.",
  keptOriginal: "Les passages non décidés gardent le texte d'origine.",
};
