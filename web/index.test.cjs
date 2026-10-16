const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];

function fixture() {
  const elements = new Map(), alerts = [];
  function element() {
    return {
      style: {}, dataset: {}, classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
      innerHTML: '', innerText: '', textContent: '', value: '',
      append() {}, replaceChildren(...nodes) { this.children = nodes; },
      addEventListener() {}, removeAttribute() {}, focus() {}, select() {},
      querySelector() { return element(); }
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
    fetch: async () => ({ ok: true, status: 200, json: async () => ({ success: 200, data: { content: [] } }) })
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
