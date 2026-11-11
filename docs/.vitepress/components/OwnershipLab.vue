<script setup lang="ts">
import { computed, ref, watch } from 'vue'

interface Cell { name: string; value: string; state?: 'moved' | 'borrowed' | 'dropped' }
interface Step { line: string; stack: Cell[]; heap: Cell[]; log: string; err?: string }

/**
 * 逐语句状态演示台（与具体语言无关）。
 * 站点骨架里只留一个中性占位场景；编写课程时把 SCENES 换成需要演示的过程即可，
 * 例如 slice 扩容、map 写入、goroutine 与 channel 的状态变化。
 * 组件的「切换场景 / 上一步 / 下一步 / 从头看」交互与数据完全解耦。
 */
const SCENES: Record<string, { label: string; desc: string; steps: Step[] }> = {
  placeholder: {
    label: '示例场景', desc: '占位数据：把这里替换成课程里需要逐步演示的过程。',
    steps: [
      { line: '// 第 1 步', stack: [], heap: [], log: '这是第一步的说明文字。' },
      { line: '// 第 2 步', stack: [{ name: 'a', value: '1' }], heap: [], log: '变量出现在左侧面板。' },
      { line: '// 第 3 步', stack: [{ name: 'a', value: '1' }], heap: [{ name: '0x01', value: '数据' }], log: '右侧面板展示与之关联的数据。' },
    ],
  },
}

const props = defineProps<{ scene?: string }>()
const current = ref(props.scene && SCENES[props.scene] ? props.scene : 'placeholder')
const idx = ref(0)
const scene = computed(() => SCENES[current.value])
const step = computed(() => scene.value.steps[Math.min(idx.value, scene.value.steps.length - 1)])
function pick(k: string) { current.value = k; idx.value = 0 }
function next() { if (idx.value < scene.value.steps.length - 1) idx.value++ }
function prev() { if (idx.value > 0) idx.value-- }
watch(current, () => { idx.value = 0 })
</script>

<template>
  <div class="go-card" style="padding:1rem">
    <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:.6rem">
      <div>
        <div class="go-eyebrow" style="color:var(--go-purple)">交互实验台 · Step Lab</div>
        <div class="go-title">一步一步看懂状态怎么变化</div>
      </div>
      <span class="go-sub">{{ idx + 1 }} / {{ scene.steps.length }}</span>
    </div>
    <div style="display:flex;gap:.35rem;flex-wrap:wrap;margin:.85rem 0 .5rem">
      <button v-for="(s, k) in SCENES" :key="k" class="go-btn ghost"
        :style="{fontSize:'12px',padding:'5px 10px',borderColor: current===k ? 'var(--go-brand)' : 'var(--go-line)', color: current===k ? 'var(--go-brand)' : 'var(--go-ink)'} "
        @click="pick(String(k))">{{ s.label }}</button>
    </div>
    <div class="go-sub" style="margin-bottom:.7rem">{{ scene.desc }}</div>
    <div class="go-lab">
      <div class="go-lab-head">
        <span style="font-size:12.5px;font-weight:700">当前执行到</span>
        <span style="font-family:var(--vp-font-family-mono);font-size:12px;color:var(--go-brand)">{{ idx + 1 }} / {{ scene.steps.length }}</span>
      </div>
      <pre style="margin:0;padding:.85rem 1rem;background:var(--vp-code-block-bg);color:var(--vp-c-text-1);font-family:var(--vp-font-family-mono);font-size:12.8px;line-height:1.85;overflow-x:auto"><template v-for="(st, i) in scene.steps" :key="i"><span :style="{opacity: i <= idx ? 1 : .28, color: i === idx ? 'var(--go-brand)' : 'inherit', fontWeight: i === idx ? 700 : 400}">{{ st.line }}</span>{{ '\n' }}</template></pre>
      <div class="go-lab-canvas">
        <div class="go-pane">
          <div class="go-pane-title">状态 A（变量）</div>
          <div v-if="!step.stack.length" class="go-sub" style="font-size:12px">空</div>
          <div v-for="(c, i) in step.stack" :key="i" class="go-cell" :class="c.state">
            <code>{{ c.name }}</code><span :class="{'go-arrow': !c.state}">{{ c.value }}</span>
          </div>
        </div>
        <div class="go-pane">
          <div class="go-pane-title">状态 B（数据）</div>
          <div v-if="!step.heap.length" class="go-sub" style="font-size:12px">空</div>
          <div v-for="(c, i) in step.heap" :key="i" class="go-cell" :class="c.state">
            <code>{{ c.name }}</code><span :class="{'go-arrow': !c.state}">{{ c.value }}</span>
          </div>
        </div>
      </div>
      <div style="padding:0 1.1rem 1rem">
        <div class="go-lab-log">{{ step.log }}</div>
        <div v-if="step.err" class="go-feedback no" style="margin-top:.55rem;font-family:var(--vp-font-family-mono);font-size:12px">{{ step.err }}</div>
      </div>
    </div>
    <div class="q-foot" style="margin-top:.9rem;flex-wrap:wrap">
      <button class="go-btn" :disabled="idx === 0" @click="prev">← 上一步</button>
      <button class="go-btn primary" :disabled="idx >= scene.steps.length - 1" @click="next">执行下一步 →</button>
      <button class="go-btn ghost" @click="idx = 0">从头看</button>
    </div>
  </div>
</template>
