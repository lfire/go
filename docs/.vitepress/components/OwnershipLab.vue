<script setup lang="ts">
import { computed, ref, watch } from 'vue'

interface Cell { name: string; value: string; state?: 'moved' | 'borrowed' | 'dropped' }
interface Step { line: string; stack: Cell[]; heap: Cell[]; log: string; err?: string }

/**
 * 逐语句状态演示台：把 Go 里最容易「以为懂了」的三个过程拆成可点击的步骤。
 * 左侧面板放当前活跃的变量 / 队列，右侧放堆上的对象或远端状态。
 * 组件的切换场景 / 上一步 / 下一步 / 从头看交互与数据完全解耦。
 */
const SCENES: Record<string, { label: string; desc: string; steps: Step[] }> = {
  'slice-grow': {
    label: '① 切片扩容', desc: 'append 遇到容量不足时，共享底层数组的两个切片会「分家」',
    steps: [
      { line: 's := make([]int, 2, 2)',
        stack: [{ name: 's', value: 'ptr→A | len=2 | cap=2' }],
        heap: [{ name: 'A', value: '[0, 0]' }],
        log: 'make 在堆上分配数组 A，切片头（ptr / len / cap）存在栈上。' },
      { line: 't := s',
        stack: [{ name: 's', value: 'ptr→A | len=2 | cap=2' }, { name: 't', value: 'ptr→A | len=2 | cap=2' }],
        heap: [{ name: 'A', value: '[0, 0]' }],
        log: '赋值只复制切片头。s 与 t 指向同一个底层数组 —— 这是绝大多数坑的来源。' },
      { line: 's = append(s, 3)',
        stack: [{ name: 's', value: 'ptr→B | len=3 | cap=4' }, { name: 't', value: 'ptr→A | len=2 | cap=2' }],
        heap: [{ name: 'A', value: '[0, 0]' }, { name: 'B', value: '[0, 0, 3]' }],
        log: 'cap 不够：分配新数组 B、拷贝旧元素再追加。s 改指向 B，t 仍指向 A。' },
      { line: 't[0] = 99',
        stack: [{ name: 's', value: 'ptr→B | len=3 | cap=4' }, { name: 't', value: 'ptr→A | len=2 | cap=2' }],
        heap: [{ name: 'A', value: '[99, 0]' }, { name: 'B', value: '[0, 0, 3]' }],
        log: '此刻改 t 不会影响到 s。若不扩容，两者会互相看见修改。' },
      { line: '// 想让 t 也看到？用 copy 或返回新切片',
        stack: [{ name: 's', value: 'ptr→B | len=3 | cap=4' }, { name: 't', value: 'ptr→A | len=2 | cap=2', state: 'moved' }],
        heap: [{ name: 'A', value: '[99, 0]' }, { name: 'B', value: '[0, 0, 3]' }],
        log: '经验：append 的结果一定要接住并传回给所有持有方，不要假设共享会一直成立。' },
    ],
  },
  'gmp-schedule': {
    label: '② GMP 调度', desc: '一个 goroutine 从创建到被线程执行，中间经过哪些队列',
    steps: [
      { line: 'go worker()',
        stack: [{ name: 'P', value: '持有本地队列' }],
        heap: [{ name: 'G(worker)', value: '新建，状态 _Grunnable' }],
        log: 'go 关键字创建一个 G，优先放进当前 P 的本地运行队列（runnext）。' },
      { line: '// 调度器寻找可运行的 G',
        stack: [{ name: 'P', value: '本地队列: 1 个 G' }, { name: 'M', value: '持有 P，空闲' }],
        heap: [{ name: 'G(worker)', value: '等待被调度' }],
        log: 'schedule() 依次看 runnext、本地队列、全局队列，最后尝试从别的 P 偷。' },
      { line: '// execute(g)',
        stack: [{ name: 'P', value: '本地队列: 空' }, { name: 'M', value: '正在执行 G' }],
        heap: [{ name: 'G(worker)', value: '_Grunning' }],
        log: 'M 切换到 G 的栈开始执行。此时 G、M、P 三者绑定。' },
      { line: '// worker 里发起了阻塞系统调用',
        stack: [{ name: 'M', value: '阻塞在 syscall' }, { name: 'P', value: 'handoff 给新 M', state: 'moved' }],
        heap: [{ name: 'G(worker)', value: '_Gsyscall' }],
        log: 'M 被阻塞时，P 会转交给另一个 M，队列里的其它 G 不会被一起卡住。', },
      { line: '// 调用返回，G 重新入队',
        stack: [{ name: 'P', value: '由新 M 持有' }, { name: 'M', value: '回到空闲或找新 P' }],
        heap: [{ name: 'G(worker)', value: '_Grunnable → 再次入队' }],
        log: '这就是 Go 能用少量线程扛住大量并发 I/O 的关键：阻塞的是 M，不是 P。' },
    ],
  },
  'channel-flow': {
    label: '③ channel 收发', desc: '无缓冲 channel 如何把发送方与接收方直接配对',
    steps: [
      { line: 'ch := make(chan int)',
        stack: [{ name: 'ch', value: 'hchan: buf=0' }],
        heap: [{ name: 'sendq', value: '空' }, { name: 'recvq', value: '空' }],
        log: '无缓冲 channel 没有数据槽位，必须收发双方同时就绪。' },
      { line: 'go func() { ch <- 42 }()',
        stack: [{ name: 'G1', value: '准备发送 42' }],
        heap: [{ name: 'sendq', value: 'G1 入队等待' }, { name: 'recvq', value: '空' }],
        log: '没有接收者，发送方 G1 被挂起并登记到 sendq，不占用 CPU。' },
      { line: 'v := <-ch',
        stack: [{ name: 'G2', value: '准备接收' }, { name: 'v', value: '待赋值' }],
        heap: [{ name: 'sendq', value: 'G1 被唤醒' }, { name: 'recvq', value: '空' }],
        log: '接收者到达时，运行时直接从 sendq 取出 G1，把值拷贝给接收方并唤醒 G1。' },
      { line: '// 值完成交接',
        stack: [{ name: 'v', value: '42' }, { name: 'G1', value: '恢复 _Grunnable' }],
        heap: [{ name: 'sendq', value: '空' }, { name: 'recvq', value: '空' }],
        log: '无缓冲 channel 的价值在于「同步交接」：发送完成的那一刻，接收也完成了。' },
      { line: 'close(ch)',
        stack: [{ name: 'ch', value: 'closed=1' }],
        heap: [{ name: 'sendq', value: '发送者会 panic' }, { name: 'recvq', value: '接收者立即返回零值' }],
        log: '关闭只应由发送方做；接收方用 v, ok := <-ch 区分「零值」和「已关闭」。' },
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
