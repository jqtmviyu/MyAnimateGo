<template>
  <AppModal v-model="downloadPathDialogVisible" align-center center width="300"
             @close="callback" title="移动文件">
    <div>
      <strong>
        检测到修改后的下载位置发生了改动，是否将已下载文件移动到新的位置？
      </strong>
      <br>
      <AppText class="mx-1" size="small">
        {{ downloadPath }}
      </AppText>
    </div>
    <div class="action">
      <AppButton icon="Check" text bg type="danger" @click="()=>{
        move = true
        editAni()
        downloadPathDialogVisible = false
      }">移动
      </AppButton>
      <AppButton icon="Close" bg text @click="()=>{
        move = false
        editAni()
        downloadPathDialogVisible = false
      }">不移动
      </AppButton>
    </div>
  </AppModal>
  <AppModal v-model="dialogVisible" title="修改订阅" center v-if="dialogVisible">
    <AniView v-model:ani="ani" edit-mode @callback="editChange"/>
  </AppModal>
</template>

<script setup>

import {ref} from "vue";
import {notify} from "@/lib/feedback";
import {dialogs} from "@/lib/dialogs";
import AniView from "./AniView.vue";
import {aniData} from "@/js/ani.js";
import * as http from '@/js/http.js'


const dialogVisible = ref(false)
const downloadPathDialogVisible = ref(false)

const ani = ref(aniData)

let move = ref(false)
let downloadPath = ref('')
let callback = ref(() => {
})

const editChange = async (fun) => {
  callback.value = fun
  let req = await http.downloadPath(ani.value)
  downloadPath.value = req.data.downloadPath
  if (req.data.change) {
    downloadPathDialogVisible.value = true
    return
  }
  editAni()
}

const editAni = () => {
  let action = () => http.setAni(move.value, ani.value)
      .then(res => {
        notify.success(res.message)
        window.$reLoadList?.()
        dialogVisible.value = false
      })
      .finally(callback.value)

  if (!move.value) {
    action()
    return
  }

  dialogs.confirm(
      '将会移动整个文件夹, 是否执意继续?',
      '警告',
      {
        confirmButtonText: '执意继续移动',
        confirmButtonClass: 'is-text is-has-bg app-button--danger',
        cancelButtonText: '取消',
        cancelButtonClass: 'is-text is-has-bg',
        type: 'warning',
      }
  )
      .then(action).catch(() => callback.value())
}

const show = (item) => {
  ani.value = JSON.parse(JSON.stringify(item))
  ani.value.showDownlaod = true
  move.value = false
  dialogVisible.value = true
}

defineExpose({
  show
})
</script>

<style scoped>
.action {
  width:100%;
  display:flex;
  justify-content:end;
  margin-top:8px;
}
</style>
