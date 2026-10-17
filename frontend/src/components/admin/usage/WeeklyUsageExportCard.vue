<template>
  <div class="card p-6" data-testid="weekly-usage-export">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.usage.weeklyExport.title') }}</h2>
    <p class="mt-1 max-w-4xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usage.weeklyExport.description') }}</p>

    <div class="mt-4 flex flex-wrap items-end gap-4">
      <div class="w-full sm:w-[28rem]">
        <label class="input-label" for="weekly-usage-week">{{ t('admin.usage.weeklyExport.week') }}</label>
        <Select
          id="weekly-usage-week"
          v-model="selectedStart"
          :options="weekOptions"
          data-testid="weekly-usage-week"
        />
      </div>
      <button
        type="button"
        class="btn btn-secondary"
        data-testid="weekly-usage-download"
        :disabled="exporting || !selectedStart"
        @click="downloadWeek"
      >
        {{ weekDownloading ? t('admin.usage.weeklyExport.downloading') : t('admin.usage.weeklyExport.download') }}
      </button>
    </div>

    <div class="mt-5 border-t border-gray-200 pt-4 dark:border-dark-700">
      <p class="mb-3 max-w-4xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usage.weeklyExport.filterHint') }}</p>
      <div class="flex flex-wrap items-end gap-4">
        <div>
          <label class="input-label">{{ t('admin.dashboard.timeRange') }}</label>
          <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" />
        </div>
        <div class="w-full sm:w-auto sm:min-w-[200px]">
          <label class="input-label">{{ t('admin.usage.group') }}</label>
          <Select v-model="groupId" :options="groupOptions" searchable data-testid="weekly-usage-group" />
        </div>
        <div ref="accountSearchRef" class="relative w-full sm:w-auto sm:min-w-[220px]">
          <label class="input-label">{{ t('admin.usage.account') }}</label>
          <input
            v-model="accountKeyword"
            type="text"
            class="input pr-8"
            data-testid="weekly-usage-account"
            :placeholder="t('admin.usage.searchAccountPlaceholder')"
            @input="debounceAccountSearch"
            @focus="showAccountDropdown = true"
          />
          <button
            v-if="accountId"
            type="button"
            class="absolute right-2 top-9 text-gray-400"
            aria-label="Clear account filter"
            @click="clearAccount"
          >
            ✕
          </button>
          <div
            v-if="showAccountDropdown && (accountResults.length > 0 || accountKeyword)"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:bg-dark-800"
          >
            <button
              v-for="account in accountResults"
              :key="account.id"
              type="button"
              class="w-full px-4 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
              @click="selectAccount(account)"
            >
              <span class="truncate">{{ account.name }}</span>
              <span class="ml-2 text-xs text-gray-400">#{{ account.id }}</span>
            </button>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-primary"
          data-testid="weekly-usage-filter-download"
          :disabled="exporting || !startDate || !endDate"
          @click="downloadFilter"
        >
          {{ filterDownloading ? t('admin.usage.weeklyExport.downloading') : t('admin.usage.weeklyExport.filterDownload') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { exportFilteredUsage, exportWeeklyUsage, listWeeklyExportWeeks, type WeeklyExportWeek } from '@/api/admin/usage'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { useAppStore } from '@/stores/app'

interface AccountOption {
  id: number
  name: string
}

const { t } = useI18n()
const appStore = useAppStore()

const weeks = ref<WeeklyExportWeek[]>([])
const selectedStart = ref('')
const weekDownloading = ref(false)
const filterDownloading = ref(false)
const startDate = ref(formatYMD(daysAgo(6)))
const endDate = ref(formatYMD(new Date()))
const groupId = ref<number | null>(null)
const groupOptions = ref<SelectOption[]>([{ value: null, label: t('admin.usage.allGroups') }])
const accountId = ref<number | null>(null)
const accountKeyword = ref('')
const accountResults = ref<AccountOption[]>([])
const showAccountDropdown = ref(false)
const accountSearchRef = ref<HTMLElement | null>(null)
let accountSearchTimeout: ReturnType<typeof setTimeout> | null = null

const exporting = computed(() => weekDownloading.value || filterDownloading.value)
const weekOptions = computed<SelectOption[]>(() =>
  weeks.value.map((week) => ({ value: week.start, label: weekLabel(week) }))
)

function daysAgo(days: number): Date {
  const date = new Date()
  date.setDate(date.getDate() - days)
  return date
}

function formatYMD(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

function weekLabel(week: WeeklyExportWeek): string {
  const text = `${shortTime(week.start)} ~ ${shortTime(week.end)}`
  return week.current ? `${text}${t('admin.usage.weeklyExport.currentSuffix')}` : text
}

function shortTime(value: string): string {
  return value.slice(0, 16).replace('T', ' ')
}

function errorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' && error.message) {
    return error.message
  }
  return fallback
}

function saveBlob(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.click()
  URL.revokeObjectURL(url)
}

async function loadWeeks() {
  try {
    const result = await listWeeklyExportWeeks()
    weeks.value = result.weeks || []
    const current = weeks.value.find((week) => week.current) || weeks.value[0]
    selectedStart.value = current?.start || ''
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.usage.weeklyExport.failed')))
  }
}

async function loadGroups() {
  try {
    const result = await adminAPI.groups.list(1, 1000)
    groupOptions.value = [
      { value: null, label: t('admin.usage.allGroups') },
      ...(result.items || []).map((group) => ({ value: group.id, label: group.name }))
    ]
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.usage.weeklyExport.filterFailed')))
  }
}

function debounceAccountSearch() {
  if (accountSearchTimeout) clearTimeout(accountSearchTimeout)
  accountSearchTimeout = setTimeout(async () => {
    if (!accountKeyword.value) {
      accountResults.value = []
      return
    }
    try {
      const result = await adminAPI.accounts.list(1, 20, { search: accountKeyword.value })
      accountResults.value = (result.items || []).map((account) => ({ id: account.id, name: account.name }))
    } catch {
      accountResults.value = []
    }
  }, 300)
}

function selectAccount(account: AccountOption) {
  accountId.value = account.id
  accountKeyword.value = account.name
  showAccountDropdown.value = false
}

function clearAccount() {
  accountId.value = null
  accountKeyword.value = ''
  accountResults.value = []
  showAccountDropdown.value = false
}

function onDocumentClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Node)) return
  if (!accountSearchRef.value?.contains(target)) showAccountDropdown.value = false
}

async function downloadWeek() {
  if (!selectedStart.value || exporting.value) return
  weekDownloading.value = true
  try {
    const { blob, fileName } = await exportWeeklyUsage(selectedStart.value)
    saveBlob(blob, fileName)
    appStore.showSuccess(t('admin.usage.weeklyExport.success'))
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.usage.weeklyExport.failed')))
  } finally {
    weekDownloading.value = false
  }
}

async function downloadFilter() {
  if (!startDate.value || !endDate.value || exporting.value) return
  filterDownloading.value = true
  try {
    const { blob, fileName } = await exportFilteredUsage({
      start_date: startDate.value,
      end_date: endDate.value,
      group_id: typeof groupId.value === 'number' ? groupId.value : undefined,
      account_id: accountId.value ?? undefined
    })
    saveBlob(blob, fileName)
    appStore.showSuccess(t('admin.usage.weeklyExport.filterSuccess'))
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.usage.weeklyExport.filterFailed')))
  } finally {
    filterDownloading.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  void loadWeeks()
  void loadGroups()
})

onUnmounted(() => {
  document.removeEventListener('click', onDocumentClick)
  if (accountSearchTimeout) clearTimeout(accountSearchTimeout)
})
</script>
