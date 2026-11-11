<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vitepress'
import { LESSONS, isDone, normPath, setDone } from '../utils'

const props = defineProps<{
  stage?: string
  level?: string
  time?: string
  goal?: string
  prereq?: string
}>()

const route = useRoute()
const done = ref(false)
// 只有列入 LESSONS 的课程/导览页才显示打卡按钮，避免「打了卡却不计入进度」
const trackable = computed(() => {
  const p = normPath(route.path).replace(/\/$/, '')
  return LESSONS.some(l => l.path.replace(/\/$/, '') === p)
})
function sync() { done.value = isDone(route.path) }
function toggle() {
  setDone(route.path, !done.value)
  sync()
}
onMounted(() => {
  sync()
  window.addEventListener('go-progress-change', sync)
})
onUnmounted(() => {
  if (typeof window !== 'undefined') window.removeEventListener('go-progress-change', sync)
})
</script>

<template>
  <div class="go-stage">
    <div class="go-stage-chips">
      <span v-if="stage" class="chip"><b>{{ stage }}</b></span>
      <span v-if="level" class="chip">难度 {{ level }}</span>
      <span v-if="time" class="chip">约 {{ time }}</span>
      <span v-if="prereq" class="chip">前置：{{ prereq }}</span>
    </div>
    <div class="go-stage-right">
      <span v-if="goal" class="go-goal">🎯 {{ goal }}</span>
      <button
        v-if="trackable"
        class="go-btn" :class="done ? 'checked' : 'ghost'"
        :aria-pressed="done"
        style="padding:5px 12px;font-size:12px;white-space:nowrap"
        @click="toggle"
      >
        {{ done ? '✓ 已学完' : '标记学完' }}
      </button>
    </div>
  </div>
</template>
