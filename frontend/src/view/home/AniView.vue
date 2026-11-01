<template>
  <PreviewView ref="previewRef" :ani="props.ani"/>
  <AniBTView ref="aniBTRef" fill-mode @callback="mikanCallback"/>
  <MikanView ref="mikanRef" fill-mode @callback="mikanCallback"/>
  <AnimeGardenView ref="animeGardenRef" fill-mode @callback="mikanCallback"/>
  <TmdbGroupView ref="tmdbGroupRef" :ani="props.ani"/>
  <div class="subscription-editor">
    <AppTabs v-model="activeName" class="tabs-center">
      <AppTab label="基本" name="base" :lazy="true">
        <AppScroll height="min(48dvh, 500px)" ref="scrollbarRef">
          <AppForm class="editor-form" @submit.prevent label-width="auto">
            <AppField label="标题" class="editor-wide">
              <div class="full-width">
                <div>
                  <AppInput v-model.trim="props.ani.title" class="full-width"/>
                </div>
                <div class="change-title-button">
                  <AppButton :loading="getBgmNameLoading"
                             bg
                             icon="DocumentAdd" text @click="getBgmName">
                    使用Bangumi
                  </AppButton>
                  <AppButton @click="props.ani.title = ani.themoviedbName"
                             icon="DocumentAdd"
                             :disabled="props.ani.title === ani.themoviedbName || !ani.themoviedbName.length" bg text>
                    使用TMDB
                  </AppButton>
                </div>
              </div>
            </AppField>
            <AppField label="TMDB" class="editor-wide">
              <div class="tmdb-group">
                <div class="app-static-input tmdb-name"><AppLink v-if="props.ani?.tmdb?.id" :href="`https://www.themoviedb.org/${props.ani.ova ? 'movie' : 'tv'}/${props.ani.tmdb.id}`" target="_blank">{{ props.ani.themoviedbName || props.ani.tmdb.id }}</AppLink><span v-else>{{ props.ani.themoviedbName || '尚未关联 TMDB' }}</span></div>
                <AppButton icon="Search" circle bg text aria-label="搜索 TMDB" @click="searchThemoviedb"/>
                <AppButton icon="Refresh" circle bg text aria-label="刷新 TMDB 信息" @click="getThemoviedbName" :loading="getThemoviedbNameLoading"/>
              </div>
            </AppField>
            <AppField v-if="!props.ani.ova && props.ani.tmdb" label="剧集组" class="editor-wide">
              <div class="tmdb-group">
                <AppInput v-model="props.ani.tmdb['tmdbGroupId']" placeholder="留空不使用剧集组"/>
                <AppButton bg text icon="Menu" aria-label="选择 TMDB 剧集组" @click="tmdbGroupRef?.show"/>
              </div>
            </AppField>
            <AppField label="Bangumi 链接" class="editor-wide">
              <div class="tmdb-group">
                <AppInput v-model.trim="props.ani.bgmUrl" placeholder="https://bgm.tv/subject/12345"/>
                <AppButton bg text icon="LinkIcon" aria-label="打开 Bangumi 页面" @click="openUrl(props.ani.bgmUrl)"/>
              </div>
            </AppField>
            <AppField label="主 RSS" class="editor-wide">
              <div class="full-width">
                <div class="rss-fields">
                  <AppInput aria-label="字幕组" v-model:model-value="props.ani.subgroup" placeholder="字幕组"/>

                  <AppInput aria-label="主 RSS 地址" v-model:model-value="props.ani.url" placeholder="https://xxx.xxx"/>
                </div>
                <div class="source-buttons" aria-label="选择 RSS 来源">
                  <AppButton bg text
                             @click="mikanRef?.show(props.ani)">
                    <template #icon>
                      <img src="@/assets/providers/mikan.png" alt="" class="icon"/>
                    </template>Mikan
                  </AppButton>
                  <AppButton bg text
                             @click="aniBTShow">
                    <template #icon>
                      <img src="@/assets/providers/anibt.png" alt="" class="icon"/>
                    </template>AniBT
                  </AppButton>
                  <AppButton bg text
                             @click="animeGardenShow">
                    <template #icon>
                      <img src="@/assets/providers/anime-garden.png" alt="" class="icon"/>
                    </template>AnimeGarden
                  </AppButton>
                </div>
              </div>
            </AppField>
            <AppField label="日期">
              <div class="form-item-flex">
                <AppDate
                    v-model="props.ani.releaseDate"
                    format="YYYY-MM-DD"
                    value-format="YYYY-MM-DD"
                />
              </div>
            </AppField>
            <AppField label="季">
              <div class="form-item-flex">
                <AppNumber
                                 :min="0"
                                 v-model="props.ani.season"
                                 :disabled="props.ani.ova"/>
              </div>
            </AppField>
            <AppField label="集数偏移">
              <div class="form-item-flex">
                <AppNumber v-model:model-value="props.ani.offset" :disabled="props.ani.ova"/>
              </div>
            </AppField>
            <AppField label="总集数">
              <div class="form-item-flex">
                <AppNumber v-model:model-value="props.ani.totalEpisodeNumber"/>
              </div>
            </AppField>
            <AppField label="匹配" class="editor-wide">
              <ExcludeView v-model:exclude="props.ani.match" :import-exclude="false"/>
            </AppField>
            <AppField label="排除" class="editor-wide">
              <ExcludeView v-model:exclude="props.ani.exclude" :import-exclude="true"/>
            </AppField>
            <AppField label="全局排除">
              <AppSwitch v-model:model-value="props.ani['globalExclude']"/>
            </AppField>
            <AppField label="剧场版">
              <AppSwitch v-model:model-value="props.ani.ova"/>
            </AppField>
            <AppField label="启用">
              <AppSwitch v-model:model-value="props.ani.enable"/>
            </AppField>
          </AppForm>
        </AppScroll>
      </AppTab>
      <AppTab label="备用 RSS" name="standby-rss" :lazy="true">
        <StandbyRssView ref="standbyRssRef" :ani="props.ani"/>
      </AppTab>
      <AppTab label="自定义" name="custom" :lazy="true">
        <AppScroll height="min(48dvh, 500px)">
          <AppForm class="editor-custom-form" @submit.prevent label-width="auto">
            <AppField label="自定义集数规则">
              <div class="full-width">
                <div>
                  <AppSwitch v-model:model-value="props.ani.customEpisode"/>
                </div>
                <div class="flex full-width">
                  <AppInput class="full-width"
                            :disabled="!props.ani.customEpisode"
                            v-model:model-value="props.ani.customEpisodeStr"/>

                  <AppNumber v-model:model-value="props.ani.customEpisodeGroupIndex"
                                   :disabled="!props.ani.customEpisode"/>
                </div>
              </div>
            </AppField>
            <AppField label="自定义路径">
              <div class="full-width">
                <div>
                  <AppSwitch v-model:model-value="props.ani.customDownloadPath"/>
                </div>
                <div>
                  <AppInput type="textarea" class="full-width" :disabled="!props.ani.customDownloadPath"
                            :autosize="{ minRows: 2}"
                            v-model:model-value="props.ani.customDownloadPathTemplate"/>
                </div>
                <div style="display: flex;justify-content: space-between;margin-top: 6px;">
                  <AppButton :disabled="!props.ani.customDownloadPath" :loading="downloadPathLoading" bg icon="Refresh"
                             text
                             @click="downloadPath"/>
                  <AppText class="mx-1" size="small">
                    最终下载位置以 <strong>预览</strong> 为准
                  </AppText>
                </div>
              </div>
            </AppField>
            <AppField label="自定义上传">
              <div class="full-width">
                <div>
                  <AppSwitch v-model="props.ani.customUploadEnable"/>
                </div>
                <div>
                  <AppInput
                      v-model="props.ani.customUploadPathTarget"
                      :autosize="{ minRows: 2}"
                      :disabled="!props.ani.customUploadEnable"
                      class="full-width"
                      type="textarea"/>
                </div>
              </div>
            </AppField>
            <AppField label="自定义完结迁移">
              <div class="full-width">
                <div>
                  <AppSwitch v-model="props.ani.customCompleted"/>
                </div>
                <div>
                  <AppInput type="textarea" class="full-width" :disabled="!props.ani.customCompleted"
                            :autosize="{ minRows: 2}"
                            v-model:model-value="props.ani.customCompletedPathTemplate"/>
                </div>
              </div>
            </AppField>
            <AppField label="重命名模版">
              <div class="full-width">
                <AppSwitch v-model="props.ani['customRenameTemplateEnable']"/>
                <br>
                <AppInput v-model:model-value="props.ani['customRenameTemplate']"
                          :disabled="!props.ani['customRenameTemplateEnable']"
                          placeholder="${title} S${seasonFormat}E${episodeFormat}"/>
                <br>

              </div>
            </AppField>
            <AppField label="自定义标签">
              <div>
                <AppSwitch v-model="props.ani.customTagsEnable"/>
                <DisableView v-model="props.ani.customTagsEnable">
                  <CustomTagsView :config="props.ani"/>
                </DisableView>
              </div>
            </AppField>
            <AppField label="其它">
              <div class="editor-options">
              <AppCheckbox v-model="props.ani.omit" label="遗漏检测"/>
              <AppCheckbox v-model="props.ani.upload" label="自动上传"/>
              <AppCheckbox v-model="props.ani.downloadNew" label="只下载最新集"/>
              <AppCheckbox v-model="props.ani['message']" label="通知"/>
              <AppCheckbox v-model="props.ani['completed']" label="完结迁移"/>
              </div>
            </AppField>
          </AppForm>
        </AppScroll>
      </AppTab>
    </AppTabs>
  </div>
  <div class="editor-footer">
    <div>
      <AppDropdown v-if="props.editMode" trigger="click">
        <AppButton bg text icon="MoreFilled">
          其他
        </AppButton>
        <template #dropdown>
          <AppDropdownMenu>
            <AppDropdownItem @click="refreshAni">
              <AppText>
                <AppIcon>
                  <RefreshRight/>
                </AppIcon>
                刷新
              </AppText>
            </AppDropdownItem>
          </AppDropdownMenu>
        </template>
      </AppDropdown>
    </div>
    <div class="editor-submit">
      <AppButton @click="previewRef.show()" bg text icon="Grid">预览</AppButton>
      <AppButton icon="Check" type="primary" :loading="okLoading" @click="async ()=>{
        standbyRssRef?.normalize()
        okLoading = true
        emit('callback',()=>okLoading = false)
      }" text bg>{{ props.confirmText }}
      </AppButton>
    </div>
  </div>
