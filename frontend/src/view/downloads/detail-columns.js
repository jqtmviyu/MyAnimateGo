import {bytes,percent,speed,dateTime,trackerState} from './transmission'
export const trackerColumns=[
 {key:'announce',label:'Tracker',required:true,width:360,minWidth:200},
 {key:'announceState',label:'状态',width:180,format:(_,row)=>trackerState(row)},
 {key:'seederCount',label:'做种节点',format:value=>value<0?'—':value},
 {key:'leecherCount',label:'下载节点',format:value=>value<0?'—':value},
 {key:'lastAnnounceTime',label:'最近通告',width:180,format:dateTime},
 {key:'nextAnnounceTime',label:'下次通告',width:180,format:dateTime},
 {key:'downloadCount',label:'下载次数',format:value=>value<0?'—':value},
 {key:'lastScrapeResult',label:'Scrape 结果',width:200},
]
export const peerColumns=[
 {key:'address',label:'地址',required:true,width:180},{key:'port',label:'端口',width:90},
 {key:'clientName',label:'客户端',width:180},{key:'progress',label:'进度',format:percent},
 {key:'rateToClient',label:'下载速度',format:speed},{key:'rateToPeer',label:'上传速度',format:speed},
 {key:'flagStr',label:'协议标志'},
 {key:'isEncrypted',label:'加密',format:value=>value?'是':'否'},
 {key:'isIncoming',label:'连接方向',format:value=>value?'传入':'传出'},
 {key:'isUTP',label:'协议',format:value=>value?'µTP':'TCP'},
 {key:'source',label:'来源',format:(_,row)=>row.flagStr?.includes('X')?'PEX':row.flagStr?.includes('H')?'DHT':'Tracker / 缓存'},
 {key:'clientIsChoked',label:'下载状态',format:value=>value?'被阻塞':'可下载'},
 {key:'peerIsChoked',label:'上传状态',format:value=>value?'被阻塞':'可上传'},
]
