/** Per-user first-run wizard dismissal (local to this browser). */
const KEY_PREFIX = 'roundpen.onboarding.v1.'

export function onboardingStorageKey(username: string): string {
  return KEY_PREFIX + encodeURIComponent(username.trim() || 'anon')
}

export function isOnboardingDone(username: string): boolean {
  try {
    return localStorage.getItem(onboardingStorageKey(username)) === '1'
  } catch {
    return false
  }
}

export function markOnboardingDone(username: string): void {
  try {
    localStorage.setItem(onboardingStorageKey(username), '1')
  } catch {
    /* private mode / quota */
  }
}

export function clearOnboardingDone(username: string): void {
  try {
    localStorage.removeItem(onboardingStorageKey(username))
  } catch {
    /* ignore */
  }
}
