<script setup>
import {onBeforeUnmount,ref,watch} from 'vue'
import api from '@/js/api'
import {notify} from '@/lib/feedback'
const props=defineProps({modelValue:Boolean,disabled:Boolean})
const emit=defineEmits(['update:modelValue','changed'])
const rows=ref([]),total=ref(0),page=ref(1),view=ref('active'),query=ref(''),ip=ref(''),loading=ref(false),busy=ref(false)
let serial=0,timer
const date=value=>value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—'
async function load(){const generation=++serial;loading.value=true;try{const {data}=await api.get(`api/peerFilterBans?view=${view.value}&page=${page.value}&q=${encodeURIComponent(query.value)}`);if(generation===serial){rows.value=data.rows;total.value=data.total;page.value=data.page}}catch{}finally{if(generation===serial)loading.value=false}}
watch(()=>props.modelValue,open=>{if(open){page.value=1;load()}})
watch(page,()=>{if(props.modelValue)load()})
watch(view,()=>{if(page.value!==1)page.value=1;else if(props.modelValue)load()})
watch(query,()=>{clearTimeout(timer);timer=setTimeout(()=>{if(!props.modelValue)return;if(page.value!==1)page.value=1;else load()},300)})
onBeforeUnmount(()=>{serial++;clearTimeout(timer)})
async function action(command,address){busy.value=true;try{const {data}=await api.post('api/peerFilterAction',{action:command,ip:address});notify.success(command==='allow'?'已加入允许地址并应用':command==='ban'?'已封禁该地址':'已解除动态封禁，仍命中名称规则时可能再次封禁');ip.value='';emit('changed',data.settings);await load()}catch{}finally{busy.value=false}}
</script>
<template>
 <AppModal :model-value="modelValue" title="节点封禁记录" width="1100px" class="peer-bans-dialog" @update:model-value="emit('update:modelValue',$event)">
  <div class="peer-bans-toolbar"><AppSelect v-model="view" aria-label="封禁记录范围"><AppOption value="active" label="当前动态封禁"/><AppOption value="released" label="最近解除（最多500条）"/></AppSelect><AppInput v-model="query" type="search" aria-label="搜索封禁记录" placeholder="搜索 IP、客户端或原因"/><AppButton :disabled="loading" @click="load">刷新</AppButton></div>
  <div class="peer-bans-table"><table><thead><tr><th>IP / 客户端</th><th>原因</th><th>到期 / 解除时间</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="`${row.ip}:${row.releasedAt||row.firstSeen}`"><td><span>{{ row.ip }}</span><small>{{ row.client||'手动添加' }}</small></td><td>{{ row.reason }}<small v-if="row.sources?.length">同时命中：{{ row.sources.join('、') }}</small></td><td>{{ date(row.releasedAt||row.expiresAt) }}</td><td><div class="peer-ban-actions"><AppButton v-if="view==='active'" text :disabled="busy||disabled" @click="action('release',row.ip)">解除</AppButton><AppButton text :disabled="busy||disabled" @click="action('allow',row.ip)">允许此 IP</AppButton></div></td></tr><tr v-if="!rows.length"><td colspan="4" class="peer-bans-empty">{{ loading?'正在读取…':'没有记录' }}</td></tr></tbody></table></div>
  <p class="settings-help">这里只列出本地识别和手动添加的动态封禁。允许地址优先于上游规则；单独解除动态封禁时，上游命中仍然有效。</p>
  <div class="peer-bans-add"><AppInput v-model.trim="ip" placeholder="填写需要临时封禁的公网 IP" aria-label="手动封禁IP"/><AppButton :disabled="!ip||busy||disabled" @click="action('ban',ip)">添加封禁</AppButton></div>
  <template #footer><span class="settings-help">共 {{ total }} 条</span><AppButton :disabled="page<=1||loading" @click="page--">上一页</AppButton><span>{{ page }}</span><AppButton :disabled="page*100>=total||loading" @click="page++">下一页</AppButton><AppButton @click="emit('update:modelValue',false)">关闭</AppButton></template>
 </AppModal>
</template>
<style scoped>
.peer-bans-toolbar,.peer-bans-add{display:flex;align-items:center;gap:12px;margin-bottom:18px}.peer-bans-toolbar>.app-select{width:220px;flex-shrink:0}.peer-bans-toolbar>.app-input,.peer-bans-add>.app-input{flex:1}.peer-bans-add{margin-top:18px}.peer-bans-table{height:min(46dvh,440px);overflow:auto;border:1px solid var(--border);border-radius:6px}.peer-bans-table table{width:100%;border-collapse:collapse;text-align:left;font-size:14px}.peer-bans-table th{position:sticky;top:0;background:var(--dialog-surface,var(--secondary));font-weight:500;color:var(--muted-foreground)}.peer-bans-table th,.peer-bans-table td{padding:10px 12px;border-bottom:1px solid var(--border)}.peer-bans-table td{vertical-align:middle}.peer-bans-table td>span,.peer-bans-table small{display:block}.peer-bans-table small{margin-top:5px;font-size:13px;color:var(--muted-foreground)}.peer-ban-actions{display:flex;gap:4px;white-space:nowrap}.peer-bans-empty{text-align:center;height:100px;color:var(--muted-foreground)}@media(max-width:700px){.peer-bans-toolbar{flex-wrap:wrap}.peer-bans-table table{min-width:750px}}
</style>
