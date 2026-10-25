import { test } from 'node:test'
import assert from 'node:assert/strict'
import { CHARS_PER_SECOND, fold, locate, sentenceAt, sentences, spokenOffset } from './readAlong.ts'

const text = 'Titre\n\nPremière phrase. Deuxième ? Oui… 3.5 reste entier.'

test('sentences cut on terminal punctuation and newlines, trimmed', () => {
  const spans = sentences(text).map((s) => text.slice(s.start, s.end))
  assert.deepEqual(spans, ['Titre', 'Première phrase.', 'Deuxième ?', 'Oui…', '3.5 reste entier.'])
})

test('sentences of an empty text is empty', () => {
  assert.deepEqual(sentences('  \n '), [])
})

test('spokenOffset is proportional once the duration is known', () => {
  assert.equal(spokenOffset(5, 10, 300), 150)
  assert.equal(spokenOffset(20, 10, 300), 300)
})

test('spokenOffset falls back to a speaking rate while streaming', () => {
  assert.equal(spokenOffset(2, null, 1000), 2 * CHARS_PER_SECOND)
  assert.equal(spokenOffset(-1, null, 1000), 0)
})

test('sentenceAt finds the span holding the offset, or the one before a gap', () => {
  const spans = sentences(text)
  assert.equal(sentenceAt(spans, 0), 0)
  assert.equal(sentenceAt(spans, text.indexOf('Deuxième') + 2), 2)
  assert.equal(sentenceAt(spans, text.length), spans.length - 1)
  assert.equal(sentenceAt([{ start: 4, end: 6 }], 1), -1)
})

test('fold keeps letters and digits, unaccented, and maps back to the source', () => {
  const f = fold('« Été, 2 fois »')
  assert.equal(f.chars, 'ete2fois')
  assert.equal(f.source[0], 2)
  assert.equal(f.source[3], 7)
})

test('locate matches across markup the spoken text no longer has', () => {
  const page = fold('- **Première** phrase. [Deuxième](x) ?').chars
  const spoken = fold('Première phrase.').chars
  assert.deepEqual(locate(page, spoken, 0), { start: 0, end: spoken.length })
})

test('locate searches forward from the cursor, so a repeated sentence is the next one', () => {
  const page = fold('Oui. Non. Oui.').chars
  assert.deepEqual(locate(page, 'oui', 1), { start: 6, end: 9 })
})

test('locate falls back to the opening of a long sentence the page ends differently', () => {
  const page = fold('Une phrase assez longue pour avoir un préfixe, suivie de la suite affichée.').chars
  const spoken = fold('Une phrase assez longue pour avoir un préfixe, puis autre chose.').chars
  assert.deepEqual(locate(page, spoken, 0), { start: 0, end: 24 })
  assert.equal(locate(page, 'absent', 0), null)
})
