<script setup>
import {LoaderCircle} from '@lucide/vue'
import AniView from './AniView.vue'
import SubscriptionDraftView from './SubscriptionDraftView.vue'
defineProps({entry: Object, compact: Boolean})
const emit = defineEmits(['confirm', 'retry'])
</script>
<template>
  <div v-if="entry.status === 'loading'" class="entry-loading" role="status"><LoaderCircle :size="27" class="animate-spin"/><p>正在读取订阅信息</p><span>解析 RSS 与番剧信息，完成后即可确认。</span></div>
  <div v-else-if="entry.status === 'error'" class="entry-error"><AppAlert :title="entry.error" type="error" show-icon :closable="false"/><AppButton @click="emit('retry')">重新读取</AppButton></div>
  <div v-else-if="entry.ani" class="entry-editor"><SubscriptionDraftView v-if="compact" :ani="entry.ani" @confirm="emit('confirm', $event)"/><AniView v-else :ani="entry.ani" @callback="emit('confirm', $event)"/></div>
</template>
<style scoped>
.entry-loading{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:230px;gap:14px;color:var(--text-secondary)}.entry-loading>svg{color:var(--primary)}.entry-loading p{font-size:14px;color:var(--text-secondary)}.entry-loading span{font-size:11px;color:var(--muted-foreground);text-align:center;line-height:1.7}.entry-error{display:flex;flex-direction:column;align-items:flex-start;gap:20px;padding:20px 0}.entry-editor{width:100%;max-width:100%;min-width:0;overflow:hidden}.entry-editor :deep(.app-tabs),.entry-editor :deep(.app-tab-content){width:100%;max-width:100%;min-width:0}
</style>
