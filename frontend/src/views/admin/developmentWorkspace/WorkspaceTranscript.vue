<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { WorkspaceDetail } from '@/api/admin/developmentWorkspace'
import Icon from '@/components/icons/Icon.vue'

const { task } = defineProps<{ task: WorkspaceDetail }>()
const { t, locale } = useI18n()
const panel = shallowRef<'conversation' | 'activity' | 'changes'>('conversation')
const evidencePanel = computed(() => panel.value === 'changes' ? 'changes' : 'activity')
const conversation = useTemplateRef<HTMLElement>('conversation')
const lastMessage = computed(() => task.messages?.at(-1))
const timestamp = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }))
let followTail = true

function showTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? '' : timestamp.value.format(date)
}

function updateFollowTail() {
  const node = conversation.value
  if (node && node.clientHeight) {
    followTail = node.scrollHeight - node.clientHeight - node.scrollTop < 96
  }
}

watch([lastMessage, panel], async () => {
  if (!followTail) return
  await nextTick()
  const node = conversation.value
  if (node?.clientHeight) node.scrollTop = node.scrollHeight
}, { immediate: true, flush: 'post' })
</script>

<template>
  <div class="thread-panes" :data-panel="panel">
    <div class="thread-tabs" :aria-label="t('admin.developmentWorkspace.details')" role="group">
      <button
        type="button"
        class="conversation-tab"
        :aria-pressed="panel === 'conversation'"
        @click="panel = 'conversation'"
      >
        <Icon name="chat" size="sm" aria-hidden="true" />
        {{ t('admin.developmentWorkspace.conversation') }}
      </button>
      <button
        type="button"
        data-testid="workspace-tab-log"
        :aria-pressed="evidencePanel === 'activity' && panel !== 'conversation'"
        @click="panel = 'activity'"
      >
        <Icon name="terminal" size="sm" aria-hidden="true" />
        {{ t('admin.developmentWorkspace.activity') }}
      </button>
      <button
        type="button"
        data-testid="workspace-tab-changes"
        :aria-pressed="panel === 'changes'"
        @click="panel = 'changes'"
      >
        <Icon name="document" size="sm" aria-hidden="true" />
        {{ t('admin.developmentWorkspace.changes') }}
      </button>
    </div>

    <section
      ref="conversation"
      class="conversation"
      :aria-label="t('admin.developmentWorkspace.conversation')"
      @scroll.passive="updateFollowTail"
    >
      <p v-if="!task.messages?.length" class="pane-empty">
        {{ t('admin.developmentWorkspace.noMessages') }}
      </p>
      <ol v-else class="message-list">
        <li v-for="message in task.messages" :key="message.id" data-testid="workspace-message" class="message">
          <div class="message-meta">
            <Icon :name="message.role === 'user' ? 'user' : 'terminal'" size="sm" aria-hidden="true" />
            <strong>{{ message.role === 'user' ? t('admin.developmentWorkspace.admin') : message.role === 'assistant' ? 'Codex' : t('admin.developmentWorkspace.system') }}</strong>
            <time v-if="showTime(message.created_at)" :datetime="message.created_at">{{ showTime(message.created_at) }}</time>
          </div>
          <p class="message-text" :class="{ 'message-user': message.role === 'user' }">{{ message.text }}</p>
        </li>
      </ol>
    </section>

    <section v-if="evidencePanel === 'activity'" class="evidence" :aria-label="t('admin.developmentWorkspace.activity')">
      <p v-if="!task.activity?.length" class="pane-empty">{{ t('admin.developmentWorkspace.noActivity') }}</p>
      <ol v-else class="evidence-list">
        <li v-for="activity in task.activity" :key="activity.id" class="evidence-item">
          <time v-if="showTime(activity.created_at)" :datetime="activity.created_at" class="evidence-time">{{ showTime(activity.created_at) }}</time>
          <pre class="evidence-text">{{ activity.text }}</pre>
        </li>
      </ol>
    </section>
    <section v-else class="evidence" :aria-label="t('admin.developmentWorkspace.changes')">
      <p v-if="!task.changes?.length" class="pane-empty">{{ t('admin.developmentWorkspace.noChanges') }}</p>
      <ul v-else class="evidence-list">
        <li v-for="change in task.changes" :key="change.path" class="evidence-item">
          <code class="evidence-text">{{ change.path }}</code>
          <div class="file-stats" v-if="change.additions != null || change.deletions != null">
            <span v-if="change.additions != null" class="text-emerald-700 dark:text-emerald-300">+{{ change.additions }}</span>
            <span v-if="change.deletions != null" class="text-rose-700 dark:text-rose-300">-{{ change.deletions }}</span>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.thread-panes { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; }
