/** 学习进度：全部保存在浏览器本地，SSR 安全 */
import { withBase } from 'vitepress'

export type Stage = 'L0' | 'L1' | 'L2' | 'L3' | 'L4' | 'L5'

export interface Lesson { path: string; title: string; stage: Stage }

/**
 * 可打卡页面清单（站点骨架阶段先置空，不预置任何课程内容）。
 *
 * 编写正文时，每新增一节就在这里补一条：
 *   { path: '/basics/variables', title: '第 09 讲 变量与类型', stage: 'L2' }
 * - path 用不带部署 base 的站内路径（如 '/basics/variables'）；
 * - ProgressTracker 据此汇总各阶段完成度；
 * - StageHeader 只对清单内的页面显示「标记学完」按钮；
 * - 清单为空时首页进度区会显示「课程目录尚未配置」，属正常状态。
 */
export const LESSONS: Lesson[] = []

export const STAGE_LABEL: Record<Stage, string> = {
  L0: '导学', L1: '编程地基', L2: '语言基础', L3: '核心机制', L4: '并发异步', L5: '工程实战',
}
export const STAGE_COLOR: Record<Stage, string> = {
  L0: '#868e96', L1: '#2b8a3e', L2: '#1c7ed6', L3: '#7048e8', L4: '#e8590c', L5: '#c92a2a',
}

// 与同一域名下的其它教程站点共用 localStorage，键名必须区分，避免进度互相串数据。
const KEY = 'go-course-done-v1'
const CHECK_KEY = 'go-course-checks-v1'

/**
 * useRoute().path 会带部署 base（如 /go/guide/），而 LESSONS 里记的是 /guide/。
 * 统一归一化后再存储、比较，打卡才能正确计入进度。
 */
function basePrefix(): string {
  try {
    const b = withBase('/')
    return b === '/' ? '' : b.replace(/\/$/, '')
  } catch { return '' }
}

export function normPath(path: string): string {
  let p = String(path || '').replace(/\.html$/, '')
  const base = basePrefix()
  if (base && p.startsWith(base + '/')) p = p.slice(base.length)
  else if (base && p === base) p = '/'
  return p
}

export function readDone(): string[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return (Array.isArray(raw) ? raw : []).map((p) => normPath(String(p)))
  } catch { return [] }
}

export function isDone(path: string) { return readDone().includes(normPath(path)) }

export function setDone(path: string, done: boolean) {
  if (typeof window === 'undefined') return
  const key = normPath(path)
  const cur = new Set(readDone())
  done ? cur.add(key) : cur.delete(key)
  localStorage.setItem(KEY, JSON.stringify([...cur]))
  window.dispatchEvent(new CustomEvent('go-progress-change'))
}

export function readChecks(id: string): boolean[] {
  if (typeof window === 'undefined') return []
  try {
    const all = JSON.parse(localStorage.getItem(CHECK_KEY) || '{}')
    return all[id] || []
  } catch { return [] }
}

export function setChecks(id: string, arr: boolean[]) {
  if (typeof window === 'undefined') return
  let all: Record<string, boolean[]> = {}
  try { all = JSON.parse(localStorage.getItem(CHECK_KEY) || '{}') } catch { /* ignore */ }
  all[id] = arr
  localStorage.setItem(CHECK_KEY, JSON.stringify(all))
}
