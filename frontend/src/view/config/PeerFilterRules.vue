<script setup>
import {ref,watch} from 'vue'
import {Plus, Trash2} from '@lucide/vue'
import api from '@/js/api'
import {notify} from '@/lib/feedback'
const props=defineProps({modelValue:{type:Array,default:()=>[]},disabled:Boolean})
const emit=defineEmits(['update:modelValue'])
const importing=ref(false),text=ref(''),busy=ref(false),sample=ref(''),preview=ref(null)
watch([sample,()=>props.modelValue],()=>{preview.value=null},{deep:true})
const newID=()=>`rule-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,10)}`
const methods=[['CONTAINS','包含'],['STARTS_WITH','开头为'],['EQUALS','等于'],['REGEX','正则']]
function add(){emit('update:modelValue',[...props.modelValue,{id:newID(),enabled:false,method:'CONTAINS',content:'',ignoreCase:true,source:'自定义'}])}
async function importRules(){busy.value=true;try{const {data}=await api.post('api/peerFilterAction',{action:'import',text:text.value});emit('update:modelValue',[...props.modelValue,...data.map(rule=>({...rule,id:newID()}))]);importing.value=false;text.value='';notify.success('规则已导入，请核对内容并启用后保存')}catch{}finally{busy.value=false}}
async function testRules(){try{preview.value=(await api.post('api/peerFilterAction',{action:'preview',name:sample.value,rules:props.modelValue})).data}catch{preview.value=null}}
</script>
<template>
 <div class="peer-rule-editor">
  <div v-for="(rule,index) in modelValue" :key="rule.id" class="peer-rule-row">
   <AppCheckbox v-model="rule.enabled" :disabled="disabled" :aria-label="`启用第${index+1}条名称规则`"/>
   <AppSelect v-model="rule.method" :disabled="disabled" :aria-label="`第${index+1}条匹配方式`"><AppOption v-for="[value,label] in methods" :key="value" :value="value" :label="label"/></AppSelect>
   <AppInput v-model="rule.content" :disabled="disabled" :aria-label="`第${index+1}条匹配内容`" placeholder="客户端名称或正则"/>
   <AppCheckbox v-model="rule.ignoreCase" :disabled="disabled">忽略大小写</AppCheckbox>
   <AppButton :disabled="disabled" text :aria-label="`删除第${index+1}条名称规则`" @click="emit('update:modelValue',modelValue.filter(item=>item.id!==rule.id))"><Trash2 :size="16"/></AppButton>
  </div>
  <p v-if="!modelValue.length" class="settings-help">尚未添加名称规则。</p>
  <div class="peer-filter-actions"><AppButton :disabled="disabled||modelValue.length>=100" @click="add"><Plus :size="15"/>添加规则</AppButton><AppButton :disabled="disabled" @click="importing=true">导入 PBH 名称规则</AppButton></div>
  <div class="peer-rule-preview"><AppInput v-model="sample" aria-label="预览客户端名称" placeholder="输入客户端名称，检查当前启用的规则"/><AppButton :disabled="!sample||disabled" @click="testRules">检查匹配</AppButton></div>
  <p v-if="preview" class="settings-help" role="status">{{ preview.length?`命中 ${preview.length} 条规则`:'未命中当前启用的规则' }}</p>
  <p class="settings-help">新增和导入规则默认关闭。正则使用 Go / RE2；不支持的方法或表达式会提示。规则保存后生效。</p>
 </div>
 <AppModal v-model="importing" title="导入 PBH 客户端名称规则" width="720px"><div class="settings-stack"><p class="settings-help">粘贴 client-name 规则中的 JSON matcher，每行一条；支持等于、包含、前缀和正则。请先核对内容再启用。</p><AppInput v-model="text" type="textarea" :rows="9" aria-label="PBH名称规则JSON" placeholder='{"method":"CONTAINS","content":"Xunlei"}'/></div><template #footer><AppButton @click="importing=false">取消</AppButton><AppButton type="primary" :loading="busy" :disabled="!text.trim()" @click="importRules">导入</AppButton></template></AppModal>
</template>
<style scoped>
.peer-rule-editor{width:100%;min-width:0}.peer-rule-row{display:grid;grid-template-columns:26px 115px minmax(140px,1fr) auto 36px;gap:10px;align-items:center;margin-bottom:12px}.peer-rule-row :deep(.app-checkbox){align-items:center;margin:0}.peer-filter-actions,.peer-rule-preview{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-top:14px}.peer-rule-preview>.app-input{flex:1;min-width:180px}@media(max-width:700px){.peer-rule-row{grid-template-columns:26px 100px minmax(0,1fr) 36px}.peer-rule-row>.app-checkbox:nth-last-child(2){grid-column:2/4;grid-row:2}.peer-rule-row>button:last-child{grid-column:4;grid-row:1}}
</style>
