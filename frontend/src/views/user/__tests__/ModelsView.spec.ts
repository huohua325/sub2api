import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const copyToClipboard = vi.hoisted(() => vi.fn(async () => true))
const getModelCatalog = vi.hoisted(() => vi.fn())
vi.mock('@/api/modelCatalog', () => ({ getModelCatalog }))

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

describe('ModelsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getModelCatalog.mockResolvedValue({ partial: false, models: [
      { id: 'gpt-6.1-terra', display_name: 'GPT-6.1 Terra', platform: 'openai', groups: [{ id: 1, name: 'Codex', source: 'upstream', observed_at: '2026-11-02T03:00:00Z' }] },
      { id: 'unreleased-2027', display_name: 'Future model', platform: 'openai', groups: [{ id: 2, name: 'API 实验', source: 'configured', observed_at: null }] },
      { id: 'grok-new', display_name: '', platform: 'grok', groups: [{ id: 3, name: 'Grok', source: 'stale', observed_at: '2026-11-02T02:00:00Z' }] },
    ] })
  })
  it('loads new account models without a frontend list and copies their public IDs', async () => {
    const wrapper = mount(ModelsView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('gpt-6.1-terra')
    expect(wrapper.text()).toContain('unreleased-2027')
    expect(wrapper.text()).toContain('API 实验')
    expect(wrapper.text()).toContain('models.sources.configured')
    expect(wrapper.text()).toContain('models.sources.stale')
    expect(wrapper.text()).not.toContain('gpt-5-nano')
    expect(wrapper.text()).not.toContain('元/百万 token')
    await wrapper.get('[data-testid="copy-unreleased-2027"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('unreleased-2027', '已复制')
    await wrapper.get('[data-testid="model-search"]').setValue('Terra')
    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(1)
    await wrapper.get('[data-testid="model-search"]').setValue('没有的名字')
    expect(wrapper.get('[data-testid="model-empty"]').text()).toContain('没有这个模型名')
    wrapper.unmount()
  })
  it('distinguishes loading and request failures from an empty catalog and retries', async () => {
    let reject!: (reason: Error) => void
    getModelCatalog.mockReturnValueOnce(new Promise((_resolve, rejectPromise) => { reject = rejectPromise }))
    const wrapper = mount(ModelsView)
    expect(wrapper.find('[data-testid="model-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-empty"]').exists()).toBe(false)
    reject(new Error('unavailable'))
    await flushPromises()
    expect(wrapper.find('[data-testid="model-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-empty"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(3)
    expect(wrapper.find('[data-testid="model-error"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps previous models visible on a failed refresh without claiming fresh data', async () => {
    const wrapper = mount(ModelsView)
    await flushPromises()
    getModelCatalog.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[data-testid="model-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(3)
    expect(wrapper.find('[data-testid="model-error"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('shows partial discovery and filters by the dynamically present families', async () => {
    getModelCatalog.mockResolvedValueOnce({partial:true, models:[
      {id:'qwen-99',display_name:'New Qwen',platform:'openai',groups:[{id:1,name:'API',source:'upstream',observed_at:null}]},
      {id:'gpt-6.1-terra',display_name:'Terra',platform:'openai',groups:[{id:1,name:'API',source:'upstream',observed_at:null}]},
    ]})
    const wrapper=mount(ModelsView)
    await flushPromises()
    expect(wrapper.find('[data-testid="model-partial"]').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Qwen')!.trigger('click')
    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('qwen-99')
    expect(wrapper.text()).not.toContain('gpt-6.1-terra')
    wrapper.unmount()
  })
})
