import { describe, expect, it } from 'vitest'
import { guideAnchorPosition, guidePurpose } from '../guideNavigation'

describe('guide route destinations', () => {
  it.each(['guide-prep', 'guide-codex', 'guide-grok', 'guide-experiment', 'guide-check'])(
    'places %s below the fixed header', id => {
      expect(guideAnchorPosition('/guide', `#${id}`)).toEqual({ el: `#${id}`, top: 112 })
    },
  )
  it('does not change other routes or scroll to arbitrary selectors', () => {
    expect(guideAnchorPosition('/keys', '#guide-experiment')).toBeUndefined()
    expect(guideAnchorPosition('/guide', '#unknown')).toBeUndefined()
    expect(guideAnchorPosition('/guide', '')).toBeUndefined()
    expect(guideAnchorPosition('/guide/', '#guide-grok')?.el).toBe('#guide-grok')
  })
  it('uses the actual chapter purpose before a conflicting legacy query', () => {
    expect(guidePurpose('#guide-experiment', 'codex')).toBe('experiment')
    expect(guidePurpose('#guide-grok', 'experiment')).toBe('grok_build')
    expect(guidePurpose('#guide-codex', 'grok_build')).toBe('codex')
    expect(guidePurpose('#guide-prep', 'experiment')).toBe('experiment')
    expect(guidePurpose('', ['experiment'])).toBe('codex')
  })
})
