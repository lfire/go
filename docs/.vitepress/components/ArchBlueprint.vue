<script setup lang="ts">
import { computed, ref } from 'vue'

/**
 * 架构蓝图：分层展示系统，每层含若干模块，模块可点击展开职责说明。
 * 层与层之间标注调用协议，用来替代「一句话 + 一张简单示意图」的架构描述。
 */
interface Node { label: string; sub?: string; detail?: string; tone?: string }
interface Layer { name: string; note?: string; protocol?: string; items: Node[] }

const props = defineProps<{
  title?: string
  layers: string | Layer[]
  caption?: string
}>()

const COLORS: Record<string, string> = {
  orange: 'var(--go-brand)', blue: 'var(--go-blue)', green: 'var(--go-green)',
  purple: 'var(--go-purple)', red: 'var(--go-red)', gray: 'var(--go-muted)',
}
function color(t?: string) { return COLORS[t || ''] || 'var(--go-brand)' }

function parse(v: any): Layer[] {
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    try { const p = JSON.parse(v); if (Array.isArray(p)) return p } catch { /* ignore */ }
  }
  return []
}
const list = computed<Layer[]>(() => parse(props.layers))
const active = ref<{ l: number; i: number } | null>(null)
const current = computed(() => {
  const a = active.value
  return a ? list.value[a.l]?.items[a.i] : null
})
function pick(l: number, i: number) {
  const a = active.value
  active.value = a && a.l === l && a.i === i ? null : { l, i }
}
</script>

<template>
  <div class="ab-wrap">
    <div v-if="title" class="go-eyebrow" style="color:var(--go-purple)">🏛 {{ title }}</div>
    <div class="ab-layers">
      <div v-for="(layer, li) in list" :key="li" class="ab-layer">
        <div class="ab-layer-head">
          <b>{{ layer.name }}</b>
          <span v-if="layer.note" class="ab-layer-note">{{ layer.note }}</span>
        </div>
        <div class="ab-items">
          <button v-for="(item, i) in layer.items" :key="i" type="button"
            class="ab-item" :class="{ active: active?.l === li && active?.i === i }"
            :style="{ '--ab-color': color(item.tone) }" @click="pick(li, i)">
            <b>{{ item.label }}</b>
            <span v-if="item.sub">{{ item.sub }}</span>
          </button>
        </div>
        <div v-if="li < list.length - 1" class="ab-arrow">
          <span v-if="layer.protocol" class="ab-proto">{{ layer.protocol }}</span>
          <i>↓</i>
        </div>
      </div>
    </div>
    <div v-if="current" class="ab-detail">
      <div class="ab-detail-head">📌 {{ current.label }}<span>点击模块可切换 / 再次点击收起</span></div>
      <p>{{ current.detail || current.sub || '该模块暂无补充职责说明。' }}</p>
    </div>
    <div v-else class="ab-hint">💡 点击任意模块，查看它的职责、技术选型与失败处理方式</div>
    <div v-if="caption" class="ab-caption">{{ caption }}</div>
  </div>
</template>
