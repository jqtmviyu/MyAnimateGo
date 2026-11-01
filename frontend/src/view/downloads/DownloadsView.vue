<script setup>
import {useMediaQuery} from '@vueuse/core'
import {computed,nextTick,provide,onActivated,onDeactivated,onMounted,onUnmounted,ref,shallowRef,watch} from 'vue'
import {ArrowDown,ArrowUp,CheckCheck,ChevronDown,ChevronLeft,ChevronRight,FilePlus2,FolderInput,Info,ListFilter,PanelBottom,Pause,PenLine,Play,RefreshCw,Search,Settings2,Tag,Trash2} from '@lucide/vue'
import LibraryToolbar from '@/view/home/LibraryToolbar.vue'
import TableHeader from './TableHeader.vue'
import {useTableLayout,downloadPreferences} from './table-layout'
import {selectRows} from './selection'
import {mergeDetailTask,sameDetailValue} from './detail-state'
import DownloadSidebar from './DownloadSidebar.vue'
import MobileTaskList from './MobileTaskList.vue'
import TorrentDetails from './TorrentDetails.vue'
import TorrentFiles from './TorrentFiles.vue'
import BatchRename from './BatchRename.vue'
import {renameTarget} from './rename-target'
import TorrentProperties from './TorrentProperties.vue'
import TransmissionSettings from './TransmissionSettings.vue'
import {createPollingChannel} from './polling'
import {taskIdentity,shortcutAllowed} from './task-state'
import {rpc as transmissionRPC,bytes,speed,percent,taskStatus,taskError,readPreferences,normalizePreferences,downloadColumns} from './transmission'
import {notify} from '@/lib/feedback'
import {ContextMenuRoot,ContextMenuTrigger,ContextMenuPortal,ContextMenuContent,ContextMenuItem,ContextMenuSeparator} from 'reka-ui'
import {dialogs} from '@/lib/dialogs'

const props=defineProps({demo:Boolean,mockTransport:Object})
const mock=props.demo?props.mockTransport:null
const rpc=(...args)=>props.demo?mock.rpc(...args):transmissionRPC(...args)
function resetMock(){mock.reset();detailOpen.value=false;selected.value=[];refresh(true)}

