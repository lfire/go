<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'
import ConceptDiagramSvg from './ConceptDiagramSvg.vue'

interface Node { x: number; y: number; w: number; h: number; label: string; sub?: string; tone?: string }
interface Arrow { x1: number; y1: number; x2: number; y2: number; label?: string }
interface Diagram { h: number; nodes: Node[]; arrows: Arrow[]; caption?: string }

const TONES: Record<string, string> = {
  orange: 'var(--go-brand)', blue: 'var(--go-blue)', green: 'var(--go-green)',
  purple: 'var(--go-purple)', red: 'var(--go-red)', gray: 'var(--go-muted)',
}

/**
 * 概念图数据（站点骨架里只留一张中性占位图）。
 * 编写课程时按 type 往这里追加条目即可，坐标以 680px 宽的 SVG viewBox 为基准：
 *   { h, nodes: [{ x, y, w, h, label, sub?, tone? }], arrows: [...], caption? }
 * 组件本身提供点击放大、滚轮 / 双指缩放、拖拽平移、ESC 关闭等交互，与数据无关。
 */
const D: Record<string, Diagram> = {
  placeholder: {
    h: 168,
    caption: '占位示意图：在这里替换为课程需要的节点与箭头即可。',
    nodes: [
      { x: 40, y: 48, w: 170, h: 62, label: '输入', sub: '从这里开始', tone: 'blue' },
      { x: 255, y: 48, w: 170, h: 62, label: '处理', sub: '你的逻辑', tone: 'purple' },
      { x: 470, y: 48, w: 170, h: 62, label: '输出', sub: '到这里结束', tone: 'green' },
    ],
    arrows: [
      { x1: 210, y1: 79, x2: 255, y2: 79 },
      { x1: 425, y1: 79, x2: 470, y2: 79 },
    ],
  },
}

const props = defineProps<{
  type?: string
  title?: string
  labels?: string
  caption?: string
}>()
const uid = useId().replace(/:/g, '')
const diagram = computed<Diagram>(() => {
  const base = D[props.type || 'placeholder'] || D['placeholder']
  if (!props.labels && !props.caption) return base
  let nodes = base.nodes
  if (props.labels) {
    const parts = props.labels.split('|').map((s) => s.trim())
    nodes = base.nodes.map((n, i) => {
      const raw = parts[i]
      if (!raw) return n
      const [label, sub] = raw.split('::').map((s) => s.trim())
      return { ...n, label, sub: sub || n.sub }
    })
  }
  return { ...base, nodes, caption: props.caption ?? base.caption }
})
/* 放大查看：滚轮无极缩放（以指针为锚点）+ 拖拽平移画布。
   画布使用固定的 680px 逻辑宽度（与 SVG viewBox 一致），缩放/平移全部由 transform 承担，
   这样窗口尺寸变化不会改变内容本身的坐标系，锚点缩放的计算也始终稳定。 */
const zoomOpen = ref(false)
const scale = ref(1)      // 绝对倍率：1 = 680px 逻辑宽度按 1:1 渲染
const fitScale = ref(1)   // 「适应窗口」倍率，同时作为缩放上下限与百分比的基准
const panX = ref(0)
const panY = ref(0)
const dragging = ref(false)
const viewport = ref<HTMLElement | null>(null)
const BASE_W = 680
const PAD = 28            // 画布四周留白
const MIN_RATIO = 0.4     // 相对适应窗口：最小缩到 40%
const MAX_RATIO = 8       // 相对适应窗口：最大放到 800%
const zoomLevel = computed(() => Math.round(scale.value / (fitScale.value || 1) * 100))
let prevOverflow = ''

const canvasStyle = computed(() => ({
  transform: `translate(${panX.value}px, ${panY.value}px) scale(${scale.value})`,
  transformOrigin: '0 0',
}))

