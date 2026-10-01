import type { Citation, FoundSource } from "../src"

const wait = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms))

export async function fakeRevise(passage: string, instruction: string): Promise<string> {
  await wait(700)
  if (/raccourcis/i.test(instruction)) return passage.split(/[.!?]/)[0].trim() + "."
  return `${passage.trim()} (${instruction.toLowerCase()})`
}

export async function fakeFindSources(passage: string): Promise<FoundSource[]> {
  await wait(900)
  const topic = passage.split(/\s+/).slice(0, 4).join(" ")
  return [
    { title: `À propos de « ${topic} »`, url: "https://www.example.org/article", excerpt: "Un extrait qui explique pourquoi la source est pertinente." },
    { title: "Une seconde lecture", url: "https://example.com/etude", excerpt: "" },
  ]
}

export async function fakeTranscribe(audio: Blob): Promise<string> {
  await wait(600)
  return `transcription simulée de ${Math.round(audio.size / 1024)} Ko`
}

export const SAMPLE_ANSWER = {
  question: "Que retenir de l'installation ?",
  answer: "L'installation a pris trois semaines [1].\n\nLe coût est resté dans le budget [2].",
  citations: [
    { index: 1, title: "Compte rendu de chantier", href: "https://example.org/chantier" },
    { index: 2, title: "Budget 2026", href: "https://example.org/budget" },
  ] satisfies Citation[],
}
