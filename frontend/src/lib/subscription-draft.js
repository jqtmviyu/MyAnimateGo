// 简洁表单与完整编辑器使用相同的备用 RSS 清理规则。
export function normalizeStandbyRss(items) {
  return items.map(item => ({...item, url: item.url.trim()})).filter(item => item.url !== '')
}
