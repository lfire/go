<script setup lang="ts">
import { withBase } from 'vitepress'
import Quiz from './Quiz.vue'
import Callout from './Callout.vue'
import ConceptDiagram from './ConceptDiagram.vue'

// 这里只描述站点框架本身具备的能力，不含任何课程内容。
const features = [
  { ico: '🧩', t: '随堂问答组件', d: '选择题、结果预测、代码填空都带即时反馈与解析，写正文时一行标签就能插入。' },
  { ico: '🔬', t: '可放大的概念图', d: '点击放大、滚轮与双指缩放、拖拽平移、ESC 关闭 —— 抽象机制可以「看」着讲。' },
  { ico: '▶️', t: '代码块工具条', d: '每个代码块自动带语言标签与复制按钮；Go 代码块还能一键复制并打开官方 Playground。' },
  { ico: '🧭', t: '路线与场景矩阵', d: '阶段路线卡片、可按分类筛选的工程场景表已就位，等课程定稿后填入数据即可。' },
  { ico: '📈', t: '本地学习进度', d: '逐页打卡、按阶段汇总、一键清空；数据只存在浏览器里，不会上传。' },
  { ico: '🌗', t: '双主题与全文搜索', d: '浅色 / 深色两套配色，本地全文搜索、目录大纲、上一节 / 下一节都已配好。' },
]
const stats = [
  { n: '18', t: '个已注册组件' }, { n: '2', t: '套配色主题' },
  { n: '0', t: '篇正文（待编写）' }, { n: '1', t: '条 Pages 流水线' },
]
</script>

<template>
  <div class="home">
    <section class="hp-hero">
      <span class="hp-kicker">从零开始 · 交互式中文教程</span>
      <h1>把 <em>Go</em> 学成<br />你真正会用的语言</h1>
      <p>站点框架、设计系统与交互组件已经就绪，教程正文正在编写中。这里会是一套从第一行代码走到工程实战、边读边练的 Go 中文教程。</p>
      <div class="hp-actions">
        <a class="hp-btn main" :href="withBase('/guide/')">🚀 查看学习路线</a>
        <a class="hp-btn alt" :href="withBase('/about/deploy')">🛠️ 本地运行与部署</a>
        <a class="hp-btn alt" href="https://github.com/lfire/go" target="_blank" rel="noopener noreferrer">📦 GitHub 仓库</a>
      </div>
      <div class="hp-stats">
        <div class="hp-stat" v-for="(s, i) in stats" :key="i"><b>{{ s.n }}</b><span>{{ s.t }}</span></div>
      </div>
    </section>

    <section class="hp-section">
      <div class="hp-section-head">
        <div class="eyebrow">Site capabilities</div>
        <h2>内容之外，先搭好的东西</h2>
        <p>这一层与写什么无关：主题、排版、交互组件、代码块增强、进度存储、搜索和自动部署都已可直接使用。正文只要按栏目往 <code>docs/</code> 里加 Markdown，再到配置里补一条目录即可。</p>
      </div>
      <div class="hp-grid">
        <div class="hp-feature" v-for="(f, i) in features" :key="i">
          <div class="ico">{{ f.ico }}</div><h4>{{ f.t }}</h4><p>{{ f.d }}</p>
        </div>
      </div>
    </section>

    <section class="hp-section" id="components">
      <div class="hp-section-head">
        <div class="eyebrow">Component check</div>
        <h2>交互组件自检</h2>
        <p>下面这几个示例用来确认全局组件、样式与放大交互都已生效，它们只是组件用法演示，不是课程内容。</p>
      </div>
      <Callout type="tip" title="组件已全局注册" text="在任意 Markdown 里直接写 Quiz、Callout、ConceptDiagram 等组件标签即可，无需单独 import。" />
      <div style="margin-top:1rem">
        <Quiz no="D-1" q="下面哪个是本站已经注册好的交互组件？" :options='["Quiz 随堂问答", "数据库服务", "应用服务器"]' answer="0" explain="交互组件统一在 .vitepress/theme/index.ts 中全局注册，Markdown 里直接写标签即可。" />
      </div>
      <div style="margin-top:1rem">
        <ConceptDiagram type="placeholder" title="概念图组件：点击可放大查看" />
      </div>
    </section>

    <section class="hp-cta">
      <h2>内容正在路上</h2>
      <p style="color:var(--go-muted);font-size:14px;line-height:1.8;margin-bottom:1.3rem">先从学习路线开始搭目录，也可以看看这套站点是怎么构建并自动发布到 GitHub Pages 的。</p>
      <a class="hp-btn main" style="background:var(--go-brand)" :href="withBase('/about/deploy')">查看部署说明 →</a>
    </section>

    <div style="text-align:center;color:var(--go-muted);font-size:12px;line-height:1.8;margin:2rem 0 .5rem">
      课程内容编写中 · 站点框架与交互组件已就绪<br />
      <a :href="withBase('/about/')" style="text-decoration:none">关于本站 →</a>
    </div>
  </div>
</template>
