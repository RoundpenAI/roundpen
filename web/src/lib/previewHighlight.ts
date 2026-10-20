import { extensionOf } from './workspaceFiles'

// Prism ships only JS/CSS/markup/clike by default (via Semi CodeHighlight).
// Side-effect imports register the languages we preview most often.
import 'prismjs/components/prism-markup.js'
import 'prismjs/components/prism-css.js'
import 'prismjs/components/prism-clike.js'
import 'prismjs/components/prism-javascript.js'
import 'prismjs/components/prism-typescript.js'
import 'prismjs/components/prism-jsx.js'
import 'prismjs/components/prism-tsx.js'
import 'prismjs/components/prism-json.js'
import 'prismjs/components/prism-yaml.js'
import 'prismjs/components/prism-toml.js'
import 'prismjs/components/prism-markdown.js'
import 'prismjs/components/prism-bash.js'
import 'prismjs/components/prism-python.js'
import 'prismjs/components/prism-go.js'
import 'prismjs/components/prism-rust.js'
import 'prismjs/components/prism-java.js'
import 'prismjs/components/prism-c.js'
import 'prismjs/components/prism-cpp.js'
import 'prismjs/components/prism-ruby.js'
import 'prismjs/components/prism-php.js'
import 'prismjs/components/prism-sql.js'

/** Map a filename to a Prism language id, or null for plain text. */
export function languageForFilename(name: string): string | null {
  switch (extensionOf(name)) {
    case 'js':
    case 'mjs':
      return 'javascript'
    case 'ts':
      return 'typescript'
    case 'jsx':
      return 'jsx'
    case 'tsx':
      return 'tsx'
    case 'css':
      return 'css'
    case 'json':
      return 'json'
    case 'yaml':
    case 'yml':
      return 'yaml'
    case 'toml':
      return 'toml'
    case 'md':
    case 'markdown':
      return 'markdown'
    case 'sh':
    case 'bash':
      return 'bash'
    case 'py':
      return 'python'
    case 'go':
      return 'go'
    case 'rs':
      return 'rust'
    case 'java':
      return 'java'
    case 'c':
    case 'h':
      return 'c'
    case 'cpp':
      return 'cpp'
    case 'rb':
      return 'ruby'
    case 'php':
      return 'php'
    case 'sql':
      return 'sql'
    case 'html':
    case 'htm':
    case 'svg':
    case 'xml':
      return 'markup'
    default:
      return null
  }
}
