<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'

/**
 * 通用图示放大容器：缩略态按容器等比缩放，点击后全屏查看。
 * 与概念图保持完全一致的交互：滚轮 / 双指缩放、拖拽平移、双击与 0 键复位、ESC 关闭。
 * 内容通过 #figure 作用域插槽提供，会渲染两次（缩略 + 放大），uid 不同以避免 SVG marker id 冲突。
 */
const props = withDefaults(defineProps<{
  title?: string
  caption?: string
  label?: string
  w: number
  h: number
}>(), { label: '图示' })

const uid = useId().replace(/:/g, '')
const zoomOpen = ref(false)
const scale = ref(1)
const fitScale = ref(1)
const panX = ref(0)
const panY = ref(0)
const dragging = ref(false)
const viewport = ref<HTMLElement | null>(null)
const PAD = 28
const MIN_RATIO = 0.4
const MAX_RATIO = 8
const zoomLevel = computed(() => Math.round(scale.value / (fitScale.value || 1) * 100))
let prevOverflow = ''

const canvasStyle = computed(() => ({
  transform: `translate(${panX.value}px, ${panY.value}px) scale(${scale.value})`,
  transformOrigin: '0 0',
  width: `${props.w}px`,
  height: `${props.h}px`,
}))

/** 按视口与图的原始比例算出「整图完整可见」的倍率，并把画布居中。 */
function fitView() {
  const el = viewport.value
  if (!el) return
  const vw = el.clientWidth
  const vh = el.clientHeight
  const pad = Math.min(PAD, vw * 0.03)
  const s = Math.max(0.05, Math.min((vw - pad * 2) / props.w, (vh - pad * 2) / props.h))
  fitScale.value = s
  scale.value = s
  panX.value = (vw - props.w * s) / 2
  panY.value = (vh - props.h * s) / 2
}

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
function onWheel(e: WheelEvent) { zoomAt(e.clientX, e.clientY, Math.exp(-e.deltaY * 0.0015)) }
function zoomByStep(factor: number) {
  const el = viewport.value
  if (!el) return
  const r = el.getBoundingClientRect()
  zoomAt(r.left + r.width / 2, r.top + r.height / 2, factor)
}
function onDblClick() { fitView() }

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
  try { (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId) } catch { /* ignore */ }
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
      <button class="go-zoom-btn" type="button" :aria-label="`放大查看：${title || label}`" @click="openZoom">
        ⤢ 放大查看
      </button>
    </div>

    <div class="go-fig-frame" role="button" tabindex="0"
      :aria-label="`点击放大查看：${title || label}`"
      @click="openZoom" @keydown.enter.prevent="openZoom" @keydown.space.prevent="openZoom">
      <slot name="figure" :uid="uid" />
    </div>

    <figcaption v-if="caption" class="go-fig-caption">{{ caption }}</figcaption>
    <slot name="footer" />

    <Teleport to="body">
      <div v-if="zoomOpen" class="go-lightbox" role="dialog" aria-modal="true"
        :aria-label="title || `放大查看${label}`" @click.self="closeZoom">
        <div class="go-lightbox-panel">
          <div class="go-lightbox-head">
            <b>{{ title || label }}</b>
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
              <slot name="figure" :uid="uid + '-zoom'" />
            </div>
          </div>
          <div class="go-lightbox-foot">
            <span v-if="caption">{{ caption }} · </span>100% 为适应窗口 · 滚轮 / 双指捏合缩放 · 拖拽平移 · 双击或 0 键复位 · ESC 关闭
          </div>
        </div>
      </div>
    </Teleport>
  </figure>
</template>
