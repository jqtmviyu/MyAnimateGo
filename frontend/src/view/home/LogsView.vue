<template>
  <div class="logs-page app-page-layout">
    <h1 class="sr-only">日志</h1>
    <LibraryToolbar>
      <template #filters>
        <span class="media-library-total">{{ filteredLogs.length }} / {{ logs.length }} 条</span>
        <AppSelect v-model="selectLevels" aria-label="日志等级" class="log-level-select" clearable multiple placeholder="全部级别">
          <AppOption v-for="level in levels" :key="level" :label="level" :value="level"><div class="level-option"><span class="level-option-name"><span class="level-dot" :class="`is-${level.toLowerCase()}`"/>{{ level }}</span><span class="level-count">{{ levelCounts[level] }}</span></div></AppOption>
        </AppSelect>
        <AppSelect v-model="selectLoggerNames" aria-label="日志来源" class="log-source-select" clearable multiple filterable placeholder="全部来源"><AppOption v-for="loggerName in loggerNames" :key="loggerName" :label="loggerName" :value="loggerName"/></AppSelect>
      </template>
      <template #search><form class="media-library-search" data-focus-container role="search" @submit.prevent><input v-model="searchText" type="search" aria-label="搜索日志" placeholder="搜索日志内容、来源或级别"><button type="submit" aria-label="搜索日志"><Search :size="17"/></button></form></template>
      <AppButton class="media-library-action" icon="Download" @click="downloadLogs">下载日志</AppButton>
      <AppButton class="media-library-action" icon="Refresh" :loading="getLogsLoading" @click="getLogs">刷新</AppButton>
      <AppButton class="media-library-action log-clear" icon="Delete" :loading="clearLoading" @click="confirmClearLogs">清空</AppButton>
    </LibraryToolbar>
    <section v-busy="loading" class="log-viewer" aria-label="日志记录">
      <div class="log-columns" aria-hidden="true"><span>时间</span><span>级别</span><span>来源</span><span>内容</span></div>
      <div ref="scrollbarRef" class="logs-scrollbar" tabindex="0" aria-label="日志列表">
        <div ref="innerRef" class="log-list">
          <AppEmpty v-if="!filteredLogs.length" :description="logs.length ? '没有匹配的日志，试试其他关键词或筛选条件' : '暂无日志'"/>
          <article v-for="(entry, index) in filteredLogs" v-else :key="`${entry.timestamp}-${index}`" class="log-entry" :class="`level-${entry.level.toLowerCase()}`">
            <AppTooltip :content="entry.timestamp"><time class="log-time">{{ entry.time }}</time></AppTooltip>
            <span class="log-level">{{ entry.level }}</span>
            <AppTooltip :content="entry.loggerName"><div class="log-source"><span class="logger-name">{{ entry.shortLoggerName }}</span><span v-if="entry.threadName" class="thread-name">{{ entry.threadName }}</span></div></AppTooltip>
            <pre class="log-message">{{ entry.message }}</pre>
          </article>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import {computed, nextTick, onActivated, ref} from "vue";
import {Search} from "@lucide/vue";
import {dialogs} from "@/lib/dialogs";
import {authorization} from "@/js/global.js";
import LibraryToolbar from "./LibraryToolbar.vue";
import * as http from "@/js/http.js";
import {formatTime} from "@/js/format.js";

const levels = ['DEBUG', 'INFO', 'WARN', 'ERROR']
const loading = ref(true)
const getLogsLoading = ref(false)
const clearLoading = ref(false)
const logs = ref([])
const searchText = ref('')
const selectLevels = ref([])
const selectLoggerNames = ref([])
const scrollbarRef = ref()
const innerRef = ref()

const loggerNames = computed(() => Array.from(new Set(
    logs.value.map(item => item.loggerName).filter(Boolean)
)).sort())

const levelCounts = computed(() => Object.fromEntries(
    levels.map(level => [level, logs.value.filter(item => item.level === level).length])
))

const normalizedLogs = computed(() => logs.value.map(item => {
  const {loggerName, ts} = item

  const timestamp = formatTime(ts)

  return {
    ...item,
    timestamp,
    time: timestamp.slice(11),
    shortLoggerName: loggerName.split('.').pop()
  }
}))

const filteredLogs = computed(() => {
  const keyword = searchText.value.trim().toLowerCase()
  return normalizedLogs.value.filter(item => {
    if (selectLevels.value.length && !selectLevels.value.includes(item.level)) {
      return false
    }
    if (selectLoggerNames.value.length && !selectLoggerNames.value.includes(item.loggerName)) {
      return false
    }
    if (!keyword) {
      return true
    }
    return [item.message, item.loggerName, item.threadName, item.level]
        .filter(Boolean)
        .some(value => value.toLowerCase().includes(keyword))
  })
})

