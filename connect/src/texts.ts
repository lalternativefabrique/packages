import type { Outcome } from './model'

export type Texts = {
  outcome: (outcome: Outcome, product: string) => string
  loadFailed: string
  retry: string
  unavailable: string
  connect: (product: string) => string
  reconnect: string
  disconnect: string
  disconnectProduct: (product: string) => string
  disconnectWarning: (product: string) => string
  disconnectFailed: string
  cancel: string
  startFailed: string
  connectedOn: (date: string) => string
  notConnected: string
  removed: string
  removedHint: (product: string) => string
  notConnectedHint: (product: string) => string
  productDown: (product: string) => string
  nothingShared: (product: string) => string
  groups: string
  choose: string
  linkTo: string
  create: (label: string) => string
  ignore: string
  save: string
  choiceLabel: (label: string) => string
  ignored: string
  linkedTo: (name: string) => string
  suggested: string
  notLinked: string
  saveFailed: string
  linkedElsewhere: string
  nameTaken: string
}

export function frenchTexts(host: string, entity = 'app'): Texts {
  return {
    outcome: (outcome, product) => {
      switch (outcome) {
        case 'connected':
          return `${product} est connecté. Relie maintenant chaque élément partagé à l’${entity} ${host} qui le suit.`
        case 'refused':
          return `Tu as refusé l’accès sur urbangate : ${host} ne lit rien de ${product}.`
        case 'invalid':
          return 'Cette connexion a expiré ou a déjà servi. Recommence depuis le bouton.'
        case 'unavailable':
          return 'urbangate ne répond pas pour le moment. Réessaie dans quelques minutes.'
      }
    },
    loadFailed: 'Les connexions ne se chargent pas pour le moment.',
    retry: 'Réessayer',
    unavailable: `Les connexions ne sont pas encore ouvertes sur ${host}.`,
    connect: (product) => `Connecter ${product}`,
    reconnect: 'Reconnecter',
    disconnect: 'Déconnecter',
    disconnectProduct: (product) => `Déconnecter ${product}`,
    disconnectWarning: (product) =>
      `${host} ne lira plus ce que tu partages de ${product}, et urbangate lui retire l’accès. Les liens que tu as faits restent en place.`,
    disconnectFailed: 'urbangate ne répond pas : la connexion est conservée, réessaie plus tard.',
    cancel: 'Annuler',
    startFailed: 'urbangate ne répond pas, réessaie.',
    connectedOn: (date) => `Connecté le ${date}`,
    notConnected: 'Pas connecté',
    removed: 'Accès retiré',
    removedHint: (product) =>
      `L’accès a été retiré sur urbangate : ${host} ne lit plus rien de ${product}. Les liens restent en place. Reconnecte-le pour reprendre.`,
    notConnectedHint: (product) => `Tu choisiras sur urbangate ce que ${host} peut lire de ${product}.`,
    productDown: (product) => `${product} ne répond pas pour le moment. La connexion est conservée.`,
    nothingShared: (product) => `Rien de partagé pour l’instant. Reconnecte ${product} pour choisir ce que tu partages.`,
    groups: 'Regroupe les éléments ci-dessous',
    choose: 'Choisir…',
    linkTo: 'Relier à',
    create: (label) => `Créer « ${label} »`,
    ignore: 'Ignorer',
    save: 'Enregistrer',
    choiceLabel: (label) => `Ce que ${host} fait de ${label}`,
    ignored: 'Ignorée',
    linkedTo: (name) => `Reliée à ${name}`,
    suggested: 'Suggestion, pas encore reliée',
    notLinked: 'Pas encore reliée',
    saveFailed: 'Pas enregistré, réessaie',
    linkedElsewhere: 'Déjà reliée ailleurs',
    nameTaken: 'Ce nom est déjà pris',
  }
}
