<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  tpl: string
  answers: string | string[]
  hint?: string
  explain?: string
  /** 逐空解析：数组第 i 项对应第 i 个空；也可传 JSON 字符串或 | 分隔字符串 */
  explains?: string | string[]
  no?: string
}>()

function toArray(v: any): string[] {
  if (Array.isArray(v)) return v.map(String)
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p.map(String) } catch { /* 当作 | 分隔 */ }
    return v.split('|').map((x) => x.trim())
  }
  return []
}

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
const perBlank = computed<string[]>(() => toArray(props.explains))
const blankExplain = (i: number) => perBlank.value[i] || ''
/** 每个空的主答案与可接受的其它写法 */
const altsOf = (i: number) =>
  String(keys.value[i] || '').split(/[~|]/).map((x) => x.trim()).filter(Boolean)
const primary = (i: number) => altsOf(i)[0] || '—'
const alternatives = (i: number) => altsOf(i).slice(1)

const values = ref<string[]>([])
const checked = ref(false)
const showHint = ref(false)

function norm(s: string) { return s.replace(/\s+/g, '').toLowerCase() }
function okAt(i: number) {
  return altsOf(i).map(norm).includes(norm(values.value[i] || ''))
}
const allOk = computed(() => keys.value.every((_, i) => okAt(i)))
function reset() { checked.value = false; values.value = [] }

/** 中文按 1em、英文数字按 0.6em 估算视觉宽度。 */
function visualEm(s: string): number {
  let w = 0
  for (const ch of s) w += ch.charCodeAt(0) > 0x2e80 ? 1 : 0.6
  return w
}
/** 输入框要同时容得下「填第 N 空」占位文案与参考答案，避免 placeholder 被截断。 */
function inputWidth(i: number): string {
  const ph = `填第 ${i + 1} 空`
  const longest = altsOf(i).reduce((m, a) => Math.max(m, visualEm(a)), 0)
  const want = Math.max(visualEm(ph), longest)
  return `${Math.max(6.2, want + 1.4).toFixed(2)}em` // 余量覆盖 padding + 边框
}
</script>

<template>
  <div class="go-card" style="border-left:4px solid var(--go-green)">
    <div class="go-eyebrow" style="color:var(--go-green)">补全代码 · Fill in the blank</div>
    <div class="q-head" style="margin:.5rem 0 1rem">
      <span v-if="no" class="q-no">{{ no }}</span>
      <span class="q-text">把 ___ 处补完整，让代码能通过编译并得到预期结果。</span>
    </div>
    <pre class="fb-tpl"><template v-for="(seg, i) in segments" :key="i">{{ seg }}<input
        v-if="i < segments.length - 1"
        v-model="values[i]"
        type="text"
        :placeholder="'填第 ' + (i + 1) + ' 空'"
        :style="{ width: inputWidth(i), border: checked ? (okAt(i) ? '1px solid var(--go-green)' : '1px solid var(--go-red)') : '1px dashed var(--go-brand)' }"
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
      <div v-if="checked" class="fb-review">
        <div class="fb-review-head" :class="allOk ? 'ok' : 'no'">
          <b>{{ allOk ? '✓ 完全正确！' : '✗ 还有地方不对' }}</b>
          <span>下面按空对照答案与解析</span>
        </div>
        <div class="fb-review-list">
          <div v-for="(_, i) in keys" :key="i" class="fb-review-item" :class="okAt(i) ? 'ok' : 'no'">
            <div class="fb-item-head">
              <span class="fb-item-no">第 {{ i + 1 }} 空</span>
              <span class="fb-item-state">{{ okAt(i) ? '✓ 答对' : '✗ 待订正' }}</span>
            </div>
            <div class="fb-item-answer">
              <span class="fb-tag">答案</span>
              <code>{{ primary(i) }}</code>
              <span v-if="alternatives(i).length" class="fb-alts">也可写：{{ alternatives(i).join('、') }}</span>
              <span class="fb-yours">你填的：<em>{{ values[i] || '（未填）' }}</em></span>
            </div>
            <div class="fb-item-explain">
              <span class="fb-tag muted">解析</span>
              <p>{{ blankExplain(i) || '见下方整体说明。' }}</p>
            </div>
          </div>
        </div>
        <div v-if="explain" class="fb-overall">
          <b>整体说明</b>
          <p>{{ explain }}</p>
        </div>
      </div>
    </Transition>

    <Transition name="go-fade">
      <div v-if="showHint && hint" class="go-callout idea" style="margin-top:.7rem">
        <span class="ico">💡</span><div>{{ hint }}</div>
      </div>
    </Transition>
  </div>
</template>
