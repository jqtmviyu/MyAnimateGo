import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {computed, nextTick, reactive, ref, watch} from 'vue'
import {normalizeAnibt, normalizeAnibtGroups, anibtSubscribedIds, anibtSeasonOptions} from '../src/lib/anibt-catalog.js'
const script=readFileSync(new URL('../src/view/home/MikanLibraryView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'')
const props=reactive({source:'anibt',subscriptions:[]})
let directoryArgs, groupID
const context={computed,nextTick,ref,watch,onMounted:()=>{},defineProps:()=>props,defineEmits:()=>()=>{},defineExpose:()=>{},normalizeAnibt,normalizeAnibtGroups,anibtSubscribedIds,anibtSeasonOptions,http:{
 aniBT:async(...args)=>{directoryArgs=args;return {data:{currentSeason:'2026秋',requestedSeason:args[0] || '2026秋',availableSeasons:['2025秋','2026冬','2026春','2026夏','2026秋','2027冬','2027春','2027夏'],byWeekday:[{weekday:1,weekdayLabel:'周一',animes:[{bgmId:123,title:{primary:'作品'},rssReleaseCount:0}]}]}}},
 aniBTGroup:async id=>{groupID=id;return {data:[{bgmId:'123',name:'字幕组',rss:'https://anibt.net/rss/anime.xml?bgmId=123&groupSlug=group',items:[{title:'资源',publishedAt:1786863045049}],groupRegex:{regexList:[],tags:[]}}]}}
}}
const view=new Function(...Object.keys(context),script+'\nreturn {load,weeks,orderedWeeks,ensureGroups,groups,selected,chooseGroups,beginSubscription,batchRef,batchItems,flowStage,season,query,seasons,currentSchedule}')(...Object.values(context))
view.season.value='2026秋';await view.load({refresh:true})
assert.deepEqual(directoryArgs,['2026秋','','',{refresh:true}])
assert.deepEqual(view.seasons.value.map(item=>item.seasonLabel),['2026夏','2026秋','2027冬'],'季度选择与官网一致，只显示相邻三季')
const item=view.weeks.value[0].items[0]
assert.equal(item.title,'作品');assert.equal(item.rssReleaseCount,0)
await view.ensureGroups(item)
assert.equal(groupID,'123');assert.equal(view.groups.value[item.url][0].label,'字幕组')
props.subscriptions=[{bgmUrl:'https://bgm.tv/subject/123',url:'https://mikanani.me/RSS/Bangumi?bangumiId=2'}]
assert.equal(view.orderedWeeks.value[0].items[0].exists,true)
props.subscriptions=[];assert.equal(view.orderedWeeks.value[0].items[0].exists,false)
view.selected.value=item
await view.chooseGroups(view.groups.value[item.url])
let sources
view.batchRef.value={open:async values=>{sources=values.map(value=>JSON.parse(value))}}
await view.beginSubscription(view.groups.value[item.url])
assert.equal(view.flowStage.value,'confirm');assert.equal(sources[0].bgmId,'123');assert.equal(sources[0].label,'字幕组');assert.equal(sources[0].displayTitle,'作品')
console.log('Anibt 目录刷新、字幕组实际加载、订阅勾选同步与确认流程通过')

view.season.value='2026夏'
await view.load()
assert.equal(view.season.value,'2026夏','请求上一季后保持选中季度')
assert.equal(view.currentSchedule.value,false,'上一季不显示今日放送')
assert.deepEqual(view.seasons.value.map(item=>item.seasonLabel),['2026夏','2026秋','2027冬'],'切换季度后仍以真实当季为中心')
view.season.value=''
await view.load()
assert.equal(view.season.value,'2026秋','初始选择使用明确当季值')
console.log('Anibt 切换季度与当前季度选中状态通过')
