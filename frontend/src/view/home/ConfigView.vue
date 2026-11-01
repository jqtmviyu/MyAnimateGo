<template>
  <div class="server-settings">
    <SettingsNavigation class="settings-desktop-nav" :categories="categories" :active="sectionName" @select="selectSection"/>
    <section class="settings-workspace" v-busy="loading" aria-label="设置内容">
      <header class="settings-content-header">
        <div class="settings-title"><RouterLink to="/catalog" class="settings-header-icon" aria-label="返回媒体库"><ChevronLeft :size="25"/></RouterLink><button class="settings-header-icon settings-menu" aria-label="打开设置分类" @click="settingsNavOpen = true"><Menu :size="23"/></button><h1>{{ activeSection.label }}</h1></div>
        <div class="flex items-center gap-2"><ThemeToggle/>
        <AppButton class="settings-save" type="primary" :disabled="loading" :loading="configButtonLoading" @click="saveConfig"><Save :size="16"/>保存</AppButton></div>
      </header>
      <div class="settings-form-scroll" :key="sectionName">
        <div class="settings-form">
          <p class="settings-description">{{ activeSection.description }}</p>
          <ExcludeView v-if="sectionName === 'exclude'" v-model:exclude="config.exclude" :show-text="true"/>
          <component v-else-if="!loading" :is="activeSection.component" :config="config"/>
        </div>
      </div>
    </section>
    <DialogRoot v-model:open="settingsNavOpen"><DialogPortal><DialogOverlay class="settings-overlay"/><DialogContent class="settings-mobile-drawer"><DialogTitle class="sr-only">设置分类</DialogTitle><DialogDescription class="sr-only">选择要修改的设置分类</DialogDescription><DialogClose class="settings-drawer-close" aria-label="关闭设置分类"><X :size="21"/></DialogClose><SettingsNavigation :categories="categories" :active="sectionName" @select="selectSection" @navigate="settingsNavOpen = false"/></DialogContent></DialogPortal></DialogRoot>
  </div>
</template>
<script setup>
import ThemeToggle from '@/components/app/ThemeToggle.vue'
import {computed, onMounted, provide, ref, watch} from "vue";
import {notify} from "@/lib/feedback";
import {md5} from "js-md5";
import ExcludeView from "@/view/config/ExcludeView.vue";
import NotificationView from "@/view/config/NotificationView.vue";
import ProxyView from "@/view/config/ProxyView.vue";
import DownloadView from "@/view/config/DownloadView.vue";
import PeerFilterSettings from "@/view/config/PeerFilterSettings.vue";
import {useRoute, useRouter} from 'vue-router'
import {ChevronLeft, Menu, Save, X} from '@lucide/vue'
import {DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle} from 'reka-ui'
import SettingsNavigation from '@/view/config/SettingsNavigation.vue'
import PageView from '@/view/config/basic/PageView.vue'
import AddSettings from '@/view/config/basic/AddView.vue'
import RenameView from '@/view/config/basic/RenameView.vue'
import RssView from '@/view/config/basic/RssView.vue'
import BangumiView from '@/view/config/basic/BangumiView.vue'
import OtherView from '@/view/config/basic/OtherView.vue'
import BackupView from '@/view/config/basic/BackupView.vue'
import MigrationView from '@/view/config/basic/MigrationView.vue'

import AboutView from "@/view/config/AboutView.vue";
import Security from "@/view/config/SecurityView.vue";

import {runtimeState} from '@/lib/runtime-state'
import {configData} from "@/js/config.js";
import * as http from "@/js/http.js";

const settingsSaveRevision = ref(0)
provide('settingsSaveRevision', settingsSaveRevision)
const configButtonLoading = ref(false)
const loading = ref(true)

const config = ref(configData)

const route = useRoute()
const router = useRouter()
const settingsNavOpen = ref(false)
const allCategories = [
  {label: '服务器', items: [
    {name: 'download', label: '下载设置', description: '连接 Transmission，设置下载目录和任务行为', component: DownloadView},
    {name: 'peerFilter', label: '节点过滤', description: '管理 Transmission 的封禁名单、节点识别与允许地址', component: PeerFilterSettings},
    {name: 'migration', label: '自动任务', description: '管理自动任务暂停与迁移状态', component: MigrationView},
    {name: 'proxy', label: '网络代理', description: '代理地址与连接配置', component: ProxyView},
    {name: 'security', label: '账号与安全', description: '登录账号与访问控制', component: Security},
  ]},
  {label: '订阅与媒体', items: [
    {name: 'add', label: '订阅默认值', description: '新订阅使用的默认规则', component: AddSettings},
    {name: 'rename', label: '文件命名', description: '剧集命名、目录与文件整理', component: RenameView},
    {name: 'rss', label: 'RSS', description: '资源订阅和刷新规则', component: RssView},
    {name: 'exclude', label: '全局排除', description: '统一排除不需要的资源'},
    {name: 'bangumi', label: 'Bangumi', description: '公开番剧资料的数据接口', component: BangumiView},
  ]},
  {label: '应用', items: [
    {name: 'page', label: '显示', description: '海报、主题色与页面行为', component: PageView},
    {name: 'notification', label: '通知', description: '消息渠道与事件通知', component: NotificationView},
    {name: 'backup', label: '备份与恢复', description: '备份配置与订阅数据', component: BackupView},
    {name: 'other', label: '其他', description: '缓存与应用行为', component: OtherView},
    {name: 'about', label: '关于', description: '版本信息与项目链接', component: AboutView},
  ]},
]
const categories = computed(() => allCategories.map(category => ({...category, items: category.items.filter(item => item.name !== 'migration' || runtimeState.developmentMode)})))
const sections = computed(() => categories.value.flatMap(category => category.items))
const sectionName = ref(sections.value.some(item => item.name === route.query.section) ? route.query.section : 'download')
const activeSection = computed(() => sections.value.find(item => item.name === sectionName.value) || sections.value[0])
const selectSection = name => {
  sectionName.value = name
  settingsNavOpen.value = false
  router.replace({path: '/settings', query: {section: name}})
}
watch(() => route.query.section, name => {
  if (route.path === '/settings' && sections.value.some(item => item.name === name)) sectionName.value = name
})