.thread-tabs { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; padding: 3px 9px; border-bottom: 1px solid rgb(var(--color-line)); }
.thread-tabs button { display: inline-flex; align-items: center; justify-content: center; gap: 5px; min-height: 40px; padding: 0 10px; color: rgb(var(--color-muted)); border-bottom: 2px solid transparent; font-size: 13px; }
.thread-tabs button:hover, .thread-tabs button[aria-pressed='true'] { color: rgb(var(--color-brand)); }
.thread-tabs button[aria-pressed='true'] { border-color: rgb(var(--color-brand)); }
.conversation, .evidence { min-width: 0; min-height: 0; overflow: auto; overscroll-behavior: contain; scrollbar-gutter: stable; }
.thread-panes[data-panel='conversation'] .evidence, .thread-panes:not([data-panel='conversation']) .conversation { display: none; }
.pane-empty { padding: 18px; color: rgb(var(--color-muted)); font-size: 13px; }
.message-list { max-width: 76ch; margin-inline: auto; padding: 16px; }
.message + .message { margin-top: 18px; }
.message-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; color: rgb(var(--color-muted)); font-size: 12px; }
.message-meta strong { color: rgb(var(--color-ink)); }
.message-meta time { margin-inline-start: auto; }
.message-text { margin-top: 6px; white-space: pre-wrap; overflow-wrap: anywhere; color: rgb(var(--color-ink)); font-size: 14px; line-height: 1.6; }
.message-user { padding: 10px 12px; background: rgb(var(--color-page)); border: 1px solid rgb(var(--color-line) / .7); border-radius: 8px; }
.evidence-list { padding: 4px 12px; }
.evidence-item { padding: 10px 0; }
.evidence-item + .evidence-item { border-top: 1px solid rgb(var(--color-line)); }
.evidence-time { display: block; margin-bottom: 4px; color: rgb(var(--color-muted)); font-size: 11px; }
.evidence-text { white-space: pre-wrap; overflow-wrap: anywhere; color: rgb(var(--color-ink)); font: 12px/1.55 var(--font-mono, monospace); }
.file-stats { display: flex; gap: 8px; margin-top: 3px; font: 12px var(--font-mono, monospace); }

@container workspace-thread (max-width: 390px) {
  .thread-tabs { gap: 2px; padding-inline: 6px; }
  .thread-tabs button { flex: 1 0 auto; gap: 0; padding-inline: 4px; white-space: nowrap; }
  .thread-tabs button svg { display: none; }
}

@container workspace-thread (min-width: 720px) {
  .thread-panes { grid-template: auto minmax(0, 1fr) / minmax(0, 1fr) minmax(200px, 30%); }
  .thread-tabs { grid-column: 2; grid-row: 1; border-inline-start: 1px solid rgb(var(--color-line)); }
  .thread-tabs .conversation-tab { display: none; }
  .conversation { grid-column: 1; grid-row: 1 / 3; }
  .evidence { grid-column: 2; grid-row: 2; border-inline-start: 1px solid rgb(var(--color-line)); }
  .thread-panes[data-panel] .conversation, .thread-panes[data-panel] .evidence { display: block; }
}
</style>
