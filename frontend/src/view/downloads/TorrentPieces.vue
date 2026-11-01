<script setup>
import {onBeforeUnmount,onMounted,ref,watch,nextTick} from 'vue'
import {resolvedTheme} from '@/lib/theme'
import {color as accent} from '@/js/global'
const props=defineProps({task:{type:Object,required:true}})
const canvas=ref(null),container=ref(null)
let observer
function paint(){
 const element=canvas.value;if(!element)return
 const width=container.value?.clientWidth || 640,cols=Math.max(1,Math.floor(width/8)),count=props.task.pieceCount || 0,rows=Math.ceil(count/cols)
 const height=Math.max(40,Math.min(800,rows*8)),scale=window.devicePixelRatio || 1
 element.width=width*scale;element.height=height*scale;element.style.height=`${height}px`
 const ctx=element.getContext('2d');ctx.scale(scale,scale);ctx.clearRect(0,0,width,height)
 let bitset='';try{bitset=atob(props.task.pieces || '')}catch{}
 const gridRows=Math.ceil(height/8),bins=Math.min(count,cols*gridRows),perBin=Math.max(1,Math.ceil(count/Math.max(1,bins)))
 const style=getComputedStyle(element)
 const color=style.getPropertyValue('--primary').trim(),partial=style.getPropertyValue('--success').trim(),empty=style.getPropertyValue('--control-border').trim()
 for(let bin=0;bin<bins;bin++){
  const start=bin*perBin,end=Math.min(count,start+perBin);if(start>=count)break
  let have=0;for(let piece=start;piece<end;piece++){if((bitset.charCodeAt(piece>>3)||0)&(128>>(piece%8)))have++}
  ctx.fillStyle=have===end-start ? color : have ? partial : empty;ctx.fillRect((bin%cols)*8,Math.floor(bin/cols)*8,6,6)
 }
}
onMounted(()=>{observer=new ResizeObserver(paint);if(container.value)observer.observe(container.value);paint()})
onBeforeUnmount(()=>observer?.disconnect())
watch([()=>props.task.pieces,()=>props.task.pieceCount,resolvedTheme,accent],()=>nextTick(paint))
</script>
<template><div ref="container" class="torrent-pieces"><p class="download-help">{{ task.pieceCount || 0 }} 个分片 · 绿色为已完成，灰色为未完成；分片较多时按连续区间聚合显示。</p><canvas ref="canvas" role="img" :aria-label="`${task.name} 的分片完成分布`"/><p class="download-help">可用性区块：{{ (task.availability || []).filter(n=>n<0).length }} 个已持有，{{ (task.availability || []).filter(n=>n>0).length }} 个可从节点获取，{{ (task.availability || []).filter(n=>n===0).length }} 个暂无来源。</p></div></template>
