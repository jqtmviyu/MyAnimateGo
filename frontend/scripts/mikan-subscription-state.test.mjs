import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {computed, nextTick, reactive, ref, watch} from 'vue'

// 执行目录组件的真实响应式逻辑，模拟列表刷新后传入最新订阅快照。
const file = new URL('../src/view/home/MikanLibraryView.vue', import.meta.url)
const source = readFileSync(file, 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1]
  .replace(/^import .*$/gm, '')
const props = reactive({subscriptions: [{url: 'https://mikanani.me/RSS/Bangumi?bangumiId=4047&subgroupid=426'}]})
const create = new Function('computed', 'nextTick', 'ref', 'watch', 'onMounted', 'defineProps', 'defineEmits', 'defineExpose', 'http', source + '\nreturn {load, weeks, orderedWeeks, selected, groups, selectedGroups, ruleGroups, matchSelection, flowStage, batchRef, batchItems, chooseGroups, beginSelected, beginSubscription, returnToGroups}')
let directoryRequest
const view = create(computed, nextTick, ref, watch, () => {}, () => props, () => () => {}, () => {}, {mikan:async(query,season,options)=>{directoryRequest=options;return {data:{weeks:[]}}}})
view.weeks.value = [{weekLabel: '星期日', items: [{url: 'https://mikanani.me/Home/Bangumi/4047', exists: true}]}]
assert.equal(view.orderedWeeks.value[0].items[0].exists, true)
props.subscriptions = []
assert.equal(view.orderedWeeks.value[0].items[0].exists, false, '删除后，缓存目录的已订阅勾选应立即消失')
props.subscriptions = [{url: 'https://mikanani.me/RSS/Bangumi?bangumiId=4047&subgroupid=99', enable: false}]
assert.equal(view.orderedWeeks.value[0].items[0].exists, true, '同作品其他字幕组或停用订阅仍属于已订阅')
view.weeks.value = [{weekLabel: '星期一', items: [{url: 'https://mikanani.me/Home/Bangumi/4047/', exists: false}]}]
assert.equal(view.orderedWeeks.value[0].items[0].exists, true, '较早的目录响应不能覆盖最新订阅状态')
console.log('Mikan 目录订阅状态同步通过')

const primary = {rss: 'https://mikanani.me/RSS/Bangumi?bangumiId=4047&subgroupid=426', label: '主字幕组'}
const backup = {rss: 'https://mikanani.me/RSS/Bangumi?bangumiId=4047&subgroupid=99', label: '备用字幕组'}
view.selected.value = {url: 'https://mikanani.me/Home/Bangumi/4047', title: '作品'}
view.groups.value[view.selected.value.url] = [backup, primary]
view.selectedGroups.value = [primary.rss, backup.rss]
await view.beginSelected()
assert.equal(view.flowStage.value, 'rules', '点击添加先展示匹配规格，不直接进入解析')
assert.deepEqual(view.ruleGroups.value.map(group => group.label), ['主字幕组', '备用字幕组'])
view.matchSelection.value[primary.rss] = JSON.stringify(['1080[Pp]', '简体'])
let sources
view.batchRef.value = {open: async items => {sources = items.map(item => JSON.parse(item))}}
await view.beginSubscription(view.ruleGroups.value)
assert.equal(view.flowStage.value, 'confirm')
assert.deepEqual(sources[0].match, ['1080[Pp]', '简体'], '所选资源规格随主 RSS 一并提交')
assert.deepEqual(sources[1].match, [], '备用 RSS 默认全部资源')
await view.returnToGroups()
assert.equal(view.flowStage.value, 'groups')
assert.equal(view.matchSelection.value[primary.rss], JSON.stringify(['1080[Pp]', '简体']), '返回列表保留已选规格')
console.log('Mikan 字幕组与匹配规则交互通过')

await view.load({refresh:true,preservePosition:true})
assert.equal(directoryRequest.refresh,true,'手动刷新绕过来源短缓存')
assert.equal(directoryRequest.metadata,false,'首页不额外补全全部作品资料')
console.log('Mikan 首页轻量读取与手动刷新参数通过')
