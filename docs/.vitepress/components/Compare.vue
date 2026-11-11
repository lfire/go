<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ panels: string | any[]; title?: string; note?: string }>()
interface Panel { label: string; code: string; note?: string }
const list = computed<Panel[]>(() => {
  const v: any = props.panels
  if (Array.isArray(v)) return v
  if (typeof v === 'string') { try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { /* ignore */ } }
  return []
})
function pretty(s?: string) { return (s || '').replace(/\\n/g, '\n') }
</script>

<template>
  <div class="go-card">
    <div class="go-eyebrow" style="color:var(--go-blue)">{{ title || '和熟悉的语言比一比' }}</div>
    <div style="display:grid;gap:.8rem;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));margin-top:.85rem">
      <div v-for="(p, i) in list" :key="i" style="border:1px solid var(--go-line);border-radius:10px;overflow:hidden">
        <div style="padding:.5rem .8rem;background:var(--go-soft);font-size:12.5px;font-weight:750;border-bottom:1px solid var(--go-line)">{{ p.label }}</div>
        <pre style="margin:0;background:var(--vp-code-block-bg);color:var(--vp-c-text-1);padding:.8rem .9rem;font-family:var(--vp-font-family-mono);font-size:12.5px;line-height:1.75;overflow-x:auto;min-height:6em">{{ pretty(p.code) }}</pre>
        <div v-if="p.note" style="padding:.5rem .8rem;font-size:12px;color:var(--go-muted);line-height:1.6">{{ p.note }}</div>
      </div>
    </div>
    <div v-if="note" class="go-callout idea" style="font-size:13px;margin:.8rem 0 0;padding:.55rem .8rem">
      <span class="ico">💡</span><div>{{ note }}</div>
    </div>
  </div>
</template>
