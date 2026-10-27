<script setup lang="ts">
withDefaults(defineProps<{
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  loading?: boolean
  disabled?: boolean
  type?: 'button' | 'submit' | 'reset'
}>(), { variant: 'primary', size: 'md', loading: false, disabled: false, type: 'button' })
</script>

<template>
  <button
    :type="type"
    :class="['btn', `btn-${variant}`, `btn-${size}`]"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
  >
    <svg v-if="loading" class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2" opacity=".25" />
      <path d="M12 3a9 9 0 019 9" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </svg>
    <slot />
  </button>
</template>
