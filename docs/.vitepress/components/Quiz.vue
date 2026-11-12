<script setup lang="ts">
import { computed, ref } from 'vue'
import RichText from './RichText.vue'

const props = defineProps<{
  q: string
  options: string[] | string
  answer: string | number
  explain?: string
  multi?: boolean | string
  no?: string
}>()

function toArray(v: unknown): string[] {
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* ignore */ }
    return v.split('|').map(s => s.trim())
  }
  return []
}
function toAnswers(v: unknown): number[] {
  if (Array.isArray(v)) return v.map(Number)
  return String(v ?? '').split(',').map(s => Number(s.trim())).filter(n => !Number.isNaN(n))
}

const opts = computed(() => toArray(props.options))
const answers = computed(() => toAnswers(props.answer).sort((a, b) => a - b))
const isMulti = computed(() => props.multi === true || props.multi === 'true' || answers.value.length > 1)
const letters = ['A', 'B', 'C', 'D', 'E', 'F', 'G']

const picked = ref<number[]>([])
const submitted = ref(false)
const correct = computed(() =>
  picked.value.length === answers.value.length &&
  [...picked.value].sort((a, b) => a - b).every((v, i) => v === answers.value[i])
)

function toggle(i: number) {
  if (submitted.value) return
  if (!isMulti.value) picked.value = [i]
  else picked.value = picked.value.includes(i) ? picked.value.filter(x => x !== i) : [...picked.value, i]
}
function stateOf(i: number) {
  if (!submitted.value) return picked.value.includes(i) ? 'selected' : ''
  if (answers.value.includes(i)) return 'correct'
  return picked.value.includes(i) ? 'wrong' : ''
}
function submit() { if (picked.value.length) submitted.value = true }
function reset() { submitted.value = false; picked.value = [] }
</script>

<template>
  <div class="go-card go-quiz">
    <div class="go-eyebrow">随堂检测 · {{ isMulti ? '多选题' : '单选题' }}</div>
    <div class="q-head" style="margin-top:.35rem">
      <span v-if="no" class="q-no">{{ no }}</span>
      <span class="q-text"><RichText :text="q" /></span>
    </div>
    <div class="go-options">
      <button
        v-for="(o, i) in opts" :key="i" class="go-opt" :class="stateOf(i)"
        :disabled="submitted" @click="toggle(i)"
      >
        <span class="mark">{{ letters[i] || i + 1 }}</span>
        <span>{{ o }}</span>
      </button>
    </div>
    <div class="q-foot">
      <button v-if="!submitted" class="go-btn primary" :disabled="!picked.length" @click="submit">
        提交答案
      </button>
      <button v-else class="go-btn ghost" @click="reset">再试一次</button>
      <span v-if="!submitted && !picked.length" class="go-sub">选一个选项开始</span>
    </div>
    <Transition name="go-fade">
      <div v-if="submitted" class="go-feedback" :class="correct ? 'ok' : 'no'">
        <b>{{ correct ? '✓ 答对了！' : '✗ 再想想～' }}</b>
        <span v-if="!correct"> 正确选项：{{ answers.map(i => letters[i] || i + 1).join('、') }}。</span>
        <span v-if="explain"> <RichText :text="explain" /></span>
      </div>
    </Transition>
  </div>
</template>
