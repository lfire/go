<script setup lang="ts">
import { withBase } from 'vitepress'
import PathMap from './PathMap.vue'
import ScenarioMatrix from './ScenarioMatrix.vue'
import ProgressTracker from './ProgressTracker.vue'
import ConceptDiagram from './ConceptDiagram.vue'
import FlowTimeline from './FlowTimeline.vue'
import Callout from './Callout.vue'

const features = [
  { ico: '🔍', t: '逐行讲解', d: '示例代码可以一行行点开看，每一行在做什么、为什么这么写，都写在旁边，不靠猜。' },
  { ico: '🎯', t: '先猜后看', d: '运行结果默认藏起来，先在脑子里跑一遍再揭晓；答错会解释错在哪，而不是只判对错。' },
  { ico: '🧩', t: '随堂检测', d: '选择题、代码填空、结果预测、动手练习交替出现，每讲至少三次主动回忆。' },
  { ico: '🖼️', t: '图文并重', d: '47 张可放大的概念图 + 流程时间线，把 GMP、GC、interface、分布式链路画出来看。' },
  { ico: '🔬', t: '交互实验台', d: '切片扩容、GMP 调度、channel 收发可以逐语句点着走，状态变化一屏看完。' },
  { ico: '📈', t: '进度与速记', d: '每讲可打卡，按 11 个阶段汇总；结尾一页纸速记把要点和坑分开列清楚。' },
]
const stats = [
  { n: '11', t: '个递进阶段' }, { n: '118', t: '讲完整课程' },
  { n: '10', t: '类工程场景' }, { n: '47', t: '张概念图示' },
]
const loop = [
  { title: '建立直觉', desc: '类比 + 概念图，先知道它解决什么', tone: 'blue' },
  { title: '可运行示例', desc: '每段代码都能编译，带输出', tone: 'green' },
  { title: '逐行拆解', desc: '点开每一行看解释', tone: 'orange' },
  { title: '猜输出 / 填空', desc: '主动回忆比多看一遍有效', tone: 'purple' },
  { title: '动手练习', desc: '带提示与参考解的编程题', tone: 'red' },
  { title: '速记卡', desc: '要点与易错点分开收束', tone: 'gray' },
]
</script>

<template>
  <div class="home">
    <section class="hp-hero">
      <span class="hp-kicker">从零基础到资深 · 交互式中文教程</span>
      <h1>把 <em>Go</em> 学成<br />你真正会用的语言</h1>
      <p>不假设你懂系统编程，也不停在「会写语法」。11 个递进阶段、118 讲，从第一行 <code>fmt.Println</code> 一路走到运行时源码、高并发架构与生产级工程 —— 每个难点都拆成能看懂、能动手、能自测的一小步。</p>
      <div class="hp-actions">
        <a class="hp-btn main" :href="withBase('/guide/opening')">🚀 从第 001 讲开始</a>
        <a class="hp-btn alt" :href="withBase('/guide/curriculum')">📖 118 讲完整目录</a>
        <a class="hp-btn alt" :href="withBase('/projects/')">🏗️ 直接看工程实战</a>
      </div>
      <div class="hp-stats">
        <div class="hp-stat" v-for="(s, i) in stats" :key="i"><b>{{ s.n }}</b><span>{{ s.t }}</span></div>
      </div>
    </section>

    <section class="hp-section">
      <div class="hp-section-head">
        <div class="eyebrow">How you learn</div>
        <h2>不是把文档搬上来，而是重新设计学习过程</h2>
        <p>同样的知识点，读一遍和「先猜、再验证、再练一遍」的留存率完全不同。整套课程把交互做成固定节奏：每一讲都按下面的闭环推进，难点另配可放大的图示和逐语句实验台。</p>
      </div>
      <FlowTimeline :steps="loop" title="一讲的学习闭环" direction="horizontal" />
      <div class="hp-grid" style="margin-top:1.6rem">
        <div class="hp-feature" v-for="(f, i) in features" :key="i">
          <div class="ico">{{ f.ico }}</div><h4>{{ f.t }}</h4><p>{{ f.d }}</p>
        </div>
      </div>
    </section>

    <section class="hp-section" id="roadmap">
      <div class="hp-section-head">
        <div class="eyebrow">Learning path</div>
        <h2>十一个阶段，一条不跳步的路线</h2>
        <p>顺序按认知负担设计：先建直觉 → 再学语法 → 再拆底层 → 再做工程 → 最后做架构与源码。前一阶段的输出，正好是下一阶段的前置；每一段都有明确的「学完能做什么」。</p>
      </div>
      <PathMap />
      <div style="margin-top:1.2rem">
        <ConceptDiagram type="compile-link" title="先看清一次 Go 程序是怎样从源码变成可执行文件的" />
      </div>
      <Callout type="tip" title="已经有经验？" text="可以直接从「阶段三 · 核心机制」或「阶段五 · 工程化」切入；每讲顶部都标了难度与前置，侧栏按阶段分组，随时能跳回来补。" />
    </section>

    <section class="hp-section" id="scenarios">
      <div class="hp-section-head">
        <div class="eyebrow">Engineering scenarios</div>
        <h2>10 类工程场景：分类 × 难度</h2>
        <p>不是零散 demo 的堆砌。命令行、网络、数据、并发、存储、微服务、云原生、跨语言、AI 工程、安全治理十类场景，每类内部从 ★ 走到 ★★★★★；其中 KV Server 是贯穿 4 讲的演进线，把接口抽象、协议、持久化、并发与分布式串成完整历史。点分类标签可以筛选。</p>
      </div>
      <ScenarioMatrix />
    </section>

    <section class="hp-section">
      <div class="hp-section-head">
        <div class="eyebrow">Your progress</div>
        <h2>学到哪了，一眼看得见</h2>
        <p>每讲顶部可以「标记学完」，这里会按阶段汇总。进度只保存在你自己的浏览器里，不会上传，也可以一键清空。</p>
      </div>
      <ProgressTracker />
    </section>

    <section class="hp-cta">
      <h2>准备好写第一行 Go 了吗？</h2>
      <p style="color:var(--go-muted);font-size:14px;line-height:1.8;margin-bottom:1.3rem">先花十分钟装好环境，第 003 讲就会带着你把每一行代码看明白。</p>
      <a class="hp-btn main" style="background:var(--go-brand)" :href="withBase('/guide/setup')">从装环境开始 →</a>
    </section>

    <div style="text-align:center;color:var(--go-muted);font-size:12px;line-height:1.8;margin:2rem 0 .5rem">
      为一遍学明白而设计 · 例子均可编译，图示可放大，进度只在本地<br />
      <a :href="withBase('/about/')" style="text-decoration:none">关于本站与内容编排思路 →</a>
    </div>
  </div>
</template>