const loadConfig = () => {
  loading.value = true
  http.config()
      .then(res => {
        config.value = res.data
        if (res.data.developmentMode && res.data.automaticTasksPaused && !route.query.section) selectSection('migration')
        else if (sections.value.some(item => item.name === route.query.section)) sectionName.value = route.query.section
        else if (!sections.value.some(item => item.name === sectionName.value) || route.query.section === 'migration') selectSection('download')
      })
      .finally(() => {
        loading.value = false
      })
}

const saveConfig = () => {
  configButtonLoading.value = true
  let my_config = JSON.parse(JSON.stringify(config.value))

  let username = my_config.login.username.trim()
  let password = my_config.login.password.trim()

  my_config.login.username = username
  if (password) {
    my_config.login.password = md5(password);
  }

  http.setConfig(my_config)
      .then(res => {
        notify.success(res.message)
        settingsSaveRevision.value++
        window.$reLoadList?.()
      })
      .finally(() => {
        configButtonLoading.value = false
      })
}

onMounted(() => {
  loadConfig()
})
</script>
<style scoped>
.server-settings{height:100%;min-height:0;display:grid;grid-template-columns:228px minmax(0,1fr);background:var(--background)}.settings-desktop-nav{border-right:1px solid var(--border)}.settings-workspace{display:flex;flex-direction:column;min-height:0;min-width:0}.settings-content-header{display:flex;justify-content:space-between;align-items:center;gap:24px;padding:22px 40px 12px;flex-shrink:0;min-height:80px}.settings-title{display:flex;align-items:center;gap:10px}.settings-title h1{font-size:22px;font-weight:600;color:var(--foreground);line-height:1.4}.settings-header-icon{display:grid;place-items:center;width:36px;height:36px;border-radius:50%;color:var(--text-secondary)}.settings-header-icon:hover{background:var(--surface-hover)}.settings-menu{display:none}.settings-save{border-radius:999px;padding:9px 22px;min-height:36px;font-weight:400}.settings-form-scroll{flex:1;overflow:auto;min-height:0;scrollbar-width:thin}.settings-form{box-sizing:content-box;max-width:700px;padding:8px 40px 72px}.settings-description{font-size:14px;line-height:1.5;color:var(--text-secondary);margin-bottom:30px}.settings-form :deep(.settings-item){grid-template-columns:minmax(0,1fr);gap:6px;border:0;padding:12px 0 14px;position:relative}.settings-form :deep(.settings-item>label){font-size:14px;font-weight:400;line-height:1.5;color:var(--foreground)}.settings-form :deep(.settings-item>.settings-value){width:100%;max-width:700px}.settings-form :deep(.app-input),.settings-form :deep(.app-select){width:100%;max-width:100%}.settings-form :deep(.app-input__inner),.settings-form :deep(.app-select [data-slot=select-trigger]){width:100%}.settings-form :deep(textarea.app-input__inner){min-height:90px}.settings-form :deep(.settings-section-heading){margin:0 0 12px}.settings-form :deep(.settings-section-heading h3){font-size:21px;line-height:1.4;font-weight:600;color:var(--foreground)}.settings-form :deep(.settings-section+.settings-section){border:0;margin-top:32px;padding-top:0}.settings-form :deep(.settings-item:has(>.settings-value [data-slot=switch])){padding-right:62px;min-height:52px}.settings-form :deep(.settings-item:has(>.settings-value [data-slot=switch]) [data-slot=switch]){position:absolute;right:0;top:12px;width:42px;height:24px;border:0;box-shadow:none;background:var(--secondary)}.settings-form :deep([data-slot=switch][data-state=checked]){background:var(--primary)!important}.settings-form :deep(.app-collapse){border:0}.settings-form :deep(.app-collapse-item__header){background:transparent}.settings-form :deep(.app-alert){border-radius:6px}.settings-form :deep(.app-panel){box-shadow:none;border-color:var(--border)}.settings-overlay{position:fixed;inset:0;background:var(--dialog-overlay);z-index:80}.settings-mobile-drawer{position:fixed;inset:0 auto 0 0;width:min(290px,88vw);z-index:81;background:var(--card)}.settings-drawer-close{position:absolute;top:18px;right:14px;width:34px;height:34px;display:grid;place-items:center;color:var(--text-secondary);border-radius:50%;z-index:1}
@media(max-width:1100px){.server-settings{grid-template-columns:216px minmax(0,1fr)}.settings-content-header{padding:20px 24px 12px}.settings-form{padding:8px 32px 64px}}
@media(max-width:760px){.server-settings{display:flex;flex-direction:column}.settings-desktop-nav{display:none}.settings-workspace{flex:1}.settings-menu{display:grid}.settings-title>.settings-header-icon:first-child{display:none}.settings-content-header{padding:16px 16px 8px;min-height:68px;gap:12px}.settings-title h1{font-size:21px}.settings-form{box-sizing:border-box;max-width:none;width:100%;padding:8px 22px 56px}.settings-save{padding:8px 16px}.settings-description{font-size:14px;margin-bottom:24px}.settings-form :deep(.settings-section-heading h3){font-size:20px}.settings-form :deep(.app-input__inner),.settings-form :deep(.app-select [data-slot=select-trigger]){min-height:34px}}
</style>
