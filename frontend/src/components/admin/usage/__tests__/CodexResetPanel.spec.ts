import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexResetPanel from '../CodexResetPanel.vue'

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
    ConfirmDialog: { props: ['show'], template: '<button v-if="show" data-testid="confirm" @click="$emit(\'confirm\')">confirm</button>' },
  } } })
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
    await w.get('[data-testid="confirm"]').trigger('click')
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
      await w.get('[data-testid="confirm"]').trigger('click')
      await flushPromises()
    }
    expect(api.confirm).toHaveBeenCalledTimes(2)
    expect(api.confirm.mock.calls[0][0].id).toBe(api.confirm.mock.calls[1][0].id)
    w.unmount()
  })
})
