import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

describe('prism php preview deps', () => {
  it('loads markup-templating before php so highlight does not throw', async () => {
    const Prism = (await import('prismjs')).default
    ;(globalThis as typeof globalThis & { Prism: typeof Prism }).Prism = Prism

    await import('prismjs/components/prism-markup.js')
    await import('prismjs/components/prism-markup-templating.js')
    await import('prismjs/components/prism-php.js')

    assert.equal(
      typeof Prism.languages['markup-templating']?.tokenizePlaceholders,
      'function',
    )
    const out = Prism.highlight(
      '<?php echo "hi";',
      Prism.languages.php,
      'php',
    )
    assert.match(out, /echo/)
  })
})
