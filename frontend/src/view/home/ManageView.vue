<template>
  <ImportAniView ref="importAniRef" @callback="getList"/>
  <DelAniView ref="delAniRef" @callback="reloadAllLists"/>
  <AppModal v-model="dialogVisible" center title="管理">
    <div class="manage-content" v-busy="loading">
      <div class="manage-header">
        <div class="manage-toolbar">
          <div class="manage-search">
            <AppInput
                v-model="text"
                aria-label="搜索订阅"
                clearable
                placeholder="搜索"
                prefix-icon="Search"
            />
          </div>
          <div class="select-width">
            <AppSelect
                v-model="releaseDate"
                aria-label="按月份筛选"
                clearable>
              <AppOption v-for="it in releaseDateList"
                         :key="it" :label="it" :value="it"
              />
            </AppSelect>
          </div>
          <div class="select-width">
            <AppSelect v-model="selectedFilter" aria-label="订阅状态">
              <AppOption v-for="filter in selectFilters"
                         :key="filter.label"
                         :label="filter.label"
                         :value="filter.label"/>
            </AppSelect>
          </div>
        </div>
        <div class="manage-header-actions">
          <AppDropdown trigger="click">
            <AppButton aria-label="批量操作" icon="MoreFilled" title="批量操作"/>
            <template #dropdown>
              <AppDropdownMenu>
                <AppDropdownItem :disabled="!hasSelection" @click="updateTotalEpisodeNumber(false)">
                  <AppText>
                    <AppIcon>
                      <RefreshRight/>
                    </AppIcon>
                    更新总集数
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem :disabled="!hasSelection" @click="updateTotalEpisodeNumber(true)">
                  <AppText type="warning">
                    <AppIcon>
                      <Refresh/>
                    </AppIcon>
                    更新总集数 [F]
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem :disabled="!hasSelection" divided @click="batchEnable(true)">
                  <AppText type="primary">
                    <AppIcon>
                      <CircleCheck/>
                    </AppIcon>
                    启用
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem :disabled="!hasSelection" @click="batchEnable(false)">
                  <AppText type="warning">
                    <AppIcon>
                      <CircleClose/>
                    </AppIcon>
                    禁用
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem divided @click="importAniRef?.show">
                  <AppText>
                    <AppIcon>
                      <Download/>
                    </AppIcon>
                    导入
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem :disabled="!hasSelection" @click="exportData">
                  <AppText>
                    <AppIcon>
                      <Upload/>
                    </AppIcon>
                    导出
                  </AppText>
                </AppDropdownItem>
                <AppDropdownItem :disabled="!hasSelection" divided @click="showDeleteDialog">
                  <AppText type="danger">
                    <AppIcon>
                      <Remove/>
                    </AppIcon>
                    删除
                  </AppText>
                </AppDropdownItem>
              </AppDropdownMenu>
            </template>
          </AppDropdown>
        </div>
      </div>
      <AppTable
          ref="tableRef"
          :data="searchList"
          height="400px"
          row-key="id"
          size="small"
          stripe
          @selection-change="handleSelectionChange"
      >
        <AppColumn type="selection" width="55" fixed/>
        <AppColumn label="标题" width="200" fixed>
          <template #default="{row}">
            <AppText :line-clamp="2" size="small">
              {{ row.title }}
            </AppText>
          </template>
        </AppColumn>
        <AppColumn label="季" prop="season" width="50"/>
        <AppColumn label="字幕组" width="100">
          <template #default="{row}">
            <AppText size="small" truncated>
              {{ row.subgroup }}
            </AppText>
          </template>
        </AppColumn>
        <AppColumn label="状态" width="80">
          <template #default="{row}">
            <AppTag v-if="row.enable">
              已启用
            </AppTag>
            <AppTag v-else type="info">
              未启用
            </AppTag>
          </template>
        </AppColumn>
        <AppColumn label="进度" width="100">
          <template #default="{row}">
            <AppTag type="warning">
              {{ row.currentEpisodeNumber }} /
              {{ row.totalEpisodeNumber || '*' }}
            </AppTag>
          </template>
        </AppColumn>
        <AppColumn label="类型" width="100">
          <template #default="{row}">
            <AppTag type="danger">{{ row.ova ? 'ova' : 'tv' }}</AppTag>
          </template>
        </AppColumn>
        <AppColumn label="URL" width="300">
          <template #default="{row}">
            <AppText :line-clamp="2" size="small">
              {{ row.url }}
            </AppText>
          </template>
        </AppColumn>
      </AppTable>
      <div>
        <p class="manage-count">共 {{ searchList.length }} 项</p>
      </div>
    </div>
  </AppModal>
