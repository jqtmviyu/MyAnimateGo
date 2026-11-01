<template>
  <AppModal v-model="dialogVisible" center title="Bangumi">
    <div class="bgm-dialog-content">
      <div>
        <div class="bgm-search-container">
          <div class="bgm-search-input-wrapper">
            <AppInput v-model:model-value="name" @keyup.enter="search" placeholder="请输入搜索标题" clearable/>
          </div>
          <div class="bgm-search-spacer"></div>
          <AppButton @click="search" :loading="searchLoading" text bg icon="Search" :disabled="!name">搜索</AppButton>
        </div>
      </div>
      <AppTable size="small" v-model:data="list" height="500px">
        <AppColumn prop="id" label="id" width="80"/>
        <AppColumn label="封面" width="120">
          <template #default="it">
            <img :alt="list[it.$index]['name']" :src="proxyImage(list[it.$index]['images']['large'])" height="100px"
                 width="78px">
          </template>
        </AppColumn>
        <AppColumn label="名称" width="200">
          <template #default="it">
            <span>{{ list[it.$index]['nameCn'] ? list[it.$index]['nameCn'] : list[it.$index]['name'] }}</span>
          </template>
        </AppColumn>
        <AppColumn label="url" prop="url" width="240"/>
        <AppColumn>
          <template #default="it">
            <div class="flex flex-center full-width">
              <AppButton bg text @click="ok(list[it.$index])">选择</AppButton>
            </div>
          </template>
        </AppColumn>
      </AppTable>
    </div>
  </AppModal>
</template>
<script setup>
import {ref} from "vue";
import {proxyImage} from "@/js/global.js";
import * as http from "@/js/http.js";

let dialogVisible = ref(false)

let name = ref('')

let searchLoading = ref(false)
let list = ref([])

let search = () => {
  searchLoading.value = true
  http.searchBgm(name.value)
      .then(res => {
        list.value = res.data
      })
      .finally(() => {
        searchLoading.value = false
      })
}

let show = (s) => {
  name.value = ''
  if (s) {
    name.value = s
    search()
  }
  list.value = []
  dialogVisible.value = true
}

let ok = (it) => {
  emit('callback', it)
  dialogVisible.value = false
}

defineExpose({show})

const emit = defineEmits(['callback'])
</script>

<style scoped>
.bgm-dialog-content {
  min-height:300px;
}

.bgm-search-container {
  display:flex;
  width:100%;
}

.bgm-search-input-wrapper {
  flex:1;
}

.bgm-search-spacer {
  width:4px;
}
</style>
