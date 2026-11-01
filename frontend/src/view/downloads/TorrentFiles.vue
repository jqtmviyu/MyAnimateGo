<script setup>
import {useMediaQuery} from '@vueuse/core'
import {computed,inject,ref,shallowRef,watch} from 'vue'
import {ChevronDown,ChevronRight,File,Folder,FolderOpen,MoreHorizontal,PenLine} from '@lucide/vue'
import {ContextMenuRoot,ContextMenuTrigger,ContextMenuPortal,ContextMenuContent,ContextMenuItem,ContextMenuSeparator} from 'reka-ui'
import {layerContext} from '@/components/app/layers'
import {notify} from '@/lib/feedback'
import TableHeader from './TableHeader.vue'
import {useTableLayout} from './table-layout'
import {selectedPrefix,selectedCount} from './file-selection'
import {bytes,percent} from './transmission'
const layer=inject(layerContext,null),menuLayer=computed(()=>(layer?.value||60)+30)
const props=defineProps({task:{type:Object,required:true},busy:Boolean,loading:Boolean})
const isMobile=useMediaQuery('(max-width: 780px)')
const emit=defineEmits(['update','rename'])
const contextNode=shallowRef(null),searchInput=ref(null),anchor=ref(null)
const fileColumns=[{key:'name',label:'文件与目录',required:true,width:420,minWidth:220},{key:'length',label:'大小',format:bytes},{key:'progress',label:'完成',format:percent},{key:'wanted',label:'下载'},{key:'priority',label:'优先级'}]
const fileTable=useTableLayout('files',fileColumns),columns=fileTable.columns
const query=ref(''),flat=ref(false),selected=ref([]),expanded=ref(new Set()),tree=shallowRef([]),leaves=shallowRef([])
watch(()=>props.task.id,()=>{selected.value=[];expanded.value=new Set();query.value='';tree.value=[];leaves.value=[]})
// 文件路径只在首次读取或重命名后建树，进度轮询只重新汇总数字。
watch(()=>props.task.files,files=>{
 if(!files)return
 const root={children:new Map()},ordered=[]
 files.forEach((file,index)=>{
  let parent=root,path=''
  file.name.split('/').forEach((part,i,parts)=>{
   path=path ? `${path}/${part}` : part
   if(!parent.children.has(part)) parent.children.set(part,{name:part,path,children:new Map(),index:i===parts.length-1 ? index : null})
   parent=parent.children.get(part)
  })
 })
 function finalize(parent,depth){return [...parent.children.values()].sort((a,b)=>Number(b.index===null)-Number(a.index===null)||a.name.localeCompare(b.name,'zh-CN')).map(node=>{
  node.depth=depth;node.start=ordered.length;node.rows=finalize(node,depth+1)
  if(node.index!==null)ordered.push(node.index)
  node.end=ordered.length;return node
 })}
 tree.value=finalize(root,0);leaves.value=ordered
 if(!expanded.value.size)expanded.value=new Set(tree.value.filter(n=>n.index===null).map(n=>n.path))
 selected.value=selected.value.filter(i=>i<files.length)
},{immediate:true})
const aggregates=computed(()=>{
 const map=new Map()
 function walk(nodes){for(const node of nodes){
  if(node.index!==null){const file=props.task.files?.[node.index] || {},stat=props.task.fileStats?.[node.index] || {};map.set(node.path,{length:file.length||0,done:stat.bytesCompleted ?? file.bytesCompleted ?? 0,wanted:Number(Boolean(stat.wanted)),count:1,priority:stat.priority ?? 0})}
  else {walk(node.rows);const totals={length:0,done:0,wanted:0,count:0,priority:null};for(const child of node.rows){const t=map.get(child.path);for(const k of ['length','done','wanted','count'])totals[k]+=t[k];if(totals.priority===null)totals.priority=t.priority;else if(totals.priority!==t.priority)totals.priority=2}map.set(node.path,totals)}
 }}walk(tree.value);return map
})
const rows=computed(()=>{
 const result=[],needle=query.value.trim().toLowerCase()
 function walk(nodes){const key=fileTable.layout.value.sort.key,desc=fileTable.layout.value.sort.descending;const ordered=[...nodes].sort((a,b)=>{if(a.index===null&&b.index!==null)return -1;if(a.index!==null&&b.index===null)return 1;const stat=n=>{const totals=aggregates.value.get(n.path)||{};return key==='name'?n.name:key==='progress'?(totals.length?totals.done/totals.length:1):key==='wanted'?totals.wanted:totals[key]};const x=stat(a),y=stat(b);return (desc?-1:1)*(typeof x==='string'?x.localeCompare(y,'zh-CN'):(x||0)-(y||0))});for(const node of ordered){
  if(needle || flat.value){if(node.index!==null && (!needle || node.path.toLowerCase().includes(needle)))result.push(node);walk(node.rows)}
  else {result.push(node);if(expanded.value.has(node.path))walk(node.rows)}
 }}walk(tree.value);return result
})
const displayLimit=ref(200)
watch([query,flat],()=>{displayLimit.value=200})
const visibleRows=computed(()=>rows.value.slice(0,displayLimit.value))
function indexes(node){return leaves.value.slice(node.start,node.end)}
function toggleExpand(path){const set=new Set(expanded.value);set.has(path)?set.delete(path):set.add(path);expanded.value=set}
const selectionPrefix=computed(()=>selectedPrefix(leaves.value,selected.value))
const checked=node=>selectedCount(selectionPrefix.value,node)===node.end-node.start
const partially=node=>{const count=selectedCount(selectionPrefix.value,node);return count>0&&count<node.end-node.start}
function choose(node,checked){const ids=new Set(indexes(node));selected.value=checked?[...new Set([...selected.value,...ids])]:selected.value.filter(i=>!ids.has(i));anchor.value=node.path}
function selectFile(event,node,checkbox=false){
 if(event.shiftKey&&anchor.value){const ids=rows.value.map(n=>n.path),start=ids.indexOf(anchor.value),end=ids.indexOf(node.path);if(start>=0){const span=[...new Set(rows.value.slice(Math.min(start,end),Math.max(start,end)+1).flatMap(indexes))];selected.value=event.ctrlKey||event.metaKey?[...new Set([...selected.value,...span])]:span;return}}
 if(checkbox||event.ctrlKey||event.metaKey)choose(node,!checked(node));else{selected.value=indexes(node);anchor.value=node.path}
}
function change(key,ids=selected.value){if(ids.length)emit('update',{[key]:ids})}
const allShownIDs=computed(()=>query.value||flat.value?rows.value.map(node=>node.index):leaves.value)
const contextIndexes=computed(()=>!contextNode.value?[]:checked(contextNode.value)?selected.value:indexes(contextNode.value))
function fileContext(event){
 const row=event.target.closest('[data-file-path]')
 contextNode.value=row?visibleRows.value.find(node=>node.path===row.dataset.filePath)||null:null
}
function expandAll(){const paths=[];function walk(nodes){for(const node of nodes){if(node.index===null)paths.push(node.path);walk(node.rows)}}walk(tree.value);expanded.value=new Set(paths)}
async function copyPath(node=contextNode.value){if(!node)return;try{await navigator.clipboard.writeText(`${props.task.downloadDir.replace(/[\\/]+$/,'')}/${node.path}`);notify.success('远程路径已复制')}catch{notify.error('无法复制路径')}}
const allSelected=computed(()=>{const chosen=new Set(selected.value);return allShownIDs.value.length>0&&allShownIDs.value.every(i=>chosen.has(i))})
function selectAll(checked){const visible=new Set(allShownIDs.value);selected.value=checked?[...new Set([...selected.value,...visible])]:selected.value.filter(i=>!visible.has(i))}
</script>
<template>
 <div class="torrent-files">
  <div class="detail-file-toolbar"><AppInput ref="searchInput" v-model="query" placeholder="搜索文件或目录" aria-label="搜索文件或目录" clearable/><AppCheckbox v-model="flat">平铺文件</AppCheckbox><span>{{ selected.length }} 项已选</span><AppButton :disabled="busy || !selected.length" @click="change('files-wanted')">下载</AppButton><AppButton :disabled="busy || !selected.length" @click="change('files-unwanted')">跳过</AppButton><AppSelect placeholder="批量优先级" :disabled="busy || !selected.length" aria-label="批量文件优先级" @update:model-value="change($event)"><AppOption value="priority-high" label="高优先级"/><AppOption value="priority-normal" label="普通优先级"/><AppOption value="priority-low" label="低优先级"/></AppSelect></div>
  <div v-if="isMobile" class="mobile-file-list">
   <div class="mobile-file-controls"><label><input type="checkbox" :checked="allSelected" :indeterminate="!allSelected&&selected.length>0" aria-label="选择筛选的全部文件" @change="selectAll($event.target.checked)">全选</label><button @click="expandAll">展开目录</button><button @click="expanded=new Set()">收起目录</button></div>
   <ul>
    <li v-for="node in visibleRows" :key="node.path" class="mobile-file" :class="{'file-is-selected':checked(node)}">
     <div class="mobile-file-heading" :style="{paddingLeft:flat||query?'0':`${Math.min(node.depth,3)*10}px`}">
      <label class="mobile-file-check"><input type="checkbox" :checked="checked(node)" :indeterminate="partially(node)" :aria-label="`选择 ${node.path}`" @change="choose(node,$event.target.checked)"></label>
      <button v-if="node.index===null" class="mobile-file-expand" :aria-label="`${expanded.has(node.path)?'折叠':'展开'} ${node.name}`" @click="toggleExpand(node.path)"><ChevronDown v-if="expanded.has(node.path)" :size="18"/><ChevronRight v-else :size="18"/></button>
      <File v-else :size="17" class="mobile-file-type"/>
      <button class="mobile-file-name" @click="node.index===null?toggleExpand(node.path):choose(node,!checked(node))">{{ flat||query?node.path:node.name }}</button>
      <AppDropdown class="download-touch-menu" @command="command=>command==='rename'?emit('rename',node.path,node.name):copyPath(node)"><button class="mobile-file-expand" :aria-label="`${node.name} 的操作菜单`"><MoreHorizontal :size="20"/></button><template #dropdown><AppDropdownMenu><AppDropdownItem command="rename" :disabled="busy">重命名</AppDropdownItem><AppDropdownItem command="copy">复制远程完整路径</AppDropdownItem></AppDropdownMenu></template></AppDropdown>
     </div>
     <div class="mobile-file-metadata"><span>{{ bytes(aggregates.get(node.path)?.length) }} · {{ percent(aggregates.get(node.path)?.length?aggregates.get(node.path).done/aggregates.get(node.path).length:1) }}</span></div>
     <div class="mobile-file-options"><AppCheckbox :model-value="aggregates.get(node.path)?.wanted===aggregates.get(node.path)?.count" :indeterminate="aggregates.get(node.path)?.wanted>0&&aggregates.get(node.path)?.wanted<aggregates.get(node.path)?.count" :disabled="busy||!task.fileStats" :aria-label="`下载 ${node.path}`" @update:model-value="change($event?'files-wanted':'files-unwanted',indexes(node))">下载</AppCheckbox><AppSelect :model-value="aggregates.get(node.path)?.priority" :disabled="busy||!task.fileStats" :aria-label="`${node.path} 的优先级`" @update:model-value="change($event===1?'priority-high':$event===-1?'priority-low':'priority-normal',indexes(node))"><AppOption :value="1" label="高优先级"/><AppOption :value="0" label="普通优先级"/><AppOption :value="-1" label="低优先级"/><AppOption :value="2" label="混合优先级" disabled/></AppSelect></div>
    </li>
   </ul>
   <p v-if="!rows.length" class="detail-empty">{{ loading?'正在读取文件…':'没有符合条件的文件' }}</p>
   <button v-if="rows.length>displayLimit" class="load-more-files" @click="displayLimit+=200">继续显示 {{ Math.min(200,rows.length-displayLimit) }} 项（共 {{ rows.length }} 项）</button>
  </div>
  <ContextMenuRoot v-else><ContextMenuTrigger as-child><div class="detail-table-scroll file-tree-scroll" @contextmenu="fileContext"><table class="detail-table file-tree-table" :style="{width:`${fileTable.totalWidth.value+84}px`}"><TableHeader :table="fileTable"><template #before><th class="check-column"><input type="checkbox" :checked="allSelected" aria-label="选择筛选的全部文件" @change="selectAll($event.target.checked)"></th></template><template #after><th style="width:48px">操作</th></template></TableHeader><tbody>
   <tr v-for="node in visibleRows" :key="node.path" :class="{'file-is-selected':checked(node)}" :data-file-path="node.path"><td><input type="checkbox" :checked="checked(node)" :indeterminate="partially(node)" :aria-label="`选择 ${node.path}`" @click.stop="selectFile($event,node,true)"></td><td v-for="column in columns" :key="column.key"><template v-if="column.key==='name'"><div class="file-tree-name"><div :style="{paddingLeft:flat || query ? '0' : `${node.depth*18}px`}"><button v-if="node.index===null" class="file-tree-expand" :aria-label="`${expanded.has(node.path)?'折叠':'展开'} ${node.name}`" @click="toggleExpand(node.path)"><ChevronDown v-if="expanded.has(node.path)" :size="15"/><ChevronRight v-else :size="15"/></button><span v-else class="file-tree-spacer"/><component :is="node.index===null ? expanded.has(node.path)?FolderOpen:Folder : File" :size="16"/><button class="file-name-button" @click="selectFile($event,node)" :title="node.path" @dblclick="emit('rename',node.path,node.name)">{{ flat || query ? node.path : node.name }}</button></div></div></template><span v-else-if="column.key==='length'" class="numeric">{{ bytes(aggregates.get(node.path)?.length) }}</span><span v-else-if="column.key==='progress'">{{ percent(aggregates.get(node.path)?.length?aggregates.get(node.path).done/aggregates.get(node.path).length:1) }}</span><AppCheckbox v-else-if="column.key==='wanted'" :model-value="aggregates.get(node.path)?.wanted===aggregates.get(node.path)?.count" :indeterminate="aggregates.get(node.path)?.wanted>0&&aggregates.get(node.path)?.wanted<aggregates.get(node.path)?.count" :disabled="busy||!task.fileStats" :aria-label="`下载 ${node.path}`" @update:model-value="change($event?'files-wanted':'files-unwanted',indexes(node))"/><AppSelect v-else-if="column.key==='priority'" :model-value="aggregates.get(node.path)?.priority" :disabled="busy||!task.fileStats" :aria-label="`${node.path} 的优先级`" @update:model-value="change($event===1?'priority-high':$event===-1?'priority-low':'priority-normal',indexes(node))"><AppOption :value="1" label="高"/><AppOption :value="0" label="普通"/><AppOption :value="-1" label="低"/><AppOption :value="2" label="混合" disabled/></AppSelect></td><td><button class="file-tree-expand" :disabled="busy" :aria-label="`重命名 ${node.path}`" @click="emit('rename',node.path,node.name)"><PenLine :size="16"/></button></td></tr>
   <tr v-if="!rows.length"><td :colspan="columns.length+2" class="detail-empty">{{ loading ? '正在读取文件…' : '没有符合条件的文件' }}</td></tr>
  </tbody></table><button v-if="rows.length>displayLimit" class="load-more-files" @click="displayLimit+=200">继续显示 {{ Math.min(200,rows.length-displayLimit) }} 项（共 {{ rows.length }} 项）</button></div></ContextMenuTrigger>
 <ContextMenuPortal><ContextMenuContent class="download-context-menu" :style="{zIndex:menuLayer}"><ContextMenuItem v-for="[key,title] in [['files-wanted','下载文件'],['files-unwanted','跳过文件'],['priority-high','高优先级'],['priority-normal','普通优先级'],['priority-low','低优先级']]" :key="key" class="download-context-item" :disabled="busy||!contextIndexes.length" @select="change(key,contextIndexes)">{{ title }}</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" :disabled="busy||!contextNode" @select="emit('rename',contextNode.path,contextNode.name)">重命名</ContextMenuItem><ContextMenuItem class="download-context-item" :disabled="!contextNode" @select="copyPath()">复制远程完整路径</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" @select="expandAll">展开全部目录</ContextMenuItem><ContextMenuItem class="download-context-item" @select="expanded=new Set()">折叠全部目录</ContextMenuItem><ContextMenuItem class="download-context-item" @select="flat=!flat">{{ flat?'切换为目录树':'平铺全部文件' }}</ContextMenuItem><ContextMenuItem class="download-context-item" @select="searchInput?.focus()">搜索文件</ContextMenuItem></ContextMenuContent></ContextMenuPortal></ContextMenuRoot>
 </div>
</template>
