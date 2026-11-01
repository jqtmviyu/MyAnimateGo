<template>
  <div
      class="download-settings-form full-width">
    <section class="settings-section">
      <div class="settings-section-heading">
        <h3>Transmission 连接</h3>
      </div>
      <SettingsItem label="地址">
        <AppInput
            v-model.trim="props.config.downloadToolHost"
            placeholder="Transmission 服务地址"
            autocomplete="new-password"/>
      </SettingsItem>
      <SettingsItem label="用户名">
        <AppInput
            v-model.trim="props.config.downloadToolUsername"
            placeholder="username"
            autocomplete="new-password">
          <template #prefix>
            <AppIcon class="app-input__icon"><User/></AppIcon>
          </template>
        </AppInput>
      </SettingsItem>
      <SettingsItem label="密码">
        <AppInput
            v-model.trim="props.config.downloadToolPassword"
            placeholder="password"
            show-password
            autocomplete="new-password">
          <template #prefix>
            <AppIcon class="app-input__icon"><Key/></AppIcon>
          </template>
        </AppInput>
      </SettingsItem>
      <SettingsItem>
        <div class="download-test-button">
          <AppButton @click="downloadLoginTest" bg text :loading="downloadLoginTestLoading" icon="Odometer">测试连接
          </AppButton>
        </div>
      </SettingsItem>
    </section>

    <section class="settings-section">
      <div class="settings-section-heading">
        <h3>保存与清理</h3>
      </div>
      <SettingsItem label="保存位置">
        <AppInput v-model.trim="props.config['downloadPathTemplate']"/>
        <div class="full-width margin-top-4" v-if="!testPathTemplate(props.config['downloadPathTemplate'])">
          <AppAlert
              type="warning"
              show-icon
              :closable="false"
          >
            <template #title>
              你的 保存位置 并未按照模版填写, 可能会遇到下载位置错误
            </template>
          </AppAlert>
        </div>
      </SettingsItem>
      <SettingsItem label="剧场版保存位置">
        <AppInput v-model.trim="props.config['ovaDownloadPathTemplate']"/>
        <div class="full-width margin-top-4" v-if="!testPathTemplate(props.config['ovaDownloadPathTemplate'])">
          <AppAlert
              type="warning"
              show-icon
              :closable="false"
          >
            <template #title>
              你的 剧场版保存位置 并未按照模版填写, 可能会遇到下载位置错误
            </template>
          </AppAlert>
        </div>
      </SettingsItem>
      <SettingsItem label="自动删除" description="自动删除已完成的任务">
        <AppSwitch v-model="props.config.delete"/>
        <div class="settings-stack">
          <p class="settings-help">如果同时开启了 <strong>备用 RSS 功能</strong>，将自动删除对应洗版视频，实现主 RSS 替换。</p>
          <div class="settings-options">
            <AppCheckbox v-model="props.config.awaitStalledUP" :disabled="!props.config.delete" label="等待做种完毕"/>
            <div class="settings-option-group">
              <AppCheckbox v-model="props.config.deleteStandbyRSSOnly" :disabled="!props.config.delete" label="仅在主 RSS 更新后删除备用 RSS"/>
              <p class="settings-help">主 RSS 不会自动删除，仅在其更新后删除对应备用 RSS 的任务与文件。</p>
            </div>
          </div>
        </div>
      </SettingsItem>
    </section>

    <section class="settings-section">
      <div class="settings-section-heading">
        <h3>任务控制</h3>
      </div>
      <SettingsItem label="失败重试次数">
        <AppNumber v-model:model-value="props.config['downloadRetry']" :max="100" :min="3"/>
      </SettingsItem>
      <SettingsItem label="同时下载限制">
        <div class="settings-stack">
          <AppNumber v-model:model-value="props.config.downloadCount" :min="0"/>
          <AppText class="mx-1" size="small">
            设置为 0 时不做限制
          </AppText>
        </div>
      </SettingsItem>
      <SettingsItem label="延迟下载">
        <AppNumber v-model:model-value="props.config.delayedDownload" :min="0">
          <template #suffix>
            <span>分钟</span>
          </template>
        </AppNumber>
      </SettingsItem>
      <SettingsItem label="自定义标签">
        <CustomTagsView :config="props.config"/>
      </SettingsItem>
    </section>

  </div>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {ref} from "vue";
import {notify} from "@/lib/feedback";
import {KeyRound as Key, UserRound as User} from "@lucide/vue";
import CustomTagsView from "@/view/config/CustomTagsView.vue";
import * as http from "@/js/http.js";

const downloadLoginTestLoading = ref(false)
const downloadLoginTest = () => {
  downloadLoginTestLoading.value = true
  http.downloadLoginTest(props.config)
      .then(res => {
        notify.success(res.message)
      })
      .finally(() => {
        downloadLoginTestLoading.value = false
      })
}

let testPathTemplate = (path) => {
  return new RegExp('\\$\{[A-z]+\}').test(path);
}


let props = defineProps(['config'])
</script>

<style scoped>
.settings-section + .settings-section {
  margin-top:28px;
  padding-top:24px;
  border-top:1px solid var(--border);
}

.settings-section-heading {
  margin-bottom:18px;
}

.settings-section-heading h3 {
  margin:0;
  color:var(--foreground);
  font-size:15px;
  font-weight:650;
  line-height:1.4;
}

.download-test-button {
  display:flex;
  width:100%;
  justify-content:flex-end;
}

.download-danger-text {
  color:var(--destructive);
}

.settings-section-advanced :deep(.app-collapse) {
  border-top:0;
}

@media (max-width: 700px) {
  .settings-section + .settings-section {
    margin-top:22px;
    padding-top:20px;
  }

  .settings-section-heading {
    margin-bottom:15px;
  }
}
</style>
