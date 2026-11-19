// Run with HAPPY_DOM_MODULE pointing to an installed happy-dom entry point.
// DOM integration test only: no browser, keychain or SQL Server accessed.
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {fileURLToPath,pathToFileURL} from 'node:url';
import path from 'node:path';
const {Window}=await import(pathToFileURL(process.env.HAPPY_DOM_MODULE).href);
const root=path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const html=await readFile(path.join(root,'ui/index.html'),'utf8');
const js=await readFile(path.join(root,'ui/app.js'),'utf8');
const preferences=new Map(),vault=new Map();let failSave=false;
async function open(){
 const win=new Window({url:'http://127.0.0.1:12345/',settings:{enableJavaScriptEvaluation:true,disableJavaScriptFileLoading:true,disableCSSFileLoading:true}});
 win.document.write(html);
 win.Option=function(text,value){const el=win.document.createElement('option');el.textContent=text;el.value=value;return el;};
 win.setInterval=()=>0; // Automatic list refresh is unrelated to credential tests.
 win.dbxPlugin={ready:Promise.resolve(),context:{},capabilities:{connectionsApi:true,storage:true},
  getConnections:async()=>[{id:'mock',name:'Mock SQL',dbType:'sqlserver'}],
  storage:{get:async k=>preferences.get(k),set:async(k,v)=>preferences.set(k,v),delete:async k=>preferences.delete(k)},
  invoke:async(method,p)=>{
   const endpoint=p.target.endpoint;
   if(method==='agent/token/status')return {supported:true,saved:vault.has(endpoint)};
   if(method==='agent/token/save'){if(failSave)throw Error('mock keychain denied');vault.set(endpoint,p.token);return {supported:true,saved:true};}
   if(method==='agent/token/delete'){vault.delete(endpoint);return {supported:true,saved:false};}
   if(method==='agent/read'){
    assert.equal(p.token||vault.get(endpoint),'mock-secret');
    return {rows:[],identity:{server_name:'MOCK',edition:'Developer',product_version:'16.0',login_name:'mock',is_sysadmin:1}};
   }
   throw Error('unexpected method '+method);
  }};
 win.eval(js);
 await win.happyDOM.waitUntilComplete();
 await new Promise(resolve=>setImmediate(resolve));
 return win;
}
let win=await open();
const $=id=>win.document.getElementById(id);
assert.equal($('remember-token').checked,false);
$('connection').value='mock';$('token').value='mock-secret';$('remember-token').checked=true;
await $('connect').onclick();
assert.equal(vault.size,1,$('notice').textContent);assert.equal($('token').value,'');assert.equal($('remember-token').checked,true);
assert(!JSON.stringify([...preferences]).includes('mock-secret'));
await win.happyDOM.close();
win=await open();
assert.equal($('token').value,'');assert.equal($('remember-token').checked,true);assert.match($('token').placeholder,/已保存/);
await $('connect').onclick();assert.match($('notice').textContent,/无需填写/);
await $('forget-token').onclick();assert.equal(vault.size,0);assert.equal($('remember-token').checked,false);
$('token').value='mock-secret';$('remember-token').checked=true;failSave=true;
await $('connect').onclick();assert.equal(vault.size,0);assert.equal($('token').value,'mock-secret');assert.match($('notice').textContent,/未能保存/);
failSave=false;await $('connect').onclick();assert.equal(vault.size,1);
$('remember-token').checked=false;$('remember-token').onchange();await win.happyDOM.waitUntilComplete();
assert.equal(vault.size,0);
await win.happyDOM.close();
console.log('PASS: opt-in save → clear input → reopen → reuse without token → delete → save failure preserves input → uncheck removes saved token. No real keychain/database accessed.');
