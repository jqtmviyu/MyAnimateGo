<script setup>
import {computed, ref, watch} from 'vue'
import RpcFieldForm from './RpcFieldForm.vue'
const props=defineProps({task:{type:Object,required:true},fields:{type:Array,default:()=>[]},busy:Boolean,rpcVersion:Number,multiple:Boolean,speedUnit:String})
const emit=defineEmits(['update'])
const draft=ref({}),base=ref({}),editing=ref(false)
const supported=computed(()=>props.fields.filter(f=>props.task[f.key]!==undefined && (!f.minRPC || props.rpcVersion>=f.minRPC)))
watch(()=>props.task,task=>{if(!editing.value){base.value=Object.fromEntries(supported.value.map(f=>[f.key,task[f.key]]));draft.value={...base.value}}},{immediate:true})
watch(supported,()=>{if(!editing.value){base.value=Object.fromEntries(supported.value.map(f=>[f.key,props.task[f.key]]));draft.value={...base.value}}})
const changes=computed(()=>Object.fromEntries(supported.value.filter(f=>JSON.stringify(draft.value[f.key])!==JSON.stringify(base.value[f.key])).map(f=>[f.key,draft.value[f.key]])))
function save(){if(Object.keys(changes.value).length)emit('update',changes.value,()=>{editing.value=false;base.value={...draft.value}})}
</script>
<template><form class="torrent-properties" @submit.prevent="save"><p class="download-help">{{ multiple ? '以第一个选中任务的设置为起点；只将你修改的字段应用到全部所选任务。' : '设置仅作用于当前任务，保存时只提交修改的字段。' }}</p><RpcFieldForm :model-value="draft" :fields="supported" :disabled="busy" :speed-unit="speedUnit" @update:model-value="draft=$event;editing=true"/><AppButton type="primary" native-type="submit" :loading="busy" :disabled="!Object.keys(changes).length">保存任务属性</AppButton></form></template>
