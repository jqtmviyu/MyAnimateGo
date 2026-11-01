export function selectRows({ids, selected, anchor, id, toggle=false, range=false}) {
  const index=ids.indexOf(id), start=ids.indexOf(anchor)
  if(index<0)return {selected,anchor}
  if(range&&start>=0) {
    const span=ids.slice(Math.min(start,index),Math.max(start,index)+1)
    return {selected:toggle?[...new Set([...selected,...span])]:span,anchor}
  }
  if(toggle) {
    const next=new Set(selected);next.has(id)?next.delete(id):next.add(id)
    return {selected:[...next],anchor:id}
  }
  return {selected:[id],anchor:id}
}
