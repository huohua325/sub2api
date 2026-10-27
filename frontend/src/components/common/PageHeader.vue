<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <header class="lab-page-header">
    <div class="min-w-0">
      <h1>{{ title }}</h1>
      <p v-if="description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="lab-page-actions"><slot name="actions" /></div>
  </header>
</template>
