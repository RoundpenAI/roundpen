type PickResult = { code?: number; msg?: string; data?: string[] }

type TrimSdk = {
  isStandaloneWeb?: boolean
  pickUserFile: (params: {
    directory: boolean
    title?: string
    sidebarGroup?: string[]
  }) => Promise<PickResult | undefined>
}

// pickUserDirectory opens the fnOS picker for one folder in the user's own files.
export async function pickUserDirectory(title: string): Promise<string | null> {
  const mod = (await import('@trimjs/web-app')) as { TrimApp: new () => TrimSdk }
  const sdk = new mod.TrimApp()
  if (sdk.isStandaloneWeb) {
    throw new Error('standalone')
  }
  const res = await sdk.pickUserFile({
    directory: true,
    title,
    sidebarGroup: ['myFiles'],
  })
  const path = res?.data?.[0]?.trim()
  if (!path) return null
  return path
}
