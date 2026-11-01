<template>
  <AppModal
      v-model="dialogVisible"
      title="导入数据"
      width="500px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
  >
    <div class="import-dialog">
      <!-- 文件上传区域 -->
      <div class="upload-section">
        <div class="section-title">
          <AppIcon>
            <document/>
          </AppIcon>
          <span>选择文件</span>
        </div>
        <div v-if="data.filename" class="file-selected">
          <AppTag
              closable
              @close="data.filename = ''"
              type="success"
              size="large"
              class="file-tag"
          >
            <AppIcon>
              <document/>
            </AppIcon>
            {{ data.filename }}
          </AppTag>
          <div class="file-info">
            <AppText type="info" size="small">
              已选择文件，共 {{ data.aniList.length }} 条数据
            </AppText>
          </div>
        </div>
        <UploadView url="api/uploadAndRead"
                    :extensions="['json']"
                    :callback="uploadCallback">
          <AppButton bg icon="Upload">选择并上传文件</AppButton>
        </UploadView>
      </div>

      <!-- 冲突处理设置 -->
      <div class="conflict-section" v-if="data.filename">
        <div class="section-title">
          <AppIcon>
            <setting/>
          </AppIcon>
          <span>冲突处理</span>
        </div>
        <div class="conflict-content">
          <AppRadioGroup v-model="data.conflict" class="conflict-options">
            <AppRadio value="REPLACE" class="conflict-option">
              <div class="option-content">
                <div class="option-title">替换现有数据</div>
                <div class="option-desc">用新数据覆盖同名的现有数据</div>
              </div>
            </AppRadio>
            <AppRadio value="SKIP" class="conflict-option">
              <div class="option-content">
                <div class="option-title">跳过冲突数据</div>
                <div class="option-desc">保留现有数据，跳过重复项</div>
              </div>
            </AppRadio>
          </AppRadioGroup>
        </div>
      </div>

      <!-- 操作按钮 -->
      <div class="action-section">
        <AppButton
            @click="dialogVisible = false"
            size="large"
            icon="Close"
        >
          取消
        </AppButton>
        <AppButton
            type="primary"
            :loading="importDataLoading"
            :disabled="!data.filename"
            @click="startImport"
            size="large"
            icon="Upload"
        >
          开始导入
        </AppButton>
      </div>
    </div>
  </AppModal>
</template>
<script setup>
import {getCurrentInstance, ref} from "vue";
import {FileText as Document, Settings2 as Setting} from "@lucide/vue";
import {notify} from "@/lib/feedback";
import {importAni} from "@/js/http.js";
import UploadView from "@/view/custom/UploadView.vue";

let importDataLoading = ref(false);

let startImport = () => {
  importDataLoading.value = true;
  importAni(data.value)
      .then(res => {
        notify.success(res.message)
        if (instance.vnode.props.onCallback) {
          emit('callback')
        }
        window.$reLoadList?.()
        dialogVisible.value = false
      })
      .finally(() => {
        importDataLoading.value = false
      })
}

let dialogVisible = ref(false);
let data = ref({
  filename: '',
  aniList: [],
  conflict: 'REPLACE'
})

let uploadCallback = (res, file) => {
  let {code, message} = res
  if (code === 200) {
    data.value.filename = file.name
    data.value.aniList = JSON.parse(res.data)
    return
  }
  notify.error(message)
}

let show = () => {
  data.value = {
    filename: '',
    aniList: [],
    conflict: 'REPLACE'
  }
  dialogVisible.value = true;
}

defineExpose({show})

const instance = getCurrentInstance()

const emit = defineEmits(['callback'])
</script>

<style scoped>
.import-dialog {
  padding:0;
}

.section-title {
  display:flex;
  align-items:center;
  gap:8px;
  font-weight:600;
  color:var(--foreground);
  margin-bottom:16px;
  font-size:16px;
}

.upload-section {
  margin-bottom:8px;
}

.file-selected {
  display:flex;
  flex-direction:column;
  gap:12px;
  padding:16px;
  background:var(--popover);
  border:1px solid var(--border);
  border-radius:8px;
  margin-bottom:8px;
}

.file-tag {
  align-self:flex-start;
  font-size:14px;
  padding:8px 12px;
}

.file-info {
  margin-top:4px;
}

.upload-sub-text em {
  color:var(--success);
  font-style:normal;
}

.conflict-section {
  margin-bottom:24px;
  padding:20px;
  border-radius:8px;
  background:var(--popover);
  border:1px solid var(--border);
}

.conflict-content {
  margin-top:12px;
}

.conflict-options {
  display:flex;
  flex-direction:column;
  gap:12px;
}

.conflict-option {
  margin:0;
  padding:12px;
  border:1px solid var(--border);
  border-radius:6px;
  transition:all 0.3s;
  box-sizing:content-box;
}

.conflict-option:hover,
.conflict-option.is-checked {
  border-color:var(--primary);
  background:var(--muted);
}

.option-content {
  margin-left:8px;
  width:200px;
}

.option-title {
  font-weight:500;
  color:var(--foreground);
  margin-bottom:4px;
}

.option-desc {
  font-size:12px;
  color:var(--muted-foreground);
  line-height:1.4;
}

.action-section {
  display:flex;
  justify-content:flex-end;
  gap:12px;
  padding-top:16px;
  border-top:1px solid var(--border);
}
</style>
