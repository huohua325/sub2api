import { checkRegistrationEmail } from '@/api/auth'
import { extractApiErrorCode } from '@/utils/apiError'

const BLOCKED_REGISTRATION_EMAIL_CODES = new Set(['EMAIL_NOT_APPROVED', 'EMAIL_EXISTS'])

function readRegisterData(): Record<string, unknown> | null {
  if (typeof sessionStorage === 'undefined') return null
  const raw = sessionStorage.getItem('register_data')
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object') return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

function textField(data: Record<string, unknown>, key: string): string {
  const value = data[key]
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * Stop the verification page from opening for an email that cannot register.
 * Pending OAuth completion keeps its own checks. Returns the register path when blocked.
 */
export async function registrationEmailGateRedirect(): Promise<string | null> {
  const data = readRegisterData()
  if (!data) return null
  const email = textField(data, 'email')
  if (!email) return null
  if (textField(data, 'pending_provider') || textField(data, 'pending_auth_token')) return null

  try {
    await checkRegistrationEmail(email)
    return null
  } catch (error) {
    const code = extractApiErrorCode(error)
    if (!code || !BLOCKED_REGISTRATION_EMAIL_CODES.has(code)) return null
    sessionStorage.setItem('register_block_code', code)
    sessionStorage.removeItem('register_data')
    return '/register'
  }
}
