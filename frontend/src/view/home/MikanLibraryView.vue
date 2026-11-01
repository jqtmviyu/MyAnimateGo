<script setup>
import {computed, nextTick, onMounted, ref, watch} from 'vue'
import {ChevronDown, ChevronLeft, ChevronRight, ExternalLink, Plus, RefreshCw, Search} from '@lucide/vue'
import * as http from '@/js/http'
import {proxyImage} from '@/js/global'
import {useWeekNavigation, weekdays as days} from '@/lib/week-navigation'
import {mikanPosterURL} from '@/lib/mikan-poster'
import MikanPosterView from './MikanPosterView.vue'
import AnibtPosterView from './AnibtPosterView.vue'
import AnibtScheduleView from './AnibtScheduleView.vue'
import {normalizeAnibt, normalizeAnibtGroups, anibtSubscribedIds, anibtSeasonOptions} from '@/lib/anibt-catalog'
import LibraryToolbar from './LibraryToolbar.vue'
import BatchAdditionView from './BatchAdditionView.vue'

const props = defineProps({subscriptions: {type: Array, default: null}, source: {type: String, default: 'anibt'}, active: {type: Boolean, default: true}})
const isAniBT = computed(() => props.source === 'anibt')
const sourceName = computed(() => isAniBT.value ? 'Anibt' : 'Mikan')
const truncated = ref(false)
const currentSeason = ref('')
const currentSchedule = computed(() => !season.value || season.value === currentSeason.value)
const emit = defineEmits(['subscribed', 'source-change'])
const weeks = ref([])
const seasons = ref([])
const season = ref('')
const query = ref('')
const searchInput = ref()
const focusSearch = async () => {
  await nextTick()
  searchInput.value?.focus()
}
const catalogScroll = ref()
const categoryId = label => `${props.source}-category-${label}`
const loading = ref(false)
const loadError = ref('')
const groups = ref({})
const groupState = ref({})
const selected = ref(null)
const groupDialog = ref(false)
const flowStage = ref('groups')
const ruleGroups = ref([])
const expandedGroups = ref([])
const groupHeading = ref()
const confirmationHeading = ref()
const highlightedRSS = ref('')
const selectedGroups = ref([])
const matchSelection = ref({})
const batchVisible = ref(false)
const batchRef = ref()
const batchItems = ref([])
const groupPromises = new Map()
const queue = []
let activeRequests = 0
let generation = 0
const subscribedIds = computed(() => isAniBT.value ? anibtSubscribedIds(props.subscriptions) : new Set((props.subscriptions || []).map(item => new URL(item.url, 'https://mikanani.me').searchParams.get('bangumiId')).filter(Boolean)))
// 目录保留作品资料，订阅标记始终使用列表刷新后的快照。
const orderedWeeks = computed(() => weeks.value.map(day => ({...day, items: day.items.map(item => ({...item, exists: props.subscriptions === null ? item.exists : subscribedIds.value.has(isAniBT.value ? String(item.bgmId) : new URL(item.url).pathname.match(/(\d+)\/?$/)?.[1])}))})).sort((a, b) => {
  const left = days.indexOf(a.weekLabel), right = days.indexOf(b.weekLabel)
  return (left < 0 ? 7 : left) - (right < 0 ? 7 : right)
}))
const {activeCategory, setCategoryElement, scrollToCategory, syncCategory, resetPosition, requestToday} = useWeekNavigation({container: catalogScroll, categories: orderedWeeks, active: () => props.active, grouped: () => !query.value.trim(), loading})
const total = computed(() => weeks.value.reduce((count, item) => count + item.items.length, 0))
const displayedGroups = computed(() => groups.value[selected.value?.url] || [])

