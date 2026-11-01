<template>
  <BgmView ref="bgmRef" @callback="bgmAdd"/>
  <CollectionPreviewView ref="collectionPreviewRef" v-model:data="data"/>
  <AppModal v-model="dialogVisible"
             center
             title="添加合集">
    <div v-busy="loading" style="height: 500px;">
      <AppScroll style="padding: 0 12px;">
        <div>
          <AppForm @submit.prevent label-width="auto">
            <AppField label="番剧名称">
              <div class="full-width">
                <div class="flex full-width">
                  <AppInput
                      v-model:model-value="data.ani.title"
                      :disabled="rssButtonLoading"
                      placeholder="请勿留空"
                      @keyup.enter="bgmRef?.show(data.ani.title)"
                  />

                  <AppButton :disabled="rssButtonLoading" bg icon="Search" text type="primary"
                             @click="bgmRef?.show(data.ani.title)"/>
                </div>
                <div v-if="data.show" class="change-title-button">
                  <AppButton :loading="getBgmNameLoading"
                             bg
                             icon="DocumentAdd" text @click="getBgmName">
                    使用Bangumi
                  </AppButton>
                  <AppButton :disabled="data.ani.title === data.ani.themoviedbName || !data.ani.themoviedbName.length"
                             bg
                             icon="DocumentAdd"
                             text
                             @click="data.ani.title = data.ani.themoviedbName">
                    使用TMDB
                  </AppButton>
                </div>
              </div>
            </AppField>
            <template v-if="data.show">
              <AppField label="TMDB">
                <div class="flex full-width" style="justify-content: space-between;">
                  <div class="app-input is-disabled">
                    <div class="app-input__wrapper"
                         style="pointer-events: auto;cursor: auto;justify-content: left;padding: 0 11px;"
                         tabindex="-1">
                      <AppLink v-if="data.ani?.tmdb?.id"
                               :href="`https://www.themoviedb.org/${data.ani.ova ? 'movie' : 'tv'}/${data.ani.tmdb.id}`"
                               target="_blank"
                               type="primary">
                        {{ data.ani.themoviedbName }}
                      </AppLink>
                      <span v-else>{{ data.ani.themoviedbName }}</span>
                    </div>
                  </div>

                  <AppButton :loading="getThemoviedbNameLoading" bg icon="Refresh" text @click="getThemoviedbName"/>
                </div>
              </AppField>
              <AppField label="字幕组">
                <div class="form-item-flex">
                  <AppInput v-model:model-value="data.ani.subgroup" placeholder="字幕组" class="width-150"/>
                </div>
              </AppField>
              <AppField label="季">
                <div class="form-item-flex">
                  <AppNumber v-model:model-value="data.ani.season" :min="0" class="width-150"/>
                </div>
              </AppField>
              <AppField label="集数偏移">
                <div class="form-item-flex">
                  <AppNumber v-model:model-value="data.ani.offset"/>
                </div>
              </AppField>
              <AppField label="日期">
                <div class="form-item-flex">
                  <AppDate
                      v-model="data.ani.releaseDate"
                      style="max-width: 150px;"
                  />
                </div>
              </AppField>
              <AppField label="匹配">
                <ExcludeView v-model:exclude="data.ani.match" :import-exclude="false"/>
              </AppField>
              <AppField label="排除">
                <ExcludeView v-model:exclude="data.ani.exclude" :import-exclude="true"/>
              </AppField>
              <AppField label="全局排除">
                <AppSwitch v-model:model-value="data.ani['globalExclude']"/>
              </AppField>
              <AppField label="剧场版">
                <AppSwitch v-model:model-value="data.ani.ova"/>
              </AppField>
              <AppField label="自定义集数规则">
                <div class="flex full-width">
                  <AppInput v-model:model-value="data.ani.customEpisodeStr"
                            class="full-width"/>

                  <AppNumber v-model:model-value="data.ani.customEpisodeGroupIndex"/>
                </div>
              </AppField>
              <AppField label="下载位置">
                <div class="full-width">
                  <AppInput v-model:model-value="data.ani.customDownloadPathTemplate" :autosize="{ minRows: 2}"
                            class="full-width"
                            type="textarea"/>
                </div>
                <div style="margin-top: 6px;">
                  <AppButton :disabled="!data.ani.customDownloadPath"
                             :loading="downloadPathLoading" bg icon="Refresh"
                             text
                             @click="downloadPath"/>
                </div>
              </AppField>
              <AppField label="重命名模版">
                <div class="full-width">
                  <AppSwitch v-model="data.ani['customRenameTemplateEnable']"/>
                  <br>
                  <AppInput v-model:model-value="data.ani['customRenameTemplate']"
                            :disabled="!data.ani['customRenameTemplateEnable']"
                            placeholder="${title} S${seasonFormat}E${episodeFormat}"/>
                  <br>

                </div>
              </AppField>
              <AppField label="自定义标签">
                <CustomTagsView :config="data.ani"/>
              </AppField>
              <AppField label="Torrent">
                <AppTag v-if="data.filename" closable @close="clearTorrent">
                  <AppTooltip :content="data.filename">
                    <AppText line-clamp="1" size="small" class="filename">
                      {{ data.filename }}
                    </AppText>
                  </AppTooltip>
                </AppTag>
                <UploadView v-else
                            url="api/uploadAndReadToBase64"
                            :extensions="['torrent']"
                            :callback="uploadCallback">
                  <AppButton bg icon="Upload">选择并上传种子</AppButton>
                </UploadView>
              </AppField>
            </template>
          </AppForm>
        </div>
      </AppScroll>
    </div>
    <div class="action">
      <AppButton :disabled="!data.filename" bg
                 icon="Grid"
                 text
                 @click="collectionPreviewRef?.show">
        预览
      </AppButton>
      <AppButton :disabled="!data.filename"
                 :loading="startLoading"
                 bg
                 icon="Check"
                 text
                 type="primary" @click="start">
        开始
      </AppButton>
    </div>
  </AppModal>
