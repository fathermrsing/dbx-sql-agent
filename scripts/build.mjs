import {spawnSync} from 'node:child_process';
import {cp,mkdir,mkdtemp,readFile,rm,writeFile} from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
const root=path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const target=process.argv[2]||`${process.platform==='win32'?'windows':process.platform}-${process.arch}`;
const match=/^(darwin|linux|windows)-(arm64|x64)$/.exec(target);if(!match)throw Error('Unsupported target: '+target);
const [_,goos,arch]=match,goarch=arch==='x64'?'amd64':arch;
const temp=await mkdtemp(path.join(os.tmpdir(),'dbx-agent-build-'));
const cache=process.env.GOCACHE||path.join(os.tmpdir(),'dbx-agent-go-cache');
function run(args,env={}){const r=spawnSync('go',args,{cwd:path.join(root,'backend'),env:{...process.env,GOCACHE:cache,...env},stdio:'inherit'});if(r.status!==0)throw Error('go command failed');}
try{
 const stage=path.join(temp,'stage'),binary=`sql-agent${goos==='windows'?'.exe':''}`;
 await mkdir(path.join(stage,'bin',target),{recursive:true});await mkdir(path.join(root,'dist'),{recursive:true});
 if(goos==='darwin'&&process.platform!=='darwin')throw Error('macOS Keychain build requires a macOS SDK host');
 run(['build','-trimpath','-ldflags=-s -w','-o',path.join(stage,'bin',target,binary),'.'],{GOOS:goos,GOARCH:goarch,CGO_ENABLED:goos==='darwin'?'1':'0'});
 const manifest=JSON.parse(await readFile(path.join(root,'manifest.json'),'utf8'));manifest.entrypoints.backend.executable=`bin/${target}/${binary}`;
 await writeFile(path.join(stage,'manifest.json'),JSON.stringify(manifest,null,2)+'\n');
 await cp(path.join(root,'assets'),path.join(stage,'assets'),{recursive:true});await cp(path.join(root,'ui'),path.join(stage,'ui'),{recursive:true});
 await cp(path.join(root,'THIRD_PARTY_NOTICES.md'),path.join(stage,'THIRD_PARTY_NOTICES.md'));
 await cp(path.join(root,'LICENSE'),path.join(stage,'LICENSE'));
 await cp(path.join(root,'PRIVACY.md'),path.join(stage,'PRIVACY.md'));
 await cp(path.join(root,'third_party','dbx-plugin-sdk','LICENSE'),path.join(stage,'DBX-SDK-LICENSE'));
 const output=path.join(root,'dist',`${manifest.id}-${manifest.version}-${target}.unsigned.dbxp`);
 run(['run','./cmd/package',stage,output]);
 const bytes=await readFile(output);await writeFile(output+'.sha256',createHash('sha256').update(bytes).digest('hex')+'  '+path.basename(output)+'\n');
 console.log('UNSIGNED DEVELOPMENT PACKAGE — requires explicit DBX development-mode installation.');
}finally{await rm(temp,{recursive:true,force:true});}