</template>
<script setup>
import {computed, nextTick, ref, watch} from "vue";
import {notify} from "@/lib/feedback";
import DelAniView from "./DelAniView.vue";
import ImportAniView from "@/view/home/ImportAniView.vue";
import {CircleCheck, CircleX as CircleClose, Download, RefreshCw as Refresh, RotateCw as RefreshRight, CircleMinus as Remove, Upload} from "@lucide/vue";
import * as http from "@/js/http.js";

const selectFilters = [
  {
    label: '全部',
    predicate: () => true
  },
  {
    label: '已启用',
    predicate: item => item.enable
  },
  {
    label: '未启用',
    predicate: item => !item.enable
  }
]

const dialogVisible = ref(false)
const loading = ref(false)
const releaseDateList = ref([])
const releaseDate = ref('')
const selectedFilter = ref(selectFilters[0].label)
const selectedItems = ref([])
const list = ref([])
const text = ref('')
const delAniRef = ref()
const importAniRef = ref()
const tableRef = ref()

const activeFilter = computed(() =>
    selectFilters.find(filter => filter.label === selectedFilter.value) ?? selectFilters[0]
)
const normalizedSearchText = computed(() => text.value.trim().toLowerCase())
const searchList = computed(() => list.value.filter(item => {
  const query = normalizedSearchText.value
  if (query) {
    const matchesText = [item.title, item.pinyin, item.pinyinInitials]
        .some(value => String(value ?? '').toLowerCase().includes(query))
    if (!matchesText) {
      return false
    }
  }

  if (releaseDate.value && releaseDate.value !== String(item.releaseDate ?? '').replace(/-\d{2}$/, '')) {
    return false
  }

  return activeFilter.value.predicate(item)
}))
const selectedIds = computed(() => selectedItems.value.map(item => item.id))
const hasSelection = computed(() => selectedIds.value.length > 0)

const clearSelection = () => {
  selectedItems.value = []
  tableRef.value?.clearSelection()
}

watch([text, releaseDate, selectedFilter], clearSelection)

const fetchList = async () => {
  const res = await http.listAni()
  const data = res.data ?? {}
  releaseDateList.value = data.releaseDateList ?? []
  list.value = (data.weekList ?? []).flatMap(week => week.items ?? [])
  await nextTick()
  clearSelection()
}

const getList = async () => {
  loading.value = true
  try {
    await fetchList()
  } finally {
    loading.value = false
  }
}

const reloadAllLists = async () => {
  await getList()
  window.$reLoadList?.()
}

const show = () => {
  releaseDate.value = ''
  selectedFilter.value = selectFilters[0].label
  text.value = ''
  clearSelection()
  dialogVisible.value = true
  getList()
}

const handleSelectionChange = value => {
  selectedItems.value = value
}

const requireSelection = () => {
  if (!hasSelection.value) {
    notify.error('未选择订阅')
    return false
  }
  return true
}

const runSelectedAction = async (request, {reload = false} = {}) => {
  if (!requireSelection()) {
    return
  }

  loading.value = true
  try {
    const res = await request([...selectedIds.value])
    notify.success(res.message)
    if (reload) {
      await fetchList()
      window.$reLoadList?.()
    }
  } finally {
    loading.value = false
  }
}

const exportData = () => {
  if (!requireSelection()) {
    return
  }

  const textContent = JSON.stringify(selectedItems.value)
  const blob = new Blob([textContent], {type: 'application/json'})
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a')
  anchor.style.display = 'none'
  anchor.href = url
  anchor.download = 'ani.v2.json'
  document.body.appendChild(anchor)
  try {
    anchor.click()
  } finally {
    document.body.removeChild(anchor)
    URL.revokeObjectURL(url)
  }
}

const showDeleteDialog = () => {
  if (requireSelection()) {
    delAniRef.value?.show(selectedItems.value)
  }
}

const batchEnable = value => {
  return runSelectedAction(ids => http.batchEnable(value, ids), {reload: true})
}

const updateTotalEpisodeNumber = force => {
  return runSelectedAction(ids => http.updateTotalEpisodeNumber(force, ids), {reload: true})
}


defineExpose({show})
</script>

<style scoped>
.manage-content {
  min-height:300px;
}

.manage-header {
  display:flex;
  align-items:flex-start;
  justify-content:space-between;
  gap:8px;
  width:100%;
  margin-bottom:8px;
}

.manage-toolbar {
  min-width:0;
  display:flex;
  flex:1;
  flex-wrap:wrap;
  gap:8px;
}

.manage-search {
  width:180px;
}

.manage-header-actions {
  flex-shrink:0;
}

.select-width {
  width:120px;
}

.manage-count {
  margin:6px;
  text-align:end;
}

@media (max-width: 560px) {
  .manage-search {
    width:100%;
  }

  .select-width {
    flex:1 1 120px;
  }
}

</style>
