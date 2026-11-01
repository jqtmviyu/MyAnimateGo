<template>
  <BatchAdditionView ref="batchAdditionRef" v-model="batchAdditionDialogVisible" :items="batchAdditionItems"
                     type="mikan"
                     @done="batchDone"/>
  <AppModal v-model="matchDialogVisible" align-center center title="匹配" width="auto" @close="cancelBatchMatch">
    <div class="match-content">
      <AppRadioGroup v-model="addAni.match">
        <div v-for="regexItems in regexList" class="match-item">
          <AppRadio :label="JSON.stringify(regexItems)"
                    :value="JSON.stringify(regexItems.map(it => it.regex))">
            <AppTag v-if="regexItems.length" v-for="regexItem in regexItems" class="tag-margin">
              {{ regexItem.label }}
            </AppTag>
            <AppTag v-else type="success">全部</AppTag>
          </AppRadio>
        </div>
      </AppRadioGroup>
    </div>
    <div class="dialog-footer">
      <AppButton icon="Check" @click="confirmMatch" text bg>确定
      </AppButton>
    </div>
  </AppModal>
  <AppModal v-model="dialogVisible" center title="Mikan">
    <AppCheckboxGroup v-model="rssList">
      <div class="content-wrapper">
        <div class="search-section">
          <div class="search-header">
            <AppInput v-model:model-value="text" clearable placeholder="请输入搜索标题"
                      prefix-icon="Search"
                      @clear="()=>{
                        text = ''
                        search()
                      }"
                      @keyup.enter="search"/>
            <AppButton :loading="searchLoading" bg icon="Search" text @click="search">搜索</AppButton>
          </div>
          <div v-if="data.seasons.length" class="flex season-selector">
            <AppSelect v-model:model-value="seasonSelect" class="season-select"
                       :disabled="text.length > 0 || loading"
                       @change="change">
              <AppOption v-for="season in data.seasons" :key="season['seasonLabel']"
                         :label="season['seasonLabel']" :value="season['seasonLabel']"/>
            </AppSelect>
            <AppButton :disabled="rssList.length < 1" bg icon="Plus" text @click="batchAddition">批量添加</AppButton>
          </div>
        </div>
        <div v-busy="loading" class="scroll-container">
          <AppTabs v-model="activeName" class="week-tabs">
            <AppTab v-for="week in data.weeks" :key="week.weekLabel"
                         :label="week.weekLabel" :name="week.weekLabel" lazy>
              <AppScroll class="week-pane-scrollbar">
                <div class="collapse-content">
                  <AppFold accordion @change="collapseChange">
                    <AppFoldItem v-for="it in week.items" :name="it.url">
                      <template #title>
                        <div class="flex collapse-title">
                          <img :src="proxyImage(it['cover'])" class="cover" @click.stop="open(it.url)">
                          <div class="flex collapse-title">
                            <AppText :truncated="false" line-clamp="1" size="small"
                                     class="title-text">
                              {{ it.title }}
                            </AppText>
                          </div>
                          <div v-if="it['score'] > 0" class="score-margin">
                            <h4 class="score-color">
                              {{ it['score'].toFixed(1) }}
                            </h4>
                          </div>
                          <AppBadge v-if="it['exists']" class="item badge-margin" type="primary"
                                    value="已订阅"/>
                        </div>
                      </template>
                      <div v-if="selectName === it.url" v-busy="groupLoading"
                           class="group-content">
                        <AppFold accordion>
                          <AppFoldItem v-for="group in groups[it.url]">
                            <template #title>
                              <div class="group-title-wrapper">
                                <div class="group-checkbox-wrapper">
                                  <AppCheckbox :value="JSON.stringify(group)" class="checkbox-margin" @click.stop
                                               @change="batchGroupChange($event, group)"/>
                                </div>
                                <div class="group-label">
                                  <AppText style="max-width: 100px;" truncated>{{ group.label }}</AppText>
                                  &nbsp;
                                  <AppText class="mx-1" size="small">{{ group['updateDay'] }}</AppText>
                                </div>
                                <div v-if="showTag()">
                                  <AppTag v-for="tag in group['groupRegex']['tags']"
                                          class="tag-margin">
                                    {{ tag }}
                                  </AppTag>
                                </div>
                                <div class="group-action">
                                  <AppButton bg @click.stop="callback(group)" icon="Plus">
                                    添加
                                  </AppButton>
                                </div>
                              </div>
                            </template>
                            <div class="group-items">
                              <div v-for="ti in group.items" class="item-margin">
                                <AppPanel shadow="never">
                                  <div>
                                    <h5>
                                      {{ ti.title }}
                                    </h5>
                                    <div class="item-footer">
                                      <p>
                                        {{ ti['formatSize'] }}
                                        {{ ti['createdAt'] }}
                                      </p>
                                      <div>
                                        <AppButton :icon="DocumentCopy" bg text @click="copy(ti['magnet'])"/>
                                        <AppButton :icon="DownloadIcon" bg text @click="openUrl(ti['torrent'])"/>
                                      </div>
                                    </div>
                                  </div>
                                </AppPanel>
                              </div>
                            </div>
                          </AppFoldItem>
                        </AppFold>
                      </div>
                    </AppFoldItem>
                  </AppFold>
                </div>
              </AppScroll>
            </AppTab>
          </AppTabs>
        </div>
      </div>
    </AppCheckboxGroup>
  </AppModal>
