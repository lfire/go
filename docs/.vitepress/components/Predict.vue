<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  q?: string
  options: string[] | string
  answer: string | number
  output?: string
  explain?: string
  no?: string
}>()

const opts = computed(() => {
  const v: any = props.options
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* ignore */ }
    return v.split('|').map(s => s.trim())
  }
  return []
})
const right = computed(() => Number(props.answer))
const letters = ['A', 'B', 'C', 'D', 'E']
const picked = ref<number | null>(null)
const shown = ref(false)
const ok = computed(() => picked.value === right.value)
function choose(i: number) { if (!shown.value) picked.value = i }
function reveal() { if (picked.value !== null) shown.value = true }
function reset() { picked.value = null; shown.value = false }
function pretty(s?: string) { return (s || '').replace(/\\n/g, '\n') }
</script>

<template>
  <div class="go-card go-predict">
    <div class="go-eyebrow" style="color:var(--go-blue)">先猜结果 · Prediction</div>
    <div class="q-head" style="margin-top:.35rem">
      <span v-if="no" class="q-no">{{ no }}</span>
      <span class="q-text">{{ q || '上面这段代码运行后会输出什么？' }}</span>
    </div>
    <div class="go-options">
      <button
        v-for="(o, i) in opts" :key="i" class="go-opt"
        :class="shown ? (i === right ? 'correct' : (i === picked ? 'wrong' : '')) : (picked === i ? 'selected' : '')"
        :disabled="shown" @click="choose(i)"
      >
        <span class="mark">{{ letters[i] || i + 1 }}</span>
        <span style="font-family:var(--vp-font-family-mono);font-size:13px">{{ o }}</span>
      </button>
    </div>
    <div class="q-foot">
      <button v-if="!shown" class="go-btn primary" :disabled="picked === null" @click="reveal">揭晓答案</button>
      <button v-else class="go-btn ghost" @click="reset">重新猜一次</button>
    </div>
    <Transition name="go-fade">
      <div v-if="shown" class="go-feedback" :class="ok ? 'ok' : 'no'" style="margin-top:.8rem">
        <b>{{ ok ? '✓ 猜对了！' : '✗ 结果是 ' + (letters[right] || '') }}</b>
        <div v-if="output" class="out">{{ pretty(output) }}</div>
        <div v-if="explain" style="margin-top:.6rem;color:var(--go-ink);font-size:13.5px">{{ explain }}</div>
      </div>
    </Transition>
  </div>
</template>
