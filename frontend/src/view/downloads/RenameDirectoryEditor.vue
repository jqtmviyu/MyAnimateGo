<script setup>
import {useId} from 'vue'
const id=useId()
defineProps({name:String,path:String,newPath:String,error:String,busy:Boolean,canSave:Boolean})
const emit=defineEmits(['update:name','save','cancel'])
</script>
<template>
 <form class="rename-inline-directory-editor" @submit.prevent="emit('save')"><div><label :for="id">目录名称</label><AppInput :id="id" :model-value="name" :disabled="busy" :aria-label="`新目录名称 ${path}`" @update:model-value="emit('update:name',$event)"/><AppButton type="primary" :loading="busy" :disabled="!canSave" native-type="submit">保存目录名</AppButton><AppButton :disabled="busy" @click="emit('cancel')">取消</AppButton></div><p>新路径：<span>{{ newPath }}</span></p><p v-if="error" class="rename-directory-error" role="alert">{{ error }}</p></form>
</template>
