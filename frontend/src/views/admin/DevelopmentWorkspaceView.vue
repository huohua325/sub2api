<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { developmentWorkspaceAPI } from '@/api/admin/developmentWorkspace'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAuthStore } from '@/stores/auth'
import WorkspaceTranscript from './developmentWorkspace/WorkspaceTranscript.vue'
import { useWorkspaceTasks } from './developmentWorkspace/useWorkspaceTasks'
import { createWorkspaceAttempts } from './developmentWorkspace/workspaceAttempts'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { copyToClipboard } = useClipboard()
const attempts = createWorkspaceAttempts(useAuthStore().user?.id ?? 0)
const selection = computed(() => typeof route.query.task === 'string' ? route.query.task : '')
const isNew = computed(() => selection.value === 'new')
const selectedTask = computed(() => isNew.value ? '' : selection.value)
const { status, tasks, detail, loading, statusError, listError, detailError, refresh } = useWorkspaceTasks(selectedTask)

const createMessage = shallowRef('')
const createBranch = shallowRef('')
const sendMessage = shallowRef('')
const busy = shallowRef<'create' | 'send' | 'stop' | ''>('')
const actionError = shallowRef<'createError' | 'sendError' | 'stopError' | ''>('')
const confirmStop = shallowRef(false)

const activeStatuses = new Set(['running', 'working', 'active', 'queued', 'pending', 'waiting', 'waiting_for_children', 'in_progress', 'inProgress', 'starting', 'preparing'])
const finalStatuses = new Set(['complete', 'completed', 'idle', 'success', 'succeeded', 'ready', 'result_available'])
const failedStatuses = new Set(['error', 'failed'])
const stoppedStatuses = new Set(['canceled', 'cancelled', 'interrupted', 'stopped', 'rolled_back'])
const canStop = computed(() => Boolean(detail.value && activeStatuses.has(detail.value.status)))

function statusKey(value: string) {
  if (value === 'queued' || value === 'pending') return 'queued'
  if (value === 'waiting' || value === 'waiting_for_children') return 'waiting'
  if (activeStatuses.has(value)) return 'running'
  if (finalStatuses.has(value)) return 'ready'
  if (failedStatuses.has(value)) return 'failed'
  if (stoppedStatuses.has(value)) return 'stopped'
  return 'unknown'
}

function taskLocation(id?: string) {
  return { query: { ...route.query, task: id || undefined } }
}

function copyTaskLink() {
  void copyToClipboard(window.location.href)
}

watch(selection, () => {
  actionError.value = ''
  sendMessage.value = ''
  confirmStop.value = false
})

async function createTask() {
  const message = createMessage.value.trim()
  const branch = createBranch.value.trim()
  if (!message || busy.value) return
  const initialSelection = selection.value
  busy.value = 'create'
  actionError.value = ''
  try {
    const payload = { message, branch: branch || undefined }
    const request_id = await attempts.requestId('create', payload)
    const task = await developmentWorkspaceAPI.create({ ...payload, request_id })
    attempts.finish('create')
    createMessage.value = ''
    if (selection.value === initialSelection && route.name === 'AdminDevelopmentWorkspace') {
      await router.replace(taskLocation(task.id))
    }
    void refresh()
  } catch {
    if (selection.value === initialSelection) actionError.value = 'createError'
  } finally {
    busy.value = ''
  }
}

async function appendMessage() {
  const id = selectedTask.value
  const message = sendMessage.value.trim()
  if (!message || id !== detail.value?.id || busy.value) return
  busy.value = 'send'
  actionError.value = ''
  try {
    const request_id = await attempts.requestId('send', { id, message })
    await developmentWorkspaceAPI.send(id, { message, request_id })
    attempts.finish('send')
    if (id === selectedTask.value) sendMessage.value = ''
    void refresh()
  } catch {
    if (id === selectedTask.value) actionError.value = 'sendError'
  } finally {
    busy.value = ''
  }
}

