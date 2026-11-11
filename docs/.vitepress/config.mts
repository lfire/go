import { defineConfig } from 'vitepress'

// GitHub 项目页部署在子路径下，base 与仓库名一致。
// 改用自定义域名 / 用户主页仓库时，构建时传 VP_BASE=/ 即可，无需改代码。
const BASE = process.env.VP_BASE || '/go/'

export default defineConfig({
  lang: 'zh-CN',
  title: 'Go 零基础通关',
  description: '一套为完全零基础学习者设计的交互式 Go 教程：递进式知识路径 + 分类分级的工程实战。',
  base: BASE,
  outDir: '.vitepress/dist',
  cacheDir: '.vitepress/cache',
  lastUpdated: true,
  cleanUrls: true,
  ignoreDeadLinks: true,
  metaChunk: true,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: `${BASE}favicon.svg` }],
    ['meta', { name: 'theme-color', content: '#00add8' }],
    ['meta', { name: 'og:title', content: 'Go 零基础通关 · 交互式教程' }],
    ['meta', { name: 'og:description', content: '从第一行代码到生产级工程，边学边练的 Go 中文教程。' }],
  ],
  vite: {
    build: {
      // 本地全文搜索索引只在打开搜索框时懒加载；提高阈值避免构建时输出无害的体积提示。
      chunkSizeWarningLimit: 700,
    },
  },
  markdown: {
    lineNumbers: true,
    container: {
      tipLabel: '小贴士',
      warningLabel: '注意',
      dangerLabel: '当心',
      infoLabel: '说明',
      detailsLabel: '展开查看详情',
    },
  },
  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'Go 零基础通关',
    outline: { level: [2, 3], label: '本页目录' },
    docFooter: { prev: '上一节', next: '下一节' },
    darkModeSwitchLabel: '外观',
    lightModeSwitchTitle: '切换到浅色模式',
    darkModeSwitchTitle: '切换到深色模式',
    sidebarMenuLabel: '课程目录',
    returnToTopLabel: '回到顶部',
    search: {
      provider: 'local',
      options: {
        translations: {
          button: { buttonText: '搜索课程', buttonAriaLabel: '搜索课程' },
          modal: {
            noResultsText: '没有找到相关内容',
            resetButtonTitle: '清除搜索',
            backButtonTitle: '返回',
            displayDetails: '展开详情',
            footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' },
          },
        },
      },
    },
    // 栏目结构与参考站点保持一致；具体讲次目录待课程定稿后填充。
    nav: [
      { text: '学习路线', link: '/guide/' },
      { text: '阶段课程', link: '/foundation/' },
      { text: '工程实战', link: '/projects/' },
      { text: '练习与速查', link: '/practice/' },
      { text: '关于', link: '/about/' },
    ],
    sidebar: {
      '/guide/': placeholderSidebar('开始之前', [
        { text: '学习路线', link: '/guide/' },
      ]),
      '/foundation/': placeholderSidebar('阶段课程', [
        { text: '阶段课程导览', link: '/foundation/' },
      ]),
      '/projects/': placeholderSidebar('工程实战', [
        { text: '工程场景地图', link: '/projects/' },
      ]),
      '/practice/': placeholderSidebar('练习与速查', [
        { text: '练习与速查', link: '/practice/' },
      ]),
      '/about/': placeholderSidebar('关于本站', [
        { text: '关于本站', link: '/about/' },
        { text: '本地运行与 Pages 部署', link: '/about/deploy' },
      ]),
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/lfire/go' }],
    footer: {
      message: '为完全零基础的学习者而写 · 站点框架与交互组件先行，课程内容陆续补充',
      copyright: '© 2026 Go 零基础通关',
    },
  },
})

/**
 * 占位侧栏。写正文时把 items 换成真实讲次即可；
 * 需要按阶段分组时，返回多个 { text, collapsed, items } 对象。
 */
function placeholderSidebar(group: string, items: { text: string; link: string }[]) {
  return [{ text: group, collapsed: false, items }]
}
