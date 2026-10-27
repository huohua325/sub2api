import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import BaseButton from '../BaseButton.vue'

describe('BaseButton', () => {
  it('blocks repeated actions while loading and keeps its accessible label', async () => {
    const onClick = vi.fn()
    const wrapper = mount(BaseButton, { props: { loading: true }, attrs: { onClick, 'aria-label': 'Create key' }, slots: { default: 'Create' } })
    await wrapper.trigger('click')
    expect(onClick).not.toHaveBeenCalled()
    expect(wrapper.attributes('disabled')).toBeDefined()
    expect(wrapper.attributes('aria-busy')).toBe('true')
    expect(wrapper.attributes('aria-label')).toBe('Create key')
    await wrapper.setProps({ loading: false })
    await wrapper.trigger('click')
    expect(onClick).toHaveBeenCalledOnce()
  })
  it('supports native form submission when explicitly requested', () => {
    const wrapper = mount(BaseButton, { props: { type: 'submit' }, attrs: { form: 'create-key' } })
    expect(wrapper.attributes('type')).toBe('submit')
    expect(wrapper.attributes('form')).toBe('create-key')
  })
})
