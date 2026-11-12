import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import { onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vitepress'
import './custom.css'

import HomePage from '../components/HomePage.vue'
import PathMap from '../components/PathMap.vue'
import ScenarioMatrix from '../components/ScenarioMatrix.vue'
import ProgressTracker from '../components/ProgressTracker.vue'
import Quiz from '../components/Quiz.vue'
import Predict from '../components/Predict.vue'
import FillBlank from '../components/FillBlank.vue'
import StepBox from '../components/StepBox.vue'
import OwnershipLab from '../components/OwnershipLab.vue'
import Compare from '../components/Compare.vue'
import Callout from '../components/Callout.vue'
import Term from '../components/Term.vue'
import Exercise from '../components/Exercise.vue'
import ConceptDiagram from '../components/ConceptDiagram.vue'
import StageHeader from '../components/StageHeader.vue'
import CheckList from '../components/CheckList.vue'
import CodeTabs from '../components/CodeTabs.vue'
import LineByLine from '../components/LineByLine.vue'
import OutputBox from '../components/OutputBox.vue'
import FlowTimeline from '../components/FlowTimeline.vue'
import KnowledgeCard from '../components/KnowledgeCard.vue'
import ArchBlueprint from '../components/ArchBlueprint.vue'
import SequenceDiagram from '../components/SequenceDiagram.vue'

const components: Record<string, any> = {
  HomePage, PathMap, ScenarioMatrix, ProgressTracker, Quiz, Predict, FillBlank,
  StepBox, OwnershipLab, Compare, Callout, Term, Exercise, ConceptDiagram,
  StageHeader, CheckList, CodeTabs,
  LineByLine, OutputBox, FlowTimeline, KnowledgeCard,
  ArchBlueprint, SequenceDiagram,
}

// OwnershipLab 是参考工程里的命名，实际是一个与语言无关的「逐语句状态演示台」，
// 这里保留别名，方便直接沿用原有写法；也可以改成更好记的 StepLab。
components.StepLab = OwnershipLab

const PLAYGROUND_URL = 'https://go.dev/play/'
const PLAY_ICON = '<svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M4 2.5v11l9-5.5z" fill="currentColor"/></svg>'
const COPY_ICON = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><rect x="5.5" y="5.5" width="8.5" height="8.5" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M10.5 3.5H3.6A1.6 1.6 0 0 0 2 5.1v6.6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
const CHECK_ICON = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M3 8.5l3.2 3.2L13 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>'
const FOLD_ICON = '<svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M3.5 10 8 5.8 12.5 10" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>'
const UNFOLD_ICON = '<svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true"><path d="M3.5 6 8 10.2 12.5 6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>'

/** shiki 把每行包在 .line 里，直接取 textContent 会丢掉换行符 */
function sourceOf(code: HTMLElement): string {
  const lines = code.querySelectorAll<HTMLElement>('.line')
  const text = lines.length
    ? Array.from(lines).map((l) => l.textContent || '').join('\n')
    : code.textContent || ''
  return text.replace(/\u200b/g, '').trim()
}

/** 复制文本，兼容非 HTTPS 环境与老浏览器。 */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch { /* 继续走降级路径 */ }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  } catch { return false }
}

/* ------------------------------------------------------------------
   代码内折叠：像 IDE 一样按结构收起函数体 / 结构体 / import 组 / 缩进块。
   只读渲染后的行文本做静态分析，不改动源 DOM 文本，复制仍拿到完整代码。
   ------------------------------------------------------------------ */
const BRACE_LANGS = new Set([
  'go', 'golang', 'proto', 'protobuf', 'json', 'js', 'javascript', 'ts',
  'typescript', 'jsx', 'tsx', 'rust', 'c', 'cpp', 'java', 'css', 'scss',
])
const INDENT_LANGS = new Set(['yaml', 'yml', 'py', 'python'])
interface FoldRange { start: number; end: number; count: number }

