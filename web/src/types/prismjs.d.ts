declare module 'prismjs' {
  const Prism: {
    languages: Record<string, unknown>
    highlightElement: (element: Element, async?: boolean) => void
    manual?: boolean
  }
  export default Prism
}

declare module 'prismjs/components/*' {}
