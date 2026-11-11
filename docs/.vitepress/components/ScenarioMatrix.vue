<script setup lang="ts">
import { computed, ref } from 'vue'
import { withBase } from 'vitepress'

interface Row { cat: string; name: string; desc: string; level: number; prereq: string; link: string }

/**
 * 工程场景矩阵：站点骨架里只留一条中性占位数据，用来保证分类筛选、
 * 难度星级、表格列宽这些交互 / 样式可验证。写课程时替换 rows 与 cats 即可。
 */
const rows: Row[] = [
  { cat: '示例分类', name: '示例项目（占位）', desc: '把这一行替换成真实的工程场景条目。', level: 1, prereq: '待定', link: '/projects/' },
]
const cats = ['全部', '示例分类']
const active = ref('全部')
const shown = computed(() => active.value === '全部' ? rows : rows.filter(r => r.cat === active.value))
function badge(l: number) { return 'l' + Math.min(5, Math.max(1, l)) }
function stars(l: number) { return '★'.repeat(l) + '☆'.repeat(5 - l) }
</script>

<template>
  <div>
    <div style="display:flex;gap:.35rem;flex-wrap:wrap;margin-bottom:1rem">
      <button v-for="c in cats" :key="c" class="go-btn ghost"
        :style="{fontSize:'12px',padding:'5px 11px',borderColor: active===c ? 'var(--go-brand)':'var(--go-line)', color: active===c ? 'var(--go-brand)':'var(--go-ink)', background: active===c ? 'var(--go-brand-soft)':'transparent'}"
        @click="active = c">{{ c }}</button>
    </div>
    <div style="overflow:hidden;border:1px solid var(--go-line);border-radius:12px;box-shadow:var(--go-shadow)">
      <table class="go-matrix" style="margin:0;width:100%">
        <colgroup>
          <col style="width:14%" /><col style="width:20%" /><col style="width:40%" />
          <col style="width:11%" /><col style="width:15%" />
        </colgroup>
        <thead><tr><th>场景分类</th><th>项目</th><th>你会练到什么</th><th>难度</th><th>建议前置</th></tr></thead>
        <tbody>
          <tr v-for="(r, i) in shown" :key="i">
            <td><span class="go-badge" :class="badge(r.level)">{{ r.cat }}</span></td>
            <td><a :href="withBase(r.link)" style="font-weight:700;text-decoration:none">{{ r.name }}</a></td>
            <td style="color:var(--go-muted);line-height:1.65">{{ r.desc }}</td>
            <td class="go-stars">{{ stars(r.level) }}</td>
            <td style="color:var(--go-muted);font-size:12px">{{ r.prereq }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="go-sub" style="margin-top:.6rem">
      组织逻辑：<b>先按工程场景归类</b>，<b>每类内部再按难度从 ★ 到 ★★★★★ 递进</b>；
      课程定稿后在此填入分类、项目、简介与前置要求即可。
    </div>
  </div>
</template>