/** 按视口与图的原始比例算出「整图完整可见」的倍率，并把画布居中。 */
function fitView() {
  const el = viewport.value
  if (!el) return
  const vw = el.clientWidth
  const vh = el.clientHeight
  const h = diagram.value.h || 200
  // 窄屏按宽度适配，固定 28px 留白会占掉太多，缩到视口的 3% 保证弹层里的图不小于正文
  const pad = Math.min(PAD, vw * 0.03)
  const s = Math.max(0.05, Math.min((vw - pad * 2) / BASE_W, (vh - pad * 2) / h))
  fitScale.value = s
  scale.value = s
  panX.value = (vw - BASE_W * s) / 2
  panY.value = (vh - h * s) / 2
}

/** 窗口尺寸变化时保持用户当前的相对倍率，只重算适配基准，不把画面突然打回 100%。 */
function onResize() {
  if (!zoomOpen.value) return
  const el = viewport.value
  if (!el) return
  const ratio = scale.value / (fitScale.value || 1)
  fitView()
  if (Math.abs(ratio - 1) > 0.001) {
    const r = el.getBoundingClientRect()
    zoomAt(r.left + r.width / 2, r.top + r.height / 2, ratio)
  }
}

function openZoom() {
  if (zoomOpen.value) return
  zoomOpen.value = true
  prevOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  // 等 flex 布局稳定后再算垂直居中，避免首帧 clientHeight 还没定下
  nextTick(() => requestAnimationFrame(() => requestAnimationFrame(fitView)))
}
function closeZoom() {
  if (!zoomOpen.value) return
  zoomOpen.value = false
  dragging.value = false
  pointers.clear()
  document.body.style.overflow = prevOverflow
  prevOverflow = ''
}

/** 以 (clientX, clientY) 为锚点缩放：锚点下的内容保持在原位置。 */
function zoomAt(clientX: number, clientY: number, factor: number) {
  const el = viewport.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const mx = clientX - rect.left
  const my = clientY - rect.top
  const cx = (mx - panX.value) / scale.value
  const cy = (my - panY.value) / scale.value
  const min = fitScale.value * MIN_RATIO
  const max = fitScale.value * MAX_RATIO
  const next = Math.min(max, Math.max(min, scale.value * factor))
  if (Math.abs(next - scale.value) < 0.0001) return
  scale.value = next
  panX.value = mx - cx * next
  panY.value = my - cy * next
}

function onWheel(e: WheelEvent) {
  // deltaY 越大缩得越多，用指数换算成连续倍率，触屏板的细小滚动也能平滑响应
  zoomAt(e.clientX, e.clientY, Math.exp(-e.deltaY * 0.0015))
}
function zoomByStep(factor: number) {
  const el = viewport.value
  if (!el) return
  const r = el.getBoundingClientRect()
  zoomAt(r.left + r.width / 2, r.top + r.height / 2, factor)
}
function onDblClick() { fitView() }

/** 单指拖拽平移；双指捏合缩放（touch-action:none 会禁用浏览器原生缩放，必须自己实现）。 */
interface Pointer { id: number; x: number; y: number }
const pointers = new Map<number, Pointer>()
let dragStart = { x: 0, y: 0, px: 0, py: 0, id: -1 }
let pinchStart = { dist: 1, mx: 0, my: 0 }

