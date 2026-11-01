<script setup>
import {computed,inject,onMounted,onBeforeUnmount,ref,watch} from 'vue'
import SettingsItem from '@/view/custom/SettingsItem.vue'
import PeerFilterRules from './PeerFilterRules.vue'
import PeerFilterBans from './PeerFilterBans.vue'
import api from '@/js/api'
import {notify} from '@/lib/feedback'
const props=defineProps({config:{type:Object,required:true}})
const draft=ref(null)
const info=ref(null),loading=ref(true),busy=ref(false),failure=ref(''),recordsOpen=ref(false)
const settings=computed(()=>props.config.peerFilter||draft.value)
const saveRevision=inject('settingsSaveRevision',ref(0))
const allowText=computed({get:()=>settings.value?.allow?.join('\n')||'',set:value=>{settings.value.allow=value.split('\n').map(line=>line.trim()).filter(Boolean)}})
const date=value=>value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'尚无记录'
const dirty=computed(()=>info.value&&settings.value&&canonical(info.value.settings)!==canonical(settings.value))
const appliedCount=computed(()=>info.value?.managed&&info.value?.appliedAt&&!info.value?.conflict?info.value.remoteSize.toLocaleString('zh-CN'):'—')
const statusLabel=computed(()=>{
 const value=info.value
 if(!value)return '正在读取'
 if(value.busy||busy.value)return '正在处理'
 if(value.conflict)return '配置已变化'
 if(value.needsMigration)return '尚未接管'
 if(!value.settings.enabled)return '已关闭'
 if(value.error)return '需要处理'
 if(!value.managed||value.revision!==value.applied)return '等待应用'
 return '已应用'
})
const hasProblem=computed(()=>Boolean(info.value?.conflict||info.value?.error))
const canonical=value=>JSON.stringify(value,(_,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.entries(v).sort(([a],[b])=>a.localeCompare(b))):v)
watch(settings,value=>{if(value&&!props.config.peerFilter&&info.value&&canonical(value)!==canonical(info.value.settings))props.config.peerFilter=value},{deep:true})
let timer,active=true,serial=0,controller
async function load(initial=false){
 clearTimeout(timer);const generation=++serial;controller?.abort();controller=new AbortController()
 try{const {data}=await api.get('api/peerFilter',{silent:!initial,signal:controller.signal});if(!active||generation!==serial)return;info.value=data;failure.value='';if(!settings.value)draft.value=structuredClone(data.settings)}
 catch(error){if(error.name!=='AbortError'&&generation===serial)failure.value=error.message}
 finally{if(generation===serial){loading.value=false;clearTimeout(timer);if(active)timer=setTimeout(()=>load(),5000)}}
}
watch(saveRevision,()=>load())
onMounted(()=>load(true))
onBeforeUnmount(()=>{active=false;serial++;controller?.abort();clearTimeout(timer)})
function recordsChanged(value){props.config.peerFilter=value;load()}
function setSource(id,enabled){settings.value.sources=enabled?[...new Set([...settings.value.sources,id])]:settings.value.sources.filter(value=>value!==id)}
async function action(command){
 if(busy.value)return;busy.value=true
 try{const saved=(await api.get('api/peerFilter')).data;if(saved.needsMigration||canonical(saved.settings)!==canonical(settings.value)){notify.info('请先点击页面右上角保存，再执行操作');return};const {data}=await api.post('api/peerFilterAction',{action:command});info.value=data;notify.success(command==='scan'?'节点扫描和名单应用完成':'规则与名单已应用')}
 catch{}finally{busy.value=false;await load()}
}
</script>
<template>
 <section class="settings-section peer-filter-settings" aria-labelledby="peer-filter-heading">
  <h2 id="peer-filter-heading" class="sr-only">节点过滤</h2>
  <p v-if="loading" class="settings-help" role="status">正在读取节点过滤设置…</p>
  <p v-if="failure" class="peer-filter-error" role="status">{{ failure }} <AppButton :disabled="loading" text @click="load(true)">重新读取</AppButton></p>
  <template v-if="settings">
   <section class="peer-overview" aria-label="过滤运行状态">
    <div class="peer-overview-heading"><h3>运行状态</h3><span class="peer-status" :class="{'peer-status-error':hasProblem,'peer-status-active':statusLabel==='已应用'}" role="status">{{ statusLabel }}</span></div>
    <div class="peer-metrics">
     <div><span>Transmission 已加载规则</span><strong>{{ appliedCount }}<small v-if="appliedCount!=='—'"> 条</small></strong><p class="settings-help">{{ info?.appliedAt ? `最近应用：${date(info.appliedAt)}` : '成功应用后显示下载器返回的实际数量' }}</p></div>
     <div><span>动态封禁</span><strong>{{ (info?.dynamicCount??0).toLocaleString('zh-CN') }}<small> 个 IP</small></strong><p class="settings-help">本地识别与手动封禁的有效地址</p></div>
    </div>
    <p v-if="info?.revision" class="settings-help peer-feed-count">当前提供给 Transmission 的名单：{{ info.rangeCount.toLocaleString('zh-CN') }} 段地址范围<span v-if="info.managed&&info.revision!==info.applied">，有更新待应用</span>。</p>
    <div class="peer-filter-actions">
     <AppButton type="primary" :loading="busy" :disabled="loading||info?.busy||!settings.enabled" @click="action('refresh')">更新规则并应用</AppButton>
     <AppButton :disabled="busy||info?.busy||!settings.enabled" @click="action('apply')">重新应用名单</AppButton>
     <AppButton :disabled="busy||info?.busy||!settings.enabled||!settings.identify" @click="action('scan')">立即扫描</AppButton>
     <AppButton :disabled="loading" @click="recordsOpen=true">封禁记录</AppButton>
    </div>
    <p v-if="dirty" class="settings-help peer-notice" role="status">配置有未保存的修改，请先点击右上角保存，再执行更新或扫描。</p>
    <p v-if="info?.paused" class="settings-help peer-notice">自动任务已暂停：规则更新和节点扫描暂停，已应用的名单继续生效，仍可手动操作。</p>
    <p v-if="info?.needsMigration&&info?.legacyEnabled" class="settings-help peer-notice">现有黑名单保持生效。填写 MyAnimeGo 地址并保存后，由节点过滤接管。</p>
    <p v-if="info?.capacityReached" class="peer-filter-error">动态封禁已达到容量上限，请清理记录或调整容量。</p>
    <p v-if="info?.error" class="peer-filter-error" role="status">{{ info.error }}</p>
    <details class="peer-runtime-details">
     <summary>运行详情</summary>
     <dl class="peer-runtime-meta"><div><dt>Transmission 版本</dt><dd>{{ info?.version||'应用时检查，需要 4.1 或更新版本' }}</dd></div><div><dt>最近扫描</dt><dd>{{ date(info?.lastScan) }}</dd></div><div><dt>最近更新来源</dt><dd>{{ date(info?.lastRefresh) }}</dd></div></dl>
     <div v-for="source in info?.sources?.filter(item=>item.enabled)" :key="source.id" class="peer-source-state"><span>{{ source.name }} · {{ source.ready?`${source.count.toLocaleString('zh-CN')} 段`:'尚未就绪' }}</span><span class="settings-help">最近成功：{{ date(source.succeededAt) }}</span><span v-if="source.error" class="peer-filter-error">{{ source.error }}{{ source.ready?'；继续使用上一版规则':'' }}</span></div>
    </details>
   </section>

   <section class="peer-config-section" aria-labelledby="peer-basic-heading">
    <h3 id="peer-basic-heading">基本设置</h3>
    <SettingsItem label="启用节点过滤" description="合并上游规则与本地识别结果，由 Transmission 拦截命中的节点。"><AppSwitch v-model="settings.enabled" :disabled="busy"/></SettingsItem>
    <SettingsItem label="MyAnimeGo 地址"><div class="settings-stack"><AppInput v-model.trim="settings.baseUrl" placeholder="http://192.168.1.10:7789" :disabled="busy" autocomplete="off"/><p class="settings-help">填写 Transmission 所在主机能访问的地址。跨主机部署请使用局域网或 Tailscale 地址。</p></div></SettingsItem>
   </section>

   <section class="peer-config-section" aria-labelledby="peer-sources-heading">
    <h3 id="peer-sources-heading">规则来源</h3>
    <SettingsItem label="公共规则"><div class="settings-stack"><template v-for="source in info?.sources" :key="source.id"><div class="peer-public-source"><AppCheckbox :model-value="settings.sources.includes(source.id)" :disabled="busy" :aria-label="`启用${source.name}`" @update:model-value="value=>setSource(source.id,value)"/><a :href="source.url" target="_blank" rel="noopener noreferrer">{{ source.name }}</a></div></template></div></SettingsItem>
    <SettingsItem label="更新间隔"><AppNumber v-model="settings.refreshMinutes" :min="1" :max="10080" :disabled="busy"><template #suffix>分钟</template></AppNumber></SettingsItem>
   </section>

   <section class="peer-config-section" aria-labelledby="peer-identify-heading">
    <h3 id="peer-identify-heading">客户端识别</h3>
    <SettingsItem label="识别问题客户端" description="按客户端名称匹配，命中后临时封禁当前 IP，未知客户端正常放行。"><AppSwitch v-model="settings.identify" :disabled="busy"/></SettingsItem>
    <SettingsItem v-if="settings.identify" label="扫描间隔"><AppNumber v-model="settings.scanSeconds" :min="2" :max="3600" :disabled="busy"><template #suffix>秒</template></AppNumber></SettingsItem>
    <SettingsItem label="临时封禁时长"><AppNumber v-model="settings.banHours" :min="1" :max="720" :disabled="busy"><template #suffix>小时</template></AppNumber></SettingsItem>
    <details class="peer-filter-details"><summary>客户端名称规则 <span class="peer-detail-count">{{ settings.rules.filter(rule=>rule.enabled).length }} / {{ settings.rules.length }} 已启用</span></summary><PeerFilterRules v-model="settings.rules" :disabled="busy"/></details>
   </section>

   <section class="peer-config-section" aria-labelledby="peer-allow-heading">
    <h3 id="peer-allow-heading">允许地址与容量</h3>
    <SettingsItem label="允许地址"><div class="settings-stack"><AppInput v-model="allowText" type="textarea" :rows="4" :disabled="busy" aria-label="允许的IP或CIDR" placeholder="每行一个 IP 或 CIDR"/><p class="settings-help">优先于上游规则和动态封禁。保存后从最终名单中扣除。</p></div></SettingsItem>
    <SettingsItem label="动态记录上限"><AppNumber v-model="settings.maxBans" :min="1" :max="100000" :disabled="busy"/></SettingsItem>
   </section>
  </template>
 </section>
 <PeerFilterBans v-model="recordsOpen" :disabled="busy||info?.busy||!settings?.enabled||dirty||info?.needsMigration" @changed="recordsChanged"/>