</template>

<script setup>
import {ref} from "vue";
import {notify} from "@/lib/feedback";
import {dialogs} from "@/lib/dialogs";
import BgmView from "./BgmView.vue";
import ExcludeView from "@/view/config/ExcludeView.vue";
import CollectionPreviewView from "./CollectionPreviewView.vue";
import CustomTagsView from "@/view/config/CustomTagsView.vue";
import {aniData} from "@/js/ani.js";
import * as http from "@/js/http.js";
import {getBgmTitle} from "@/js/http.js";
import UploadView from "@/view/custom/UploadView.vue";

let start = () => {
  startLoading.value = true
  http.startCollection(data.value)
      .then((res) => {
        dialogs.confirm(
            res.message,
            '合集任务',
            {
              confirmButtonText: '确定',
              confirmButtonClass: 'is-text is-has-bg app-button--primary',
              type: 'success',
              center: true,
              showCancelButton: false
            }
        )
      })
      .finally(() => {
        startLoading.value = false
      })
}

let downloadPathLoading = ref(false)
let downloadPath = () => {
  downloadPathLoading.value = true
  let newAni = JSON.parse(JSON.stringify(data.value.ani))
  newAni.customDownloadPath = false
  http.downloadPath(newAni)
      .then(res => {
        data.value.ani.customDownloadPathTemplate = res.data.downloadPath
      })
      .finally(() => {
        downloadPathLoading.value = false
      })
}

let collectionPreviewRef = ref()

let startLoading = ref(false);

let bgmRef = ref()

let rssButtonLoading = ref(false)
let loading = ref(false)

// 清空已经上传的种子文件及其 Base64 内容。
let clearTorrent = () => {
  data.value.filename = ''
  data.value.torrent = ''
}

let bgmAdd = (bgm) => {
  loading.value = true
  data.value.show = false
  data.value.torrent = ''
  data.value.filename = ''
  http.getAniBySubjectId(bgm['id'])
      .then((res) => {
        data.value.ani = res.data
        data.value.ani.subgroup = '未知字幕组'
        data.value.ani.customEpisode = true
        data.value.show = true
        data.value.ani.match = []
        data.value.ani.exclude = ['^(SPs?|CDs|Scans|PV|menu)/', 'Fonts|NCED|NCOP|迷你动画']
      })
      .finally(() => {
        loading.value = false
      })
}

let uploadCallback = (res, file) => {
  data.value.filename = file.name
  data.value.torrent = res.data
  // 获取字幕组
  http.getCollectionSubgroup(data.value)
      .then(res => {
        data.value.ani.subgroup = res.data
        if (res.data !== '未知字幕组') {
          notify.success(`字幕组已更新为 ${res.data}`)
        }
      })
}

let data = ref({
  filename: '',
  torrent: '',
  ani: aniData,
  show: false,
})

let dialogVisible = ref(false)

let show = () => {
  data.value.show = false
  data.value.ani.title = ''
  data.value.torrent = ''
  data.value.filename = ''
  dialogVisible.value = true
}

let getBgmNameLoading = ref(false)

let getBgmName = () => {
  getBgmNameLoading.value = true
  getBgmTitle(data.value.ani)
      .then(res => {
        data.value.ani.title = res.data
      })
      .finally(() => {
        getBgmNameLoading.value = false
      })
}

let getThemoviedbNameLoading = ref(false)

let getThemoviedbName = () => {
  if (!data.value.ani.title.length) {
    return
  }

  getThemoviedbNameLoading.value = true
  http.getThemoviedbName(data.value.ani)
      .then(res => {
        notify.success(res.message)
        data.value.ani['themoviedbName'] = res.data['themoviedbName']
        data.value.ani['tmdb'] = res.data['tmdb']
      })
      .finally(() => {
        getThemoviedbNameLoading.value = false
      })
}


defineExpose({show})

</script>

<style scoped>
.change-title-button {
  width:100%;
  justify-content:end;
  display:flex;
  margin-top:12px;
}

.form-item-flex {
  width:100%;
  display:flex;
  justify-content:end;
}

.filename {
  max-width:300px;
  color:var(--muted-foreground);
}

.action {
  width:100%;
  display:flex;
  justify-content:space-between;
  margin-top:10px;
}
</style>
