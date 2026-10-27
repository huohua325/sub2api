import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  it('returns to a pointer trigger even on browsers that do not focus clicked buttons', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: false, title: 'Create key' } })
    trigger.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
    await wrapper.setProps({ show: true })
    await nextTick()
    await wrapper.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })
  it('wraps Tab and Shift+Tab inside the topmost dialog', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: true, title: 'Create key' },
      slots: { default: '<input aria-label="Name" />', footer: '<button id="last-dialog-action">Create</button>' },
    })
    await nextTick()
    const first = document.querySelector('.modal-header button') as HTMLButtonElement
    const last = document.querySelector('#last-dialog-action') as HTMLButtonElement
    first.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(last)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(first)
    wrapper.unmount()
  })
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})
