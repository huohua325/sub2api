import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const copyToClipboard = vi.hoisted(() => vi.fn(async () => true))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: { template: '<span />' },
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({ global: { t: (key: string) => key } }),
  useI18n: () => ({
    t: (key: string, params?: { count?: number }) => {
      const text: Record<string, string> = {
        'models.title': '模型',
        'models.lead': '把名字填进 model。',
        'models.search': '搜索模型名',
        'models.empty': '没有这个模型名',
        'models.copy': '复制',
        'models.copied': '已复制',
        'models.familyLabel': '家族',
        'models.useLabel': '用途',
        'models.footnote': '密钥要建在对应分组',
        'models.input': '输入',
        'models.output': '输出',
        'models.cache': '缓存',
        'models.unit': '元/百万 token',
        'models.families.all': '全部',
        'models.families.gpt': 'GPT',
        'models.families.grok': 'Grok',
        'models.families.claude': 'Claude',
        'models.families.gemini': 'Gemini',
        'models.families.qwen': 'Qwen',
        'models.families.kimi': 'Kimi',
        'models.families.deepseek': 'DeepSeek',
        'models.families.glm': 'GLM',
        'models.uses.all': '全部用途',
        'models.uses.chat': '对话',
        'models.uses.coding': '编程',
        'models.notes.peak': '高峰两倍',
        'models.notes.long2': '20万两倍',
        'models.notes.long3': '25.6万三倍',
        'models.notes.long5': '10万五倍',
        'models.notes.long55': '27.2万另计',
      }
      if (key === 'models.count') return `${params?.count ?? 0} 个模型`
      return text[key] || key
    },
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

import ModelsView from '../ModelsView.vue'
import { experimentModels } from '@/data/experimentModels'

describe('ModelsView', () => {
  it('lists GlobalAI names with prices and copies a name', async () => {
    const ids = experimentModels.map((model) => model.id)
    expect(ids).toContain('gpt-4o')
    expect(ids).toContain('gpt-5-nano')
    expect(ids).toContain('claude-haiku-4-5-20251001')
    expect(ids).not.toContain('gpt-6.1-sol')
    expect(ids).not.toContain('grok-4.20-0309-reasoning')

    const nano = experimentModels.find((model) => model.id === 'gpt-5-nano')
    expect(nano).toMatchObject({ family: 'gpt', input: 0.03, output: 0.24 })

    const wrapper = mount(ModelsView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })

    expect(wrapper.text()).toContain('gpt-5-nano')
    expect(wrapper.text()).toContain('0.03')
    expect(wrapper.text()).toContain('元/百万 token')
    expect(wrapper.text()).not.toContain('订阅账号')
    expect(wrapper.text()).not.toContain('gpt-6.1-sol')

    await wrapper.get('[data-testid="copy-qwen3.8-max"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('qwen3.8-max', '已复制')

    await wrapper.get('[data-testid="model-search"]').setValue('没有的名字')
    expect(wrapper.get('[data-testid="model-empty"]').text()).toContain('没有这个模型名')
  })
})