const tasks=shallowRef([]),stats=shallowRef({}),session=shallowRef({}),preferences=ref(props.demo?normalizePreferences({}):readPreferences())
const isMobile=useMediaQuery('(max-width: 780px)')
const filtersOpen=ref(false),taskScroll=ref(null)
const search=ref(''),filter=ref('all'),label=ref(''),directory=ref(''),page=ref(1)
provide(downloadPreferences,preferences)
const tasksTable=useTableLayout('tasks',downloadColumns,preferences)
const sortKey=computed(()=>tasksTable.layout.value.sort.key)
const descending=computed(()=>tasksTable.layout.value.sort.descending)
const contextIDs=ref([])
const selected=ref([]),selectionAnchor=ref(null),focusedTask=ref(null)
const detailError=ref(''),statsError=ref(''),sessionError=ref('')
const loading=ref(false),busy=ref(false),connected=ref(false),error=ref(''),lastUpdated=ref(null)
const detailViews=ref(new Set())
const detail=shallowRef(null),detailOpen=ref(false),detailTab=ref('overview'),detailLoading=ref(false),detailFields=ref([])
const detailPresentation=computed(()=>!detailOpen.value||!detail.value?'closed':!isMobile.value&&preferences.value.detailsDocked?'docked':'dialog')
const batchRenameOpen=ref(false),batchRenameTask=shallowRef(null),batchRenameDirectory=ref('')
const settingsOpen=ref(false),settingsSection=ref('下载')
const detailInitialLoading=computed(()=>detailLoading.value&&!detailViews.value.has(detailTab.value))
const detailRefreshing=computed(()=>detailLoading.value&&detailViews.value.has(detailTab.value))
const addOpen=ref(false),addURL=ref(''),addFiles=ref([]),addPaused=ref(false),addDirectory=ref(''),addLabels=ref([]),addPriority=ref(0),addSequential=ref(false),fileInput=ref(null),freeSpace=ref(null)
const reviewBeforeStart=ref(false),reviewOpen=ref(false),reviewIDs=ref([]),reviewIndex=ref(0),reviewTask=shallowRef(null),reviewLoading=ref(false)
const removeOpen=ref(false),removeIDs=ref([]),deleteFiles=ref(false)
const editor=ref(null),editIDs=ref([]),editorPath=ref(''),editorName=ref(''),editorLabels=ref([]),labelMode=ref('replace'),moveData=ref(true),trackerText=ref(''),propertyTask=ref(null),propertyFields=ref([])
let active=false,fullAt=0,forceFull=true,filesAt=0,filesIdentity='',invalidatedFilesIdentity='',preferenceTimer
const filters=[
 {value:'all',label:'全部任务',match:()=>true},{value:'downloading',label:'下载中',match:t=>t.status===4},{value:'complete',label:'已完成',match:t=>t.percentDone>=1},
 {value:'seeding',label:'做种中',match:t=>t.status===6},{value:'active',label:'正在传输',match:t=>t.rateDownload>0||t.rateUpload>0},
 {value:'inactive',label:'无传输',match:t=>t.rateDownload===0&&t.rateUpload===0&&t.status!==0},{value:'running',label:'运行中',match:t=>t.status!==0},
 {value:'paused',label:'已暂停',match:t=>t.status===0},{value:'error',label:'异常',match:t=>Boolean(taskError(t))},
 {value:'waiting',label:'等待中',match:t=>[1,3,5].includes(t.status)},{value:'checking',label:'校验中',match:t=>t.status===2},
 {value:'metadata',label:'获取元数据',match:t=>t.status===4&&t.metadataPercentComplete<1},
]
const activeFilterCount=computed(()=>Number(filter.value!=='all')+Number(Boolean(label.value))+Number(Boolean(directory.value)))
function clearFilters(){filter.value='all';label.value='';directory.value=''}
function mobileFilter(kind,value){if(kind==='status')filter.value=value;else if(kind==='label')label.value=value;else directory.value=value;filtersOpen.value=false}
const labels=computed(()=>[...new Set(tasks.value.flatMap(t=>t.labels||[]))].sort())
const directories=computed(()=>[...new Set([...preferences.value.directories,...tasks.value.map(t=>t.downloadDir).filter(Boolean)])].sort())
const normalizedPath=path=>(path||'').replace(/\\/g,'/').replace(/\/+$/,'')
const visible=computed(()=>{
 const match=filters.find(f=>f.value===filter.value)?.match||(()=>true),terms=search.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
 const column=downloadColumns.find(c=>c.key===sortKey.value),value=task=>{const raw=task[sortKey.value];return Array.isArray(raw)||raw===undefined?column?.format?.(raw,task)??raw:raw}
 return tasks.value.filter(t=>match(t)&&(!label.value||(label.value==='\0'?!t.labels?.length:t.labels?.includes(label.value)))&&(!directory.value||normalizedPath(t.downloadDir)===directory.value||preferences.value.recursiveDirectories&&normalizedPath(t.downloadDir).startsWith(directory.value+'/'))&&terms.every(term=>`${t.name} ${t.hashString} ${t.downloadDir} ${(t.labels||[]).join(' ')}`.toLowerCase().includes(term)))
 .sort((a,b)=>{const x=value(a),y=value(b);return (descending.value?-1:1)*(typeof x==='string'?x.localeCompare(String(y??''),'zh-CN'):(Number(x)||0)-(Number(y)||0))||a.id-b.id})
})
const pageCount=computed(()=>Math.max(1,Math.ceil(visible.value.length/preferences.value.pageSize)))
const paged=computed(()=>visible.value.slice((page.value-1)*preferences.value.pageSize,page.value*preferences.value.pageSize))
const columns=tasksTable.columns
const visibleIDs=computed(()=>visible.value.map(t=>t.id)),selectedSet=computed(()=>new Set(selected.value))
const allSelected=computed(()=>paged.value.length>0&&paged.value.every(t=>selectedSet.value.has(t.id)))
watch([search,filter,label,directory,()=>preferences.value.pageSize],()=>{page.value=1})
watch(pageCount,n=>{page.value=Math.min(page.value,n)})
watch([page,search,filter,label,directory,sortKey,descending],async()=>{await nextTick();taskScroll.value?.scrollTo({top:0})})
watch(isMobile,mobile=>{if(!mobile)filtersOpen.value=false})
watch(preferences,()=>{if(props.demo)return;clearTimeout(preferenceTimer);preferenceTimer=setTimeout(()=>localStorage.setItem('transmission-ui',JSON.stringify(preferences.value)),250)},{deep:true})
function savePreferences(value){preferences.value=normalizePreferences(value);refresh(true)}
function toggleAll(checked){const ids=paged.value.map(t=>t.id);selected.value=checked?[...new Set([...selected.value,...ids])]:selected.value.filter(id=>!ids.includes(id))}
function toggleTask(id,checked){selectionAnchor.value=id;selected.value=checked?[...new Set([...selected.value,id])]:selected.value.filter(i=>i!==id)}
function selectTask(event,task,checkbox=false) {
 const next=selectRows({ids:visibleIDs.value,selected:selected.value,anchor:selectionAnchor.value,id:task.id,toggle:checkbox||event.ctrlKey||event.metaKey,range:event.shiftKey})
 selected.value=next.selected;selectionAnchor.value=next.anchor;focusedTask.value=task.id
 if(detailOpen.value&&next.selected.includes(task.id))showDetails(task)
}
function taskContext(event){
 const row=event.target.closest('[data-task-id]'),task=row&&tasks.value.find(task=>task.id===Number(row.dataset.taskId))
 contextIDs.value=task?(selectedSet.value.has(task.id)?[...selected.value]:[task.id]):[]
}
function rowClick(event,task){if(!event.target.closest('button,input,a,[role=combobox],[role=checkbox]'))selectTask(event,task)}
async function rowKey(event,task) {
 if(event.target!==event.currentTarget)return
 if(event.key!=='ArrowDown'&&event.key!=='ArrowUp'&&event.key!==' ')return
 event.preventDefault()
 const index=visibleIDs.value.indexOf(task.id),nextID=event.key===' '?task.id:visibleIDs.value[Math.max(0,Math.min(visibleIDs.value.length-1,index+(event.key==='ArrowDown'?1:-1)))]
 const next=tasks.value.find(t=>t.id===nextID);if(!next)return
 selectTask(event,next,event.key===' ')
 page.value=Math.floor(visibleIDs.value.indexOf(nextID)/preferences.value.pageSize)+1
 await nextTick();document.querySelector(`[data-task-id="${nextID}"]`)?.focus()
}
function showDetails(task){
 if(detailOpen.value&&detail.value?.id===task.id)return
 const tab=detailOpen.value?detailTab.value:'overview'
 detail.value=task;detailViews.value=new Set();detailFields.value=[];detailError.value=''
 filesAt=0;filesIdentity='';invalidatedFilesIdentity='';detailOpen.value=true;detailTab.value=tab
}
function toggleDetails(){
 if(detailOpen.value){detailOpen.value=false;return}
 const task=tasks.value.find(task=>task.id===focusedTask.value)||tasks.value.find(task=>task.id===selected.value[0])||paged.value[0]
 if(task)openDetails(task)
}
function detailDialogChanged(open){if(detailPresentation.value==='dialog'&&!open)detailOpen.value=false}

