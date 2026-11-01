<template>
  <AppModal v-model="dialogVisible" center title="修改通知">
    <AppScroll :always="true" class="notification-config-scrollbar">
      <SettingsItem label="通知类型">
        <AppSelect v-model="notificationConfig['notificationType']">
          <AppOption v-for="it in notificationTypeList"
                     :key="it.name"
                     :label="it.label"
                     :value="it.name"
          />
        </AppSelect>
      </SettingsItem>
      <SettingsItem label="备注">
        <AppInput v-model="notificationConfig['comment']" placeholder="无备注"/>
      </SettingsItem>
      <SettingsItem label="通知状态">
        <AppCheckboxGroup v-model:model-value="notificationConfig['statusList']">
          <AppCheckbox label="开始下载" value="DOWNLOAD_START"/>
          <AppCheckbox label="下载完成" value="DOWNLOAD_END"/>
          <AppCheckbox label="缺集" value="OMIT"/>
          <AppCheckbox label="错误" value="ERROR"/>
          <AppCheckbox label="订阅完结" value="COMPLETED"/>
        </AppCheckboxGroup>
      </SettingsItem>
      <EmbyRefreshNotificationView v-model:notification-config="notificationConfig" v-model:config="props.config"/>
      <WebhookNotificationView v-model:notification-config="notificationConfig" v-model:config="props.config"/>
      <SettingsItem label="顺序">
        <div>
          <AppNumber
              v-model="notificationConfig['sort']"
              class="notification-input-width"
          />
        </div>
      </SettingsItem>
      <SettingsItem label="重试">
        <AppNumber
            :min="0"
            :max="100"
            v-model="notificationConfig['retry']"
            class="notification-input-width"
        />
      </SettingsItem>
      <SettingsItem label="开启">
        <AppSwitch v-model="notificationConfig['enable']"/>
      </SettingsItem>
    </AppScroll>
    <div class="flex notification-config-footer">
      <AppButton bg text @click="messageTest" icon="Odometer" :loading="messageTestLoading">测试
      </AppButton>
      <AppButton @click="dialogVisible = false" text bg icon="Check" type="primary">确定
      </AppButton>
    </div>
  </AppModal>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {ref} from "vue";
import EmbyRefreshNotificationView from "./notification/EmbyRefreshNotificationView.vue";
import WebhookNotificationView from "./notification/WebhookNotificationView.vue";
import {notificationTypeList} from "@/js/notification-type.js";
import {notify} from "@/lib/feedback";
import {testNotification} from "@/js/http.js";


const notificationConfig = ref({
  comment: '', notificationTemplate: '${notification}', notificationType: 'WEB_HOOK',
  webHookMethod: 'POST', webHookUrl: '', webHookHeader: '', webHookBody: '',
  embyHost: '', embyApiKey: '', embyRefreshViewIds: [], embyDelayed: 0,
  enable: true, retry: 3, sort: 10,
  statusList: ['DOWNLOAD_START', 'OMIT', 'ERROR'],
})

const messageTestLoading = ref(false)

const messageTest = () => {
  messageTestLoading.value = true

  let config = JSON.parse(JSON.stringify(notificationConfig.value))
  config.embyDelayed = 0

  testNotification(config)
      .then(res => {
        notify.success(res.message)
      })
      .finally(() => {
        messageTestLoading.value = false
      })
}

let dialogVisible = ref(false)

let props = defineProps(['config'])

let show = (newNotificationConfig) => {
  notificationConfig.value = newNotificationConfig
  dialogVisible.value = true
}

defineExpose({
  show
})

</script>

<style scoped>
.notification-config-scrollbar {
  height:530px;
  padding:15px;
}

.notification-config-footer {
  justify-content:space-between;
  width:100%;
  margin-top:8px;
}
</style>
