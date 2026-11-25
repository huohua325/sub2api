const retentionMs = 60 * 60 * 1000

interface Attempt {
  fingerprint: string
  requestId: string
  createdAt: number
}

function hex(bytes: Uint8Array) {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

function newId() {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const value = hex(bytes)
  return `${value.slice(0, 8)}-${value.slice(8, 12)}-${value.slice(12, 16)}-${value.slice(16, 20)}-${value.slice(20)}`
}

function validAttempt(attempt: Attempt | undefined, fingerprint: string, now: number) {
  return attempt && attempt.fingerprint === fingerprint && attempt.createdAt <= now &&
    now - attempt.createdAt < retentionMs && /^[a-f0-9-]{36}$/.test(attempt.requestId)
}

export function createWorkspaceAttempts(adminId: number) {
  const inMemory = new Map<string, Attempt>()
  const storageKey = (action: string) => `development-workspace:${adminId}:${action}`

  async function requestId(action: string, payload: unknown) {
    const body = JSON.stringify(payload)
    const canPersist = Boolean(crypto.subtle) && adminId > 0
    // Only a digest and the matching request ID survive a reload.
    const fingerprint = canPersist
      ? hex(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(body))))
      : body
    let previous = inMemory.get(action)
    if (!previous && canPersist) {
      try {
        const stored = sessionStorage.getItem(storageKey(action))
        if (stored) previous = JSON.parse(stored) as Attempt
      } catch {
        // Some browser privacy settings disable session storage.
      }
    }
    const now = Date.now()
    if (previous && validAttempt(previous, fingerprint, now)) return previous.requestId
    const attempt = { fingerprint, requestId: newId(), createdAt: now }
    inMemory.set(action, attempt)
    if (canPersist) {
      try {
        sessionStorage.setItem(storageKey(action), JSON.stringify(attempt))
      } catch {
        // The in-memory attempt still allows safe retries in the current page.
      }
    }
    return attempt.requestId
  }

  function finish(action: string) {
    inMemory.delete(action)
    try {
      sessionStorage.removeItem(storageKey(action))
    } catch {
      // The browser may disallow storage access.
    }
  }

  return { requestId, finish }
}
