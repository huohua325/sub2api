import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const copyToClipboard = vi.hoisted(() => vi.fn(async () => true))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({ global: { t: (key: string) => key } }),
  useI18n: () => ({
    t: (key: string, params?: { count?: number }) => {
      const text: Record<string, string> = {
        'models.title': '模型',
        'models.lead': '把名字填进 model。额度看仪表盘。',
        'models.search': '搜索模型名',
        'models.empty': '没有这个模型名',
        'models.copy': '复制',
        'models.copied': '已复制',
        'models.familyLabel': '家族',
        'models.useLabel': '用途',
        'models.footnote': '密钥要建在对应分组',
        'models.families.all': '全部',
        'models.families.gpt': 'GPT',
        'models.families.grok': 'Grok',
        'models.families.qwen': 'Qwen',
        'models.families.kimi': 'Kimi',
        'models.families.deepseek': 'DeepSeek',
        'models.families.glm': 'GLM',
        'models.uses.all': '全部用途',
        'models.uses.chat': '对话',
        'models.uses.coding': '编程',
        'models.uses.image': '图像',
        'models.uses.video': '视频',
        'models.sources.subscription': '订阅账号',
        'models.sources.experiment': '实验接口',
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
  it('lists account models and Global Chinese models, and copies a name', async () => {
    const ids = experimentModels.map((model) => model.id)
    expect(ids.some((id) => id.includes('claude'))).toBe(false)
    expect(ids).not.toContain('gpt-4o')
    expect(ids).not.toContain('gpt-5.5')
    expect(ids).toContain('grok-4.20-0309-reasoning')
    expect(ids).toContain('qwen3.8-max')

    const wrapper = mount(ModelsView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })

    expect(wrapper.text()).toContain('grok-4.20-0309-reasoning')
    expect(wrapper.text()).toContain('qwen3.8-max')
    expect(wrapper.text()).not.toContain('claude')
    expect(wrapper.text()).not.toContain('gpt-4o')
    expect(wrapper.text()).not.toContain('gpt-5.5')

    await wrapper.get('[data-testid="copy-qwen3.8-max"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('qwen3.8-max', '已复制')

    await wrapper.get('[data-testid="model-search"]').setValue('没有的名字')
    expect(wrapper.get('[data-testid="model-empty"]').text()).toContain('没有这个模型名')
  })
})