/** 去掉字符串与注释里的括号，避免把 fmt.Println("{") 之类的内容算进层级。 */
function stripCodeNoise(line: string): string {
  return line
    .replace(/"(?:\\.|[^"\\])*"/g, '""')
    .replace(/`(?:\\.|[^`\\])*`/g, '``')
    .replace(/'(?:\\.|[^'\\])*'/g, "''")
    .replace(/\/\*.*?\*\//g, '')
    .replace(/\/\/.*$/, '')
}

function braceRanges(texts: string[]): FoldRange[] {
  const stack: { ch: string; line: number }[] = []
  const ranges: FoldRange[] = []
  const pairs: Record<string, string> = { '}': '{', ')': '(', ']': '[' }
  texts.forEach((raw, i) => {
    for (const ch of stripCodeNoise(raw)) {
      if (ch === '{' || ch === '(' || ch === '[') {
        stack.push({ ch, line: i })
      } else if (ch === '}' || ch === ')' || ch === ']') {
        const top = stack[stack.length - 1]
        if (top && top.ch === pairs[ch]) {
          stack.pop()
          // 至少隔 3 行才有折叠价值；结束行（} / )）保留，结构才完整
          if (i - top.line >= 3) ranges.push({ start: top.line, end: i, count: i - top.line - 1 })
        }
      }
    }
  })
  // 同一行可能同时开多个块，只保留跨度最大的那个
  const byStart = new Map<number, FoldRange>()
  for (const r of ranges) {
    const prev = byStart.get(r.start)
    if (!prev || r.end > prev.end) byStart.set(r.start, r)
  }
  return [...byStart.values()].sort((a, b) => a.start - b.start)
}

function indentRanges(texts: string[]): FoldRange[] {
  const ranges: FoldRange[] = []
  const stack: { indent: number; line: number }[] = []
  texts.forEach((raw, i) => {
    const t = raw.replace(/\s+$/, '')
    if (!t.trim() || /^\s*#/.test(t)) return
    const indent = t.match(/^\s*/)?.[0].length ?? 0
    while (stack.length && stack[stack.length - 1].indent >= indent) {
      const top = stack.pop()!
      const end = i - 1
      if (end - top.line >= 2) ranges.push({ start: top.line, end, count: end - top.line - 1 })
    }
    if (/:\s*$/.test(t)) stack.push({ indent, line: i })
  })
  return ranges
}

function applyInlineFolds(block: HTMLElement, code: HTMLElement, lang: string) {
  const lines = Array.from(code.querySelectorAll<HTMLElement>(':scope > .line'))
  if (lines.length < 8) return
  const texts = lines.map((l) => (l.textContent || '').replace(/\u200b/g, ''))
  let ranges: FoldRange[] = []
  if (BRACE_LANGS.has(lang)) ranges = braceRanges(texts)
  else if (INDENT_LANGS.has(lang)) ranges = indentRanges(texts)
  if (!ranges.length) return

  // VitePress 把行号容器放在 <pre> 之外、作为代码块 div 的子元素，要从 block 里取
  const nums = Array.from(block.querySelectorAll<HTMLElement>('.line-numbers-wrapper .line-number'))
  // shiki 的 .line 是 inline，每行靠紧随其后的一个 "\n" 文本节点换行。
  // 只把 .line 设为 display:none 会留下这些换行符，代码区就会出现与隐藏行数等高的空白，
  // 且容器高度不收缩、与已收缩的行号列错位。隐藏行时同步清空它的换行节点，展开时还原。
  const tails = lines.map((line) => {
    const n = line.nextSibling
    return n && n.nodeType === Node.TEXT_NODE ? (n as Text) : null
  })
  const tailText = tails.map((t) => t?.nodeValue ?? null)
  const HIDDEN = 'go-hidden-line'
  const setHidden = (i: number, hidden: boolean) => {
    lines[i]?.classList.toggle(HIDDEN, hidden)
    if (tails[i]) tails[i]!.nodeValue = hidden ? '' : (tailText[i] ?? '')
    const num = nums[i]
    if (num) {
      num.classList.toggle(HIDDEN, hidden)
      const br = num.nextElementSibling
      if (br && br.tagName === 'BR') br.classList.toggle(HIDDEN, hidden)
    }
  }

  const states = ranges.map(() => false)
  const buttons: HTMLButtonElement[] = []
  function sync() {
    // 嵌套折叠：只要被任一已折叠区间覆盖就隐藏，避免展开外层时把内层已折叠的行放出来
    for (let i = 0; i < lines.length; i++) {
      setHidden(i, ranges.some((r, k) => states[k] && i > r.start && i < r.end))
    }
    ranges.forEach((r, k) => {
      lines[r.start]?.classList.toggle('go-line-folded', states[k])
      buttons[k]?.classList.toggle('is-folded', states[k])
      buttons[k]?.setAttribute('aria-expanded', String(!states[k]))
    })
  }

  ranges.forEach((r, k) => {
    const head = lines[r.start]
    if (!head) return
    const btn = document.createElement('button')
    btn.className = 'go-line-fold'
    btn.type = 'button'
    btn.title = `折叠这 ${r.count} 行（第 ${r.start + 1}–${r.end + 1} 行）`
    btn.setAttribute('aria-label', `折叠第 ${r.start + 1} 至 ${r.end + 1} 行`)
    btn.setAttribute('aria-expanded', 'true')
    head.insertBefore(btn, head.firstChild)
    head.dataset.foldedLines = String(r.count)
    buttons.push(btn)
    btn.addEventListener('click', (e) => {
      e.stopPropagation()
      states[k] = !states[k]
      sync()
    })
  })
  block.classList.add('go-inline-foldable')
}

/** 给所有代码块加统一工具条：语言标签 + Go Playground（仅 Go）+ 复制 + 折叠 */
function enhanceCodeBlocks() {
  if (typeof document === 'undefined') return
  const blocks = document.querySelectorAll<HTMLElement>('.vp-doc div[class*="language-"]')
  blocks.forEach((block) => {
    if (block.classList.contains('go-enhanced')) return
    const pre = block.querySelector('pre')
    const code = block.querySelector('code')
    if (!pre || !code) return
    block.classList.add('go-enhanced')

    const m = block.className.match(/language-([a-z0-9+#-]+)/i)
    const lang = (m?.[1] || 'text').toLowerCase()

    const bar = document.createElement('div')
    bar.className = 'go-codebar'

    const tag = document.createElement('span')
    tag.className = 'go-codebar-lang'
    tag.textContent = lang.toUpperCase()

    const actions = document.createElement('div')
    actions.className = 'go-codebar-actions'

    // Go Playground 的分享链接必须先由服务端生成 snippet id（POST /_/share 不开放跨域），
    // 纯静态站点拿不到带代码的 URL。因此这里的做法是：复制代码 + 打开 Playground，
    // 用户粘贴后点 Run 即可；比起发一个跨域请求失败再降级，这条路径始终可用。
    if (lang === 'go' || lang === 'golang') {
      const run = document.createElement('button')
      run.className = 'go-run'
      run.type = 'button'
      run.innerHTML = PLAY_ICON + '<span>Playground 运行</span>'
      run.title = '复制代码并打开 Go Playground，粘贴后点 Run'
      run.setAttribute('aria-label', '复制代码并在 Go Playground 中运行')
      let restore: ReturnType<typeof setTimeout> | undefined
      run.addEventListener('click', () => {
        // 先发起复制（仍处于用户手势中），再同步打开标签页，避免弹窗被拦截。
        const copied = copyText(sourceOf(code))
        window.open(PLAYGROUND_URL, '_blank', 'noopener,noreferrer')
        const original = PLAY_ICON + '<span>Playground 运行</span>'
        copied.then((ok) => {
          run.innerHTML = ok
            ? CHECK_ICON + '<span>已复制，粘贴即可运行</span>'
            : PLAY_ICON + '<span>请点右侧复制按钮</span>'
          if (restore) clearTimeout(restore)
          restore = setTimeout(() => { run.innerHTML = original }, 2600)
        })
      })
      actions.append(run)
    }

    const copy = document.createElement('button')
    copy.className = 'go-copy'
    copy.type = 'button'
    copy.innerHTML = COPY_ICON
    copy.title = '复制代码'
    copy.setAttribute('aria-label', '复制代码')
    let restore: ReturnType<typeof setTimeout> | undefined
    copy.addEventListener('click', async () => {
      const ok = await copyText(sourceOf(code))
      if (ok) {
        copy.classList.add('done')
        copy.innerHTML = CHECK_ICON
        copy.title = '已复制'
      }
      if (restore) clearTimeout(restore)
      restore = setTimeout(() => {
        copy.classList.remove('done')
        copy.innerHTML = COPY_ICON
        copy.title = '复制代码'
      }, 1600)
    })
    actions.append(copy)

    // 代码折叠：短代码块不显示按钮；折叠态仍可复制 / 送 Playground（读的是完整 DOM）
    const lineCount = sourceOf(code).split('\n').length
    if (lineCount >= 8) {
      block.classList.add('go-foldable')

      const fold = document.createElement('button')
      fold.className = 'go-fold'
      fold.type = 'button'
      fold.innerHTML = FOLD_ICON + '<span>折叠</span>'
      fold.title = '折叠代码'
      fold.setAttribute('aria-label', '折叠代码')
      fold.setAttribute('aria-expanded', 'true')
      actions.insertBefore(fold, actions.firstChild)

      const expandBar = document.createElement('button')
      expandBar.className = 'go-fold-bar'
      expandBar.type = 'button'
      expandBar.innerHTML = `${UNFOLD_ICON}<span>展开全部 · 共 ${lineCount} 行</span>`
      expandBar.setAttribute('aria-label', `展开全部代码，共 ${lineCount} 行`)
      block.appendChild(expandBar)

      const setCollapsed = (collapsed: boolean) => {
        block.classList.toggle('is-collapsed', collapsed)
        fold.innerHTML = collapsed
          ? UNFOLD_ICON + '<span>展开</span>'
          : FOLD_ICON + '<span>折叠</span>'
        fold.title = collapsed ? '展开代码' : '折叠代码'
        fold.setAttribute('aria-label', collapsed ? '展开代码' : '折叠代码')
        fold.setAttribute('aria-expanded', String(!collapsed))
      }
      fold.addEventListener('click', () =>
        setCollapsed(!block.classList.contains('is-collapsed')),
      )
      expandBar.addEventListener('click', () => setCollapsed(false))
      setCollapsed(false)
    }

    applyInlineFolds(block, code, lang)

    bar.append(tag, actions)
    block.insertBefore(bar, block.firstChild)
  })
}

/** 按「视觉宽度」估算单元格文本长度：中文/全角算 2，其余算 1。 */
function visualWidth(s: string): number {
  let w = 0
  for (const ch of s) w += /[\u2e80-\uffef]/u.test(ch) ? 2 : 1
  return w
}

/**
 * 表格增强：
 * 1) 短内容列（编号、标签、状态等）保持不换行；
 * 2) 每张表包一层可横向滚动的容器，并按各列「理想宽度」算出 min-width，
 *    宽表滚动而不是把每个单元格挤成竖排文字；窄表仍自适应铺满，不出现滚动条。
 * 3) 可滚动时固定首列、显示右侧渐隐与提示；滚到尽头自动收起。
 */
function optimizeTables() {
  if (typeof document === 'undefined') return
  const tables = document.querySelectorAll<HTMLTableElement>('.vp-doc table')
  tables.forEach((table) => {
    if (table.dataset.goTableOptimized) return
    table.dataset.goTableOptimized = '1'

    const fixed = getComputedStyle(table).tableLayout === 'fixed'
    const rows = Array.from(table.querySelectorAll('tr'))
    const colCount = rows.reduce((n, r) => Math.max(n, r.children.length), 0)

    // 1) 短列不换行
    if (!fixed && colCount >= 2) {
      const widths = new Array<number>(colCount).fill(0)
      for (const row of rows) {
        for (let i = 0; i < row.children.length; i++) {
          const text = (row.children[i].textContent || '').trim()
          widths[i] = Math.max(widths[i], visualWidth(text))
        }
      }
      const longest = Math.max(...widths)
      const short = widths.map((w) => w <= 20 && w < longest)
      if (short.some(Boolean)) {
        for (const row of rows) {
          for (let i = 0; i < row.children.length; i++) {
            if (short[i]) (row.children[i] as HTMLElement).classList.add('go-nowrap')
          }
        }
      }
    }

    // 2) 包两层容器：frame 放提示，scroll 负责滚动
    let scrollEl: HTMLElement | null = table.parentElement
    if (!scrollEl || !scrollEl.classList.contains('go-table-scroll')) {
      scrollEl = document.createElement('div')
      scrollEl.className = 'go-table-scroll'
      table.parentNode?.insertBefore(scrollEl, table)
      scrollEl.appendChild(table)
    }
    let frameEl: HTMLElement | null = scrollEl.parentElement
    if (!frameEl || !frameEl.classList.contains('go-table-frame')) {
      frameEl = document.createElement('div')
      frameEl.className = 'go-table-frame'
      scrollEl.parentNode?.insertBefore(frameEl, scrollEl)
      frameEl.appendChild(scrollEl)
      const tip = document.createElement('div')
      tip.className = 'go-table-tip'
      tip.innerHTML = '<span>⇆</span> 表格较宽，可左右滚动查看（Shift + 滚轮 / 触控板横滑）'
      frameEl.appendChild(tip)
    }

    // 3) 用离屏副本量出各列理想宽度，得出表格 min-width。
    //    每列下限 96px 保证不挤成竖排，上限 300px 防止某一列的长句把表撑得过长。
    if (!fixed && colCount >= 3) {
      const probe = table.cloneNode(true) as HTMLTableElement
      probe.className = (table.className + ' vp-doc').trim()
      probe.removeAttribute('data-go-table-optimized')
      probe.style.cssText =
        'position:fixed;left:-99999px;top:0;visibility:hidden;width:auto;' +
        'max-width:none;min-width:0;table-layout:auto;margin:0;border:0;'
      probe.querySelectorAll<HTMLElement>('th,td').forEach((cell) => {
        cell.classList.remove('go-nowrap')
        cell.style.whiteSpace = 'nowrap'
        cell.style.width = 'auto'
      })
      document.body.appendChild(probe)
      const colW = new Array<number>(colCount).fill(0)
      probe.querySelectorAll('tr').forEach((r) => {
        Array.from(r.children).forEach((c, i) => {
          if (i < colCount) colW[i] = Math.max(colW[i], (c as HTMLElement).getBoundingClientRect().width)
        })
      })
      probe.remove()
      const MIN_COL = 96, MAX_COL = 300
      const total = colW.reduce((n, w) => n + Math.min(MAX_COL, Math.max(MIN_COL, Math.ceil(w))), 0)
      const minWidth = Math.min(1600, total)
      if (minWidth > 520) table.style.minWidth = minWidth + 'px'
    }

    // 4) 滚动状态同步：是否需要滚动条 / 是否已到最右
    const s = scrollEl as HTMLElement
    const f = frameEl as HTMLElement
    const sync = () => {
      const canScroll = s.scrollWidth - s.clientWidth > 4
      f.classList.toggle('is-scrollable', canScroll)
      f.classList.toggle(
        'at-end',
        !canScroll || s.scrollLeft + s.clientWidth >= s.scrollWidth - 6,
      )
    }
    s.addEventListener('scroll', sync, { passive: true })
    requestAnimationFrame(sync)
    if (typeof ResizeObserver !== 'undefined') {
      const ro = new ResizeObserver(sync)
      ro.observe(s)
      ro.observe(table)
    }
  })
}

let timer: ReturnType<typeof setTimeout> | undefined

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    for (const [name, comp] of Object.entries(components)) {
      app.component(name, comp)
    }
  },
  setup() {
    const router = useRouter()
    const run = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        enhanceCodeBlocks()
        optimizeTables()
      }, 120)
    }
    onMounted(() => {
      run()
      router.onAfterRouteChanged = run
    })
    onUnmounted(() => {
      router.onAfterRouteChanged = undefined
    })
  },
} satisfies Theme
