<script setup lang="ts">
import { computed } from 'vue'

/** 概念图的 SVG 本体，正文与放大弹层共用同一份渲染，保证两处完全一致。 */
interface Node { x: number; y: number; w: number; h: number; label: string; sub?: string; tone?: string }
interface Arrow { x1: number; y1: number; x2: number; y2: number; label?: string }
interface Diagram { h: number; nodes: Node[]; arrows: Arrow[]; caption?: string }

const props = defineProps<{ diagram: Diagram; uid: string }>()

const TONES: Record<string, string> = {
  orange: 'var(--go-brand)', blue: 'var(--go-blue)', green: 'var(--go-green)',
  purple: 'var(--go-purple)', red: 'var(--go-red)', gray: 'var(--go-muted)',
}
function tone(t?: string) { return TONES[t || 'gray'] || TONES.gray }

/** 粗略估算文本宽度，超出方框时按可用宽度缩小字号，避免居中后溢出。 */
function visualWidth(s: string, base: number): number {
  let w = 0
  for (const ch of s) w += ch.charCodeAt(0) > 0x2e80 ? base : base * 0.56
  return w
}
function fitSize(text: string, boxWidth: number, base: number): number {
  const avail = Math.max(40, boxWidth - 16)
  const need = visualWidth(text, base)
  return need > avail ? Math.max(9, base * (avail / need)) : base
}

/**
 * 自动水平居中：按所有节点与箭头的实际包围盒计算平移量，让整组内容在 680 宽的
 * viewBox 里左右留白相等。这样无需为每张图手工配平坐标（早期某张图原本
 * 节点只占到 x=590，右边比左边多留 70px）。
 */
const offsetX = computed(() => {
  const d = props.diagram
  if (!d.nodes.length) return 0
  const xs = [
    ...d.nodes.flatMap(n => [n.x, n.x + n.w]),
    ...d.arrows.flatMap(a => [a.x1, a.x2]),
  ]
  const minX = Math.min(...xs)
  const maxX = Math.max(...xs)
  return Math.round(((680 - (maxX - minX)) / 2 - minX) * 10) / 10
})

/** 垂直方向同理：不少早期图 h 留得偏大、节点固定从 y=55 起，导致下方一大片空白。 */
const offsetY = computed(() => {
  const d = props.diagram
  if (!d.nodes.length) return 0
  const ys = [
    ...d.nodes.flatMap(n => [n.y, n.y + n.h]),
    ...d.arrows.flatMap(a => [a.y1, a.y2]),
  ]
  const minY = Math.min(...ys)
  const maxY = Math.max(...ys)
  return Math.round(((d.h - (maxY - minY)) / 2 - minY) * 10) / 10
})
</script>

<template>
  <svg :viewBox="`0 0 680 ${diagram.h}`" role="img"
    style="width:100%;height:auto;display:block">
    <defs>
      <marker :id="`ar-${uid}`" markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto">
        <path d="M0 0 L9 4.5 L0 9 z" fill="var(--go-muted)" />
      </marker>
    </defs>
    <g :transform="`translate(${offsetX} ${offsetY})`">
      <g v-for="(a, i) in diagram.arrows" :key="'a' + i">
        <line :x1="a.x1" :y1="a.y1" :x2="a.x2" :y2="a.y2" stroke="var(--go-muted)" stroke-width="1.6"
          stroke-dasharray="4 3" :marker-end="`url(#ar-${uid})`" />
        <text v-if="a.label" :x="(a.x1 + a.x2) / 2" :y="(a.y1 + a.y2) / 2 - 7"
          text-anchor="middle" font-size="11" fill="var(--go-muted)">{{ a.label }}</text>
      </g>
      <g v-for="(n, i) in diagram.nodes" :key="'n' + i">
        <rect :x="n.x" :y="n.y" :width="n.w" :height="n.h" rx="9"
          :fill="tone(n.tone)" fill-opacity="0.08" :stroke="tone(n.tone)" stroke-width="1.4" />
        <text :x="n.x + n.w / 2" :y="n.y + (n.sub ? 23 : n.h / 2 + 5)"
          text-anchor="middle" :font-size="fitSize(n.label, n.w, 12.5)" font-weight="650"
          fill="var(--go-ink)">{{ n.label }}</text>
        <text v-if="n.sub" :x="n.x + n.w / 2" :y="n.y + 42"
          text-anchor="middle" :font-size="fitSize(n.sub, n.w, 11)" fill="var(--go-muted)">{{ n.sub }}</text>
      </g>
    </g>
  </svg>
</template>