function applyDetails({id, view, data, freshFiles}) {
 if (!active || !detailOpen.value || detail.value?.id !== id || detailTab.value !== view) return
 if (!data.torrents?.length) {detailOpen.value=false;notify.info('此任务已从 Transmission 移除');return}
 detail.value=mergeDetailTask(detail.value,data.torrents[0])
 if(!detailViews.value.has(view))detailViews.value=new Set([...detailViews.value,view])
 if(data._fields&&!sameDetailValue(detailFields.value,data._fields))detailFields.value=data._fields
 if(freshFiles) {filesAt=Date.now();filesIdentity=taskIdentity(detail.value);invalidatedFilesIdentity=''}
 detailError.value=''
}
const listChannel=createPollingChannel({
 enabled:()=>active&&!document.hidden,
 interval:()=>preferences.value.listInterval*1000,
 changed:state=>{loading.value=state.loading},
 read:async signal=>{
  const full=forceFull||Date.now()-fullAt>60000
  const data=await rpc('torrent-get',full?{}:{ids:'recently-active'},{silent:true,signal,trackers:true,columns:[...preferences.value.columns,sortKey.value]})
  return {full,data}
 },
 apply:({full,data})=>{
  if(full) {tasks.value=data.torrents||[];fullAt=Date.now();forceFull=false}
  else if(data.torrents?.length||data.removed?.length) {
   const removed=new Set(data.removed||[]),map=new Map(tasks.value.filter(t=>!removed.has(t.id)).map(t=>[t.id,t]))
   for(const task of data.torrents||[])map.set(task.id,{...map.get(task.id),...task})
   tasks.value=[...map.values()]
  }
  const ids=new Set(tasks.value.map(t=>t.id));selected.value=selected.value.filter(id=>ids.has(id))
  if(detailOpen.value) {
   const current=tasks.value.find(t=>t.id===detail.value?.id)
   if(!current) detailOpen.value=false
   else if(detail.value.files&&taskIdentity(current)!==filesIdentity&&taskIdentity(current)!==invalidatedFilesIdentity) {
    // 保留旧文件直到新路径返回，同一变化只触发一次重读，避免慢请求被反复取消。
    invalidatedFilesIdentity=taskIdentity(current)
    detail.value=mergeDetailTask(detail.value,{name:current.name,editDate:current.editDate,downloadDir:current.downloadDir,'file-count':current['file-count']})
    filesAt=0
    if(detailTab.value==='files')detailChannel.restart()
   }
  }
  connected.value=true;error.value='';lastUpdated.value=new Date()
 },
 fail:e=>{error.value=e.message;connected.value=false;forceFull=true},
})
const statsChannel=createPollingChannel({
 enabled:()=>active&&!document.hidden,interval:()=>preferences.value.listInterval*1000,
 read:signal=>rpc('session-stats',{}, {silent:true,signal}),
 apply:data=>{stats.value=data;statsError.value=''},fail:e=>{statsError.value=e.message},
})
const sessionChannel=createPollingChannel({
 enabled:()=>active&&!document.hidden,interval:()=>preferences.value.sessionInterval*1000,
 read:signal=>rpc('session-get',{}, {silent:true,signal}),
 apply:data=>{session.value=data;sessionError.value=''},fail:e=>{sessionError.value=e.message},
})
const detailChannel=createPollingChannel({
 enabled:()=>active&&!document.hidden&&detailOpen.value&&detail.value?.id!=null,
 interval:()=>preferences.value.detailInterval*1000,
 changed:state=>{detailLoading.value=state.loading},
 read:async signal=>{
  const id=detail.value.id,view=detailTab.value
  const freshFiles=view==='files'&&(!detail.value.files||Date.now()-filesAt>60000||filesIdentity!==taskIdentity(detail.value))
  const data=await rpc('torrent-get',{ids:[id]},{silent:true,signal,details:true,view,freshFiles})
  return {id,view,data,freshFiles}
 },apply:applyDetails,fail:e=>{detailError.value=e.message},
})
const channels=[listChannel,statsChannel,sessionChannel,detailChannel]
watch([()=>preferences.value.columns.join('|'),sortKey],()=>{forceFull=true;listChannel.restart()})
function refresh(manual=false) {
 if(manual) {forceFull=true;filesAt=0;return Promise.all(channels.map(channel=>channel.restart()))}
 return listChannel.refresh()
}
function start(){active=true;forceFull=true;channels.forEach(channel=>channel.start())}
function stop(){active=false;filtersOpen.value=false;channels.forEach(channel=>channel.stop());detailOpen.value=false;addOpen.value=false;removeOpen.value=false;settingsOpen.value=false;reviewOpen.value=false;batchRenameOpen.value=false;editor.value=null}
function visibility(){if(!active)return;if(document.hidden)channels.forEach(channel=>channel.stop());else{forceFull=true;channels.forEach(channel=>channel.start())}}
function keys(event){
 if(!shortcutAllowed({active,modalOpen:Boolean(editor.value||addOpen.value||settingsOpen.value||removeOpen.value||reviewOpen.value||batchRenameOpen.value),popupDetails:detailPresentation.value==='dialog',target:event.target,document}))return
 if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='a'){event.preventDefault();selected.value=[...visibleIDs.value];return}
 if(event.key==='F3'){event.preventDefault();action('torrent-start')}
 if(event.key==='F4'){event.preventDefault();action('torrent-stop')}
 if(event.key==='F6'&&selected.value.length){event.preventDefault();openEditor('move')}
 if(event.key==='F7'&&selected.value.length){event.preventDefault();openEditor('labels')}
 if(event.key==='F2'&&selected.value.length===1){event.preventDefault();openRenameRoot(selected.value[0])}
 if(event.key==='Delete'&&selected.value.length){event.preventDefault();removeTasks()}
}
function activate(){if(active)return;document.addEventListener('visibilitychange',visibility);document.addEventListener('keydown',keys);start()}
onMounted(activate)
onActivated(activate)
onDeactivated(()=>{document.removeEventListener('visibilitychange',visibility);document.removeEventListener('keydown',keys);stop()})
onUnmounted(()=>{document.removeEventListener('visibilitychange',visibility);document.removeEventListener('keydown',keys);stop()})
function refreshAfterWrite(){forceFull=true;filesAt=0;[listChannel,statsChannel,detailChannel].forEach(channel=>channel.restart())}
async function action(method,ids=selected.value,args={}){
 const snapshot=[...ids]
 if(busy.value||!snapshot.length||!connected.value)return false
 if(props.demo&&method!=='torrent-rename-path'){notify.info('模拟页只演示重命名操作');return false}
 if(method==='torrent-verify'){try{await dialogs.confirm(`重新读取并校验 ${snapshot.length} 个任务的本地文件，校验期间任务可能暂停。`,'重新校验')}catch{return false}}
 if(busy.value)return false
 busy.value=true
 try{for(let i=0;i<snapshot.length;i+=200)await rpc(method,{ids:snapshot.slice(i,i+200),...args});notify.success('操作已提交');await refreshAfterWrite();return true}catch{return false}finally{busy.value=false}
}
function removeTasks(ids=selected.value){removeIDs.value=[...ids];deleteFiles.value=false;removeOpen.value=true}
async function confirmRemove(){if(await action('torrent-remove',removeIDs.value,{'delete-local-data':deleteFiles.value}))removeOpen.value=false}
function taskCommand(command,ids){
 if(command==='details'){const task=tasks.value.find(t=>t.id===ids[0]);if(task)openDetails(task)}
 else if(command==='rename')openRenameRoot(ids[0])
 else if(['move','labels','properties','trackers'].includes(command))openEditor(command,ids)
 else if(command==='magnet')copyMagnets(ids)
 else if(command==='remove')removeTasks(ids)
 else action(command,ids)
}
function openDetails(task){focusedTask.value=task.id;showDetails(task)}
function reloadDetail(){return detailChannel.restart()}
async function updateTask(args,onSuccess){if(detail.value&&await action('torrent-set',[detail.value.id],args)){onSuccess?.();await reloadDetail()}}
watch([()=>detail.value?.id,detailTab],([id])=>{if(detailOpen.value&&id!=null){detailError.value='';reloadDetail()}})
watch(detailOpen,open=>{if(!open){detail.value=null;detailChannel.restart();detailError.value='';filesAt=0;filesIdentity='';invalidatedFilesIdentity='';detailViews.value=new Set()}})
function openSettings(section='下载'){if(props.demo)return;settingsSection.value=section;settingsOpen.value=true}
async function openRenameRoot(id){return openRename(id)}
async function openRename(id,path,name){
 if(busy.value)return
 busy.value=true
 try{
  const data=await rpc('torrent-get',{ids:[id]},{details:true,view:'files',freshFiles:true}),task=data.torrents?.[0]
  if(!task)return
  const target=renameTarget(task,path,name)
  if(!target){notify.info('文件列表尚未就绪，请稍后重试');return}
  if(target.directory){batchRenameTask.value=task;batchRenameDirectory.value=target.path;batchRenameOpen.value=true}
  else{editIDs.value=[id];editorPath.value=target.path;editorName.value=target.name;editor.value='rename'}
 }catch{}finally{busy.value=false}
}
function batchDirectoryRenamed({before,after}){
 const current=batchRenameDirectory.value
 if(current===before||current.startsWith(before+'/'))batchRenameDirectory.value=after+current.slice(before.length)
}
function batchRenameChanged(task){
 if(task){batchRenameTask.value=task;if(detail.value?.id===task.id)detail.value={...detail.value,...task}}
 refreshAfterWrite();if(reviewOpen.value)loadReviewTask()
}
async function openEditor(kind,ids=selected.value){
 if(!ids.length||busy.value)return
 editIDs.value=[...ids];editorPath.value=tasks.value.find(t=>t.id===ids[0])?.downloadDir||'';editorLabels.value=[];labelMode.value='replace';moveData.value=true;freeSpace.value=null
 if(kind==='labels'&&ids.length===1)editorLabels.value=[...(tasks.value.find(t=>t.id===ids[0])?.labels||[])]
 if(kind==='trackers'||kind==='properties'){
  busy.value=true
  try{const data=await rpc('torrent-get',{ids:[ids[0]]},{details:true,view:kind==='trackers'?'trackers':'options'});const task=data.torrents?.[0];if(!task)return;if(kind==='trackers')trackerText.value=task.trackerList??(task.trackerStats||[]).map(t=>t.announce).join('\n');else{propertyTask.value=task;propertyFields.value=data._fields||[]}}catch{return}finally{busy.value=false}
 }
 editor.value=kind
}
async function applyEditor(){
 if(editor.value==='rename'){
  if(await action('torrent-rename-path',editIDs.value,{path:editorPath.value,name:editorName.value})){if(detail.value?.id===editIDs.value[0]){filesAt=0;reloadDetail()}editor.value=null;if(reviewOpen.value)await loadReviewTask()}
 }else if(editor.value==='move'){
  if(await action('torrent-set-location',editIDs.value,{location:editorPath.value,move:moveData.value})){rememberDirectory(editorPath.value);editor.value=null}
 }else if(editor.value==='trackers'){
  if(await action('torrent-set',editIDs.value,{trackerList:trackerText.value}))editor.value=null
 }else if(editor.value==='labels'){
  if(busy.value)return
  const byLabels=new Map()
  for(const task of tasks.value.filter(t=>editIDs.value.includes(t.id))){const next=labelMode.value==='replace'?editorLabels.value:labelMode.value==='add'?[...new Set([...(task.labels||[]),...editorLabels.value])]:(task.labels||[]).filter(l=>!editorLabels.value.includes(l));const key=JSON.stringify(next);if(!byLabels.has(key))byLabels.set(key,[]);byLabels.get(key).push(task.id)}
  busy.value=true
  try{for(const [labels,ids] of byLabels){for(let i=0;i<ids.length;i+=200)await rpc('torrent-set',{ids:ids.slice(i,i+200),labels:JSON.parse(labels)})}editor.value=null;notify.success('任务标签已保存')}catch{}finally{busy.value=false;refreshAfterWrite()}
 }
}
async function saveProperties(args,done){if(await action('torrent-set',editIDs.value,args)){done?.();editor.value=null}}
function rememberDirectory(path){if(path&&!preferences.value.directories.includes(path))preferences.value.directories=[path,...preferences.value.directories].slice(0,30)}
async function checkSpace(path){if(!path)return;try{freeSpace.value=await rpc('free-space',{path})}catch{}}
async function copyMagnets(ids=selected.value){
 try{const snapshot=[...ids],magnets=[];for(let i=0;i<snapshot.length;i+=200){const data=await rpc('torrent-get',{ids:snapshot.slice(i,i+200)},{view:'magnets'});magnets.push(...(data.torrents||[]).map(t=>t.magnetLink||`magnet:?xt=urn:btih:${t.hashString}&dn=${encodeURIComponent(t.name)}`))}await navigator.clipboard.writeText(magnets.join('\n'));notify.success('磁力链接已复制')}catch{notify.error('复制失败，请检查连接和浏览器剪贴板权限')}
}
function chooseFiles(event){addFiles.value=[...(event.target.files||[])]}
async function readTorrent(file){if(file.size>2*1024*1024)throw Error(`${file.name} 超过 2 MiB`);return new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=()=>reject(Error(`无法读取 ${file.name}`));reader.readAsDataURL(file)})}
async function addTorrent(){
 if(busy.value)return
 const urls=addURL.value.split('\n').map(v=>v.trim()).filter(Boolean);if(!urls.length&&!addFiles.value.length)return
 busy.value=true;let count=0;const created=[],trackerFailures=[]
 try{
  const entries=[...addFiles.value.map(file=>({file})),...urls.map(url=>({url}))]
  for(const entry of entries){
   const args={paused:addPaused.value||reviewBeforeStart.value,labels:addLabels.value,bandwidthPriority:addPriority.value}
   if(addDirectory.value)args['download-dir']=addDirectory.value
   if(session.value['rpc-version']>=18)args.sequential_download=addSequential.value
   if(entry.file)args.metainfo=await readTorrent(entry.file);else args.filename=entry.url
   const result=await rpc('torrent-add',args),added=result['torrent-added']
   if(added)created.push(added.id)
   count++
   if(entry.file)addFiles.value=addFiles.value.filter(f=>f!==entry.file);else addURL.value=addURL.value.split('\n').filter(line=>line.trim()!==entry.url).join('\n')
   if(added&&preferences.value.defaultTrackers.trim()){
    try{const existing=await rpc('torrent-get',{ids:[added.id]},{details:true,view:'trackers',silent:true});await rpc('torrent-set',{ids:[added.id],trackerList:[existing.torrents?.[0]?.trackerList,preferences.value.defaultTrackers].filter(Boolean).join('\n\n')},{silent:true})}
    catch{trackerFailures.push(added.name||`任务 ${added.id}`)}
   }
  }
  notify.success(`已处理 ${count} 个下载任务`);rememberDirectory(addDirectory.value);addOpen.value=false
  if(reviewBeforeStart.value&&created.length){reviewIDs.value=created;reviewIndex.value=0;reviewOpen.value=true;await loadReviewTask()}
 }catch(e){if(!e.message?.includes('Transmission'))notify.error(e.message);if(count)notify.info(`已成功处理 ${count} 项，未完成项保留在表单中`)}finally{busy.value=false;refreshAfterWrite();if(trackerFailures.length)notify.info(`任务已添加，${trackerFailures.length} 项默认 Tracker 未保存，可在任务详情中重试`)}
}
async function loadReviewTask(){const id=reviewIDs.value[reviewIndex.value];if(id==null)return;reviewLoading.value=true;try{const data=await rpc('torrent-get',{ids:[id]},{details:true,view:'files',freshFiles:true});if(reviewIDs.value[reviewIndex.value]===id)reviewTask.value=data.torrents?.[0]||null}finally{reviewLoading.value=false}}
async function updateReview(args){if(reviewTask.value&&await action('torrent-set',[reviewTask.value.id],args))await loadReviewTask()}
async function finishReview(){if(addPaused.value||await action('torrent-start',reviewIDs.value))reviewOpen.value=false}
watch(reviewIndex,()=>{if(reviewOpen.value)loadReviewTask()})
function addStart(){addDirectory.value=session.value['download-dir']||'';freeSpace.value=null;addOpen.value=true}
function resizeDetail(event){
 const start=event.clientY,height=preferences.value.detailHeight
 event.currentTarget.setPointerCapture(event.pointerId)
 const element=event.currentTarget
 const move=e=>preferences.value.detailHeight=Math.max(180,Math.min(window.innerHeight*.65,height+start-e.clientY))
 const end=()=>{element.removeEventListener('pointermove',move);element.removeEventListener('pointerup',end);element.removeEventListener('pointercancel',end)}
 element.addEventListener('pointermove',move);element.addEventListener('pointerup',end);element.addEventListener('pointercancel',end)
}
</script>


