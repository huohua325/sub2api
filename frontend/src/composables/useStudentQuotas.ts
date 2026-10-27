import { onMounted, onUnmounted, shallowRef } from 'vue'
import { getMyPlatformQuotas } from '@/api/user'
import type { PurposeAllowanceItem } from '@/api/admin/users'

const pollMs = 60_000

export function useStudentQuotas() {
  const allowances = shallowRef<PurposeAllowanceItem[] | null>(null)
  const loading = shallowRef(true)
  const failed = shallowRef(false)
  let timer = 0
  let disposed = false
  let inFlight: Promise<void> | null = null

  function load(quiet = false): Promise<void> {
    if (disposed) return Promise.resolve()
    if (inFlight) return inFlight
    inFlight = read(quiet).finally(() => { inFlight = null })
    return inFlight
  }

  async function read(quiet: boolean) {
    const startedAt = Date.now()
    if (!quiet && allowances.value == null) loading.value = true
    try {
      const data = await getMyPlatformQuotas()
      if (disposed) return
      const receivedAt = Date.now()
      const serverNow = Date.parse(data.server_now ?? '')
      const offset = Number.isFinite(serverNow) ? serverNow - (startedAt + receivedAt) / 2 : 0
      allowances.value = (data.purpose_allowances ?? []).map((row) => ({ ...row, clock_offset_ms: offset, read_at: new Date(receivedAt + offset).toISOString() }))
      failed.value = false
    } catch {
      if (disposed) return
      failed.value = true
    } finally {
      if (!disposed) loading.value = false
    }
  }

  function stopPoll() {
    window.clearInterval(timer)
    timer = 0
  }

  function startPoll() {
    stopPoll()
    if (document.hidden) return
    timer = window.setInterval(() => {
      void load(true)
    }, pollMs)
  }

  function onVisibility() {
    if (document.hidden) {
      stopPoll()
      return
    }
    void load(true)
    startPoll()
  }

  onMounted(() => {
    void load(false)
    startPoll()
    document.addEventListener('visibilitychange', onVisibility)
  })

  onUnmounted(() => {
    disposed = true
    stopPoll()
    document.removeEventListener('visibilitychange', onVisibility)
  })

  return {
    allowances,
    loading,
    failed,
    reload: () => load(false),
  }
}
