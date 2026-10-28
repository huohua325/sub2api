<template>
  <section class="quota-board" data-testid="quota-ruler" :aria-busy="loading">
    <header class="quota-board-head">
      <div>
        <p class="quota-board-kicker">{{ t('design.quotaTitle') }}</p>
        <p class="quota-board-lead">{{ t('design.quotaLead') }}</p>
        <p v-if="lead" class="quota-board-lead">{{ lead }}</p>
        <p v-else-if="loading" class="quota-board-lead">{{ t('dashboard.ruler.loading') }}</p>
      </div>
      <button type="button" class="quota-board-reload" @click="emit('reload')">
        {{ t('dashboard.ruler.reload') }}
      </button>
    </header>

    <div v-if="loading" class="quota-board-grid" :aria-label="t('dashboard.ruler.loading')">
      <div v-for="id in purposeOrder" :key="id" class="quota-meter is-skeleton">
        <p class="quota-meter-name" translate="no">{{ t(`dashboard.purpose.${id}`) }}</p>
        <p class="quota-meter-bone" />
        <p class="quota-meter-track" />
      </div>
    </div>

    <div v-else-if="failed && allowances == null" class="quota-board-fail" role="alert">
      <p>{{ t('dashboard.ruler.loadFailed') }}</p>
    </div>

    <div v-else>
      <p v-if="failed" class="quota-board-stale" role="status">{{ t('dashboard.ruler.stale') }}</p>
      <div class="quota-board-grid">
      <article
        v-for="meter in meters"
        :key="meter.id"
        class="quota-meter"
        :class="{ 'is-featured': meter.featured }"
        :data-testid="`${meter.id}-purpose-card`"
      >
        <div class="quota-meter-top">
          <h2 class="quota-meter-name" translate="no">{{ meter.title }}</h2>
          <span class="quota-badge" :class="meter.tone">{{ meter.badge }}</span>
        </div>
        <p v-if="meter.missing" class="quota-meter-missing">{{ t('dashboard.ruler.missing') }}</p>
        <template v-else>
          <p class="quota-meter-used">{{ t('dashboard.ruler.usedLabel') }}</p>
          <p class="quota-meter-percent">{{ meter.percent }}<span>%</span></p>
          <p class="quota-meter-amount" :aria-hidden="meter.amount ? undefined : true">{{ meter.amount }}</p>
          <div class="quota-meter-track" aria-hidden="true">
            <div class="quota-meter-fill" :class="meter.tone" :style="{ width: Math.max(meter.percent, meter.percent > 0 ? 2 : 0) + '%' }" />
          </div>
          <p v-if="meter.resetLine" class="quota-meter-reset">{{ meter.resetLine }}</p>
          <p v-if="meter.pending" class="quota-meter-reset">{{ t('dashboard.ruler.pendingHint') }}</p>
        </template>
        <p v-if="meter.row?.activity_status && meter.row.activity_status !== 'ready'" class="quota-meter-reset" role="status">{{ t(`codingQuota.student.${meter.row.activity_status}`) }}</p>
        <p v-if="meter.exhausted" class="quota-meter-note">
          {{ t('dashboard.ruler.exhausted') }}
          <router-link to="/usage">{{ t('dashboard.ruler.viewUsage') }}</router-link>
        </p>
        <details class="quota-meter-details">
          <summary>{{ t('dashboard.ruler.details') }}</summary>
          <dl>
            <dt>{{ t('dashboard.ruler.periodStart') }}</dt><dd>{{ absoluteTime(meter.row?.period_start) }}</dd>
            <dt>{{ t('dashboard.ruler.resetTime') }}</dt><dd>{{ absoluteTime(meter.row?.resets_at) }}</dd>
            <dt>{{ t('dashboard.ruler.dataState') }}</dt><dd>{{ meter.dataState }}</dd>
            <dt>{{ t('dashboard.ruler.ledgerUpdated') }}</dt><dd>{{ absoluteTime(meter.row?.updated_at) }}</dd>
            <dt>{{ t('dashboard.ruler.pageRead') }}</dt><dd>{{ absoluteTime(meter.row?.read_at) }}</dd>
          </dl>
          <p v-if="meter.row?.reset_reason === 'platform_reset'">{{ t('dashboard.ruler.platformReset') }}</p>
          <p>{{ t(meter.id === 'experiment' ? 'dashboard.ruler.monthExplanation' : 'dashboard.ruler.weekExplanation') }}</p>
        </details>
      </article>
      </div>
    </div>

    <footer v-if="!loading && keyCount === 0" class="quota-board-foot">
      <router-link class="quota-board-action" to="/guide">{{ t('dashboard.ruler.createKey') }}</router-link>
    </footer>
    <footer v-else-if="!loading && keyCount != null" class="quota-board-foot">
      <router-link class="quota-board-action" to="/keys">{{ t('dashboard.ruler.keysInUse', { count: keyCount }) }}</router-link>
      <router-link class="quota-board-action is-quiet" to="/usage">{{ t('dashboard.ruler.viewUsage') }}</router-link>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PurposeAllowanceItem } from '@/api/admin/users'

