<template>
  <SettingsItem label="RSS开关"><AppSwitch v-model="props.config.rss"/></SettingsItem>
  <SettingsItem label="RSS间隔">
    <AppNumber v-model="props.config.rssSleepMinutes" :disabled="!props.config.rss" :min="10"><template #suffix>分钟</template></AppNumber>
  </SettingsItem>
  <SettingsItem label="RSS超时">
    <AppNumber v-model="props.config.rssTimeout" :max="60" :min="6"><template #suffix>秒</template></AppNumber>
  </SettingsItem>
  <SettingsItem label="自动跳过" description="已下载的文件会自动跳过。需启用自动重命名，并确保 Transmission 与本程序使用相同的下载目录路径。">
    <AppSwitch v-model="props.config.fileExist" :disabled="!props.config.rename"/>
  </SettingsItem>
  <SettingsItem label="自动禁用订阅" description="根据 Bangumi 获取总集数，全部集数下载完成后自动禁用订阅。">
    <AppSwitch v-model="props.config.autoDisabled"/>
    <div class="settings-options settings-option-group">
      <AppCheckbox v-model="props.config.completed" :disabled="!props.config.autoDisabled" label="订阅完结迁移"/>
      <AppInput v-model="props.config.completedPathTemplate" :disabled="!props.config.autoDisabled || !props.config.completed"/>
    </div>
  </SettingsItem>
  <SettingsItem label="自动更新总集数">
    <AppSwitch v-model="props.config.updateTotalEpisodeNumber"/>
    <div class="settings-options"><AppCheckbox v-model="props.config.forceUpdateTotalEpisodeNumber" :disabled="!props.config.updateTotalEpisodeNumber" class="app-checkbox-danger" label="强制更新"/></div>
  </SettingsItem>
  <SettingsItem label="自动跳过X.5集"><AppSwitch v-model="props.config.skip5"/></SettingsItem>
  <SettingsItem label="遗漏检测" description="总开关。检测到 RSS 中缺少集数时发送通知。"><AppSwitch v-model="props.config.omit"/></SettingsItem>
  <SettingsItem label="备用RSS">
    <AppSwitch v-model="props.config.standbyRss"/>
    <div class="settings-options"><AppCheckbox v-model="props.config.coexist" :disabled="!props.config.standbyRss" label="多字幕组共存模式"/></div>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";

let props = defineProps(['config'])
</script>

<style scoped>
.text-extra-small {
  font-size:var(--text-caption);
}

.justify-end {
  justify-content:end;
}
</style>
