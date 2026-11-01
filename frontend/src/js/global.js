import {themeMode as store, initThemeMode} from '@/lib/theme'
import {useLocalStorage} from "@vueuse/core";

/**
 * 保存登录信息
 */
let rememberThePassword = useLocalStorage('rememberThePassword', {
    remember: false,
    username: '',
    password: ''
})

/**
 * 令牌
 */
const authorization = useLocalStorage('authorization', '')

/**
 * 显示评分
 */
const showScore = useLocalStorage('show-score', true)

/**
 * 按星期展示
 */
const showWeek = useLocalStorage("show-week", true)

/**
 * 点击订阅封面时执行的操作
 */
const coverClickAction = useLocalStorage('cover-click-action', 'edit')
if (coverClickAction.value === 'playlist') coverClickAction.value = 'edit'

/**
 * 启动页
 */
const startupPage = useLocalStorage('startup-page', '/catalog')
if (!['/catalog', '/subscriptions', '/logs'].includes(startupPage.value)) startupPage.value = '/catalog'

/**
 * 显示更新时间
 */
const showLastDownloadTime = useLocalStorage("show-last-download-time", true);

/**
 * 强调色
 */
const color = useLocalStorage('ani-rss-accent', '#52b54b')
if (['#18765b', '#109d58'].includes(color.value.toLowerCase())) color.value = '#52b54b'

/**
 * 改动强调色
 */
const colorChange = (v) => {
    const el = document.documentElement
    if (!v || !CSS.supports('color', v)) return
    el.style.setProperty('--primary', v)
    el.style.setProperty('--ring', v)
    el.style.setProperty('--sidebar-primary', v)
    const hex = v.replace('#', '')
    if (/^[0-9a-f]{6}$/i.test(hex)) {
        const brightness = [0, 2, 4].map(offset => parseInt(hex.slice(offset, offset + 2), 16) / 255)
        const luminance = brightness[0] * .2126 + brightness[1] * .7152 + brightness[2] * .0722
        el.style.setProperty('--primary-foreground', luminance > .63 ? '#14251c' : '#ffffff')
    }
}

/**
 * 主题初始化
 */
const initTheme = () => {
    initThemeMode()

    // 修改强调色
    colorChange(color.value)
}

/**
 * 初始化
 */
const init = () => {
    initTheme()
}

const base64Encode = s => {
    const encoder = new TextEncoder();
    const data = encoder.encode(s);
    return window.btoa(String.fromCharCode(...data));
}

const getBaseUrl = () => {
    const {protocol, host, pathname} = location
    return `${protocol}//${host}${pathname}`
}

const toApiUrl = (path, params) => {
    const url = new URL(getBaseUrl())
    url.pathname += path
    url.search = new URLSearchParams(params).toString()
    return url.toString();
}

const proxyImage = imgUrl => {
    return toApiUrl('api/proxyImage', {
        imgUrl: base64Encode(imgUrl),
        s: authorization.value
    })
}

const toApiFile = filename => {
    return toApiUrl('api/file', {
        filename: base64Encode(filename),
        s: authorization.value
    })
}

export {
    rememberThePassword,
    authorization,
    store,
    showScore,
    showWeek,
    coverClickAction,
    startupPage,
    showLastDownloadTime,
    color,
    colorChange,
    init,
    initTheme,
    base64Encode,
    toApiUrl,
    proxyImage,
    toApiFile,
    getBaseUrl
};
