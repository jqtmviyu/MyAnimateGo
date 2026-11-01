<template>
  <div class="subscription-page app-page-layout">
    <AddView ref="addRef"/><CollectionView ref="collectionRef"/><ManageView ref="manageRef"/>
    <h1 class="sr-only">{{ scope === 'catalog' ? '全部番剧' : '我的订阅' }}</h1>
    <div v-show="scope === 'catalog'" class="catalog-body app-page-content"><KeepAlive><MikanLibraryView :key="catalogSource" :source="catalogSource" :active="scope === 'catalog'" :subscriptions="subscriptions" ref="catalogRef" @source-change="catalogSource = $event" @subscribed="listRef?.getList()"/></KeepAlive></div>
    <div v-show="scope === 'subscribed'" class="subscription-body app-page-content">
      <LibraryToolbar>

        <template #search><form class="media-library-search" data-focus-container role="search" @submit.prevent><input ref="searchInput" v-model="title" type="search" aria-label="搜索我的订阅" placeholder="搜索订阅标题、拼音"><button type="submit" aria-label="搜索订阅"><Search :size="17"/></button></form></template>
        <template #filters>
          <span class="media-library-total">{{ visibleTotal }} 项</span>
          <AppSelect v-model="enable" class="subscription-state-filter" aria-label="订阅状态" @change="selectChange"><AppOption v-for="item in enableSelect" :key="item.label" :label="item.label" :value="item.label"/></AppSelect>
          <AppSelect v-model="releaseDate" class="subscription-month-filter" aria-label="按月份筛选" @change="selectChange"><AppOption label="所有月份" value=""/><AppOption v-for="month in releaseDateList" :key="month" :label="month" :value="month"/></AppSelect>
          <button class="media-library-reset" :disabled="enable === '全部' && !releaseDate" @click="releaseDate = ''; enable = '全部'; selectChange()">重置筛选</button>
        </template>
        <AppButton class="media-library-action" icon="Plus" @click="addRef?.show">添加订阅</AppButton>
        <AppButton class="media-library-action" icon="Plus" @click="collectionRef?.show">添加合集</AppButton>
        <AppButton class="media-library-action" icon="Refresh" :loading="refreshLoading" @click="confirmRefreshAni">刷新全部订阅</AppButton>
        <AppButton class="media-library-action" @click="manageRef?.show()"><ListChecks :size="16"/>批量管理</AppButton>
      </LibraryToolbar>
      <SubscriptionListView ref="listRef" :filter="filter" :active="scope === 'subscribed'" :title="title" @create="addRef?.show" @loaded="listLoaded" @filtered="visibleTotal = $event"/>
    </div>
  </div>
</template>
<script setup>
import {computed, onMounted, ref} from "vue";
import {notify} from "@/lib/feedback";
import {dialogs} from "@/lib/dialogs";
import {useLocalStorage} from "@vueuse/core";
import {ListChecks, Search} from '@lucide/vue';
import LibraryToolbar from './LibraryToolbar.vue';
import MikanLibraryView from './MikanLibraryView.vue';
import {useRoute} from 'vue-router';
import SubscriptionListView from "@/view/home/SubscriptionListView.vue";
import AddView from "@/view/home/AddView.vue";
import CollectionView from "@/view/home/CollectionView.vue";
import ManageView from "@/view/home/ManageView.vue";

import * as http from "@/js/http.js";

const route = useRoute()
const scope = computed(() => route.path === '/catalog' ? 'catalog' : 'subscribed')
const catalogSource = useLocalStorage('catalog-source', 'anibt')
const catalogRef = ref()
const searchInput = ref()
const visibleTotal = ref(0)
const listRef = ref()
const addRef = ref()
const collectionRef = ref()
const manageRef = ref()
const title = ref('')
const releaseDate = ref('')
const releaseDateList = ref([])
const subscriptions = ref(null)
const refreshLoading = ref(false)
const enable = useLocalStorage('select-enable', '已启用')
const enableSelect = [
  {
    label: '全部',
    fun: () => true
  },
  {
    label: '已启用',
    fun: item => item.enable
  },
  {
    label: '未启用',
    fun: item => !item.enable
  }
]
const filter = ref(() => true)

const selectChange = () => {
  filter.value = it => {
    const selectedEnable = enableSelect.find(item => item.label === enable.value)
    if (selectedEnable && !selectedEnable.fun(it)) {
      return false
    }
    if (!releaseDate.value) {
      return true
    }

    return releaseDate.value === it.releaseDate?.replace(/-\d{2}$/, '')
  }
}

const listLoaded = data => {
  subscriptions.value = data.subscriptions
  releaseDateList.value = data.releaseDateList || []
}

const refreshAni = () => {
  refreshLoading.value = true
  http.refreshAll()
      .then(res => {
        notify.success(res.message)
        listRef.value?.getList()
      })
      .finally(() => {
        refreshLoading.value = false
      })
}

// 全量刷新可能触发大量订阅请求，执行前通过模态弹窗进行确认。
const confirmRefreshAni = () => {
  dialogs.confirm(
      '将立即刷新全部订阅，是否继续？',
      '刷新全部订阅',
      {
        confirmButtonText: '确认刷新',
        confirmButtonClass: 'is-text is-has-bg app-button--primary',
        cancelButtonText: '取消',
        cancelButtonClass: 'is-text is-has-bg',
        type: 'warning'
      }
  ).then(refreshAni).catch(() => {
  })
}

onMounted(() => {
  selectChange()
})
</script>
<style scoped>
.catalog-body,.subscription-body{padding:0}.subscription-state-filter{width:110px}.subscription-month-filter{width:144px}
</style>
