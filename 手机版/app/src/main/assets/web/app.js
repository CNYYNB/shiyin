/* 拾音 — 界面逻辑（运行在 WebView 中，通过 window.Android 与原生播放服务通信） */
(function () {
  'use strict';
  const A = window.Android;
  // 必须最先定义：原生端通过 App.xxx 回调；1.1 里这行在文件后面，导致脚本在加载时就报 ReferenceError 中断。
  window.App = window.App || {};
  const $ = (id) => document.getElementById(id);
  const ROW = 64;

  // ---------------- 工具 ----------------
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  function fmt(ms) {
    if (!ms || ms < 0) ms = 0;
    const s = Math.floor(ms / 1000), m = Math.floor(s / 60), h = Math.floor(m / 60);
    const ss = String(s % 60).padStart(2, '0');
    return h ? `${h}:${String(m % 60).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
  }
  function hue(str) { let h = 0; for (let i = 0; i < str.length; i++) h = (h * 31 + str.charCodeAt(i)) | 0; return Math.abs(h) % 360; }
  function firstChar(s) { s = String(s || '').trim(); const m = s.match(/[\p{L}\p{N}]/u); return m ? m[0].toUpperCase() : '♪'; }
  const noCover = new Set();
  function coverHTML(id, name, cls) {
    const h = hue(name || id || '');
    const style = `background:linear-gradient(135deg,hsl(${h},45%,38%),hsl(${(h + 40) % 360},50%,24%))`;
    const cv = id && byId.get(id) ? byId.get(id).cv || 0 : 0;
    const img = id && !noCover.has(id) ? `<img src="/cover/${encodeURIComponent(id)}${cv ? '?v=' + cv : ''}" loading="lazy" decoding="async" data-cid="${esc(id)}" onerror="window.__noCover(this)">` : '';
    return `<div class="cov ${cls || ''}" style="${style}"><span class="ch">${esc(firstChar(name))}</span>${img}</div>`;
  }
  window.__noCover = (img) => { noCover.add(img.getAttribute('data-cid')); img.remove(); };
  function kvGet(k, def) { try { const v = A.kvGet(k); return v == null ? def : JSON.parse(v); } catch (e) { return def; } }
  function kvSet(k, v) { try { A.kvSet(k, JSON.stringify(v)); } catch (e) {} }
  // 繁体 → 简体（t2s.js，OpenCC 词典）。设置「歌词转简体」默认开启；作用于歌词、歌名、歌手、专辑的显示。
  let t2sOn = kvGet('t2sLyrics', true) !== false;
  // 主题：'dark'（默认）/ 'light'
  let theme = kvGet('theme', 'dark') === 'light' ? 'light' : 'dark';
  function applyTheme() {
    document.body.classList.toggle('light', theme === 'light');
    const m = document.querySelector('meta[name="color-scheme"]'); if (m) m.setAttribute('content', theme);
    try { A.setTheme(theme === 'light'); } catch (e) {}
  }
  applyTheme();
  function zh(s) {
    if (!t2sOn || !s || !window.T2S) return s;
    try { return window.T2S.convert(s); } catch (e) { return s; }
  }
  let toastTimer = 0;
  function toast(msg, ms) {
    const t = $('toast'); if (!t) return;
    t.textContent = msg; t.classList.remove('hidden'); clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.add('hidden'), ms || Math.max(2200, Math.min(5000, String(msg).length * 120)));
  }
  const collator = new Intl.Collator('zh-Hans-CN', { numeric: true, sensitivity: 'base' });
  const cmp = (a, b) => collator.compare(a, b);
  const ICON = {
    play: '<svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>',
    pause: '<svg viewBox="0 0 24 24"><path d="M6 5h4v14H6zm8 0h4v14h-4z"/></svg>',
    more: '<svg viewBox="0 0 24 24"><path d="M12 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>',
    all: '<svg viewBox="0 0 24 24"><path d="M7 7h10v3l4-4-4-4v3H5v6h2zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2z"/></svg>',
    one: '<svg viewBox="0 0 24 24"><path d="M7 7h10v3l4-4-4-4v3H5v6h2zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2zm-4-2V9h-1l-2 1v1h1.5v4z"/></svg>',
    shuffle: '<svg viewBox="0 0 24 24"><path d="M10.6 9.2 5.4 4 4 5.4l5.2 5.2zM14.5 4l2 2L4 18.6 5.4 20 18 7.5l2 2V4zm.3 9.4-1.4 1.4 3.1 3.1-2 2H20v-5.5l-2 2z"/></svg>',
  };
  const MODE_NAME = { all: '列表循环', one: '单曲循环', shuffle: '随机播放' };

  // ---------------- 数据 ----------------
  let tracks = [], byId = new Map(), albums = [], artists = [];
  let prefs = kvGet('prefs', { sort: 'title', dir: 1 });
  let playlists = kvGet('playlists', null);
  if (!Array.isArray(playlists)) playlists = [{ id: 'fav', name: '我喜欢', ids: [], created: Date.now() }];
  if (!playlists.find((p) => p.id === 'fav')) playlists.unshift({ id: 'fav', name: '我喜欢', ids: [], created: Date.now() });
  let offsets = kvGet('lyricOffsets', {});
  const savePlaylists = () => kvSet('playlists', playlists);

  let libReport = {};
  function loadLibrary() {
    let lib = { tracks: [] };
    try { lib = JSON.parse(A.getLibrary()); } catch (e) {}
    tracks = lib.tracks || [];
    // 保留原文（oTitle/oArtist/oAlbum：歌词偏移的键、手动搜索 LRCLIB 用原文），显示用简体
    for (const t of tracks) {
      t.oTitle = t.title; t.oArtist = t.artist; t.oAlbum = t.album;
      if (t2sOn) { t.title = zh(t.title); t.artist = zh(t.artist); t.album = zh(t.album); }
    }
    libReport = lib.report || {};
    byId = new Map(tracks.map((t) => [t.id, t]));
    remapIds();
    const am = new Map(), rm = new Map();
    for (const t of tracks) {
      const ak = t.album + '\u0001' + (t.albumId > 0 ? t.albumId : '');
      let a = am.get(ak);
      if (!a) { a = { key: ak, name: t.album, artists: new Map(), tracks: [] }; am.set(ak, a); }
      a.tracks.push(t); a.artists.set(t.artist, (a.artists.get(t.artist) || 0) + 1);
      for (const name of splitArtists(t.artist)) {
        let r = rm.get(name);
        if (!r) { r = { name, tracks: [] }; rm.set(name, r); }
        r.tracks.push(t);
      }
    }
    albums = [...am.values()].map((a) => {
      a.artist = [...a.artists.entries()].sort((x, y) => y[1] - x[1])[0][0];
      if (a.artists.size > 2) a.artist = '群星';
      a.tracks.sort((x, y) => (x.trackNo || 999) - (y.trackNo || 999) || cmp(x.title, y.title));
      return a;
    }).sort((x, y) => cmp(x.name, y.name));
    artists = [...rm.values()].sort((x, y) => (x.name === '未知歌手') - (y.name === '未知歌手') || cmp(x.name, y.name));
    return lib;
  }
  // 歌曲移动位置/重新扫描后 id 会变：用稳定的 key（文件名+大小）把歌单、我喜欢、播放记录里的旧 id 换成新 id
  function remapIds() {
    if (!tracks.length) return;
    const keyToId = new Map(tracks.map((t) => [t.key, t.id]));
    const old = kvGet('idKey', {}) || {};
    const remap = (id) => (byId.has(id) ? id : (old[id] && keyToId.get(old[id])) || id);
    let changed = false;
    for (const p of playlists) {
      const ids = [...new Set(p.ids.map(remap))];
      if (ids.join() !== p.ids.join()) { p.ids = ids; changed = true; }
    }
    if (changed) savePlaylists();
    const sess = kvGet('session', null);
    if (sess && sess.ids) { const ids = sess.ids.map(remap); if (ids.join() !== sess.ids.join()) { sess.ids = ids; kvSet('session', sess); } }
    const nk = {};
    for (const [id, k] of Object.entries(old)) if (!byId.has(id)) nk[id] = k; // 还没找回来的旧 id 先留着
    const ks = Object.keys(nk); if (ks.length > 3000) for (const k of ks.slice(0, ks.length - 3000)) delete nk[k];
    for (const t of tracks) nk[t.id] = t.key;
    kvSet('idKey', nk);
  }
  function remapQueueAfterMove(oldQueue, oldIndex, pos, wasPlaying) {
    const old = kvGet('idKey', {}) || {};
    const keyToId = new Map(tracks.map((t) => [t.key, t.id]));
    const ids = oldQueue.map((id) => (byId.has(id) ? id : (old[id] && keyToId.get(old[id])) || null));
    const cur = ids[oldIndex];
    const list = ids.filter(Boolean);
    if (!list.length) return;
    const idx = Math.max(0, cur ? list.indexOf(cur) : 0);
    A.play(JSON.stringify(list), idx, cur ? pos : 0, wasPlaying);
  }
  function splitArtists(s) {
    const parts = String(s || '').split(/\s*(?:\/|、|;|；|&|,|，| feat\.? | ft\. )\s*/i).map((x) => x.trim()).filter(Boolean);
    return parts.length ? [...new Set(parts)] : ['未知歌手'];
  }
  const SORTS = {
    title: { name: '标题', fn: (a, b) => cmp(a.title, b.title) },
    artist: { name: '歌手', fn: (a, b) => cmp(a.artist, b.artist) || cmp(a.album, b.album) || (a.trackNo - b.trackNo) },
    album: { name: '专辑', fn: (a, b) => cmp(a.album, b.album) || (a.trackNo - b.trackNo) || cmp(a.title, b.title) },
    added: { name: '添加时间', fn: (a, b) => b.dateAdded - a.dateAdded },
    duration: { name: '时长', fn: (a, b) => a.duration - b.duration },
    folder: { name: '文件夹', fn: (a, b) => cmp(a.folder, b.folder) || cmp(a.title, b.title) },
  };
  function sortedSongs() {
    const s = SORTS[prefs.sort] || SORTS.title;
    const arr = tracks.slice().sort(s.fn);
    if (prefs.dir < 0) arr.reverse();
    return arr;
  }

  // ---------------- 虚拟列表 ----------------
  function VList(scrollEl, spacerEl, renderRow, onClick) {
    let items = [], first = -1, last = -1, raf = 0, topPad = 0;
    function draw() {
      raf = 0;
      const st = scrollEl.scrollTop - topPad, h = scrollEl.clientHeight || 800;
      const f = Math.max(0, Math.floor(st / ROW) - 6), l = Math.min(items.length, Math.ceil((st + h) / ROW) + 6);
      if (f === first && l === last) return;
      first = f; last = l;
      let html = '';
      for (let i = f; i < l; i++) html += renderRow(items[i], i).replace('<div class="row', `<div style="top:${i * ROW}px" data-i="${i}" class="row`);
      spacerEl.innerHTML = html;
    }
    scrollEl.addEventListener('scroll', () => { if (!raf) raf = requestAnimationFrame(draw); }, { passive: true });
    spacerEl.addEventListener('click', (e) => {
      const row = e.target.closest('.row'); if (!row) return;
      const i = +row.dataset.i; onClick(items[i], i, !!e.target.closest('.more'), e);
    });
    return {
      set(list, keepScroll) { items = list; spacerEl.style.height = items.length * ROW + 'px'; first = last = -1; if (!keepScroll) scrollEl.scrollTop = 0; draw(); },
      refresh() { first = last = -1; draw(); },
      get items() { return items; },
    };
  }
  function songRow(t, i, opts) {
    if (!t) return '<div class="row"></div>';
    const playing = state.id === t.id ? ' playing' : '';
    const lead = opts && opts.num ? `<span class="num">${t.trackNo || i + 1}</span>` : coverHTML(t.id, t.album !== '未知专辑' ? t.album : t.title, '');
    return `<div class="row${playing}">${lead}<div class="txt"><div class="t1">${esc(t.title)}</div><div class="t2">${esc(t.artist)} · ${esc(t.album)}</div></div><button class="more">${ICON.more}</button></div>`;
  }

  // ---------------- 主界面 ----------------
  let curTab = 'songs';
  const songList = VList($('songScroll'), $('songSpacer'), (t, i) => songRow(t, i), (t, i, more) => {
    if (more) return songMenu(t); playList(songList.items.map((x) => x.id), i);
  });
  function renderSongs(keep) {
    $('sortLabel').textContent = (SORTS[prefs.sort] || SORTS.title).name + (prefs.dir < 0 ? '↓' : '');
    $('songCount').textContent = `共 ${tracks.length} 首`;
    songList.set(sortedSongs(), keep);
  }
  function renderAlbums() {
    $('albumGrid').innerHTML = albums.map((a, i) => `<div class="card" data-i="${i}">${coverHTML(a.tracks[0].id, a.name)}<div class="t1">${esc(a.name)}</div><div class="t2">${esc(a.artist)} · ${a.tracks.length} 首</div></div>`).join('');
  }
  function renderArtists() {
    $('artistList').innerHTML = artists.map((r, i) => `<div class="row" data-i="${i}">${coverHTML(r.tracks[0].id, r.name, 'round')}<div class="txt"><div class="t1">${esc(r.name)}</div><div class="t2">${r.tracks.length} 首歌曲</div></div></div>`).join('');
  }
  function renderPlaylists() {
    $('playlistList').innerHTML = playlists.map((p, i) => {
      const ids = p.ids.filter((id) => byId.has(id));
      const cid = ids[0] || '';
      return `<div class="row" data-i="${i}">${coverHTML(cid, p.name)}<div class="txt"><div class="t1">${esc(p.name)}</div><div class="t2">${ids.length} 首歌曲</div></div></div>`;
    }).join('');
  }
  function renderAll(keep) {
    renderSongs(keep); renderAlbums(); renderArtists(); renderPlaylists();
    renderEmpty();
  }
  function renderEmpty() {
    const el = $('emptyState');
    const show = !tracks.length && !scanning;
    el.classList.toggle('hidden', !show);
    if (!show) return;
    const perm = A.hasPermission();
    const af = A.allFilesApplies() ? A.hasAllFiles() : true;
    const r = libReport || {};
    const dirs = (r.searched && r.searched.length) ? r.searched.join('、') : '下载 (Download)、音乐 (Music)';
    let lines = [];
    if (!perm) lines.push('还没有获得「音乐和音频」权限，所以读不到手机里的歌曲。');
    else {
      lines.push(r.at ? `已搜索：系统媒体库中的全部音频，以及 ${esc(dirs)}` + (r.folders ? `，和你添加的 ${r.folders} 个文件夹` : '') + '。' : '还没有扫描过。');
      lines.push('支持 FLAC、MP3、M4A/AAC、WAV、OGG、OPUS 等格式。');
      if (r.skipped) lines.push(`跳过了 ${r.skipped} 个暂不支持的文件（${esc(r.skippedExt || '')}）。`);
      if (!af) lines.push('如果歌曲刚下载还没出现，可以开启「所有文件访问」（可选），直接读取文件夹。');
    }
    el.innerHTML = `<p>还没有找到音乐</p><p class="sub">${lines.join('<br>')}</p>
      <div class="empty-btns">
        ${perm ? '<button class="btn primary" data-e="scan">重新扫描</button>' : '<button class="btn primary" data-e="perm">授予权限</button>'}
        <button class="btn ghost" data-e="addf">添加文件夹</button>
        ${perm && !af ? '<button class="btn ghost" data-e="allf">授权所有文件访问（可选）</button>' : ''}
        ${!perm ? '<button class="btn ghost" data-e="app">打开应用设置</button>' : ''}
      </div>`;
  }
  $('emptyState').addEventListener('click', (e) => {
    const b = e.target.closest('[data-e]'); if (!b) return;
    const a = b.dataset.e;
    if (a === 'scan') rescan();
    if (a === 'perm') A.requestPermission();
    if (a === 'addf') A.addFolder();
    if (a === 'allf') A.requestAllFiles();
    if (a === 'app') A.openAppSettings();
  });
  $('tabs').addEventListener('click', (e) => {
    const b = e.target.closest('button'); if (!b) return;
    curTab = b.dataset.tab;
    for (const x of $('tabs').children) x.classList.toggle('active', x === b);
    for (const p of ['songs', 'albums', 'artists', 'playlists']) $('pane-' + p).classList.toggle('hidden', p !== curTab);
    if (curTab === 'songs') songList.refresh();
  });
  $('albumGrid').addEventListener('click', (e) => { const c = e.target.closest('.card'); if (c) openAlbum(albums[+c.dataset.i]); });
  $('artistList').addEventListener('click', (e) => { const c = e.target.closest('.row'); if (c) openArtist(artists[+c.dataset.i]); });
  $('playlistList').addEventListener('click', (e) => { const c = e.target.closest('.row'); if (c) openPlaylist(playlists[+c.dataset.i].id); });
  $('playAllBtn').onclick = () => { const ids = sortedSongs().map((t) => t.id); if (!ids.length) return; A.setMode('all'); playList(ids, 0); };
  $('shuffleAllBtn').onclick = () => { if (!tracks.length) return; A.setMode('shuffle'); const ids = tracks.map((t) => t.id); playList(ids, Math.floor(Math.random() * ids.length)); };
  $('sortBtn').onclick = () => {
    sheet(`<div class="sh-head"><div class="t1">排序方式</div></div><div class="sh-body">` +
      Object.entries(SORTS).map(([k, s]) => `<button class="item ${prefs.sort === k ? 'sel' : ''}" data-k="${k}">${s.name}${prefs.sort === k ? '<span class="r">' + (prefs.dir < 0 ? '倒序' : '正序') + '</span>' : ''}</button>`).join('') +
      `<button class="item" data-k="__rev">切换正序 / 倒序</button></div>`, (b) => {
      const k = b.dataset.k; if (!k) return;
      if (k === '__rev') prefs.dir = -prefs.dir; else { prefs.sort = k; prefs.dir = 1; }
      kvSet('prefs', prefs); closeSheet(); renderSongs();
    });
  };
  $('newPlaylistBtn').onclick = () => newPlaylist();
  $('searchBtn').onclick = () => openPage('searchPage', () => { $('searchInput').value = ''; renderSearch(''); setTimeout(() => $('searchInput').focus(), 50); });
  $('settingsBtn').onclick = () => openPage('settingsPage', renderSettings);

  // ---------------- 页面栈 ----------------
  const stack = [];
  function openPage(id, init) {
    const el = $(id);
    if (stack[stack.length - 1] !== id) { const i = stack.indexOf(id); if (i >= 0) stack.splice(i, 1); stack.push(id); }
    el.classList.remove('hidden');
    el.style.zIndex = 20 + stack.length;
    if (init) init();
  }
  function closeTop() {
    if (!$('sheet').classList.contains('hidden')) { closeSheet(); return true; }
    if (!$('nowPage').classList.contains('hidden')) { closeNow(); return true; }
    const id = stack.pop(); if (!id) return false;
    $(id).classList.add('hidden');
    if (id === 'detailPage') { detailStack.pop(); const prev = detailStack[detailStack.length - 1]; if (prev) showDetail(prev, true); }
    return true;
  }
  $('detailBack').onclick = closeTop; $('searchBack').onclick = closeTop; $('settingsBack').onclick = closeTop;

  // ---------------- 详情页 ----------------
  const detailStack = [];
  let detailCtx = null;
  const detailList = VList($('detailScroll'), $('detailSpacer'), (t, i) => songRow(t, i, detailCtx && detailCtx.num ? { num: 1 } : null), (t, i, more) => {
    if (more) return songMenu(t, detailCtx); playList(detailList.items.map((x) => x.id), i);
  });
  function showDetail(ctx, again) {
    detailCtx = ctx;
    if (!again) detailStack.push(ctx);
    $('detailTitle').textContent = ctx.title;
    $('detailHead').innerHTML = `${coverHTML(ctx.coverId, ctx.name, ctx.round ? 'round' : '')}<div class="meta"><div class="name">${esc(ctx.name)}</div><div class="sub">${esc(ctx.sub)}</div><div class="acts"><button class="chip accent" id="dPlay">${ICON.play.replace('<svg', '<svg style="width:18px;height:18px"')}播放全部</button><button class="chip" id="dShuffle">随机</button></div></div>`;
    $('dPlay').onclick = () => { const ids = ctx.list().map((t) => t.id); if (ids.length) { A.setMode('all'); playList(ids, 0); } };
    $('dShuffle').onclick = () => { const ids = ctx.list().map((t) => t.id); if (ids.length) { A.setMode('shuffle'); playList(ids, Math.floor(Math.random() * ids.length)); } };
    $('detailMore').classList.toggle('hidden', !ctx.menu);
    $('detailMore').onclick = ctx.menu || null;
    detailList.set(ctx.list());
    openPage('detailPage');
  }
  function openAlbum(a) {
    if (!a) return;
    showDetail({ title: '专辑', name: a.name, sub: `${a.artist} · ${a.tracks.length} 首`, coverId: a.tracks[0].id, num: true, list: () => a.tracks });
  }
  function openArtist(r) {
    if (!r) return;
    const list = r.tracks.slice().sort((x, y) => cmp(x.album, y.album) || (x.trackNo - y.trackNo) || cmp(x.title, y.title));
    showDetail({ title: '歌手', name: r.name, sub: `${list.length} 首歌曲`, coverId: list[0].id, round: true, list: () => list });
  }
  function openPlaylist(pid) {
    const p = playlists.find((x) => x.id === pid); if (!p) return;
    const list = () => p.ids.map((id) => byId.get(id)).filter(Boolean);
    const l = list();
    showDetail({
      title: '歌单', name: p.name, sub: `${l.length} 首歌曲`, coverId: l[0] ? l[0].id : '', playlist: p, list,
      menu: () => sheet(`<div class="sh-head"><div class="t1">${esc(p.name)}</div></div><div class="sh-body">` +
        (p.id !== 'fav' ? `<button class="item" data-a="rename">重命名</button>` : '') +
        `<button class="item" data-a="clean">移除已不存在的歌曲</button>` +
        (p.id !== 'fav' ? `<button class="item danger" data-a="del">删除歌单</button>` : '') + '</div>', (b) => {
        const a = b.dataset.a; if (!a) return;
        if (a === 'rename') promptSheet('重命名歌单', p.name, (v) => { p.name = v; savePlaylists(); renderPlaylists(); closeTop(); openPlaylist(p.id); });
        if (a === 'clean') { p.ids = p.ids.filter((id) => byId.has(id)); savePlaylists(); closeSheet(); renderPlaylists(); detailList.set(list(), true); toast('已清理'); }
        if (a === 'del') confirmSheet(`删除歌单「${p.name}」？`, '歌曲文件不会被删除', () => { playlists = playlists.filter((x) => x !== p); savePlaylists(); renderPlaylists(); closeTop(); toast('已删除'); });
      }),
    }, false);
  }

  // ---------------- 传到电脑 ----------------
  // 2.2：服务在前台服务里运行（切到后台/锁屏也不停，通知栏可停止，空闲 30 分钟自动停止）；
  // 电脑连不进手机时可「改用手机连电脑」（手机主动连电脑 TCP 18522）。
  const KIND = { wifi: 'Wi-Fi', hotspot: '热点', eth: '有线/USB', vpn: 'VPN', mobile: '移动数据', other: '' };
  let pcFoundList = [];
  function syncSheet() {
    let info = {}; try { info = JSON.parse(A.syncStart()); } catch (e) {}
    if (!info.ok) { toast('开启失败，请重试'); return; }
    const addr = () => {
      let c = info.cands && info.cands.length ? info.cands : (info.ips || []).map((ip) => ({ ip, kind: 'wifi' }));
      if (hsOn(info)) c = c.filter((x) => x.kind === 'hotspot').concat(c.filter((x) => x.kind !== 'hotspot')); // 2.3: 开着热点时热点地址排第一
      if (!c.length) return '没连 WiFi？';
      return c.map((x, i) => `<div class="${i ? 'alt' : ''}">${esc(x.ip)}:${info.port}${KIND[x.kind] ? `<span class="k">${KIND[x.kind]}</span>` : ''}</div>`).join('');
    };
    const lastPc = info.lastPc || '';
    sheet(`<div class="sh-head"><div class="t1">传到电脑</div><div class="t2">在电脑版拾音点「从手机导入」，会自动找到这台手机</div></div>
      <button class="sync-jump" data-a="jump" id="syncJump">${info.vpn ? '手机开着 VPN，建议改用「手机连电脑」 ↓' : '电脑连不上？改用手机连电脑 ↓'}</button>
      <div class="sync-box">
        <div id="syncHs">${hsBox(info)}</div>
        <div class="sync-l">配对码</div><div class="sync-code">${esc(String(info.code).replace(/(\d{3})(\d{3})/, '$1 $2'))}</div>
        <div class="sync-l">找不到手机时在电脑上手动输入（有多个就逐个试）</div><div class="sync-ip" id="syncIp">${addr()}</div>
        <div class="sync-warn" id="syncVpn" style="${info.vpn ? '' : 'display:none'}">手机开着 VPN，电脑可能连不进来。连不上时请先关闭手机 VPN，或用下面的「手机连电脑」。</div>
        <div class="sync-st" id="syncSt">等待电脑连接…</div>
        <div class="sh-note">切到后台或锁屏也会继续传（通知栏可点「停止」，30 分钟没有传输会自动停止）。电脑只能读取，不能修改或删除手机上的歌。</div>
      </div>
      ${guideHtml(info)}
      <div class="sync-rev form" id="pcRev">
        <div class="sync-h">找不到？改用手机连电脑</div>
        <div class="sh-note">电脑搜不到手机、手动输入也连不上时（例如路由器开了“AP 隔离”、手机开着 VPN），可以让手机主动连电脑：在电脑拾音的「从手机导入」窗口里能看到电脑的 IP 和配对码。</div>
        <div class="btns" style="margin-top:8px"><button class="btn ghost" data-a="find" id="pcFind">搜索电脑</button></div>
        <div id="pcFound"></div>
        <label>电脑 IP</label><input id="pcHost" value="${esc(lastPc)}" placeholder="例如 192.168.0.103" inputmode="url">
        <div class="sync-subwarn" id="pcSubWarn" style="display:none"></div>
        <label>电脑上的配对码</label><input id="pcCode" maxlength="6" inputmode="numeric" placeholder="6 位数字">
        <div class="btns"><button class="btn primary" data-a="pcgo" id="pcGo">连接电脑</button></div>
        <div class="sync-st" id="pcSt" style="display:none"></div>
        <div class="btns" style="margin-top:14px"><button class="btn ghost" data-a="diag" id="netDiag">网络诊断</button></div>
        <div class="sh-note">用上面填的电脑 IP 检查 Wi-Fi、路由和端口，生成可复制的报告。</div>
        <div id="diagOut"></div>
      </div>
      <div class="form"><div class="btns"><button class="btn primary" data-a="stop">完成</button></div></div>`, (b) => {
      const a = b.dataset.a;
      if (a === 'stop') closeSheet();
      if (a === 'hsset') { try { A.openHotspotSettings(); } catch (e) { toast('请在系统设置里打开「个人热点」'); } }
      if (a === 'guide') { const g = $('syncGuide'); if (g) g.scrollIntoView({ block: 'start', behavior: 'smooth' }); }
      if (a === 'find') findPc();
      if (a === 'jump') { const r = $('pcRev'); if (r) r.scrollIntoView({ block: 'start', behavior: 'smooth' }); }
      if (a === 'diag') {
        const host = ($('pcHost').value || '').trim().replace('：', ':').split(':')[0];
        const out = $('diagOut'); if (out) out.innerHTML = '<div class="sh-note">正在诊断（约 10 秒）…</div>';
        try { A.netDiag(host); } catch (e) { if (out) out.innerHTML = '<div class="sh-note">这个版本不支持诊断</div>'; }
      }
      if (a === 'copydiag') { const t = $('diagRep'); if (t) { t.select(); try { document.execCommand('copy'); toast('已复制'); } catch (e) {} } }
      if (b.dataset.pc) { $('pcHost').value = b.dataset.pc; if (sheetSubnetCheck) sheetSubnetCheck(); const c = $('pcCode'); if (c) c.focus(); }
      if (a === 'pcgo') {
        const host = $('pcHost').value.trim(), code = $('pcCode').value.replace(/\D/g, '');
        if (!host) return pcStatus('请先搜索电脑，或输入电脑上显示的 IP', true);
        checkPcSubnet();
        if (code.length !== 6) return pcStatus('请输入电脑上显示的 6 位配对码', true);
        pcStatus('正在连接电脑…'); A.pcConnect(host, code);
      }
    });
    const ci = $('pcCode'); if (ci) ci.oninput = () => { ci.value = ci.value.replace(/\D/g, '').slice(0, 6); };
    const checkPcSubnet = () => { const w = $('pcSubWarn'), h = $('pcHost'); if (!w || !h) return; const m = subnetWarn(h.value, info.subnets || []); w.textContent = m; w.style.display = m ? '' : 'none'; };
    const hi = $('pcHost'); if (hi) { hi.addEventListener('input', checkPcSubnet); hi.addEventListener('change', checkPcSubnet); }
    sheetSubnetCheck = checkPcSubnet; checkPcSubnet();
    const timer = setInterval(() => {
      let st = {}; try { st = JSON.parse(A.syncStatus()); } catch (e) {}
      const el = $('syncSt'); if (!el) return;
      if (st.subnets) info.subnets = st.subnets;
      if (st.cands) { info.cands = st.cands; info.ips = st.ips; const ipEl = $('syncIp'); if (ipEl) ipEl.innerHTML = addr(); }
      const v = $('syncVpn'); if (v) v.style.display = st.vpn ? '' : 'none';
      const hsWas = hsOn(info); info.hotspot = st.hotspot; info.gw = st.gw;
      if (hsOn(info) !== hsWas || hsOn(info)) { const hb = $('syncHs'); if (hb) hb.innerHTML = hsBox(info); const g1 = $('gStep1'); if (g1) g1.outerHTML = step1Html(info); const ipEl = $('syncIp'); if (ipEl) ipEl.innerHTML = addr(); }
      const gw = $('gGw'); if (gw && st.gw) gw.textContent = st.gw;
      const rev = st.rev || {};
      if (!st.client && rev.state !== 'connected') {
        if (!hasLan(st)) suspect('手机现在没有连 Wi-Fi，也没开热点，电脑找不到这台手机。');
        else if (rev.state === 'error' && rev.msg && !/配对码/.test(rev.msg)) suspect('手机连不上电脑，可能是路由器开了 AP 隔离。');
      }
      if (!st.running && (!rev.state || rev.state === 'idle')) { el.textContent = (st.stopReason || '已停止') + '，关闭本页后可重新开启'; el.classList.remove('on'); }
      else if (rev.state === 'connected') { el.textContent = `已连上电脑 ${rev.pc} · 已传 ${st.served || 0} 首`; el.classList.add('on'); }
      else { el.textContent = st.client ? `已连接电脑 ${st.client} · 已传 ${st.served || 0} 首` : '等待电脑连接…'; el.classList.toggle('on', !!st.client); }
      if (rev.state === 'connected') pcStatus(`已连上电脑「${rev.pc}」。请在电脑上选择要导入的歌曲`, false, true);
      else if (rev.state === 'retrying') pcStatus('和电脑的连接断了，正在重连…（电脑上的「从手机导入」窗口要保持打开）');
      else if (rev.state === 'error' && rev.msg) pcStatus(rev.msg, true);
    }, 1000);
    sheetOnClose = () => { clearInterval(timer); sheetSubnetCheck = null; try { A.syncStop(); } catch (e) {} };
    if (!lastPc) findPc();
    if (!hasLan(info) && !hsOn(info)) suspect('手机现在没有连 Wi-Fi，也没开热点，电脑找不到这台手机。');
  }
  // 2.3：手机开热点 → 电脑连手机热点（推荐）；连不上 / 疑似 AP 隔离时按顺序给出办法（电脑热点放最后、不推荐）
  const hsOn = (i) => !!(i && i.hotspot && i.hotspot.on);
  const hasLan = (i) => (i.cands || []).some((x) => x.kind === 'wifi' || x.kind === 'hotspot' || x.kind === 'eth');
  function hsBox(i) {
    if (!hsOn(i)) return '';
    const ips = (i.hotspot.ips || []);
    return `<div class="sync-hs"><div class="sync-hs-t">手机热点已开启</div>
      <div class="sync-hs-h">电脑连上这台手机的热点后，点连接</div>
      <div class="sync-hs-ip">${ips.length ? ips.map((ip) => esc(ip + ':' + i.port)).join('<br>') : '（热点地址见下面的列表）'}</div>
      <div class="sh-note">在电脑拾音「从手机导入」里点「重新搜索」，或输入上面的地址和配对码，再点「连接手机」。传歌只在手机和电脑之间进行，不耗手机流量；电脑连着热点时自己上网会用手机流量，保险起见可以先关掉手机的移动数据。</div></div>`;
  }
  function step1Html(i) {
    return `<li id="gStep1"><div class="g-h">手机开热点，电脑连手机的热点 <span class="g-tag rec">推荐</span>${hsOn(i) ? ' <span class="g-tag ok">热点已开启</span>' : ''}</div>
      <div class="g-b">打开手机的<b>个人热点</b>，电脑连上这个热点，再在电脑拾音「从手机导入」里连接（地址和配对码见上面）。传歌<b>不耗手机流量</b>；保险起见可以先<b>关掉移动数据</b>，电脑就不会用手机流量上网。</div>
      ${hsOn(i) ? '' : '<div class="btns"><button class="btn ghost" data-a="hsset">打开热点设置</button></div>'}</li>`;
  }
  function guideHtml(i) {
    return `<div class="sync-guide" id="syncGuide">
      <div class="g-alert" id="gAlert" style="display:none"></div>
      <div class="sync-h">连不上？按这个顺序试</div>
      <div class="sh-note">同一个 Wi-Fi 还是连不上，多半是路由器开了 AP 隔离：</div>
      <ol class="g-list">${step1Html(i)}
        <li><div class="g-h">用数据线拷贝</div><div class="g-b">在「设置」里打开<b>「导出歌单和改名到拾音文件夹」</b>；数据线连电脑，手机选「传输文件」，把 <b>内部存储/Music/拾音</b> 整个文件夹复制到电脑的拾音文件夹，电脑拾音点「重新扫描」。</div></li>
        <li><div class="g-h">关闭路由器的 AP 隔离</div><div class="g-b">浏览器打开路由器管理页（<b id="gGw">${esc(i.gw || '例如 192.168.0.1')}</b>），在无线高级设置里关闭“AP 隔离”/“客户端隔离”；手机和电脑连同一个 Wi-Fi、同一个频段，不要连访客网络。</div></li>
        <li class="g-bad"><div class="g-h">电脑开热点 <span class="g-tag bad">不推荐</span></div><div class="g-b">部分笔记本的无线网卡驱动在开热点传大量文件时可能让电脑死机。上面都不行时再用，并先关掉电脑代理软件的 TUN 模式。</div></li>
      </ol></div>`;
  }
  function suspect(msg) {
    const el = $('gAlert'); if (!el || el.dataset.msg === msg) return;
    el.dataset.msg = msg; el.innerHTML = `<b>${esc(msg)}</b>请按下面的顺序换一种方式。`; el.style.display = '';
    const j = $('syncJump'); if (j && !j.dataset.g) { j.dataset.g = '1'; j.dataset.a = 'guide'; j.textContent = '连不上？看推荐的连接方式 ↓'; }
  }
  let sheetSubnetCheck = null;
  // 2.2.1: warn when the typed PC IP is on none of the phone's networks (e.g. 192.168.137.1 = Windows hotspot while the phone is on home Wi-Fi)
  function ip2n(ip) { const p = String(ip).trim().split('.'); if (p.length !== 4) return null; let n = 0; for (const x of p) { if (!/^\d{1,3}$/.test(x) || +x > 255) return null; n = n * 256 + +x; } return n; }
  function subnetWarn(host, subs) {
    const ip = String(host || '').trim().replace('：', ':').split(':')[0];
    const n = ip2n(ip); if (n == null || !subs || !subs.length) return '';
    const inSub = (s) => { const b = ip2n(s.ip); if (b == null) return false; const pf = Math.max(0, Math.min(32, s.prefix | 0)); const div = Math.pow(2, 32 - pf); return Math.floor(n / div) === Math.floor(b / div); };
    const lan = subs.filter((s) => s.kind !== 'vpn' && s.kind !== 'mobile');
    if (!lan.length || lan.some(inSub)) return '';
    const w = lan[0]; const pre = w.ip.split('.').slice(0, 3).join('.');
    if (ip.startsWith('192.168.137.')) return `这是电脑“移动热点”的地址，手机要先连上电脑的热点才能用。手机现在连的 Wi-Fi 上，电脑 IP 应该是 ${pre}.x 开头的那个`;
    return `这个 IP 和手机不在同一个网络，试试 ${pre}.x 开头的那个`;
  }
  App.onNetDiag = (rep) => {
    const out = $('diagOut'); if (!out) return;
    if (/路由器开了 AP 隔离/.test(rep)) suspect('诊断结果：路由器开了 AP 隔离（手机和电脑都没问题）。');
    out.innerHTML = `<textarea class="diag-rep" id="diagRep" readonly>${esc(rep)}</textarea><div class="btns"><button class="btn ghost" data-a="copydiag">复制报告</button></div>`;
  };
  function pcStatus(msg, err, ok) {
    const el = $('pcSt'); if (!el) return;
    el.style.display = msg ? '' : 'none'; el.textContent = msg || '';
    el.classList.toggle('on', !!ok); el.classList.toggle('err', !!err);
  }
  function findPc() {
    const box = $('pcFound'); if (!box) return;
    box.innerHTML = '<div class="sh-note">正在搜索局域网里的电脑…</div>';
    A.pcDiscover();
  }
  App.onPcFound = (json) => {
    const box = $('pcFound'); if (!box) return;
    try { pcFoundList = JSON.parse(json) || []; } catch (e) { pcFoundList = []; }
    if (!pcFoundList.length) { box.innerHTML = '<div class="sh-note">没有搜到电脑。请在电脑拾音打开「从手机导入」，或直接输入电脑上显示的 IP。</div>'; return; }
    box.innerHTML = pcFoundList.map((p) => `<button class="item" data-pc="${esc(p.ip + ':' + p.port)}">${esc(p.name || '电脑')}<span class="r">${esc(p.ip)}</span></button>`).join('');
    if (pcFoundList.length === 1) { const h = $('pcHost'); if (h) h.value = pcFoundList[0].ip + ':' + pcFoundList[0].port; if (sheetSubnetCheck) sheetSubnetCheck(); }
  };
  App.onPcConnect = (json) => {
    let r = {}; try { r = JSON.parse(json); } catch (e) {}
    if (r.ok) pcStatus(`已连上电脑「${r.pc}」。请在电脑上选择要导入的歌曲`, false, true);
    else pcStatus(r.msg || '连接失败', true);
  };

  // ---------------- 菜单 / 对话框 ----------------
  let sheetHandler = null;
  function sheet(html, onClick) {
    if (sheetOnClose) { const f = sheetOnClose; sheetOnClose = null; try { f(); } catch (e) {} }
    $('sheet').innerHTML = html; sheetHandler = onClick; delete $('sheet').dataset.kind;
    $('sheet').classList.remove('hidden'); $('sheetMask').classList.remove('hidden');
  }
  let sheetOnClose = null;
  function closeSheet() { $('sheet').classList.add('hidden'); $('sheetMask').classList.add('hidden'); $('sheet').innerHTML = ''; sheetHandler = null; const f = sheetOnClose; sheetOnClose = null; if (f) try { f(); } catch (e) {} }
  $('sheetMask').onclick = closeSheet;
  // 2.2.1: keep sheets inside the visible area (keyboard up = smaller visualViewport) and keep the focused
  // input visible; the sheet itself scrolls (style.css), nothing here blocks touch scrolling.
  const setVvh = () => { const h = (window.visualViewport && window.visualViewport.height) || window.innerHeight; if (h) document.documentElement.style.setProperty('--vvh', Math.round(h) + 'px'); };
  setVvh();
  if (window.visualViewport) window.visualViewport.addEventListener('resize', () => { setVvh(); const a = document.activeElement; if (a && a.closest && a.closest('#sheet')) setTimeout(() => { try { a.scrollIntoView({ block: 'center' }); } catch (e) {} }, 50); });
  window.addEventListener('resize', setVvh);
  $('sheet').addEventListener('focusin', (e) => { const t = e.target; if (t && /^(INPUT|TEXTAREA)$/.test(t.tagName)) setTimeout(() => { try { t.scrollIntoView({ block: 'center' }); } catch (x) {} }, 300); });
  $('sheet').addEventListener('click', (e) => { const b = e.target.closest('button,.res'); if (b && sheetHandler) sheetHandler(b, e); });
  function promptSheet(title, val, cb) {
    sheet(`<div class="sh-head"><div class="t1">${esc(title)}</div></div><div class="form"><input id="pIn" value="${esc(val || '')}" maxlength="60"><div class="btns"><button class="btn ghost" data-a="c">取消</button><button class="btn primary" data-a="ok">确定</button></div></div>`, (b) => {
      if (b.dataset.a === 'c') closeSheet();
      if (b.dataset.a === 'ok') { const v = $('pIn').value.trim(); if (!v) return toast('名称不能为空'); closeSheet(); cb(v); }
    });
    setTimeout(() => { const i = $('pIn'); if (i) { i.focus(); i.select(); } }, 60);
  }
  function confirmSheet(title, sub, cb) {
    sheet(`<div class="sh-head"><div class="t1">${esc(title)}</div><div class="t2">${esc(sub || '')}</div></div><div class="form"><div class="btns"><button class="btn ghost" data-a="c">取消</button><button class="btn primary" data-a="ok">确定</button></div></div>`, (b) => {
      if (b.dataset.a === 'c') closeSheet(); if (b.dataset.a === 'ok') { closeSheet(); cb(); }
    });
  }
  function newPlaylist(thenAdd) {
    promptSheet('新建歌单', '', (name) => {
      const p = { id: 'p' + Date.now().toString(36), name, ids: thenAdd ? [thenAdd] : [], created: Date.now() };
      playlists.push(p); savePlaylists(); renderPlaylists(); toast(thenAdd ? `已添加到「${name}」` : `已创建「${name}」`);
    });
  }
  function addToPlaylistSheet(t) {
    sheet(`<div class="sh-head"><div class="t1">添加到歌单</div><div class="t2">${esc(t.title)}</div></div><div class="sh-body"><button class="item" data-p="__new">＋ 新建歌单</button>` +
      playlists.map((p) => `<button class="item" data-p="${esc(p.id)}">${esc(p.name)}<span class="r">${p.ids.includes(t.id) ? '已添加' : p.ids.length + ' 首'}</span></button>`).join('') + '</div>', (b) => {
      const pid = b.dataset.p; if (!pid) return;
      closeSheet();
      if (pid === '__new') return newPlaylist(t.id);
      const p = playlists.find((x) => x.id === pid);
      if (p.ids.includes(t.id)) return toast('歌曲已在歌单中');
      p.ids.push(t.id); savePlaylists(); renderPlaylists(); updateFav(); toast(`已添加到「${p.name}」`);
    });
  }
  function songMenu(t, ctx) {
    const inPl = ctx && ctx.playlist;
    sheet(`<div class="sh-head"><div class="t1">${esc(t.title)}</div><div class="t2">${esc(t.artist)} · ${esc(t.album)}</div></div><div class="sh-body">
      <button class="item" data-a="next">下一首播放</button>
      <button class="item" data-a="queue">添加到播放队列</button>
      <button class="item" data-a="pl">添加到歌单</button>
      ${inPl ? '<button class="item" data-a="rm">从此歌单移除</button>' : ''}
      <button class="item" data-a="album">查看专辑<span class="r">${esc(t.album)}</span></button>
      <button class="item" data-a="artist">查看歌手<span class="r">${esc(t.artist)}</span></button>
      <button class="item" data-a="edit">编辑歌名 / 歌手 / 专辑<span class="r">可联网识别中文名</span></button>
      <button class="item" data-a="cover">更换封面</button>
      <button class="item" data-a="info">歌曲信息</button></div>`, (b) => {
      const a = b.dataset.a; if (!a) return;
      if (a === 'edit') return editSheet(t);
      if (a === 'cover') return coverSheet(t);
      if (a === 'next') { A.playNext(t.id); closeSheet(); toast('将在下一首播放'); }
      if (a === 'queue') { A.addToQueue(t.id); closeSheet(); toast('已添加到播放队列'); }
      if (a === 'pl') addToPlaylistSheet(t);
      if (a === 'rm') { const p = ctx.playlist; p.ids = p.ids.filter((x) => x !== t.id); savePlaylists(); renderPlaylists(); updateFav(); closeSheet(); detailList.set(ctx.list(), true); toast('已移除'); }
      if (a === 'album') { closeSheet(); closeNowIfOpen(); openAlbum(albums.find((x) => x.tracks.includes(t))); }
      if (a === 'artist') { closeSheet(); closeNowIfOpen(); const name = splitArtists(t.artist)[0]; openArtist(artists.find((x) => x.name === name)); }
      if (a === 'info') {
        sheet(`<div class="sh-head"><div class="t1">歌曲信息</div></div><div class="sh-body">` +
          [['标题', t.title], ['歌手', t.artist], ['专辑', t.album], ...(t.edited ? [['原标签', `${t.rTitle} · ${t.rArtist} · ${t.rAlbum}`]] : []), ['时长', fmt(t.duration)], ['音轨', t.trackNo || '-'], ['大小', (t.size / 1048576).toFixed(1) + ' MB'], ['位置', t.path || t.folder]]
            .map(([k, v]) => `<div class="item"><span style="color:var(--fg2);flex:none;width:44px">${k}</span><span style="word-break:break-all">${esc(v)}</span></div>`).join('') + '</div>', () => {});
      }
    });
  }

  // ---------------- 中文名识别 / 编辑信息 / 封面 ----------------
  const hasCjk = (x) => /[\u3400-\u9fff]/.test(String(x || ''));
  const toZh = (x) => (t2sOn && window.T2S ? (() => { try { return window.T2S.convert(String(x || '')); } catch (e) { return String(x || ''); } })() : String(x || ''));
  const needsZh = (t) => !t.edited && !t.tried && !(hasCjk(t.rTitle) && hasCjk(t.rArtist));
  const sameRawAlbum = (a, b) => a.rAlbum === b.rAlbum && a.albumId === b.albumId && a.rAlbum !== '未知专辑';
  function reloadKeep() {
    loadLibrary(); renderAll(true);
    if (!$('settingsPage').classList.contains('hidden')) renderSettings();
    if (!$('detailPage').classList.contains('hidden') && detailCtx) detailList.set(detailCtx.list(), true);
    if (curTrack()) { const t = curTrack(); $('miniTitle').textContent = t.title; $('miniArtist').textContent = t.artist; $('nowTitle').textContent = t.title; $('nowArtist').textContent = `${t.artist} · ${t.album}`; }
  }
  let matchReq = 0; const matchCbs = new Map();
  function match(t, title, artist, cb) { const id = ++matchReq; matchCbs.set(id, cb); A.matchOnline(t.id, title || '', artist || '', id); }
  App.onMatch = (req, json) => { const cb = matchCbs.get(req); matchCbs.delete(req); let r = {}; try { r = JSON.parse(json); } catch (e) {} if (cb) cb(r); };
  const thumb = (u) => String(u || '').replace('600x600bb', '100x100bb');

  /** 应用一个识别结果；同专辑 / 同歌手里还没改过的歌，专辑名和歌手名一起换成中文 */
  function applyMatch(t, c, opts) {
    const src = opts.src || 'online';
    const title = toZh(c.title), artist = toZh(c.artist), album = toZh(c.album);
    A.setMeta(t.id, title, artist, album, src);
    if (opts.spread !== false) {
      for (const u of tracks) {
        if (u === t || u.edited) continue;
        if (album && sameRawAlbum(u, t)) A.setMetaField(u.id, 'album', album, src);
        if (artist && u.rArtist === t.rArtist && !hasCjk(u.rArtist) && hasCjk(artist)) A.setMetaField(u.id, 'artist', artist, src);
      }
    }
    if (c.art && (opts.cover === true || (opts.cover === 'ifMissing' && !A.hasArt(t.id)))) {
      const ids = [t.id];
      if (opts.cover === 'ifMissing') for (const u of tracks) if (u !== t && sameRawAlbum(u, t) && !u.cv && !A.hasArt(u.id)) ids.push(u.id);
      A.setCoverUrl(JSON.stringify(ids), c.art);
    }
  }

  // 批量识别：逐首查询（Apple 目录有频率限制，每首约 3 秒）
  const batch = { running: false, list: [], i: 0, ok: 0 };
  function startBatch() {
    batch.list = tracks.filter(needsZh); batch.i = 0; batch.ok = 0;
    if (!batch.list.length) return toast('没有需要识别的歌曲');
    batch.running = true; $('taskBanner').classList.remove('hidden');
    batchNext();
  }
  function stopBatch() { batch.running = false; $('taskBanner').classList.add('hidden'); reloadKeep(); toast(`已停止：识别了 ${batch.ok} 首`); }
  function batchNext() {
    if (!batch.running) return;
    if (batch.i >= batch.list.length) {
      batch.running = false; $('taskBanner').classList.add('hidden'); reloadKeep();
      toast(`识别完成：${batch.ok} 首改成了中文名` + (batch.list.length - batch.ok ? `，${batch.list.length - batch.ok} 首没找到（可在歌曲菜单里手动改）` : ''), 5000);
      return;
    }
    const t = batch.list[batch.i];
    const left = Math.ceil((batch.list.length - batch.i) * 3.5 / 60);
    $('taskText').textContent = `正在识别中文名 ${batch.i + 1}/${batch.list.length}（已改 ${batch.ok} 首，约 ${left} 分钟）`;
    match(t, '', '', (r) => {
      if (!batch.running) return;
      if (r.ok && r.best >= 0) {
        // 前面的歌可能已经把这首的歌手/专辑同步成中文了，这里用整首的识别结果覆盖
        applyMatch(t, r.items[r.best], { src: 'online', cover: 'ifMissing' }); batch.ok++;
      } else if (r.ok) A.markTried(t.id);
      else if (/频繁/.test(r.error || '')) { $('taskText').textContent = '查询太频繁，休息一分钟后继续…'; setTimeout(batchNext, 60000); return; }
      batch.i++;
      if (batch.i % 10 === 0) reloadKeep();
      batchNext();
    });
  }
  $('taskCancel').onclick = (e) => { e.stopPropagation(); stopBatch(); };

  function editSheet(t) {
    let cands = [];
    sheet(`<div class="sh-head"><div class="t1">编辑歌曲信息</div><div class="t2">只改拾音里的显示，不修改音乐文件${t.edited ? `<br>原标签：${esc(t.rTitle)} · ${esc(t.rArtist)} · ${esc(t.rAlbum)}` : ''}</div></div>
      <div class="form"><label>歌名</label><input id="edTitle" value="${esc(t.title)}"><label>歌手</label><input id="edArtist" value="${esc(t.artist)}"><label>专辑</label><input id="edAlbum" value="${esc(t.album)}">
      <label class="chk"><input type="checkbox" id="edSpread" checked> 同步到同歌手、同专辑的其他歌曲</label>
      <div class="btns"><button class="btn ghost" data-a="net">联网识别</button>${t.edited ? '<button class="btn ghost" data-a="reset">恢复原名</button>' : ''}<button class="btn primary" data-a="save">保存</button></div></div>
      <div class="sh-body" id="edRes"></div>`, (b) => {
      const a = b.dataset.a;
      if (a === 'net') {
        $('edRes').innerHTML = '<div class="sh-note"><span class="spinner" style="display:inline-block;vertical-align:middle"></span> 正在查找…</div>';
        const ti = t.rTitle, ar = t.rArtist;
        match(t, ti, ar === '未知歌手' ? '' : ar, (r) => {
          if (!$('edRes')) return;
          if (!r.ok) { $('edRes').innerHTML = `<div class="sh-note">查找失败：${esc(r.error || '')}</div>`; return; }
          cands = r.items || [];
          if (r.best > 0) cands.unshift(cands.splice(r.best, 1)[0]);
          $('edRes').innerHTML = cands.length ? cands.slice(0, 12).map((c, i) => {
            const diff = t.duration && c.duration ? Math.round((c.duration - t.duration) / 1000) : null;
            return `<button class="res mres" data-i="${i}">${c.art ? `<img src="${esc(thumb(c.art))}" alt="">` : '<span class="ph"></span>'}<span class="mtxt"><span class="t1">${esc(toZh(c.title))}${i === 0 && r.best >= 0 ? '<span class="tag">最匹配</span>' : ''}</span><span class="t2">${esc(toZh(c.artist))} · ${esc(toZh(c.album))} · ${fmt(c.duration)}${diff !== null ? `（${diff >= 0 ? '+' : ''}${diff}s）` : ''}</span></span></button>`;
          }).join('') + '<div class="sh-note">点一个结果填入上面，再点「保存」</div>' : '<div class="sh-note">没有找到，可以直接手动输入中文名</div>';
        });
      }
      if (b.classList.contains('mres')) {
        const c = cands[+b.dataset.i]; if (!c) return;
        $('edTitle').value = toZh(c.title); $('edArtist').value = toZh(c.artist); $('edAlbum').value = toZh(c.album);
        $('edRes').dataset.art = c.art || '';
        toast('已填入，确认后点保存');
      }
      if (a === 'reset') { A.clearMeta(t.id); closeSheet(); reloadKeep(); toast('已恢复原名'); }
      if (a === 'save') {
        const ti = $('edTitle').value.trim(), ar = $('edArtist').value.trim(), al = $('edAlbum').value.trim();
        if (!ti) return toast('歌名不能为空');
        const art = $('edRes').dataset.art || '';
        applyMatch(t, { title: ti, artist: ar, album: al, art }, { src: 'manual', spread: $('edSpread').checked, cover: art ? 'ifMissing' : false });
        closeSheet(); reloadKeep(); toast('已保存');
      }
    });
  }

  function coverSheet(t) {
    const album = albums.find((x) => x.tracks.includes(t));
    const n = album ? album.tracks.length : 1;
    let cands = [];
    sheet(`<div class="sh-head"><div class="t1">更换封面</div><div class="t2">${esc(t.title)}</div></div>
      <div class="form">${n > 1 ? `<label class="chk"><input type="checkbox" id="cvAlbum" checked> 用于整张专辑「${esc(t.album)}」（${n} 首）</label>` : ''}</div>
      <div class="sh-body">
      <button class="item" data-a="pick">从相册 / 文件选择图片</button>
      <button class="item" data-a="net">在线查找封面</button>
      ${t.cv ? '<button class="item" data-a="reset">恢复默认封面</button>' : ''}
      <div id="cvRes" class="cv-grid"></div></div>`, (b) => {
      const ids = () => JSON.stringify(($('cvAlbum') && $('cvAlbum').checked && album) ? album.tracks.map((x) => x.id) : [t.id]);
      const a = b.dataset.a;
      if (a === 'pick') { coverPending = true; A.pickCover(ids()); }
      if (a === 'reset') { A.resetCover(ids()); closeSheet(); afterCover(); toast('已恢复默认封面'); }
      if (a === 'net') {
        $('cvRes').innerHTML = '<div class="sh-note"><span class="spinner" style="display:inline-block;vertical-align:middle"></span> 正在查找…</div>';
        const ar = t.artist === '未知歌手' ? '' : t.artist;
        match(t, `${t.album !== '未知专辑' ? t.album : t.title}`, ar, (r) => {
          if (!$('cvRes')) return;
          const seen = new Set();
          cands = (r.items || []).filter((c) => c.art && !seen.has(c.art) && seen.add(c.art));
          if (!cands.length) {
            match(t, t.rTitle, t.rArtist === '未知歌手' ? '' : t.rArtist, (r2) => {
              if (!$('cvRes')) return;
              cands = (r2.items || []).filter((c) => c.art && !seen.has(c.art) && seen.add(c.art));
              showCands();
            });
          } else showCands();
        });
      }
      if (b.classList.contains('cvc')) {
        const c = cands[+b.dataset.i]; if (!c) return;
        coverPending = true; A.setCoverUrl(ids(), c.art); toast('正在设置封面…');
      }
    });
    function showCands() {
      $('cvRes').innerHTML = cands.length ? cands.slice(0, 12).map((c, i) => `<button class="res cvc" data-i="${i}"><img src="${esc(thumb(c.art).replace('100x100bb', '300x300bb'))}" alt=""><span>${esc(toZh(c.album))}</span></button>`).join('') : '<div class="sh-note">没有找到，可以从相册选择</div>';
    }
  }
  let coverPending = false;
  function afterCover() {
    noCover.clear(); reloadKeep();
    const t = curTrack(); if (t) onTrackChange();
  }
  App.onCoverChanged = (ok, msg) => {
    if (!coverPending && ok) { afterCover(); return; } // 批量识别时自动补的封面
    coverPending = false;
    if (ok) { closeSheet(); afterCover(); toast('封面已更换'); } else if (msg) toast(msg);
  };

  // ---------------- 搜索 ----------------
  let searchTimer = 0;
  $('searchInput').addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => renderSearch($('searchInput').value), 120); });
  let searchSongs = [];
  function renderSearch(q) {
    q = q.trim().toLowerCase();
    const box = $('searchResults');
    if (!q) { box.innerHTML = `<div class="sh-note">输入关键词搜索 ${tracks.length} 首歌曲</div>`; return; }
    const qz = zh(q); // 转简体后，输入繁体关键词也能搜到
    const hit = (s) => { const v = String(s).toLowerCase(); return v.includes(q) || v.includes(qz); };
    searchSongs = tracks.filter((t) => hit(t.title) || hit(t.artist) || hit(t.album)).slice(0, 200);
    const ar = artists.filter((r) => hit(r.name)).slice(0, 10);
    const al = albums.filter((a) => hit(a.name)).slice(0, 10);
    let h = '';
    if (ar.length) h += '<div class="sec-title">歌手</div>' + ar.map((r) => `<div class="row" data-k="ar" data-n="${esc(r.name)}">${coverHTML(r.tracks[0].id, r.name, 'round')}<div class="txt"><div class="t1">${esc(r.name)}</div><div class="t2">${r.tracks.length} 首</div></div></div>`).join('');
    if (al.length) h += '<div class="sec-title">专辑</div>' + al.map((a) => `<div class="row" data-k="al" data-n="${esc(a.key)}">${coverHTML(a.tracks[0].id, a.name)}<div class="txt"><div class="t1">${esc(a.name)}</div><div class="t2">${esc(a.artist)}</div></div></div>`).join('');
    if (searchSongs.length) h += '<div class="sec-title">歌曲</div>' + searchSongs.map((t, i) => songRow(t, i).replace('<div class="row', `<div data-k="s" data-i="${i}" class="row`)).join('');
    box.innerHTML = h || '<div class="sh-note">没有找到相关内容</div>';
  }
  $('searchResults').addEventListener('click', (e) => {
    const r = e.target.closest('.row'); if (!r) return;
    const k = r.dataset.k;
    if (k === 'ar') openArtist(artists.find((x) => x.name === r.dataset.n));
    if (k === 'al') openAlbum(albums.find((x) => x.key === r.dataset.n));
    if (k === 's') {
      const t = searchSongs[+r.dataset.i];
      if (e.target.closest('.more')) return songMenu(t);
      playList(searchSongs.map((x) => x.id), +r.dataset.i);
    }
  });

  // ---------------- 设置 ----------------
  function renderSettings() {
    let folders = [];
    try { folders = JSON.parse(A.getFolders()); } catch (e) {}
    const allFiles = A.hasAllFiles(), battery = A.isIgnoringBattery();
    let lib = {}; try { lib = JSON.parse(A.getLibrary()); } catch (e) {}
    const when = lib.scannedAt ? new Date(lib.scannedAt).toLocaleString('zh-CN', { hour12: false }) : '从未';
    const outside = A.outsideCount(), autoOrg = kvGet('autoOrganize', false);
    let exp = { on: true, error: '' }; try { exp = JSON.parse(A.exportStatus()); } catch (e) {}
    const pending = tracks.filter(needsZh).length, autoCount = tracks.filter((t) => t.edited).length;
    $('settingsBody').innerHTML = `
      <div class="set-title">音乐库</div>
      <div class="set-group">
        <button class="set-item" data-a="scan"><div class="txt"><div>重新扫描</div><div class="t2">共 ${tracks.length} 首 · ${albums.length} 张专辑 · ${artists.length} 位歌手<br>上次扫描：${esc(when)}<br>音乐库会保存下来，打开时不再自动扫描；下载新歌后点这里</div></div></button>
        <button class="set-item" data-a="addf"><div class="txt"><div>添加文件夹</div><div class="t2">把媒体库扫描不到的文件夹也加入音乐库（同目录的 .lrc 歌词也会被读取）</div></div><span class="r">＋</span></button>
        <button class="set-item" data-a="org"><div class="txt"><div>整理到拾音文件夹</div><div class="t2">把歌曲和同名歌词文件移到 ${esc(A.homePath())}，方便管理。${outside ? `还有 ${outside} 首不在里面` : '全部歌曲都已在里面'}</div></div><span class="r">${outside ? '整理' : '✓'}</span></button>
        <button class="set-item" data-a="autoorg"><div class="txt"><div>扫描后自动整理新歌</div><div class="t2">手动扫描找到的新歌自动移入拾音文件夹</div></div><span class="r ${autoOrg ? 'ok' : ''}">${autoOrg ? '已开启' : '已关闭'}</span></button>
        <button class="set-item" data-a="zh"><div class="txt"><div>识别中文歌名</div><div class="t2">英文歌名/歌手名的华语歌，联网找到对应的中文名和封面（不修改文件）。${batch.running ? `进行中 ${batch.i}/${batch.list.length}` : `待识别 ${pending} 首`}</div></div><span class="r">${batch.running ? '停止' : '开始'}</span></button>
        ${autoCount ? `<button class="set-item" data-a="zhundo"><div class="txt"><div>撤销自动识别</div><div class="t2">把 ${autoCount} 首自动改过名字的歌恢复成文件里的原名（手动改的不受影响）</div></div></button>` : ''}
        ${folders.map((f) => `<button class="set-item" data-a="rmf" data-u="${esc(f.uri)}"><div class="txt"><div>${esc(f.label)}</div><div class="t2">已添加的文件夹 · 点按移除</div></div><span class="r">移除</span></button>`).join('')}
      </div>
      <div class="set-title">电脑</div>
      <div class="set-group">
        <button class="set-item" data-a="pc"><div class="txt"><div>传到电脑</div><div class="t2">手机和电脑连同一个 WiFi，用电脑版拾音一键导入全部歌曲、歌词、中文名、封面和歌单</div></div><span class="r">开启</span></button>
        <button class="set-item" data-a="export"><div class="txt"><div>导出歌单和改名到拾音文件夹</div><div class="t2">把歌单、我喜欢、改过的歌名和封面存成拾音文件夹里的「拾音数据.json」。用数据线把「拾音」文件夹拷到电脑，电脑版扫描时会自动读取${exp.error ? `<br><span style="color:#ff6b6b">${esc(exp.error)}</span>` : ''}</div></div><span class="r ${exp.on ? 'ok' : ''}">${exp.on ? '已开启' : '已关闭'}</span></button>
      </div>
      <div class="set-title">外观</div>
      <div class="set-group">
        <button class="set-item" data-a="theme"><div class="txt"><div>主题</div><div class="t2">深色或浅色，点按切换</div></div><span class="r">${theme === 'light' ? '浅色' : '深色'}</span></button>
      </div>
      <div class="set-title">歌词与权限</div>
      <div class="set-group">
        <button class="set-item" data-a="t2s"><div class="txt"><div>歌词转简体</div><div class="t2">把繁体歌词、歌名、歌手和专辑名显示为简体中文（不修改文件）</div></div><span class="r ${t2sOn ? 'ok' : ''}">${t2sOn ? '已开启' : '已关闭'}</span></button>
        <button class="set-item" data-a="perm"><div class="txt"><div>音乐和音频权限</div><div class="t2">读取手机里的歌曲必须开启</div></div><span class="r ${A.hasPermission() ? 'ok' : ''}">${A.hasPermission() ? '已授权' : '去授权'}</span></button>
        <button class="set-item" data-a="allf"><div class="txt"><div>所有文件访问（可选）</div><div class="t2">开启后会直接读取下载、音乐等文件夹，媒体库还没收录的歌曲也能找到，并能读取歌曲旁边的 .lrc 歌词。不开启也能正常使用</div></div><span class="r ${allFiles ? 'ok' : ''}">${allFiles ? '已开启' : '去开启'}</span></button>
        <button class="set-item" data-a="bat"><div class="txt"><div>后台播放不被打断</div><div class="t2">关闭电池优化，避免息屏后被系统停止。澎湃 OS 还可在「应用信息 → 省电策略」中选择「无限制」</div></div><span class="r ${battery ? 'ok' : ''}">${battery ? '已关闭优化' : '去设置'}</span></button>
        <button class="set-item" data-a="app"><div class="txt"><div>应用信息</div><div class="t2">权限、通知、省电策略</div></div><span class="r">›</span></button>
      </div>
      <div class="set-title">其他</div>
      <div class="set-group">
        <button class="set-item" data-a="upd"><div class="txt"><div>检查更新</div><div class="t2">当前版本 ${esc(A.version())}<span id="updSetMsg" class="${upd.msgCls}">${upd.msg ? ' · ' + esc(upd.msg) : ''}</span></div></div><span class="r ${upd.info ? 'ok' : ''}" id="updSetR">${updSetLabel()}</span></button>
        <button class="set-item" data-a="cover"><div class="txt"><div>清除封面缓存</div><div class="t2">更换了歌曲封面后可用</div></div></button>
        <div class="set-item"><div class="txt"><div>拾音 ${esc(A.version())}</div><div class="t2">本地播放器，只播放手机中的音乐。在线歌词来自 QQ音乐、酷狗、网易云和 LRCLIB；中文歌名和封面来自 Apple Music 目录。</div></div></div>
      </div>`;
  }
  $('settingsBody').addEventListener('click', (e) => {
    const b = e.target.closest('[data-a]'); if (!b) return;
    const a = b.dataset.a;
    if (a === 'scan') { rescan(); }
    if (a === 'org') { if (A.outsideCount()) organize(false); else toast('全部歌曲都已在拾音文件夹'); }
    if (a === 'pc') { syncSheet(); return; }
    if (a === 'export') { const on = !kvGet('exportData', true); kvSet('exportData', on); if (on) A.exportNow(); renderSettings(); toast(on ? '已开启：歌单和改名会保存到拾音文件夹' : '已关闭导出'); }
    if (a === 'autoorg') { kvSet('autoOrganize', !kvGet('autoOrganize', false)); renderSettings(); }
    if (a === 'zh') { if (batch.running) stopBatch(); else startBatch(); renderSettings(); }
    if (a === 'zhundo') confirmSheet('撤销自动识别？', '自动改成的中文名会恢复成文件里的原名', () => { const n = A.clearAutoMeta(); reloadKeep(); toast(`已恢复 ${n} 首`); });
    if (a === 't2s') {
      t2sOn = !t2sOn; kvSet('t2sLyrics', t2sOn);
      loadLibrary(); renderAll(true); renderSettings();
      if (curTrack()) onTrackChange(); // 刷新正在播放的标题，并按新设置重新显示歌词
      toast(t2sOn ? '歌词将显示为简体中文' : '歌词按原文显示');
    }
    if (a === 'theme') {
      theme = theme === 'light' ? 'dark' : 'light'; kvSet('theme', theme);
      applyTheme(); renderSettings();
      if (curTrack()) onTrackChange();
    }
    if (a === 'addf') A.addFolder();
    if (a === 'rmf') confirmSheet('移除这个文件夹？', '文件不会被删除，只是不再加入音乐库', () => { A.removeFolder(b.dataset.u); renderSettings(); rescan(); });
    if (a === 'allf') A.requestAllFiles();
    if (a === 'perm') { if (A.hasPermission()) A.openAppSettings(); else A.requestPermission(); }
    if (a === 'bat') A.requestIgnoreBattery();
    if (a === 'app') A.openAppSettings();
    if (a === 'upd') { if (upd.busy || upd.info) showUpdateSheet(); else checkUpdate(); }
    if (a === 'cover') { A.clearCoverCache(); noCover.clear(); toast('封面缓存已清除'); setTimeout(() => renderAll(true), 400); }
  });


  // ---------------- 应用内更新 ----------------
  const upd = { info: null, checking: false, busy: false, pct: 0, bytes: 0, phase: '', msg: '', msgCls: '', seq: 0, timer: 0 };
  const mb = (n) => (n / 1048576).toFixed(1) + ' MB';
  function updSetLabel() {
    if (upd.busy) return upd.pct >= 0 ? `正在下载 ${upd.pct}%` : '正在下载…';
    if (upd.checking) return '检查中…';
    if (upd.info) return `有新版本 ${esc(upd.info.versionName)}`;
    return '›';
  }
  function refreshUpdSet() {
    const r = $('updSetR'); if (r) { r.innerHTML = updSetLabel(); r.classList.toggle('ok', !!upd.info); }
    const m = $('updSetMsg'); if (m) { m.textContent = upd.msg ? ' · ' + upd.msg : ''; m.className = upd.msgCls; }
  }
  function setUpdMsg(msg, cls) { upd.msg = msg; upd.msgCls = cls || ''; refreshUpdSet(); }
  function checkUpdate() {
    if (upd.checking) return;
    upd.checking = true; setUpdMsg('检查中…');
    clearTimeout(upd.timer);
    // 兜底：原生端 12 秒内一定会回调；15 秒还没结果就当失败，避免界面一直卡在「检查中」
    upd.timer = setTimeout(() => {
      if (!upd.checking) return;
      upd.checking = false; setUpdMsg('检查失败：连接超时', 'err'); toast('检查更新失败：连接超时');
    }, 15000);
    try { A.checkUpdate(false); } catch (e) { clearTimeout(upd.timer); upd.checking = false; setUpdMsg('检查失败：' + (e && e.message || '内部错误'), 'err'); toast('检查更新失败'); }
  }
  function showBanner() {
    if (!upd.info) return;
    $('updBannerText').textContent = `有新版本 ${upd.info.versionName}`;
    $('updBanner').classList.remove('hidden');
  }
  function hideBanner() { $('updBanner').classList.add('hidden'); }
  $('updBanner').addEventListener('click', (e) => {
    if (e.target.closest('#updBannerClose')) { hideBanner(); return; }
    showUpdateSheet();
  });
  function updProgHTML() {
    if (!upd.busy && upd.phase !== 'installing') return '';
    const indet = upd.busy && upd.pct < 0;
    const w = upd.phase === 'installing' ? 100 : Math.max(0, upd.pct);
    const lbl = upd.phase === 'installing' ? '校验通过，正在打开安装…' : (upd.pct >= 0 ? `正在下载 ${upd.pct}% · ${mb(upd.bytes)}` : `正在下载 · ${mb(upd.bytes)}`);
    return `<div class="upd-prog"><div class="bar ${indet ? 'indet' : ''}"><i id="updBar" style="width:${indet ? '' : w + '%'}"></i></div><div class="lbl" id="updLbl">${lbl}</div></div>`;
  }
  function showUpdateSheet() {
    const i = upd.info; if (!i) return;
    const btns = upd.busy
      ? '<button class="btn ghost" data-a="later">后台下载</button><button class="btn ghost" data-a="cancel">取消下载</button>'
      : '<button class="btn ghost" data-a="later">稍后</button><button class="btn primary" data-a="go">立即更新</button>';
    sheet(`<div class="sh-head"><div class="t1">发现新版本 ${esc(i.versionName)}</div><div class="t2">当前版本 ${esc(A.version())}</div></div>
      <div class="sh-body"><div class="upd-notes">${esc(i.notes || '修复问题，提升体验')}</div></div>
      <div id="updProg">${updProgHTML()}</div>
      <div class="form"><div class="btns" id="updBtns">${btns}</div></div>`, (b) => {
      const a = b.dataset.a;
      if (a === 'later') closeSheet();
      if (a === 'cancel') { A.cancelUpdate(); }
      if (a === 'go') {
        upd.busy = true; upd.pct = -1; upd.bytes = 0; upd.phase = 'download';
        A.startUpdate(); showUpdateSheet(); refreshUpdSet();
      }
    });
    $('sheet').dataset.kind = 'update';
  }
  const updSheetOpen = () => !$('sheet').classList.contains('hidden') && $('sheet').dataset.kind === 'update' && $('updProg');
  App.onUpdateCheck = (json, silent) => {
    let r = {}; try { r = JSON.parse(json); } catch (e) { r = { ok: false, message: '返回数据无法解析' }; }
    const hhmm = () => new Date().toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' });
    if (!silent) { upd.checking = false; clearTimeout(upd.timer); }
    if (r.ok && r.hasUpdate) {
      upd.info = { versionCode: r.versionCode, versionName: r.versionName, notes: r.notes };
      if (!silent) { upd.msg = `发现新版本 ${r.versionName}`; upd.msgCls = 'ok'; }
      if (silent) showBanner(); else { hideBanner(); showUpdateSheet(); }
    } else if (r.ok) {
      upd.info = null; hideBanner();
      if (!silent) { upd.msg = `已是最新版本（${hhmm()} 检查）`; upd.msgCls = 'ok'; toast('已是最新版本'); }
    } else if (!silent) {
      const why = r.message || '请检查网络';
      upd.msg = '检查失败：' + why; upd.msgCls = 'err';
      toast('检查更新失败：' + why, 4000);
    }
    refreshUpdSet();
  };
  App.onUpdateStatus = (kind, pct, bytes, msg) => {
    if (kind === 'progress') {
      upd.pct = pct; upd.bytes = bytes;
      if (updSheetOpen()) {
        const bar = $('updBar'), lbl = $('updLbl');
        if (bar && pct >= 0) { bar.parentNode.classList.remove('indet'); bar.style.width = pct + '%'; }
        if (lbl) lbl.textContent = pct >= 0 ? `正在下载 ${pct}% · ${mb(bytes)}` : `正在下载 · ${mb(bytes)}`;
      }
      refreshUpdSet(); return;
    }
    upd.busy = false;
    if (kind === 'installing') {
      upd.phase = 'installing';
      if (updSheetOpen()) { $('updProg').innerHTML = updProgHTML(); setTimeout(() => { if (updSheetOpen()) closeSheet(); upd.phase = ''; }, 1200); }
      else upd.phase = '';
    } else {
      upd.phase = '';
      if (kind === 'cancelled') toast('已取消下载');
      else if (msg) toast(msg);
      if (msg === '已是最新版本') { upd.info = null; hideBanner(); closeSheet(); }
      else if (updSheetOpen()) showUpdateSheet();
    }
    refreshUpdSet();
  };

  // ---------------- 扫描 ----------------
  let scanning = false;
  function rescan() {
    if (!A.hasPermission()) { showPerm(); return; }
    scanning = true; $('scanBanner').classList.remove('hidden'); $('scanText').textContent = '正在扫描…';
    $('emptyState').classList.add('hidden');
    A.rescan();
  }
  App.rescan = () => rescan();

  // ---------------- 播放状态 ----------------
  let state = { id: '', playing: false, pos: 0, dur: 0, mode: 'all', count: 0 };
  let lastId = null, seeking = false, restored = false;
  function playList(ids, index) {
    if (!ids.length) return;
    A.play(JSON.stringify(ids), index, 0, true);
    state.id = ids[index]; onTrackChange();
  }
  function poll() {
    let s;
    try { s = JSON.parse(A.getState()); } catch (e) { return; }
    if (!s || s.id === undefined) return;
    state = s;
    if (!restored) { restored = true; if (!s.count) restoreSession(); }
    if (s.id !== lastId) onTrackChange();
    updateProgress();
  }
  function curTrack() { return byId.get(state.id); }
  function onTrackChange() {
    lastId = state.id;
    const t = curTrack();
    $('mini').classList.toggle('hidden', !t);
    document.body.classList.toggle('no-mini', !t);
    if (!t) { $('nowTitle').textContent = '未在播放'; return; }
    const cname = t.album !== '未知专辑' ? t.album : t.title;
    $('miniCover').outerHTML = coverHTML(t.id, cname, 'mini-cover').replace('class="cov mini-cover"', 'class="cov mini-cover" id="miniCover"');
    $('miniTitle').textContent = t.title; $('miniArtist').textContent = t.artist;
    $('nowCover').outerHTML = coverHTML(t.id, cname, 'now-cover').replace('class="cov now-cover"', 'class="cov now-cover" id="nowCover"');
    $('nowTitle').textContent = t.title; $('nowArtist').textContent = `${t.artist} · ${t.album}`;
    $('nowBg').style.backgroundImage = noCover.has(t.id) ? '' : `url("/cover/${encodeURIComponent(t.id)}${t.cv ? '?v=' + t.cv : ''}")`;
    if (!noCover.has(t.id)) $('nowBg').style.backgroundColor = `hsl(${hue(cname)},${theme === 'light' ? '45%,70%' : '40%,25%'})`;
    updateFav();
    songList.refresh(); if (!$('detailPage').classList.contains('hidden')) detailList.refresh();
    loadLyrics(t, false);
    saveSessionSoon();
  }
  function updateProgress() {
    const playIcon = state.playing || (state.playWhenReady && state.buffering) ? ICON.pause : ICON.play;
    if ($('playBtn').dataset.ic !== playIcon) { $('playBtn').innerHTML = playIcon; $('playBtn').dataset.ic = playIcon; $('miniPlay').innerHTML = playIcon; }
    const dur = state.dur || (curTrack() ? curTrack().duration : 0);
    const p = dur ? Math.min(1, state.pos / dur) : 0;
    $('miniProg').style.width = (p * 100).toFixed(2) + '%';
    if (!$('nowPage').classList.contains('hidden')) {
      if (!seeking) { $('seekBar').value = Math.round(p * 1000); $('seekBar').style.setProperty('--p', (p * 100).toFixed(2) + '%'); $('tCur').textContent = fmt(state.pos); }
      $('tDur').textContent = fmt(dur);
      if ($('modeBtn').dataset.m !== state.mode) {
        $('modeBtn').dataset.m = state.mode; $('modeBtn').innerHTML = ICON[state.mode] || ICON.all;
        $('modeBtn').classList.toggle('on', state.mode !== 'all');
      }
      syncLyrics();
    }
  }
  $('seekBar').addEventListener('input', () => {
    seeking = true; const dur = state.dur || 0; const v = +$('seekBar').value;
    $('seekBar').style.setProperty('--p', v / 10 + '%'); $('tCur').textContent = fmt(dur * v / 1000);
  });
  $('seekBar').addEventListener('change', () => {
    const dur = state.dur || 0; const ms = Math.round(dur * +$('seekBar').value / 1000);
    A.seek(ms); state.pos = ms; seeking = false; lyricUserScroll = 0;
  });
  $('playBtn').onclick = () => A.toggle();
  $('miniPlay').onclick = (e) => { e.stopPropagation(); A.toggle(); };
  $('miniNext').onclick = (e) => { e.stopPropagation(); A.next(); };
  $('nextBtn').onclick = () => A.next();
  $('prevBtn').onclick = () => A.prev();
  $('modeBtn').onclick = () => {
    const order = ['all', 'one', 'shuffle'];
    const m = order[(order.indexOf(state.mode) + 1) % 3];
    A.setMode(m); state.mode = m; toast(MODE_NAME[m]); saveSessionSoon();
  };
  $('mini').onclick = () => openNow();
  $('nowClose').onclick = () => closeNow();
  $('nowMore').onclick = () => { const t = curTrack(); if (t) songMenu(t); };
  $('favBtn').onclick = () => {
    const t = curTrack(); if (!t) return;
    const fav = playlists.find((p) => p.id === 'fav');
    if (fav.ids.includes(t.id)) { fav.ids = fav.ids.filter((x) => x !== t.id); toast('已取消喜欢'); }
    else { fav.ids.unshift(t.id); toast('已添加到「我喜欢」'); }
    savePlaylists(); renderPlaylists(); updateFav();
  };
  function updateFav() { const t = curTrack(); const fav = playlists.find((p) => p.id === 'fav'); $('favBtn').classList.toggle('on', !!(t && fav && fav.ids.includes(t.id))); }
  function openNow() { if (!curTrack()) return; $('nowPage').classList.remove('hidden'); document.body.classList.add('now-open'); $('modeBtn').dataset.m = ''; updateProgress(); if (lyricView) setTimeout(() => centerLyric(true), 30); }
  function closeNow() { $('nowPage').classList.add('hidden'); document.body.classList.remove('now-open'); }
  function closeNowIfOpen() { if (!$('nowPage').classList.contains('hidden')) closeNow(); }
  let lyricView = false;
  function setLyricView(on) {
    lyricView = on;
    $('swCover').classList.toggle('active', !on); $('swLyrics').classList.toggle('active', on);
    $('coverView').classList.toggle('hidden', on); $('lyricsView').classList.toggle('hidden', !on);
    if (on) { lastLine = -2; lyricUserScroll = 0; syncLyrics(); setTimeout(() => centerLyric(true), 20); }
  }
  $('swCover').onclick = () => setLyricView(false);
  $('swLyrics').onclick = () => setLyricView(true);
  $('coverView').onclick = () => setLyricView(true);

  // 播放队列
  $('nowQueue').onclick = () => {
    let q = []; try { q = JSON.parse(A.getQueue()); } catch (e) {}
    const cur = state.index;
    sheet(`<div class="sh-head" id="queueOpen"><div class="t1">播放队列</div><div class="t2">${q.length} 首 · ${MODE_NAME[state.mode] || ''}</div></div><div class="sh-body" id="qBody">` +
      q.map((id, i) => { const t = byId.get(id) || { title: '(已移除)', artist: '' }; return `<button class="item ${i === cur ? 'sel' : ''}" data-i="${i}"><span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(t.title)} <span style="color:var(--fg3);font-size:12.5px">· ${esc(t.artist)}</span></span><span class="r" data-rm="${i}">✕</span></button>`; }).join('') + '</div>', (b, e) => {
      const rm = e.target.closest('[data-rm]');
      if (rm) { A.removeFromQueue(+rm.dataset.rm); b.remove(); renumberQueue(); return; }
      if (b.dataset.i != null) { A.playAt(+b.dataset.i); closeSheet(); }
    });
    setTimeout(() => { const s = document.querySelector('#qBody .sel'); if (s) s.scrollIntoView({ block: 'center' }); }, 30);
  };
  function renumberQueue() { document.querySelectorAll('#qBody .item').forEach((el, i) => { el.dataset.i = i; const r = el.querySelector('[data-rm]'); if (r) r.dataset.rm = i; }); }

  // ---------------- 会话保存/恢复 ----------------
  let saveTimer = 0;
  function saveSessionSoon() { clearTimeout(saveTimer); saveTimer = setTimeout(saveSession, 800); }
  function saveSession() {
    if (!state.id) return;
    let q = []; try { q = JSON.parse(A.getQueue()); } catch (e) {}
    if (!q.length) return;
    kvSet('session', { ids: q, index: Math.max(0, state.index || 0), pos: state.pos || 0, mode: state.mode });
  }
  function restoreSession() {
    const s = kvGet('session', null);
    if (!s || !s.ids || !s.ids.length) return;
    const ids = s.ids.filter((id) => byId.has(id)); if (!ids.length) return;
    const curId = s.ids[s.index];
    let idx = ids.indexOf(curId); if (idx < 0) idx = 0;
    A.setMode(s.mode || 'all');
    A.play(JSON.stringify(ids), idx, idx === ids.indexOf(curId) ? s.pos || 0 : 0, false);
  }

  // ---------------- 歌词 ----------------
  let lyric = { id: '', lines: [], synced: false, source: '' }, lastLine = -2, lyricUserScroll = 0;
  const SRC_NAME = { lrc: '本地 LRC', embedded: '内嵌歌词', online: 'LRCLIB', qq: 'QQ音乐', kugou: '酷狗', netease: '网易云', manual: '手动选择', none: '' };
  function parseLRC(text) {
    const lines = []; let offset = 0;
    const tagRe = /\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]/g;
    for (const raw of String(text).replace(/\r/g, '').split('\n')) {
      const off = raw.match(/^\[offset:\s*([+-]?\d+)\s*\]/i);
      if (off) { offset = parseInt(off[1], 10) || 0; continue; }
      const times = []; let m, lastEnd = 0; tagRe.lastIndex = 0;
      while ((m = tagRe.exec(raw)) && m.index === lastEnd) {
        const frac = m[3] ? parseInt(m[3].padEnd(3, '0').slice(0, 3), 10) : 0;
        times.push((+m[1]) * 60000 + (+m[2]) * 1000 + frac); lastEnd = tagRe.lastIndex;
      }
      if (!times.length) continue;
      const txt = raw.slice(lastEnd).replace(/<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>/g, '').trim();
      for (const t of times) lines.push({ t: t - offset, text: txt });
    }
    lines.sort((a, b) => a.t - b.t);
    // 合并同一时间的双语行（原文 + 翻译）
    const merged = [];
    for (const l of lines) {
      const prev = merged[merged.length - 1];
      if (prev && prev.t === l.t && l.text) { prev.tr = prev.tr ? prev.tr + ' ' + l.text : l.text; continue; }
      merged.push({ t: l.t, text: l.text });
    }
    return merged.filter((l, i) => l.text || (merged[i + 1] && merged[i + 1].t - l.t > 4000));
  }
  function loadLyrics(t, force) {
    lyric = { id: t.id, lines: [], synced: false, source: '', loading: true };
    renderLyrics('正在查找歌词…');
    A.getLyrics(t.id, !!force);
  }
  function renderLyrics(msg) {
    const box = $('lyricsBox');
    lastLine = -2;
    $('lyricSrc').textContent = lyric.source && SRC_NAME[lyric.source] ? '来源：' + SRC_NAME[lyric.source] : '';
    const off = offsets[lyricKey()] || 0;
    $('offLabel').textContent = `偏移 ${off > 0 ? '+' : ''}${(off / 1000).toFixed(1)}s`;
    if (msg || !lyric.lines.length) {
      box.className = 'lyrics';
      box.innerHTML = `<div class="msg">${esc(msg || '暂无歌词')}${msg ? '' : '<br><button class="chip" id="lyrSearch2">手动搜索歌词</button>'}</div>`;
      const b = $('lyrSearch2'); if (b) b.onclick = openLyricSearch;
      $('lyricPeek').innerHTML = msg ? '' : '<span style="color:var(--fg3)">暂无歌词</span>';
      return;
    }
    box.className = 'lyrics ' + (lyric.synced ? 'synced' : 'plain');
    box.innerHTML = '<div class="pad"></div>' + lyric.lines.map((l, i) => `<div class="ln" data-i="${i}">${esc(l.text) || '♪'}${l.tr ? `<span class="tr">${esc(l.tr)}</span>` : ''}</div>`).join('') + '<div class="pad"></div>';
    if (!lyric.synced) $('lyricPeek').innerHTML = '<span style="color:var(--fg3)">点按封面查看歌词</span>';
    syncLyrics(); setTimeout(() => centerLyric(true), 20);
  }
  function lyricKey() { const t = byId.get(lyric.id); return t ? (t.oTitle || t.title) + '|' + (t.oArtist || t.artist) : lyric.id; }
  App.onLyrics = (id, json) => {
    if (id !== state.id && id !== lyric.id) return;
    let r = {}; try { r = JSON.parse(json); } catch (e) {}
    lyric = { id, lines: [], synced: false, source: r.source || 'none' };
    const text = zh(r.text || ''); // 本地 .lrc / 内嵌 / LRCLIB / 缓存 / 手动选择的歌词都在显示时转换
    if (text.trim()) {
      const parsed = parseLRC(text);
      if (parsed.length >= 2 || (parsed.length && r.synced)) { lyric.lines = parsed; lyric.synced = true; }
      else { lyric.lines = text.replace(/\r/g, '').split('\n').map((s) => ({ t: 0, text: s.replace(/^\[[a-z]+:.*\]$/i, '').trim() })).filter((l, i, a) => l.text || (i > 0 && a[i - 1].text)); lyric.synced = false; }
    }
    renderLyrics(lyric.lines.length ? '' : (r.error ? r.error : ''));
    if (!lyric.lines.length && r.error) {
      $('lyricsBox').innerHTML = `<div class="msg">${esc(r.error)}<br><button class="chip" id="lyrRetry">重试</button> <button class="chip" id="lyrSearch2">手动搜索</button></div>`;
      $('lyrRetry').onclick = () => { const t = curTrack(); if (t) loadLyrics(t, true); };
      $('lyrSearch2').onclick = openLyricSearch;
    }
  };
  function curLyricIndex() {
    if (!lyric.synced || !lyric.lines.length) return -1;
    const pos = (state.pos || 0) + (offsets[lyricKey()] || 0) + 150;
    let lo = 0, hi = lyric.lines.length - 1, ans = -1;
    while (lo <= hi) { const mid = (lo + hi) >> 1; if (lyric.lines[mid].t <= pos) { ans = mid; lo = mid + 1; } else hi = mid - 1; }
    return ans;
  }
  function syncLyrics() {
    if (lyric.id !== state.id || !lyric.synced) return;
    const i = curLyricIndex();
    if (i === lastLine) return;
    lastLine = i;
    const l = lyric.lines;
    const peek = (k) => (l[k] ? esc(l[k].text) : '');
    $('lyricPeek').innerHTML = i < 0 ? `<div>${peek(0)}</div>` : `<div class="cur">${peek(i) || '♪'}</div><div>${peek(i + 1)}</div>`;
    if (!lyricView) return;
    const box = $('lyricsBox');
    const prev = box.querySelector('.ln.active'); if (prev) prev.classList.remove('active');
    const el = box.querySelector(`.ln[data-i="${i}"]`); if (el) el.classList.add('active');
    centerLyric(false);
  }
  function centerLyric(instant) {
    if (!lyric.synced || Date.now() - lyricUserScroll < 3500) return;
    const box = $('lyricsBox');
    const el = box.querySelector('.ln.active') || box.querySelector('.ln');
    if (!el) return;
    const top = el.offsetTop - box.clientHeight / 2 + el.clientHeight / 2;
    box.scrollTo({ top, behavior: instant ? 'auto' : 'smooth' });
  }
  $('lyricsBox').addEventListener('touchstart', () => { lyricUserScroll = Date.now(); }, { passive: true });
  $('lyricsBox').addEventListener('touchmove', () => { lyricUserScroll = Date.now(); }, { passive: true });
  $('lyricsBox').addEventListener('wheel', () => { lyricUserScroll = Date.now(); }, { passive: true });
  $('lyricsBox').addEventListener('click', (e) => {
    const ln = e.target.closest('.ln'); if (!ln || !lyric.synced) return;
    const l = lyric.lines[+ln.dataset.i]; if (!l) return;
    const ms = Math.max(0, l.t - (offsets[lyricKey()] || 0));
    A.seek(ms); state.pos = ms; lyricUserScroll = 0; lastLine = -2; syncLyrics();
    if (!state.playing) A.toggle();
  });
  function shiftOffset(d) {
    const k = lyricKey(); const v = Math.round(((offsets[k] || 0) + d) / 100) * 100;
    if (v) offsets[k] = v; else delete offsets[k];
    kvSet('lyricOffsets', offsets);
    $('offLabel').textContent = `偏移 ${v > 0 ? '+' : ''}${(v / 1000).toFixed(1)}s`;
    lastLine = -2; lyricUserScroll = 0; syncLyrics();
  }
  $('offMinus').onclick = () => shiftOffset(-500);
  $('offPlus').onclick = () => shiftOffset(500);
  $('lyricSearchBtn').onclick = () => openLyricSearch();

  let lyrReq = 0, lyrResults = [];
  function openLyricSearch() {
    const t = curTrack(); if (!t) return;
    const artist = t.artist === '未知歌手' ? '' : t.artist;
    sheet(`<div class="sh-head"><div class="t1">搜索歌词</div><div class="t2">同时查找 QQ音乐、酷狗、网易云和 LRCLIB，选中后会保存在本机</div></div>
      <div class="form"><label>歌名</label><input id="lsTitle" value="${esc(t.title)}"><label>歌手（可留空）</label><input id="lsArtist" value="${esc(artist)}">
      <div class="btns"><button class="btn ghost" data-a="reset">恢复自动匹配</button><button class="btn primary" data-a="go">搜索</button></div></div>
      <div class="sh-body" id="lsRes"></div>`, (b) => {
      if (b.dataset.a === 'go') doLyricSearch();
      if (b.dataset.a === 'reset') { A.resetLyrics(t.id); closeSheet(); loadLyrics(t, false); toast('已恢复自动匹配'); }
      if (b.classList.contains('res')) {
        const r = lyrResults[+b.dataset.i]; if (!r) return;
        A.saveLyrics(t.id, r.text, !!r.synced); closeSheet();
        App.onLyrics(t.id, JSON.stringify({ source: 'manual', text: r.text, synced: r.synced }));
        setLyricView(true); toast('已应用并保存歌词');
      }
    });
  }
  function doLyricSearch() {
    const title = $('lsTitle').value.trim(), artist = $('lsArtist').value.trim();
    if (!title) return toast('请输入歌名');
    $('lsRes').innerHTML = '<div class="sh-note"><span class="spinner" style="display:inline-block;vertical-align:middle"></span> 正在搜索…</div>';
    A.searchLyrics(title, artist, ++lyrReq);
  }
  App.onLyricSearch = (req, json) => {
    if (req !== lyrReq || !$('lsRes')) return;
    let r = {}; try { r = JSON.parse(json); } catch (e) {}
    if (!r.ok) { $('lsRes').innerHTML = `<div class="sh-note">搜索失败：${esc(r.error || '')}</div>`; return; }
    const t = curTrack(); const d = t ? t.duration / 1000 : 0;
    lyrResults = (r.items || []).sort((a, b) => (b.synced - a.synced) || (Math.abs(a.duration - d) - Math.abs(b.duration - d)));
    $('lsRes').innerHTML = lyrResults.length ? lyrResults.map((x, i) => {
      const diff = d && x.duration ? Math.round(x.duration - d) : null;
      const pv = zh(String(x.text)).replace(/\[[^\]]*\]/g, '').split('\n').map((s) => s.trim()).filter(Boolean).slice(0, 3).join(' / ');
      return `<button class="res" data-i="${i}"><div class="t1">${esc(zh(x.title))}${x.synced ? '<span class="tag">滚动</span>' : '<span class="tag gray">纯文本</span>'}<span class="tag gray">${esc(x.src || '')}</span></div><div class="t2">${esc(zh(x.artist))}${x.album ? ' · ' + esc(zh(x.album)) : ''} · ${fmt(x.duration * 1000)}${diff !== null ? `（${diff >= 0 ? '+' : ''}${diff}s）` : ''}</div><div class="pv">${esc(pv)}</div></button>`;
    }).join('') : '<div class="sh-note">没有找到，换个关键词试试</div>';
  };

  // ---------------- 原生回调 ----------------
  App.onBack = () => closeTop();
  App.onState = () => poll();
  App.onController = () => { setTimeout(poll, 100); };
  App.onPermission = (ok, blocked) => {
    if (ok) { hidePerm(); scanning = true; $('scanBanner').classList.remove('hidden'); $('scanText').textContent = '正在扫描…'; $('emptyState').classList.add('hidden'); A.rescan(); return; }
    showPerm(blocked);
  };
  App.onScanProgress = (msg) => { scanning = true; $('scanBanner').classList.remove('hidden'); $('emptyState').classList.add('hidden'); $('scanText').textContent = msg; };
  App.onScanDone = (n) => {
    scanning = false; $('scanBanner').classList.add('hidden');
    const before = tracks.length;
    loadLibrary(); renderAll(true);
    if (!$('settingsPage').classList.contains('hidden')) renderSettings();
    if (n < 0) toast('扫描失败，请稍后重试'); else toast(`扫描完成，共 ${n} 首` + (before && n !== before ? `（${n > before ? '+' : ''}${n - before}）` : ''));
    lastId = null; poll();
    if (n > 0 && kvGet('autoOrganize', false) && A.hasAllFiles() && A.outsideCount() > 0) setTimeout(() => organize(true), 600);
  };
  function organize(auto) {
    if (A.allFilesApplies() && !A.hasAllFiles()) {
      confirmSheet('需要「所有文件访问」权限', '移动歌曲文件需要这个权限。点确定去开启，开启后返回再点一次「整理」。', () => A.requestAllFiles());
      return;
    }
    let q = []; try { q = JSON.parse(A.getQueue()); } catch (e) {}
    organizeCtx = { q, index: state.index || 0, pos: state.pos || 0, playing: !!state.playing, auto };
    scanning = true; $('scanBanner').classList.remove('hidden'); $('scanText').textContent = '正在整理…';
    A.organize();
  }
  let organizeCtx = null;
  App.onOrganizeDone = (json, n) => {
    scanning = false; $('scanBanner').classList.add('hidden');
    let r = {}; try { r = JSON.parse(json); } catch (e) {}
    loadLibrary(); renderAll(true);
    if (!$('settingsPage').classList.contains('hidden')) renderSettings();
    const c = organizeCtx; organizeCtx = null;
    if (c && c.q.length) remapQueueAfterMove(c.q, c.index, c.pos, c.playing);
    lastId = null; poll();
    if (!r.ok) return toast(r.error || '整理失败', 4000);
    kvSet('autoOrganize', true);
    const parts = [`已移入 ${r.moved} 首`];
    if (r.dup) parts.push(`${r.dup} 首已存在`);
    if (r.failed) parts.push(`${r.failed} 首移动失败`);
    toast((c && c.auto ? '新歌已整理：' : '整理完成：') + parts.join('，'), 4000);
  };
  App.onFolderAdded = (label) => { toast('已添加：' + label); renderSettings(); rescan(); };
  App.saveSession = () => saveSession();

  let permBlocked = false;
  function showPerm(blocked) {
    if (blocked !== undefined) permBlocked = !!blocked;
    $('permPage').classList.remove('hidden'); $('mainPage').classList.add('hidden');
    $('permText').textContent = permBlocked
      ? '「音乐和音频」权限被拒绝了，系统不会再弹出授权框。请点下面的按钮打开应用设置 → 权限 → 音乐和音频，选择「允许」，返回后会自动扫描。'
      : '需要读取手机里的音乐文件（FLAC、MP3 等），才能为你建立音乐库。所有内容只在本机使用。';
    $('permBtn').textContent = permBlocked ? '打开应用设置去授权' : '授予权限并扫描音乐';
    $('permSettingsBtn').classList.toggle('hidden', permBlocked);
  }
  function hidePerm() { $('permPage').classList.add('hidden'); $('mainPage').classList.remove('hidden'); }
  $('permBtn').onclick = () => { if (permBlocked) A.openAppSettings(); else A.requestPermission(); };
  $('permSettingsBtn').onclick = () => A.openAppSettings();

  // ---------------- 启动 ----------------
  const lib = loadLibrary();
  document.body.classList.add('no-mini');
  renderAll();
  if (A.isScanning()) { scanning = true; $('scanBanner').classList.remove('hidden'); renderEmpty(); }
  if (!A.hasPermission() && !tracks.length) showPerm();
  else if (A.hasPermission() && !lib.scannedAt) rescan(); // 只有第一次自动扫描；之后用保存的音乐库，新歌请手动「重新扫描」
  setInterval(() => { if (!document.hidden) poll(); }, 250);
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { saveSession(); return; }
    if (!$('permPage').classList.contains('hidden') && A.hasPermission()) { hidePerm(); rescan(); }
    if (!$('settingsPage').classList.contains('hidden')) renderSettings();
    if (!scanning) renderEmpty();
    if (curTrack() && !lyric.lines.length && lyric.id === state.id) loadLyrics(curTrack(), false);
  });
  setInterval(() => { if (state.playing) saveSession(); }, 5000);
  poll();
  // 启动时静默检查更新（原生端限制为每 12 小时最多联网一次）
  setTimeout(() => { try { A.checkUpdate(true); } catch (e) {} }, 2500);
})();
