<script setup>
import {useMediaQuery} from '@vueuse/core'
import {computed, ref, watch} from 'vue'
import {rpc, bytes, ratio, duration, downloadColumns,normalizePreferences} from './transmission'
import RpcFieldForm from './RpcFieldForm.vue'
import {notify} from '@/lib/feedback'
const props=defineProps({modelValue:Boolean,preferences:{type:Object,required:true},initialSection:{type:String,default:'下载'}})
const isMobile=useMediaQuery('(max-width: 780px)')
const interfaceFields=computed(()=>[{key:'listInterval',label:'任务增量刷新间隔',kind:'int',unit:'秒'},{key:'detailInterval',label:'详情刷新间隔',kind:'int',unit:'秒'},{key:'sessionInterval',label:'连接状态刷新间隔',kind:'int',unit:'秒'},{key:'pageSize',label:'每页任务数',kind:'int'},...(isMobile.value?[]:[{key:'detailsDocked',label:'在下方固定显示任务详情',kind:'bool'}])])
const emit=defineEmits(['update:modelValue','saved','preferences'])
const content=ref(null)
const loading=ref(false),busy=ref(false),failure=ref(''),section=ref('下载'),baseline=ref({}),draft=ref({}),fields=ref([]),stats=ref({}),result=ref('')
const prefs=ref({}),directoryText=ref(''),groups=ref([]),groupDraft=ref(null)
const sections=['下载','网络','带宽','队列','带宽组','状态统计','界面']
const speedUnit=computed(()=>draft.value.units?.['speed-units']?.[0] || 'kB/s')
const supported=computed(()=>fields.value.filter(f=>!['blocklist-enabled','blocklist-url'].includes(f.key)&&draft.value[f.key]!==undefined && (!f.minRPC || draft.value['rpc-version']>=f.minRPC)))
const sectionFields=computed(()=>supported.value.filter(f=>f.group===section.value))
const changes=computed(()=>Object.fromEntries(supported.value.filter(f=>JSON.stringify(draft.value[f.key])!==JSON.stringify(baseline.value[f.key])).map(f=>[f.key,draft.value[f.key]])))
const groupFields=[{key:'name',label:'带宽组名称',kind:'string'},{key:'honorsSessionLimits',label:'遵循全局速度限制',kind:'bool'},...['down','up'].flatMap(dir=>[{key:`speed-limit-${dir}-enabled`,label:`限制${dir==='down'?'下载':'上传'}速度`,kind:'bool'},{key:`speed-limit-${dir}`,label:`${dir==='down'?'下载':'上传'}速度上限`,kind:'int',unit:'kB/s'}])]
watch(()=>props.modelValue,async open=>{
 if(!open)return
 section.value=props.initialSection;result.value='';failure.value='';loading.value=true;groupDraft.value=null
 prefs.value=normalizePreferences(props.preferences);directoryText.value=prefs.value.directories.join('\n')
 try {const [data,totals]=await Promise.all([rpc('session-get',{}, {view:'settings'}),rpc('session-stats')]);fields.value=data._fields || [];delete data._fields;baseline.value=structuredClone(data);draft.value=data;stats.value=totals} catch(e){failure.value=e.message} finally{loading.value=false}
})
watch(section,()=>{if(content.value)content.value.scrollTop=0},{flush:'post'})
watch(section,async value=>{if(value==='带宽组' && draft.value['rpc-version']>=17){try{groups.value=(await rpc('group-get')).group || []}catch{groups.value=[]}}})
async function save() {
 if(busy.value)return
 busy.value=true
 try {
  if(Object.keys(changes.value).length) await rpc('session-set',changes.value)
  const preferences=normalizePreferences({...prefs.value,directories:directoryText.value.split('\n').map(s=>s.trim()).filter(Boolean)})
  emit('preferences',preferences);emit('saved');emit('update:modelValue',false);notify.success('Transmission 设置已保存')
 }catch{}finally{busy.value=false}
}
async function diagnostic(method,args={}) {
 if(busy.value)return
 busy.value=true;result.value=''
 try {const data=await rpc(method,args);result.value=data['port-is-open'] ? '当前监听端口可从外网访问' : '当前监听端口未开放'}catch{}finally{busy.value=false}
}
async function saveGroup() {
 if(!groupDraft.value?.name?.trim() || busy.value)return
 busy.value=true
 try {await rpc('group-set',groupDraft.value);groups.value=(await rpc('group-get')).group || [];groupDraft.value=null;notify.success('带宽组已保存')}catch{}finally{busy.value=false}
}
function setColumn(key,checked){prefs.value.columns=checked?[...prefs.value.columns,key]:prefs.value.columns.filter(k=>k!==key);if(prefs.value.tables.tasks)prefs.value.tables.tasks={...prefs.value.tables.tasks,visible:prefs.value.columns}}
function exportPrefs() {const url=URL.createObjectURL(new Blob([JSON.stringify(normalizePreferences({...prefs.value,directories:directoryText.value.split('\n').filter(Boolean)}),null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='anirss-transmission-ui.json';a.click();URL.revokeObjectURL(url)}
async function importPrefs(event) {try{const file=event.target.files?.[0];if(!file)return;if(file.size>100000)throw Error('界面配置文件过大');prefs.value=normalizePreferences(JSON.parse(await file.text()));directoryText.value=prefs.value.directories.join('\n');notify.success('界面配置已导入，保存后生效')}catch(e){notify.error(e.message)}finally{event.target.value=''}}
</script>
<template>
 <AppModal :model-value="modelValue" title="Transmission 设置" width="1000px" class="transmission-settings-dialog" @update:model-value="emit('update:modelValue',$event)">
  <p v-if="loading" class="download-help">正在读取下载器设置…</p><p v-else-if="failure" class="download-error">{{ failure }}</p>
  <div v-else class="transmission-settings-layout">
   <nav aria-label="Transmission 设置分类"><button v-for="name in sections" :key="name" :aria-current="section===name ? 'page':undefined" @click="section=name">{{ name }}</button></nav>
   <section ref="content" class="transmission-settings-content" tabindex="0" aria-label="当前设置分类内容">
    <template v-if="['下载','网络','带宽','队列'].includes(section)"><h3>{{ section }}</h3><p class="download-help">这里修改的是 Transmission 下载器设置，作用于它的全部任务。</p><RpcFieldForm v-model="draft" :fields="sectionFields" :disabled="busy" :speed-unit="speedUnit"/>
     <template v-if="section==='网络'"><div class="detail-task-actions"><AppButton :disabled="busy" @click="diagnostic('port-test')">检测当前端口</AppButton></div><p v-if="result" role="status">{{ result }}</p></template>
    </template>
    <template v-else-if="section==='带宽组'"><h3>带宽组</h3><p v-if="draft['rpc-version']<17" class="download-help">当前下载器版本不支持带宽组 RPC。</p><template v-else><p class="download-help">任务可以加入带宽组，单独共享一组上传和下载速度限制。</p><div class="bandwidth-group-list"><button v-for="group in groups" :key="group.name" @click="groupDraft={...group}"><span>{{ group.name }}</span><span>↓ {{ group['speed-limit-down-enabled'] ? group['speed-limit-down']+' '+speedUnit : '无限制' }} / ↑ {{ group['speed-limit-up-enabled'] ? group['speed-limit-up']+' '+speedUnit : '无限制' }}</span></button></div><AppButton @click="groupDraft={name:'',honorsSessionLimits:true,'speed-limit-down-enabled':false,'speed-limit-down':100,'speed-limit-up-enabled':false,'speed-limit-up':100}">新建带宽组</AppButton><template v-if="groupDraft"><RpcFieldForm v-model="groupDraft" :fields="groupFields" :disabled="busy" :speed-unit="speedUnit"/><AppButton type="primary" :disabled="busy || !groupDraft.name?.trim()" @click="saveGroup">保存带宽组</AppButton></template></template></template>
    <template v-else-if="section==='状态统计'"><h3>下载器状态</h3><dl class="torrent-facts"><div><dt>版本</dt><dd>{{ draft.version }}</dd></div><div><dt>任务数</dt><dd>{{ stats.torrentCount }} · 活跃 {{ stats.activeTorrentCount }} · 暂停 {{ stats.pausedTorrentCount }}</dd></div></dl><section v-for="[key,title] in [['current-stats','当前会话'],['cumulative-stats','累计统计']]" :key="key"><h4>{{ title }}</h4><dl class="torrent-facts"><div><dt>已下载</dt><dd>{{ bytes(stats[key]?.downloadedBytes) }}</dd></div><div><dt>已上传</dt><dd>{{ bytes(stats[key]?.uploadedBytes) }}</dd></div><div><dt>分享率</dt><dd>{{ ratio(stats[key]?.downloadedBytes ? stats[key]?.uploadedBytes/stats[key]?.downloadedBytes : -1) }}</dd></div><div><dt>运行时间</dt><dd>{{ duration(stats[key]?.secondsActive) }}</dd></div><div><dt>已添加文件</dt><dd>{{ stats[key]?.filesAdded }}</dd></div><div><dt>会话数</dt><dd>{{ stats[key]?.sessionCount }}</dd></div></dl></section></template>
    <template v-else><h3>界面与刷新</h3><p class="download-help">保存在当前浏览器，隐藏页面时停止轮询。</p><RpcFieldForm v-model="prefs" :fields="interfaceFields"/><p class="download-help">{{ isMobile?'手机使用弹窗查看详情，桌面的详情显示方式单独保留。':'详情入口使用这里设置的显示方式。打开后跟随当前任务切换；收起后选择任务不会自动展开。' }}</p>
    <template v-if="!isMobile"><h4>任务列表列</h4><div class="column-choices"><AppCheckbox v-for="column in downloadColumns" :key="column.key" :model-value="prefs.columns.includes(column.key)" :disabled="column.key==='name'" @update:model-value="checked=>setColumn(column.key,checked)">{{ column.label }}</AppCheckbox></div></template>
    <h4>常用保存目录</h4><AppInput v-model="directoryText" type="textarea" :rows="4" aria-label="常用保存目录，一行一个" placeholder="一行一个远程下载目录"/>
    <h4>手动添加任务时追加的默认 Tracker</h4><AppInput v-model="prefs.defaultTrackers" type="textarea" :rows="4" aria-label="默认 Tracker，每行一个，空行分组"/>
    <div class="detail-task-actions"><AppButton @click="exportPrefs">导出界面配置</AppButton><label class="import-preferences">导入界面配置<input type="file" accept="application/json,.json" @change="importPrefs"></label></div>
    </template>
   </section>
  </div>
  <template #footer><span v-if="Object.keys(changes).length" class="download-help">{{ Object.keys(changes).length }} 项下载器设置待保存</span><AppButton :disabled="busy" @click="emit('update:modelValue',false)">取消</AppButton><AppButton type="primary" :loading="busy" :disabled="loading || Boolean(failure)" @click="save">保存设置</AppButton></template>
 </AppModal>
</template>
