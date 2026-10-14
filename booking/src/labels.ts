export type Labels = {
  chooseDay: string
  previousDays: string
  nextDays: string
  loading: string
  noAvailability: string
  loadFailed: string
  slots: string
  timeZoneNote: (zone: string) => string
  name: string
  email: string
  note: string
  confirm: string
  sending: string
  confirmed: string
  invitationSent: string
  cancel: string
  failure: (status: number | undefined) => string
}

export const frLabels: Labels = {
  chooseDay: 'Choisissez un jour',
  previousDays: 'Jours précédents',
  nextDays: 'Jours suivants',
  loading: 'Recherche des disponibilités…',
  noAvailability: 'Aucune disponibilité sur ces jours.',
  loadFailed: "Les disponibilités n'ont pas pu être chargées. Réessayez dans un instant.",
  slots: 'Créneaux',
  timeZoneNote: (zone) => `Heures affichées dans votre fuseau : ${zone}`,
  name: 'Nom',
  email: 'E-mail',
  note: 'Message (facultatif)',
  confirm: 'Confirmer le rendez-vous',
  sending: 'Envoi…',
  confirmed: 'Rendez-vous confirmé',
  invitationSent: 'Une invitation vous a été envoyée par e-mail.',
  cancel: 'Annuler ce rendez-vous',
  failure: (status) => {
    if (status === 409) return "Ce créneau vient d'être pris. Choisissez-en un autre."
    if (status === 429) return 'Trop de tentatives. Réessayez dans un instant.'
    if (status === 400) return 'Vérifiez votre nom et votre adresse e-mail.'
    return "La réservation n'a pas abouti. Réessayez."
  },
}
