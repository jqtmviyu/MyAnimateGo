import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {computed, nextTick, ref} from 'vue'
import {formatTime} from '../src/js/format.js'

const source = readFileSync(new URL('../src/view/home/LogsView.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
const entries = [
  {ts:1700000000000,level:'INFO',loggerName:'ani.pipeline',threadName:'worker-1',message:'RSS 更新完成'},
  {ts:1700000001000,level:'ERROR',loggerName:'ani.pipeline',threadName:'worker-2',message:'连接失败\n  at download'},
  {ts:1700000002000,level:'WARN',loggerName:'ani.metadata',threadName:'worker-3',message:'封面等待重试'},
]
const create = new Function('computed','nextTick','onActivated','ref','formatTime','http',source+'\nreturn {logs, searchText, selectLevels, selectLoggerNames, filteredLogs, getLogs, scrollbarRef, innerRef}')
const view = create(computed,nextTick,()=>{},ref,formatTime,{logs:async()=>({data:entries})})
let scroll
view.scrollbarRef.value = {scrollTo:options=>{scroll=options}}
view.innerRef.value = {scrollHeight:900}
await view.getLogs()
assert.deepEqual(scroll,{top:900})
assert.equal(view.filteredLogs.value.length,3)
view.searchText.value='WORKER-2'
assert.equal(view.filteredLogs.value[0].message,'连接失败\n  at download')
view.searchText.value=''
view.selectLevels.value=['INFO','ERROR']
view.selectLoggerNames.value=['ani.pipeline']
assert.equal(view.filteredLogs.value.length,2)
view.searchText.value='download'
assert.equal(view.filteredLogs.value.length,1)
view.selectLoggerNames.value=['ani.metadata']
assert.equal(view.filteredLogs.value.length,0)
view.selectLoggerNames.value=['missing-source','ani.pipeline']
await view.getLogs()
assert.deepEqual(view.selectLoggerNames.value,['ani.pipeline'])
console.log('日志内容搜索、多选筛选、刷新与滚动通过')
