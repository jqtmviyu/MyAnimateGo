<template>
  <template v-if="notificationConfig['notificationType'] === 'WEB_HOOK'">
    <SettingsItem label="Method">
      <AppSelect v-model:model-value="notificationConfig['webHookMethod']">
        <AppOption v-for="item in ['POST','GET','PUT','DELETE']"
                   :key="item"
                   :label="item"
                   :value="item"/>
      </AppSelect>
    </SettingsItem>
    <SettingsItem label="URL">
      <AppInput v-model:model-value="notificationConfig['webHookUrl']" type="textarea"
                autosize
                placeholder="https://www.xxx.com?text=test_${notification}"/>
    </SettingsItem>
    <SettingsItem label="Header">
      <AppInput
          type="textarea"
          :autosize="{ minRows: 2}"
          v-model="notificationConfig['webHookHeader']"
          :placeholder="'Authorization: Bearer xxx\nCookie: 123456'"/>
    </SettingsItem>
    <SettingsItem label="Body">
      <AppInput v-model="notificationConfig['webHookBody']"
                type="textarea"
                :autosize="{ minRows: 2}"
                placeholder='{"text":"test_${notification}"}'/>
    </SettingsItem>

  </template>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";

let props = defineProps(['notificationConfig', 'config'])
</script>
