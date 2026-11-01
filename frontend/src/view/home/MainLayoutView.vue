<script setup>
import ThemeToggle from '@/components/app/ThemeToggle.vue'
import {computed, onMounted, ref} from 'vue'
import {RouterLink, RouterView, useRoute} from 'vue-router'
import {CirclePause, Download, Library, LogOut, Menu, Rss, ScrollText, Settings2, UserRound, X} from '@lucide/vue'
import {DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle} from 'reka-ui'
import {Button} from '@/components/ui/button'
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger} from '@/components/ui/dropdown-menu'
import {authorization} from '@/js/global'
import * as http from '@/js/http'
import {runtimeState} from '@/lib/runtime-state'

const route = useRoute()
const drawerOpen = ref(false)
const username = computed(() => runtimeState.username)
const isSettings = computed(() => route.path === '/settings')
const pages = [
  {path: '/catalog', label: '全部番剧', icon: Library},
  {path: '/subscriptions', label: '我的订阅', icon: Rss},
  {path: '/downloads', label: '下载', icon: Download},
  {path: '/logs', label: '日志', icon: ScrollText},
]
const focusMain = () => document.getElementById('main-content')?.focus()
onMounted(async () => {
  try { await http.config() } catch { /* 请求层展示连接或登录错误。 */ }
})
</script>

<template>
  <div class="media-shell" :class="{'settings-shell': isSettings}">
    <a href="#main-content" class="skip-link" @click.prevent="focusMain">跳到主要内容</a>
    <header v-if="!isSettings" class="media-header">
      <div class="media-identity"><button class="header-icon" aria-label="打开导航" @click="drawerOpen = true"><Menu :size="22"/></button><RouterLink to="/catalog" class="media-brand"><Rss :size="25"/>MyAnimeGo</RouterLink></div>
      <nav aria-label="主导航" class="media-nav"><RouterLink v-for="page in pages" :key="page.path" :to="page.path" :aria-current="route.path === page.path ? 'page' : undefined">{{ page.label }}</RouterLink></nav>
      <div class="media-account">
        <ThemeToggle/>
        <RouterLink v-if="runtimeState.developmentMode && runtimeState.automaticTasksPaused" to="/settings?section=migration" class="header-icon paused-link" aria-label="自动任务已暂停"><CirclePause :size="19"/></RouterLink>
        <DropdownMenu>
          <DropdownMenuTrigger as-child><Button variant="ghost" size="icon" aria-label="账号菜单" class="header-icon"><UserRound :size="21"/></Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end" class="w-48"><DropdownMenuLabel>{{ username }}</DropdownMenuLabel><DropdownMenuSeparator/><DropdownMenuItem @select="authorization = ''"><LogOut class="size-4"/>退出登录</DropdownMenuItem></DropdownMenuContent>
        </DropdownMenu>
        <RouterLink to="/settings" class="header-icon" aria-label="打开设置"><Settings2 :size="22"/></RouterLink>
      </div>
    </header>
    <main id="main-content" tabindex="-1" class="media-main"><RouterView v-slot="{Component}"><KeepAlive><component :is="Component"/></KeepAlive></RouterView></main>
    <DialogRoot v-model:open="drawerOpen"><DialogPortal><DialogOverlay class="navigation-overlay"/><DialogContent class="navigation-drawer"><DialogDescription class="sr-only">选择要打开的页面</DialogDescription><div class="drawer-heading"><DialogTitle>MyAnimeGo</DialogTitle><DialogClose class="header-icon" aria-label="关闭导航"><X :size="21"/></DialogClose></div><nav aria-label="展开导航"><RouterLink v-for="page in pages" :key="page.path" :to="page.path" :aria-current="route.path === page.path ? 'page' : undefined" @click="drawerOpen = false"><component :is="page.icon" :size="20"/>{{ page.label }}</RouterLink></nav></DialogContent></DialogPortal></DialogRoot>
  </div>
</template>

<style scoped>
.media-shell{height:100%;display:flex;flex-direction:column;background:var(--background)}.media-header{min-height:76px;flex-shrink:0;position:relative;display:grid;grid-template-columns:minmax(180px,1fr) auto minmax(180px,1fr);align-items:center;gap:24px;padding:12px 24px;background:var(--background)}.media-identity{display:flex;align-items:center;gap:14px}.media-brand{display:flex;align-items:center;gap:9px;font-size:23px;font-weight:600;letter-spacing:-.04em;white-space:nowrap;color:var(--foreground)}.media-brand>svg{color:var(--primary)}.header-icon{width:40px;height:40px;display:inline-flex;align-items:center;justify-content:center;border:0;border-radius:50%;color:var(--text-secondary);background:none;flex-shrink:0}.header-icon:hover{background:var(--surface-hover);color:var(--foreground)}.media-nav{display:flex;align-items:center;gap:2px;background:var(--surface-raised);border-radius:999px;padding:4px}.media-nav a{display:block;padding:8px 23px;color:var(--text-secondary);font-size:15px;line-height:1.3;font-weight:600;border-radius:999px}.media-nav a:hover{color:var(--foreground)}.media-nav a[aria-current]{color:var(--nav-selected-foreground);background:var(--nav-selected)}.media-account{justify-self:end;display:flex;align-items:center;gap:6px}.paused-link{color:var(--warning)}.media-main{flex:1;min-height:0;min-width:0;outline:none;overflow:hidden}.skip-link{position:absolute;top:-100px;left:16px;z-index:100;background:var(--primary);padding:10px 16px}.skip-link:focus{top:12px}.navigation-overlay{position:fixed;inset:0;z-index:80;background:var(--dialog-overlay)}.navigation-drawer{position:fixed;inset:0 auto 0 0;z-index:81;width:min(290px,85vw);padding:24px 16px;background:var(--card);box-shadow:8px 0 40px var(--shadow-color)}.drawer-heading{display:flex;align-items:center;justify-content:space-between;padding:0 10px 20px;font-size:22px}.navigation-drawer nav{display:flex;flex-direction:column;gap:4px}.navigation-drawer nav a{display:flex;align-items:center;gap:14px;border-radius:8px;padding:12px 14px;font-size:15px;color:var(--text-secondary)}.navigation-drawer nav a[aria-current]{background:var(--primary);color:var(--primary-foreground)}.navigation-drawer nav a:hover{background:var(--surface-hover)}
@media(max-width:1100px){.media-header{gap:12px;padding:12px 18px;grid-template-columns:minmax(140px,1fr) auto minmax(140px,1fr)}.media-nav a{padding:8px 16px}.media-account{gap:0}.media-brand{font-size:20px}}
@media(max-width:780px){.media-header{display:flex;flex-wrap:wrap;min-height:64px;gap:6px 16px;padding:10px 14px 8px}.media-identity{gap:6px}.media-account{margin-left:auto}.media-nav{order:3;width:max-content;max-width:100%;margin:0 auto}.media-nav a{font-size:13px;padding:8px 18px}.media-brand{font-size:19px}.header-icon{width:34px;height:34px}.media-account>.paused-link{display:none}}
@media(max-width:380px){.media-nav a{padding:8px 15px}.media-brand{font-size:17px}.media-identity{gap:2px}.media-account{gap:0}}
</style>