function startDrag(p: Pointer) {
  dragStart = { x: p.x, y: p.y, px: panX.value, py: panY.value, id: p.id }
}
function pinchState() {
  const [a, b] = [...pointers.values()]
  return {
    dist: Math.max(1, Math.hypot(a.x - b.x, a.y - b.y)),
    mx: (a.x + b.x) / 2,
    my: (a.y + b.y) / 2,
  }
}
function onPointerDown(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  try { (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId) } catch { /* 非活跃指针时忽略 */ }
  pointers.set(e.pointerId, { id: e.pointerId, x: e.clientX, y: e.clientY })
  if (pointers.size === 1) {
    dragging.value = true
    startDrag([...pointers.values()][0])
  } else if (pointers.size === 2) {
    dragging.value = false
    pinchStart = pinchState()
  }
}
function onPointerMove(e: PointerEvent) {
  if (!pointers.has(e.pointerId)) return
  pointers.set(e.pointerId, { id: e.pointerId, x: e.clientX, y: e.clientY })
  if (pointers.size >= 2) {
    const now = pinchState()
    zoomAt(now.mx, now.my, now.dist / pinchStart.dist)
    // 捏合的同时两指整体移动，也要跟着平移，观感才跟手
    panX.value += now.mx - pinchStart.mx
    panY.value += now.my - pinchStart.my
    pinchStart = now
    return
  }
  if (!dragging.value || e.pointerId !== dragStart.id) return
  panX.value = dragStart.px + (e.clientX - dragStart.x)
  panY.value = dragStart.py + (e.clientY - dragStart.y)
}
function onPointerUp(e: PointerEvent) {
  pointers.delete(e.pointerId)
  const rest = [...pointers.values()]
  if (rest.length === 1) {
    // 双指变成单指时无缝接着拖，避免画面跳一下
    dragging.value = true
    startDrag(rest[0])
  } else if (rest.length === 0) {
    dragging.value = false
    dragStart.id = -1
  }
}

function onKeydown(e: KeyboardEvent) {
  if (!zoomOpen.value) return
  if (e.key === 'Escape') closeZoom()
  else if (e.key === '+' || e.key === '=') zoomByStep(1.2)
  else if (e.key === '-' || e.key === '_') zoomByStep(1 / 1.2)
  else if (e.key === '0') fitView()
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onResize)
  if (zoomOpen.value) document.body.style.overflow = prevOverflow
})
</script>

<template>
  <figure class="go-figure">
    <div class="go-fig-head">
      <div v-if="title" class="go-eyebrow">{{ title }}</div>
      <span v-else />
      <button class="go-zoom-btn" type="button" :aria-label="`放大查看：${title || '概念图'}`" @click="openZoom">
        ⤢ 放大查看
      </button>
    </div>

    <div class="go-fig-frame" role="button" tabindex="0"
      :aria-label="`点击放大查看：${title || '概念图'}`"
      @click="openZoom" @keydown.enter.prevent="openZoom" @keydown.space.prevent="openZoom">
      <ConceptDiagramSvg :diagram="diagram" :uid="uid" />
    </div>

    <figcaption v-if="diagram.caption" class="go-fig-caption">{{ diagram.caption }}</figcaption>

    <Teleport to="body">
      <div v-if="zoomOpen" class="go-lightbox" role="dialog" aria-modal="true"
        :aria-label="title || '放大查看概念图'" @click.self="closeZoom">
        <div class="go-lightbox-panel">
          <div class="go-lightbox-head">
            <b>{{ title || '概念图' }}</b>
            <div class="go-lightbox-actions">
              <button class="go-btn ghost" type="button" aria-label="缩小" @click="zoomByStep(1 / 1.2)">−</button>
              <span class="go-zoom-level" aria-live="polite">{{ zoomLevel }}%</span>
              <button class="go-btn ghost" type="button" aria-label="放大" @click="zoomByStep(1.2)">＋</button>
              <button class="go-btn ghost" type="button" @click="fitView">适应窗口</button>
              <button class="go-btn ghost" type="button" aria-label="关闭放大" @click="closeZoom">✕ 关闭</button>
            </div>
          </div>
          <div ref="viewport" class="go-lightbox-body" :class="{ dragging }"
            @wheel.prevent="onWheel" @pointerdown="onPointerDown" @pointermove="onPointerMove"
            @pointerup="onPointerUp" @pointercancel="onPointerUp" @dblclick="onDblClick">
            <div class="go-zoom-canvas" :style="canvasStyle">
              <ConceptDiagramSvg :diagram="diagram" :uid="uid + '-zoom'" />
            </div>
          </div>
          <div class="go-lightbox-foot">
            <span v-if="diagram.caption">{{ diagram.caption }} · </span>100% 为适应窗口 · 滚轮 / 双指捏合缩放 · 拖拽平移 · 双击或 0 键复位 · ESC 关闭
          </div>
        </div>
      </div>
    </Teleport>
  </figure>
</template>
