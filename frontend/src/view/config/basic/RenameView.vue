<template>
  <SettingsItem label="自动重命名">
    <AppSwitch v-model:model-value="props.config.rename"/>
  </SettingsItem>
  <SettingsItem label="重命名间隔">
    <AppNumber v-model:model-value="props.config['renameSleepSeconds']"
                     :disabled="!config.rename"
                     :min="5">
      <template #suffix>
        <span>秒</span>
      </template>
    </AppNumber>
  </SettingsItem>
  <SettingsItem label="最大文件名长度">
    <AppNumber v-model:model-value="props.config.maxFileNameLength" :min="0"/>
  </SettingsItem>
  <SettingsItem label="重命名模版">
    <div class="full-width settings-stack">
      <div>
        <AppInput v-model:model-value="props.config.renameTemplate"
                  placeholder="${title} S${seasonFormat}E${episodeFormat}"/>
      </div>
      <div>
        <AppAlert
            v-if="!testRenameTemplate(props.config.renameTemplate)"
            class="mt-8"
            type="warning"
            show-icon
            :closable="false"
        >
          <template #title>
            模板内至少需要保留 S${seasonFormat}E${episodeFormat} or S${season}E${episode} 否则会导致无法正常重命名
          </template>
        </AppAlert>
      </div>

    </div>
  </SettingsItem>
  <SettingsItem label="剔除年份" description="重命名时剔除年份，例如 (2024)。">
    <AppSwitch v-model="props.config.renameDelYear"/>
  </SettingsItem>
  <SettingsItem label="剔除TMDB ID" description="重命名时剔除 TMDB ID，例如 [tmdbid=242143]。">
    <AppSwitch v-model="props.config.renameDelTmdbId"/>
  </SettingsItem>
  <SettingsItem label="字幕独立文件夹">
    <div class="settings-stack">
      <AppSwitch v-model:model-value="props.config.subtitleIndependentFolderEnabled" disabled/>
      <AppInput v-model="config.subtitleIndependentFolderName" disabled/>
      <AppText class="mx-1" size="small">
        当前 Transmission 不支持此字幕子目录操作；原配置已保留。
      </AppText>
    </div>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";

let testRenameTemplate = renameTemplate => {
  let test = [
    'S${season}E${episode}',
    'S${seasonFormat}E${episodeFormat}'
  ]
  for (let s of test) {
    if (renameTemplate.indexOf(s) > -1) {
      return true;
    }
  }
  return false;
}

let props = defineProps(['config'])
</script>

<style scoped>
.mt-8 {
  margin-top:8px;
}

.text-extra-small {
  font-size:var(--text-caption);
}
</style>
