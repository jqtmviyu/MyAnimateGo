// Mikan 目录给出方形缩略图，海报墙改取详情页同规格的竖版封面。
export function mikanPosterURL(source) {
  if (!source) return ''
  const url = new URL(source, location.href)
  if (url.pathname.startsWith('/images/Bangumi/')) {
    url.searchParams.set('width', '400')
    url.searchParams.set('height', '560')
  }
  return url.toString()
}