async function stopTask() {
  const id = selectedTask.value
  const task = detail.value
  confirmStop.value = false
  if (!task || id !== task.id || !canStop.value || busy.value) return
  busy.value = 'stop'
  actionError.value = ''
  try {
    const latestPrompt = task.messages?.filter(message => message.role === 'user').at(-1)
    const run = task.run_id || latestPrompt?.id || task.updated_at
    const requestId = await attempts.requestId('stop', { id, run })
    await developmentWorkspaceAPI.stop(id, requestId)
    attempts.finish('stop')
    void refresh()
  } catch {
    if (id === selectedTask.value) actionError.value = 'stopError'
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <AppLayout :page-header="false" compact-header>
    <div class="workspace-shell">
      <h1 class="workspace-title">{{ t('admin.developmentWorkspace.title') }}</h1>
      <div v-if="loading" class="workspace-empty"><LoadingSpinner /></div>
      <div v-else-if="statusError || !status?.enabled" class="workspace-empty">
        <Icon name="terminal" size="lg" aria-hidden="true" />
        <p :role="statusError ? 'alert' : 'status'">
          {{ t(statusError ? 'admin.developmentWorkspace.statusError' : 'admin.developmentWorkspace.disabled') }}
        </p>
        <button v-if="statusError" class="btn btn-secondary btn-sm" type="button" @click="refresh">
          <Icon name="refresh" size="sm" aria-hidden="true" />
          {{ t('admin.developmentWorkspace.retry') }}
        </button>
      </div>

      <div v-else class="workspace" :data-detail="Boolean(selection)">
        <aside class="task-sidebar">
          <div class="task-sidebar-header">
            <h2>{{ t('admin.developmentWorkspace.tasks') }}</h2>
            <RouterLink class="btn btn-primary btn-sm new-task" :to="taskLocation('new')" data-testid="workspace-new-task">
              <Icon name="plus" size="sm" aria-hidden="true" />
              {{ t('admin.developmentWorkspace.newTask') }}
            </RouterLink>
          </div>
          <nav class="task-list" :aria-label="t('admin.developmentWorkspace.tasks')" data-testid="workspace-task-list">
            <p v-if="listError" class="inline-error m-2" role="status">{{ t('admin.developmentWorkspace.listError') }}</p>
            <p v-else-if="!tasks.length" class="list-empty">{{ t('admin.developmentWorkspace.noTasks') }}</p>
            <RouterLink
              v-for="task in tasks"
              :key="task.id"
              :to="taskLocation(task.id)"
              custom
              v-slot="{ href, navigate }"
            >
              <a
                :href="href"
                class="task-link"
                :data-current="task.id === selectedTask"
                :aria-current="task.id === selectedTask ? 'page' : undefined"
                :title="task.title"
                @click="navigate"
              >
                <span class="status-dot" :data-state="statusKey(task.status)" aria-hidden="true"></span>
                <span class="task-link-content">
                  <span class="task-link-title">{{ task.title || t('admin.developmentWorkspace.untitled') }}</span>
                  <span class="task-link-meta">
                    {{ t(`admin.developmentWorkspace.status.${statusKey(task.status)}`) }}
                    <span v-if="task.branch"> / {{ task.branch }}</span>
                  </span>
                </span>
              </a>
            </RouterLink>
          </nav>
        </aside>

        <section class="workspace-thread">
          <RouterLink v-if="selection" :to="taskLocation()" class="back-link">
            <Icon name="arrowLeft" size="sm" aria-hidden="true" />
            {{ t('admin.developmentWorkspace.tasks') }}
          </RouterLink>

          <template v-if="!selectedTask">
            <div class="workspace-toolbar">
              <div class="min-w-0">
                <h2>{{ t('admin.developmentWorkspace.newTask') }}</h2>
                <p class="workspace-meta"><Icon name="terminal" size="sm" aria-hidden="true" /> {{ status.project || 'T3 Code' }} <span v-if="status.model">/ {{ status.model }}</span></p>
              </div>
            </div>
            <form class="new-task-form" @submit.prevent="createTask">
              <label for="workspace-create-message" class="input-label">{{ t('admin.developmentWorkspace.request') }}</label>
              <textarea
                id="workspace-create-message"
                v-model="createMessage"
                class="input workspace-input create-input"
                rows="5"
                maxlength="16000"
                :placeholder="t('admin.developmentWorkspace.requestPlaceholder')"
                :disabled="busy === 'create'"
                required
                data-testid="workspace-create-message"
              ></textarea>
              <div class="create-actions">
                <label class="input-label branch-label" for="workspace-branch">
                  {{ t('admin.developmentWorkspace.baseBranch') }}
                  <input
                    id="workspace-branch"
                    v-model.trim="createBranch"
                    type="text"
                    class="input workspace-input font-mono"
                    :placeholder="status.default_branch"
                    :disabled="busy === 'create'"
                    autocapitalize="off"
                    autocomplete="off"
                    spellcheck="false"
                    maxlength="255"
                    data-testid="workspace-branch"
                  />
                </label>
                <button class="btn btn-primary" type="submit" :disabled="!createMessage.trim() || Boolean(busy)" data-testid="workspace-create-submit">
                  <Icon name="play" size="sm" aria-hidden="true" />
                  {{ t(busy === 'create' ? 'admin.developmentWorkspace.starting' : 'admin.developmentWorkspace.start') }}
                </button>
              </div>
              <p v-if="actionError" class="inline-error" role="alert">{{ t(`admin.developmentWorkspace.${actionError}`) }}</p>
            </form>
          </template>

          <template v-else-if="detail">
            <div class="active-thread">
              <div class="workspace-toolbar active-toolbar">
                <div class="min-w-0">
                  <h2 data-testid="workspace-task-title">{{ detail.title || t('admin.developmentWorkspace.untitled') }}</h2>
                  <p class="workspace-meta" role="status">
                    <span class="status-dot" :data-state="statusKey(detail.status)" aria-hidden="true"></span>
                    {{ t(`admin.developmentWorkspace.status.${statusKey(detail.status)}`) }}
                    <span v-if="detail.branch" class="font-mono">/ {{ detail.branch }}</span>
                  </p>
                </div>
                <div class="toolbar-actions">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm icon-button"
                    :aria-label="t('admin.developmentWorkspace.copyLink')"
                    :title="t('admin.developmentWorkspace.copyLink')"
                    @click="copyTaskLink"
                  ><Icon name="link" size="sm" aria-hidden="true" /></button>
                  <button
                    v-if="canStop"
                    type="button"
                    class="btn btn-secondary btn-sm stop-button"
                    :aria-label="t('admin.developmentWorkspace.stop')"
                    :title="t('admin.developmentWorkspace.stop')"
                    :disabled="Boolean(busy)"
                    data-testid="workspace-stop"
                    @click="confirmStop = true"
                  >
                    <Icon name="x" size="sm" aria-hidden="true" />
                    <span>{{ t('admin.developmentWorkspace.stop') }}</span>
                  </button>
                </div>
              </div>
              <p v-if="detailError" class="inline-error mx-3 mt-2" role="status">{{ t('admin.developmentWorkspace.detailError') }}</p>
              <WorkspaceTranscript :key="detail.id" :task="detail" />
              <form class="composer" @submit.prevent="appendMessage">
                <label for="workspace-send-message" class="sr-only">{{ t('admin.developmentWorkspace.followUp') }}</label>
                <textarea
                  id="workspace-send-message"
                  v-model="sendMessage"
                  class="input workspace-input"
                  rows="2"
                  maxlength="16000"
                  :placeholder="t('admin.developmentWorkspace.followUp')"
                  :disabled="busy === 'send'"
                  required
                  data-testid="workspace-send-message"
                ></textarea>
                <button
                  class="btn btn-primary icon-button"
                  type="submit"
                  :title="t('admin.developmentWorkspace.send')"
                  :aria-label="t('admin.developmentWorkspace.send')"
                  :disabled="!sendMessage.trim() || Boolean(busy)"
                  data-testid="workspace-send-submit"
                ><Icon name="arrowUp" size="md" aria-hidden="true" /></button>
                <p v-if="actionError" class="inline-error composer-error" role="alert">{{ t(`admin.developmentWorkspace.${actionError}`) }}</p>
              </form>
            </div>
          </template>

          <div v-else class="workspace-empty">
            <template v-if="detailError">
              <p role="alert">{{ t('admin.developmentWorkspace.detailError') }}</p>
              <button class="btn btn-secondary btn-sm" type="button" @click="refresh">
                <Icon name="refresh" size="sm" aria-hidden="true" />
                {{ t('admin.developmentWorkspace.retry') }}
              </button>
            </template>
            <LoadingSpinner v-else />
          </div>
        </section>
      </div>
    </div>
    <ConfirmDialog
      :show="confirmStop"
      :title="t('admin.developmentWorkspace.stop')"
      :message="t('admin.developmentWorkspace.stopConfirm')"
      :confirm-text="t('admin.developmentWorkspace.stop')"
      danger
      @confirm="stopTask"
      @cancel="confirmStop = false"
    />
  </AppLayout>
</template>

<style scoped>
.workspace-shell { container: workspace / inline-size; }
.workspace-title { margin-bottom: 14px; color: rgb(var(--color-ink)); font-size: 20px; font-weight: 700; line-height: 1.3; }
.workspace { display: grid; min-height: min(550px, 72dvh); border: 1px solid rgb(var(--color-line)); border-radius: 8px; background: rgb(var(--color-surface)); }
.task-sidebar, .workspace-thread { min-width: 0; }
.task-sidebar { display: flex; flex-direction: column; min-height: 0; background: rgb(var(--color-page) / .7); }
.task-sidebar-header, .workspace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 12px; border-bottom: 1px solid rgb(var(--color-line)); }
.task-sidebar-header h2, .workspace-toolbar h2 { color: rgb(var(--color-ink)); font-size: 15px; font-weight: 650; overflow-wrap: anywhere; }
.task-list { min-height: 0; max-height: 65dvh; overflow: auto; padding: 5px; }
.task-link { display: flex; align-items: flex-start; gap: 8px; min-height: 48px; padding: 8px; border-radius: 6px; }
.task-link:hover { background: rgb(var(--color-surface)); }
.task-link[data-current='true'] { background: rgb(var(--color-brand-soft)); }
.task-link-content { display: grid; min-width: 0; gap: 2px; }
.task-link-title, .task-link-meta { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-link-title { color: rgb(var(--color-ink)); font-size: 13px; font-weight: 600; }
.task-link-meta, .workspace-meta { color: rgb(var(--color-muted)); font-size: 12px; }
.workspace-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; margin-top: 5px; overflow-wrap: anywhere; }
.status-dot { display: inline-block; flex: 0 0 8px; width: 8px; height: 8px; border-radius: 50%; background: #8290a6; }
.task-link .status-dot { margin-top: 5px; }
.status-dot[data-state='running'] { background: #0284c7; }
.status-dot[data-state='waiting'], .status-dot[data-state='queued'] { background: #c27a15; }
.status-dot[data-state='ready'] { background: #169773; }
.status-dot[data-state='failed'] { background: #e05266; }
.list-empty { padding: 12px 8px; color: rgb(var(--color-muted)); font-size: 13px; }
.workspace-empty { display: flex; min-height: 170px; flex-direction: column; align-items: center; justify-content: center; gap: 9px; padding: 20px; text-align: center; color: rgb(var(--color-muted)); }
.workspace-thread { container: workspace-thread / inline-size; }
.workspace[data-detail='false'] .workspace-thread, .workspace[data-detail='true'] .task-sidebar { display: none; }
.back-link { display: inline-flex; align-items: center; min-height: 44px; gap: 6px; margin-inline: 12px; color: rgb(var(--color-brand)); font-size: 13px; }
.workspace a:focus-visible, .workspace button:focus-visible { outline: 2px solid rgb(var(--color-brand)); outline-offset: 2px; }
.new-task { flex: 0 0 auto; }
.new-task-form { width: min(100%, 720px); padding: 14px; }
.create-input { min-height: 110px; }
.create-actions { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 10px; margin-top: 13px; }
.branch-label { flex: 1 0 min(100%, 174px); min-width: 0; margin: 0; }
.branch-label input { display: block; margin-top: 5px; }
.create-actions .btn { margin-inline-start: auto; }
.workspace-input { min-width: 0; max-width: 100%; font-size: 16px; }
textarea.workspace-input { resize: vertical; max-height: 40dvh; }
.inline-error { padding: 7px 9px; color: #9f1239; background: #fff1f2; border-radius: 5px; font-size: 13px; }
:global(.dark) .inline-error { color: #fecdd3; background: #4c0519; }
.new-task-form .inline-error { margin-top: 10px; }
.active-thread { display: grid; grid-template-rows: auto auto minmax(185px, 1fr) auto; min-height: min(540px, 70dvh); }
.active-thread > .thread-panes { grid-row: 3; max-height: min(530px, 63dvh); }
.active-toolbar { align-items: flex-start; }
.toolbar-actions { display: flex; gap: 5px; flex-shrink: 0; }
.icon-button { min-width: 44px; padding-inline: 8px; }
.composer { display: grid; grid-row: 4; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 8px; padding: 9px; border-top: 1px solid rgb(var(--color-line)); }
.composer-error { grid-column: 1 / -1; }

@container workspace (min-width: 660px) {
  .workspace-title { margin-bottom: 18px; font-size: 26px; }
  .workspace { grid-template-columns: minmax(185px, 25%) minmax(0, 1fr); }
  .workspace[data-detail] .workspace-thread { display: block; }
  .workspace[data-detail] .task-sidebar { display: flex; border-inline-end: 1px solid rgb(var(--color-line)); }
  .back-link { display: none; }
  .new-task-form { padding: 20px; }
}

@container workspace (max-width: 659px) {
  .new-task { min-height: 44px; }
}

@container workspace (max-width: 390px) {
  .stop-button { min-width: 44px; padding-inline: 8px; }
  .stop-button span { display: none; }
}
</style>
