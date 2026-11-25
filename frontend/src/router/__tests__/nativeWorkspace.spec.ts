import { afterEach, describe, expect, it, vi } from 'vitest'
import { submitNativeWorkspaceTicket } from '@/router/nativeWorkspace'

describe('submitNativeWorkspaceTicket', () => {
  afterEach(() => vi.restoreAllMocks())

  it('submits the ticket in the POST body without leaving it in the dashboard', () => {
    const submit = vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
      expect(this.isConnected).toBe(true)
      expect(this.action).toBe('https://t3.example.test/_mile/enter')
      expect(this.method).toBe('post')
      expect([...new FormData(this)]).toEqual([['ticket', 'secret-test-ticket']])
    })

    submitNativeWorkspaceTicket({ url: 'https://t3.example.test/_mile/enter', ticket: 'secret-test-ticket' })

    expect(submit).toHaveBeenCalledOnce()
    expect(document.querySelector('input[name="ticket"]')).toBeNull()
  })

  it.each([
    'http://t3.example.test/_mile/enter',
    'https://t3.example.test/_mile/enter?token=secret',
    'https://t3.example.test/_mile/enter#secret',
    'https://user:pass@t3.example.test/_mile/enter',
    'https://t3.example.test/other',
    'javascript:alert(1)',
    '//t3.example.test/_mile/enter',
  ])('rejects gateway URL %s without submitting the ticket', (url) => {
    const submit = vi.spyOn(HTMLFormElement.prototype, 'submit')

    expect(() => submitNativeWorkspaceTicket({ url, ticket: 'secret-test-ticket' })).toThrow()
    expect(submit).not.toHaveBeenCalled()
  })
})
