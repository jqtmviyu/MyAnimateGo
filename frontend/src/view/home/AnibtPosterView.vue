<script setup>
import PosterActionIcon from '@/components/app/PosterActionIcon.vue'
import {ref} from 'vue'
import {Check, Image, Star} from '@lucide/vue'
import {proxyImage} from '@/js/global'
import {anibtTime, anibtPremiere} from '@/lib/anibt-catalog'
defineProps({item: Object})
const emit = defineEmits(['open'])
const failed = ref(false)
</script>
<template>
  <article class="anibt-poster">
    <div class="media-poster-art">
      <img v-if="item.cover && !failed" :src="proxyImage(item.cover)" alt="" loading="lazy" @error="failed = true">
      <div v-else class="anibt-cover-empty"><Image :size="32"/><span>{{ item.title }}</span></div>
      <span class="anibt-air-time">{{ anibtTime(item.airingAt) }}</span>
      <button class="media-poster-action" :aria-label="`选择 ${item.title} 的订阅字幕组`" @click="emit('open', item)"><PosterActionIcon/></button>
      <span v-if="item.exists" class="anibt-subscribed" aria-label="已订阅"><Check :size="13"/></span>
    </div>
    <button class="media-poster-title anibt-title" :title="item.title" @click="emit('open', item)">{{ item.title }}</button>
    <p class="anibt-original" :title="item.originalTitle">{{ item.originalTitle === item.title ? item.format || '动画' : item.originalTitle }}</p>
    <div class="anibt-meta"><span>{{ item.format || '动画' }}<template v-if="item.episodes"> · 共 {{ item.episodes }} 集</template></span><span v-if="item.rating"><Star :size="11"/>{{ item.rating.toFixed(1) }}</span></div>
    <p class="anibt-release" :class="{available: item.rssReleaseCount > 0}">{{ item.rssReleaseCount > 0 ? `${item.rssReleaseCount} 条资源` : '暂无资源' }}<span v-if="anibtPremiere(item)"> · {{ anibtPremiere(item) }}</span></p>
  </article>
</template>
<style scoped>
.anibt-poster{min-width:0}.anibt-title{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;white-space:normal!important;overflow:hidden;line-height:1.5;min-height:3em;text-align:left}.anibt-air-time{position:absolute;top:8px;left:8px;padding:4px 7px;border-radius:4px;background:var(--media-overlay);color:var(--media-foreground);font-size:11px;pointer-events:none;font-variant-numeric:tabular-nums}.anibt-subscribed{position:absolute;top:6px;right:6px;display:grid;place-items:center;width:24px;height:24px;border-radius:50%;background:var(--primary);color:var(--primary-foreground);pointer-events:none}.anibt-original{font-size:11px;color:var(--text-subtle);overflow:hidden;white-space:nowrap;text-overflow:ellipsis;line-height:1.6;margin-top:3px;min-height:18px}.anibt-meta{display:flex;justify-content:space-between;gap:6px;color:var(--muted-foreground);font-size:11px;margin-top:8px}.anibt-meta>span:last-child{display:inline-flex;align-items:center;gap:4px}.anibt-meta svg{color:var(--warning);fill:var(--warning)}.anibt-release{color:var(--text-subtle);font-size:10px;margin-top:6px;line-height:1.5}.anibt-release.available{color:var(--success)}.anibt-release>span{color:var(--text-subtle)}.anibt-cover-empty{height:100%;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:12px;color:var(--text-subtle);text-align:center;padding:16px;font-size:12px}
</style>