const purposeOrder = ['openai', 'grok', 'experiment'] as const

const props = defineProps<{
  allowances: PurposeAllowanceItem[] | null
  loading: boolean
  failed: boolean
  keyCount: number | null
}>()

const emit = defineEmits<{ reload: [] }>()
const { t } = useI18n()
const nowMs = shallowRef(Date.now())
let timer = 0
const requestedResets = new Set<string>()
function tick() { if (!document.hidden) nowMs.value = Date.now() }

onMounted(() => {
  timer = window.setInterval(tick, 1000)
  document.addEventListener('visibilitychange', tick)
})
onUnmounted(() => {
  window.clearInterval(timer)
  document.removeEventListener('visibilitychange', tick)
})

watch(() => (props.allowances ?? []).filter((row) => {
  const end = Date.parse(row.resets_at ?? '')
  return Number.isFinite(end) && end <= nowMs.value + (row.clock_offset_ms ?? 0)
}).map((row) => `${row.purpose}:${row.resets_at}`).join('|'), (due) => {
  const keys = due.split('|').filter(Boolean)
  if (keys.some((key) => !requestedResets.has(key))) {
    keys.forEach((key) => requestedResets.add(key))
    emit('reload')
  }
}, { immediate: true })

const meters = computed(() => {
  const built = purposeOrder.map((id) => {
    const row = (props.allowances ?? []).find((item) => item.purpose === id)
    const usesDisplay = row != null && Object.prototype.hasOwnProperty.call(row, 'display_percent')
    const missing = !row || (usesDisplay && row.display_percent == null) || row.data_status === 'error'
    const rawPercent = usesDisplay ? (row?.display_percent ?? 0) : ratioPercent(row)
    const percent = missing || !Number.isFinite(rawPercent) ? 0 : Math.min(100, Math.max(0, rawPercent))
    const exhausted = !missing && (row?.blocked ?? rawExhausted(row))
    const pending = row?.data_status === 'pending' || row?.data_status === 'partial'
    const near = !missing && percent >= 80 && !exhausted
    return {
      id,
      row,
      dataState: missing ? t('dashboard.ruler.missing') : props.failed ? t('dashboard.ruler.stale') : t(`dashboard.ruler.states.${row?.data_status ?? 'current'}`),
      title: t(`dashboard.purpose.${id}`),
      missing,
      percent,
      amount: missing ? '' : amountLine(id, row),
      resetLine: missing ? '' : resetLine(row),
      near,
      exhausted,
      pending,
      tone: exhausted ? 'is-full' : near ? 'is-near' : missing ? 'is-missing' : 'is-ok',
      badge: missing
        ? t('admin.usageMonitor.noReading')
        : exhausted
          ? t('dashboard.ruler.full')
          : row?.data_status === 'estimated'
            ? t('dashboard.ruler.states.estimated')
          : pending || props.failed
            ? t('dashboard.ruler.pending')
          : near
            ? t('dashboard.ruler.near')
            : t('dashboard.ruler.ready'),
      featured: false,
    }
  })
  const featured = built.reduce((best, meter) => {
    if (meter.missing) return best
    if (!best || meter.percent > best.percent) return meter
    return best
  }, undefined as (typeof built)[number] | undefined)
  if (featured) featured.featured = true
  return built
})

const lead = computed(() => {
  const featured = meters.value.find((meter) => meter.featured)
  if (!featured) return ''
  return t('dashboard.ruler.lead', { name: featured.title, percent: featured.percent })
})

function ratioPercent(row: PurposeAllowanceItem | undefined): number {
  const limit = row?.limit_amount ?? 0
  const used = row?.used_amount ?? 0
  if (limit <= 0) return 0
  return Math.min(100, Math.round((used / limit) * 100))
}

