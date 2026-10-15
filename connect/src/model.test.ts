import { describe, expect, it } from 'vitest'
import { codeOf, currentChoice, isStored, linkRequest, readOutcome, statusOf } from './model'
import type { Resource } from './model'

const resource = (link: Resource['link'], suggested = ''): Resource => ({
  id: 'r1', type: 'brand', label: 'Lafab', url: '', parent: '', depth: 0, mappable: true, suggested, link,
})

describe('choices', () => {
  it('starts from the stored link, then the suggestion', () => {
    expect(currentChoice(resource({ state: 'linked', app: 'a' }))).toBe('link:a')
    expect(currentChoice(resource({ state: 'ignored', app: '' }))).toBe('ignore')
    expect(currentChoice(resource({ state: 'none', app: '' }, 'b'))).toBe('link:b')
    expect(currentChoice(resource({ state: 'none', app: '' }))).toBe('')
  })

  it('knows a choice that is already saved', () => {
    expect(isStored(resource({ state: 'linked', app: 'a' }), 'link:a')).toBe(true)
    expect(isStored(resource({ state: 'linked', app: 'a' }), 'link:b')).toBe(false)
    expect(isStored(resource({ state: 'none', app: '' }, 'a'), 'link:a')).toBe(false)
  })

  it('turns a choice into the request go/connect expects', () => {
    expect(linkRequest('r1', 'link:a')).toEqual({ resource_id: 'r1', choice: 'link', app: 'a' })
    expect(linkRequest('r1', 'ignore')).toEqual({ resource_id: 'r1', choice: 'ignore' })
    expect(linkRequest('r1', 'create')).toEqual({ resource_id: 'r1', choice: 'create' })
  })
})

describe('errors and outcomes', () => {
  it('reads a status from a fetch error or an axios-like one', () => {
    expect(statusOf({ status: 409 })).toBe(409)
    expect(statusOf({ response: { status: 503 } })).toBe(503)
    expect(statusOf('boom')).toBeUndefined()
  })

  it('reads the code a product refused with', () => {
    expect(codeOf({ message: 'linked_elsewhere' })).toBe('linked_elsewhere')
    expect(codeOf({ code: 'name_taken', message: 'x' })).toBe('name_taken')
  })

  it('accepts only known outcomes from the URL', () => {
    expect(readOutcome('refused')).toBe('refused')
    expect(readOutcome('hacked')).toBeUndefined()
  })
})
