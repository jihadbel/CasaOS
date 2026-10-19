const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];

function fixture(fetchImpl = null) {
  const elements = new Map(), alerts = [];
  function element() {
    const classes = new Set(); let text = '';
    return {
      style: {}, dataset: {}, classList: {
        add(c) { classes.add(c); }, remove(c) { classes.delete(c); }, contains(c) { return classes.has(c); },
        toggle(c, force) { const enabled = force ?? !classes.has(c); if (enabled) classes.add(c); else classes.delete(c); return enabled; }
      },
      innerHTML: '', value: '',
      get innerText() { return text; }, set innerText(value) { text = String(value); },
      get textContent() { return text; }, set textContent(value) { text = String(value); },
      append(...nodes) { this.children = [...(this.children || []), ...nodes]; }, replaceChildren(...nodes) { this.children = nodes; },
      addEventListener() {}, removeAttribute() {}, focus() {}, select() {},
      querySelector() { return element(); }, querySelectorAll() { return []; }
    };
  }
  const document = {
    addEventListener() {}, querySelectorAll() { return []; }, createElement: element,
    execCommand() { return true; },
    getElementById(id) { if (!elements.has(id)) elements.set(id, element()); return elements.get(id); }
  };
  const sandbox = {
    document, console, URL, AbortController, navigator: {}, alert: text => alerts.push(text), confirm: () => true,
    setTimeout() {}, setInterval() {}, history: { replaceState() {} },
    window: {
      addEventListener() {}, isSecureContext: false,
      location: { hostname: 'localhost', origin: 'http://localhost:8080', href: 'http://localhost:8080/', port: '8080', hash: '' },
      open() {}
    },
    localStorage: { getItem() { return null; }, setItem() {} },
    Audio: class { addEventListener() {} pause() {} play() { return Promise.resolve(); } },
    FormData: class { append() {} },
    fetch: fetchImpl || (async () => ({ ok: true, status: 200, json: async () => ({ success: 200, data: { content: [] } }) }))
  };
  vm.createContext(sandbox);
  vm.runInContext(script, sandbox);
  return { sandbox, elements, alerts, run: code => vm.runInContext(code, sandbox) };
}

