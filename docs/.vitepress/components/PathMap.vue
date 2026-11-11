<script setup lang="ts">
import { withBase } from 'vitepress'

/**
 * 阶段路线卡片：站点骨架里只留中性占位数据。
 * 课程定稿后替换成真实的阶段名称、简介、链接与配色即可，卡片交互不变。
 */
const stages = [
  { lv: '阶段一', name: '待编写', desc: '在这里配置第一阶段的名称、简介与链接。', link: '/guide/', color: '#2b8a3e', meta: '占位' },
  { lv: '阶段二', name: '待编写', desc: '在这里配置第二阶段的名称、简介与链接。', link: '/foundation/', color: '#1c7ed6', meta: '占位' },
  { lv: '阶段三', name: '待编写', desc: '在这里配置第三阶段的名称、简介与链接。', link: '/projects/', color: '#7048e8', meta: '占位' },
]
</script>

<template>
  <div class="go-path">
    <a v-for="(s, i) in stages" :key="i" class="go-path-card" :href="withBase(s.link)" :style="{borderTop:'3px solid '+s.color}">
      <div class="lv" :style="{color:s.color}">{{ s.lv }} <span style="color:var(--go-muted);font-weight:600">· {{ s.meta }}</span></div>
      <div class="nm">{{ s.name }}</div>
      <div class="ds">{{ s.desc }}</div>
      <div style="margin-top:.7rem;font-size:12px;font-weight:700" :style="{color:s.color}">进入阶段 →</div>
    </a>
  </div>
</template>
