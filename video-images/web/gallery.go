package main

// galleryHTML 内网画廊看板(自包含 HTML/CSS/JS,无外部依赖)。
// 数据来自 /api/list(config✕桶合并);每 30 秒自动刷新;
// 卡住/从没出图的标红排前;点击可选内网/外网域名打开。
const galleryHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>视频缩略图看板</title>
<style>
  :root{--bg:#0f1216;--card:#1a1f27;--fg:#e6e9ef;--mut:#8b93a3;--ok:#2ec26b;--bad:#ff4d4f;--line:#262c36}
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.4 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif}
  header{position:sticky;top:0;background:var(--bg);border-bottom:1px solid var(--line);padding:12px 16px;z-index:9}
  .row{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
  h1{font-size:16px;margin:0;font-weight:600}
  .stat{color:var(--mut)} .stat b{color:var(--fg)}
  .ok{color:var(--ok)} .bad{color:var(--bad)}
  input,select{background:var(--card);border:1px solid var(--line);color:var(--fg);border-radius:8px;padding:7px 10px;outline:none}
  input{min-width:200px}
  label.sw{color:var(--mut);display:flex;align-items:center;gap:6px;cursor:pointer}
  .grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:12px;padding:16px}
  .card{background:var(--card);border:1px solid var(--line);border-radius:10px;overflow:hidden;transition:.15s}
  .card:hover{transform:translateY(-2px)}
  .card.bad{border-color:var(--bad);box-shadow:0 0 0 1px var(--bad) inset}
  .thumb{display:block;width:100%;aspect-ratio:4/3;object-fit:cover;background:#0a0d11}
  .noimg{display:flex;align-items:center;justify-content:center;width:100%;aspect-ratio:4/3;background:#0a0d11;color:var(--bad);font-size:12px}
  .meta{padding:8px 10px}
  .name{font-weight:600;font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .age{font-size:12px;margin-top:2px}
  .ts{font-size:11px;color:var(--mut);margin-top:1px}
  .dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:4px;vertical-align:middle}
  .empty{color:var(--mut);padding:40px;text-align:center}
</style>
</head>
<body>
<header>
  <div class="row">
    <h1>📹 视频缩略图</h1>
    <span class="stat">共 <b id="total">0</b> 路</span>
    <span class="stat ok">🟢 正常 <b id="okc">0</b></span>
    <span class="stat bad">🔴 卡住/无图 <b id="badc">0</b></span>
    <span class="stat" style="margin-left:auto" id="upd">—</span>
  </div>
  <div class="row" style="margin-top:10px">
    <input id="q" placeholder="🔍 输入文件名筛选…" autocomplete="off">
    <label class="sw">打开方式
      <select id="opendom"><option value="int">内网</option><option value="ext">外网</option></select>
    </label>
    <label class="sw"><input type="checkbox" id="badfirst" checked> 卡住的排前面</label>
    <label class="sw"><input type="checkbox" id="auto" checked> 每 30 秒自动刷新</label>
  </div>
</header>
<div id="grid" class="grid"></div>
<div id="empty" class="empty" hidden>没有匹配的图片</div>
<script>
let DATA=[], STALE=300, NOW=0, PUB='';
const $=s=>document.querySelector(s);
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function human(sec){ if(sec<60)return sec+'秒前'; if(sec<3600)return Math.floor(sec/60)+'分前'; if(sec<86400)return Math.floor(sec/3600)+'小时前'; return Math.floor(sec/86400)+'天前'; }
function fmt(ts){ const d=new Date(ts*1000), p=n=>String(n).padStart(2,'0'); return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds()); }
async function load(){
  try{
    const r=await fetch('/api/list',{cache:'no-store'}); const j=await r.json();
    DATA=j.items||[]; STALE=j.stale_seconds||300; NOW=j.now||Math.floor(Date.now()/1000); PUB=(j.public_base||'').replace(/\/$/,'');
    if(!PUB){ const s=$('#opendom'); if(s.options.length>1) s.remove(1); } // 没配公网域名就只留内网
    $('#upd').textContent='刷新于 '+fmt(NOW);
    render();
  }catch(e){ $('#upd').textContent='刷新失败'; }
}
// 点击打开的完整链接: 外网用 PUB,内网用相对路径
function openHref(name){ return ($('#opendom').value==='ext' && PUB) ? PUB+'/'+encodeURIComponent(name) : '/'+encodeURIComponent(name); }
function render(){
  const q=$('#q').value.trim().toLowerCase();
  const badFirst=$('#badfirst').checked;
  let items=DATA.map(o=>({...o, age:o.has_image?(NOW-o.modified):0, bad:!o.has_image||(NOW-o.modified)>STALE}));
  const okc=items.filter(o=>!o.bad).length;
  $('#total').textContent=items.length; $('#okc').textContent=okc; $('#badc').textContent=items.length-okc;
  if(q) items=items.filter(o=>o.name.toLowerCase().includes(q));
  items.sort((a,b)=> badFirst&&a.bad!==b.bad ? (a.bad?-1:1) : (a.has_image?a.age:1e9)-(b.has_image?b.age:1e9));
  const grid=$('#grid');
  $('#empty').hidden=items.length>0; grid.hidden=items.length===0;
  grid.innerHTML=items.map(o=>{
    const nm=esc(o.name), base=esc(o.name.replace(/\.jpg$/i,''));
    const cls=o.bad?'card bad':'card';
    let media, age;
    if(!o.has_image){
      media='<div class="noimg">⚠ 从未出图</div>';
      age='<div class="age bad"><span class="dot" style="background:var(--bad)"></span>配置了但没图</div>';
    }else{
      media='<a href="'+openHref(o.name)+'" target="_blank"><img class="thumb" loading="lazy" src="/'+encodeURIComponent(o.name)+'?v='+o.modified+'" onerror="this.outerHTML=\'<div class=noimg>⚠ 无图</div>\'"></a>';
      const dot=o.bad?'var(--bad)':'var(--ok)';
      age='<div class="age '+(o.bad?'bad':'ok')+'"><span class="dot" style="background:'+dot+'"></span>'+human(o.age)+'</div><div class="ts">'+fmt(o.modified)+'</div>';
    }
    return '<div class="'+cls+'">'+media+'<div class="meta"><div class="name" title="'+nm+'">'+base+'</div>'+age+'</div></div>';
  }).join('');
}
$('#q').addEventListener('input',render);
$('#badfirst').addEventListener('change',render);
$('#opendom').addEventListener('change',render);
setInterval(()=>{ if($('#auto').checked) load(); },30000);
load();
</script>
</body>
</html>`
