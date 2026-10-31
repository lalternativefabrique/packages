import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createVoicePlayer } from './voicePlayer.ts'

class FakeContext {
  currentTime = 0
  static resumed = 0
  static live: FakeSource[] = []
  destination = {}
  resume() {
    FakeContext.resumed++
    return Promise.resolve()
  }
  close() {
    return Promise.resolve()
  }
  decodeAudioData() {
    return Promise.resolve({ duration: 2 })
  }
  createBufferSource() {
    const source = new FakeSource()
    FakeContext.live.push(source)
    return source
  }
}

class FakeSource {
  buffer: unknown
  playbackRate = { value: 1 }
  onended: (() => void) | null = null
  stopped = false
  connect() {}
  start() {}
  stop() {
    this.stopped = true
  }
}

function install(response: () => Response) {
  FakeContext.resumed = 0
  FakeContext.live = []
  Object.assign(globalThis, { AudioContext: FakeContext, fetch: async () => response() })
}

const audio = () => new Response(new Uint8Array([1]), { headers: { 'Content-Type': 'audio/mpeg' } })
const resolve = async () => ({ url: 'https://v/x', body: {}, text: 'Bonjour.' })
const settled = () => new Promise((r) => setTimeout(r, 5))

test('resumes the context inside the gesture, before any await', () => {
  install(audio)
  const player = createVoicePlayer(resolve)
  player.toggle()
  assert.equal(FakeContext.resumed, 1)
  player.stop()
})

test('plays, pauses and returns to idle when the reading ends', async () => {
  install(audio)
  const player = createVoicePlayer(resolve)
  let changes = 0
  player.subscribe(() => changes++)
  player.toggle()
  assert.equal(player.getSnapshot().state, 'loading')
  await settled()
  assert.equal(player.getSnapshot().state, 'playing')
  assert.equal(player.getSnapshot().text, 'Bonjour.')
  assert.equal(player.getSnapshot().duration, 2)
  assert.ok(changes > 0)

  player.toggle()
  assert.equal(player.getSnapshot().state, 'paused')
  assert.equal(FakeContext.live[0].stopped, true)

  player.toggle()
  assert.equal(player.getSnapshot().state, 'playing')
  FakeContext.live[1].onended?.()
  assert.deepEqual(player.getSnapshot(), { state: 'idle', position: 0, buffered: 0, duration: null, rate: 1, text: null })
})

test('a refused reading is unavailable and stop keeps it so', async () => {
  install(() => new Response('', { status: 500 }))
  const player = createVoicePlayer(resolve)
  player.toggle()
  await settled()
  assert.equal(player.getSnapshot().state, 'unavailable')
  player.stop()
  assert.equal(player.getSnapshot().state, 'unavailable')
})

test('keeps the rate across snapshots and stops notifying after unsubscribe', () => {
  install(audio)
  const player = createVoicePlayer(resolve)
  let changes = 0
  const off = player.subscribe(() => changes++)
  player.setRate(1.5)
  assert.equal(player.getSnapshot().rate, 1.5)
  off()
  player.setRate(1)
  assert.equal(changes, 1)
})
