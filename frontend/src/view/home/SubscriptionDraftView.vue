<script setup>
import {ref} from 'vue'
import {ChevronDown, ChevronUp, Eye, Plus} from '@lucide/vue'
import AniView from './AniView.vue'
import PreviewView from './PreviewView.vue'

const props = defineProps({ani: Object})
const emit = defineEmits(['confirm'])
const advanced = ref(false)
const busy = ref(false)
const previewRef = ref()
const confirm = () => {
  busy.value = true
  emit('confirm', () => { busy.value = false })
}
</script>
<template>
  <div class="subscription-draft">
    <PreviewView ref="previewRef" :ani="props.ani"/>
    <button class="draft-advanced-toggle" :aria-expanded="advanced" @click="advanced = !advanced">{{ advanced ? '返回常用设置' : '高级设置' }}<ChevronUp v-if="advanced" :size="15"/><ChevronDown v-else :size="15"/></button>
    <AniView v-if="advanced" :ani="props.ani" confirm-text="添加订阅" @callback="emit('confirm', $event)"/>
    <form v-else @submit.prevent="confirm">
      <label class="draft-field">订阅名称<AppInput v-model.trim="props.ani.title" aria-label="订阅名称" required/></label>
      <div class="draft-source"><span>主字幕组</span><strong>{{ props.ani.subgroup }}</strong><p v-if="props.ani.standbyRssList?.length">备用 RSS：{{ props.ani.standbyRssList.map(item => item.label).join('、') }}</p></div>
      <div v-if="!props.ani.ova" class="draft-columns"><label class="draft-field">季<AppNumber v-model="props.ani.season" aria-label="订阅季" :min="0"/></label><label class="draft-field">集数偏移<AppNumber v-model="props.ani.offset" aria-label="订阅集数偏移"/></label></div>
      <div class="draft-switches"><label><span>启用订阅</span><AppSwitch v-model="props.ani.enable" aria-label="启用订阅"/></label><label><span>使用全局排除规则</span><AppSwitch v-model="props.ani.globalExclude" aria-label="使用全局排除规则"/></label></div>
      <div class="draft-summary"><p>{{ props.ani.customDownloadPath ? '使用自定义保存位置' : '保存位置沿用全局设置' }}</p><p>{{ props.ani.match?.length ? `已设置 ${props.ani.match.length} 条匹配规则` : '接收所选字幕组的全部资源' }}，可通过预览核对资源与保存位置。</p></div>
      <footer class="draft-footer"><AppButton :disabled="busy" @click="previewRef?.show()"><Eye :size="15"/>预览</AppButton><AppButton native-type="submit" type="primary" :loading="busy"><Plus :size="16"/>添加订阅</AppButton></footer>
    </form>
  </div>
</template>
<style scoped>
.subscription-draft{min-width:0}.draft-advanced-toggle{display:flex;align-items:center;justify-content:flex-end;gap:7px;margin:0 0 16px auto;color:var(--text-secondary);font-size:12px}.draft-advanced-toggle:hover{color:var(--foreground)}.draft-field{display:flex;flex-direction:column;gap:8px;font-size:12px;color:var(--text-secondary);min-width:0}.draft-field :deep(.app-input){width:100%}.draft-source{display:grid;grid-template-columns:74px minmax(0,1fr);gap:8px 12px;padding:18px 0;color:var(--muted-foreground);font-size:12px;border-bottom:1px solid var(--border)}.draft-source strong{font-weight:400;color:var(--foreground);overflow-wrap:anywhere}.draft-source p{grid-column:2;line-height:1.7;font-size:11px}.draft-columns{display:grid;grid-template-columns:1fr 1fr;gap:20px;margin:20px 0}.draft-switches{display:flex;flex-direction:column;gap:17px;padding:18px 0;border-bottom:1px solid var(--border)}.draft-switches :deep([data-slot=switch]){width:42px;height:24px;border:0;box-shadow:none}.draft-switches>label{display:flex;align-items:center;justify-content:space-between;gap:16px;color:var(--text-secondary);font-size:13px}.draft-summary{color:var(--muted-foreground);font-size:11px;line-height:1.8;margin:16px 0 22px}.draft-footer{display:flex;justify-content:flex-end;gap:10px;border-top:1px solid var(--border);padding-top:16px}.draft-footer :deep(.app-button){border-radius:999px;padding-inline:18px}.subscription-draft :deep(.app-field){min-width:0}.subscription-draft :deep(.app-input){max-width:100%}
@media(max-width:500px){.draft-columns{gap:12px}.draft-footer :deep(.app-button){min-height:38px}.draft-source{grid-template-columns:64px minmax(0,1fr)}}
</style>