const load = async (options = {}) => {
  const previousScroll = catalogScroll.value?.scrollTop || 0
  const request = ++generation
  loading.value = true
  if (!options.preservePosition && !query.value.trim()) requestToday()
  loadError.value = ''
  try {
    const chosenSeason = seasons.value.find(item => item.seasonLabel === season.value)
    const {data} = isAniBT.value
      ? await http.aniBT(season.value, '', query.value.trim(), {refresh: options.refresh === true})
      : await http.mikan(query.value.trim(), chosenSeason || {}, {metadata: false, refresh: options.refresh === true})
    if (request !== generation) return
    truncated.value = !!data.truncated
    weeks.value = isAniBT.value ? normalizeAnibt(data) : (data.weeks || []).map(item => ({...item, weekLabel: item.weekLabel === 'Search' ? '搜索结果' : item.weekLabel}))
    if (isAniBT.value) {
      currentSeason.value = data.currentSeason || ''
      seasons.value = anibtSeasonOptions(currentSeason.value)
      if (!season.value) season.value = data.requestedSeason || currentSeason.value
    } else if (data.seasons?.length) {
      seasons.value = data.seasons
      if (!season.value) season.value = data.seasons.find(item => item.select)?.seasonLabel || ''
    }
    await nextTick()
    if (options.preservePosition && catalogScroll.value) { catalogScroll.value.scrollTop = previousScroll; syncCategory() }
    else if (query.value.trim()) await resetPosition()
    for (const item of weeks.value.flatMap(day => day.items)) {
      if (item.groups?.length) { groups.value[item.url] = item.groups; groupState.value[item.url] = 'loaded' }
    }
  } catch (error) {
    if (request === generation) loadError.value = error.message || `${sourceName.value} 目录加载失败`
  } finally { if (request === generation) loading.value = false }
}