function absoluteTime(value: string | undefined): string {
  const timestamp = Date.parse(value ?? '')
  if (!Number.isFinite(timestamp) || timestamp <= 0) return t('dashboard.ruler.unknownTime')
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).format(timestamp) + ' UTC+8'
}

function rawExhausted(row: PurposeAllowanceItem | undefined): boolean {
  if (!row) return false
  if (row.point_used != null && row.point_limit != null) return Number(row.point_used) >= Number(row.point_limit)
  return row.used_amount != null && row.limit_amount != null && row.limit_amount >= 0 && row.used_amount >= row.limit_amount
}

function amountLine(id: (typeof purposeOrder)[number], row: PurposeAllowanceItem | undefined): string {
  if (id !== 'experiment' || row?.used_amount == null || row.limit_amount == null) return ''
  return `${formatNumber(row.used_amount)} / ${formatNumber(row.limit_amount)} ${t('dashboard.purpose.yuan')}`
}

function formatNumber(value: number | string): string {
  const number = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(number)) return String(value)
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 }).format(number)
}

function resetLine(row: PurposeAllowanceItem | undefined): string {
  if (!row) return ''
  const scope = scopeLabel(row.reset_scope)
  const time = countdown(row.resets_at, row.clock_offset_ms ?? 0)
  if (scope && time) return `${scope} · ${time}`
  return scope || time
}

function scopeLabel(scope: PurposeAllowanceItem['reset_scope']): string {
  if (scope === 'shared') return t('dashboard.ruler.sharedWindow')
  if (scope === 'personal') return t('dashboard.ruler.personalWindow')
  if (scope === 'month') return t('dashboard.ruler.monthWindow')
  if (scope === 'week') return t('dashboard.ruler.personalWindow')
  return ''
}

