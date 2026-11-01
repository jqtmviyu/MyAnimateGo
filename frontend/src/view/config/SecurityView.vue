<template>
  <SettingsItem label="用户名">
    <AppInput v-model:model-value="props.config.login.username" autocomplete="new-password">
      <template #prefix>
        <AppIcon class="app-input__icon">
          <User/>
        </AppIcon>
      </template>
    </AppInput>
  </SettingsItem>
  <SettingsItem label="密码">
    <AppInput v-model:model-value="props.config.login.password" autocomplete="new-password">
      <template #prefix>
        <AppIcon class="app-input__icon">
          <Key/>
        </AppIcon>
      </template>
    </AppInput>
  </SettingsItem>
  <SettingsItem label="登录有效">
    <AppNumber v-model:model-value="props.config.loginEffectiveHours" :min="0">
      <template #suffix>
        <span>小时</span>
      </template>
    </AppNumber>
  </SettingsItem>
  <SettingsItem label="其他">
    <AppCheckbox v-model="props.config['multiLoginForbidden']" label="禁止多端登录"/>
    <AppCheckbox v-model="props.config.innerIP" label="禁止公网访问"/>
    <AppCheckbox v-model="props.config.verifyLoginIp" label="如果IP发生改变登录将失效"/>
    <AppCheckbox v-model="props.config.limitLoginAttempts" label="限制尝试次数"/>
    <AppCheckbox v-model="props.config.allowCors" label="允许跨域"/>
  </SettingsItem>
  <SettingsItem label="IP白名单">
    <div class="full-width settings-stack">
      <AppSwitch v-model:model-value="config['ipWhitelist']"/>
      <div class="full-width settings-stack">
        <AppInput class="full-width" type="textarea"
                  :autosize="{ minRows: 2}"
                  :disabled="!config['ipWhitelist']"
                  :placeholder="'127.0.0.1\n192.168.1.0/24'" v-model:model-value="config['ipWhitelistStr']"/>
        <AppText class="mx-1" size="small">
          对IP白名单跳过身份验证, 换行可填写多个
        </AppText>
      </div>
    </div>
  </SettingsItem>
  <SettingsItem label="信任的反代IP">
    <div class="full-width settings-stack">
      <AppCheckbox label="启用" v-model="config.reverseProxyTrustIpListEnabled"/>
      <AppTagsInput v-model="config.reverseProxyTrustIpList"/>
    </div>
  </SettingsItem>
  <SettingsItem label="Api Key">
    <div class="flex full-width">
      <AppInput v-model:model-value="props.config.apiKey" readonly/>
      <div class="login-api-key-buttons flex">
        <AppButton bg text @click="createApiKey">生成</AppButton>
        <AppButton bg text @click="copy(props.config.apiKey)">复制</AppButton>
      </div>
    </div>
  </SettingsItem>
</template>

<script setup>
import SettingsItem from "@/view/custom/SettingsItem.vue";
import {notify} from "@/lib/feedback";
import {KeyRound as Key, UserRound as User} from "@lucide/vue";

let generateRandomString = (length) => {
  const charset = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let randomString = '';
  for (let i = 0; i < length; i++) {
    const randomIndex = Math.floor(Math.random() * charset.length);
    randomString += charset[randomIndex];
  }
  return randomString;
}

let createApiKey = () => {
  props.config.apiKey = generateRandomString(64);
}

let copy = (v) => {
  const input = document.createElement('input');
  input.value = v
  document.body.appendChild(input);
  input.select();
  document.execCommand('copy');
  document.body.removeChild(input);
  notify.success('已复制')
}

let props = defineProps(['config'])
</script>

<style scoped>
.login-api-key-buttons {
  margin-left:12px;
}
</style>