</template>

<script setup>

import ExcludeView from "@/view/config/ExcludeView.vue";
import PreviewView from "./PreviewView.vue";
import {onMounted, ref} from "vue";
import {notify} from "@/lib/feedback";
import {dialogs} from "@/lib/dialogs";
import StandbyRssView from "./StandbyRssView.vue";
import MikanView from "./MikanView.vue";
import TmdbGroupView from "./TmdbGroupView.vue";
import CustomTagsView from "@/view/config/CustomTagsView.vue";
import {RefreshCw as Refresh, RotateCw as RefreshRight} from "@lucide/vue";
import * as http from "@/js/http.js";
import {getBgmTitle} from "@/js/http.js";
import AniBTView from "@/view/home/AniBTView.vue";
import AnimeGardenView from "@/view/home/AnimeGardenView.vue";
import DisableView from "@/view/custom/DisableView.vue";

const activeName = ref('base')

const aniBTRef = ref()
const mikanRef = ref()
const animeGardenRef = ref()
const tmdbGroupRef = ref()
const standbyRssRef = ref()

let previewRef = ref()
let okLoading = ref(false)

let getThemoviedbNameLoading = ref(false)

let getThemoviedbName = () => {
  if (!props.ani.title.length) {
    return
  }

  getThemoviedbNameLoading.value = true
  http.getThemoviedbName({
    title: props.ani.title,
    ova: props.ani.ova
  })
      .then(res => {
        notify.success(res.message)
        props.ani['themoviedbName'] = res.data['themoviedbName']
        props.ani['tmdb'] = res.data['tmdb']
      })
      .finally(() => {
        getThemoviedbNameLoading.value = false
      })
}

