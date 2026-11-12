<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  title?: string
  task: string
  hints?: string
  solution?: string
  explain?: string
}>()

const hintList = computed(() => {
  const v: any = props.hints
  if (!v) return []
  if (Array.isArray(v)) return v.map(String)
  try { const p = JSON.parse(String(v)); if (Array.isArray(p)) return p.map(String) } catch { /* ignore */ }
  return String(v).split('|').map(s => s.trim()).filter(Boolean)
})
const opened = ref(true)
const hintAt = ref(0)
const showSolution = ref(false)
const code = computed(() => String(props.solution ?? '').replace(/\\n/g, '\n'))
</script>

<template>
  <div class="go-exercise" style="margin:1.4rem 0">
    <div class="go-ex-head" @click="opened = !opened">
      <span>🏋️ {{ title || '动手练习' }}</span>
      <span style="font-size:12px;color:var(--go-muted)">{{ opened ? '收起 ▲' : '展开 ▼' }}</span>
    </div>
    <div v-if="opened" class="go-ex-body">
      <div style="font-size:14px;line-height:1.8" v-html="task.replace(/\\n/g,'<br>')"></div>
      <div class="q-foot" style="margin-top:.9rem;flex-wrap:wrap">
        <button v-if="hintList.length" class="go-btn ghost" :disabled="hintAt >= hintList.length" @click="hintAt++">
          {{ hintAt >= hintList.length ? '提示已全部给出' : `给点提示 (${hintAt}/${hintList.length})` }}
        </button>
        <button v-if="solution" class="go-btn ghost" @click="showSolution = !showSolution">{{ showSolution ? '收起参考实现' : '查看参考实现' }}</button>
      </div>
      <div v-for="(h, i) in hintList" :key="i">
        <Transition name="go-fade">
          <div v-if="i < hintAt" class="go-callout idea" style="font-size:13px;padding:.55rem .8rem;margin:.5rem 0">
            <span class="ico">💡</span><div><b>提示 {{ i + 1 }}：</b>{{ h }}</div>
          </div>
        </Transition>
      </div>
      <Transition name="go-fade">
        <div v-if="solution && showSolution">
          <pre class="go-ex-solution">{{ code }}</pre>
        </div>
      </Transition>
      <div v-if="explain" class="go-sub" style="margin-top:.6rem">{{ explain }}</div>
    </div>
  </div>
</template>
