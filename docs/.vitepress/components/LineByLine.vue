<script setup lang="ts">
import { computed, ref } from 'vue'

/** 逐行代码讲解：点左侧任意一行，右侧给出这一行在做什么。 */
const props = defineProps<{
  title?: string
  code: string
  notes: string | string[]
}>()

const lines = computed(() => String(props.code || '').replace(/\\n/g, '\n').split('\n'))
const noteList = computed<string[]>(() => {
  const v: any = props.notes
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* 当作 | 分隔 */ }
    return v.split('|').map(s => s.trim())
  }
  return []
})
const active = ref(0)
function pick(i: number) { active.value = Math.max(0, Math.min(lines.value.length - 1, i)) }
</script>

<template>
  <div class="go-card lbf-card">
    <div class="go-eyebrow" style="color:var(--go-blue)">{{ title || '逐行讲解 · Line by line' }}</div>
    <div class="lbf-grid">
      <div class="lbf-code">
        <button v-for="(l, i) in lines" :key="i" type="button" class="lbf-line"
          :class="{ active: active === i }" @click="pick(i)" :aria-pressed="active === i">
          <span class="lbf-no">{{ i + 1 }}</span><code>{{ l || ' ' }}</code>
        </button>
      </div>
      <div class="lbf-note">
        <div class="lbf-note-head">第 {{ active + 1 }} 行 <span>/ 共 {{ lines.length }} 行</span></div>
        <p>{{ noteList[active] || '这一行是结构性的样板代码，没有额外说明。' }}</p>
        <div class="lbf-nav">
          <button class="go-btn ghost" type="button" :disabled="active === 0" @click="pick(active - 1)">← 上一行</button>
          <button class="go-btn ghost" type="button" :disabled="active >= lines.length - 1" @click="pick(active + 1)">下一行 →</button>
        </div>
      </div>
    </div>
  </div>
</template>
