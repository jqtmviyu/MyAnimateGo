const weekdayNames = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']
export const anibtTitle = item => item.title?.chinese || item.title?.primary || item.title?.japanese || '未命名番剧'
export const anibtTime = (value, timeZone) => value ? new Intl.DateTimeFormat('zh-CN', {timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23'}).format(new Date(value * 1000)) : '时间待定'
export const anibtDate = value => value ? new Intl.DateTimeFormat('zh-CN', {year: 'numeric', month: '2-digit', day: '2-digit'}).format(new Date(value)) : ''
export const anibtToday = (now, timeZone) => {
  const date = new Intl.DateTimeFormat('en-US', {timeZone, weekday: 'short'}).format(now)
  return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(date)
}
export const anibtMinutes = value => {
  if (!value) return null
  const [hours, minutes] = anibtTime(value).split(':').map(Number)
  return hours * 60 + minutes
}
export const normalizeAnibt = data => (data.byWeekday || []).map(week => {
  const label = week.weekdayLabel?.replace(/^周([一二三四五六日天])$/, (_, day) => `星期${day === '天' ? '日' : day}`)
  const weekday = label && !weekdayNames.includes(label) ? -1 : week.weekday
  return {
    weekLabel: label || weekdayNames[weekday] || '未排播', weekday,
    items: (week.animes || []).map(item => ({...item, bgmId: String(item.bgmId), title: anibtTitle(item), originalTitle: item.title?.japanese || item.title?.primary || '', url: `https://anibt.net/anime/${item.bgmId}`})).sort((a,b) => (anibtMinutes(a.airingAt) ?? Infinity) - (anibtMinutes(b.airingAt) ?? Infinity))
  }
})
export const normalizeAnibtGroups = groups => groups.map(group => ({
  ...group, label: group.name || group.slug, bgmId: String(group.bgmId),
  updateDay: group.lastUpdatedAt ? `${anibtDate(group.lastUpdatedAt)} 更新` : '',
  items: (group.items || []).map(item => ({...item, createdAt: anibtDate(item.publishedAt)}))
}))
export const anibtSubscribedIds = subscriptions => new Set((subscriptions || []).flatMap(item => {
  const id = item.bgmUrl?.match(/(?:subject\/)?(\d+)\/?$/)?.[1]
  const rssId = new URL(item.url || '', 'https://anibt.net').searchParams.get('bgmId')
  return [id, rssId].filter(Boolean)
}))

export const anibtPremiere = item => {
  if (!item.premiereDate) return item.seasonLabel || ''
  if (item.premierePrecision === 'year') return `${item.premiereDate.slice(0,4)} 年首播`
  if (item.premierePrecision === 'season') return item.seasonLabel || ''
  if (item.premierePrecision === 'month') return `${item.premiereDate.slice(0,7)} 首播`
  return `${item.premiereDate} 首播`
}

// 官网季度入口固定围绕当季，接口的全部已收录季度用于其他查询。
export const anibtSeasonOptions = currentSeason => {
  const match = /^(\d{4})(冬|春|夏|秋)$/.exec(currentSeason)
  if (!match) return []
  const names = ['冬', '春', '夏', '秋']
  const current = Number(match[1]) * 4 + names.indexOf(match[2])
  return [-1, 0, 1].map(offset => {
    const index = current + offset
    const year = Math.floor(index / 4)
    const name = names[index % 4]
    return {seasonLabel: `${year}${name}`, label: `${year} ${name}季${offset === 0 ? '（当前季度）' : ''}`}
  })
}
