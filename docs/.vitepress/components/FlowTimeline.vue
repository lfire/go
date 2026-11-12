<script setup lang="ts">
import { computed } from 'vue'

/** 流程 / 时间线：横向展示步骤链，纵向展示阶段演进。 */
interface Step { title: string; desc?: string; tone?: string }
const props = withDefaults(defineProps<{
  title?: string
  steps: string | any[]
  direction?: 'horizontal' | 'vertical'
}>(), { direction: 'horizontal' })

const list = computed<Step[]>(() => {
  const v: any = props.steps
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { /* 当作 || 分隔 */ }
    return v.split('||').map(s => ({ title: s.trim() }))
  }
  return []
})
const COLORS: Record<string, string> = {
  orange: 'var(--go-brand)', blue: 'var(--go-blue)', green: 'var(--go-green)',
  purple: 'var(--go-purple)', red: 'var(--go-red)', gray: 'var(--go-muted)',
}
function color(t?: string) { return COLORS[t || ''] || 'var(--go-brand)' }
</script>

<template>
  <div class="ft-wrap">
    <div v-if="title" class="go-eyebrow" style="color:var(--go-purple)">{{ title }}</div>
    <div class="ft-list" :class="direction">
      <div v-for="(s, i) in list" :key="i" class="ft-item" :style="{ '--ft-color': color(s.tone) }">
        <div class="ft-dot"><b>{{ i + 1 }}</b></div>
        <div class="ft-card">
          <div class="ft-title">{{ s.title }}</div>
          <div v-if="s.desc" class="ft-desc">{{ s.desc }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
