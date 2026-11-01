<template>
  <template v-if="notificationConfig['notificationType'] === 'EMBY_REFRESH'">
    <SettingsItem label="EmbyHost">
      <AppInput v-model="notificationConfig['embyHost']" placeholder="http://x.x.x.x:8096"/>
    </SettingsItem>
    <SettingsItem label="Emby密钥">
      <AppInput v-model="notificationConfig['embyApiKey']"/>
    </SettingsItem>
    <SettingsItem label="媒体库">
      <div>
        <AppCheckboxGroup v-model="notificationConfig['embyRefreshViewIds']">
          <AppCheckbox
              v-for="view in views"
              :key="view.id"
              :label="view.name"
              :value="view.id"/>
        </AppCheckboxGroup>
        <div>
          <AppButton :loading="getEmbyViewsLoading" bg icon="Refresh" text @click="getEmbyViews"/>
        </div>
      </div>
    </SettingsItem>
    <SettingsItem label="延迟">
      <AppNumber v-model="notificationConfig['embyDelayed']"
                       class="notification-input-width"
                       :min="0">
        <template #suffix>
          <span>秒</span>
        </template>
      </AppNumber>
    </SettingsItem>
  </template>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {onMounted, ref} from "vue";
import * as http from "@/js/http.js";

const views = ref([])

const getEmbyViewsLoading = ref(false)

const getEmbyViews = () => {
  getEmbyViewsLoading.value = true
  http.getEmbyViews(props.notificationConfig)
      .then(res => {
        views.value = res.data
      })
      .finally(() => {
        getEmbyViewsLoading.value = false
      })
}

onMounted(() => {
  if (props.notificationConfig['embyHost'] && props.notificationConfig['embyApiKey']) {
    getEmbyViews()
  }
})

let props = defineProps(['notificationConfig', 'config'])
</script>
