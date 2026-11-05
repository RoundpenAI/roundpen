import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { buildUpstreamURL, llmUpstreamPath, mapFreeModel } from './llm.ts'

describe('llmUpstreamPath', () => {
  it('strips /llm prefix', () => {
    assert.equal(llmUpstreamPath('/llm/v1/chat/completions'), '/v1/chat/completions')
    assert.equal(llmUpstreamPath('/llm/v1/models'), '/v1/models')
    assert.equal(llmUpstreamPath('/llm'), '/v1/models')
  })
})

describe('mapFreeModel', () => {
  it('maps roundpen-free and empty to the upstream free model', () => {
    assert.equal(mapFreeModel('roundpen-free', 'openrouter/free'), 'openrouter/free')
    assert.equal(mapFreeModel('', 'openrouter/free'), 'openrouter/free')
    assert.equal(mapFreeModel('openai/gpt-4o-mini', 'openrouter/free'), 'openai/gpt-4o-mini')
  })
})

describe('buildUpstreamURL', () => {
  it('joins without doubling /v1', () => {
    assert.equal(
      buildUpstreamURL('https://openrouter.ai/api/v1', '/v1/chat/completions', ''),
      'https://openrouter.ai/api/v1/chat/completions',
    )
    assert.equal(
      buildUpstreamURL('https://openrouter.ai/api', '/v1/chat/completions', ''),
      'https://openrouter.ai/api/v1/chat/completions',
    )
  })
})
