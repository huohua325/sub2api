import { describe, expect, it } from 'vitest'
import { groupStudentNavigation } from '../studentNavigation'

describe('student navigation grouping', () => {
  it('orders the available workspace entries without restoring filtered routes', () => {
    const groups = groupStudentNavigation([{ path: '/keys' }, { path: '/guide' }, { path: '/dashboard' }])
    expect(groups).toEqual([{ id: 'workspace', items: [{ path: '/dashboard' }, { path: '/keys' }, { path: '/guide' }] }])
  })
  it('retains custom entries and their metadata and removes empty groups', () => {
    const custom = { path: '/custom/course', label: 'Course', icon: 'book' }
    expect(groupStudentNavigation([custom])).toEqual([{ id: 'account', items: [custom] }])
    expect(groupStudentNavigation([])).toEqual([])
  })
})
