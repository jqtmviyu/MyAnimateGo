<script setup>
import {computed,inject,ref,watch} from 'vue'
import {ChevronDown,ChevronRight,Folder,Tag} from '@lucide/vue'
import {ContextMenuRoot,ContextMenuTrigger,ContextMenuPortal,ContextMenuContent,ContextMenuItem,ContextMenuSeparator} from 'reka-ui'
import {downloadPreferences} from './table-layout'
import {bytes} from './transmission'
const props=defineProps({tasks:{type:Array,default:()=>[]},filters:Array,status:String,label:String,directory:String})
const emit=defineEmits(['status','label','directory'])
const preferences=inject(downloadPreferences),expanded=ref(new Set()),query=ref(''),contextSection=ref('status')
const sectionNames={status:'状态',directory:'目录',labels:'标签'}
const sections=computed(()=>preferences.value.sidebarOrder.filter(key=>!preferences.value.hiddenSidebar.includes(key)))
const filters=computed(()=>props.filters.filter(f=>f.value==='all'||!preferences.value.hiddenStatuses.includes(f.value)))
watch(filters,value=>{if(!value.some(f=>f.value===props.status))emit('status','all')})
const summary=computed(()=>{
 const labels=new Map(),root={path:'',children:new Map(),count:0,size:0,directCount:0,directSize:0},statuses=Object.fromEntries(props.filters.map(f=>[f.value,{count:0,size:0}]))
 const bump=(map,key,size)=>{const entry=map.get(key)||{count:0,size:0};entry.count++;entry.size+=size;map.set(key,entry)}
 for(const task of props.tasks){
  const size=task.sizeWhenDone||0;root.count++;root.size+=size
  for(const filter of props.filters)if(filter.match(task)){statuses[filter.value].count++;statuses[filter.value].size+=size}
  for(const label of task.labels?.length?task.labels:['\0'])bump(labels,label,size)
  let parent=root,path='';const parts=(task.downloadDir||'').replace(/\\/g,'/').split('/').filter(Boolean)
  for(const part of parts){path+=`/${part}`;if(!parent.children.has(part))parent.children.set(part,{path,name:part,children:new Map(),count:0,size:0,directCount:0,directSize:0});parent=parent.children.get(part);parent.count++;parent.size+=size}
  parent.directCount++;parent.directSize+=size
 }
 return {labels:[...labels].sort(([a],[b])=>a.localeCompare(b,'zh-CN')),root,statuses}
})
const directories=computed(()=>{
 const rows=[],needle=query.value.toLowerCase();let root=summary.value.root
 if(preferences.value.compactDirectories)while(root.children.size===1&&!root.directCount){const next=[...root.children.values()][0];if(!next.children.size)break;root=next}
 function walk(node,depth){for(const child of [...node.children.values()].sort((a,b)=>a.name.localeCompare(b.name,'zh-CN'))){if(!needle||child.path.toLowerCase().includes(needle))rows.push({...child,depth});if(needle||expanded.value.has(child.path))walk(child,depth+1)}}
 if(root.path)rows.push({...root,depth:0});walk(root,root.path?1:0);return rows
})
function expand(path){const next=new Set(expanded.value);next.has(path)?next.delete(path):next.add(path);expanded.value=next}
function toggleSection(key){const before=preferences.value.hiddenSidebar;preferences.value.hiddenSidebar=before.includes(key)?before.filter(k=>k!==key):[...before,key]}
function toggleStatus(key){const before=preferences.value.hiddenStatuses;preferences.value.hiddenStatuses=before.includes(key)?before.filter(k=>k!==key):[...before,key]}
function moveSection(delta){const order=[...preferences.value.sidebarOrder],index=order.indexOf(contextSection.value),target=index+delta;if(target<0||target>=order.length)return;[order[index],order[target]]=[order[target],order[index]];preferences.value.sidebarOrder=order}
</script>
<template>
 <ContextMenuRoot><ContextMenuTrigger as-child><aside class="download-sidebar" aria-label="下载任务分类">
 <section v-for="section in sections" :key="section" @contextmenu="contextSection=section"><h3>{{ sectionNames[section] }}<button v-if="section==='status'&&status!=='all'" @click="emit('status','all')">全部</button><button v-else-if="section==='directory'&&directory" @click="emit('directory','')">清除</button><button v-else-if="section==='labels'&&label" @click="emit('label','')">清除</button></h3>
 <template v-if="section==='status'"><button v-for="item in filters" :key="item.value" class="download-branch" :aria-current="status===item.value?'page':undefined" @click="emit('status',item.value)"><span class="branch-status-dot" :class="`branch-${item.value}`"/><span>{{ item.label }}</span><small>{{ summary.statuses[item.value].count }}</small><small v-if="preferences.showBranchSizes" class="branch-size">{{ bytes(summary.statuses[item.value].size) }}</small></button></template>
 <template v-else-if="section==='directory'"><input v-model="query" class="directory-search" type="search" aria-label="筛选目录分支" placeholder="查找目录"><div v-for="dir in directories" :key="dir.path" class="directory-branch" :style="{paddingLeft:`${dir.depth*14}px`}"><button v-if="dir.children.size" class="branch-expand" :aria-label="`${expanded.has(dir.path)?'折叠':'展开'} ${dir.name}`" @click="expand(dir.path)"><ChevronDown v-if="expanded.has(dir.path)" :size="14"/><ChevronRight v-else :size="14"/></button><span v-else class="branch-expand"/><button class="download-branch" :aria-current="directory===dir.path?'page':undefined" :title="dir.path" @click="emit('directory',directory===dir.path?'':dir.path)"><Folder :size="14"/><span>{{ dir.name }}</span><small>{{ preferences.recursiveDirectories?dir.count:dir.directCount }}</small><small v-if="preferences.showBranchSizes" class="branch-size">{{ bytes(preferences.recursiveDirectories?dir.size:dir.directSize) }}</small></button></div></template>
 <template v-else-if="section==='labels'"><button v-for="[name,count] in summary.labels" :key="name" class="download-branch" :aria-current="label===name?'page':undefined" @click="emit('label',label===name?'':name)"><Tag :size="14"/><span>{{ name==='\0'?'无标签':name }}</span><small>{{ count.count }}</small><small v-if="preferences.showBranchSizes" class="branch-size">{{ bytes(count.size) }}</small></button></template>
 </section></aside></ContextMenuTrigger><ContextMenuPortal><ContextMenuContent class="download-context-menu" style="z-index:90"><div class="download-context-heading">分类显示</div><ContextMenuItem v-for="key in preferences.sidebarOrder" :key="key" class="download-context-item" @select.prevent="toggleSection(key)">{{ preferences.hiddenSidebar.includes(key)?'　':'✓' }} {{ sectionNames[key] }}</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" @select.prevent="moveSection(-1)">当前分类上移</ContextMenuItem><ContextMenuItem class="download-context-item" @select.prevent="moveSection(1)">当前分类下移</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" @select.prevent="preferences.recursiveDirectories=!preferences.recursiveDirectories">{{ preferences.recursiveDirectories?'✓':'　' }} 目录包含子目录任务</ContextMenuItem><ContextMenuItem class="download-context-item" @select.prevent="preferences.compactDirectories=!preferences.compactDirectories">{{ preferences.compactDirectories?'✓':'　' }} 压缩公共上级目录</ContextMenuItem><ContextMenuItem class="download-context-item" @select.prevent="preferences.showBranchSizes=!preferences.showBranchSizes">{{ preferences.showBranchSizes?'✓':'　' }} 显示任务大小</ContextMenuItem><template v-if="contextSection==='status'"><ContextMenuSeparator class="download-context-separator"/><div class="download-context-heading">显示状态项</div><ContextMenuItem v-for="item in props.filters" :key="item.value" class="download-context-item" :disabled="item.value==='all'" @select.prevent="toggleStatus(item.value)">{{ preferences.hiddenStatuses.includes(item.value)?'　':'✓' }} {{ item.label }}</ContextMenuItem></template></ContextMenuContent></ContextMenuPortal></ContextMenuRoot>
</template>
