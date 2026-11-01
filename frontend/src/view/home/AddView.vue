<template>
  <BatchAdditionView ref="batchAdditionRef" v-model="batchAdditionDialogVisible" :items="[]" type="other"
                     @done="batchDone"/>
  <AnimeGardenView ref="animeGardenRef"/>
  <AniBTView ref="aniBTRef"/>
  <MikanView ref="mikanRef"/>
  <BgmView ref="bgmRef" @callback="bgmCallback"/>
  <AppModal v-model="dialogVisible" class="add-subscription-dialog" center title="添加订阅"
             :close-on-click-modal="!rssButtonLoading"
             :close-on-press-escape="!rssButtonLoading"
             :show-close="!rssButtonLoading"
  >
    <div class="add-source-step">
      <AppTabs v-model="activeName" class="add-source-tabs tabs-center" tab-position="top">
        <AppTab
            v-for="source in rssSources"
            :key="source.name"
            :label="source.label"
            :name="source.name">
          <div class="source-panel">
            <div class="source-heading">
              <div class="source-identity">
                <img :src="source.icon" :alt="source.label" class="source-icon"/>
                <div>
                  <h3>{{ source.label }}</h3>
                  <AppText size="small" type="info">RSS 订阅</AppText>
                </div>
              </div>
              <AppButton
                  bg
                  text
                  type="primary"
                  :disabled="rssButtonLoading"
                  @click="source.open">
                <AppIcon class="app-icon--left">
                  <Search/>
                </AppIcon>
                浏览 {{ source.label }}
              </AppButton>
            </div>
            <AppForm class="source-form" label-position="top" @submit.prevent>
              <AppField label="RSS 地址">
                <AppInput
                    v-model="ani.url"
                    :autosize="{ minRows: 3, maxRows: 5 }"
                    :disabled="rssButtonLoading"
                    :placeholder="source.placeholder"
                    type="textarea"/>
              </AppField>
            </AppForm>
            <AppAlert
                :closable="false"
                show-icon
                title="请使用单个番剧与字幕组的 RSS，聚合订阅集中更新时可能出现遗漏。"
                type="info"/>
          </div>
        </AppTab>
        <AppTab label="其他" name="other">
          <div class="source-panel other-source-panel">
            <AppForm class="source-form" label-position="top" @submit.prevent>
              <AppField label="番剧名称">
                <div class="title-search-row">
                  <AppInput
                      v-model="ani.title"
                      :disabled="rssButtonLoading"
                      placeholder="请勿留空"/>
                  <AppButton
                      aria-label="搜索 Bangumi"
                      bg
                      text
                      type="primary"
                      :disabled="rssButtonLoading"
                      title="搜索 Bangumi"
                      @click="bgmRef?.show(ani.title)">
                    <AppIcon class="app-icon--left">
                      <Search/>
                    </AppIcon>
                  </AppButton>
                </div>
              </AppField>
              <AppField label="Bangumi 地址">
                <AppInput
                    v-model="ani.bgmUrl"
                    :disabled="rssButtonLoading"
                    placeholder="https://bgm.tv/subject/123456"/>
              </AppField>
              <AppField label="RSS 地址">
                <AppInput
                    v-model="ani.url"
                    :autosize="{ minRows: 3, maxRows: 5 }"
                    :disabled="rssButtonLoading"
                    placeholder="https://example.com/anime.xml"
                    type="textarea"/>
              </AppField>
            </AppForm>
            <AppAlert
                :closable="false"
                show-icon
                title="请核对 RSS 来源、字幕组与集数，再预览订阅。"
                type="warning"/>
          </div>
        </AppTab>
      </AppTabs>
      <div class="action">
        <AppButton :loading="rssButtonLoading" bg text type="primary" @click="getRss">
          下一步
          <AppIcon class="app-icon--right">
            <ArrowRight/>
          </AppIcon>
        </AppButton>
      </div>
    </div>
  </AppModal>
</template>

