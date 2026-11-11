import { KEY_GROUP_PROVIDERS, type KeyGroupProvider } from '@/utils/keyGroupProviders'

export interface GuideSection {
  id: string
  titleKey: string
  purpose?: KeyGroupProvider
}

// Quick entries, desktop/mobile contents and route anchors share these chapters.
export const GUIDE_SECTIONS: readonly GuideSection[] = [
  { id: 'guide-prep', titleKey: 'guide.manual.prep.title' },
  { id: 'guide-codex', titleKey: 'guide.manual.codex.title', purpose: 'codex' },
  { id: 'guide-grok', titleKey: 'guide.manual.grok.title', purpose: 'grok_build' },
  { id: 'guide-experiment', titleKey: 'keys.providers.experiment', purpose: 'experiment' },
  { id: 'guide-check', titleKey: 'guide.manual.check.title' },
]
export const GUIDE_HEADER_OFFSET = 112

export function guidePurpose(hash: string, queryPurpose: unknown): KeyGroupProvider {
  const chapterPurpose = GUIDE_SECTIONS.find(section => `#${section.id}` === hash)?.purpose
  if (chapterPurpose) return chapterPurpose
  return KEY_GROUP_PROVIDERS.includes(queryPurpose as KeyGroupProvider) ? queryPurpose as KeyGroupProvider : 'codex'
}

export function guideAnchorPosition(path: string, hash: string) {
  if (path.replace(/\/+$/, '') !== '/guide' || !GUIDE_SECTIONS.some(section => `#${section.id}` === hash)) return undefined
  return { el: hash, top: GUIDE_HEADER_OFFSET }
}
