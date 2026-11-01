<template>
  <EditAniView ref="editAniRef"/><CoverView ref="coverRef"/><DelAniView ref="delAniRef"/>
  <div class="subscription-list" v-busy="loading">
    <nav v-if="showWeek && filterList.length" class="media-library-categories" aria-label="我的订阅分类目录"><button v-for="day in filterList" :key="day.weekLabel" :aria-current="activeCategory === day.weekLabel ? 'location' : undefined" :aria-controls="categoryId(day.weekLabel)" @click="scrollToCategory(day.weekLabel)">{{ day.weekLabel }}<span>{{ day.items.length }}</span></button></nav>
    <div ref="scrollContainer" class="subscription-scroll media-library-scroll" tabindex="0" aria-label="我的订阅海报" @scroll.passive="syncCategory">
      <AppEmpty v-if="!flatFilterList.length && !loading" :description="loadError || (total ? '没有匹配的订阅，试试其他筛选条件' : '还没有订阅，添加第一部喜欢的作品吧')">
        <AppButton v-if="loadError" @click="getList">重试</AppButton><AppButton v-else-if="!total" type="primary" icon="Plus" @click="emit('create')">添加订阅</AppButton>
      </AppEmpty>
      <template v-if="showWeek"><section v-for="day in filterList" :id="categoryId(day.weekLabel)" :ref="element => setCategoryElement(day.weekLabel, element)" :key="day.weekLabel" class="media-library-day"><h2>{{ day.weekLabel }}<span>{{ day.items.length }} 个订阅</span></h2><div class="media-poster-grid"><AniCoverView v-for="item in day.items" :key="item.id" :item="item" @edit="editAniRef?.show" @cover="coverRef?.show" @del="delAniRef?.show"/></div></section></template>
      <div v-else class="media-poster-grid"><AniCoverView v-for="item in flatFilterList" :key="item.id" :item="item" @edit="editAniRef?.show" @cover="coverRef?.show" @del="delAniRef?.show"/></div>
    </div>
  </div>
</template>
<script setup>
import {computed, nextTick, onMounted, onUnmounted, ref, watch} from 'vue'
import EditAniView from './EditAniView.vue'
import CoverView from './CoverView.vue'
import DelAniView from './DelAniView.vue'
import {listAni} from '@/js/http'
import AniCoverView from './AniCoverView.vue'
import {useWeekNavigation, weekdays} from '@/lib/week-navigation'
import {showWeek} from '@/js/global'

const props = defineProps({title: {type: String, default: ''}, filter: Function, active: {type: Boolean, default: true}})
const emit = defineEmits(['loaded', 'filtered', 'create'])
const editAniRef = ref(), delAniRef = ref(), coverRef = ref()
const weekList = ref([]), releaseDateList = ref([])
const loading = ref(true), loadError = ref('')
const scrollContainer = ref()
const total = computed(() => weekList.value.reduce((count, day) => count + day.items.length, 0))
// 使用列表接口提供的排序序号，和应用设置中的排序保持一致。
const compare = (a, b) => Number(a.sort || 0) - Number(b.sort || 0)
const filterList = computed(() => {
  const query = props.title.trim().toLowerCase()
  return weekList.value.map(day => ({...day, items: day.items.filter(item => !props.filter || props.filter(item)).filter(item => !query || [item.title, item.pinyin, item.pinyinInitials].some(value => value?.toLowerCase().includes(query))).sort(compare)})).filter(day => day.items.length).sort((a, b) => {
    const left = weekdays.indexOf(a.weekLabel), right = weekdays.indexOf(b.weekLabel)
    return (left < 0 ? 7 : left) - (right < 0 ? 7 : right)
  })
})
const flatFilterList = computed(() => filterList.value.flatMap(day => day.items).sort(compare))
const categoryId = label => `subscription-category-${label}`
const {activeCategory, setCategoryElement, scrollToCategory, syncCategory, resetPosition} = useWeekNavigation({container: scrollContainer, categories: filterList, active: () => props.active, grouped: showWeek, loading})
watch(filterList, () => emit('filtered', flatFilterList.value.length), {immediate: true})
watch([() => props.title, () => props.filter], resetPosition)
const getList = async () => {
  loading.value = true
  loadError.value = ''
  try {
    const {data} = await listAni()
    weekList.value = data.weekList
    releaseDateList.value = data.releaseDateList
    emit('loaded', {releaseDateList: releaseDateList.value, total: total.value, subscriptions: weekList.value.flatMap(day => day.items)})
  } catch (error) { loadError.value = error.message } finally { loading.value = false }
}
onMounted(() => { window.$reLoadList = getList; getList() })
onUnmounted(() => { if (window.$reLoadList === getList) delete window.$reLoadList })
defineExpose({releaseDateList, getList})
</script>
<style scoped>
.subscription-list{height:100%;min-height:0;display:flex;flex-direction:column;overflow:hidden}.subscription-scroll{outline:none}.subscription-scroll:focus-visible{outline:2px solid var(--ring);outline-offset:-2px}
</style>
