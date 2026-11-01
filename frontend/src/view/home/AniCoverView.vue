<template>
  <article class="group subscription-poster" :data-subscription-id="item.id">
    <div class="media-poster-art">
      <img v-if="item.cover && !imageFailed" :src="toApiFile(item.cover)" alt="" loading="lazy" class="h-full w-full object-cover" :class="{'grayscale opacity-55': !item.enable}" @error="imageFailed = true">
      <div v-else class="flex h-full w-full flex-col items-center justify-center gap-3 bg-muted text-primary/40"><Picture class="size-10" :stroke-width="1"/><span class="px-3 text-center text-xs">{{ item.title }}</span></div>
      <button type="button" class="media-poster-action z-10" :aria-label="`${coverActionLabel}：${item.title}`" @click="handleCoverClick"><PosterActionIcon :action="coverClickAction"/></button>
      <span v-if="scoreText" class="absolute left-2.5 top-2.5 z-20 flex items-center gap-1 rounded-md bg-black/60 px-2 py-1 text-[11px] font-semibold text-white backdrop-blur" :aria-label="`${item.title} 评分 ${scoreText}`"><Star class="size-3 fill-amber-400 text-amber-400"/>{{ scoreText }}</span>
      <span v-if="!item.enable" class="absolute bottom-2.5 left-2.5 z-20 rounded-md bg-black/60 px-2 py-1 text-[10px] text-white backdrop-blur">未启用</span>
      <span v-if="item.currentEpisodeNumber > 0" class="subscription-episode" :aria-label="`已更新至第 ${item.currentEpisodeNumber} 集`">{{ item.currentEpisodeNumber }}</span>
      <div class="absolute right-2 top-2 z-20 sm:opacity-0 sm:transition-opacity sm:group-hover:opacity-100 sm:group-focus-within:opacity-100" :class="{'!opacity-100': actionsVisible}">
        <AppDropdown @visible-change="actionsVisible = $event">
          <AppButton circle text :aria-label="`${item.title} 的订阅操作`" class="size-8 bg-card/90 text-foreground shadow-sm backdrop-blur"><MoreHorizontal class="size-4"/></AppButton>
          <template #dropdown><AppDropdownMenu>
            <AppDropdownItem @click="emit('cover', item)"><Picture class="size-4"/>更换封面</AppDropdownItem>
            <AppDropdownItem @click="openBgmUrl(item)"><ExternalLink class="size-4"/>在 Bangumi 查看</AppDropdownItem>
            <AppDropdownItem divided @click="refreshAni"><RefreshRight class="size-4"/>刷新订阅</AppDropdownItem>
            <AppDropdownItem divided @click="emit('edit', item)"><EditIcon class="size-4"/>编辑订阅</AppDropdownItem>
            <AppDropdownItem class="text-destructive focus:text-destructive" @click="emit('del', [item])"><Delete class="size-4"/>删除订阅</AppDropdownItem>
          </AppDropdownMenu></template>
        </AppDropdown>
      </div>
    </div>
    <button type="button" class="media-poster-title" :title="item.title" :aria-label="`${coverActionLabel}：${item.title}（标题）`" @click="handleCoverClick">{{ item.title }}</button>
    <div class="subscription-details">
      <p class="subscription-progress"><span>{{ seasonText }}</span><span>{{ progressText }}</span></p>
      <div class="subscription-source"><span class="subscription-subgroup" :title="item.subgroup || '未指定字幕组'">{{ item.subgroup || '未指定字幕组' }}</span><span v-if="backupCount" class="subscription-backups" :title="backupText" :aria-label="backupText">+{{ backupCount }}</span></div>
      <div class="subscription-meta"><span class="subscription-status" :class="{'is-enabled': item.enable}"><i aria-hidden="true"/>{{ item.enable ? '订阅中' : '已停用' }}</span><span :title="downloadText">{{ downloadText }}</span></div>
    </div>
  </article>
</template>

<script setup>
import PosterActionIcon from '@/components/app/PosterActionIcon.vue'
import {computed, ref, watch} from "vue";
import {Trash2 as Delete, Pencil as EditIcon, MoreHorizontal, ExternalLink, Image as Picture, RotateCw as RefreshRight, Star} from "@lucide/vue";
import {notify} from "@/lib/feedback";
import {coverClickAction, showScore, toApiFile} from "@/js/global.js";
import {formatDate} from "@/js/format.js";
import * as http from "@/js/http.js";