function decodeAttribute(text) {
  const entities = { amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'" };
  return text.replace(/&(amp|lt|gt|quot|#39);/g, (_, name) => entities[name]);
}

test('hostile file names are data, never JavaScript handlers or markup', () => {
  const f = fixture();
  const name = "x&#39;);globalThis.injected=1;foo(&#39;.txt";
  const path = 'C:/files/' + name;
  f.sandbox.items = [{ name, path, is_dir: false, size: 5 }];
  f.run('filteredItems=items;renderGridView(document.getElementById("probe"))');
  const result = f.elements.get('probe').innerHTML;
  const card = result.match(/class="item-card"\s+([^>]+)>/)[1];
  assert.equal(/onclick=/.test(card), false);
  const attributes = Object.fromEntries([...card.matchAll(/data-(\w+)="([^"]*)"/g)].map(m => [m[1], decodeAttribute(m[2])]));
  assert.equal(attributes.path, path);
  assert.equal(attributes.name, name);
  let opened;
  f.sandbox.openCodeModal = (p, n) => { opened = { p, n }; };
  f.sandbox.fakeEvent = { target: { closest: () => ({ dataset: attributes }) }, stopPropagation() {} };
  f.run('onFileAction(fakeEvent)');
  assert.deepEqual(opened, { p: path, n: name });
  assert.equal(f.sandbox.injected, undefined);
  f.sandbox.items = [{ name: '<img src=x onerror=alert(1)>.txt', path: 'C:/x.txt', is_dir: false }];
  f.run('filteredItems=items;renderListView(document.getElementById("probe"))');
  assert.equal(f.elements.get('probe').innerHTML.includes('<img src=x'), false);
});

test('folder image navigation uses a media-only playlist and updates video position', () => {
  const f = fixture();
  f.run('filteredItems=[{name:"folder",is_dir:true},{name:"a.jpg",path:"C:/a.jpg"},{name:"b.mp4",path:"C:/b.mp4"},{name:"z.txt"}];renderGridView(document.getElementById("probe"));openEnhancedLightbox("C:/a.jpg","a.jpg",1);navigateLightbox(1)');
  assert.equal(f.run('currentMediaIndex'), 1);
  assert.equal(f.run('mediaPlaylist.length'), 2);
  f.run('navigateLightbox(1)');
  assert.equal(f.run('currentMediaIndex'), 0);
});

test('TypeScript opens as code and only ZIP gets an extraction action', () => {
  const f = fixture();
  assert.equal(f.run('isVideo("app.ts")'), false);
  assert.equal(f.run('itemAction({name:"app.ts",is_dir:false})'), 'code');
  assert.equal(f.run('getSmartDestinationPath("app.ts")').endsWith('/Videos'), false);
  assert.equal(f.run('isZip("backup.rar")'), true);
  assert.equal(f.run('canExtract("backup.rar")'), false);
  assert.equal(f.run('canExtract("backup.zip")'), true);
});

test('HTTP and transport upload failures never display a success message', async () => {
  for (const transportFailure of [false, true]) {
    const f = fixture();
    f.sandbox.XMLHttpRequest = class {
      constructor() { this.upload = {}; this.status = 500; this.responseText = '{"success":500,"message":"disk full"}'; }
      open() {}
      send() { if (transportFailure) this.onerror(); else this.onload(); }
    };
    await f.run('uploadFiles([{name:"file.txt"}])');
    assert.match(f.elements.get('uploadStatusText').innerText, /فشل/);
    assert.ok(f.alerts.length > 0);
  }
});

test('editor save uses the registered PUT route', async () => {
  const f = fixture();
  let request;
  f.sandbox.fetch = async (url, options) => {
    if (options?.method === 'PUT') request = { url, options };
    return { ok: true, json: async () => ({ success: 200, data: 'original' }) };
  };
  await f.run('openCodeModal("C:/test.txt","test.txt")');
  f.elements.get('codeTextarea').value = 'edited';
  await f.run('saveCodeContent()');
  assert.equal(request.url, '/v1/file');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { path: 'C:/test.txt', content: 'edited' });
});

test('editor ignores stale reads after closing and switching files', async () => {
  const f = fixture();
  let releaseA, releaseB, saved;
  f.sandbox.fetch = (url, options) => {
    if (options?.method === 'PUT') {
      saved = JSON.parse(options.body);
      return Promise.resolve({ ok: true, json: async () => ({success: 200}) });
    }
    return new Promise(resolve => {
      const release = () => resolve({ ok: true, json: async () => ({success: 200, data: url.includes('A.txt') ? 'A contents' : 'B contents'}) });
      if (url.includes('A.txt')) releaseA = release; else releaseB = release;
    });
  };
  const a = f.run('openCodeModal("C:/A.txt","A.txt")');
  f.run('closeCodeModal()');
  const b = f.run('openCodeModal("C:/B.txt","B.txt")');
  releaseB(); await b;
  releaseA(); await a;
  await f.run('saveCodeContent()');
  assert.deepEqual(saved, {path: 'C:/B.txt', content: 'B contents'});
  assert.equal(f.elements.get('btnSaveCode').disabled, false);
});

test('editor cannot save while loading or after HTTP or transport read failures', async () => {
  for (const failure of ['pending', 'transport', 'http']) {
    const f = fixture(); let writes = 0;
    f.sandbox.fetch = (url, options) => {
      if (options?.method === 'PUT') { writes++; return Promise.resolve({ok: true, json: async () => ({success: 200})}); }
      if (failure === 'pending') return new Promise(() => {});
      if (failure === 'transport') return Promise.reject(Error('network failed'));
      return Promise.resolve({ok: false, json: async () => ({success: 500, message: 'read failed'})});
    };
    const read = f.run('openCodeModal("C:/important.txt","important.txt")');
    if (failure !== 'pending') await read;
    await f.run('saveCodeContent()');
    assert.equal(writes, 0);
    assert.equal(f.elements.get('codeTextarea').value, '');
    assert.equal(f.elements.get('codeTextarea').readOnly, true);
    assert.equal(f.elements.get('btnSaveCode').disabled, true);
    assert.ok(f.elements.get('codeStatus').textContent);
  }
});

test('a pending save keeps its original path and cannot reset a different editor', async () => {
  const f = fixture(); let releaseSave, saved;
  f.sandbox.fetch = (url, options) => {
    if (options?.method === 'PUT') {
      saved = JSON.parse(options.body);
      return new Promise(resolve => { releaseSave = () => resolve({ok: true, json: async () => ({success: 200})}); });
    }
    return Promise.resolve({ok: true, json: async () => ({success: 200, data: 'contents'})});
  };
  await f.run('openCodeModal("C:/A.txt","A.txt")');
  const save = f.run('saveCodeContent()');
  await f.run('saveCodeContent()');
  await f.run('openCodeModal("C:/B.txt","B.txt")');
  const buttonText = f.elements.get('btnSaveCode').innerHTML;
  releaseSave(); await save;
  assert.equal(saved.path, 'C:/A.txt');
  assert.equal(f.run('currentEditingFilePath'), 'C:/B.txt');
  assert.equal(f.elements.get('btnSaveCode').innerHTML, buttonText);
  assert.equal(f.elements.get('btnSaveCode').disabled, false);
});

test('background audio retains its queue across folder changes', () => {
  const f = fixture();
  f.run('filteredItems=[{name:"a.mp3",path:"C:/music/a.mp3"},{name:"b.mp3",path:"C:/music/b.mp3"}];playAudioTrack("C:/music/a.mp3","a.mp3",0)');
  f.run('filteredItems=[{name:"other.mp3",path:"C:/other/other.mp3"}];nextAudioTrack()');
  assert.equal(f.run('currentAudioIndex'), 1);
  assert.equal(f.run('audioPlaylist.length'), 2);
  assert.equal(f.run('audioPlayer.src'), '/v1/file?path=C%3A%2Fmusic%2Fb.mp3');
  f.run('filteredItems=[];nextAudioTrack();prevAudioTrack()');
  assert.equal(f.run('currentAudioIndex'), 1);
  assert.equal(f.run('audioPlaylist.length'), 2);
  f.run('filteredItems=[{name:"new.mp3",path:"C:/new.mp3"}];playAudioTrack("C:/new.mp3","new.mp3",0)');
  assert.equal(f.run('audioPlaylist.length'), 1);
  assert.equal(f.run('audioPlaylist[0].path'), 'C:/new.mp3');
});

test('share and phone links use discovered LAN IP and actual scheme and port', () => {
  const f = fixture();
  f.run('serverAddresses=["10.20.30.40"];setupPhoneQRCode();openShareModal("C:/a.txt","a.txt",1)');
  assert.ok(f.elements.get('shareUrlInput').value.startsWith('http://10.20.30.40:8080/'));
  f.run('updateQRCodeUrl("10.20.30.41");openShareModal("C:/a.txt","a.txt")');
  assert.ok(f.elements.get('shareUrlInput').value.startsWith('http://10.20.30.41:8080/'));
  assert.equal(html.includes('192.168.1.35'), false);
});

test('clipboard fallback works on HTTP LAN when Clipboard API is unavailable', async () => {
  const f = fixture();
  f.elements.set('shareUrlInput', { value: 'http://10.0.0.1/', focus() {}, select() {} });
  await f.run('copyShareUrl()');
  assert.equal(f.elements.get('btnCopyShare').textContent, 'تم النسخ ✓');
});

test('settings saved by another device are loaded from the server', async () => {
  const f = fixture();
  f.sandbox.fetch = async url => ({
    ok: true,
    json: async () => url === '/api/settings'
      ? { success: true, settings: { photosPath: 'E:/photos', defaultPath: 'E:/cloud' } }
      : { addresses: ['10.1.2.3'] }
  });
  f.sandbox.switchMainTab = () => {};
  await f.run('startDashboard()');
  assert.equal(f.run('appSettings.photosPath'), 'E:/photos');
  assert.equal(f.run('currentPath'), 'E:/cloud');
  assert.equal(f.run('authenticated'), true);
});

test('phone uploads honor the configured phone folder before type sorting', () => {
  const f=fixture();
  f.run('appSettings.mobilePath="E:/PhoneBackup";appSettings.photosPath="C:/Photos"');
  for(const name of ['photo.jpg','movie.mp4','notes.txt','app.apk','other.bin']) {
    f.sandbox.probeName=name;
    assert.equal(f.run('getSmartDestinationPath(probeName,true)'),'E:/PhoneBackup');
  }
  f.run('appSettings.smartAutoSort=false');
  assert.equal(f.run('getSmartDestinationPath("photo.jpg",true)'),'E:/PhoneBackup');
  assert.equal(f.run('getSmartDestinationPath("photo.jpg",false)'),f.run('currentPath'));
});

test('drive-root compression uses a normal ZIP file name', async () => {
  const f=fixture();let payload;
  f.sandbox.fetch=async(url,options)=>{payload=JSON.parse(options.body);return{ok:true,json:async()=>({success:false,error:'probe'})};};
  await f.run('currentPath="C:/";compressCurrentFolder()');
  assert.equal(payload.output,'C:/C-drive.zip');
  assert.equal(payload.files[0],'C:/');
});

test('search and clearing it preserve the selected file category', () => {
  const f=fixture();
  f.run('allItems=[{name:"picture.jpg"},{name:"notes.txt"}];activeFilter="photos";handleSearch({target:{value:"notes"}})');
  assert.equal(f.run('filteredItems.length'),0);
  f.run('handleSearch({target:{value:""}})');
  assert.equal(f.run('filteredItems.length'),1);
  assert.equal(f.run('filteredItems[0].name'),'picture.jpg');
  f.run('handleSearch({target:{value:"picture"}});setCategoryFilter("documents")');
  assert.equal(f.run('filteredItems.length'),0);
});

test('timeline ignores obsolete filter responses and requests after leaving it', async () => {
  const f=fixture();let releaseOld,releaseNew;
  f.sandbox.fetch=url=>new Promise(resolve=>{
    const old=url.includes('photos');
    const release=()=>resolve({ok:true,json:async()=>({success:true,total:1,timeline:[{period:'test',items:[{name:old?'old.jpg':'new.mp4',path:old?'C:/old.jpg':'C:/new.mp4',is_video:!old,date_str:'2026-10-15',size:1}]}]})});
    if(old)releaseOld=release;else releaseNew=release;
  });
  const old=f.run('activeTimelineFilter="photos";loadTimeline()');
  const current=f.run('activeTimelineFilter="videos";loadTimeline()');
  releaseNew();await current;releaseOld();await old;
  assert.equal(f.run('mediaPlaylist[0].name'),'new.mp4');
  const pending=f.run('activeTimelineFilter="photos";loadTimeline()');
  f.run('switchMainTab("files",false)');
  releaseOld();await pending;
  assert.equal(f.run('mediaPlaylist[0].name'),'new.mp4');
});

test('timeline pages replace media, expose partial scans, and reset on filter change', async () => {
  const f=fixture();const requests=[];
  f.sandbox.fetch=async url=>{
    requests.push(url);
    const offset=Number(new URL(url,'http://localhost').searchParams.get('offset'));
    return {ok:true,json:async()=>({success:true,total:201,has_more:offset<200,truncated:true,timeline:[{period:'test',items:[{name:`page-${offset}.jpg`,path:`C:/page-${offset}.jpg`,is_video:false,date_str:'2026-10-16',size:1}]}]})};
  };
  await f.run('loadTimeline()');
  assert.match(requests[0],/limit=100&offset=0/);
  assert.equal(f.elements.get('timelinePrevious').disabled,true);
  assert.equal(f.elements.get('timelineNext').disabled,false);
  assert.equal(f.elements.get('timelineScanNotice').style.display,'block');
  await f.run('loadTimeline(100)');
  assert.equal(f.run('timelineOffset'),100);
  assert.equal(f.run('mediaPlaylist.length'),1);
  assert.equal(f.run('mediaPlaylist[0].name'),'page-100.jpg');
  assert.equal(f.elements.get('timelinePrevious').disabled,false);
  await f.run('loadTimeline(200)');
  assert.equal(f.elements.get('timelineNext').disabled,true);
  f.run('setTimelineFilter("photos")');
  await new Promise(resolve=>setImmediate(resolve));
  assert.match(requests.at(-1),/type=photos&limit=100&offset=0/);
  assert.equal(f.run('timelineOffset'),0);
});

test('timeline ignores a stale response from another page', async () => {
  const f=fixture();let oldPage;
  f.sandbox.fetch=url=>{
    const offset=Number(new URL(url,'http://localhost').searchParams.get('offset'));
    const result={ok:true,json:async()=>({success:true,total:200,has_more:offset===0,timeline:[{period:'test',items:[{name:`page-${offset}.jpg`,path:`C:/page-${offset}.jpg`,size:1}]}]})};
    return offset===0?new Promise(resolve=>{oldPage=()=>resolve(result);}):Promise.resolve(result);
  };
  const old=f.run('loadTimeline()');
  await f.run('loadTimeline(100)');
  oldPage();await old;
  assert.equal(f.run('timelineOffset'),100);
  assert.equal(f.run('mediaPlaylist[0].name'),'page-100.jpg');
});

test('timeline aborts obsolete scans when changing pages or leaving the gallery', async () => {
  const f=fixture();const signals=[];
  f.sandbox.fetch=async (url,options)=>{
    if(options?.signal)signals.push(options.signal);
    return {ok:true,json:async()=>({success:true,total:0,timeline:[]})};
  };
  await f.run('loadTimeline()');
  await f.run('loadTimeline(100)');
  assert.equal(signals[0].aborted,true);
  assert.equal(signals[1].aborted,false);
  f.run('switchMainTab("files",false)');
  assert.equal(signals[1].aborted,true);
});

test('timeline video cards do not start metadata requests eagerly', async () => {
  const f=fixture();
  f.sandbox.fetch=async()=>({ok:true,json:async()=>({success:true,total:50,timeline:[{period:'test',items:Array.from({length:50},(_,i)=>({name:`${i}.mp4`,path:`C:/${i}.mp4`,is_video:true,size:1,date_str:'2026-10-16'}))}]})});
  await f.run('loadTimeline()');
  const html=f.elements.get('timelineContainer').innerHTML;
  assert.equal((html.match(/<video /g)||[]).length,50);
  assert.equal((html.match(/preload="none"/g)||[]).length,50);
  assert.doesNotMatch(html,/<video\s+src=/);
  assert.equal((html.match(/data-preview-src=/g)||[]).length,50);
});

test('timeline videos load only near the viewport and cancel when leaving', () => {
  const f=fixture();let callback,observer;const watched=[],unwatched=[];
  f.sandbox.IntersectionObserver=class {
    constructor(cb) {callback=cb;observer=this;this.disconnected=false;}
    observe(video) {watched.push(video);}
    unobserve(video) {unwatched.push(video);}
    disconnect() {this.disconnected=true;}
  };
  const makeVideo=i=>({dataset:{previewSrc:`/v1/file?path=${i}.mp4#t=0.5`},loads:0,pauses:0,
    load(){this.loads++;},pause(){this.pauses++;},hasAttribute(name){return name==='src'&&this.src!==undefined;},removeAttribute(name){delete this[name];}});
  const videos=[makeVideo(1),makeVideo(2)];
  f.sandbox.previewContainer={querySelectorAll(){return videos;}};
  f.run('observeTimelineVideoPreviews(previewContainer)');
  assert.equal(watched.length,2);
  assert.equal(videos[0].src,undefined);
  callback([{target:videos[0],isIntersecting:false},{target:videos[1],isIntersecting:true}]);
  assert.equal(videos[0].src,undefined);
  assert.equal(videos[1].src,videos[1].dataset.previewSrc);
  assert.equal(videos[1].loads,1);
  assert.deepEqual(unwatched,[videos[1]]);
  f.run('switchMainTab("files",false)');
  assert.equal(observer.disconnected,true);
  assert.equal(videos[1].src,undefined);
  assert.equal(videos[1].pauses,1);
  callback([{target:videos[0],isIntersecting:true}]);
  assert.equal(videos[0].src,undefined);
});

test('creating a folder reports server errors and keeps the input available', async () => {
  for(const response of [{ok:false,status:500,success:500},{ok:true,status:200,success:500}]) {
    const f=fixture();let closed=false;
    f.sandbox.fetch=async()=>({ok:response.ok,status:response.status,json:async()=>({success:response.success,message:'Access denied'})});
    f.sandbox.closeNewFolderModal=()=>{closed=true;};
    f.elements.set('folderNameInput',{value:'new-folder'});
    await f.run('submitCreateFolder()');
    assert.equal(closed,false);
    assert.equal(f.elements.get('folderNameInput').value,'new-folder');
    assert.match(f.alerts[0],/Access denied/);
  }
  const f=fixture();let closed=false;
  f.sandbox.fetch=async()=>({ok:true,json:async()=>({success:200})});
  f.sandbox.closeNewFolderModal=()=>{closed=true;};f.sandbox.loadCurrentFolder=()=>{};
  f.elements.set('folderNameInput',{value:'new-folder'});
  await f.run('submitCreateFolder()');assert.equal(closed,true);
});

test('editor explains unsupported encodings and does not enable saving', async () => {
  const f=fixture();
  f.sandbox.fetch=async()=>({ok:false,status:415,json:async()=>({success:415,message:'Unsupported encoding'})});
  await f.run('openCodeModal("C:/legacy.txt","legacy.txt")');
  assert.match(f.elements.get('codeStatus').textContent,/UTF-8/);
  assert.equal(f.elements.get('btnSaveCode').disabled,true);
  assert.equal(f.elements.get('codeTextarea').value,'');
});

test('logout clears account data, media and private settings from the page', () => {
  const f=fixture();
  f.run('currentAccount={id:"alice",username:"alice",admin:true};allItems=[{name:"private.txt"}];filteredItems=allItems;audioPlaylist=[{path:"private.mp3"}];mediaPlaylist=[{path:"private.jpg"}];trashedItemsList=[{id:"private"}];document.getElementById("codeTextarea").value="private contents";document.getElementById("settingDefaultPath").value="alice home";showLogin()');
  assert.equal(f.run('currentAccount'),null);
  for(const name of ['allItems','filteredItems','audioPlaylist','mediaPlaylist','trashedItemsList'])assert.equal(f.run(name+'.length'),0);
  assert.equal(f.elements.get('codeTextarea').value,'');
  assert.equal(f.elements.get('settingDefaultPath').value,'');
  assert.equal(f.elements.get('manageUsersButton').style.display,'none');
});

test('switching accounts removes the audio panel, filename and pending playback state', async () => {
  const f = fixture(); let finishPlayback;
  f.sandbox.pendingPlayback = new Promise(resolve => { finishPlayback = resolve; });
  f.run('audioPlayer.play=()=>pendingPlayback;playAudioTrack("C:/private/alice/secret-recording.mp3","secret-recording.mp3")');
  assert.equal(f.elements.get('floatingAudioPlayer').classList.contains('active'), true);
  assert.equal(f.elements.get('audioTrackTitle').textContent, 'secret-recording.mp3');
  f.run('showLogin();applyAccountSession({user:{id:"bob",username:"bob",admin:false},privateStorage:true})');
  finishPlayback(); await Promise.resolve(); await Promise.resolve();
  assert.equal(f.elements.get('floatingAudioPlayer').classList.contains('active'), false);
  for (const id of ['audioTrackTitle','audioTrackSub','audioCurrentTime','audioDuration']) assert.equal(f.elements.get(id).textContent, '');
  assert.equal(f.run('isAudioPlaying'), false);
  assert.equal(f.run('currentShareItem.path'), '');
  assert.equal(f.run('audioPlayer.src'), '');
});

test('private navigation stays below the user home and hides account management from members', () => {
  const f=fixture();
  f.run('applyAccountSession({user:{id:"alice",username:"alice",admin:false},privateStorage:true});appSettings.defaultPath="C:/service/files/alice";currentPath="C:/service/files/alice/Photos";renderBreadcrumbs()');
  assert.equal(f.elements.get('manageUsersButton').style.display,'none');
  const labels=f.elements.get('breadcrumbsContainer').children.map(n=>n.textContent);
  assert.deepEqual(labels,['ملفاتي','/','الصور']);
  assert.equal(f.run('privateDefaultSettings().defaultPath'),'C:/service/files/alice');
  f.run('applyAccountSession({user:{id:"admin",username:"admin",admin:true},privateStorage:true})');
  assert.equal(f.elements.get('manageUsersButton').style.display,'inline-flex');
});

test('responses from a logged out account cannot repopulate the page', async () => {
  let release;
  const f=fixture(()=>new Promise(resolve=>release=resolve));
  const pending=f.run('fetch("/api/trash")');
  f.run('showLogin()');
  release({ok:true,status:200,json:async()=>({items:[{name:'private.txt'}]})});
  await assert.rejects(pending,/تم تغيير الحساب/);
});

test('local login sends the username and displays the authenticated account', async () => {
  const requests=[];
  const f=fixture(async(url,options)=>{requests.push({url,options});return{ok:true,status:200,json:async()=>url==='/api/auth/session'?{authenticated:true,user:{id:'alice',username:'alice',admin:false},privateStorage:true}:{}};});
  f.sandbox.fetch=async url=>({ok:true,json:async()=>url==='/api/settings'?{success:true,privateStorage:true,settings:{defaultPath:'C:/private/alice'}}:{addresses:[]}});
  f.sandbox.switchMainTab=()=>{};
  f.run('document.getElementById("loginUsername").value="alice";document.getElementById("loginPassword").value="password-123456"');
  await f.run('submitLogin({preventDefault(){}})');
  const loginRequest=requests.find(r=>r.options?.method==='POST');
  assert.deepEqual(JSON.parse(loginRequest.options.body),{username:'alice',password:'password-123456'});
  assert.equal(f.run('currentAccount.username'),'alice');
  assert.equal(f.elements.get('loginPassword').value,'');
});

test('private file links never send filenames to the external QR service', () => {
  const f=fixture();f.run('privateStorageMode=true;serverAddresses=["192.168.1.5"];openShareModal("C:/private/alice/secret.txt","secret.txt",5);setupPhoneQRCode()');
  assert.equal(f.elements.get('shareQrImg').style.display,'none');
  assert.equal(f.elements.get('shareQrImg').src,undefined);
  assert.equal(f.elements.get('qrImage').style.display,'none');
});

test('failed folder navigation discards old files and preserves the error while filtering', async () => {
  for (const transportFailure of [false, true]) {
    const f = fixture(async () => {
      if (transportFailure) throw Error('synthetic connection failure');
      return {ok: false, status: 500, json: async () => ({success: 500, message: 'synthetic folder unavailable'})};
    });
    f.sandbox.console = {error() {}};
    f.run('currentPath="/private/new-folder";allItems=[{name:"old-file.txt",path:"/private/old-folder/old-file.txt"}];filteredItems=[...allItems];mediaPlaylist=[{path:"/private/old-folder/old.jpg"}]');
    await f.run('loadCurrentFolder()');
    assert.equal(f.run('allItems.length'), 0);
    assert.equal(f.run('filteredItems.length'), 0);
    assert.equal(f.run('mediaPlaylist.length'), 0);
    assert.equal(f.run('folderLoadState'), 'error');
    const errorView = f.elements.get('viewContainer').innerHTML;
    f.run('setCategoryFilter("all");changeSort("name_asc");setViewMode("list")');
    assert.equal(f.elements.get('viewContainer').innerHTML, errorView);
    assert.equal(errorView.includes('old-file.txt'), false);
    assert.equal(f.elements.get('emptyState').style.display, 'none');
    assert.equal(f.elements.get('countAll').innerText, '0');
  }
});

test('pending navigation cannot restore old files and a successful retry replaces the error', async () => {
  let complete;
  const f = fixture(() => new Promise(resolve => { complete = resolve; }));
  f.run('currentPath="/new";allItems=[{name:"old-file.txt"}];filteredItems=[...allItems]');
  const pending = f.run('loadCurrentFolder()');
  f.run('setCategoryFilter("photos")');
  assert.equal(f.run('allItems.length'), 0);
  assert.match(f.elements.get('viewContainer').innerHTML, /جارٍ تحميل/);
  assert.equal(f.elements.get('emptyState').style.display, 'none');
  complete({ok: false, status: 500, json: async () => ({success: 500, message: 'failed'})});
  await pending;
  const retry = f.run('loadCurrentFolder()');
  complete({ok: true, status: 200, json: async () => ({success: 200, data: {content: [{name: 'new.jpg', path: '/new/new.jpg'}]}})});
  await retry;
  assert.equal(f.run('folderLoadState'), 'ready');
  assert.equal(f.run('allItems[0].name'), 'new.jpg');
  assert.match(f.elements.get('viewContainer').innerHTML, /new\.jpg/);
});

test('an obsolete failed folder request cannot replace a newer successful listing', async () => {
  let finishOld;
  const f = fixture(url => url.includes('old-folder')
    ? new Promise(resolve => { finishOld = resolve; })
    : Promise.resolve({ok: true, status: 200, json: async () => ({success: 200, data: {content: [{name: 'new.txt', path: '/new-folder/new.txt'}]}})}));
  f.run('currentPath="/old-folder"');
  const old = f.run('loadCurrentFolder()');
  f.run('currentPath="/new-folder"');
  await f.run('loadCurrentFolder()');
  finishOld({ok: false, status: 500, json: async () => ({success: 500, message: 'obsolete failure'})});
  await old;
  assert.equal(f.run('folderLoadState'), 'ready');
  assert.equal(f.run('allItems[0].name'), 'new.txt');
  assert.equal(f.elements.get('viewContainer').innerHTML.includes('obsolete failure'), false);
});

test('gateway mode explains the unavailable local login and never submits a password', async () => {
  const requests = [];
  const f = fixture(async (url, options) => {
    requests.push({url, method: options?.method || 'GET'});
    return {ok: true, status: 200, json: async () => ({authenticated: false, authenticationMode: 'gateway'})};
  });
  await f.run('initializeDashboardSession()');
  for (const id of ['loginUsername', 'loginPassword', 'loginSubmit']) assert.equal(f.elements.get(id).disabled, true);
  assert.match(f.elements.get('loginError').textContent, /-standalone/);
  await f.run('submitLogin({preventDefault(){}})');
  assert.deepEqual(requests, [{url: '/api/auth/session', method: 'GET'}]);
  assert.equal(f.run('authenticated'), false);
});

test('session discovery still enables local login and restores authenticated local sessions', async () => {
  for (const authenticated of [false, true]) {
    const info = {authenticated, user: {username: 'alice'}, privateStorage: true};
    const f = fixture(async () => ({ok: true, status: 200, json: async () => info}));
    let started = null;
    f.sandbox.startDashboard = async session => { started = session; };
    await f.run('initializeDashboardSession()');
    if (authenticated) assert.equal(started, info);
    else {
      assert.equal(started, null);
      assert.equal(f.elements.get('loginSubmit').disabled, false);
      assert.equal(f.run('localLoginEnabled'), true);
    }
  }
});
