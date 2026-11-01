<script setup>
import {computed,inject,ref,watch} from 'vue'
import {ContextMenuRoot,ContextMenuTrigger,ContextMenuPortal,ContextMenuContent,ContextMenuItem,ContextMenuSeparator} from 'reka-ui'
import {layerContext} from '@/components/app/layers'
import {downloadPreferences} from './table-layout'
import DataRows from './DataRows.vue'
import {trackerColumns,peerColumns} from './detail-columns'
import {ArrowDown, ArrowUp, CheckCheck, Pause, Play, Radio, RefreshCw} from '@lucide/vue'
import TorrentFiles from './TorrentFiles.vue'
import TorrentPieces from './TorrentPieces.vue'
import TorrentProperties from './TorrentProperties.vue'
import {bytes, speed, percent, ratio, duration, dateTime, taskStatus, taskError, trackerState} from './transmission'

const props=defineProps({task:{type:Object,required:true},tab:{type:String,default:'overview'},loading:Boolean,refreshing:Boolean,busy:Boolean,fields:{type:Array,default:()=>[]},rpcVersion:Number,speedUnit:String})
const emit=defineEmits(['update:tab','update','action','rename','move','labels','trackers','magnet'])
const allTabs=[{value:'overview',label:'概览'},{value:'files',label:'文件'},{value:'pieces',label:'分片'},{value:'trackers',label:'Tracker'},{value:'peers',label:'连接'},{value:'options',label:'任务设置'}]
const preferences=inject(downloadPreferences),layer=inject(layerContext,null),menuLayer=computed(()=>(layer?.value||60)+30),contextTab=ref('overview')
const tabs=computed(()=>preferences.value.detailTabs.filter(key=>!preferences.value.hiddenDetailTabs.includes(key)).map(key=>allTabs.find(tab=>tab.value===key)).filter(Boolean))
watch(tabs,value=>{if(!value.some(tab=>tab.value===props.tab))emit('update:tab','overview')})
function toggleTab(key){const hidden=preferences.value.hiddenDetailTabs;preferences.value.hiddenDetailTabs=hidden.includes(key)?hidden.filter(k=>k!==key):[...hidden,key]}
function moveTab(direction){const order=[...preferences.value.detailTabs],index=order.indexOf(contextTab.value),next=index+direction;if(next<0||next>=order.length)return;[order[index],order[next]]=[order[next],order[index]];preferences.value.detailTabs=order}
function tabKey(event) {
  const index=tabs.value.findIndex(t=>t.value===props.tab)
  const next=event.key==='ArrowRight' ? (index+1)%tabs.value.length : event.key==='ArrowLeft' ? (index+tabs.value.length-1)%tabs.value.length : event.key==='Home' ? 0 : event.key==='End' ? tabs.value.length-1 : -1
  if(next<0) return
  event.preventDefault();emit('update:tab',tabs.value[next].value)
  document.getElementById(`torrent-tab-${tabs.value[next].value}`)?.focus()
}
</script>

