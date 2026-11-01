<template>
  <SettingsItem label="只下载最新集">
    <AppSwitch v-model:model-value="props.config.downloadNew"/>
  </SettingsItem>
  <SettingsItem label="标题添加年份">
    <AppSwitch v-model:model-value="props.config.titleYear"/>
  </SettingsItem>
  <SettingsItem label="自动剧集偏移">
    <AppSwitch v-model:model-value="props.config.offset"/>
  </SettingsItem>
  <SettingsItem label="BGM日语标题">
    <AppSwitch v-model:model-value="props.config['bgmJpName']"/>
  </SettingsItem>
  <SettingsItem label="TMDB ID" description="自动将 TMDB 编号添加到标题，例如：女仆冥土小姐。 [tmdbid=242143]">
    <AppSwitch v-model="props.config.tmdbId"/>
  </SettingsItem>
  <SettingsItem label="TMDB标题" description="自动使用 TMDB 的标题">
    <AppSwitch v-model="props.config.tmdb"/>
    <div class="settings-options settings-options-inline">
      <AppCheckbox v-model="props.config.tmdbAnime" label="仅获取动漫"/>
      <AppCheckbox v-model="props.config.tmdbOriginalName" label="使用原标题"/>
      <AppCheckbox v-model="props.config.tmdbRomaji" label="优先获取罗马音"/>
    </div>
  </SettingsItem>
  <SettingsItem label="TMDB语言">
    <AppSelect v-model:model-value="props.config['tmdbLanguage']" class="width-150">
      <AppOption v-for="item in tmdb_i18n" :value="item.i18n_tag"
                 :label="`${item.native_name} (${item.i18n_tag})`"
                 :key="item.i18n_tag">
        <div style="width: 100%;">
          <span class="float-left">{{ item.native_name }}</span>
          <AppText type="info" class="float-right" size="small">
            {{ item.i18n_tag }}
          </AppText>
        </div>
      </AppOption>
    </AppSelect>
  </SettingsItem>
  <SettingsItem label="开启全局排除">
    <AppSwitch v-model:model-value="props.config.enabledExclude" :disabled="props.config.importExclude"/>
  </SettingsItem>
  <SettingsItem label="导入全局排除">
    <AppSwitch v-model:model-value="props.config.importExclude" :disabled="props.config.enabledExclude"/>
  </SettingsItem>
  <SettingsItem label="封面质量">
    <AppSelect v-model="props.config['bgmImageSize']" class="width-150">
      <AppOption v-for="item in ['large','medium','common']" :key="item"
                 :value="item"/>
    </AppSelect>
  </SettingsItem>
  <SettingsItem label="自定义集数规则">
    <AppSwitch v-model="props.config.customEpisode"/>
    <div class="settings-options">
      <div class="flex full-width">
        <AppInput v-model="props.config.customEpisodeStr"
                  class="full-width"/>
        <div class="spacer-8"></div>
        <AppNumber v-model="props.config.customEpisodeGroupIndex"/>
      </div>
    </div>
  </SettingsItem>
  <SettingsItem label="自动上传">
    <AppSwitch v-model="props.config['upload']"/>
  </SettingsItem>
  <SettingsItem label="TMDB API 地址">
    <AppInput v-model="props.config.tmdbApi" placeholder="https://api.themoviedb.org"/>
  </SettingsItem>
  <SettingsItem label="TMDB API 密钥">
    <AppInput v-model="props.config.tmdbApiKey" show-password placeholder="留空使用系统默认"/>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {tmdb_i18n} from "@/js/tmdb-i18n.js";

let props = defineProps(['config'])
</script>

<style scoped>
.float-left {
  float:left;
}

.float-right {
  float:right;
}

.spacer-8 {
  width:8px;
}
</style>
