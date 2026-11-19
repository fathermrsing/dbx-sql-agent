import {spawn,spawnSync} from 'node:child_process';
import {mkdtemp,rm} from 'node:fs/promises';
import {createInterface} from 'node:readline';
import {createServer} from 'node:http';
import os from 'node:os';import path from 'node:path';import {fileURLToPath} from 'node:url';import assert from 'node:assert/strict';
const root=path.dirname(path.dirname(fileURLToPath(import.meta.url))),temp=await mkdtemp(path.join(os.tmpdir(),'agent-smoke-'));let child,writes=0;
const identity={server_name:'MOCK-SQL',product_version:'16.0',edition:'Developer',database_name:'msdb',login_name:'mock',is_sysadmin:1};
const server=createServer(async(req,res)=>{try{assert.equal(req.headers.authorization,'Bearer mock-secret');let body='';for await(const c of req)body+=c;const r=JSON.parse(body);let result={};if(r.method==='initialize')result={protocolVersion:'2025-11-25'};if(r.method==='tools/list')result={tools:[{name:'dbx_execute_batch'}]};if(r.method==='tools/call'){const a=r.params.arguments;assert.equal(a.connection_id,'mock');assert.equal(a.database,'msdb');let records=[];if(a.sql.includes('SERVERPROPERTY'))records=[identity];else if(a.sql.startsWith('EXEC('))writes++;else if(a.sql.includes('TOP (2001)'))records=[{job_id:'11111111-2222-3333-4444-555555555555',name:'mock',enabled:0}];const columns=Object.keys(records[0]||{});result={structuredContent:{results:[{columns,rows:records.map(x=>columns.map(k=>x[k])),error_message:typeof records[0]?.[columns[0]]==='string'?records[0][columns[0]]:null}]}};}res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));}catch(e){res.writeHead(500);res.end(String(e));}});
try{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const target={endpoint:`http://127.0.0.1:${server.address().port}/mcp`,connectionId:'mock'};
 const binary=path.join(temp,'sidecar');assert.equal(spawnSync('go',['build','-o',binary,'.'],{cwd:path.join(root,'backend'),env:{...process.env,GOCACHE:process.env.GOCACHE||path.join(os.tmpdir(),'dbx-agent-go-cache')},stdio:'inherit'}).status,0);
 child=spawn(binary,[],{env:{...process.env,DBX_PLUGIN_DATA_DIR:path.join(temp,'data')},stdio:['pipe','pipe','inherit']});let seq=0;const pending=new Map();
 createInterface({input:child.stdout}).on('line',line=>{const r=JSON.parse(line),p=pending.get(r.id);if(p){clearTimeout(p.timer);pending.delete(r.id);r.error?p.reject(Error(r.error.message)):p.resolve(r.result);}});
 function call(method,params){return new Promise((resolve,reject)=>{const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error('RPC timeout'));},10000);pending.set(id,{resolve,reject,timer});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id,method,params})+'\n');});}
 const init=await call('plugin/initialize',{host:{protocolVersions:[1],hostApiVersion:'1.4.0',dbxVersion:'0.6.70'},plugin:{id:'io.github.fathermrsing.sql-agent',version:'0.1.3'},permissions:['host.events']});assert.equal(init.plugin.id,'io.github.fathermrsing.sql-agent');
 const args={target,token:'mock-secret'};const jobs=await call('agent/read',{...args,resource:'jobs'});assert.equal(jobs.rows.length,1);
 const plan=await call('agent/plan',{...args,change:{action:'create',name:'native-smoke'}});assert.equal(writes,0);assert.match(plan.sql,/sp_add_job/);
 await assert.rejects(call('agent/start',{...args,planId:plan.id,confirmation:'WRONG'}),/确认/);assert.equal(writes,0);
 const op=await call('agent/start',{...args,planId:plan.id,confirmation:plan.confirmation});let state;
 for(let i=0;i<100;i++){state=await call('agent/status',{operationId:op.id});if(state.state!=='running')break;await new Promise(r=>setTimeout(r,20));}assert.equal(state.state,'accepted');assert.equal(writes,1);
 await assert.rejects(call('agent/start',{...args,planId:plan.id,confirmation:plan.confirmation}),/已使用/);
 const exited=new Promise((resolve,reject)=>child.on('exit',c=>c===0?resolve():reject(Error('exit '+c))));child.stdin.end();await exited;child=null;
 console.log('PASS: native JSONL initialize → mock MCP jobs → preview (zero writes) → wrong confirmation refused → single mock execute → accepted → replay refused. No real database accessed.');
}finally{if(child)child.kill();await new Promise(r=>server.close(r));await rm(temp,{recursive:true,force:true});}
