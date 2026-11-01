<template>
  <SettingsItem label="Bangumi API 地址">
    <AppInput v-model="props.config.bgmApi" placeholder="https://api.bgm.tv"/>
    <AppText size="small">用于读取番剧名称、集数、开播日期、评分和封面。</AppText>
  </SettingsItem>
  <SettingsItem label="Bangumi Token（可选）">
    <AppInput v-model.trim="props.config.bgmToken" show-password autocomplete="new-password" placeholder="留空时匿名访问"/>
    <AppText size="small">填写后随番剧资料请求携带，用于需要认证的接口访问。</AppText>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from '@/view/custom/SettingsItem.vue'
const props = defineProps(['config'])
</script>
