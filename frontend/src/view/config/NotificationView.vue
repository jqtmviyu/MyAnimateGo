<template>
  <AppFold>
    <AppFoldItem title="通知模板">
      <AppInput v-model="props.config['notificationTemplate']" :autosize="{ minRows: 2}"
                placeholder="${text}" type="textarea"/>

    </AppFoldItem>
  </AppFold>
  <div class="notification-container">
    <div>
      <AppSpace wrap class="flex flex-wrap gap-4" size="small">
        <AppPanel v-for="it in props.config['notificationConfigList']" shadow="never" class="notification-card">
          <div class="flex notification-card-content">
            <div class="notification-card-main">
              <AppTooltip :content="getLabel(it['notificationType'])" placement="top">
                <p class="notification-card-title">
                  {{ getLabel(it['notificationType']) }}
                </p>
              </AppTooltip>
              <AppText line-clamp="1" size="small" class="notification-card-text" truncated>
                {{ it['comment'] ? it['comment'] : '无备注' }}
              </AppText>
            </div>
            <div>
              <AppDropdown trigger="click">
                <AppButton circle icon="MoreFilled" size="large" text type="primary"/>
                <template #dropdown>
                  <AppDropdownMenu>
                    <AppDropdownItem @click="notificationConfigRef?.show(it)">
                      <AppText type="primary">
                        <AppIcon>
                          <Edit/>
                        </AppIcon>
                        编辑
                      </AppText>
                    </AppDropdownItem>
                    <AppDropdownItem @click="del(it)">
                      <AppText type="danger">
                        <AppIcon>
                          <Delete/>
                        </AppIcon>
                        删除
                      </AppText>
                    </AppDropdownItem>
                  </AppDropdownMenu>
                </template>
              </AppDropdown>
            </div>
          </div>
        </AppPanel>
      </AppSpace>
    </div>
    <AppButton
        icon="FolderAdd"
        bg text
        type="primary"
        @click="add"
        class="notification-add-button"
        :loading="addLoading"
    >
      添加通知
    </AppButton>
  </div>
  <NotificationConfigView ref="notificationConfigRef" v-model:config="props.config"/>
</template>

<script setup>
import NotificationConfigView from "./NotificationConfigView.vue";
import {ref} from "vue";

import {getLabel} from "@/js/notification-type.js";
import {Trash2 as Delete, Pencil as Edit} from "@lucide/vue";
import {newNotification} from "@/js/http.js";

let addLoading = ref(false)

let add = () => {
  addLoading.value = true
  newNotification()
      .then((res) => {
        props.config['notificationConfigList'].push(res.data)
        notificationConfigRef.value?.show(res.data)
      })
      .finally(() => {
        addLoading.value = false
      })
}

let del = (it) => {
  props.config['notificationConfigList'] = props.config['notificationConfigList'].filter(item => item !== it)
}

let notificationConfigRef = ref()

let props = defineProps(['config'])
</script>

<style scoped>
.notification-template-link {
  width:100%;
  justify-content:end;
}

.notification-container {
  margin-top:8px;
  padding:0 4px;
}

.notification-card {
  width:190px;
  height:88px;
}

.notification-card :deep([data-slot="card-content"]) {
  height:100%;
  box-sizing:border-box;
}

.notification-card-content {
  height:100%;
  align-items:center;
  justify-content:space-between;
}

.notification-card-main {
  min-width:0;
  flex:1;
}

.notification-card-title {
  height:22px;
  margin:0 0 4px;
  overflow:hidden;
  line-height:22px;
  text-overflow:ellipsis;
  white-space:nowrap;
}

.notification-card-text {
  max-width:120px;
}

.notification-add-button {
  margin-top:12px;
}
</style>

<style>
/* 通知组件通用样式 - 非scoped以便子组件使用 */
.notification-input-width {
  width:160px !important;
}

.notification-flex-between {
  justify-content:space-between;
  width:100%;
}

.notification-margin-top-right {
  margin-top:4px;
  margin-right:4px;
}

.notification-margin-top-center {
  margin-top:4px;
  align-items:center;
}

.notification-margin-left {
  margin-left:4px;
}

.notification-flex-end {
  display:flex;
  justify-content:end;
}
</style>