</template>

<script setup>
import {ref} from "vue";
import {notify} from "@/lib/feedback";
import {Copy as DocumentCopy, Download as DownloadIcon} from "@lucide/vue";
import {proxyImage} from "@/js/global.js";
import * as http from "@/js/http.js";
import BatchAdditionView from './BatchAdditionView.vue'

// 回填模式由编辑订阅和备用 RSS 场景显式启用，默认保持新增订阅流程。
const props = defineProps({
  fillMode: {type: Boolean, default: false}
})

// 批量添加订阅
let rssList = ref([]);
let batchAdditionItems = ref([])

let groupLoading = ref(false)
let activeName = ref("")
let dialogVisible = ref(false)
let loading = ref(false)
let data = ref({
  'seasons': [],
  'items': [],
  'weeks': []
})

let seasonSelect = ref('')

let show = (ani) => {
  seasonSelect.value = ''
  dialogVisible.value = true
  text.value = ''
  data.value = {
    'seasons': [],
    'items': [],
    'weeks': []
  }
  rssList.value = []
  batchMatchMap.value = {}
  searchAni(ani)
  list(text.value)
}

let searchAni = ani => {
  if (!ani) {
    return
  }

  if (ani.url) {
    let url = new URL(ani.url);
    let searchParams = url.searchParams;
    let mikanId = searchParams.get("bangumiId");
    if (mikanId) {
      text.value = `id: ${mikanId}`
      return
    }
  }

  let title = ani.mikanTitle ? ani.mikanTitle : ani.title
  title = title.replace(/ ?\((19|20)\d{2}\)/g, "").trim()
  title = title.replace(/ ?\[tmdbid=(\d+)]/g, "").trim()
  if (title.length > 2) {
    text.value = title
  }
}

let text = ref('')

let searchLoading = ref(false)
let search = () => {
  if (text.value.length === 1) {
    notify.error("搜索最少需要两个字符")
    return
  }
  searchLoading.value = true
  list(text.value).finally(() => {
    searchLoading.value = false
  })
}

let list = async (text, body) => {
  loading.value = true
  text = text ? text : ''
  body = body ? body : {}
  return http.mikan(text, body)
      .then(res => {
        let {seasons, weeks, totalItems} = res.data;

        if (totalItems < 1) {
          notify.warning("搜索结果为空")
        }

        if (seasons.length) {
          data.value.seasons = seasons
        }
        data.value.weeks = weeks
        if (weeks.length) {
          activeName.value = weeks[0].weekLabel
        }
        for (let season of data.value.seasons) {
          if (season['select'] && !seasonSelect.value) {
            seasonSelect.value = season['seasonLabel']
            return
          }
        }
      })
      .finally(() => {
        loading.value = false
      });
}

let change = (v) => {
  let body = data.value.seasons.filter(item => item['seasonLabel'] === v)
  if (body.length) {
    list('', body[0])
  }
}

let selectName = ref('')
let groups = ref({})

let collapseChange = (v) => {
  if (!v) {
    return
  }
  selectName.value = v
  if (groups.value[v]) {
    return;
  }
  groupLoading.value = true
  http.mikanGroup(v)
      .then(res => {
        groups.value[v] = res.data
      })
      .finally(() => {
        groupLoading.value = false
      })
}


let matchDialogVisible = ref(false)

let addAni = ref({
  'url': '',
  'match': '',
  'group': ''
})

let regexList = ref([])
let batchMatchGroup = ref(null)
let batchMatchMap = ref({})
let selectedMatchGroup = ref(null)

let callback = v => {
  let {rss, bgmUrl, label} = v
  regexList.value = JSON.parse(JSON.stringify(v.groupRegex.regexList))

  addAni.value.url = rss
  addAni.value.bgmUrl = bgmUrl
  addAni.value.subgroup = label
  addAni.value.match = '[]'

  regexList.value.push([])
  selectedMatchGroup.value = v
  batchMatchGroup.value = null
  matchDialogVisible.value = true
}

