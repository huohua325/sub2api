import { onMounted, onUnmounted, ref, shallowRef, watch, type Ref } from 'vue'
import {
  developmentWorkspaceAPI,
  type WorkspaceDetail,
  type WorkspaceStatus,
  type WorkspaceTask,
} from '@/api/admin/developmentWorkspace'

const pollInterval = 4000

export function useWorkspaceTasks(selected: Readonly<Ref<string>>) {
  const status = shallowRef<WorkspaceStatus>()
  const tasks = shallowRef<WorkspaceTask[]>([])
  const detail = shallowRef<WorkspaceDetail>()
  const loading = ref(true)
  const statusError = ref(false)
  const listError = ref(false)
  const detailError = ref(false)
  let request: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let mounted = false

  async function refresh() {
    if (!mounted) return
    clearTimeout(timer)
    request?.abort()
    const controller = new AbortController()
    request = controller
    const { signal } = controller
    const id = selected.value

    try {
      if (!status.value) {
        const result = await developmentWorkspaceAPI.status(signal)
        if (signal.aborted) return
        status.value = result
        statusError.value = false
      }
      if (!status.value.enabled) return

      const listRequest = developmentWorkspaceAPI.list(signal)
        .then((rows) => {
          if (signal.aborted) return
          tasks.value = rows ?? []
          listError.value = false
        })
        .catch(() => {
          if (!signal.aborted) listError.value = true
        })
      const detailRequest = id
        ? developmentWorkspaceAPI.get(id, signal)
          .then((task) => {
            if (signal.aborted) return
            detail.value = task
            detailError.value = false
          })
          .catch(() => {
            if (!signal.aborted) detailError.value = true
          })
        : undefined
      await Promise.all([listRequest, detailRequest])
    } catch {
      if (!signal.aborted) statusError.value = true
    } finally {
      if (!signal.aborted) {
        loading.value = false
        if (status.value?.enabled && !document.hidden) {
          timer = setTimeout(refresh, pollInterval)
        }
      }
    }
  }

  function handleVisibility() {
    if (document.hidden) {
      clearTimeout(timer)
    } else {
      void refresh()
    }
  }

  watch(selected, () => {
    detail.value = undefined
    detailError.value = false
    void refresh()
  }, { flush: 'sync' })

  onMounted(() => {
    mounted = true
    document.addEventListener('visibilitychange', handleVisibility)
    window.addEventListener('online', refresh)
    void refresh()
  })

  onUnmounted(() => {
    mounted = false
    document.removeEventListener('visibilitychange', handleVisibility)
    window.removeEventListener('online', refresh)
    clearTimeout(timer)
    request?.abort()
  })

  return { status, tasks, detail, loading, statusError, listError, detailError, refresh }
}
