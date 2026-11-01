<script setup>
import {useId} from 'vue'
const prefix=useId()
const props=defineProps({fields:{type:Array,default:()=>[]},modelValue:{type:Object,required:true},disabled:Boolean,speedUnit:{type:String,default:'kB/s'}})
const emit=defineEmits(['update:modelValue'])
const set=(key,value)=>emit('update:modelValue',{...props.modelValue,[key]:value})
function time(value) {return `${String(Math.floor((value||0)/60)).padStart(2,'0')}:${String((value||0)%60).padStart(2,'0')}`}
function setTime(key,value) {if(/^\d\d:\d\d$/.test(value)){const [h,m]=value.split(':').map(Number);set(key,h*60+m)}}
const days=['日','一','二','三','四','五','六']
</script>
<template>
  <div class="rpc-fields">
    <div v-for="field in fields" :key="field.key" class="rpc-field" :class="{'rpc-field-check':field.kind==='bool'}">
      <AppCheckbox v-if="field.kind==='bool'" :model-value="modelValue[field.key]" :disabled="disabled" @update:model-value="set(field.key,$event)">{{ field.label }}</AppCheckbox>
      <template v-else>
        <label :for="`${prefix}-${field.key}`">{{ field.label }}</label>
        <AppSelect v-if="field.kind==='enum'" :model-value="modelValue[field.key]" :aria-label="field.label" :disabled="disabled" @update:model-value="set(field.key,$event)"><AppOption v-for="option in field.options" :key="option.value" :value="option.value" :label="option.label"/></AppSelect>
        <div v-else-if="field.kind==='days'" class="rpc-days"><AppCheckbox v-for="(day,index) in days" :key="day" :model-value="Boolean(modelValue[field.key] & (1<<index))" :disabled="disabled" @update:model-value="checked=>set(field.key,checked ? modelValue[field.key] | (1<<index) : modelValue[field.key] & ~(1<<index))">周{{ day }}</AppCheckbox></div>
        <input v-else-if="field.kind==='time'" :id="`${prefix}-${field.key}`" class="rpc-time" type="time" :value="time(modelValue[field.key])" :disabled="disabled" @input="setTime(field.key,$event.target.value)">
        <AppInput v-else-if="['string','text'].includes(field.kind)" :id="`${prefix}-${field.key}`" :model-value="modelValue[field.key]" :disabled="disabled" @update:model-value="set(field.key,$event)"/>
        <div v-else class="rpc-number"><input :id="`${prefix}-${field.key}`" type="number" :min="field.kind==='port' ? 1 : 0" :max="field.kind==='port' ? 65535 : 2147483647" :step="field.kind==='number' ? .05 : 1" :value="modelValue[field.key]" :disabled="disabled" @input="($event.target.value!=='' && $event.target.validity.valid) && set(field.key,Number($event.target.value))"><span v-if="field.unit">{{ field.unit==='kB/s' ? speedUnit : field.unit }}</span></div>
      </template>
    </div>
  </div>
</template>
