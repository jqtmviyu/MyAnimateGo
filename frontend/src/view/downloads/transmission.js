import api from '@/js/api'

export const rpc = async (method, args = {}, options = {}) => {
  const {data} = await api.post('api/transmissionRPC', {method, arguments: args, details: Boolean(options.details), view: options.view || '', freshFiles: Boolean(options.freshFiles), trackers: Boolean(options.trackers), columns: options.columns}, options)
  return data || {}
}
export {statusNames, taskStatus, taskError, trackerState} from './task-state'
import {normalizeLayout} from './table-layout'
import {taskError, trackerState} from './task-state'
export function bytes(value = 0) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const i = Math.min(4, Math.floor(Math.log(value) / Math.log(1024)))
  return `${(value / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][i]}`
}
export const speed = value => `${bytes(value)}/s`
export const percent = value => `${((value || 0) * 100).toFixed(1)}%`
export const ratio = value => value === -2 ? '∞' : value < 0 ? '—' : Number(value || 0).toFixed(2)
export function duration(value) {
  if (value < 0 || !Number.isFinite(value)) return '—'
  if (value < 60) return `${Math.floor(value)} 秒`
  if (value < 3600) return `${Math.floor(value / 60)} 分`
  if (value < 86400) return `${Math.floor(value / 3600)} 时 ${Math.floor(value % 3600 / 60)} 分`
  return `${Math.floor(value / 86400)} 天 ${Math.floor(value % 86400 / 3600)} 时`
}
export const dateTime = value => value > 0 ? new Date(value * 1000).toLocaleString('zh-CN', {hour12: false}) : '—'

export const preferenceDefaults={listInterval:3,detailInterval:5,sessionInterval:60,pageSize:100,detailsDocked:true,detailHeight:320,columns:['name','status','percentDone','sizeWhenDone','rateDownload','rateUpload','eta','uploadRatio'],directories:[],defaultTrackers:'',tables:{},sidebarOrder:['status','directory','labels'],hiddenSidebar:[],hiddenStatuses:[],recursiveDirectories:true,compactDirectories:true,showBranchSizes:false,detailTabs:['overview','files','pieces','trackers','peers','options'],hiddenDetailTabs:[]}
export function readPreferences() {
  try {
    const stored=JSON.parse(localStorage.getItem('transmission-ui') || '{}')
    const normalized=normalizePreferences(stored)
    if(Object.hasOwn(stored,'groupBy')) localStorage.setItem('transmission-ui',JSON.stringify(normalized))
    return normalized
  } catch {return {...preferenceDefaults}}
}
export function normalizePreferences(value={}) {
  const result={...preferenceDefaults}
  for(const key of Object.keys(result)) {
    if(typeof result[key]==='boolean' && typeof value[key]==='boolean') result[key]=value[key]
    else if(typeof result[key]==='number' && Number.isFinite(value[key])) result[key]=Math.max(key==='pageSize'?25:key==='detailHeight'?180:2,Math.min(key==='pageSize'?500:key==='detailHeight'?700:3600,Math.round(value[key])))
    else if(typeof result[key]==='string' && typeof value[key]==='string') result[key]=value[key]
    else if(Array.isArray(result[key]) && Array.isArray(value[key])) result[key]=value[key].filter(v=>typeof v==='string').slice(0,100)
  }
  result.tables={}
  for(const name of ['tasks','files','trackers','peers']) {
    const entry=value.tables?.[name]
    if(entry&&typeof entry==='object')result.tables[name]={order:Array.isArray(entry.order)?entry.order.filter(k=>typeof k==='string'):[],visible:Array.isArray(entry.visible)?entry.visible.filter(k=>typeof k==='string'):undefined,widths:Object.fromEntries(Object.entries(entry.widths||{}).filter(([k,v])=>Number.isFinite(v))),sort:{key:typeof entry.sort?.key==='string'?entry.sort.key:'',descending:Boolean(entry.sort?.descending)}}
  }
  result.columns=result.columns.filter(key=>downloadColumns.some(c=>c.key===key))
  if(!result.columns.includes('name')) result.columns.unshift('name')
  if(result.tables.tasks)result.columns=normalizeLayout(result.tables.tasks,downloadColumns,{visible:result.columns}).visible
  result.sidebarOrder=[...new Set([...result.sidebarOrder.filter(k=>['status','directory','labels'].includes(k)), 'status','directory','labels'])]
  result.hiddenSidebar=result.hiddenSidebar.filter(k=>result.sidebarOrder.includes(k))
  result.detailTabs=[...new Set([...result.detailTabs.filter(k=>['overview','files','pieces','trackers','peers','options'].includes(k)),'overview','files','pieces','trackers','peers','options'])]
  result.hiddenDetailTabs=result.hiddenDetailTabs.filter(k=>k!=='overview')
  return result
}
export const downloadColumns=[
 {key:'name',label:'名称',required:true,width:360,minWidth:220}, {key:'status',label:'状态'}, {key:'percentDone',label:'进度'},
 {key:'sizeWhenDone',label:'选中大小',format:bytes},{key:'totalSize',label:'总大小',format:bytes},{key:'haveValid',label:'已完成',format:bytes},
 {key:'rateDownload',label:'下载速度',format:speed},{key:'rateUpload',label:'上传速度',format:speed},{key:'eta',label:'剩余时间',format:duration},{key:'uploadRatio',label:'分享率',format:ratio},
 {key:'downloadedEver',label:'已下载',format:bytes},{key:'uploadedEver',label:'已上传',format:bytes},{key:'leftUntilDone',label:'剩余大小',format:bytes},
 {key:'addedDate',label:'添加时间',format:dateTime},{key:'doneDate',label:'完成时间',format:dateTime},{key:'activityDate',label:'最近活动',format:dateTime},
 {key:'downloadDir',label:'保存目录'},{key:'labels',label:'标签',format:v=>(v||[]).join('、')},{key:'bandwidthPriority',label:'优先级',format:v=>({[-1]:'低',0:'普通',1:'高'}[v]||'—')},
 {key:'queuePosition',label:'队列',format:v=>Number(v)+1},{key:'group',label:'带宽组'},{key:'secondsSeeding',label:'做种时长',format:duration},
 {key:'peersSendingToUs',label:'下载节点'},{key:'peersGettingFromUs',label:'上传节点'},{key:'metadataPercentComplete',label:'元数据',format:percent},{key:'file-count',label:'文件数'},
 {key:'trackerStats',label:'Tracker',format:v=>[...new Set((v||[]).map(t=>trackerHost(t.announce)))].join('、')},
 {key:'trackerStatus',label:'Tracker 状态',format:(_,task)=>(task.trackerStats||[]).map(trackerState).join('、')},
 {key:'trackerSeeds',label:'做种节点',format:(_,task)=>`${task.peersSendingToUs||0} / ${Math.max(-1,...(task.trackerStats||[]).map(t=>t.seederCount))}`},
 {key:'trackerPeers',label:'下载节点总数',format:(_,task)=>`${task.peersGettingFromUs||0} / ${Math.max(-1,...(task.trackerStats||[]).map(t=>t.leecherCount))}`},
 {key:'errorString',label:'错误信息',format:(_,task)=>taskError(task)},{key:'id',label:'任务 ID'},{key:'isPrivate',label:'私有种子',format:v=>v?'是':'否'},
]
export function trackerHost(url) {try{return new URL(url).hostname}catch{return url || '未知 Tracker'}}