<script setup>
import {ref} from "vue";
import {notify} from "@/lib/feedback";
import {ChevronRight as ArrowRight, Search} from "@lucide/vue";
import MikanView from "./MikanView.vue";
import BgmView from "./BgmView.vue";
import BatchAdditionView from './BatchAdditionView.vue'
import {aniData} from "@/js/ani.js";
import AniBTView from "@/view/home/AniBTView.vue";
import {useLocalStorage} from "@vueuse/core";
import AnimeGardenView from "@/view/home/AnimeGardenView.vue";
import mikanIcon from "@/assets/providers/mikan.png";
import aniBTIcon from "@/assets/providers/anibt.png";
import animeGardenIcon from "@/assets/providers/anime-garden.png";

const aniBTRef = ref()
const mikanRef = ref()
const animeGardenRef = ref()
const bgmRef = ref()
const batchAdditionRef = ref()
const batchAdditionDialogVisible = ref(false)

const rssSources = [
  {
    name: 'mikan',
    label: 'Mikan',
    icon: mikanIcon,
    placeholder: 'https://mikanani.me/RSS/Bangumi?bangumiId=xxx&subgroupid=xxx',
    open: () => mikanRef.value?.show()
  },
  {
    name: 'ani-bt',
    label: 'AniBT',
    icon: aniBTIcon,
    placeholder: 'https://anibt.net/rss/anime.xml?bgmId=xxx&groupSlug=xxx',
    open: () => aniBTRef.value?.show()
  },
  {
    name: 'anime-garden',
    label: 'AnimeGarden',
    icon: animeGardenIcon,
    placeholder: 'https://api.animes.garden/feed.xml?subject=xxx&fansub=xxx',
    open: () => animeGardenRef.value?.show()
  }
]

const dialogVisible = ref(false)

const ani = ref(aniData)

const rssButtonLoading = ref(false)

// 手动 RSS 也交给统一弹窗串行加载并确认，不再进入 AddView 内部的第二步。
const getRss = async () => {
  if (activeName.value === 'other') {
    if (!ani.value.bgmUrl) {
      notify.error('请选择在 Bangumi 中所对应的番剧')
      return
    }
  }
  ani.value.type = activeName.value
  batchAdditionDialogVisible.value = true
  await batchAdditionRef.value?.openRequests([JSON.parse(JSON.stringify(ani.value))])
}

const activeName = useLocalStorage('add-active-name', 'mikan')

const show = () => {
  ani.value = JSON.parse(JSON.stringify(aniData))
  dialogVisible.value = true
  rssButtonLoading.value = false
}

let bgmCallback = it => {
  ani.value.title = it['name_cn'] ? it['name_cn'] : it['name']
  ani.value.bgmUrl = it.url
}

let batchDone = () => {
  dialogVisible.value = false
  window.$reLoadList?.()
}

defineExpose({show})
</script>

<style scoped>
.add-source-tabs {
  min-width:0;
}

.source-panel {
  min-height:272px;
  padding:14px 4px 4px;
}

.source-heading {
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:16px;
  margin-bottom:20px;
}

.source-identity {
  min-width:0;
  display:flex;
  align-items:center;
  gap:10px;
}

.source-identity h3 {
  margin:0;
  font-size:16px;
  line-height:1.35;
}

.source-icon {
  width:40px;
  height:40px;
  flex-shrink:0;
  border-radius:8px;
  object-fit:cover;
}

.source-form :deep(.app-form-item) {
  margin-bottom:16px;
}

.source-form :deep(.app-form-item__label) {
  height:auto;
  padding-bottom:7px;
  color:var(--foreground);
  font-weight:600;
  line-height:1.4;
}

.title-search-row {
  width:100%;
  display:grid;
  grid-template-columns:minmax(0, 1fr) 40px;
  gap:8px;
}

.title-search-row .app-button {
  width:40px;
  margin:0;
}

.other-source-panel {
  min-height:366px;
}

.action {
  width:100%;
  display:flex;
  justify-content:flex-end;
  margin-top:12px;
  padding-top:16px;
  border-top:1px solid var(--border);
}

.ani-step-back {
  margin:0 12px 10px;
}

@media (max-width: 600px) {
  .source-panel {
    min-height:250px;
    padding-top:10px;
  }

  .source-heading {
    align-items:flex-start;
    gap:10px;
    margin-bottom:16px;
  }

  .source-heading .app-button {
    max-width:172px;
    margin:0;
  }

  .source-icon {
    width:36px;
    height:36px;
  }

  .other-source-panel {
    min-height:350px;
  }
}
</style>
