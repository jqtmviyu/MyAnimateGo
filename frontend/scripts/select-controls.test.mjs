import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {createSSRApp, h} from 'vue'
import {renderToString} from 'vue/server-renderer'

const server = await createServer({root:new URL('..',import.meta.url).pathname, server:{middlewareMode:true}, appType:'custom', logLevel:'error'})
try {
  const {AppSelect, AppOption} = await server.ssrLoadModule('/src/components/app/controls/choices.js')
  const render = modelValue => renderToString(createSSRApp({render:()=>h(AppSelect,{modelValue,multiple:true,clearable:true,placeholder:'全部级别'},{default:()=>[
    h(AppOption,{value:'INFO',label:'信息'}),h(AppOption,{value:'WARN',label:'警告'}),h(AppOption,{value:'ERROR',label:'错误'}),
  ]})}))
  const two = await render(['INFO','ERROR'])
  assert.ok(two.includes('信息、错误'),'多选摘要显示实际选中名称')
  assert.ok(two.includes('pr-[60px]'),'为清空与箭头保留独立空间')
  const three = await render(['INFO','WARN','ERROR'])
  assert.ok(three.includes('信息 +2'))
  assert.ok(three.includes('title="信息、警告、错误"'))
  const empty = await render([])
  assert.ok(empty.includes('全部级别'))
  assert.ok(!empty.includes('aria-label="清空选择"'))
  console.log('选择器名称摘要、完整提示和清空空间通过')
} finally {
  await server.close()
}
