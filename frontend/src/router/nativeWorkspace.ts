import type { NativeWorkspaceTicket } from '@/api/admin/developmentWorkspace'

const loopbackHosts = new Set(['127.0.0.1', 'localhost', '[::1]'])

export function submitNativeWorkspaceTicket(value: NativeWorkspaceTicket): void {
  if (typeof value?.url !== 'string' || typeof value.ticket !== 'string' || !value.ticket.trim()) {
    throw new Error('Invalid workspace ticket')
  }
  const url = new URL(value.url)
  const loopbackPreview = url.protocol === 'http:' &&
    loopbackHosts.has(url.hostname) && loopbackHosts.has(window.location.hostname)
  if ((!loopbackPreview && url.protocol !== 'https:') ||
      url.pathname !== '/_mile/enter' || url.username || url.password || url.search || url.hash) {
    throw new Error('Invalid workspace gateway URL')
  }

  const form = document.createElement('form')
  form.action = url.href
  form.method = 'post'
  form.rel = 'noreferrer'
  form.hidden = true
  const input = document.createElement('input')
  input.type = 'hidden'
  input.name = 'ticket'
  input.value = value.ticket
  form.append(input)
  document.body.append(form)
  try {
    form.submit()
  } finally {
    form.remove()
  }
}