let searchThemoviedb = () => {
  dialogs.prompt('输入 TmdbId', {
    confirmButtonText: '确定',
    confirmButtonClass: 'is-text is-has-bg app-button--primary',
    cancelButtonText: '取消',
    cancelButtonClass: 'is-text is-has-bg',
  })
      .then(value => {
        http.getThemoviedbName({
          tmdbId: value.value,
          ova: props.ani.ova
        })
            .then(res => {
              notify.success(res.message)
              props.ani['themoviedbName'] = res.data['themoviedbName']
              props.ani['tmdb'] = res.data['tmdb']
            })
      }).catch(() => {})
}

onMounted(() => {
  init()
})

let scrollbarRef = ref()

let init = () => {
  scrollbarRef.value.setScrollTop(0)
  activeName.value = 'base'
}

let refreshAni = () => {
  http.refreshAni(props.ani)
      .then(res => {
        notify.success(res.message)
      })
}

let downloadPathLoading = ref(false)
let downloadPath = () => {
  downloadPathLoading.value = true
  let newAni = JSON.parse(JSON.stringify(props.ani))
  newAni.customDownloadPath = false
  http.downloadPath(newAni)
      .then(res => {
        props.ani.customDownloadPathTemplate = res.data.downloadPath
      })
      .finally(() => {
        downloadPathLoading.value = false
      })
}

