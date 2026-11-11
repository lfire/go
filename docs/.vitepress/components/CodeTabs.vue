<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{ tabs: string | any[]; title?: string }>()
interface Tab { label: string; code: string; note?: string }
const list = computed<Tab[]>(() => {
  const v: any = props.tabs
  if (Array.isArray(v)) return v
  if (typeof v === 'string') { try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { /* ignore */ } }
  return []
})
const active = ref(0)
const code = computed(() => (list.value[active.value]?.code || '').replace(/\\n/g, '\n'))
</script>

<template>
  <div class="go-card">
    <div class="go-eyebrow">{{ title || '对照阅读' }}</div>
    <div class="go-tabs" style="margin-top:.6rem">
      <button
        v-for="(t, i) in list" :key="i" class="go-tab" :class="{ active: active === i }"
        @click="active = i"
      >{{ t.label }}</button>
    </div>
    <div class="go-panel">
      <pre style="background:var(--vp-code-block-bg);color:var(--vp-c-text-1);padding:.9rem 1rem;border-radius:10px;font-family:var(--vp-font-family-mono);font-size:13px;line-height:1.75;overflow-x:auto">{{ code }}</pre>
      <div v-if="list[active]?.note" class="go-callout note" style="font-size:13px;padding:.55rem .8rem">
        <span class="ico">📌</span><div>{{ list[active].note }}</div>
      </div>
    </div>
  </div>
</template>
