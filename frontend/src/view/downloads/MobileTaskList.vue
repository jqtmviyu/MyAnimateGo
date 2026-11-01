<script setup>
import {ArrowDown, ArrowUp, MoreHorizontal, Pause, Play} from '@lucide/vue'
import {bytes, duration, percent, speed, taskError, taskStatus} from './transmission'

defineProps({tasks: {type: Array, required: true}, selected: {type: Set, required: true}, busy: Boolean, connected: Boolean})
const emit = defineEmits(['select', 'details', 'command'])
const commands = [
  {key: 'details', label: '查看详情'},
  {key: 'rename', label: '重命名'},
  {key: 'move', label: '移动 / 更改位置'},
  {key: 'labels', label: '编辑标签'},
  {key: 'properties', label: '任务属性'},
  {key: 'trackers', label: '编辑 Tracker'},
  {key: 'magnet', label: '复制磁力链接'},
  {key: 'torrent-start-now', label: '强制启动', divided: true},
  {key: 'torrent-verify', label: '重新校验'},
  {key: 'torrent-reannounce', label: '重新通告 Tracker'},
  {key: 'queue-move-top', label: '移到队首', divided: true},
  {key: 'queue-move-up', label: '上移'},
  {key: 'queue-move-down', label: '下移'},
  {key: 'queue-move-bottom', label: '移到队尾'},
  {key: 'remove', label: '删除任务…', divided: true},
]
const progress = task => Math.min(1, Math.max(0, (task.status === 2 ? task.recheckProgress : task.percentDone) || 0))
</script>

<template>
  <ul class="mobile-task-list" aria-label="下载任务">
    <li v-for="task in tasks" :key="task.id" class="mobile-task" :class="{'is-selected': selected.has(task.id)}">
      <div class="mobile-task-heading">
        <label class="mobile-task-check">
          <input type="checkbox" :checked="selected.has(task.id)" :aria-label="`选择 ${task.name}`" @change="emit('select', task.id, $event.target.checked)">
        </label>
        <button class="mobile-task-name" :aria-label="`查看 ${task.name} 的详情`" @click="emit('details', task)">{{ task.name }}</button>
        <AppDropdown class="download-touch-menu" @command="emit('command', $event, task)">
          <button class="mobile-task-icon" :aria-label="`${task.name} 的操作菜单`"><MoreHorizontal :size="21"/></button>
          <template #dropdown><AppDropdownMenu>
            <AppDropdownItem v-for="item in commands" :key="item.key" :command="item.key" :divided="item.divided" :variant="item.key==='remove'?'destructive':'default'" :disabled="!['details','magnet'].includes(item.key) && (busy || !connected)">{{ item.label }}</AppDropdownItem>
          </AppDropdownMenu></template>
        </AppDropdown>
      </div>
      <div class="mobile-task-content">
        <div class="mobile-task-state"><span :class="{'text-destructive': taskError(task), 'text-primary': !taskError(task) && [4,6].includes(task.status)}">{{ taskStatus(task) }}</span><span>{{ percent(progress(task)) }} · {{ bytes(task.sizeWhenDone) }}</span></div>
        <div class="task-progress" role="progressbar" :aria-label="`${task.name} 的进度`" :aria-valuenow="Math.round(progress(task) * 100)" aria-valuemin="0" aria-valuemax="100"><i :style="{width: `${progress(task) * 100}%`}"/></div>
        <div class="mobile-task-transfer">
          <div class="mobile-task-speeds"><span><ArrowDown :size="14"/>{{ speed(task.rateDownload) }}</span><span><ArrowUp :size="14"/>{{ speed(task.rateUpload) }}</span><span v-if="task.status === 4 && task.eta >= 0" class="mobile-task-eta">剩余 {{ duration(task.eta) }}</span></div>
          <button class="mobile-task-icon" :disabled="busy || !connected" :aria-label="`${task.status === 0 ? '继续' : '暂停'} ${task.name}`" @click="emit('command', task.status === 0 ? 'torrent-start' : 'torrent-stop', task)"><Play v-if="task.status === 0" :size="19"/><Pause v-else :size="19"/></button>
        </div>
        <p v-if="taskError(task)" class="mobile-task-error">{{ taskError(task) }}</p>
        <p v-if="task.labels?.length" class="mobile-task-labels" :title="task.labels.join('、')">{{ task.labels.join(' · ') }}</p>
      </div>
    </li>
  </ul>
</template>

<style scoped>
@reference "../../css/theme.css";
.mobile-task-list {@apply m-0 list-none p-0;}
.mobile-task {@apply min-w-0 border-b border-border py-3;}
.mobile-task.is-selected {background:color-mix(in srgb,var(--primary) 9%,transparent);}
.mobile-task-heading {display:grid;grid-template-columns:40px minmax(0,1fr) 44px;align-items:start;gap:4px;}
.mobile-task-check,.mobile-task-icon {@apply inline-flex shrink-0 items-center justify-center rounded-full text-muted-foreground;min-height:44px;min-width:44px;}
.mobile-task-check {min-width:40px;cursor:pointer;}
.mobile-task-icon:hover {@apply bg-surface-hover text-foreground;}
.mobile-task-icon:disabled {@apply opacity-40;}
.mobile-task-name {@apply min-w-0 text-left text-sm font-medium leading-relaxed text-foreground;min-height:44px;padding:8px 0;overflow-wrap:anywhere;}
.mobile-task-content {padding:0 4px 0 44px;}
.mobile-task-state {@apply flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground;}
.mobile-task-content>.task-progress {height:4px;margin-top:9px;}
.mobile-task-transfer {@apply flex items-center justify-between gap-2;}
.mobile-task-speeds {@apply flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 py-2 text-xs text-muted-foreground; font-variant-numeric:tabular-nums;}
.mobile-task-speeds>span {@apply inline-flex items-center gap-1;}
.mobile-task-eta {flex-basis:100%;}
.mobile-task-error {@apply mb-2 text-xs leading-relaxed text-destructive;overflow-wrap:anywhere;}
.mobile-task-labels {@apply mb-1 truncate text-xs text-muted-foreground;}
</style>
