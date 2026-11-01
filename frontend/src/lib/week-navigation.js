import {computed, nextTick, onActivated, onDeactivated, onMounted, ref, toValue, watch} from 'vue'

export const weekdays = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']

// 等页面可见且分类渲染完成后定位，后台刷新继续保留用户的滚动位置。
export function useWeekNavigation({container, categories, active = true, grouped = true, loading = false}) {
  const elements = new Map()
  const activeCategory = ref('')
  const mounted = ref(false), activated = ref(true), pendingToday = ref(true)
  const visible = computed(() => mounted.value && activated.value && toValue(active))
  const setCategoryElement = (label, element) => {
    if (element) elements.set(label, element)
    else elements.delete(label)
  }
  const scrollToCategory = (label, automatic = false) => {
    const root = container.value, element = elements.get(label)
    if (!root || !element || !root.clientHeight) return false
    pendingToday.value = false
    activeCategory.value = label
    root.scrollTo({top: root.scrollTop + element.getBoundingClientRect().top - root.getBoundingClientRect().top, behavior: automatic || matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'})
    return true
  }
  const syncCategory = () => {
    const root = container.value
    if (!root || !visible.value || !toValue(grouped)) return
    const groups = toValue(categories)
    let label = groups[0]?.weekLabel || ''
    const top = root.getBoundingClientRect().top + 16
    for (const day of groups) if (elements.get(day.weekLabel)?.getBoundingClientRect().top <= top) label = day.weekLabel
    if (root.scrollTop > 0 && root.scrollTop + root.clientHeight >= root.scrollHeight - 2) label = groups.at(-1)?.weekLabel || label
    activeCategory.value = label
  }
  const requestToday = () => { pendingToday.value = true }
  onMounted(() => { mounted.value = true; requestToday() })
  onActivated(() => { activated.value = true; requestToday() })
  onDeactivated(() => { activated.value = false })
  watch(() => toValue(active), value => { if (value) requestToday() })
  watch(() => toValue(grouped), value => { if (value) requestToday() })
  watch([visible, () => toValue(categories), () => toValue(loading), () => toValue(grouped), pendingToday], async () => {
    await nextTick()
    if (!visible.value || toValue(loading) || !toValue(grouped)) return
    if (pendingToday.value) {
      const groups = toValue(categories), labels = new Set(groups.map(day => day.weekLabel)), today = new Date().getDay()
      const label = weekdays.map((_, offset) => weekdays[(today + offset) % 7]).find(day => labels.has(day)) || groups[0]?.weekLabel
      if (label) scrollToCategory(label, true)
    } else syncCategory()
  }, {flush: 'post'})
  const resetPosition = async () => {
    await nextTick()
    if (container.value) container.value.scrollTop = 0
    syncCategory()
  }
  return {activeCategory, setCategoryElement, scrollToCategory, syncCategory, requestToday, resetPosition}
}
