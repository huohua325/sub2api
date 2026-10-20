export type ModelFamily = 'gpt' | 'grok' | 'qwen' | 'kimi' | 'deepseek' | 'glm'
export type ModelUse = 'chat' | 'coding' | 'image' | 'video'
export type ModelSource = 'subscription' | 'experiment'

export interface ExperimentModel {
  id: string
  family: ModelFamily
  use: ModelUse
  source: ModelSource
}

export const modelFamilies: ModelFamily[] = ['gpt', 'grok', 'qwen', 'kimi', 'deepseek', 'glm']
export const modelUses: ModelUse[] = ['chat', 'coding', 'image', 'video']

function row(
  id: string,
  family: ModelFamily,
  use: ModelUse,
  source: ModelSource
): ExperimentModel {
  return { id, family, use, source }
}

const gpt = (id: string, use: ModelUse = 'chat') => row(id, 'gpt', use, 'subscription')
const grok = (id: string, use: ModelUse = 'chat') => row(id, 'grok', use, 'subscription')
const lab = (id: string, family: ModelFamily) => row(id, family, 'chat', 'experiment')

export const experimentModels: ExperimentModel[] = [
  gpt('gpt-6.1-sol'),
  gpt('gpt-6-sol'),
  gpt('gpt-6-luna'),
  gpt('gpt-6-astra'),
  gpt('gpt-5.6-sol'),
  gpt('gpt-5.6-terra'),
  gpt('gpt-5.6-luna'),
  gpt('gpt-5.4'),
  gpt('gpt-5.4-mini'),
  gpt('gpt-5.3-codex-spark', 'coding'),
  gpt('gpt-5.2'),
  gpt('gpt-image-2.5-flare', 'image'),
  gpt('gpt-image-2.5-sunburst', 'image'),
  gpt('gpt-image-2', 'image'),
  gpt('gpt-image-1.5', 'image'),
  gpt('gpt-image-1', 'image'),

  grok('grok-4.7'),
  grok('grok-4.6'),
  grok('grok-4.5'),
  grok('grok-4.3'),
  grok('grok-build-0.1', 'coding'),
  grok('grok-composer-2.5-fast', 'coding'),
  grok('grok-4.20-0309-reasoning'),
  grok('grok-4.20-0309-non-reasoning'),
  grok('grok-4.20-multi-agent-0309'),
  grok('grok-imagine-image-quality', 'image'),
  grok('grok-imagine-image', 'image'),
  grok('grok-imagine-image-2.0', 'image'),
  grok('grok-imagine-video', 'video'),
  grok('grok-imagine-video-1.5', 'video'),

  lab('qwen3.8-max', 'qwen'),
  lab('qwen3.7-max', 'qwen'),
  lab('qwen3.7-plus', 'qwen'),
  lab('qwen3.6-plus', 'qwen'),
  lab('qwen3.7-flash', 'qwen'),
  lab('qwen3.6-flash', 'qwen'),

  lab('kimi-k3', 'kimi'),
  lab('kimi-k2.6', 'kimi'),
  lab('kimi-k2.5', 'kimi'),

  lab('deepseek-v4-pro', 'deepseek'),
  lab('deepseek-v4-pro-0813', 'deepseek'),
  lab('deepseek-v4.1-flash', 'deepseek'),
  lab('deepseek-v4-flash', 'deepseek'),
  lab('deepseek-v4-flash-0731', 'deepseek'),

  lab('glm-5.3', 'glm'),
  lab('glm-5.3-flash', 'glm'),
  lab('glm-5.2', 'glm'),
]