<template>
  <div class="torrent-details" :aria-busy="loading">
    <div class="torrent-detail-status"><span class="task-status" :class="{'status-error':taskError(task)}">{{ taskStatus(task) }}</span><span>{{ percent(task.percentDone) }} · {{ bytes(task.sizeWhenDone) }}</span><span><ArrowDown :size="14"/>{{ speed(task.rateDownload) }}</span><span><ArrowUp :size="14"/>{{ speed(task.rateUpload) }}</span><span class="detail-refresh-marker" :class="{active:refreshing}" :aria-hidden="!refreshing" title="正在更新详情"><RefreshCw :size="13" :class="{'is-spinning':refreshing}"/></span></div>
    <p v-if="taskError(task)" class="download-error" role="alert">{{ taskError(task) }}</p>
    <ContextMenuRoot><ContextMenuTrigger as-child><div class="detail-tabs" role="tablist" aria-label="任务详情分类" @keydown="tabKey"><button v-for="item in tabs" :id="`torrent-tab-${item.value}`" :key="item.value" :aria-controls="`torrent-panel-${item.value}`" role="tab" :aria-selected="tab===item.value" :tabindex="tab===item.value ? 0 : -1" @contextmenu="contextTab=item.value" @click="emit('update:tab',item.value)">{{ item.label }}<span v-if="item.value==='files' && task.files">{{ task.files.length }}</span><span v-if="item.value==='peers'">{{ task.peersConnected || 0 }}</span></button></div></ContextMenuTrigger><ContextMenuPortal><ContextMenuContent class="download-context-menu" :style="{zIndex:menuLayer}"><div class="download-context-heading">详情标签</div><ContextMenuItem v-for="item in allTabs" :key="item.value" class="download-context-item" :disabled="item.value==='overview'" @select.prevent="toggleTab(item.value)">{{ preferences.hiddenDetailTabs.includes(item.value)?'　':'✓' }} {{ item.label }}</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" @select.prevent="moveTab(-1)">当前标签左移</ContextMenuItem><ContextMenuItem class="download-context-item" @select.prevent="moveTab(1)">当前标签右移</ContextMenuItem><ContextMenuItem class="download-context-item" @select="preferences.detailTabs=allTabs.map(t=>t.value);preferences.hiddenDetailTabs=[]">重置详情标签</ContextMenuItem></ContextMenuContent></ContextMenuPortal></ContextMenuRoot>
    <div v-if="loading" class="detail-loading">正在读取任务详情…</div>
    <section :id="`torrent-panel-${tab}`" role="tabpanel" :aria-labelledby="`torrent-tab-${tab}`" class="torrent-panel">
      <template v-if="tab==='overview'">
        <dl class="torrent-facts">
          <div><dt>保存位置</dt><dd>{{ task.downloadDir }}</dd></div><div><dt>已下载</dt><dd>{{ bytes(task.downloadedEver) }}</dd></div><div><dt>已上传</dt><dd>{{ bytes(task.uploadedEver) }}</dd></div><div><dt>剩余大小</dt><dd>{{ bytes(task.leftUntilDone) }}</dd></div><div><dt>分享率</dt><dd>{{ ratio(task.uploadRatio) }}</dd></div><div><dt>剩余时间</dt><dd>{{ duration(task.eta) }}</dd></div><div><dt>添加时间</dt><dd>{{ dateTime(task.addedDate) }}</dd></div><div><dt>完成时间</dt><dd>{{ dateTime(task.doneDate) }}</dd></div><div><dt>最近活动</dt><dd>{{ dateTime(task.activityDate) }}</dd></div><div><dt>连接</dt><dd>{{ task.peersConnected || 0 }} 个节点 · {{ task.peersSendingToUs || 0 }} 下载 / {{ task.peersGettingFromUs || 0 }} 上传</dd></div><div><dt>有效 / 未校验数据</dt><dd>{{ bytes(task.haveValid) }} / {{ bytes(task.haveUnchecked) }}</dd></div><div><dt>损坏数据</dt><dd>{{ bytes(task.corruptEver) }}</dd></div><div><dt>可从节点获取</dt><dd>{{ bytes(task.desiredAvailable) }}</dd></div><div><dt>下载 / 做种时长</dt><dd>{{ duration(task.secondsDownloading) }} / {{ duration(task.secondsSeeding) }}</dd></div><div><dt>平均下载 / 上传速度</dt><dd>{{ speed(task.secondsDownloading ? task.downloadedEver/task.secondsDownloading : 0) }} / {{ speed(task.secondsSeeding ? task.uploadedEver/task.secondsSeeding : 0) }}</dd></div><div><dt>种子类型</dt><dd>{{ task.isPrivate ? '私有种子' : '公开种子' }} · 元数据 {{ percent(task.metadataPercentComplete) }}</dd></div><div><dt>带宽组 / 最大连接</dt><dd>{{ task.group || '默认' }} / {{ task['peer-limit'] ?? '—' }}</dd></div><div><dt>分片</dt><dd>{{ task.pieceCount ?? '—' }} × {{ bytes(task.pieceSize) }}</dd></div><div><dt>队列位置</dt><dd>{{ (task.queuePosition ?? 0)+1 }}</dd></div><div class="fact-wide"><dt>种子哈希</dt><dd class="selectable-hash">{{ task.hashString }}</dd></div><div v-if="task.labels?.length"><dt>标签</dt><dd>{{ task.labels.join('、') }}</dd></div><div v-if="task.creator"><dt>创建者</dt><dd>{{ task.creator }}</dd></div><div v-if="task.comment" class="fact-wide"><dt>备注</dt><dd>{{ task.comment }}</dd></div>
        </dl>
        <div class="detail-task-actions"><AppButton :disabled="busy" @click="emit('rename')">重命名</AppButton><AppButton :disabled="busy" @click="emit('move')">移动 / 更改位置</AppButton><AppButton :disabled="busy" @click="emit('labels')">编辑标签</AppButton><AppButton @click="emit('magnet')">复制磁力链接</AppButton><AppButton :disabled="busy" @click="emit('action',task.status===0 ? 'torrent-start' : 'torrent-stop')"><Play v-if="task.status===0" :size="16"/><Pause v-else :size="16"/>{{ task.status===0 ? '继续' : '暂停' }}</AppButton><AppButton :disabled="busy" @click="emit('action','torrent-verify')"><CheckCheck :size="16"/>重新校验</AppButton><AppButton :disabled="busy" @click="emit('action','torrent-reannounce')"><Radio :size="16"/>重新通告 Tracker</AppButton></div>
      </template>
      <TorrentFiles v-if="tab==='files'" :task="task" :busy="busy" :loading="loading" @update="(...args)=>emit('update',...args)" @rename="(...args)=>emit('rename',...args)"/>
      <TorrentPieces v-if="tab==='pieces'" :task="task"/>
      <template v-if="tab==='trackers'">
        <DataRows name="trackers" :columns="trackerColumns" :rows="task.trackerStats||[]" :row-key="row=>row.id"/>
        <AppButton :disabled="busy" @click="emit('action','torrent-reannounce')">重新通告</AppButton><AppButton :disabled="busy" @click="emit('trackers')">编辑 Tracker</AppButton>
      </template>
      <template v-if="tab==='peers'">
        <DataRows name="peers" :columns="peerColumns" :rows="task.peers||[]" :row-key="row=>`${row.address}:${row.port}`"/>
      </template>
      <TorrentProperties v-if="tab==='options'" :task="task" :fields="fields" :busy="busy" :rpc-version="rpcVersion" :speed-unit="speedUnit" @update="(...args)=>emit('update',...args)"/>
    </section>
  </div>
</template>
