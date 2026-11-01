<template>
  <SettingsItem label="外观"><AppSelect v-model="store" class="width-200"><AppOption value="dark" label="暗黑模式"/><AppOption value="light" label="白天模式"/><AppOption value="auto" label="跟随系统"/></AppSelect></SettingsItem>
  <SettingsItem label="主题色">
    <AppColor v-model="color" :predefine="predefineColors"
                     @blur="colorChange(color)"
                     @change="colorChange(color)"
                     @active-change="colorChange"/>
  </SettingsItem>
  <SettingsItem label="排序">
    <AppSelect v-model="props.config['sortType']" class="width-150">
      <AppOption value="SCORE" label="评分"/>
      <AppOption value="PINYIN" label="拼音"/>
      <AppOption value="DOWNLOAD_TIME" label="更新时间"/>
    </AppSelect>
  </SettingsItem>
  <SettingsItem label="点击封面">
    <AppSelect v-model="coverClickAction" class="width-150">
      <AppOption label="编辑订阅" value="edit"/>
      <AppOption label="编辑封面" value="cover"/>
    </AppSelect>
  </SettingsItem>
  <SettingsItem label="启动页">
    <AppSelect v-model="startupPage" class="width-150">
      <AppOption label="全部番剧" value="/catalog"/>
      <AppOption label="我的订阅" value="/subscriptions"/>
      <AppOption label="日志" value="/logs"/>
    </AppSelect>
  </SettingsItem>
  <SettingsItem label="其他">
    <AppCheckbox v-model="showScore" label="显示评分"/>
    <AppCheckbox v-model="showWeek" label="按星期展示"/>
  </SettingsItem>
</template>

<script setup>
import {ref} from "vue";
import {
  store,
  color,
  colorChange,
  coverClickAction,
  showScore,
  showWeek,
  startupPage,
} from "@/js/global.js";
import SettingsItem from "@/view/custom/SettingsItem.vue";

let predefineColors = ref([
  '#409eff', '#52B54B', '#BF3545', '#CB7574',
  '#9AAEC7', '#2EC5B6', '#1C1C1C', '#F7B1A9',
  '#B18874', '#E9BA86', '#F68F6C', '#F0458B',
  '#C35653', '#40494E', '#6F0000', '#8D3647',
  '#E6C5D0', '#2377B3', '#49312D', '#7C9AB6',
  '#A5B18D', '#E8662A', '#AB5D50'
])

let props = defineProps(['config'])
</script>