</template>
<style scoped>
.peer-overview{padding-bottom:24px;border-bottom:1px solid var(--border)}
.peer-overview-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:22px}
.peer-overview h3,.peer-config-section h3{margin:0;font-size:16px;font-weight:600;line-height:1.5;color:var(--foreground)}
.peer-status{display:inline-flex;align-items:center;gap:7px;font-size:14px;color:var(--muted-foreground)}
.peer-status::before{content:'';width:7px;height:7px;border-radius:50%;background:currentColor;flex-shrink:0}
.peer-status-active{color:var(--primary)}.peer-status-error{color:var(--destructive)}
.peer-metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px}
.peer-metrics>div>span{font-size:14px;color:var(--muted-foreground)}
.peer-metrics strong{display:block;margin:6px 0;font-size:28px;font-weight:600;line-height:1.3;color:var(--foreground);font-variant-numeric:tabular-nums}
.peer-metrics small{font-size:14px;font-weight:400}.peer-metrics p{margin:0}
.peer-feed-count{margin:16px 0 0}.peer-filter-actions{display:flex;flex-wrap:wrap;gap:10px;margin:20px 0 0}
.peer-notice{margin:14px 0 0;padding-left:12px;border-left:2px solid var(--border)}
.peer-filter-error{color:var(--destructive);font-size:14px;line-height:1.6;overflow-wrap:anywhere;margin-top:12px}
.peer-runtime-details{margin-top:20px}.peer-runtime-details>summary,.peer-filter-details>summary{cursor:pointer;color:var(--foreground);font-size:14px;line-height:1.6;padding:4px 0}
.peer-runtime-details>summary{color:var(--muted-foreground)}
.peer-runtime-details[open]>summary,.peer-filter-details[open]>summary{margin-bottom:16px}
.peer-runtime-meta{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px 24px;margin:0 0 16px}
.peer-runtime-meta dt{font-size:14px;color:var(--muted-foreground);margin-bottom:5px}.peer-runtime-meta dd{font-size:14px;margin:0;overflow-wrap:anywhere}
.peer-source-state{display:flex;flex-direction:column;gap:5px;border-top:1px solid var(--border);padding:12px 0;font-size:14px}
.peer-config-section{padding:24px 0;border-bottom:1px solid var(--border)}.peer-config-section:last-child{border-bottom:0;padding-bottom:0}.peer-config-section h3{margin-bottom:18px}
.peer-filter-details{padding:4px 0}.peer-detail-count{color:var(--muted-foreground);margin-left:8px;font-size:14px}
.peer-public-source{display:flex;align-items:center;gap:8px;min-height:32px}.peer-public-source :deep(.app-checkbox){flex-shrink:0}.peer-public-source a{font-size:14px;line-height:1.6;color:var(--foreground);text-decoration:underline;text-decoration-color:var(--border);text-underline-offset:3px}.peer-public-source a:hover{color:var(--primary);text-decoration-color:currentColor}.peer-public-source a:focus-visible{outline:2px solid var(--ring);outline-offset:3px;border-radius:2px}
.settings-help a{text-decoration:underline;text-underline-offset:3px}
@media(max-width:600px){.peer-metrics,.peer-runtime-meta{grid-template-columns:1fr;gap:20px}.peer-overview-heading{margin-bottom:18px}}
</style>
