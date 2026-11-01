import {computed, watch} from 'vue'
import {useLocalStorage, usePreferredDark} from '@vueuse/core'

export const themeMode = useLocalStorage('ani-rss-theme', 'dark')
const prefersDark = usePreferredDark()
export const resolvedTheme = computed(() => themeMode.value === 'auto' ? (prefersDark.value ? 'dark' : 'light') : themeMode.value)
if (!['dark', 'light', 'auto'].includes(themeMode.value)) themeMode.value = 'dark'

export function initThemeMode() {
  watch(resolvedTheme, mode => {
    const root = document.documentElement
    root.classList.toggle('dark', mode === 'dark')
    root.dataset.theme = mode
    root.style.colorScheme = mode
    const meta = document.getElementById('themeColorMeta')
    if (meta) meta.content = getComputedStyle(root).getPropertyValue('--background').trim()
  }, {immediate: true})
}

export function toggleTheme() {
  themeMode.value = resolvedTheme.value === 'dark' ? 'light' : 'dark'
}
