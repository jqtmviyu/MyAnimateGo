import {computed,onBeforeUnmount,ref,shallowRef,watch} from 'vue'
import api from '@/js/api'
import {notify} from '@/lib/feedback'

export const parentPath=path=>path.includes('/')?path.slice(0,path.lastIndexOf('/')):''
export const pathName=path=>path.split('/').pop()
export const replacePath=(path,before,after)=>path===before||path.startsWith(before+'/')?after+path.slice(before.length):path
export function fileDirectories(files=[]){
 const paths=new Set()
 for(const file of files)for(let dir=parentPath(file.name);dir;dir=parentPath(dir))paths.add(dir)
 return [...paths].sort((a,b)=>a.localeCompare(b,'zh-CN'))
}
function nameError(name){
 if(!name.trim())return '请输入目录名称'
 if(['.','..'].includes(name)||/[\\/\x00\r\n]/.test(name))return '目录名称不能包含路径分隔符或控制字符'
 if(new TextEncoder().encode(name).length>255)return '目录名称过长'
 return ''
}
const defaultPattern='(?:S\\d+E|第|\\[|[ _-])(\\d+(?:\\.\\d+)?)'

export function useRenameSession(props,emit){
 const task=shallowRef(null),root=ref(''),rules=ref({}),plan=shallowRef(null),results=shallowRef(null)
 const loading=ref(false),busy=ref(false),failure=ref(''),filter=ref('changes'),page=ref(1),selected=ref([])
 const editingPath=ref(''),directoryName=ref(''),directoryFailure=ref('')
 const completed=new Map(),patterns={episode:defaultPattern,replace:'(.+)'}
 let timer,controller,revision=0,applying=false
 const snapshotDirectories=computed(()=>fileDirectories(task.value?.files))
 const directories=computed(()=>plan.value?.directories||snapshotDirectories.value)
 const destination=computed(()=>{const parent=parentPath(editingPath.value);return (parent?parent+'/':'')+directoryName.value})
 const directoryError=computed(()=>{
  if(!editingPath.value)return ''
  const invalid=nameError(directoryName.value);if(invalid)return invalid
  const target=destination.value.toLowerCase()
  if(snapshotDirectories.value.some(path=>path!==editingPath.value&&path.toLowerCase()===target)||(task.value?.files||[]).some(file=>file.name.toLowerCase()===target))return '同级已有同名目录或文件'
  return directoryFailure.value
 })
 const canSaveDirectory=computed(()=>!!editingPath.value&&!directoryError.value&&directoryName.value!==pathName(editingPath.value))
 const editing=computed(()=>({path:editingPath.value,name:directoryName.value,newPath:destination.value,error:directoryError.value,busy:busy.value,canSave:canSaveDirectory.value}))
 const ready=computed(()=>plan.value?.rows.filter(row=>row.status==='ready')||[])
 const selectedSet=computed(()=>new Set(selected.value))
 const allSelected=computed(()=>ready.value.length>0&&ready.value.slice(0,500).every(row=>selectedSet.value.has(row.index)))
 const display=computed(()=>results.value||plan.value?.rows||[])
 const visible=computed(()=>display.value.filter(row=>filter.value==='all'||(filter.value==='problems'?['invalid','conflict','failed'].includes(row.status):!['unmatched','unchanged'].includes(row.status))).sort((a,b)=>parentPath(a.path).localeCompare(parentPath(b.path),'zh-CN')||a.index-b.index))
 const pages=computed(()=>Math.max(1,Math.ceil(visible.value.length/100)))
 const rows=computed(()=>visible.value.slice((page.value-1)*100,page.value*100))
 const conflicts=computed(()=>display.value.filter(row=>['invalid','conflict','failed'].includes(row.status)).length)
 const finished=computed(()=>results.value?.filter(row=>row.status==='done').length||0)
 const stale=computed(()=>loading.value||!!failure.value||!plan.value)
 const canApply=computed(()=>!busy.value&&!stale.value&&!editingPath.value&&!results.value&&selected.value.length>0&&selected.value.length<=500)

 function call(method,payload,signal){
  if(props.request)return props.request(method,payload,signal)
  const body=['torrent-rename-preview','torrent-rename-batch'].includes(method)?{method,rename:payload}:{method,arguments:payload,details:method==='torrent-get',view:method==='torrent-get'?'files':'',freshFiles:method==='torrent-get'}
  return api.post('api/transmissionRPC',body,{silent:true,signal}).then(result=>result.data)
 }
 function cancelRead(){revision++;clearTimeout(timer);controller?.abort();loading.value=false}
 function close(){if(!busy.value)emit('update:modelValue',false)}
 function editDirectory(path){if(busy.value)return;editingPath.value=path;directoryName.value=pathName(path);directoryFailure.value=''}
 function cancelDirectory(){if(!busy.value){editingPath.value='';directoryFailure.value=''}}
 function updateDirectoryName(name){directoryName.value=name;directoryFailure.value=''}
 function toggle(index){selected.value=selectedSet.value.has(index)?selected.value.filter(id=>id!==index):[...selected.value,index]}
 function selectAll(value){selected.value=value?ready.value.slice(0,500).map(row=>row.index):[]}
 function changeMode(mode){if(busy.value||mode===rules.value.mode)return;patterns[rules.value.mode]=rules.value.pattern;rules.value.mode=mode;rules.value.pattern=patterns[mode]}
 async function refreshTask(){
  const data=await call('torrent-get',{ids:[task.value.id]})
  const current=data.torrents?.[0]
  if(!current||current.hashString!==task.value.hashString)throw Error('任务已变化，请关闭后重新打开重命名')
  task.value=current
 }
 async function preview(){
  cancelRead();if(!props.modelValue||!task.value||busy.value)return
  const current=revision,abort=new AbortController();controller=abort;loading.value=true;failure.value='';results.value=null
  try{
   const data=await call('torrent-rename-preview',{...rules.value,id:task.value.id},abort.signal)
   if(current!==revision)return
   if(data.hash!==task.value.hashString)throw Error('任务已变化，请重新打开重命名')
   for(const row of data.rows)if(completed.get(row.index)===row.path){row.status='done';row.message='本次已完成';row.target=row.name}
   plan.value=data;selected.value=data.rows.filter(row=>row.status==='ready').slice(0,500).map(row=>row.index);page.value=1
  }catch(error){if(error.name!=='AbortError'&&current===revision){failure.value=error.message;plan.value=null}}
  finally{if(current===revision)loading.value=false}
 }
 async function saveDirectory(){
  if(busy.value||!canSaveDirectory.value)return
  const before=editingPath.value,after=destination.value,name=directoryName.value
  busy.value=true;directoryFailure.value='';cancelRead();let renamed=false,metadataReady=false
  try{
   await refreshTask()
   if(!snapshotDirectories.value.includes(before)){editingPath.value='';failure.value='目录路径已变化，请刷新预览后重新选择';return}
   if(directoryError.value)throw Error(directoryError.value)
   await call('torrent-rename-path',{ids:[task.value.id],path:before,name})
   renamed=true
   // 先保留已成功的改名结果，再读取实际状态；后续读取失败也不会重复提交旧路径。
   const remap=path=>replacePath(path,before,after)
   root.value=remap(root.value);rules.value.directory=remap(rules.value.directory)
   for(const [index,path] of completed)completed.set(index,remap(path))
   task.value={...task.value,name:before===task.value.name?name:task.value.name,files:task.value.files.map(file=>({...file,name:remap(file.name)}))}
   editingPath.value='';plan.value=null;results.value=null;emit('directory-renamed',{before,after})
   await refreshTask();metadataReady=true
   notify.success('目录名已更新')
  }catch(error){if(renamed){failure.value=`目录已改名，读取最新状态失败：${error.message}。请刷新预览后继续。`;notify.info('目录已改名，请刷新预览核对结果')}else directoryFailure.value=error.message}
  finally{busy.value=false;if(renamed)emit('changed',task.value)}
  if(metadataReady)await preview()
 }
 async function apply(){
  if(!canApply.value)return
  const input={...rules.value,id:plan.value.id,hash:plan.value.hash,items:plan.value.rows.filter(row=>selectedSet.value.has(row.index))}
  applying=true;busy.value=true;cancelRead();failure.value=''
  try{
   const output=await call('torrent-rename-batch',input);results.value=output;page.value=1
   for(const row of output)if(row.status==='done')completed.set(row.index,parentPath(row.path)+'/'+row.target)
   if(output.every(row=>row.status==='done'))notify.success(`已重命名 ${output.length} 个文件`)
   else notify.info('部分文件未完成，请核对逐项结果')
   try{await refreshTask()}catch(error){failure.value=`操作结果已保留，读取最新文件失败：${error.message}`}
  }catch(error){failure.value=`${error.message}。请刷新预览核对实际文件名，再继续操作。`;plan.value=null}
  finally{applying=false;busy.value=false;emit('changed',task.value)}
 }
 watch(()=>props.modelValue,open=>{
  cancelRead();if(!open)return
  task.value=props.task;root.value=props.directory||props.task?.files?.[0]?.name.split('/')[0]||''
  completed.clear();plan.value=null;results.value=null;selected.value=[];filter.value='changes';page.value=1;failure.value='';editingPath.value='';directoryFailure.value=''
  patterns.episode=defaultPattern;patterns.replace='(.+)'
  const name=props.task?.name||'',match=name.match(/^(.*?)\s+Season\s*(\d+)$/i)
  rules.value={directory:root.value,recursive:true,mediaOnly:true,mode:'episode',pattern:defaultPattern,replacement:'$1',title:match?match[1]:name,season:match?Number(match[2]):1,seasonPattern:'(?i)(?:^|/)Season[ _]*(\\d+)(?:/|$)',ignoreCase:true,preserveExtension:true}
 },{immediate:true})
 watch(rules,()=>{if(!props.modelValue||busy.value)return;cancelRead();loading.value=true;timer=setTimeout(preview,350)},{deep:true,flush:'sync'})
 watch(()=>props.modelValue,open=>{if(open)preview()},{immediate:true,flush:'post'})
 watch(pages,value=>{page.value=Math.min(page.value,value)})
 watch(filter,()=>{page.value=1})
 onBeforeUnmount(()=>{cancelRead();if(applying)notify.info('请在下载器中核对正在处理的重命名结果')})
 return {task,root,rules,results,loading,busy,failure,filter,page,directories,editing,ready,selected,selectedSet,allSelected,visible,pages,rows,conflicts,finished,stale,canApply,close,editDirectory,cancelDirectory,updateDirectoryName,toggle,selectAll,changeMode,preview,saveDirectory,apply}
}
