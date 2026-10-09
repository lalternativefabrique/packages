export type Labels = {
  microphone: string
  camera: string
  shareScreen: string
  stopScreen: string
  screenLimit: string
  leave: string
  grid: string
  stage: string
  screens: string
  pinForAll: string
  unpin: string
  pinnedForAll: string
  screenOf: (name: string) => string
  connecting: string
}

export const frLabels: Labels = {
  microphone: 'Micro',
  camera: 'Caméra',
  shareScreen: 'Partager un écran',
  stopScreen: 'Arrêter',
  screenLimit: "Limite d'écrans partagés atteinte",
  leave: 'Quitter',
  grid: 'Grille',
  stage: 'Scène',
  screens: 'Écrans',
  pinForAll: 'Épingler pour tous',
  unpin: 'Désépingler',
  pinnedForAll: 'Épinglé pour tous',
  screenOf: (name) => `Écran de ${name}`,
  connecting: 'Connexion…',
}
