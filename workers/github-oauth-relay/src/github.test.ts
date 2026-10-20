import assert from 'node:assert/strict'
import { afterEach, describe, it } from 'node:test'
import { githubProfile } from './github.ts'

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('githubProfile', () => {
  it('uses the public email from /user when present', async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = String(input)
      assert.equal(url, 'https://api.github.com/user')
      return Response.json({ login: 'octo', name: 'Octo', email: 'octo@example.com' })
    }) as typeof fetch

    const profile = await githubProfile('tok')
    assert.deepEqual(profile, {
      subject: 'octo',
      login: 'octo',
      name: 'Octo',
      email: 'octo@example.com',
    })
  })

  it('falls back to verified primary email from /user/emails', async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === 'https://api.github.com/user') {
        return Response.json({ login: 'octo', name: 'Octo', email: null })
      }
      if (url === 'https://api.github.com/user/emails') {
        return Response.json([
          { email: 'other@example.com', primary: false, verified: true },
          { email: 'primary@example.com', primary: true, verified: true },
        ])
      }
      throw new Error(`unexpected url ${url}`)
    }) as typeof fetch

    const profile = await githubProfile('tok')
    assert.equal(profile.email, 'primary@example.com')
  })

  it('continues without email when /user/emails returns 403', async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === 'https://api.github.com/user') {
        return Response.json({ login: 'octo', name: 'Octo', email: null })
      }
      if (url === 'https://api.github.com/user/emails') {
        return new Response('forbidden', { status: 403 })
      }
      throw new Error(`unexpected url ${url}`)
    }) as typeof fetch

    const profile = await githubProfile('tok')
    assert.deepEqual(profile, {
      subject: 'octo',
      login: 'octo',
      name: 'Octo',
      email: '',
    })
  })
})
