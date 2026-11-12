<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  tpl: string
  answers: string | string[]
  hint?: string
  explain?: string
  no?: string
}>()

const source = computed(() => String(props.tpl ?? '').replace(/\\n/g, '\n'))
const segments = computed(() => source.value.split('___'))
/** 兼容三种写法：:answers='[...]'（Vue 解析成数组）、JSON 字符串、"a|b" 分隔字符串。 */
const keys = computed<string[]>(() => {
  const v: any = props.answers ?? ''
  if (Array.isArray(v)) return v.map(String)
  const s = String(v)
  try {
    const p = JSON.parse(s)
    if (Array.isArray(p)) return p.map(String)
  } catch { /* 按 | 分隔处理 */ }
  return s.split('|').map((x) => x.trim())
})
const values = ref<string[]>([])
const checked = ref(false)
const showHint = ref(false)

function norm(s: string) { return s.replace(/\s+/g, '').toLowerCase() }
function okAt(i: number) {
  const want = keys.value[i] || ''
  const alts = want.split('~').map(norm)
  return alts.includes(norm(values.value[i] || ''))
}
const allOk = computed(() => keys.value.every((_, i) => okAt(i)))
function reset() { checked.value = false; values.value = [] }
</script>

<template>
  <div class="go-card" style="border-left:4px solid var(--go-green)">
    <div class="go-eyebrow" style="color:var(--go-green)">补全代码 · Fill in the blank</div>
    <div style="margin:.6rem 0 1rem;font-size:14.5px;font-weight:650">{{ '把 ___ 处补完整，让代码能通过编译并得到预期结果。' }}</div>
    <pre style="background:var(--vp-code-block-bg);color:var(--vp-c-text-1);padding:1rem;border-radius:10px;font-family:var(--vp-font-family-mono);font-size:13.5px;line-height:2.05;overflow-x:auto;white-space:pre"><template v-for="(seg, i) in segments" :key="i">{{ seg }}<input
        v-if="i < segments.length - 1"
        v-model="values[i]"
        type="text"
        :placeholder="'填第 ' + (i + 1) + ' 空'"
        :style="{ width: Math.max(6, (keys[i] || '').length + 3) + 'ch', border: checked ? (okAt(i) ? '1px solid var(--go-green)' : '1px solid var(--go-red)') : '1px dashed var(--go-brand)', background: 'var(--go-surface)', color: 'var(--go-brand)', borderRadius: '5px', padding: '2px 7px', font: 'inherit', textAlign: 'center', outline: 'none' }"
        :disabled="checked"
        @keyup.enter="!checked && (checked = true)"
      /></template></pre>
    <div class="q-foot" style="margin-top:.2rem">
      <button v-if="!checked" class="go-btn primary" @click="checked = true">检查答案</button>
      <template v-else>
        <button class="go-btn ghost" @click="reset">再写一次</button>
        <button v-if="hint" class="go-btn ghost" @click="showHint = !showHint">{{ showHint ? '收起提示' : '看提示' }}</button>
      </template>
    </div>
    <Transition name="go-fade">
      <div v-if="checked" class="go-feedback" :class="allOk ? 'ok' : 'no'" style="margin-top:.8rem">
        <b>{{ allOk ? '✓ 完全正确！' : '✗ 还有地方不对' }}</b>
        <span v-if="!allOk"> 参考答案：{{ keys.join(' / ') }}</span>
        <span v-if="explain"> {{ explain }}</span>
      </div>
    </Transition>
    <Transition name="go-fade">
      <div v-if="showHint && hint" class="go-callout idea" style="margin-top:.7rem">
        <span class="ico">💡</span><div>{{ hint }}</div>
      </div>
    </Transition>
  </div>
</template>
