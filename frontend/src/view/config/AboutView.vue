<template>
  <div class="about-container">
    <div class="about-header">
      <img alt="MyAnimeGo" height="80" src="/icon.svg" width="80"/>
      <h1>MyAnimeGo</h1>
    </div>
    <div v-busy="actionLoading" class="about-actions">
      <PopconfirmView title="你确定要退出吗?" @confirm="logout">
        <template #reference><AppButton type="danger" bg text icon="Back">退出</AppButton></template>
      </PopconfirmView>
      <PopconfirmView title="你确定重启吗?" @confirm="stop(0)">
        <template #reference><AppButton bg icon="RefreshRight" text type="warning">重启</AppButton></template>
      </PopconfirmView>
      <PopconfirmView title="你确定关闭吗?" @confirm="stop(1)">
        <template #reference><AppButton bg icon="SwitchButton" text type="danger">关闭</AppButton></template>
      </PopconfirmView>
    </div>
  </div>
</template>

<script setup>
import {ref} from 'vue'
import {notify} from '@/lib/feedback'
import PopconfirmView from '@/view/custom/PopconfirmView.vue'
import {authorization} from '@/js/global'
import * as http from '@/js/http'

const actionLoading = ref(false)
const logout = () => {
  authorization.value = ''
  location.reload()
}
const stop = status => {
  actionLoading.value = true
  http.stop(status).then(res => {
    notify.success(res.message)
    setTimeout(logout, 5000)
  }).finally(() => { actionLoading.value = false })
}
</script>

<style scoped>
.about-container{display:flex;flex-direction:column;align-items:center;gap:24px}
.about-header{display:flex;align-items:center;gap:20px}
.about-header h1{margin:0;font-size:24px;font-weight:600}
.about-actions{display:flex;flex-wrap:wrap;gap:12px}
</style>
