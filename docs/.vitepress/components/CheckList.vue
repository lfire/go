<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { readChecks, setChecks } from '../utils'

const props = defineProps<{ id: string; title?: string; items: string | string[] }>()
const list = computed<string[]>(() => {
  const v: any = props.items
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* ignore */ }
    return v.split('|').map(s => s.trim()).filter(Boolean)
  }
  return []
})
const state = ref<boolean[]>([])
onMounted(() => {
  const saved = readChecks(props.id)
  state.value = list.value.map((_, i) => saved[i] || false)
})
function toggle(i: number) {
  const next = [...state.value]
  next[i] = !next[i]
  state.value = next
  setChecks(props.id, next)
}
function reset() {
  state.value = list.value.map(() => false)
  setChecks(props.id, state.value)
}
const count = computed(() => state.value.filter(Boolean).length)
</script>

<template>
  <div class="go-card">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:1rem">
      <div>
        <div class="go-eyebrow">学习清单 · Checklist</div>
        <div class="go-title">{{ title || '确认你已掌握' }}</div>
      </div>
      <span class="go-sub">{{ count }} / {{ list.length }}</span>
    </div>
    <div style="height:5px;border-radius:99px;background:var(--go-soft);margin:.8rem 0 1rem;overflow:hidden">
      <i :style="{display:'block',height:'100%',borderRadius:'99px',background:'var(--go-green)',width:(list.length?count/list.length*100:0)+'%',transition:'width .3s ease'}"></i>
    </div>
    <label v-for="(it, i) in list" :key="i" class="go-check" :class="{done: state[i]}">
      <input type="checkbox" :checked="state[i]" @change="toggle(i)" />
      <span>{{ it }}</span>
    </label>
    <button class="go-btn ghost" style="margin-top:.7rem;font-size:12px;padding:4px 10px" @click="reset">重置清单</button>
  </div>
</template>