<template>
 <div class="downloads-page app-page-layout">
  <h1 class="sr-only">Transmission 下载管理</h1>
  <div v-if="demo" class="rename-demo-banner"><div><strong>重命名交互预览</strong><span>模拟数据 · 选择任务后使用原有“重命名”按钮或右键菜单；所有操作仅在本页内存中生效。</span></div><AppButton @click="resetMock">重置示例</AppButton><RouterLink to="/downloads">返回下载器</RouterLink></div>
  <LibraryToolbar class="downloads-library-toolbar">
   <template #filters><button v-if="isMobile" class="download-filter-trigger" :aria-expanded="filtersOpen" @click="filtersOpen=true"><ListFilter :size="18"/>筛选<span v-if="activeFilterCount" class="download-filter-count">{{ activeFilterCount }}</span></button><span class="media-library-total">{{ tasks.length }} 个任务</span></template>
   <template #search><form class="media-library-search" data-focus-container role="search" @submit.prevent><input v-model="search" type="search" aria-label="搜索下载任务" placeholder="搜索名称、路径、标签或哈希"><button aria-label="搜索下载任务"><Search :size="17"/></button></form></template>
   <AppButton class="media-library-action" :disabled="demo||!connected" @click="addStart"><FilePlus2 :size="16"/><span>{{ isMobile?'添加':'添加任务' }}</span></AppButton><AppButton class="media-library-action" aria-label="下载器设置" :disabled="demo||!connected" @click="openSettings()"><Settings2 :size="16"/><span class="download-control-label">下载器设置</span></AppButton><AppButton class="media-library-action" aria-label="刷新下载任务" :disabled="loading" @click="refresh(true)"><RefreshCw :size="16" :class="{'is-spinning':loading}"/><span class="download-control-label">刷新</span></AppButton>
  </LibraryToolbar>
  <div v-if="error" class="download-error" role="alert"><span>{{ error }}<small v-if="lastUpdated">保留上次成功获取的数据。</small></span><RouterLink to="/settings?section=download">检查连接设置</RouterLink></div>
  <div class="download-workspace">
   <DownloadSidebar v-if="!isMobile" :tasks="tasks" :filters="filters" :status="filter" :label="label" :directory="directory" @status="filter=$event" @label="label=$event" @directory="directory=$event"/>
   <div class="download-main-pane">
    <div v-show="!isMobile||selected.length" class="download-actions" aria-label="下载任务操作">
     <span class="selection-count">{{ selected.length ? `已选 ${selected.length} 项` : '选择任务以管理' }}</span>
     <button :disabled="!selected.length||busy||!connected" title="继续 F3" @click="action('torrent-start')"><Play :size="16"/>继续</button><button :disabled="!selected.length||busy||!connected" title="暂停 F4" @click="action('torrent-stop')"><Pause :size="16"/>暂停</button>
     <button :disabled="selected.length!==1||busy||!connected" class="download-desktop-action" title="重命名 F2" @click="openRenameRoot(selected[0])"><PenLine :size="16"/>重命名</button><button :disabled="!selected.length||busy||!connected" class="download-desktop-action" title="移动 F6" @click="openEditor('move')"><FolderInput :size="16"/>移动</button><button :disabled="!selected.length||busy||!connected" class="download-desktop-action" title="标签 F7" @click="openEditor('labels')"><Tag :size="16"/>标签</button>
     <AppDropdown class="download-touch-menu" @command="command=>taskCommand(command,selected)"><button class="download-more" :disabled="!selected.length||busy||!connected">更多<ChevronDown :size="14"/></button><template #dropdown><AppDropdownMenu><template v-if="isMobile"><AppDropdownItem command="rename" :disabled="selected.length!==1">重命名</AppDropdownItem><AppDropdownItem command="move">移动 / 更改位置</AppDropdownItem><AppDropdownItem command="labels">编辑标签</AppDropdownItem></template><AppDropdownItem command="torrent-start-now" :divided="isMobile">强制启动（跳过队列）</AppDropdownItem><AppDropdownItem command="torrent-verify">重新校验</AppDropdownItem><AppDropdownItem command="torrent-reannounce">重新通告 Tracker</AppDropdownItem><AppDropdownItem command="queue-move-top" divided>移到队首</AppDropdownItem><AppDropdownItem command="queue-move-up">上移</AppDropdownItem><AppDropdownItem command="queue-move-down">下移</AppDropdownItem><AppDropdownItem command="queue-move-bottom">移到队尾</AppDropdownItem><AppDropdownItem command="properties" divided>编辑任务属性</AppDropdownItem><AppDropdownItem command="trackers">编辑 Tracker</AppDropdownItem><AppDropdownItem command="magnet">复制磁力链接</AppDropdownItem><AppDropdownItem v-if="isMobile" command="remove" variant="destructive" divided>删除任务…</AppDropdownItem></AppDropdownMenu></template></AppDropdown>
     <button class="remove-task download-desktop-action" :disabled="!selected.length||busy||!connected" @click="removeTasks()"><Trash2 :size="16"/>删除</button><button v-if="selected.length" class="clear-selection" @click="selected=[]">清除</button>
    </div>
    <div v-if="directory||label||filter!=='all'" class="download-filter-path"><button v-if="filter!=='all'" @click="filter='all'">{{ filters.find(f=>f.value===filter)?.label }} ×</button><button v-if="directory" @click="directory=''">目录：{{ directory }} ×</button><button v-if="label" @click="label=''">标签：{{ label==='\0'?'无标签':label }} ×</button></div>
    <div v-if="isMobile" class="download-mobile-list-controls">
     <label class="download-select-page"><input type="checkbox" :checked="allSelected" :indeterminate="!allSelected&&paged.some(t=>selectedSet.has(t.id))" :disabled="!paged.length" aria-label="选择本页任务" @change="toggleAll($event.target.checked)">本页</label>
     <div class="download-mobile-sort"><AppSelect :model-value="sortKey" aria-label="下载任务排序" @update:model-value="tasksTable.sort($event)"><AppOption v-for="column in downloadColumns" :key="column.key" :value="column.key" :label="column.label"/></AppSelect><button :aria-label="descending?'改为升序排列':'改为降序排列'" @click="tasksTable.sort(sortKey)"><ArrowDown v-if="descending" :size="18"/><ArrowUp v-else :size="18"/></button></div>
    </div>
    <div v-if="isMobile" ref="taskScroll" class="download-mobile-scroll" aria-label="下载任务列表">
     <MobileTaskList :tasks="paged" :selected="selectedSet" :busy="busy" :connected="connected" @select="toggleTask" @details="openDetails" @command="(command,task)=>taskCommand(command,[task.id])"/>
     <div v-if="!visible.length" class="download-empty"><RefreshCw v-if="loading&&!lastUpdated" :size="24" class="is-spinning"/><Search v-else :size="26"/><strong>{{ loading&&!lastUpdated?'正在读取任务…':error&&!lastUpdated?'暂时无法获取任务':tasks.length?'没有符合条件的任务':'暂无下载任务' }}</strong><span>{{ tasks.length?'调整筛选条件或搜索关键词':'订阅下载和手动添加的任务会显示在这里' }}</span></div>
    </div>
    <ContextMenuRoot v-else><ContextMenuTrigger as-child><div ref="taskScroll" class="download-table-scroll" aria-label="下载任务列表" tabindex="0" @contextmenu="taskContext">
     <table class="download-table" :style="{width:`${Math.max(820,tasksTable.totalWidth.value+126)}px`}"><TableHeader :table="tasksTable"><template #before><th class="check-column"><input type="checkbox" :checked="allSelected" :indeterminate="!allSelected&&paged.some(t=>selectedSet.has(t.id))" aria-label="选择本页任务" @change="toggleAll($event.target.checked)"></th></template><template #after><th class="row-actions-heading">操作</th></template></TableHeader><tbody>
             <tr v-for="task in paged" :key="task.id" :class="{'is-selected':selectedSet.has(task.id),'is-current':detail?.id===task.id,'has-error':taskError(task)}"  :data-task-id="task.id" :tabindex="focusedTask===task.id||(!focusedTask&&paged[0]?.id===task.id)?0:-1" :aria-selected="selectedSet.has(task.id)" @click="rowClick($event,task)" @keydown="rowKey($event,task)">
        <td><input type="checkbox" :checked="selectedSet.has(task.id)" :aria-label="`选择 ${task.name}`" @click.stop="selectTask($event,task,true)"></td>
        <td v-for="column in columns" :key="column.key" :class="column.key==='name'?'task-name-cell':'numeric'">
         <template v-if="column.key==='name'"><button class="task-name" :title="[task.name,task.downloadDir,(task.labels||[]).join('、'),taskError(task)].filter(Boolean).join('\n')" @click="selectTask($event,task)" @dblclick="openRenameRoot(task.id)">{{ task.name }}</button></template>
         <template v-else-if="column.key==='status'"><span class="task-status" :class="{'status-active':[4,6].includes(task.status),'status-error':taskError(task)}" :title="taskError(task)||`${task.peersConnected||0} 个连接`">{{ taskStatus(task) }}</span></template>
         <template v-else-if="column.key==='percentDone'"><div class="task-progress-inline"><span class="task-progress-label">{{ percent(task.status===2?task.recheckProgress:task.percentDone) }}</span><div class="task-progress" role="progressbar" :aria-label="`${task.name} 的进度`" :aria-valuenow="Math.round(task.percentDone*100)" aria-valuemin="0" aria-valuemax="100"><i :style="{width:`${Math.min(100,Math.max(0,(task.status===2?task.recheckProgress:task.percentDone)*100))}%`}"/></div></div></template>
         <span v-else :title="String(task[column.key]??'')">{{ column.format?column.format(task[column.key],task):task[column.key]??'—' }}</span>
        </td>
        <td><div class="task-row-actions"><button :disabled="busy||!connected" :aria-label="`${task.status===0?'继续':'暂停'} ${task.name}`" @click="action(task.status===0?'torrent-start':'torrent-stop',[task.id])"><Play v-if="task.status===0" :size="16"/><Pause v-else :size="16"/></button><button :aria-label="`查看 ${task.name} 的详情`" @click="openDetails(task)"><Info :size="16"/></button></div></td>
       </tr>

      <tr v-if="!visible.length"><td :colspan="columns.length+2" class="download-empty"><RefreshCw v-if="loading&&!lastUpdated" :size="24" class="is-spinning"/><Search v-else :size="26"/><strong>{{ loading&&!lastUpdated?'正在读取任务…':error&&!lastUpdated?'暂时无法获取任务':tasks.length?'没有符合条件的任务':'暂无下载任务' }}</strong><span>{{ tasks.length?'调整左侧分类或搜索关键词':'订阅下载和手动添加的任务会显示在这里' }}</span></td></tr>
     </tbody></table>
    </div>
    </ContextMenuTrigger><ContextMenuPortal><ContextMenuContent class="download-context-menu"><ContextMenuItem v-for="[command,title] in [['torrent-start','继续'],['torrent-start-now','强制启动'],['torrent-stop','暂停'],['torrent-verify','重新校验'],['torrent-reannounce','重新通告 Tracker'],['queue-move-top','移到队首'],['queue-move-up','上移'],['queue-move-down','下移'],['queue-move-bottom','移到队尾']]" :key="command" class="download-context-item" :disabled="!contextIDs.length||busy||!connected" @select="action(command,contextIDs)">{{ title }}</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" :disabled="contextIDs.length!==1||busy" @select="openRenameRoot(contextIDs[0])">重命名</ContextMenuItem><ContextMenuItem v-for="[command,title] in [['move','移动 / 更改位置'],['labels','编辑标签'],['trackers','编辑 Tracker'],['properties','任务属性']]" :key="command" class="download-context-item" :disabled="!contextIDs.length||busy||!connected" @select="openEditor(command,contextIDs)">{{ title }}</ContextMenuItem><ContextMenuItem class="download-context-item" :disabled="!contextIDs.length" @select="copyMagnets(contextIDs)">复制磁力链接</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" :disabled="!contextIDs.length||busy||!connected" @select="removeTasks(contextIDs)">删除任务…</ContextMenuItem></ContextMenuContent></ContextMenuPortal></ContextMenuRoot>
    <div class="download-pagination"><span>{{ visible.length }} 项 · {{ bytes(visible.reduce((n,t)=>n+(t.sizeWhenDone||0),0)) }}</span><button :disabled="page<=1" aria-label="上一页下载任务" @click="page--"><ChevronLeft :size="16"/></button><span>{{ page }} / {{ pageCount }}</span><button :disabled="page>=pageCount" aria-label="下一页下载任务" @click="page++"><ChevronRight :size="16"/></button><button v-if="!isMobile" class="detail-dock-toggle" :aria-expanded="detailOpen" :disabled="!detailOpen&&!visible.length" :title="preferences.detailsDocked?'下方详情面板':'弹窗详情'" @click="toggleDetails"><PanelBottom v-if="preferences.detailsDocked" :size="15"/><Info v-else :size="15"/>{{ detailOpen?'收起详情':'显示详情' }}</button></div>
   </div>
  </div>
  <section v-if="detailPresentation==='docked'" class="download-detail-dock" :style="{height:`${preferences.detailHeight}px`}" aria-label="选中任务详情">
   <div class="download-detail-resize" role="separator" aria-label="调整详情高度" aria-orientation="horizontal" :aria-valuenow="Math.round(preferences.detailHeight)" :aria-valuemin="180" :aria-valuemax="Math.round(700)" tabindex="0" @pointerdown="resizeDetail" @keydown.up.prevent="preferences.detailHeight=Math.min(700,preferences.detailHeight+20)" @keydown.down.prevent="preferences.detailHeight=Math.max(180,preferences.detailHeight-20)"/>
   <div class="download-detail-heading"><strong>{{ detail.name }}</strong></div><div class="download-detail-body"><p v-if="detailError" class="download-error" role="status">详情读取失败：{{ detailError }}。列表继续更新。</p><TorrentDetails :task="detail" v-model:tab="detailTab" :fields="detailFields" :rpc-version="session['rpc-version']" :speed-unit="session.units?.['speed-units']?.[0]" :loading="detailInitialLoading" :refreshing="detailRefreshing" :busy="busy||!connected" @update="updateTask" @action="method=>action(method,[detail.id])" @rename="(path,name)=>path?openRename(detail.id,path,name):openRenameRoot(detail.id)" @move="openEditor('move',[detail.id])" @labels="openEditor('labels',[detail.id])" @trackers="openEditor('trackers',[detail.id])" @magnet="copyMagnets([detail.id])"/></div>
  </section>
  <footer class="download-summary"><span v-if="statsError||sessionError" class="summary-warning" :title="statsError||sessionError">{{ statsError?'速率统计暂未更新':'会话设置暂未更新' }}</span><span class="connection-state" :class="{online:connected}"><i/>{{ connected?`Transmission ${session.version||''}`:error?'连接失败':'正在连接' }}</span><span><ArrowDown :size="14"/>{{ speed(stats.downloadSpeed) }}</span><span><ArrowUp :size="14"/>{{ speed(stats.uploadSpeed) }}</span><button class="summary-bandwidth" :class="{'alt-speed':session['alt-speed-enabled']}" :disabled="!connected" @click="openSettings('带宽')">{{ session['alt-speed-enabled']?'备用限速已开启':'普通带宽' }}</button><span v-if="lastUpdated" class="updated-at">{{ lastUpdated.toLocaleTimeString('zh-CN',{hour12:false}) }}</span><button class="summary-statistics" aria-label="查看下载器状态" @click="openSettings('状态统计')">{{ isMobile?'状态':'下载器状态' }}</button></footer>

  <AppModal class="download-detail-dialog" v-if="detailPresentation==='dialog'" :model-value="true" :title="detail.name||'任务详情'" width="1080px" @update:model-value="detailDialogChanged"><p v-if="detailError" class="download-error" role="status">详情读取失败：{{ detailError }}</p><TorrentDetails v-if="detail" :task="detail" v-model:tab="detailTab" :fields="detailFields" :rpc-version="session['rpc-version']" :speed-unit="session.units?.['speed-units']?.[0]" :loading="detailInitialLoading" :refreshing="detailRefreshing" :busy="busy||!connected" @update="updateTask" @action="method=>action(method,[detail.id])" @rename="(path,name)=>path?openRename(detail.id,path,name):openRenameRoot(detail.id)" @move="openEditor('move',[detail.id])" @labels="openEditor('labels',[detail.id])" @trackers="openEditor('trackers',[detail.id])" @magnet="copyMagnets([detail.id])"/></AppModal>
  <AppModal class="download-task-dialog" v-model="addOpen" title="添加下载任务" width="680px"><form id="add-transmission-task" class="download-form" @submit.prevent="addTorrent"><p class="download-help">支持多行磁力链接、HTTP(S) 种子地址和多个本地种子文件。任务直接添加到 Transmission。</p><label for="torrent-url">链接（每行一个）</label><AppInput id="torrent-url" v-model="addURL" type="textarea" :rows="3" placeholder="magnet:?xt=…"/><input ref="fileInput" type="file" multiple accept=".torrent,application/x-bittorrent" aria-label="选择种子文件" @change="chooseFiles"><span v-if="addFiles.length" class="download-help">已选 {{ addFiles.length }} 个种子文件</span><label>保存目录</label><AppSelect v-model="addDirectory" filterable allow-create placeholder="下载器默认目录" aria-label="新任务保存目录"><AppOption v-for="dir in directories" :key="dir" :value="dir" :label="dir"/></AppSelect><button type="button" class="text-action" @click="checkSpace(addDirectory)">查询目录剩余空间</button><span v-if="freeSpace" class="download-help">可用 {{ bytes(freeSpace['size-bytes']) }} / 总计 {{ bytes(freeSpace['total_size']) }}</span><label>标签</label><AppTagsInput v-model="addLabels" placeholder="输入标签后按 Enter"/><label>带宽优先级</label><AppSelect v-model="addPriority" aria-label="新任务优先级"><AppOption :value="1" label="高"/><AppOption :value="0" label="普通"/><AppOption :value="-1" label="低"/></AppSelect><AppCheckbox v-model="reviewBeforeStart">开始下载前选择文件（先创建暂停任务）</AppCheckbox><AppCheckbox v-model="addPaused">添加后保持暂停（可先到文件详情选择下载内容）</AppCheckbox><AppCheckbox v-if="session['rpc-version']>=18" v-model="addSequential">顺序下载</AppCheckbox></form><template #footer><AppButton :disabled="busy" @click="addOpen=false">取消</AppButton><AppButton type="primary" :loading="busy" :disabled="!addFiles.length&&!addURL.trim()" native-type="submit" form="add-transmission-task">添加任务</AppButton></template></AppModal>
  <AppModal class="download-task-dialog" v-model="reviewOpen" title="选择下载文件" width="1080px"><p class="download-help">新任务已在 Transmission 中保持暂停。选择完成后可以开始下载；关闭此窗口会保留暂停状态。已存在的重复任务不会被改动。</p><div class="download-pagination"><button :disabled="reviewIndex<=0||reviewLoading" @click="reviewIndex--">上一个</button><span>{{ reviewIndex+1 }} / {{ reviewIDs.length }} · {{ reviewTask?.name }}</span><button :disabled="reviewIndex>=reviewIDs.length-1||reviewLoading" @click="reviewIndex++">下一个</button></div><TorrentFiles v-if="reviewTask" :task="reviewTask" :busy="busy||reviewLoading" :loading="reviewLoading" @update="updateReview" @rename="(path,name)=>openRename(reviewTask.id,path,name)"/><template #footer><AppButton :disabled="busy" @click="reviewOpen=false">保留暂停</AppButton><AppButton type="primary" :loading="busy" @click="finishReview">{{ addPaused?'完成并保留暂停':'完成并开始下载' }}</AppButton></template></AppModal>
  <AppModal class="download-task-dialog" v-model="removeOpen" title="删除下载任务" width="500px"><div class="download-form"><p>从 Transmission 移除 {{ removeIDs.length }} 个任务，MyAnimeGo 订阅会保留。</p><AppCheckbox v-model="deleteFiles">同时删除已下载的本地文件</AppCheckbox><p class="download-help" :class="{'destructive-copy':deleteFiles}">{{ deleteFiles?'本地文件将被永久删除，无法恢复。':'默认保留文件。仍启用的订阅之后可能重新添加下载任务。' }}</p></div><template #footer><AppButton :disabled="busy" @click="removeOpen=false">取消</AppButton><AppButton type="danger" :loading="busy" :disabled="!connected" @click="confirmRemove">{{ deleteFiles?'删除任务和文件':'仅删除任务' }}</AppButton></template></AppModal>
  <BatchRename v-model="batchRenameOpen" :task="batchRenameTask" :directory="batchRenameDirectory" :request="mock?.rename" :demo="demo" @directory-renamed="batchDirectoryRenamed" @changed="batchRenameChanged"/>
  <AppModal class="download-task-dialog" :model-value="Boolean(editor)" :title="({rename:'重命名',move:'移动 / 更改位置',labels:'编辑任务标签',trackers:'编辑 Tracker',properties:'编辑任务属性'})[editor]" width="680px" @update:model-value="!$event&&(editor=null)">
   <div v-if="editor==='rename'" class="download-form"><p class="download-help">{{ demo?'模拟文件路径':'当前路径' }}：{{ editorPath }}</p><label for="torrent-rename">新名称</label><AppInput id="torrent-rename" v-model="editorName"/><p class="download-help">Transmission 会重命名远程文件或目录。MyAnimeGo 订阅的命名规则保持独立，后续自动整理可能再次应用订阅规则。</p></div>
   <div v-if="editor==='move'" class="download-form"><p>修改 {{ editIDs.length }} 个任务的保存位置</p><label for="torrent-move-location">保存目录</label><AppInput id="torrent-move-location" v-model="editorPath" placeholder="输入远程目录" autocomplete="off" :spellcheck="false" @update:model-value="freeSpace=null"/><p v-if="editIDs.length>1" class="download-help">默认填入第一个任务的当前目录，保存后统一使用此路径。</p><AppCheckbox v-model="moveData">将已有文件移动到新位置</AppCheckbox><p class="download-help">{{ moveData?'由 Transmission 搬动文件；订阅的默认目录保持独立。':'只修改任务位置，查找目标目录中已有的文件。可在完成后重新校验。' }}</p><button class="text-action" @click="checkSpace(editorPath)">查询目标目录空间</button><p v-if="freeSpace" class="download-help">可用 {{ bytes(freeSpace['size-bytes']) }}</p></div>
   <div v-if="editor==='labels'" class="download-form"><p>修改 {{ editIDs.length }} 个任务的标签</p><AppSelect v-model="labelMode" aria-label="标签修改方式"><AppOption value="replace" label="替换全部标签"/><AppOption value="add" label="追加标签"/><AppOption value="remove" label="移除指定标签"/></AppSelect><AppTagsInput v-model="editorLabels" placeholder="输入标签后按 Enter"/><div class="known-labels"><button v-for="name in labels" :key="name" :disabled="editorLabels.includes(name)" @click="editorLabels=[...editorLabels,name]">{{ name }}</button></div><p class="download-help">ani-rss 标签用于自动任务归属识别。管理订阅任务时请保留此标签。</p></div>
   <div v-if="editor==='trackers'" class="download-form"><p class="download-help">应用到 {{ editIDs.length }} 个任务，替换原有 Tracker。每行一个地址，空行分隔不同 tier。</p><AppInput v-model="trackerText" type="textarea" :rows="10" aria-label="Tracker 列表"/></div>
   <TorrentProperties v-if="editor==='properties'&&propertyTask" :task="propertyTask" :fields="propertyFields" :busy="busy" :multiple="editIDs.length>1" :rpc-version="session['rpc-version']" :speed-unit="session.units?.['speed-units']?.[0]" @update="saveProperties"/>
   <template #footer><AppButton :disabled="busy" @click="editor=null">取消</AppButton><AppButton v-if="editor!=='properties'" type="primary" :loading="busy" :disabled="(editor==='rename'&&!editorName.trim())||(editor==='move'&&!editorPath.trim())" @click="applyEditor">保存</AppButton></template>
  </AppModal>
  <AppModal v-if="isMobile" v-model="filtersOpen" title="筛选下载任务" width="420px" class="download-filter-dialog">
   <DownloadSidebar :tasks="tasks" :filters="filters" :status="filter" :label="label" :directory="directory" @status="mobileFilter('status',$event)" @label="mobileFilter('label',$event)" @directory="mobileFilter('directory',$event)"/>
   <template #footer><AppButton :disabled="!activeFilterCount" @click="clearFilters">重置筛选</AppButton><AppButton type="primary" @click="filtersOpen=false">完成</AppButton></template>
  </AppModal>
  <TransmissionSettings v-model="settingsOpen" :initial-section="settingsSection" :preferences="preferences" @saved="sessionChannel.restart();refresh(true)" @preferences="savePreferences"/>
 </div>
</template>
<style src="./downloads.css"></style>
<style src="./downloads-mobile.css"></style>
