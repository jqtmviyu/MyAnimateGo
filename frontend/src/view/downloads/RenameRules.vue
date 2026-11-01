<script setup>
import {useId} from 'vue'
const id=useId()
defineProps({rules:Object,directories:Array,disabled:Boolean})
</script>
<template>
 <fieldset class="rename-fields" :disabled="disabled">
  <div class="rename-main-fields" :class="rules.mode">
   <template v-if="rules.mode==='episode'"><div><label :for="`${id}-rename-title`">番剧名称</label><AppInput :id="`${id}-rename-title`" v-model="rules.title"/></div><div><label :for="`${id}-rename-season`">季度</label><input :id="`${id}-rename-season`" v-model.number="rules.season" type="number" min="0" max="999"></div><div><label :for="`${id}-rename-pattern`">集数匹配</label><AppInput :id="`${id}-rename-pattern`" v-model="rules.pattern" class="rename-code"/></div></template>
   <template v-else><div><label :for="`${id}-rename-pattern`">匹配内容</label><AppInput :id="`${id}-rename-pattern`" v-model="rules.pattern" class="rename-code"/></div><div><label :for="`${id}-rename-replace`">替换为</label><AppInput :id="`${id}-rename-replace`" v-model="rules.replacement" class="rename-code" placeholder="例如：星海旅人 S01E${1}"/></div></template>
  </div>
  <p class="rename-field-help">{{ rules.mode==='episode'?'括号捕获集数，生成「番剧名 S01E01」；扩展名与字幕语言后缀保留。':'使用 $1 或 ${1} 引用捕获组，$0 代表完整匹配。' }}</p>
  <details class="rename-options"><summary>更多选项</summary><div class="rename-options-body">
   <div class="rename-directory-field"><label>处理目录</label><AppSelect v-model="rules.directory" aria-label="处理目录"><AppOption value="" label="所有文件夹"/><AppOption v-for="dir in directories" :key="dir" :value="dir" :label="dir"/></AppSelect></div>
   <div v-if="rules.mode==='episode'" class="rename-directory-field"><label :for="`${id}-rename-season-pattern`">从目录提取季度</label><AppInput :id="`${id}-rename-season-pattern`" v-model="rules.seasonPattern" class="rename-code"/></div>
   <div class="rename-option-toggles"><AppCheckbox v-model="rules.recursive" :disabled="!rules.directory">包含子目录</AppCheckbox><AppCheckbox v-model="rules.mediaOnly">仅视频和字幕</AppCheckbox><AppCheckbox v-model="rules.ignoreCase">忽略大小写</AppCheckbox><AppCheckbox v-if="rules.mode==='replace'" v-model="rules.preserveExtension">保留扩展名</AppCheckbox></div>
  </div></details>
 </fieldset>
</template>
