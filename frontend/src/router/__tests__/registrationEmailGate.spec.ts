import { beforeEach, describe, expect, it, vi } from 'vitest'
import { registrationEmailGateRedirect } from '@/router/registrationEmailGate'

const { checkRegistrationEmailMock } = vi.hoisted(() => ({
  checkRegistrationEmailMock: vi.fn()
}))

vi.mock('@/api/auth', () => ({
  checkRegistrationEmail: (...args: unknown[]) => checkRegistrationEmailMock(...args)
}))

describe('registrationEmailGateRedirect', () => {
  beforeEach(() => {
    sessionStorage.clear()
    checkRegistrationEmailMock.mockReset()
  })

  it('sends an unapproved email back to the register form before the code page opens', async () => {
    sessionStorage.setItem(
      'register_data',
      JSON.stringify({ email: 'student@school.edu', password: 'secret-123' })
    )
    checkRegistrationEmailMock.mockRejectedValueOnce({ reason: 'EMAIL_NOT_APPROVED' })

    await expect(registrationEmailGateRedirect()).resolves.toBe('/register')
    expect(sessionStorage.getItem('register_data')).toBeNull()
    expect(sessionStorage.getItem('register_block_code')).toBe('EMAIL_NOT_APPROVED')
  })

  it('lets an approved email continue to the verification page', async () => {
    sessionStorage.setItem(
      'register_data',
      JSON.stringify({ email: 'student@school.edu', password: 'secret-123' })
    )
    checkRegistrationEmailMock.mockResolvedValueOnce(undefined)

    await expect(registrationEmailGateRedirect()).resolves.toBeNull()
    expect(sessionStorage.getItem('register_data')).toContain('student@school.edu')
  })

  it('does not intercept a pending oauth completion', async () => {
    sessionStorage.setItem(
      'register_data',
      JSON.stringify({ email: 'student@school.edu', pending_provider: 'google' })
    )

    await expect(registrationEmailGateRedirect()).resolves.toBeNull()
    expect(checkRegistrationEmailMock).not.toHaveBeenCalled()
  })
})
