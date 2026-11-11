<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  title?: string
  steps: string | any[]
  startLabel?: string
}>()

interface Step { title: string; body?: string; tip?: string }
function parse(v: any): Step[] {
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { /* ignore */ }
    return v.split('||').map(s => ({ title: s.trim() }))
  }
  return []
}
function esc(s: string) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function html(s?: string) {
  if (!s) return ''
  return esc(s.replace(/\\n/g, '\n'))
    .split('\n').map(line => line.replace(/`([^`]+)`/g, '<code>$1</code>')).join('<br>')
}

const items = computed(() => parse(props.steps))
const shown = ref(1)
const all = ref(false)
const visible = computed(() => all.value ? items.value.length : shown.value)
function next() { if (shown.value < items.value.length) shown.value++ }
function prev() { if (shown.value > 1) shown.value-- }
function toggleAll() { all.value = !all.value; if (!all.value) shown.value = 1 }
</script>

<template>
  <div class="go-card">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:1rem;flex-wrap:wrap">
      <div>
        <div class="go-eyebrow">跟着做 · Guided Steps</div>
        <div class="go-title">{{ title || '分步实现' }}</div>
      </div>
      <button class="go-btn ghost" @click="toggleAll">{{ all ? '逐步模式' : '展开全部' }}</button>
    </div>
    <div style="height:5px;border-radius:99px;background:var(--go-soft);margin:.9rem 0 1.3rem;overflow:hidden">
      <i :style="{ display:'block',height:'100%',borderRadius:'99px',background:'var(--go-brand)',width: (visible/items.length*100)+'%',transition:'width .35s ease' }"></i>
    </div>
    <div class="go-steps">
      <div v-for="(s, i) in items" :key="i" v-show="i < visible" class="go-step">
        <h4>{{ s.title }}</h4>
        <div v-if="s.body" class="go-sub" style="font-size:13.5px;line-height:1.9" v-html="html(s.body)"></div>
        <div v-if="s.tip" class="go-callout tip" style="margin:.55rem 0 0;font-size:13px;padding:.5rem .75rem">
          <span class="ico">💡</span><div>{{ s.tip }}</div>
        </div>
      </div>
    </div>
    <div v-if="!all" class="q-foot">
      <button class="go-btn" :disabled="shown <= 1" @click="prev">上一步</button>
      <button class="go-btn primary" :disabled="shown >= items.length" @click="next">
        {{ shown >= items.length ? '已完成全部步骤' : '下一步 →' }}
      </button>
      <span class="go-sub">{{ shown }} / {{ items.length }}</span>
    </div>
  </div>
</template>
