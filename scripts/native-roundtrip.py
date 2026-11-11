#!/usr/bin/env python3
# Uses the locally authenticated official CLIs; creates only dedicated test sessions.
import json,os,pathlib,subprocess,time,urllib.request
def agent_text(d):
 if d.get('type')=='assistant':return str(d.get('message'))
 item=d.get('params',{}).get('item',{})
 return str(item.get('text','')) if item.get('type')=='agentMessage' else ''
state=pathlib.Path(os.environ.get('XDG_STATE_HOME',str(pathlib.Path.home()/'.local/state')))/'agent-workbench';cookie=''
def api(path,body=None):
 global cookie
 req=urllib.request.Request('http://127.0.0.1:17880/api/'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Host':'agent.localhost:17880','Origin':'http://agent.localhost:17880','Content-Type':'application/json','Cookie':cookie})
 with urllib.request.urlopen(req,timeout=60) as r:
  if r.headers.get('Set-Cookie'):cookie=r.headers['Set-Cookie'].split(';')[0]
  return json.load(r)
if api('auth/status')['pairingRequired']:api('login',{'code':(state/'pairing-code').read_text()})
cwd=pathlib.Path('/tmp/awb-native-roundtrip');cwd.mkdir(exist_ok=True);results=[]
for agent in ['codex','claude']:
 marker='WORKBENCH_NATIVE_'+agent.upper();s=api('sessions',{'agent':agent,'cwd':str(cwd),'title':agent+' native roundtrip','initialMessage':'Remember the token '+marker+'. Reply OK only; do not use tools or change files.'});sid=s['id'];result={'agent':agent,'nativeIdentity':sid==agent+':'+s['nativeId']}
 try:
  deadline=time.monotonic()+120
  while time.monotonic()<deadline:
   current=next((x for x in api('sessions') if x['id']==sid),None)
   if current and current['status'] in ['idle','error']:break
   time.sleep(.25)
  api('sessions/'+sid+'/stop',{})
  prompt='What exact token did I ask you to remember? Reply with the token only. Do not use tools.'
  args=['codex','exec','resume',s['nativeId'],prompt,'--json','--skip-git-repo-check'] if agent=='codex' else ['claude','--resume',s['nativeId'],'-p',prompt,'--output-format','json']
  run=subprocess.run(args,cwd=cwd,capture_output=True,text=True,timeout=120);result['officialCLIResume']=run.returncode==0 and marker in run.stdout;result['cliExitCode']=run.returncode
  if run.returncode!=0:result['cliErrorCategory']='git-trust' if 'git' in run.stderr.lower() else 'native-cli-error'
  history=api('history?session='+sid)['events'];result['nativeHistoryRecords']=len(history);result['cliReplyVisible']=any(marker in agent_text(e['data']) for e in history)
 finally:
  api('sessions/'+sid+'/stop',{})
 results.append(result);print(json.dumps(result),flush=True)
pathlib.Path('artifacts/native-roundtrip.json').write_text(json.dumps(results,indent=2))
if any(not r['officialCLIResume'] or not r['cliReplyVisible'] for r in results):raise SystemExit(1)