const coverActionLabel = computed(() => ({edit: '编辑订阅', cover: '更换封面'})[coverClickAction.value] || '更换封面')
const actionsVisible = ref(false)
const imageFailed = ref(false)
const props = defineProps(["item"])
watch(() => props.item.cover, () => { imageFailed.value = false })

const scoreText = computed(() => {
  if (!showScore.value) {
    return ''
  }
  const score = Number(props.item.score)
  return Number.isFinite(score) && score > 0 ? score.toFixed(1) : ''
})
const seasonText = computed(() => props.item.ova ? '剧场版 / OVA' : `第 ${props.item.season ?? 1} 季`)
const progressText = computed(() => {
  const current = Number(props.item.currentEpisodeNumber) || 0
  const total = Number(props.item.totalEpisodeNumber) || 0
  if (total > 0) return `${current} / ${total} 集`
  return current > 0 ? `进度 ${current} 集` : '等待更新'
})
const backupCount = computed(() => props.item.standbyRssList?.length || 0)
const backupText = computed(() => `备用 RSS：${(props.item.standbyRssList || []).map(item => item.label || '未命名来源').join('、')}`)
// 此字段记录下载任务时间，和 Mikan 上的资源更新日期含义不同。
const downloadText = computed(() => props.item.lastDownloadTime > 0 ? `最近下载 ${formatDate(props.item.lastDownloadTime)}` : '暂无下载记录')

// 从封面菜单直接刷新当前订阅。
const refreshAni = () => {
  http.refreshAni(props.item)
      .then(res => notify.success(res.message))
}


const handleCoverClick = () => {
  const action = ['edit', 'cover'].includes(coverClickAction.value)
      ? coverClickAction.value
      : 'cover'
  emit(action, props.item)
}

const openBgmUrl = it => {
  if (it.bgmUrl?.length) {
    window.open(it.bgmUrl, '_blank', 'noopener')
    return
  }
  if (it.title?.length) {
    let title = it.title.replace(/ ?\((19|20)\d{2}\)/g, "").trim()
    title = title.replace(/ ?\[tmdbid=(\d+)]/g, "").trim()
    window.open(`https://bgm.tv/subject_search/${encodeURIComponent(title)}?cat=2`, '_blank', 'noopener')
  }
}

const emit = defineEmits(['edit', 'cover', 'del'])
</script>

<style scoped>
.subscription-poster{min-width:0;text-align:center}.subscription-episode{position:absolute;top:5px;right:5px;z-index:12;min-width:24px;height:24px;border-radius:999px;display:grid;place-items:center;padding:0 5px;background:var(--primary);color:var(--primary-foreground);font-size:11px;pointer-events:none}.subscription-poster:hover .subscription-episode,.subscription-poster:focus-within .subscription-episode{opacity:0}
.subscription-poster>.media-poster-title{white-space:normal;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;line-height:1.5;min-height:3em;text-align:left;margin-top:10px}
.subscription-details{text-align:left;margin-top:6px;display:flex;flex-direction:column;gap:8px;min-width:0}.subscription-progress{display:flex;align-items:center;justify-content:space-between;gap:8px;font-size:12px;line-height:1.5;color:var(--text-secondary);font-variant-numeric:tabular-nums}.subscription-progress>span:last-child{color:var(--text-secondary)}.subscription-source{display:flex;align-items:center;gap:5px;min-width:0;height:23px}.subscription-subgroup,.subscription-backups{display:block;font-size:10px;line-height:21px;border:1px solid var(--border);border-radius:4px;padding:0 7px;background:var(--subtle-background);color:var(--muted-foreground)}.subscription-subgroup{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}.subscription-backups{flex-shrink:0;color:var(--text-secondary)}.subscription-meta{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:4px 8px;font-size:10px;line-height:1.6;color:var(--text-subtle)}.subscription-status{display:inline-flex;align-items:center;gap:5px;white-space:nowrap;color:var(--muted-foreground)}.subscription-status i{width:5px;height:5px;border-radius:50%;background:var(--secondary)}.subscription-status.is-enabled i{background:var(--primary)}
@media(max-width:600px){.subscription-progress{font-size:11px}.subscription-details{gap:7px}.subscription-meta{font-size:10px}}
</style>
