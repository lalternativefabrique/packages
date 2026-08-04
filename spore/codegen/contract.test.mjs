import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const contract = JSON.parse(
  readFileSync(resolve(import.meta.dirname, '..', 'openapi.json'), 'utf8'),
)

const sdkFacingPaths = [
  '/api-keys',
  '/emails',
  '/emails/{id}',
  '/identities',
  '/identities/{id}',
  '/suppressions',
  '/templates',
  '/webhooks/endpoints',
]

test('uses the public OpenAPI 3 contract served by Spore', () => {
  assert.match(contract.openapi, /^3\./)
  assert.deepEqual(contract.servers, [{ url: 'https://api.sporee.fr' }])
  assert.equal(contract.components.securitySchemes.BearerAuth.name, 'Authorization')
  assert.equal(contract.components.securitySchemes.BearerAuth.in, 'header')
})

test('describes every essential SDK-facing route', () => {
  for (const path of sdkFacingPaths) {
    assert.ok(contract.paths[path], `missing SDK route: ${path}`)
  }
})

test('gives every operation a unique stable identifier', () => {
  const seen = new Set()
  for (const [path, item] of Object.entries(contract.paths)) {
    for (const [method, operation] of Object.entries(item)) {
      if (!['get', 'post', 'put', 'patch', 'delete'].includes(method)) continue
      assert.ok(operation.operationId, `${method.toUpperCase()} ${path} has no operationId`)
      assert.ok(!seen.has(operation.operationId), `duplicate operationId: ${operation.operationId}`)
      seen.add(operation.operationId)
    }
  }
})