let getBgmNameLoading = ref(false)

let getBgmName = () => {
  getBgmNameLoading.value = true
  getBgmTitle(props.ani)
      .then(res => {
        props.ani.title = res.data
      })
      .finally(() => {
        getBgmNameLoading.value = false
      })
}

let mikanCallback = v => {
  let {subgroup, match, url} = v
  props.ani.url = url
  props.ani.subgroup = subgroup

  let newMatch = JSON.parse(match).map(s => `{{${subgroup}}}:${s}`)

  // 剔除旧的同字幕组规则
  props.ani.match = props.ani.match.filter(it => it.indexOf(`{{${subgroup}}}:`) !== 0)

  props.ani.match.push(...newMatch)
}


let animeGardenShow = () => {
  let bgmUrl = props.ani.bgmUrl;
  animeGardenRef.value?.show(bgmUrl)
}

let aniBTShow = () => {
  let bgmUrl = props.ani.bgmUrl;
  aniBTRef.value?.show(bgmUrl)
}

let openUrl = (url) => window.open(url)

let props = defineProps({
  ani: Object,
  editMode: {type: Boolean, default: false},
  confirmText: {type: String, default: '确定'}
})
const emit = defineEmits(['callback'])
</script>

<style scoped>
.subscription-editor{min-width:0}.editor-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px 24px;padding:4px 12px 20px 2px}.editor-form :deep(>.app-field){margin:0}.subscription-editor :deep(.app-field>label){font-size:12px;color:var(--text-secondary);font-weight:400}.editor-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 24px}.editor-options :deep(.app-checkbox){min-width:0;line-height:1.5}.editor-custom-form{display:grid;gap:20px;padding:4px 12px 20px 2px}.editor-custom-form :deep(>.app-field){margin:0}.editor-custom-form :deep(.full-width>.flex){gap:10px}.editor-custom-form :deep(.full-width>div+div){margin-top:10px}.editor-wide{grid-column:1/-1}.editor-form :deep(.app-field>div){min-width:0}.editor-form :deep(.app-number){width:100%}
.change-title-button,.source-buttons{display:flex;flex-wrap:wrap;gap:8px;margin-top:10px;justify-content:flex-start}.source-buttons .icon{width:18px;height:18px;object-fit:contain;border-radius:3px}
.tmdb-group{display:flex;align-items:center;width:100%;gap:8px;min-width:0}.tmdb-group>.app-input{flex:1}.tmdb-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:flex;align-items:center;padding:7px 12px;color:var(--muted-foreground);font-size:13px}.rss-fields{display:grid;grid-template-columns:150px minmax(0,1fr);gap:10px}.form-item-flex{width:100%;display:flex;justify-content:flex-start}.form-item-flex>*{width:100%}.editor-footer{display:flex;justify-content:space-between;gap:12px;padding-top:16px;margin-top:16px;border-top:1px solid var(--border)}.editor-submit{display:flex;gap:8px;flex-wrap:wrap}.subscription-editor :deep(.app-tabs){min-width:0}.subscription-editor :deep(.app-tabs-list){margin-bottom:16px}.subscription-editor :deep(.app-tab-content){min-width:0}.subscription-editor :deep(.full-width){min-width:0}
@media(max-width:600px){.editor-options{grid-template-columns:minmax(0,1fr);gap:8px}.editor-form{grid-template-columns:minmax(0,1fr);gap:18px;padding-right:8px}.editor-wide{grid-column:auto}.rss-fields{grid-template-columns:minmax(0,1fr)}.source-buttons{gap:6px}.source-buttons :deep(.app-button){padding-inline:10px;font-size:11px}}
</style>
