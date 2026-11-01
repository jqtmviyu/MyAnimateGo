<template>
  <section class="migration-review space-y-4 rounded-xl border border-border bg-card p-4 text-sm leading-relaxed">
    <h3 class="font-semibold">迁移审核与自动任务</h3>
    <AppAlert :closable="false" :type="config.automaticTasksPaused ? 'warning' : 'success'"
              :title="config.automaticTasksPaused ? '自动任务已暂停' : '自动任务已启用'"/>
    <p>启用后将处理待执行任务，并按设置下载、重命名、删除文件和发送通知。请先核对订阅路径、下载器关联和通知配置。</p>
    <AppFold v-if="config.migrationReport" v-model="openSections">
      <AppFoldItem title="查看迁移报告" name="report">
        <p>来源版本：{{ config.migrationReport.declaredVersion || config.migrationReport.sourceVersion }}；
          导入订阅：{{ config.migrationReport.subscriptionCount }} 条。报告记录导入时状态。</p>
        <ul v-if="config.migrationReport.warnings?.length">
          <li v-for="warning in config.migrationReport.warnings" :key="warning">{{ warning }}</li>
        </ul>
        <p>待确认字段：{{ config.migrationReport.pendingFields?.join('、') || '无' }}</p>
        <p>未转换的文件或目录：{{ config.migrationReport.unconvertedPaths === undefined ? '此报告未记录，请重新预览原始来源' : (config.migrationReport.unconvertedPaths?.join('、') || '无') }}</p>
        <p>未识别字段：{{ config.migrationReport.unknownFields?.join('、') || '无' }}</p>
        <details>
          <summary>完整迁移报告</summary>
          <pre class="migration-report">{{ JSON.stringify(config.migrationReport, null, 2) }}</pre>
        </details>
      </AppFoldItem>
    </AppFold>
    <p v-else>此数据没有已保存的迁移报告。已有数据请结合导入命令输出核对；新建实例请核对当前设置。</p>
    <AppCheckbox v-if="config.automaticTasksPaused" v-model="reviewed">
      已核对迁移报告或导入输出、待确认项和当前设置
    </AppCheckbox>
    <div class="task-actions">
      <AppButton v-if="config.automaticTasksPaused" type="primary" :disabled="!reviewed" :loading="busy" @click="changeState(false)">启用自动任务</AppButton>
      <AppButton v-else type="warning" :loading="busy" @click="changeState(true)">暂停自动任务</AppButton>
    </div>
    <p>暂停会取消正在运行任务的可取消操作，已经完成的下载器或文件操作仍会保留。</p>
  </section>
</template>

<script setup>
import {ref} from 'vue';
import {notify} from "@/lib/feedback";
import {dialogs} from "@/lib/dialogs";
import {setAutomaticTasksPaused} from '@/js/http.js';

const props = defineProps(['config'])
const reviewed = ref(false)
const openSections = ref(['report'])
const busy = ref(false)

const changeState = async paused => {
  if (!paused) {
    try {
      await dialogs.confirm('启用后将执行待处理任务，并可能下载、移动或删除文件以及发送通知。确认已完成核对并启用？', '启用自动任务', {confirmButtonText: '确认启用', cancelButtonText: '取消', type: 'warning'})
    } catch {
      return
    }
  }
  busy.value = true
  try {
    const result = await setAutomaticTasksPaused(paused, reviewed.value)
    props.config.automaticTasksPaused = result.data.automaticTasksPaused
    reviewed.value = false
    notify.success(result.message)
  } catch {
    // 请求层已显示服务端错误，保留页面当前状态以便重试。
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.migration-review { margin-bottom:20px; }
.migration-report { white-space:pre-wrap; overflow-wrap:anywhere; }
.task-actions { margin-top:12px; }
</style>
