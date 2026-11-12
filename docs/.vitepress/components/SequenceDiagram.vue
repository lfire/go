<script setup lang="ts">
import { computed } from 'vue'
import FigureLightbox from './FigureLightbox.vue'

/**
 * 时序图：按参与方数量生成「舒适宽度」的画布，不为了塞进正文栏而把内容挤在一起。
 * 缩略态等比缩放显示在当前位置；点击（或右上角按钮）用 FigureLightbox 放大查看，
 * 与概念图一样支持滚轮 / 双指缩放、拖拽平移、ESC 关闭。
 */
interface Step {
  from: string
  to: string
  label: string
  note?: string
  kind?: 'call' | 'return' | 'self' | 'async'
}
const props = defineProps<{
  title?: string
  actors: string | string[]
  steps: string | Step[]
  caption?: string
}>()

function toArray(v: any): any[] {
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { return v.split('|').map(s => s.trim()) }
  }
  return []
}
const actors = computed<string[]>(() => toArray(props.actors).map(String))
const steps = computed<Step[]>(() => toArray(props.steps) as Step[])

/* ---- 画布尺寸：每个参与方固定宽度 + 明确的间隙，绝不让方框贴在一起 ---- */
const PAD_X = 72          // 左右留白，也给最右侧自处理消息的注释留位置
const BOX_W = 132         // 参与方框宽
const GAP_MIN = 30        // 参与方框之间最小间隙
const TOP = 88
const ROW = 76            // 每条消息独占一行，标签在上、注释在下，互不重叠
const W = computed(() =>
  Math.max(600, PAD_X * 2 + actors.value.length * BOX_W + Math.max(0, actors.value.length - 1) * GAP_MIN),
)
const H = computed(() => TOP + steps.value.length * ROW + 36)
const gap = computed(() => {
  const n = actors.value.length
  return n > 1 ? (W.value - PAD_X * 2 - n * BOX_W) / (n - 1) : 0
})
const xs = computed(() =>
  actors.value.map((_, i) => Math.round(PAD_X + BOX_W / 2 + i * (BOX_W + gap.value))),
)
function idx(name: string): number {
  const i = actors.value.indexOf(String(name))
  return i < 0 ? 0 : i
}
function yOf(i: number) { return TOP + i * ROW + 30 }

const COLORS: Record<string, string> = {
  call: 'var(--go-muted)', return: 'var(--go-green)',
  self: 'var(--go-purple)', async: 'var(--go-blue)',
}
function color(s: Step) { return COLORS[s.kind || 'call'] || COLORS.call }
function isSelf(s: Step) { return s.kind === 'self' || (s.from === s.to && s.kind !== 'return') }
function midX(s: Step) { return (xs.value[idx(s.from)] + xs.value[idx(s.to)]) / 2 }

/** 中文按全角、英文按半角估算；只在确实过长时轻微缩小，下限 10.5px（细节交给放大查看）。 */
function fit(text: string, avail: number, base: number): number {
  let w = 0
  for (const ch of text) w += ch.charCodeAt(0) > 0x2e80 ? base : base * 0.56
  return w > avail ? Math.max(10.5, base * (avail / w)) : base
}
function labelSize(s: Step) {
  const avail = Math.max(120, Math.abs(xs.value[idx(s.to)] - xs.value[idx(s.from)]) - 16)
  return fit(s.label, avail, 14.5)
}
function noteSize(s: Step) {
  const avail = Math.max(190, Math.abs(xs.value[idx(s.to)] - xs.value[idx(s.from)]) + 130)
  return fit(s.note || '', avail, 12.5)
}
</script>

<template>
  <FigureLightbox class="sd-figure" :w="W" :h="H" :title="title" :caption="caption" label="时序图">
    <template #figure="{ uid }">
      <svg :viewBox="`0 0 ${W} ${H}`" role="img"
        style="width:100%;height:auto;display:block">
        <defs>
          <marker v-for="(v, k) in COLORS" :key="k" :id="`sd-${uid}-${k}`"
            markerWidth="10" markerHeight="10" refX="9" refY="5" orient="auto">
            <path d="M0 0 L10 5 L0 10 z" :fill="v" />
          </marker>
        </defs>

        <!-- 参与方与生命线 -->
        <g v-for="(a, i) in actors" :key="'a' + i">
          <rect :x="xs[i] - BOX_W / 2" y="14" :width="BOX_W" height="42" rx="10"
            fill="var(--go-brand-soft)" stroke="var(--go-brand)" stroke-width="1.3" />
          <text :x="xs[i]" y="41" text-anchor="middle" font-size="16" font-weight="700"
            fill="var(--go-ink)">{{ a }}</text>
          <line :x1="xs[i]" y1="58" :x2="xs[i]" :y2="H - 22"
            stroke="var(--go-line)" stroke-width="1.4" stroke-dasharray="5 6" />
        </g>

        <!-- 消息 -->
        <g v-for="(s, i) in steps" :key="'s' + i">
          <text x="26" :y="yOf(i) + 5" font-size="12" fill="var(--go-muted)">{{ i + 1 }}</text>

          <template v-if="isSelf(s)">
            <path :d="`M ${xs[idx(s.from)]} ${yOf(i)} h 44 v 20 h -44`"
              fill="none" :stroke="color(s)" stroke-width="1.8"
              :marker-end="`url(#sd-${uid}-self)`" />
            <text :x="xs[idx(s.from)] + 22" :y="yOf(i) - 16" text-anchor="middle"
              :font-size="labelSize(s)" fill="var(--go-ink)">{{ s.label }}</text>
            <text v-if="s.note" :x="xs[idx(s.from)] + 22" :y="yOf(i) + 42"
              text-anchor="middle" :font-size="noteSize(s)" fill="var(--go-muted)">{{ s.note }}</text>
          </template>

          <template v-else>
            <line :x1="xs[idx(s.from)]" :y1="yOf(i)" :x2="xs[idx(s.to)]" :y2="yOf(i)"
              :stroke="color(s)" stroke-width="1.9"
              :stroke-dasharray="s.kind === 'return' ? '6 5' : undefined"
              :marker-end="`url(#sd-${uid}-${s.kind || 'call'})`" />
            <text :x="midX(s)" :y="yOf(i) - 16" text-anchor="middle"
              :font-size="labelSize(s)" fill="var(--go-ink)">{{ s.label }}</text>
            <text v-if="s.note" :x="midX(s)" :y="yOf(i) + 26" text-anchor="middle"
              :font-size="noteSize(s)" fill="var(--go-muted)">{{ s.note }}</text>
          </template>
        </g>
      </svg>
    </template>

    <template #footer>
      <div class="sd-legend">
        <span><i class="call"></i>同步调用</span>
        <span><i class="return"></i>返回</span>
        <span><i class="self"></i>自处理</span>
        <span><i class="async"></i>异步消息</span>
        <span class="sd-zoom-hint">⤢ 点击图可放大查看全部细节</span>
      </div>
    </template>
  </FigureLightbox>
</template>
