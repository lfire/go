import { withBase } from 'vitepress'

export interface RichPart {
  t: 'text' | 'link' | 'strong' | 'code'
  v: string
  href?: string
}

/** 站内 .md 链接要转成 cleanUrl 并补上部署 base；外链与锚点原样保留。 */
function hrefOf(url: string): string {
  const u = url.trim()
  if (/^(https?:|mailto:|#)/.test(u)) return u
  if (u.startsWith('/')) return withBase(u.replace(/\.md$/, ''))
  return u
}

/**
 * 组件属性（如 <Callout text="..." />）传进来的都是纯字符串，
 * markdown 不会被引擎解析。这里补上最小子集：[链接](路径)、**加粗**、`代码`，
 * 让教程里写在属性中的交叉引用真正可点击。
 */
export function parseRich(input: string): RichPart[] {
  const s = String(input || '')
  // 链接文字里可能自带方括号（如 `切片 &[T]`），所以用非贪婪匹配到第一个 `](`
  const re = /(\[(.+?)\]\(([^)]+)\)|\*\*[^*]+\*\*|`[^`]+`)/g
  const out: RichPart[] = []
  let last = 0
  for (const m of s.matchAll(re)) {
    const i = m.index ?? 0
    if (i > last) out.push({ t: 'text', v: s.slice(last, i) })
    const tok = m[0]
    if (tok.startsWith('[')) {
      const mm = tok.match(/^\[(.+)\]\(([^)]+)\)$/)
      out.push(mm
        ? { t: 'link', v: mm[1], href: hrefOf(mm[2]) }
        : { t: 'text', v: tok })
    } else if (tok.startsWith('**')) {
      out.push({ t: 'strong', v: tok.slice(2, -2) })
    } else {
      out.push({ t: 'code', v: tok.slice(1, -1) })
    }
    last = i + tok.length
  }
  if (last < s.length) out.push({ t: 'text', v: s.slice(last) })
  return out
}
