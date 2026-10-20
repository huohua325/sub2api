import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

const showRate = vi.hoisted(() => ({ value: true }))

vi.mock('@/utils/showConsoleMoney', () => ({
  showConsoleMoney: () => showRate.value,
}))

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })

  it('hides the rate from ordinary users and keeps it for admins', () => {
    const mountItem = () => mount(GroupOptionItem, {
      props: {
        name: 'openai pro 100',
        platform: 'openai',
        rateMultiplier: 0.5,
      },
      global: { stubs: { GroupBadge: true } },
    })

    showRate.value = false
    expect(mountItem().text()).not.toContain('0.5x')

    showRate.value = true
    expect(mountItem().text()).toContain('0.5x')
  })
})
