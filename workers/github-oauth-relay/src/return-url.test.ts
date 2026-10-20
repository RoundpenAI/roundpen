import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { isLocalOrPrivateHost, nasCloudCallback, parseReturnOrigin } from './return-url.ts'

describe('parseReturnOrigin', () => {
  it('keeps LAN http origins', () => {
    assert.equal(parseReturnOrigin('http://192.168.1.8:9527/ignored'), 'http://192.168.1.8:9527')
    assert.equal(parseReturnOrigin('http://nas.local'), 'http://nas.local')
    assert.equal(parseReturnOrigin('http://10.0.0.9:19001'), 'http://10.0.0.9:19001')
  })

  it('allows public https', () => {
    assert.equal(parseReturnOrigin('https://roundpen.example:443'), 'https://roundpen.example')
  })

  it('rejects public http and credentials', () => {
    assert.throws(() => parseReturnOrigin('http://example.com'), /LAN/)
    assert.throws(() => parseReturnOrigin('https://user:pass@example.com'), /credentials/)
    assert.throws(() => parseReturnOrigin('javascript:alert(1)'), /http or https/)
  })
})

describe('isLocalOrPrivateHost', () => {
  it('recognizes RFC1918 and localhost', () => {
    assert.equal(isLocalOrPrivateHost('127.0.0.1'), true)
    assert.equal(isLocalOrPrivateHost('172.16.4.1'), true)
    assert.equal(isLocalOrPrivateHost('localhost'), true)
    assert.equal(isLocalOrPrivateHost('example.com'), false)
  })
})

describe('nasCloudCallback', () => {
  it('lands on the cloud callback path', () => {
    const u = nasCloudCallback('http://192.168.1.8:9527', '/v1/auth/oauth/github/cloud/callback', {
      ticket: 'abc',
      state: 's',
    })
    assert.equal(u, 'http://192.168.1.8:9527/v1/auth/oauth/github/cloud/callback?ticket=abc&state=s')
    const g = nasCloudCallback('https://roundpen.example', '/v1/connectors/google/cloud/callback', {
      ticket: 'abc',
      state: 's',
    })
    assert.equal(g, 'https://roundpen.example/v1/connectors/google/cloud/callback?ticket=abc&state=s')
  })

  it('rejects a callback path outside the registry', () => {
    assert.throws(() => nasCloudCallback('https://roundpen.example', '/v1/elsewhere', { ticket: 'abc' }), /registered/)
  })
})