// 字幕组只在打开订阅弹窗时请求，相同作品复用已读取的结果。
const drainGroups = () => {
  while (activeRequests < 3 && queue.length) {
    const {item, resolve, reject} = queue.shift()
    activeRequests++
    groupState.value[item.url] = 'loading'
    const request = isAniBT.value ? http.aniBTGroup(item.bgmId) : http.mikanGroup(item.url)
    request.then(result => {
      groups.value[item.url] = isAniBT.value ? normalizeAnibtGroups(result.data || []) : result.data || []
      groupState.value[item.url] = 'loaded'
      resolve(groups.value[item.url])
    }).catch(error => { groupState.value[item.url] = 'error'; reject(error) }).finally(() => {
      activeRequests--
      groupPromises.delete(item.url)
      drainGroups()
    })
  }
}
const ensureGroups = item => {
  if (groupState.value[item.url] === 'loaded') return Promise.resolve(groups.value[item.url])
  if (groupPromises.has(item.url)) return groupPromises.get(item.url)
  const promise = new Promise((resolve, reject) => queue.push({item, resolve, reject}))
  groupPromises.set(item.url, promise)
  drainGroups()
  return promise
}
const openGroups = async (item, rss = '') => {
  selected.value = item
  highlightedRSS.value = rss
  selectedGroups.value = []
  matchSelection.value = {}
  flowStage.value = 'groups'
  ruleGroups.value = []
  expandedGroups.value = []
  batchVisible.value = false
  groupDialog.value = true
  try {
    const available = await ensureGroups(item)
    if (!groupDialog.value || selected.value?.url !== item.url) return
    if (rss && selected.value?.url === item.url) {
      await nextTick()
      document.getElementById('selected-mikan-group')?.scrollIntoView({block: 'nearest'})
    }
  } catch { /* 弹窗提供重试入口，请求层显示错误详情。 */ }
}
const ruleOptions = group => group.groupRegex?.regexList || []
const chooseGroups = async sources => {
  ruleGroups.value = sources
  for (const group of sources) matchSelection.value[group.rss] ??= '[]'
  flowStage.value = 'rules'
  await nextTick()
  confirmationHeading.value?.focus()
}
const toggleResources = rss => {
  expandedGroups.value = expandedGroups.value.includes(rss) ? expandedGroups.value.filter(value => value !== rss) : [...expandedGroups.value, rss]
}
const sourceWithRules = group => ({...group, displayTitle: selected.value.title, match: JSON.parse(matchSelection.value[group.rss] || '[]')})
const beginSubscription = async sources => {
  batchItems.value = sources.map(group => JSON.stringify(sourceWithRules(group)))
  flowStage.value = 'confirm'
  batchVisible.value = true
  await nextTick()
  confirmationHeading.value?.focus()
  await batchRef.value.open(batchItems.value)
}
const beginSelected = () => {
  const available = groups.value[selected.value.url] || []
  chooseGroups(selectedGroups.value.map(rss => available.find(group => group.rss === rss)))
}
const returnToGroups = async () => {
  flowStage.value = 'groups'
  batchVisible.value = false
  await nextTick()
  groupHeading.value?.focus()
}
const subscribed = () => emit('subscribed')
watch(groupDialog, open => { if (!open) batchVisible.value = false })
onMounted(load)
defineExpose({reload: load, focusSearch})
</script>
<template>
  <div class="mikan-library">
    <LibraryToolbar>
      <template #search><form class="media-library-search" data-focus-container role="search" @submit.prevent="load"><input ref="searchInput" v-model="query" type="search" :aria-label="`搜索 ${sourceName} 番剧`" :placeholder="`搜索 ${sourceName} 番剧`"><button type="submit" :aria-label="`提交 ${sourceName} 搜索`"><Search :size="17"/></button></form></template>
      <template #filters><div class="catalog-sources" role="group" aria-label="番剧来源"><button :aria-pressed="isAniBT" @click="emit('source-change', 'anibt')">Anibt</button><button :aria-pressed="!isAniBT" @click="emit('source-change', 'mikan')">Mikan</button></div><span class="media-library-total">{{ total }} 项</span></template>
      <AppSelect v-if="seasons.length" v-model="season" class="catalog-season-select" :class="{'anibt-season-select': isAniBT}" :disabled="!!query.trim()" :aria-label="`${sourceName} 季度`" @change="load"><AppOption v-if="!isAniBT" label="当前季度" value=""/><AppOption v-for="item in seasons" :key="item.seasonLabel" :label="item.label || item.seasonLabel" :value="item.seasonLabel"/></AppSelect>
      <button class="media-library-icon" :disabled="loading" :aria-label="`刷新 ${sourceName} 目录`" @click="load({refresh: true, preservePosition: true})"><RefreshCw :size="17" :class="{'animate-spin': loading}"/></button>
    </LibraryToolbar>
    <nav class="media-library-categories" aria-label="番剧分类目录"><button v-for="item in orderedWeeks" :key="item.weekLabel" :aria-current="activeCategory === item.weekLabel ? 'location' : undefined" :aria-controls="categoryId(item.weekLabel)" @click="scrollToCategory(item.weekLabel)">{{ item.weekLabel }}<span>{{ item.items.length }}</span></button></nav>
    <div ref="catalogScroll" class="library-scroll media-library-scroll" v-busy="loading" @scroll.passive="syncCategory">
      <AppEmpty v-if="loadError && !loading" :description="loadError"><AppButton @click="load">重新加载</AppButton></AppEmpty>
      <AppEmpty v-else-if="!total && !loading" :description="`${sourceName} 中没有匹配的番剧`"/>
      <AnibtScheduleView v-if="isAniBT && currentSchedule && !query.trim() && total" :weeks="orderedWeeks" @open="openGroups" @today="label => label && scrollToCategory(label)"/>
      <p v-if="truncated" class="catalog-notice" role="status">搜索结果较多，当前显示前 {{ total }} 部，请补充关键词缩小范围。</p>
      <section v-for="day in orderedWeeks" :id="categoryId(day.weekLabel)" :ref="element => setCategoryElement(day.weekLabel, element)" :key="day.weekLabel" class="library-day media-library-day">
        <h2>{{ day.weekLabel }}<span>{{ day.items.length }} 部番剧</span></h2>
        <div class="media-poster-grid"><component :is="isAniBT ? AnibtPosterView : MikanPosterView" v-for="item in day.items" :key="item.url" :item="item" @open="openGroups"/></div>
      </section>
    </div>
    <AppModal v-model="groupDialog" title="添加订阅" width="min(760px, calc(100vw - 24px))" class="mikan-subscribe-dialog" @opened="groupHeading?.focus()">
      <template v-if="selected">
        <div class="subscribe-identity"><img v-if="selected.cover" :src="proxyImage(mikanPosterURL(selected.cover))" alt=""><div><h3>{{ selected.title }}</h3><p>{{ sourceName }}<span v-if="selected.updateDate"> · {{ selected.updateDate }}</span></p><a :href="selected.url" target="_blank" rel="noopener noreferrer">在 {{ sourceName }} 查看<ExternalLink :size="12"/></a></div></div>
        <div class="subscribe-stage"><template v-if="flowStage === 'groups'"><h3 ref="groupHeading" tabindex="-1">选择字幕组</h3><span>展开查看资源，点击添加订阅</span></template><template v-else><button aria-label="返回选择字幕组" @click="returnToGroups"><ChevronLeft :size="16"/>字幕组</button><h3 ref="confirmationHeading" tabindex="-1">{{ flowStage === 'rules' ? '选择匹配规则' : '确认订阅' }}</h3></template></div>
        <div v-show="flowStage === 'groups'">
          <div v-if="groupState[selected.url] === 'error'" class="groups-message"><p>字幕组加载失败</p><AppButton @click="openGroups(selected)">重新加载</AppButton></div>
          <p v-else-if="groupState[selected.url] !== 'loaded'" class="groups-message" role="status">正在加载字幕组…</p>
          <AppEmpty v-else-if="!displayedGroups.length" description="暂无可用字幕组"/>
          <div v-else class="groups-rows">
            <section v-for="group in displayedGroups" :id="highlightedRSS === group.rss ? 'selected-mikan-group' : undefined" :key="group.rss" class="subtitle-group" :class="{'chosen-group': selectedGroups.includes(group.rss)}">
              <div class="group-row">
                <input v-model="selectedGroups" type="checkbox" :value="group.rss" :aria-label="`选择 ${group.label}`">
                <button class="group-expand" :aria-label="`查看 ${group.label} 的资源`" :aria-expanded="expandedGroups.includes(group.rss)" @click="toggleResources(group.rss)">
                  <span class="group-copy"><span class="group-heading"><span class="group-name">{{ group.label }}</span><span class="group-update">{{ group.updateDay }}</span><span v-if="isAniBT && group.totalReleases" class="group-update">{{ group.totalReleases }} 条资源</span><span v-if="selectedGroups.includes(group.rss)" class="group-role">{{ selectedGroups[0] === group.rss ? '主 RSS' : '备用' }}</span></span><span v-if="group.groupRegex?.tags?.length" class="group-tags"><span v-for="tag in group.groupRegex.tags" :key="tag">{{ tag }}</span></span></span>
                  <ChevronDown :size="16" :class="{'rotate-180': expandedGroups.includes(group.rss)}"/>
                </button>
                <AppButton size="small" :aria-label="`添加 ${group.label} 的订阅`" @click="chooseGroups([group])"><Plus :size="14"/>添加</AppButton>
              </div>
              <div v-if="expandedGroups.includes(group.rss)" class="group-resources"><p v-if="isAniBT && group.totalReleases > group.items.length" class="resource-limit">显示最近 {{ group.items.length }} 条，共 {{ group.totalReleases }} 条</p><p v-if="!group.items?.length">暂无资源</p><article v-for="(resource, index) in group.items" :key="index"><p>{{ resource.title }}</p><div><span>{{ resource.formatSize }}</span><span v-if="isAniBT">{{ [resource.resolution, resource.format, ...(resource.language || []), ...(resource.customTags || [])].filter(Boolean).join(' · ') }}</span><time>{{ resource.createdAt }}</time><a v-if="isAniBT && resource.magnet" :href="resource.magnet">磁力链接</a><a v-if="resource.torrent" :href="resource.torrent" target="_blank" rel="noopener noreferrer">下载种子</a></div></article></div>
            </section>
          </div>
        </div>
        <div v-if="flowStage === 'rules'" class="group-rules">
          <fieldset v-for="group in ruleGroups" :key="group.rss"><legend>{{ group.label }}</legend><label v-for="(rules, index) in ruleOptions(group)" :key="index" class="rule-choice"><input v-model="matchSelection[group.rss]" type="radio" :name="`rule-${group.rss}`" :value="JSON.stringify(rules.map(rule => rule.regex))"><span class="group-tags"><span v-for="(rule, ruleIndex) in rules" :key="ruleIndex">{{ rule.label }}</span></span></label><label class="rule-choice"><input v-model="matchSelection[group.rss]" type="radio" :name="`rule-${group.rss}`" value="[]"><span>全部资源</span></label></fieldset>
        </div>
        <div v-show="flowStage === 'confirm'"><BatchAdditionView ref="batchRef" v-model="batchVisible" :items="batchItems" :type="isAniBT ? 'ani-bt' : 'mikan'" embedded compact @added="subscribed" @done="groupDialog = false"/></div>
      </template>
      <template v-if="selected && (flowStage === 'rules' || (flowStage === 'groups' && selectedGroups.length))" #footer>
        <footer v-if="flowStage === 'groups'" class="groups-footer"><span>已选 {{ selectedGroups.length }} 个字幕组 · 首个为主 RSS</span><AppButton type="primary" @click="beginSelected">添加所选<ChevronRight :size="15"/></AppButton></footer>
        <footer v-else class="groups-footer"><span>按所选规格筛选后续资源</span><AppButton type="primary" @click="beginSubscription(ruleGroups)">确定<ChevronRight :size="15"/></AppButton></footer>
      </template>
    </AppModal>
  </div>
