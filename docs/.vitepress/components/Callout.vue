<script setup lang="ts">
import RichText from './RichText.vue'

withDefaults(defineProps<{
  type?: 'tip' | 'warn' | 'idea' | 'note'
  title?: string
  text?: string
}>(), { type: 'tip' })
const icons: Record<string, string> = { tip: '✅', warn: '⚠️', idea: '💡', note: '📌' }
</script>

<template>
  <div class="go-callout" :class="type">
    <span class="ico">{{ icons[type] }}</span>
    <div>
      <b v-if="title" style="display:block;margin-bottom:.15rem"><RichText :text="title" /></b>
      <span v-if="text"><RichText :text="text" /></span>
      <slot v-else />
    </div>
  </div>
</template>
