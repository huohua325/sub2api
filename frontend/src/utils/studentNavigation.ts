type NavigationItem = { path: string }
export type StudentNavigationGroup = 'workspace' | 'activity' | 'account'

const groups: { id: StudentNavigationGroup; paths: string[] }[] = [
  { id: 'workspace', paths: ['/dashboard', '/models', '/keys', '/guide', '/batch-image'] },
  { id: 'activity', paths: ['/usage', '/monitor', '/available-channels'] },
  { id: 'account', paths: ['/profile', '/subscriptions', '/purchase', '/orders', '/redeem', '/affiliate'] },
]

/** Group an already permission-filtered list, retaining custom entries and hiding empty groups. */
export function groupStudentNavigation<T extends NavigationItem>(items: readonly T[]) {
  const known = new Set(groups.flatMap((group) => group.paths))
  return groups.map((group) => ({
    id: group.id,
    items: [
      ...group.paths.flatMap((path) => items.filter((item) => item.path === path)),
      ...(group.id === 'account' ? items.filter((item) => !known.has(item.path)) : []),
    ],
  })).filter((group) => group.items.length > 0)
}