// 批量勾选字幕组时先选择匹配规则，规则确认后随该字幕组一起提交。
let batchGroupChange = (checked, group) => {
  if (!checked) {
    delete batchMatchMap.value[group.rss]
    return
  }
  callback(group)
  batchMatchGroup.value = group
}

let confirmMatch = () => {
  if (batchMatchGroup.value) {
    batchMatchMap.value[batchMatchGroup.value.rss] = JSON.parse(addAni.value.match)
  } else if (props.fillMode) {
    // 编辑场景将选中的 RSS、字幕组和规则回填给父组件。
    emit('callback', addAni.value)
    dialogVisible.value = false
  } else {
    // 新增场景继续进入统一添加弹窗，保留正常的订阅确认流程。
    const source = {...selectedMatchGroup.value, match: JSON.parse(addAni.value.match)}
    batchAdditionItems.value = [JSON.stringify(source)]
    batchAdditionDialogVisible.value = true
    batchAdditionRef.value?.open(batchAdditionItems.value)
  }
  batchMatchGroup.value = null
  matchDialogVisible.value = false
}

let cancelBatchMatch = () => {
  if (!batchMatchGroup.value) return
  const value = JSON.stringify(batchMatchGroup.value)
  rssList.value = rssList.value.filter(item => item !== value)
  delete batchMatchMap.value[batchMatchGroup.value.rss]
  batchMatchGroup.value = null
}

let showTag = () => {
  return window.innerWidth > 900;
}

let open = url => {
  window.open(url);
}

defineExpose({show})

let emit = defineEmits(['callback'])


let batchAdditionNum = ref(0)
let batchAdditionDialogVisible = ref(false)
let batchAdditionRef = ref()

let batchAddition = async () => {
  batchAdditionItems.value = rssList.value.map(value => {
    const source = JSON.parse(value)
    source.match = batchMatchMap.value[source.rss] || []
    return JSON.stringify(source)
  })
  batchAdditionDialogVisible.value = true
  await batchAdditionRef.value?.open(batchAdditionItems.value)
}

let batchDone = () => {
  window.$reLoadList?.()
}

let copy = (v) => {
  const input = document.createElement('input');
  input.value = v;
  document.body.appendChild(input);
  input.select();
  document.execCommand('copy');
  document.body.removeChild(input);
  notify.success('已复制')
}

let openUrl = (url) => window.open(url)

</script>

<style scoped>
.app-collapse {
  --app-collapse-header-height: 55px;
}

.match-item {
  margin-right:12px;
  display:inline;
}

.tag-margin {
  margin-right:4px;
}

.dialog-footer {
  display:flex;
  width:100%;
  justify-content:end;
}

.content-wrapper {
  min-height:300px;
}

.search-section {
  margin:4px;
}

.search-header {
  display:flex;
  justify-content:space-between;
  gap:8px;
}

.season-selector {
  margin-top:8px;
  width:100%;
  justify-content:space-between;
}

.season-select {
  max-width:140px;
}

.scroll-container {
  margin:8px 0 4px 0;
  height:600px;
}

.week-tabs {
  margin:0 4px;
}

.collapse-content {
  margin-left:15px;
}

.collapse-title {
  align-items:center;
}

.title-text {
  margin-left:6px;
  line-height:1.6;
  font-weight:bold;
}

.score-margin {
  margin-left:4px;
}

.score-color {
  color:var(--source-mikan);
}

.badge-margin {
  margin-left:4px;
}

.group-content {
  margin-left:15px;
  min-height:50px;
}

.group-title-wrapper {
  width:100%;
  display:flex;
  justify-content:space-between;
}

.group-checkbox-wrapper {
  height:100%;
}

.checkbox-margin {
  margin-right:8px;
}

.group-label {
  display:flex;
  align-items:center;
  flex:1;
  text-align:start;
}

.group-action {
  display:flex;
  align-items:center;
  margin-right:14px;
  margin-left:4px;
}

.group-items {
  margin-left:15px;
}

.item-margin {
  margin-bottom:4px;
}

.item-footer {
  width:100%;
  display:flex;
  justify-content:space-between;
  align-items:center;
}

.cover {
  border-radius:var(--radius);
  cursor:pointer;
  width:45px;
  height:45px;
  object-fit:cover;
  flex-shrink:0;
}

.match-content {
  max-width:500px;
  min-width:200px;
  margin-bottom:4px;
}
</style>
