import { webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createWorkspaceAttempts } from './workspaceAttempts'

describe('development workspace retry identity', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.stubGlobal('crypto', webcrypto)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('reuses the same request after reopening a task without storing the instruction', async () => {
    const payload = { id: 'one', message: 'sensitive prompt' }
    const first = await createWorkspaceAttempts(2).requestId('send', payload)
    const reopened = createWorkspaceAttempts(2)
    expect(await reopened.requestId('send', payload)).toBe(first)
    expect(sessionStorage.getItem('development-workspace:2:send')).not.toContain(payload.message)

    reopened.finish('send')
    expect(await createWorkspaceAttempts(2).requestId('send', payload)).not.toBe(first)
  })

  it('uses different request IDs for changed input, admins and expired attempts', async () => {
    vi.useFakeTimers()
    const payload = { message: 'initial task', branch: 'main' }
    const first = await createWorkspaceAttempts(2).requestId('create', payload)
    expect(await createWorkspaceAttempts(3).requestId('create', payload)).not.toBe(first)
    const second = await createWorkspaceAttempts(2).requestId('create', { ...payload, branch: 'dev' })
    expect(second).not.toBe(first)
    vi.advanceTimersByTime(60 * 60 * 1000)
    expect(await createWorkspaceAttempts(2).requestId('create', { ...payload, branch: 'dev' })).not.toBe(second)
  })
})