</template>
<style scoped>
.catalog-sources{display:flex;align-items:center;gap:2px;padding:3px;border:1px solid var(--border);border-radius:6px;background:var(--subtle-background)}.catalog-sources button{font-size:12px;color:var(--muted-foreground);padding:5px 13px;border-radius:4px;line-height:1.5;transition:background .15s,color .15s}.catalog-sources button[aria-pressed=true]{background:var(--surface-raised);color:var(--foreground)}.catalog-sources button:hover{color:var(--foreground)}.catalog-sources button:focus-visible{outline:2px solid var(--primary);outline-offset:1px}.catalog-notice{font-size:12px;color:var(--text-secondary);padding:0 0 20px}.resource-limit{color:var(--muted-foreground)!important;font-size:11px!important;padding-bottom:10px}

.catalog-season-select{width:150px}.catalog-season-select.anibt-season-select{width:210px}.mikan-library{height:100%;min-height:0;display:flex;flex-direction:column}.subscribe-identity{display:flex;align-items:center;gap:20px;padding-bottom:20px}.subscribe-identity>img{width:72px;height:108px;object-fit:cover;border-radius:5px;flex-shrink:0}.subscribe-identity h3{font-size:22px;line-height:1.4;font-weight:500;color:var(--foreground);overflow-wrap:anywhere}.subscribe-identity p{font-size:11px;color:var(--muted-foreground);margin-top:10px}.subscribe-identity a{display:inline-flex;align-items:center;gap:6px;color:var(--text-secondary);font-size:11px;margin-top:10px}.subscribe-identity a:hover{color:var(--foreground)}.subscribe-stage{display:flex;align-items:center;gap:14px;border-top:1px solid var(--border);padding:17px 0}.subscribe-stage h3{font-size:15px;font-weight:500;color:var(--text-secondary);outline:none}.subscribe-stage>span{font-size:11px;color:var(--text-subtle)}.subscribe-stage>button{display:flex;align-items:center;gap:4px;color:var(--muted-foreground);font-size:12px}.subscribe-stage>button:hover{color:var(--foreground)}
.groups-message{padding:40px 0;text-align:center;font-size:13px;color:var(--muted-foreground)}.groups-message p{margin-bottom:16px}
.groups-rows{min-width:0}
.subtitle-group{border-top:1px solid var(--border)}.subtitle-group:first-child{border-top:0}.chosen-group{background:var(--primary-soft)}
.group-row{display:grid;grid-template-columns:18px minmax(0,1fr) auto;align-items:center;gap:12px;padding:14px 4px}.group-row>input,.rule-choice>input{accent-color:var(--primary);width:15px;height:15px;cursor:pointer}
.group-expand{display:flex;align-items:center;gap:12px;min-width:0;text-align:left;padding:3px 0}.group-expand>svg{color:var(--muted-foreground);flex-shrink:0;transition:transform .15s}.group-expand:hover .group-name{color:var(--foreground)}.group-copy{display:flex;flex:1;min-width:0;flex-direction:column;gap:8px}.group-heading{display:flex;align-items:baseline;flex-wrap:wrap;gap:5px 12px}.group-name{font-size:14px;color:var(--foreground);overflow-wrap:anywhere}.group-update{font-size:10px;color:var(--muted-foreground)}.group-role{font-size:10px;color:var(--success)}
.group-tags{display:flex;align-items:center;gap:5px;flex-wrap:wrap}.group-tags>span{border:1px solid var(--border);background:var(--subtle-background);color:var(--text-secondary);border-radius:4px;padding:2px 6px;font-size:10px;line-height:1.5}
.group-resources{padding:0 12px 14px 34px}.group-resources article{border-top:1px solid var(--border);padding:12px 0}.group-resources p{font-size:12px;line-height:1.7;color:var(--text-secondary);overflow-wrap:anywhere}.group-resources article>div{display:flex;gap:12px;flex-wrap:wrap;font-size:10px;color:var(--text-subtle);margin-top:6px}.group-resources a{color:var(--success);margin-left:auto}
.groups-footer{display:flex;align-items:center;justify-content:space-between;gap:16px;width:100%}.groups-footer>span{font-size:11px;color:var(--muted-foreground)}.groups-footer :deep(.app-button){padding-inline:18px}
.group-rules{min-width:0;padding:2px}.group-rules fieldset{border:0;border-bottom:1px solid var(--border);padding:0 0 16px;margin-bottom:18px;min-width:0}.group-rules fieldset:last-child{margin-bottom:0;padding-bottom:0;border-bottom:0}.group-rules legend{font-size:14px;color:var(--foreground);margin-bottom:12px}.rule-choice{display:flex;align-items:center;gap:10px;padding:10px 12px;cursor:pointer;border-radius:6px;color:var(--text-secondary);font-size:12px}.rule-choice:has(input:checked){background:var(--primary-soft)}.rule-choice:hover{background:var(--subtle-background)}
@media(max-width:600px){.subscribe-identity{gap:14px;padding-bottom:16px}.subscribe-identity>img{width:56px;height:84px}.subscribe-identity h3{font-size:19px}.subscribe-identity p{font-size:10px;margin-top:7px}.subscribe-stage{padding:14px 0}.subscribe-stage>span{font-size:10px}.group-row{gap:8px;padding:12px 2px}.group-name{font-size:13px}.group-tags>span{font-size:9px}.group-resources{padding-left:26px}.groups-footer{gap:8px}.groups-footer>span{font-size:10px}}
</style>
