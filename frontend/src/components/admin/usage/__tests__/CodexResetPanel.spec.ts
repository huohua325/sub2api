import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexResetPanel from '../CodexResetPanel.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), confirm: vi.fn() }))
vi.mock('@/api/admin/usageMonitor', () => ({
  listCodexResets: api.list, confirmCodexReset: api.confirm,
  usageMonitorErrorText: () => 'failed',
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

beforeEach(() => {
  api.list.mockReset().mockResolvedValue([])
  api.confirm.mockReset().mockResolvedValue({})
})

function render() {
  return mount(CodexResetPanel, { global: { stubs: {
    teleport: true,
  } } })
}

function dialogConfirm(w: ReturnType<typeof render>) {
  return w.getComponent(ConfirmDialog).findAll('button').find(b => b.text() === 'common.confirm')!
}

const candidate = {
  id: 'candidate:official-window-001', effective_at: '2026-10-28T06:35:59.123456Z',
  detected_at: '2026-10-28T06:36:10Z', status: 'candidate', source: 'official_observation',
  note: 'single-account evidence only', evidence: '{}',
}

describe('CodexResetPanel', () => {
  it('requires a reason and explicit dialog confirmation', async () => {
    const w = render()
    await flushPromises()
    const submit = w.findAll('button').find(b => b.text() === 'admin.usageMonitor.resetConfirm')!
    await submit.trigger('click')
    expect(w.get('[role="alert"]').text()).toBe('admin.usageMonitor.resetRequired')
    expect(api.confirm).not.toHaveBeenCalled()
    await w.get('input[maxlength]').setValue('confirmed platform-wide reset')
    await submit.trigger('click')
    expect(api.confirm).not.toHaveBeenCalled()
    await dialogConfirm(w).trigger('click')
    await flushPromises()
    expect(api.confirm).toHaveBeenCalledWith(expect.objectContaining({ confirm: true, note: 'confirmed platform-wide reset' }))
    expect(w.emitted('changed')).toHaveLength(1)
    w.unmount()
  })

  it('reuses the event identity when retrying an uncertain response', async () => {
    api.confirm.mockRejectedValueOnce(new Error('timeout'))
    const w = render()
    await flushPromises()
    await w.get('input[maxlength]').setValue('verified upstream reset')
    const submit = w.findAll('button').find(b => b.text() === 'admin.usageMonitor.resetConfirm')!
    for (let attempt = 0; attempt < 2; attempt++) {
      await submit.trigger('click')
      await dialogConfirm(w).trigger('click')
      await flushPromises()
    }
    expect(api.confirm).toHaveBeenCalledTimes(2)
    expect(api.confirm.mock.calls[0][0].id).toBe(api.confirm.mock.calls[1][0].id)
    w.unmount()
  })

  it('opens candidate confirmation independently of the manual form and requires explicit evidence', async () => {
    api.list.mockResolvedValue([candidate])
    const w = render()
    await flushPromises()
    await w.get('input[type="datetime-local"]').setValue('')
    await w.get('li button').trigger('click')
    expect(w.getComponent(ConfirmDialog).props('show')).toBe(true)
    expect(api.confirm).not.toHaveBeenCalled()
    await dialogConfirm(w).trigger('click')
    expect(api.confirm).not.toHaveBeenCalled()
    expect(w.getComponent(ConfirmDialog).get('[role="alert"]').text()).toBe('admin.usageMonitor.resetRequired')
    await w.getComponent(ConfirmDialog).get('textarea').setValue('  verified platform-wide reset  ')
    await dialogConfirm(w).trigger('click')
    await flushPromises()
    expect(api.confirm).toHaveBeenCalledTimes(1)
    expect(api.confirm).toHaveBeenCalledWith({
      id: candidate.id, effective_at: candidate.effective_at, note: 'verified platform-wide reset', confirm: true,
    })
    expect(w.emitted('changed')).toHaveLength(1)
    w.unmount()
  })

  it('keeps candidate confirmation isolated from a manual reset draft on cancellation', async () => {
    api.list.mockResolvedValue([candidate])
    const w = render()
    await flushPromises()
    await w.get('input[type="datetime-local"]').setValue('2026-10-28T14:29')
    await w.get('input[maxlength]').setValue('manual draft evidence')
    await w.get('li button').trigger('click')
    expect(w.getComponent(ConfirmDialog).get('textarea').element.value).toBe('')
    await w.getComponent(ConfirmDialog).findAll('button').find(b => b.text() === 'common.cancel')!.trigger('click')
    expect(w.get('input[type="datetime-local"]').element.value).toBe('2026-10-28T14:29')
    expect(w.get('input[maxlength]').element.value).toBe('manual draft evidence')
    expect(api.confirm).not.toHaveBeenCalled()
    await w.findAll('button').find(b => b.text() === 'admin.usageMonitor.resetConfirm')!.trigger('click')
    await dialogConfirm(w).trigger('click')
    await flushPromises()
    expect(api.confirm).toHaveBeenCalledWith(expect.objectContaining({ id: expect.stringMatching(/^manual:/), note: 'manual draft evidence' }))
    w.unmount()
  })

  it('retains candidate identity, exact time and evidence on a failed confirmation and prevents duplicate submits', async () => {
    api.list.mockResolvedValue([candidate])
    let reject!: (error: Error) => void
    api.confirm.mockImplementationOnce(() => new Promise((_, fail) => { reject = fail }))
    const w = render()
    await flushPromises()
    await w.get('li button').trigger('click')
    await w.getComponent(ConfirmDialog).get('textarea').setValue('verified upstream reset')
    const submit = dialogConfirm(w)
    await submit.trigger('click')
    await submit.trigger('click')
    expect(api.confirm).toHaveBeenCalledTimes(1)
    reject(new Error('timeout'))
    await flushPromises()
    expect(w.getComponent(ConfirmDialog).props('show')).toBe(true)
    expect(w.getComponent(ConfirmDialog).get('[role="alert"]').text()).toBe('failed')
    await dialogConfirm(w).trigger('click')
    await flushPromises()
    expect(api.confirm).toHaveBeenCalledTimes(2)
    expect(api.confirm.mock.calls[0][0]).toEqual(api.confirm.mock.calls[1][0])
    expect(api.confirm.mock.calls[1][0]).toEqual({ id: candidate.id, effective_at: candidate.effective_at, note: 'verified upstream reset', confirm: true })
    w.unmount()
  })
})
