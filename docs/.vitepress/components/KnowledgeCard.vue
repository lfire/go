<script setup lang="ts">
import { computed } from 'vue'

/** 一页纸速记：要点 / 易错点 / 一句话总结。 */
const props = defineProps<{
  title?: string
  points: string | string[]
  pitfalls?: string | string[]
  summary?: string
}>()
function arr(v: any): string[] {
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* 当作 | 分隔 */ }
    return v.split('|').map(s => s.trim()).filter(Boolean)
  }
  return []
}
const ps = computed(() => arr(props.points))
const bad = computed(() => arr(props.pitfalls))
</script>

<template>
  <div class="kc-card">
    <div class="kc-head">📌 {{ title || '一页纸速记' }}</div>
    <div class="kc-cols">
      <div class="kc-col">
        <div class="kc-label ok">✓ 记住这些</div>
        <ul><li v-for="(p, i) in ps" :key="i">{{ p }}</li></ul>
      </div>
      <div v-if="bad.length" class="kc-col">
        <div class="kc-label bad">⚠ 别踩这些坑</div>
        <ul><li v-for="(p, i) in bad" :key="i">{{ p }}</li></ul>
      </div>
    </div>
    <div v-if="summary" class="kc-summary"><b>一句话</b> {{ summary }}</div>
  </div>
</template>
