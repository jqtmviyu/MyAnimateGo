<script setup>
import PosterActionIcon from '@/components/app/PosterActionIcon.vue'
import {ref} from 'vue'
import {Check, Image} from '@lucide/vue'
import {proxyImage} from '@/js/global'
import {mikanPosterURL} from '@/lib/mikan-poster'
defineProps({item: Object})
const emit = defineEmits(['open'])
const imageFailed = ref(false)
</script>
<template>
  <article class="mikan-poster">
    <div class="mikan-art media-poster-art">
      <img v-if="item.cover && !imageFailed" :src="proxyImage(mikanPosterURL(item.cover))" alt="" loading="lazy" @error="imageFailed = true">
      <div v-else class="mikan-cover-empty"><Image :size="36"/><span>{{ item.title }}</span></div>
      <button class="mikan-open media-poster-action" :aria-label="`选择 ${item.title} 的订阅字幕组`" @click="emit('open', item)"><PosterActionIcon/></button>
      <span v-if="item.exists" class="mikan-exists" aria-label="已订阅"><Check :size="13"/></span>
    </div>
    <button :title="item.title" class="mikan-title media-poster-title" @click="emit('open', item)">{{ item.title }}</button>
    <p class="mikan-update media-poster-date">{{ item.updateDate || '暂无更新日期' }}</p>
  </article>
</template>
<style scoped>
.mikan-poster{min-width:0}.mikan-exists{position:absolute;top:5px;right:5px;display:grid;place-items:center;background:var(--primary);color:var(--primary-foreground);width:24px;height:24px;border-radius:50%;pointer-events:none}.mikan-cover-empty{height:100%;display:flex;flex-direction:column;justify-content:center;align-items:center;gap:12px;color:var(--text-subtle);padding:16px;text-align:center;font-size:12px}
</style>
