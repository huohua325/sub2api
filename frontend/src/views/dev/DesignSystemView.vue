<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from '@/composables/useTheme'
import BaseButton from '@/components/common/BaseButton.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SpectrumArt from '@/components/common/SpectrumArt.vue'

const { t } = useI18n()
const { toggleTheme } = useTheme()
const dialog = ref(false)
const selected = ref('codex')
const name = ref('')
const options = [{ value: 'codex', label: 'Codex' }, { value: 'grok', label: 'Grok' }]
</script>

<template>
  <main class="mx-auto max-w-6xl space-y-8 p-6 md:p-12">
    <PageHeader :title="t('design.showcase')" :description="t('design.showcaseLead')">
      <template #actions><BaseButton variant="secondary" @click="toggleTheme">{{ t('design.theme') }}</BaseButton></template>
    </PageHeader>
    <section class="lab-welcome">
      <div class="lab-welcome-copy"><span class="lab-kicker">MILE Lab · Sora</span><h1>{{ t('design.heroTitle') }}</h1><p>{{ t('design.heroLead') }}</p><BaseButton>{{ t('design.enter') }}</BaseButton></div>
      <SpectrumArt class="lab-welcome-art" />
    </section>
    <section class="lab-panel space-y-6 p-6">
      <h2 class="text-lg font-semibold">{{ t('common.actions') }}</h2>
      <div class="flex flex-wrap gap-3">
        <BaseButton @click="dialog = true">{{ t('design.openDialog') }}</BaseButton>
        <BaseButton variant="secondary">{{ t('common.cancel') }}</BaseButton>
        <BaseButton variant="ghost">{{ t('common.refresh') }}</BaseButton>
        <BaseButton variant="danger">{{ t('common.delete') }}</BaseButton>
        <BaseButton disabled>{{ t('common.disabled') }}</BaseButton>
        <BaseButton loading>{{ t('common.loading') }}</BaseButton>
      </div>
      <div class="grid gap-6 md:grid-cols-2">
        <label class="space-y-2 text-sm"><span>{{ t('design.sampleInput') }}</span><input v-model="name" class="input w-full" :placeholder="t('design.samplePlaceholder')" /></label>
        <div class="space-y-2 text-sm"><label for="showcase-select">{{ t('design.routes') }}</label><Select id="showcase-select" v-model="selected" :options="options" /></div>
      </div>
      <div class="flex flex-wrap gap-3"><StatusBadge v-for="status in ['success', 'warning', 'error', 'inactive']" :key="status" :status="status" :label="status" /></div>
      <Skeleton width="60%" height="20px" /><Skeleton width="100%" height="60px" />
    </section>
    <section class="lab-panel overflow-x-auto">
      <table class="table min-w-[560px]"><thead><tr><th>{{ t('design.sampleInput') }}</th><th>{{ t('common.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr><td>API experiment · 0123456789</td><td><StatusBadge status="active" :label="t('common.enabled')" /></td><td><BaseButton variant="ghost" size="sm">{{ t('common.edit') }}</BaseButton></td></tr></tbody></table>
      <EmptyState :title="t('design.sampleEmpty')" :description="t('design.sampleEmptyDescription')" />
    </section>
    <BaseDialog :show="dialog" :title="t('design.sampleDialog')" @close="dialog = false">
      <p class="text-sm leading-7 text-muted">{{ t('design.sampleDescription') }}</p>
      <template #footer><BaseButton variant="secondary" @click="dialog = false">{{ t('common.cancel') }}</BaseButton><BaseButton @click="dialog = false">{{ t('common.confirm') }}</BaseButton></template>
    </BaseDialog>
  </main>
</template>