const scrollToBottom = async () => {
  await nextTick()
  scrollbarRef.value?.scrollTo({top: innerRef.value?.scrollHeight || 0})
}

const getLogs = () => {
  getLogsLoading.value = true
  if (!logs.value.length) {
    loading.value = true
  }
  return http.logs()
      .then(async res => {
        logs.value = res.data || []
        selectLoggerNames.value = selectLoggerNames.value
            .filter(loggerName => loggerNames.value.includes(loggerName))
        await scrollToBottom()
      })
      .finally(() => {
        loading.value = false
        getLogsLoading.value = false
      })
}

const clearLogs = () => {
  clearLoading.value = true
  http.clearLogs()
      .then(() => {
        selectLoggerNames.value = []
        return getLogs()
      })
      .finally(() => {
        clearLoading.value = false
      })
}

// 清理操作不可恢复，执行接口前通过模态弹窗进行二次确认。
const confirmClearLogs = () => {
  dialogs.confirm(
      '日志清空后无法恢复，是否继续？',
      '清空日志',
      {
        confirmButtonText: '确认清空',
        confirmButtonClass: 'is-text is-has-bg app-button--danger',
        cancelButtonText: '取消',
        cancelButtonClass: 'is-text is-has-bg',
        type: 'warning'
      }
  ).then(clearLogs).catch(() => {
  })
}

const downloadLogs = () => {
  window.open(`api/downloadLogs?s=${authorization.value}`)
}

onActivated(getLogs)
</script>

<style scoped>
.logs-page{min-width:0}.log-level-select{width:148px;min-width:0}.log-source-select{width:180px;min-width:0}.logs-page :deep(.media-library-filters){flex-wrap:wrap}.log-clear{color:var(--destructive)}.log-clear:hover{background:var(--danger-soft);color:var(--destructive)}
.level-option,.level-option-name{display:flex;align-items:center;gap:7px}.level-option{width:100%;justify-content:space-between;gap:18px}.level-count{font-size:11px;color:var(--muted-foreground)}.level-dot{width:6px;height:6px;border-radius:50%;background:var(--secondary);flex-shrink:0}.level-dot.is-info{background:var(--success)}.level-dot.is-warn{background:var(--warning)}.level-dot.is-error{background:var(--destructive)}
.log-viewer{display:flex;flex:1;flex-direction:column;min-height:0;min-width:0;overflow:hidden;background:transparent}.log-columns,.log-entry{display:grid;grid-template-columns:82px 64px minmax(120px,190px) minmax(0,1fr);gap:18px;padding-inline:var(--media-gutter)}.log-columns{flex-shrink:0;padding-block:12px;border-bottom:1px solid var(--border);color:var(--muted-foreground);font-size:11px}.logs-scrollbar{flex:1;min-height:0;overflow:auto;scrollbar-gutter:stable;outline:none;padding-bottom:32px}.logs-scrollbar:focus-visible{outline:2px solid var(--ring);outline-offset:-2px}.log-list{min-height:100%}.log-list :deep(.app-empty){padding:60px 20px}.log-entry{align-items:start;padding-block:13px;border-bottom:1px solid var(--border);transition:background-color .12s}.log-entry:hover{background:var(--subtle-background)}.log-entry.level-error{background:var(--danger-soft)}
.log-time,.log-message,.log-level{font-family:"SFMono-Regular",Consolas,"Liberation Mono",monospace}.log-time{font-size:11px;line-height:1.8;white-space:nowrap;color:var(--muted-foreground)}.log-level{width:fit-content;min-width:46px;border-radius:4px;padding:3px 6px;background:var(--subtle-background);color:var(--muted-foreground);font-size:10px;line-height:1.4;text-align:center}.level-info .log-level{color:var(--success);background:var(--primary-soft)}.level-warn .log-level{color:var(--warning);background:var(--danger-soft)}.level-error .log-level{color:var(--destructive);background:var(--danger-soft)}.log-source{min-width:0;display:flex;flex-direction:column;gap:3px}.logger-name,.thread-name{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.logger-name{font-size:12px;line-height:1.6;color:var(--text-secondary)}.thread-name{font-size:10px;color:var(--text-subtle)}.log-message{margin:0;min-width:0;white-space:pre-wrap;overflow-wrap:anywhere;font-size:12px;line-height:1.8;color:var(--text-secondary)}
@media(max-width:700px){.log-columns{display:none}.log-entry{grid-template-columns:70px 52px minmax(0,1fr);gap:8px 12px;padding-block:14px}.log-message{grid-column:1/-1}.thread-name{display:none}.log-level-select,.log-source-select{width:132px}}
</style>
