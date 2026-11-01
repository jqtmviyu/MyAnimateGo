<template>
  <AppModal
      v-model="dialogVisible"
      align-center
      center
      title="更换封面"
      width="min(680px, calc(100vw - 24px))">
    <div class="cover-layout">
      <div class="cover-preview-column">
        <div class="cover-preview">
          <AppImage
              v-if="previewUrl"
              :alt="ani.title"
              :preview-src-list="[previewUrl]"
              :src="previewUrl"
              fit="cover"
              hide-on-click-modal>
            <template #error>
              <div class="cover-placeholder">
                <AppIcon>
                  <Picture/>
                </AppIcon>
              </div>
            </template>
          </AppImage>
          <div v-else class="cover-placeholder">
            <AppIcon>
              <Picture/>
            </AppIcon>
          </div>
        </div>
        <AppTooltip :content="ani.title" placement="bottom">
          <AppText class="cover-anime-title" truncated>{{ ani.title }}</AppText>
        </AppTooltip>
      </div>

      <div class="cover-controls">
        <section class="cover-source-section">
          <div class="cover-source-title">
            <AppIcon>
              <LinkIcon/>
            </AppIcon>
            <span>图片链接</span>
          </div>
          <div class="cover-url-row">
            <AppInput
                v-model="ani.image"
                :prefix-icon="LinkIcon"
                clearable
                placeholder="https://lain.bgm.tv/pic/cover/1234.jpg"
                @keyup.enter="refreshCover"/>
            <AppTooltip content="获取封面" placement="top">
              <AppButton
                  :disabled="!ani.image"
                  :loading="refreshLoading"
                  bg
                  icon="Refresh"
                  @click="refreshCover"/>
            </AppTooltip>
          </div>
        </section>

        <section class="cover-source-section">
          <div class="cover-source-title">
            <AppIcon>
              <UploadFilled/>
            </AppIcon>
            <span>本地文件</span>
          </div>
          <UploadView
              url="api/upload"
              :extensions="['jpg', 'jpeg', 'png']"
              :callback="uploadCallback">
            <AppButton bg icon="Upload">
              选择图片
            </AppButton>
          </UploadView>
        </section>
      </div>
    </div>

    <template #footer>
      <div class="cover-footer">
        <AppButton @click="dialogVisible = false" bg text icon="Close">取消</AppButton>
        <AppButton :loading="saveLoading" type="primary" bg text @click="save" icon="Check">
          保存
        </AppButton>
      </div>
    </template>
  </AppModal>
</template>

<script setup>
import {computed, ref} from "vue";
import {notify} from "@/lib/feedback";
import {Link, Image as Picture, Upload as UploadFilled} from "@lucide/vue";
import {toApiFile} from "@/js/global.js";
import * as http from "@/js/http.js";
import UploadView from "@/view/custom/UploadView.vue";

const dialogVisible = ref(false)
const ani = ref({})
const time = ref()
const refreshLoading = ref(false)
const saveLoading = ref(false)
let sourceAni

const previewUrl = computed(() => ani.value.cover
    ? `${toApiFile(ani.value.cover)}&t=${time.value}`
    : '')

const refreshCover = () => {
  refreshLoading.value = true
  http.refreshCover(ani.value)
      .then(res => {
        time.value = new Date().getTime()
        ani.value.cover = res.data
      })
      .finally(() => {
        refreshLoading.value = false
      })
}

const uploadCallback = res => {
  const {code, message, data} = res
  if (code === 200) {
    ani.value.cover = data
    time.value = new Date().getTime()
    return
  }
  notify.error(message)
}

const show = newAni => {
  sourceAni = newAni
  time.value = new Date().getTime()
  ani.value = JSON.parse(JSON.stringify(newAni))
  dialogVisible.value = true
}

const save = () => {
  saveLoading.value = true
  http.setAni(false, ani.value)
      .then(res => {
        notify.success(res.message)
        if (sourceAni) {
          Object.assign(sourceAni, ani.value)
        }
        window.$reLoadList?.()
        dialogVisible.value = false
      })
      .finally(() => {
        saveLoading.value = false
      })
}

defineExpose({show})
</script>

<style scoped>
.cover-layout {
  min-width:0;
  display:grid;
  grid-template-columns:190px minmax(0, 1fr);
  gap:22px;
  align-items:start;
}

.cover-preview-column {
  min-width:0;
  display:grid;
  justify-items:center;
  gap:8px;
}

.cover-preview {
  width:190px;
  aspect-ratio:2 / 3;
  overflow:hidden;
  border:1px solid var(--border);
  border-radius:8px;
  background-color:var(--muted);
}

.cover-preview :deep(.app-image) {
  width:100%;
  height:100%;
  display:block;
}

.cover-placeholder {
  width:100%;
  height:100%;
  display:flex;
  align-items:center;
  justify-content:center;
  color:var(--muted-foreground);
  background-color:var(--muted);
  font-size:34px;
}

.cover-anime-title {
  display:block;
  max-width:190px;
  font-weight:600;
  text-align:center;
}

.cover-controls {
  min-width:0;
  display:grid;
}

.cover-source-section {
  min-width:0;
  padding:4px 0 18px;
  border-bottom:1px solid var(--border);
}

.cover-source-section + .cover-source-section {
  padding-top:18px;
  border-bottom:none;
}

.cover-source-title {
  display:flex;
  align-items:center;
  gap:7px;
  margin-bottom:10px;
  color:var(--foreground);
  font-size:14px;
  font-weight:600;
}

.cover-url-row {
  min-width:0;
  display:grid;
  grid-template-columns:minmax(0, 1fr) 34px;
  gap:8px;
}

.cover-url-row .app-button {
  width:34px;
  height:32px;
  padding:0;
}

.cover-footer {
  display:flex;
  justify-content:flex-end;
}

@media (max-width: 600px) {
  .cover-layout {
    grid-template-columns:minmax(0, 1fr);
    gap:18px;
  }

  .cover-preview {
    width:min(170px, 52vw);
  }

  .cover-anime-title {
    max-width:min(240px, 80vw);
  }
}
</style>