function countdown(iso: string | undefined, offset: number): string {
  if (!iso) return ''
  const end = new Date(iso).getTime()
  if (Number.isNaN(end)) return ''
  const remaining = end - (nowMs.value + offset)
  if (remaining <= 0) return t('dashboard.ruler.awaitingReset')
  if (remaining <= 60_000) return t('dashboard.platformQuota.resetSoon')
  const totalMinutes = Math.floor(remaining / 60_000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  const parts: string[] = []
  if (days > 0) parts.push(t('dashboard.platformQuota.countdownDay', { count: days }))
  if (hours > 0 || days > 0) parts.push(t('dashboard.platformQuota.countdownHour', { count: hours }))
  if (days === 0) parts.push(t('dashboard.platformQuota.countdownMinute', { count: minutes }))
  return t('dashboard.platformQuota.resetsAt', { time: parts.join('') })
}
</script>

<style scoped>

.quota-board { overflow: hidden; border: 1px solid rgb(var(--color-line) / .8); border-radius: 24px; background: rgb(var(--color-surface)); box-shadow: var(--shadow-panel); }
.quota-board-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 26px 28px 10px; }
.quota-board-kicker { color: rgb(var(--color-ink)); font-size: 16px; font-weight: 600; }
.quota-board-lead { margin-top: 8px; color: rgb(var(--color-muted)); font-size: 13px; line-height: 1.7; }
.quota-board-reload { flex: 0 0 auto; min-height: 44px; border: 1px solid rgb(var(--color-line)); border-radius: 10px; background: rgb(var(--color-surface)); padding: 0 14px; color: rgb(var(--color-muted)); font-size: 12px; cursor: pointer; }
.quota-board-reload:hover { color: rgb(var(--color-brand)); border-color: rgb(var(--color-brand) / .4); }
.quota-board-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0; padding: 18px 12px 24px; }
.quota-meter { min-width: 0; padding: 8px 24px; position: relative; }
.quota-meter + .quota-meter { border-left: 1px solid rgb(var(--color-line) / .65); }
.quota-meter-top { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.quota-meter-name { font-size: 14px; font-weight: 600; color: rgb(var(--color-ink)); }
.quota-meter-name::before { content: ''; display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: rgb(var(--color-brand)); margin-right: 8px; }
.quota-meter:nth-child(2) .quota-meter-name::before { background: #2795a5; }
.quota-meter:nth-child(3) .quota-meter-name::before { background: #9570c8; }
.quota-badge { font-size: 10px; padding: 4px 7px; border-radius: 6px; font-weight: 500; line-height: 1.4; }
.quota-badge.is-ok { color: rgb(var(--color-success)); background: rgb(var(--color-success) / .08); }
.quota-badge.is-near { color: rgb(var(--color-warning)); background: rgb(var(--color-warning) / .08); }
.quota-badge.is-full { color: rgb(var(--color-danger)); background: rgb(var(--color-danger) / .08); }
.quota-badge.is-missing { color: rgb(var(--color-muted)); background: rgb(var(--color-line) / .3); }
.quota-meter-used { margin-top: 20px; font-size: 12px; color: rgb(var(--color-muted)); }
.quota-meter-details { margin-top: 8px; font-size: 11px; line-height: 1.8; color: rgb(var(--color-muted)); }
.quota-meter-details summary { display: flex; align-items: center; min-height: 44px; color: rgb(var(--color-brand)); cursor: pointer; }
.quota-meter-details dt { margin-top: 8px; font-weight: 600; }
.quota-meter-details dd { overflow-wrap: anywhere; }
.quota-meter-details p { margin-top: 12px; }
.quota-meter-percent { font-size: clamp(40px, 4vw, 56px); line-height: 1.35; margin-top: 2px; font-weight: 500; letter-spacing: -.07em; font-variant-numeric: tabular-nums; color: rgb(var(--color-ink)); }
.quota-meter-percent span { font-size: 20px; margin-left: 6px; letter-spacing: -.02em; color: rgb(var(--color-muted)); }
.quota-meter-amount { min-height: 22px; margin-top: 4px; font-size: 12px; font-variant-numeric: tabular-nums; color: rgb(var(--color-muted)); }
.quota-meter-track { overflow: hidden; margin-top: 22px; height: 8px; border-radius: 99px; background: repeating-linear-gradient(90deg, rgb(var(--color-line) / .5) 0, rgb(var(--color-line) / .5) calc(10% - 3px), transparent calc(10% - 3px), transparent 10%); }
.quota-meter-fill { height: 100%; border-radius: inherit; background: #6750d8; transition: width 220ms ease-out; }
.quota-meter-fill.is-near { background: rgb(var(--color-warning)); }
.quota-meter-fill.is-full { background: rgb(var(--color-danger)); }
.quota-meter-reset { margin-top: 14px; min-height: 36px; color: rgb(var(--color-muted)); font-size: 11px; line-height: 1.7; }
.quota-meter-note { font-size: 12px; margin-top: 8px; color: rgb(var(--color-danger)); line-height: 1.7; }
.quota-meter-note a { margin-left: 6px; text-decoration: underline; text-underline-offset: 3px; }
.quota-meter-missing { padding: 38px 0; color: rgb(var(--color-muted)); font-size: 14px; }
.quota-board-fail { padding: 36px 28px; color: rgb(var(--color-danger)); font-size: 14px; }
.quota-board-stale { padding: 12px 28px 0; color: rgb(var(--color-warning)); font-size: 12px; }
.quota-board-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 20px; padding: 16px 28px; border-top: 1px solid rgb(var(--color-line) / .6); background: rgb(var(--color-page) / .55); }
.quota-board-action { display: inline-flex; min-height: 28px; align-items: center; color: rgb(var(--color-brand)); font-size: 12px; font-weight: 500; }
.quota-board-action:hover { text-decoration: underline; text-underline-offset: 4px; }
.quota-board-action.is-quiet { color: rgb(var(--color-muted)); margin-left: auto; }
.quota-meter-bone { width: 55%; height: 64px; margin-top: 24px; border-radius: 12px; background: rgb(var(--color-line) / .4); }
.is-skeleton { animation: quota-pulse 1.5s ease-in-out infinite; }
@keyframes quota-pulse { 50% { opacity: .45; } }
@media (max-width: 1100px) and (min-width: 768px) { .quota-meter { padding-inline: 16px; } .quota-board-grid { padding-inline: 6px; } }
@media (max-width: 767px) {
  .quota-board-head { padding: 22px 20px 10px; }
  .quota-board-grid { grid-template-columns: 1fr; padding: 0 20px 12px; }
  .quota-meter { padding: 24px 0; }
  .quota-meter + .quota-meter { border-left: 0; border-top: 1px solid rgb(var(--color-line) / .65); }
  .quota-meter-percent { margin-top: 16px; font-size: 48px; }
  .quota-meter-reset { min-height: 0; }
  .quota-board-foot { padding-inline: 20px; gap: 12px; }
  .quota-board-action { min-height: 44px; }
}
</style>
