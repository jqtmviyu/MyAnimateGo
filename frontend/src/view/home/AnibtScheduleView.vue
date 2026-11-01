<script setup>
import {computed, onMounted, onUnmounted, ref, onActivated, onDeactivated} from 'vue'
import {Clock3, ArrowUpRight} from '@lucide/vue'
import {anibtToday, anibtMinutes, anibtTime} from '@/lib/anibt-catalog'
const props = defineProps({weeks: {type:Array, default:()=>[]}})
const emit = defineEmits(['open', 'today'])
const now = ref(new Date())
let timer
const start = () => { clearInterval(timer); now.value = new Date(); timer = setInterval(() => {now.value = new Date()}, 30000) }
onMounted(start); onActivated(start); onDeactivated(() => clearInterval(timer)); onUnmounted(() => clearInterval(timer))
const today = computed(() => props.weeks.find(week => week.weekday === anibtToday(now.value)))
const items = computed(() => today.value?.items || [])
const minutes = computed(() => anibtMinutes(now.value.getTime()/1000))
const scheduled = computed(() => items.value.filter(item => item.airingAt))
const aired = computed(() => scheduled.value.filter(item => anibtMinutes(item.airingAt) <= minutes.value).length)
const next = computed(() => scheduled.value.filter(item => anibtMinutes(item.airingAt) > minutes.value).sort((a,b)=>anibtMinutes(a.airingAt)-anibtMinutes(b.airingAt))[0])
const progress = computed(() => scheduled.value.length ? aired.value / scheduled.value.length * 100 : 0)
</script>
<template>
  <section class="anibt-schedule" aria-label="今日放送时间表">
    <button class="schedule-heading" @click="emit('today', today?.weekLabel)"><Clock3 :size="20"/><span><strong>今日放送</strong><small>{{ today?.weekLabel || '今天' }} · {{ items.length }} 部</small></span></button>
    <div class="schedule-progress"><div><time>{{ anibtTime(now.getTime()/1000) }}</time><span>本地时间 · 已播 {{ aired }} · 待播 {{ scheduled.length - aired }} · 时间待定 {{ items.length - scheduled.length }}</span></div><div class="schedule-track"><i :style="{width: `${progress}%`}"/></div></div>
    <button v-if="next" class="schedule-next" @click="emit('open', next)"><span>下一部 · {{ anibtTime(next.airingAt) }}</span><strong>{{ next.title }}</strong><ArrowUpRight :size="17"/></button>
    <p v-else class="schedule-finished">{{ items.length ? '今日暂无后续放送' : '今日暂无放送安排' }}</p>
  </section>
</template>
<style scoped>
.anibt-schedule{display:flex;align-items:center;gap:30px;margin:4px 0 28px;padding:20px 24px;background:var(--subtle-background);border:1px solid var(--border);border-radius:8px}.schedule-heading{display:flex;align-items:center;gap:12px;text-align:left;flex-shrink:0}.schedule-heading>svg{color:var(--success)}.schedule-heading span{display:flex;flex-direction:column;gap:5px}.schedule-heading strong{font-weight:500;font-size:17px;color:var(--foreground)}.schedule-heading small{font-size:11px;color:var(--muted-foreground)}.schedule-progress{flex:1;min-width:140px}.schedule-progress>div:first-child{display:flex;align-items:baseline;gap:14px;flex-wrap:wrap}.schedule-progress time{font-size:25px;font-variant-numeric:tabular-nums;color:var(--foreground);line-height:1.3}.schedule-progress span{font-size:10px;color:var(--muted-foreground)}.schedule-track{height:3px;background:var(--surface-raised);margin-top:12px;border-radius:4px;overflow:hidden}.schedule-track i{display:block;height:100%;background:var(--primary)}.schedule-next{width:31%;min-width:0;text-align:left;display:grid;grid-template-columns:1fr auto;gap:5px 12px;border-left:1px solid var(--border);padding-left:24px}.schedule-next span{font-size:10px;color:var(--success);grid-column:1}.schedule-next strong{font-size:13px;font-weight:400;white-space:nowrap;text-overflow:ellipsis;overflow:hidden;color:var(--foreground);grid-column:1}.schedule-next svg{grid-column:2;grid-row:1/3;align-self:center;color:var(--muted-foreground)}.schedule-next:hover strong{color:var(--foreground)}.schedule-finished{font-size:12px;color:var(--muted-foreground)}@media(max-width:700px){.anibt-schedule{gap:18px;padding:16px;flex-wrap:wrap}.schedule-heading strong{font-size:15px}.schedule-progress{min-width:100px}.schedule-progress span{font-size:9px}.schedule-next{width:100%;border-left:0;border-top:1px solid var(--border);padding:12px 0 0}.schedule-progress time{font-size:20px}}
</style>
