<template>
  <SettingsItem label="Mikan">
    <AppInput v-model:model-value="props.config.mikanHost" placeholder="https://mikanani.me"/>
  </SettingsItem>
  <SettingsItem label="最大日志条数">
    <div class="width-150">
      <AppSelect v-model:model-value="props.config.logsMax">
        <AppOption v-for="it in [128,256,512]" :key="it" :label="it" :value="it"/>
      </AppSelect>
    </div>
  </SettingsItem>
  <SettingsItem label="DEBUG">
    <AppSwitch v-model:model-value="props.config.debug"/>
  </SettingsItem>
  <SettingsItem label="缓存">
    <div class="full-width">
      <div>
        <AppButton :loading="clearCacheLoading" bg icon="Delete" @click="clearCache">清理</AppButton>
      </div>
      <div>
        <AppText class="mx-1" size="small">
          清理现在不被使用的缓存
        </AppText>
      </div>
    </div>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {notify} from "@/lib/feedback";
import {ref} from "vue";
import * as http from "@/js/http.js";


let clearCacheLoading = ref(false)
let clearCache = () => {
  clearCacheLoading.value = true
  http.clearCache()
      .then(res => {
        notify.success(res.message);
      })
      .finally(() => {
        clearCacheLoading.value = false
      })
}


let props = defineProps(['config'])
</script>
