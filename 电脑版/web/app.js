/* 拾音 电脑版 v0.2 — UI（功能、用词与手机版拾音保持一致） */
'use strict';

// ---------------- helpers ----------------
const $ = (s, r = document) => r.querySelector(s);
const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtTime = (sec) => { if (!isFinite(sec) || sec < 0) sec = 0; sec = Math.floor(sec); const h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60, s = sec % 60; return (h ? h + ':' + String(m).padStart(2, '0') : m) + ':' + String(s).padStart(2, '0'); };
const fmtMs = (ms) => fmtTime((ms || 0) / 1000);
const fmtSize = (b) => b >= 1 << 30 ? (b / (1 << 30)).toFixed(2) + ' GB' : b >= 1 << 20 ? (b / (1 << 20)).toFixed(1) + ' MB' : Math.max(1, Math.round(b / 1024)) + ' KB';
const ICON = {
  note: '<svg viewBox="0 0 24 24"><path d="M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6z"/></svg>',
  play: '<svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>',
  pause: '<svg viewBox="0 0 24 24"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>',
  shuffle: '<svg viewBox="0 0 24 24"><path d="M10.59 9.17 5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z"/></svg>',
  repeat: '<svg viewBox="0 0 24 24"><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z"/></svg>',
  one: '<svg viewBox="0 0 24 24"><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4zm-4-2V9h-1l-2 1v1h1.5v4H13z"/></svg>',
  heart: '<svg viewBox="0 0 24 24"><path d="M16.5 3c-1.74 0-3.41.81-4.5 2.09C10.91 3.81 9.24 3 7.5 3 4.42 3 2 5.42 2 8.5c0 3.78 3.4 6.86 8.55 11.54L12 21.35l1.45-1.32C18.6 15.36 22 12.28 22 8.5 22 5.42 19.58 3 16.5 3zm-4.4 15.55-.1.1-.1-.1C7.14 14.24 4 11.39 4 8.5 4 6.5 5.5 5 7.5 5c1.54 0 3.04.99 3.57 2.36h1.87C13.46 5.99 14.96 5 16.5 5c2 0 3.5 1.5 3.5 3.5 0 2.89-3.14 5.74-7.9 10.05z"/></svg>',
  heartOn: '<svg viewBox="0 0 24 24"><path d="m12 21.35-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>',
  more: '<svg viewBox="0 0 24 24"><path d="M12 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>',
  back: '<svg viewBox="0 0 24 24"><path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z"/></svg>',
  vol: '<svg viewBox="0 0 24 24"><path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3A4.5 4.5 0 0 0 14 7.97v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"/></svg>',
  mute: '<svg viewBox="0 0 24 24"><path d="M16.5 12A4.5 4.5 0 0 0 14 7.97v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51A8.8 8.8 0 0 0 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3 3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06a8.99 8.99 0 0 0 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4 9.91 6.09 12 8.18V4z"/></svg>',
  folder: '<svg viewBox="0 0 24 24"><path d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/></svg>',
  plus: '<svg viewBox="0 0 24 24"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>',
  x: '<svg viewBox="0 0 24 24"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>',
  list: '<svg viewBox="0 0 24 24"><path d="M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18A3 3 0 1 0 19 17V8h3V6h-5z"/></svg>',
  sort: '<svg viewBox="0 0 24 24"><path d="M3 18h6v-2H3v2zM3 6v2h18V6H3zm0 7h12v-2H3v2z"/></svg>',
};

async function api(path, opts = {}) {
  const o = { method: opts.method || 'GET', headers: {} };
  if (opts.body !== undefined) {
    if (opts.raw) { o.body = opts.body; } else { o.body = JSON.stringify(opts.body); o.headers['Content-Type'] = 'application/json'; }
  }
  const r = await fetch(path, o);
  let data = null;
  try { data = await r.json(); } catch (_) {}
  if (!r.ok) throw new Error((data && data.error) || ('请求失败 ' + r.status));
  return data;
}

let toastTimer;
function toast(msg, ms) {
  const t = $('#toast'); t.textContent = msg; t.classList.remove('hidden');
  clearTimeout(toastTimer); toastTimer = setTimeout(() => t.classList.add('hidden'), ms || Math.max(2400, Math.min(5000, String(msg).length * 120)));
}

// ---------------- state ----------------
const S = {
  info: null, cfg: {}, prefs: {}, tracks: [], byId: new Map(), byKey: new Map(), playlists: [],
  tab: 'songs', stack: [], q: '', outside: 0, scanned: 0,
  // player
  base: [], queue: [], qi: -1, mode: 'seq', vol: 0.8, muted: false,
  lyr: null, lyrIdx: -1, lyrTrack: null,
};
const audio = $('#audio');

const t2s = (s) => (S.cfg.t2s && window.T2S && s) ? window.T2S.convert(s) : s;
const toZh = (s) => t2s(String(s || ''));
const hasCjk = (x) => /[\u3400-\u9fff]/.test(String(x || ''));
const collator = new Intl.Collator('zh-Hans-CN', { numeric: true, sensitivity: 'base' });
const cmp = (a, b) => collator.compare(a || '', b || '');

function decorate(t) {
  t.dTitle = t2s(t.title); t.dArtist = t2s(t.artist); t.dAlbum = t2s(t.album);
  t.search = (t.title + ' ' + t.artist + ' ' + t.album + ' ' + t.dTitle + ' ' + t.dArtist + ' ' + t.dAlbum + ' ' + t.fileName).toLowerCase();
  t.folder = t.path ? t.path.replace(/[\\/][^\\/]*$/, '') : '';
  return t;
}
function splitArtists(s) {
  const parts = String(s || '').split(/\s*(?:\/|、|;|；|&|,|，| feat\.? | ft\. )\s*/i).map((x) => x.trim()).filter(Boolean);
  return parts.length ? [...new Set(parts)] : ['未知歌手'];
}

async function savePrefs(patch) {
  Object.assign(S.prefs, patch);
  try { S.prefs = await api('/api/prefs', { method: 'POST', body: patch }); } catch (_) {}
}

async function loadLibrary() {
  const d = await api('/api/library');
  S.tracks = d.tracks.map(decorate);
  S.byId = new Map(S.tracks.map((t) => [t.id, t]));
  S.byKey = new Map();
  for (const t of S.tracks) { if (!S.byKey.has(t.key)) S.byKey.set(t.key, t); }
  S.playlists = d.playlists;
  S.scan = d.scan; S.org = d.organize; S.outside = d.outside || 0; S.scanned = d.scanned || 0;
  render(true);
  updateNowPlaying();
  renderQueue();
  refreshSettings();
  if (d.scan && d.scan.running) pollScan();
}

function coverUrl(t) { return t && t.cover ? `/api/cover?id=${t.id}&v=${t.cv || 0}-${t.added}-${S.prefs.coverEpoch || 0}` : ''; }
function coverHTML(t, cls = '') {
  const u = coverUrl(t);
  return u ? `<div class="cover ${cls}" style="background-image:url('${u}')"></div>` : `<div class="cover ${cls}">${ICON.note}</div>`;
}
function setCover(el, t) {
  const u = coverUrl(t);
  el.classList.add('cover');
  el.style.backgroundImage = u ? `url('${u}')` : '';
  el.innerHTML = u ? '' : ICON.note;
}

const favPl = () => S.playlists.find((p) => p.fav);
const isFav = (t) => { const f = favPl(); return !!(t && f && f.keys.includes(t.key)); };
async function savePlaylists() {
  try { S.playlists = await api('/api/playlists', { method: 'PUT', body: S.playlists }); } catch (e) { toast('保存歌单失败：' + e.message); }
}
async function toggleFav(t) {
  const f = favPl(); if (!f || !t) return;
  const i = f.keys.indexOf(t.key);
  if (i >= 0) { f.keys.splice(i, 1); toast('已取消喜欢'); } else { f.keys.unshift(t.key); toast('已添加到「我喜欢」'); }
  await savePlaylists();
  render(true); updateNowPlaying();
}
const plTracks = (p) => p.keys.map((k) => S.byKey.get(k)).filter(Boolean);

// ---------------- theme（深色 / 浅色 / 跟随系统） ----------------
const darkMQ = window.matchMedia ? matchMedia('(prefers-color-scheme: light)') : null;
let lastDark = null;
function applyTheme(mode) {
  mode = ['dark', 'light', 'system'].includes(mode) ? mode : 'dark';
  const root = document.documentElement;
  root.dataset.theme = mode;
  const light = mode === 'light' || (mode === 'system' && !!(darkMQ && darkMQ.matches));
  root.classList.toggle('light', light);
  if (lastDark !== !light) { lastDark = !light; api('/api/theme', { method: 'POST', body: { dark: !light } }).catch(() => {}); }
}
if (darkMQ) {
  const onSys = () => { if ((S.cfg.theme || 'dark') === 'system') applyTheme('system'); };
  if (darkMQ.addEventListener) darkMQ.addEventListener('change', onSys); else if (darkMQ.addListener) darkMQ.addListener(onSys);
}
async function setTheme(mode) {
  applyTheme(mode);
  try { S.cfg = await api('/api/config', { method: 'POST', body: { theme: mode } }); } catch (e) { toast(e.message); }
  refreshSettings();
}
const THEME_NAME = { dark: '深色', light: '浅色', system: '跟随系统' };

// ---------------- views ----------------
const SORTS = {
  title: { name: '标题', fn: (a, b) => cmp(a.dTitle, b.dTitle) },
  artist: { name: '歌手', fn: (a, b) => cmp(a.dArtist, b.dArtist) || cmp(a.dAlbum, b.dAlbum) || ((a.trackNo || 0) - (b.trackNo || 0)) },
  album: { name: '专辑', fn: (a, b) => cmp(a.dAlbum, b.dAlbum) || ((a.trackNo || 0) - (b.trackNo || 0)) || cmp(a.dTitle, b.dTitle) },
  added: { name: '添加时间', fn: (a, b) => b.added - a.added },
  duration: { name: '时长', fn: (a, b) => a.duration - b.duration },
  folder: { name: '文件夹', fn: (a, b) => cmp(a.folder, b.folder) || cmp(a.dTitle, b.dTitle) },
};
const sortPref = () => { const s = S.prefs.sort || {}; return { sort: SORTS[s.sort] ? s.sort : 'title', dir: s.dir < 0 ? -1 : 1 }; };
function sortedSongs(list) {
  const p = sortPref();
  const arr = list.slice().sort(SORTS[p.sort].fn);
  if (p.dir < 0) arr.reverse();
  return arr;
}
function sortMenu(x, y) {
  const p = sortPref();
  const items = Object.entries(SORTS).map(([k, s]) => ({ label: (p.sort === k ? '✓ ' : '　 ') + s.name, r: p.sort === k ? (p.dir < 0 ? '倒序' : '正序') : '', fn: () => { savePrefs({ sort: { sort: k, dir: 1 } }); render(); } }));
  items.push('-', { label: '切换正序 / 倒序', fn: () => { savePrefs({ sort: { sort: p.sort, dir: -p.dir } }); render(); } });
  showMenu(items, x, y);
}

function filterTracks(list) {
  const q = S.q.trim().toLowerCase();
  if (!q) return list;
  const qs = t2s(q).toLowerCase();
  return list.filter((t) => t.search.includes(q) || t.search.includes(qs));
}
function albumGroups() {
  const m = new Map();
  for (const t of S.tracks) {
    let a = m.get(t.dAlbum);
    if (!a) { a = { name: t.dAlbum, ts: [], artists: new Map() }; m.set(t.dAlbum, a); }
    a.ts.push(t); a.artists.set(t.dArtist, (a.artists.get(t.dArtist) || 0) + 1);
  }
  for (const a of m.values()) {
    a.artist = [...a.artists.entries()].sort((x, y) => y[1] - x[1])[0][0];
    if (a.artists.size > 2) a.artist = '群星';
    a.ts.sort((x, y) => (x.trackNo || 999) - (y.trackNo || 999) || cmp(x.dTitle, y.dTitle));
  }
  return m;
}
function artistGroups() {
  const m = new Map();
  for (const t of S.tracks) for (const n of splitArtists(t.dArtist)) {
    if (!m.has(n)) m.set(n, []);
    m.get(n).push(t);
  }
  return m;
}

let currentList = []; // what the song rows currently show (for click-to-play)

function render(keepScroll) {
  const main = $('#main');
  const st = keepScroll ? main.scrollTop : 0;
  document.querySelectorAll('#tabs button').forEach((b) => b.classList.toggle('on', b.dataset.tab === S.tab && !S.stack.length));
  const top = S.stack[S.stack.length - 1];
  let html = '';
  if (top) html = renderDetail(top);
  else if (S.tab === 'songs') html = renderSongsTab();
  else if (S.tab === 'albums') html = renderAlbums();
  else if (S.tab === 'artists') html = renderArtists();
  else html = renderPlaylists();
  main.innerHTML = scanBar() + html;
  main.scrollTop = st;
  markCurrent();
}

function scanBar() {
  const o = S.org;
  if (o && o.running) return `<div class="scanbar"><div class="spin"></div>正在整理${o.total ? ` ${o.done}/${o.total}` : ''}…</div>`;
  const s = S.scan;
  if (!s || !s.running) return '';
  return `<div class="scanbar"><div class="spin"></div>正在扫描音乐… 已找到 ${s.found} 首${s.total ? `，读取中 ${s.done}/${s.total}` : ''}</div>`;
}

function toolbar(n, withSort) {
  const p = sortPref();
  const sort = withSort ? `<button class="btn ghost sortbtn" data-act="sort" title="排序方式">${ICON.sort}${SORTS[p.sort].name}${p.dir < 0 ? '↓' : ''}</button>` : '';
  return `<div class="toolbar"><button class="btn primary" data-act="playAll">${ICON.play}播放全部</button><button class="btn ghost" data-act="shuffleAll">${ICON.shuffle}随机播放</button><span class="spacer"></span>${sort}<span class="count">共 ${n} 首</span></div>`;
}

function songRows(list) {
  currentList = list;
  if (!list.length) return '';
  let h = `<div class="list-head"><span class="r">#</span><span></span><span>歌曲</span><span>专辑</span><span></span><span class="r">时长</span><span></span></div><div class="songs" data-list="1">`;
  list.forEach((t, i) => {
    h += `<div class="row" data-id="${t.id}" data-i="${i}"><span class="idx">${i + 1}</span>${coverHTML(t)}<div style="min-width:0"><div class="t">${esc(t.dTitle)}</div><div class="a">${esc(t.dArtist)}</div></div><div class="al">${esc(t.dAlbum)}</div><button class="icon-btn heart ${isFav(t) ? 'on' : ''}" data-act="fav" title="喜欢">${isFav(t) ? ICON.heartOn : ICON.heart}</button><span class="d">${t.duration ? fmtMs(t.duration) : '--:--'}</span><button class="icon-btn more" data-act="more" title="更多">${ICON.more}</button></div>`;
  });
  return h + '</div>';
}

function renderSongsTab() {
  if (!S.tracks.length) {
    if (S.scan && S.scan.running) return '';
    return `<div class="empty"><h3>还没有找到音乐</h3>把音乐文件放进「${esc(S.cfg.musicDir || '')}」后点「重新扫描」，<br>或者直接从手机把歌传过来。支持 FLAC、MP3、M4A/AAC、WAV、OGG、OPUS 等格式。<br><button class="btn primary" data-act="openPhone">从手机导入</button><button class="btn ghost" data-act="rescan">重新扫描</button><button class="btn ghost" data-act="settings">添加文件夹</button></div>`;
  }
  const list = sortedSongs(filterTracks(S.tracks));
  if (!list.length) return `<div class="view-head"><h2>歌曲</h2></div><div class="empty">没有找到「${esc(S.q)}」</div>`;
  return `<div class="view-head"><h2>${S.q ? '搜索结果' : '全部歌曲'}</h2></div>${toolbar(list.length, true)}${songRows(list)}`;
}

function renderAlbums() {
  const q = S.q.trim().toLowerCase();
  let groups = [...albumGroups().values()];
  if (q) groups = groups.filter((g) => g.name.toLowerCase().includes(q) || g.ts.some((t) => t.dArtist.toLowerCase().includes(q)));
  groups.sort((a, b) => cmp(a.name, b.name));
  if (!groups.length) return `<div class="empty">${S.tracks.length ? '没有找到专辑' : '还没有歌曲'}</div>`;
  let h = `<div class="view-head"><h2>专辑</h2><span class="count">${groups.length} 张</span></div><div class="grid">`;
  for (const g of groups) {
    const c = g.ts.find((t) => t.cover);
    h += `<div class="card" data-open="album" data-name="${esc(g.name)}">${coverHTML(c)}<div class="n">${esc(g.name)}</div><div class="s">${esc(g.artist)} · ${g.ts.length} 首</div></div>`;
  }
  return h + '</div>';
}

function renderArtists() {
  const q = S.q.trim().toLowerCase();
  let groups = [...artistGroups()].map(([name, ts]) => ({ name, ts }));
  if (q) groups = groups.filter((g) => g.name.toLowerCase().includes(q));
  groups.sort((a, b) => (a.name === '未知歌手') - (b.name === '未知歌手') || cmp(a.name, b.name));
  if (!groups.length) return `<div class="empty">${S.tracks.length ? '没有找到歌手' : '还没有歌曲'}</div>`;
  let h = `<div class="view-head"><h2>歌手</h2><span class="count">${groups.length} 位</span></div><div class="alist">`;
  for (const g of groups) {
    const c = g.ts.find((t) => t.cover);
    h += `<div class="arow" data-open="artist" data-name="${esc(g.name)}">${coverHTML(c)}<div class="n">${esc(g.name)}</div><div class="s">${g.ts.length} 首歌曲</div></div>`;
  }
  return h + '</div>';
}

function renderPlaylists() {
  let h = `<div class="view-head"><h2>歌单</h2><button class="btn ghost" data-act="newPl">${ICON.plus}新建歌单</button></div><div class="grid">`;
  for (const p of S.playlists) {
    const ts = plTracks(p);
    const c = ts.find((t) => t.cover);
    const cov = p.fav ? `<div class="cover">${ICON.heartOn}</div>` : coverHTML(c);
    h += `<div class="card ${p.fav ? 'fav' : ''}" data-open="playlist" data-pid="${esc(p.id)}">${cov}<div class="n">${esc(p.name)}</div><div class="s">${ts.length} 首歌曲</div></div>`;
  }
  h += `<div class="card new" data-act="newPl"><div class="cover">${ICON.plus}</div><div class="n">新建歌单</div></div>`;
  return h + '</div>';
}

function detailTracks(v) {
  if (v.type === 'album') { const g = albumGroups().get(v.name); return g ? g.ts : []; }
  if (v.type === 'artist') return S.tracks.filter((t) => splitArtists(t.dArtist).includes(v.name)).sort((x, y) => cmp(x.dAlbum, y.dAlbum) || ((x.trackNo || 0) - (y.trackNo || 0)) || cmp(x.dTitle, y.dTitle));
  if (v.type === 'playlist') { const p = S.playlists.find((x) => x.id === v.id); return p ? plTracks(p) : []; }
  return [];
}

function renderDetail(v) {
  let title = '', kind = '', sub = '', extra = '';
  const ts = detailTracks(v);
  if (v.type === 'album') { title = v.name; kind = '专辑'; const g = albumGroups().get(v.name); sub = g ? g.artist : ''; }
  if (v.type === 'artist') { title = v.name; kind = '歌手'; }
  let favCover = false;
  if (v.type === 'playlist') {
    const p = S.playlists.find((x) => x.id === v.id);
    if (!p) { S.stack.pop(); return render(); }
    title = p.name; kind = '歌单'; favCover = !!p.fav;
    if (!p.fav) extra = `<button class="btn ghost" data-act="renamePl">重命名</button><button class="btn danger" data-act="delPl">删除歌单</button>`;
    extra += `<button class="icon-btn" data-act="plMenu" title="更多">${ICON.more}</button>`;
  }
  const fts = filterTracks(ts);
  const cov = ts.find((t) => t.cover);
  const coverEl = favCover ? `<div class="cover favc">${ICON.heartOn}</div>` : coverHTML(cov, v.type === 'artist' ? 'round' : '');
  return `<div class="view-head" style="padding-bottom:0"><button class="icon-btn back" data-act="back" title="返回">${ICON.back}</button></div>
    <div class="detail-hero">${coverEl}<div class="meta"><div class="kind">${kind}</div><h2>${esc(title)}</h2><div class="sub">${esc(sub)}${sub ? ' · ' : ''}${ts.length} 首歌曲</div></div></div>
    <div class="toolbar"><button class="btn primary" data-act="playAll" ${fts.length ? '' : 'disabled'}>${ICON.play}播放全部</button><button class="btn ghost" data-act="shuffleAll" ${fts.length ? '' : 'disabled'}>${ICON.shuffle}随机播放</button><span class="spacer"></span>${extra}</div>
    ${fts.length ? songRows(fts) : (currentList = [], `<div class="empty">${ts.length ? '没有匹配的歌曲' : '这里还没有歌曲。<br>在歌曲上点右键 →「添加到歌单」'}</div>`)}`;
}

function markCurrent() {
  const cur = curTrack();
  document.querySelectorAll('.row.cur').forEach((r) => r.classList.remove('cur'));
  if (cur) document.querySelectorAll(`.row[data-id="${cur.id}"]`).forEach((r) => r.classList.add('cur'));
}

function openView(v) {
  closePlayer(); closeQueue();
  S.stack.push(v); $('#main').scrollTop = 0; render();
}

// ---------------- events: main ----------------
$('#tabs').addEventListener('click', (e) => {
  const b = e.target.closest('button'); if (!b) return;
  S.tab = b.dataset.tab; S.stack = []; render(); closePlayer();
});
const searchInp = $('#search');
let searchT;
searchInp.addEventListener('input', () => {
  $('#searchClear').classList.toggle('hidden', !searchInp.value);
  clearTimeout(searchT); searchT = setTimeout(() => { S.q = searchInp.value; render(); }, 120);
});
$('#searchClear').onclick = () => { searchInp.value = ''; S.q = ''; $('#searchClear').classList.add('hidden'); render(); };

$('#main').addEventListener('click', async (e) => {
  const act = e.target.closest('[data-act]');
  const row = e.target.closest('.row');
  if (act) {
    const a = act.dataset.act;
    const top = S.stack[S.stack.length - 1];
    if (a === 'playAll') return playList(currentList, 0, 'seq');
    if (a === 'shuffleAll') return playList(currentList, -1, 'shuffle');
    if (a === 'fav' && row) return toggleFav(S.byId.get(row.dataset.id));
    if (a === 'more' && row) { const r = act.getBoundingClientRect(); return songMenu(S.byId.get(row.dataset.id), r.left - 200, r.bottom); }
    if (a === 'sort') { const r = act.getBoundingClientRect(); return sortMenu(r.left, r.bottom + 4); }
    if (a === 'back') { S.stack.pop(); return render(); }
    if (a === 'newPl') return newPlaylist();
    if (a === 'renamePl') return renamePlaylist(top.id);
    if (a === 'delPl') return deletePlaylist(top.id);
    if (a === 'plMenu') { const r = act.getBoundingClientRect(); return playlistMenu(top.id, r.left - 200, r.bottom); }
    if (a === 'openPhone') return openPhone();
    if (a === 'rescan') return rescan();
    if (a === 'settings') return openSettings();
    return;
  }
  const open = e.target.closest('[data-open]');
  if (open) {
    const o = open.dataset.open;
    S.stack.push(o === 'playlist' ? { type: 'playlist', id: open.dataset.pid } : { type: o, name: open.dataset.name });
    $('#main').scrollTop = 0;
    return render();
  }
  if (row) playList(currentList, +row.dataset.i);
});
$('#main').addEventListener('contextmenu', (e) => {
  const row = e.target.closest('.row');
  e.preventDefault();
  if (row) songMenu(S.byId.get(row.dataset.id), e.clientX, e.clientY);
});
document.addEventListener('contextmenu', (e) => { if (!e.target.closest('input, textarea, .upd-notes, .info-tbl')) e.preventDefault(); });

// ---------------- context menu ----------------
function showMenu(items, x, y) {
  const m = $('#menu');
  m.innerHTML = items.map((it, i) => it === '-' ? '<hr>' : `<button data-i="${i}" class="${it.cls || ''}"><span>${esc(it.label)}</span>${it.r ? `<span class="r">${esc(it.r)}</span>` : ''}</button>`).join('');
  m.classList.remove('hidden');
  const r = m.getBoundingClientRect();
  m.style.left = Math.max(8, Math.min(x, innerWidth - r.width - 8)) + 'px';
  m.style.top = Math.max(8, Math.min(y, innerHeight - r.height - 8)) + 'px';
  m.onclick = (e) => { const b = e.target.closest('button'); if (!b) return; hideMenu(); items[+b.dataset.i].fn(); };
}
function hideMenu() { $('#menu').classList.add('hidden'); }
document.addEventListener('mousedown', (e) => { if (!e.target.closest('#menu')) hideMenu(); });
window.addEventListener('blur', hideMenu);

// 歌曲菜单：与手机版相同的项目和顺序（最后一项「在文件夹中显示」为电脑版独有）
function songMenu(t, x, y) {
  if (!t) return;
  const top = S.stack[S.stack.length - 1];
  const inPl = top && top.type === 'playlist' && $('#player').classList.contains('hidden') ? top.id : null;
  const items = [
    { label: '下一首播放', fn: () => playNext(t) },
    { label: '添加到播放队列', fn: () => addToQueue(t) },
    { label: '添加到歌单', fn: () => addToPlaylist(t) },
  ];
  if (inPl) items.push({ label: '从此歌单移除', fn: () => removeFromPlaylist(inPl, t) });
  items.push(
    { label: '查看专辑', r: t.dAlbum, fn: () => openView({ type: 'album', name: t.dAlbum }) },
    { label: '查看歌手', r: t.dArtist, fn: () => openView({ type: 'artist', name: splitArtists(t.dArtist)[0] }) },
    { label: '编辑歌名 / 歌手 / 专辑', r: '可联网识别中文名', fn: () => editMeta(t) },
    { label: '更换封面', fn: () => coverDialog(t) },
    { label: '歌曲信息', fn: () => songInfo(t) },
    '-',
    { label: '在文件夹中显示', fn: () => api('/api/reveal?id=' + t.id, { method: 'POST' }) });
  showMenu(items, x, y);
}

function playlistMenu(id, x, y) {
  const p = S.playlists.find((q) => q.id === id); if (!p) return;
  const items = [];
  if (!p.fav) items.push({ label: '重命名', fn: () => renamePlaylist(id) });
  items.push({ label: '移除已不存在的歌曲', fn: () => cleanPlaylist(id) });
  if (!p.fav) items.push({ label: '删除歌单', cls: 'red', fn: () => deletePlaylist(id) });
  showMenu(items, x, y);
}

// ---------------- modals ----------------
function modal(html, cls = '') {
  const o = $('#overlay');
  o.innerHTML = `<div class="modal ${cls}">${html}</div>`;
  delete o.dataset.kind;
  o.classList.remove('hidden');
  o.onmousedown = (e) => { if (e.target === o && !o.dataset.sticky) closeModal(); };
  const m = o.firstElementChild;
  m.querySelectorAll('[data-close]').forEach((b) => b.onclick = closeModal);
  return m;
}
function closeModal() {
  const o = $('#overlay'); o.classList.add('hidden'); o.innerHTML = ''; delete o.dataset.sticky; delete o.dataset.kind;
  if (phoneTimer) { clearInterval(phoneTimer); phoneTimer = null; }
}
const modalOpen = () => !$('#overlay').classList.contains('hidden');
const modalKind = () => modalOpen() ? $('#overlay').dataset.kind || '' : '';

function prompt2(title, label, value, ok) {
  const m = modal(`<h3>${esc(title)}</h3><div class="body"><div class="field"><label>${esc(label)}</label><input type="text" id="pv" maxlength="60" value="${esc(value || '')}"></div></div><div class="foot"><button class="btn ghost" data-close>取消</button><button class="btn primary" id="pok">确定</button></div>`);
  const inp = $('#pv', m); inp.focus(); inp.select();
  const go = () => { const v = inp.value.trim(); if (!v) { toast('名称不能为空'); return inp.focus(); } closeModal(); ok(v); };
  $('#pok', m).onclick = go;
  inp.onkeydown = (e) => { if (e.key === 'Enter') go(); };
}
function confirm2(title, text, okLabel, ok) {
  const m = modal(`<h3>${esc(title)}</h3><div class="body">${esc(text)}</div><div class="foot"><button class="btn ghost" data-close>取消</button><button class="btn primary" id="cok">${esc(okLabel || '确定')}</button></div>`);
  $('#cok', m).onclick = () => { closeModal(); ok(); };
}

function newPlaylist(thenAdd) {
  prompt2('新建歌单', '歌单名称', '', async (name) => {
    const p = { id: 'pl' + Date.now().toString(36), name, keys: thenAdd ? [thenAdd.key] : [], created: Math.floor(Date.now() / 1000) };
    S.playlists.push(p); await savePlaylists(); render(true);
    toast(thenAdd ? `已添加到「${name}」` : `已创建「${name}」`);
  });
}
function renamePlaylist(id) {
  const p = S.playlists.find((x) => x.id === id); if (!p || p.fav) return;
  prompt2('重命名歌单', '歌单名称', p.name, async (name) => { p.name = name; await savePlaylists(); render(true); });
}
function deletePlaylist(id) {
  const p = S.playlists.find((x) => x.id === id); if (!p || p.fav) return;
  confirm2(`删除歌单「${p.name}」？`, '歌曲文件不会被删除', '删除', async () => {
    S.playlists = S.playlists.filter((x) => x !== p); await savePlaylists();
    const top = S.stack[S.stack.length - 1]; if (top && top.id === id) S.stack.pop();
    render(); toast('已删除');
  });
}
async function cleanPlaylist(id) {
  const p = S.playlists.find((x) => x.id === id); if (!p) return;
  const before = p.keys.length;
  p.keys = p.keys.filter((k) => S.byKey.has(k));
  await savePlaylists(); render(true);
  toast(before - p.keys.length ? `已清理 ${before - p.keys.length} 首` : '已清理，没有不存在的歌曲');
}
function addToPlaylist(t) {
  const pls = S.playlists;
  const m = modal(`<h3>添加到歌单<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body"><div style="color:var(--fg3);font-size:13px;margin-bottom:6px">${esc(t.dTitle)}</div><div class="pick" data-new="1"><div class="cover">${ICON.plus}</div><div>新建歌单…</div></div>${pls.map((p, i) => `<div class="pick" data-i="${i}">${p.fav ? `<div class="cover favc">${ICON.heartOn}</div>` : coverHTML(plTracks(p).find((x) => x.cover))}<div style="flex:1">${esc(p.name)}</div><span class="count" style="color:var(--fg3)">${p.keys.includes(t.key) ? '已添加' : plTracks(p).length + ' 首'}</span></div>`).join('')}</div>`);
  m.querySelector('.body').onclick = async (e) => {
    const pk = e.target.closest('.pick'); if (!pk) return;
    if (pk.dataset.new) { closeModal(); return newPlaylist(t); }
    const p = pls[+pk.dataset.i]; closeModal();
    if (p.keys.includes(t.key)) return toast('歌曲已在歌单中');
    p.keys.push(t.key); await savePlaylists(); render(true); updateNowPlaying(); toast(`已添加到「${p.name}」`);
  };
}
async function removeFromPlaylist(id, t) {
  const p = S.playlists.find((x) => x.id === id); if (!p) return;
  p.keys = p.keys.filter((k) => k !== t.key); await savePlaylists(); render(true); updateNowPlaying(); toast('已移除');
}

// ---------------- 编辑信息 / 联网识别 ----------------
const thumb = (u, n) => String(u || '').replace('600x600bb', `${n || 100}x${n || 100}bb`);
async function reloadKeep() { try { await loadLibrary(); } catch (_) {} }

function editMeta(t) {
  let cands = [];
  const raw = `${t.rawTitle || '（无）'} · ${t.rawArtist || '未知歌手'} · ${t.rawAlbum || '未知专辑'}`;
  const m = modal(`<h3>编辑歌曲信息<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body">
    <div class="field" style="color:var(--fg3);font-size:13px;margin-top:0">只改拾音里的显示，不修改音乐文件${t.edited ? `<br>原标签：${esc(raw)}` : ''}</div>
    <div class="field"><label>歌名</label><input type="text" id="eT" value="${esc(t.title)}"></div>
    <div class="row2"><div class="field"><label>歌手</label><input type="text" id="eA" value="${esc(t.artist === '未知歌手' ? '' : t.artist)}"></div>
    <div class="field"><label>专辑</label><input type="text" id="eL" value="${esc(t.album === '未知专辑' ? '' : t.album)}"></div></div>
    <label class="chk"><input type="checkbox" id="eSpread" checked> 同步到同歌手、同专辑的其他歌曲</label>
    <div class="res-list" id="eRes"></div>
    </div><div class="foot"><button class="btn ghost" id="eNet" style="margin-right:auto">联网识别</button>${t.edited ? '<button class="btn ghost" id="eReset">恢复原名</button>' : ''}<button class="btn ghost" data-close>取消</button><button class="btn primary" id="eOk">保存</button></div>`, 'wide');
  m.style.height = 'auto';
  $('#eT', m).focus();
  const res = $('#eRes', m);
  $('#eNet', m).onclick = async () => {
    res.innerHTML = '<div class="note"><div class="spin"></div>正在查找…</div>';
    const ar = t.rawArtist || '';
    let r;
    try { r = await api(`/api/match?id=${t.id}&title=${encodeURIComponent(t.rawTitle || t.title)}&artist=${encodeURIComponent(ar === '未知歌手' ? '' : ar)}`); } catch (e) { r = { ok: false, error: e.message }; }
    if (!document.body.contains(res)) return;
    if (!r.ok) { res.innerHTML = `<div class="note">查找失败：${esc(r.error || '')}</div>`; return; }
    cands = r.items || [];
    if (r.best > 0) cands.unshift(cands.splice(r.best, 1)[0]);
    res.innerHTML = cands.length ? cands.slice(0, 12).map((c, i) => {
      const diff = t.duration && c.duration ? Math.round((c.duration - t.duration) / 1000) : null;
      return `<button class="res mres" data-i="${i}">${c.art ? `<img src="${esc(thumb(c.art))}" alt="">` : '<span class="ph"></span>'}<span class="mt"><div class="t1">${esc(toZh(c.title))}${i === 0 && r.best >= 0 ? '<span class="tag best">最匹配</span>' : ''}</div><div class="t2">${esc(toZh(c.artist))} · ${esc(toZh(c.album))} · ${fmtMs(c.duration)}${diff !== null ? `（${diff >= 0 ? '+' : ''}${diff}s）` : ''}</div></span></button>`;
    }).join('') + '<div class="note">点一个结果填入上面，再点「保存」</div>' : '<div class="note">没有找到，可以直接手动输入中文名</div>';
  };
  res.onclick = (e) => {
    const b = e.target.closest('.mres'); if (!b) return;
    const c = cands[+b.dataset.i]; if (!c) return;
    $('#eT', m).value = toZh(c.title); $('#eA', m).value = toZh(c.artist); $('#eL', m).value = toZh(c.album);
    res.dataset.art = c.art || '';
    res.querySelectorAll('.res').forEach((x) => x.classList.toggle('sel', x === b));
    toast('已填入，确认后点保存');
  };
  $('#eOk', m).onclick = async () => {
    const title = $('#eT', m).value.trim(), artist = $('#eA', m).value.trim(), album = $('#eL', m).value.trim();
    if (!title) return toast('歌名不能为空');
    const art = res.dataset.art || '';
    try {
      await api('/api/meta', { method: 'POST', body: { id: t.id, title, artist, album, src: 'manual', spread: $('#eSpread', m).checked, art, cover: art ? 'ifMissing' : '' } });
      closeModal(); await reloadKeep(); toast('已保存');
    } catch (e) { toast(e.message); }
  };
  const rs = $('#eReset', m);
  if (rs) rs.onclick = async () => { try { await api('/api/meta', { method: 'POST', body: { id: t.id, reset: true } }); closeModal(); await reloadKeep(); toast('已恢复原名'); } catch (e) { toast(e.message); } };
}

// ---------------- 更换封面 ----------------
let coverTarget = null;
function coverIds(t, m) {
  const cb = m && $('#cvAlbum', m);
  if (cb && cb.checked) { const g = albumGroups().get(t.dAlbum); if (g) return g.ts.map((x) => x.id); }
  return [t.id];
}
function coverDialog(t) {
  const g = t.dAlbum !== '未知专辑' ? albumGroups().get(t.dAlbum) : null;
  const n = g ? g.ts.length : 1;
  let cands = [];
  const m = modal(`<h3>更换封面<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body">
    <div style="color:var(--fg3);font-size:13px;margin-bottom:8px">${esc(t.dTitle)}</div>
    ${n > 1 ? `<label class="chk" style="margin-bottom:6px"><input type="checkbox" id="cvAlbum" checked> 用于整张专辑「${esc(t.dAlbum)}」（${n} 首）</label>` : ''}
    <div class="act-list"><div class="pick" data-a="pick">${ICON.folder}<div>从电脑选择图片</div></div><div class="pick" data-a="net">${ICON.note}<div>在线查找封面</div></div>${t.customCover ? `<div class="pick" data-a="reset">${ICON.back}<div>恢复默认封面</div></div>` : ''}</div>
    <div id="cvRes" class="cv-grid"></div></div>`, 'wide');
  m.style.height = 'auto';
  const box = $('#cvRes', m);
  const show = () => {
    box.innerHTML = cands.length ? cands.slice(0, 12).map((c, i) => `<button class="cvc" data-i="${i}"><img src="${esc(thumb(c.art, 300))}" alt=""><span>${esc(toZh(c.album))}</span></button>`).join('') : '<div class="note">没有找到，可以从电脑选择图片</div>';
  };
  const done = async (msg) => { closeModal(); await reloadKeep(); toast(msg); };
  m.querySelector('.body').onclick = async (e) => {
    const a = e.target.closest('[data-a]');
    if (a && a.dataset.a === 'pick') { coverTarget = { t, ids: coverIds(t, m) }; const f = $('#coverFile'); f.value = ''; f.click(); return; }
    if (a && a.dataset.a === 'reset') {
      try { await api(`/api/cover?id=${t.id}&ids=${coverIds(t, m).join(',')}`, { method: 'DELETE' }); done('已恢复默认封面'); } catch (er) { toast(er.message); }
      return;
    }
    if (a && a.dataset.a === 'net') {
      box.innerHTML = '<div class="note"><div class="spin"></div>正在查找…</div>';
      const seen = new Set();
      const take = (r) => (r && r.items || []).filter((c) => c.art && !seen.has(c.art) && seen.add(c.art));
      const ar = t.dArtist === '未知歌手' ? '' : t.dArtist;
      let r = null; try { r = await api(`/api/match?id=${t.id}&title=${encodeURIComponent(t.dAlbum !== '未知专辑' ? t.dAlbum : t.dTitle)}&artist=${encodeURIComponent(ar)}`); } catch (er) { r = { ok: false, error: er.message }; }
      cands = take(r);
      if (!cands.length) {
        try { const r2 = await api(`/api/match?id=${t.id}&title=${encodeURIComponent(t.rawTitle || t.title)}&artist=${encodeURIComponent(t.rawArtist || '')}`); cands = take(r2); if (!r.ok && r2.ok) r = r2; } catch (_) {}
      }
      if (!document.body.contains(box)) return;
      if (!cands.length && r && !r.ok) { box.innerHTML = `<div class="note">查找失败：${esc(r.error || '')}</div>`; return; }
      show(); return;
    }
    const c = e.target.closest('.cvc');
    if (c) {
      const x = cands[+c.dataset.i]; if (!x) return;
      toast('正在设置封面…');
      try { await api('/api/cover/url', { method: 'POST', body: { ids: coverIds(t, m), url: x.art } }); done('封面已更换'); } catch (er) { toast(er.message); }
    }
  };
}
$('#coverFile').addEventListener('change', async () => {
  const f = $('#coverFile').files[0]; if (!f || !coverTarget) return;
  const { t, ids } = coverTarget; coverTarget = null;
  try {
    await api(`/api/cover?id=${t.id}&ids=${ids.join(',')}`, { method: 'POST', body: f, raw: true });
    if (modalKind() === '' && modalOpen()) closeModal();
    await reloadKeep(); toast('封面已更换');
  } catch (e) { toast(e.message); }
});

// ---------------- 歌曲信息 ----------------
function songInfo(t) {
  const kbps = t.duration > 0 ? Math.round(t.size * 8 / t.duration) : 0; // bits per ms = kbit/s
  const rows = [['标题', t.dTitle], ['歌手', t.dArtist], ['专辑', t.dAlbum]];
  if (t.edited) rows.push(['原标签', `${t.rawTitle || '（无）'} · ${t.rawArtist || '未知歌手'} · ${t.rawAlbum || '未知专辑'}`]);
  rows.push(['时长', t.duration ? fmtMs(t.duration) : '未知'], ['音轨', t.trackNo || '-'], ['格式', (t.ext || '').toUpperCase()],
    ['大小', fmtSize(t.size)], ['码率', kbps ? `约 ${kbps} kbps（平均）` : '未知'], ['文件名', t.fileName], ['位置', t.path]);
  const m = modal(`<h3>歌曲信息<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body"><table class="info-tbl">${rows.map(([k, v]) => `<tr><td>${k}</td><td>${esc(v)}</td></tr>`).join('')}</table></div><div class="foot"><button class="btn ghost" id="iReveal">在文件夹中显示</button><button class="btn primary" data-close>关闭</button></div>`);
  $('#iReveal', m).onclick = () => api('/api/reveal?id=' + t.id, { method: 'POST' });
}

// ---------------- 识别中文歌名（批量） ----------------
const needsZh = (t) => !t.edited && !t.tried && !(hasCjk(t.title) && hasCjk(t.artist));
const batch = { running: false, list: [], i: 0, ok: 0, errs: 0, timer: 0 };
function startBatch() {
  batch.list = S.tracks.filter(needsZh); batch.i = 0; batch.ok = 0; batch.errs = 0;
  if (!batch.list.length) return toast('没有需要识别的歌曲');
  batch.running = true; $('#taskBanner').classList.remove('hidden');
  refreshSettings();
  batchNext();
}
function stopBatch(msg) {
  batch.running = false; clearTimeout(batch.timer); $('#taskBanner').classList.add('hidden');
  reloadKeep(); toast(msg || `已停止：识别了 ${batch.ok} 首`);
}
async function batchNext() {
  if (!batch.running) return;
  if (batch.i >= batch.list.length) {
    batch.running = false; $('#taskBanner').classList.add('hidden'); await reloadKeep();
    toast(`识别完成：${batch.ok} 首改成了中文名` + (batch.list.length - batch.ok ? `，${batch.list.length - batch.ok} 首没找到（可在歌曲菜单里手动改）` : ''), 5000);
    return;
  }
  const t = batch.list[batch.i];
  const left = Math.ceil((batch.list.length - batch.i) * 3.5 / 60);
  $('#taskText').textContent = `正在识别中文名 ${batch.i + 1}/${batch.list.length}（已改 ${batch.ok} 首，约 ${left} 分钟）`;
  refreshSettings();
  let r;
  try { r = await api('/api/match?id=' + t.id); } catch (e) { r = { ok: false, error: e.message }; }
  if (!batch.running) return;
  if (r.ok && r.best >= 0) {
    const c = r.items[r.best];
    // 前面的歌可能已经把这首的歌手/专辑同步成中文了，这里用整首的识别结果覆盖
    try { await api('/api/meta', { method: 'POST', body: { id: t.id, title: toZh(c.title), artist: toZh(c.artist), album: toZh(c.album), src: 'online', spread: true, art: c.art, cover: 'ifMissing' } }); batch.ok++; } catch (_) {}
    batch.errs = 0;
  } else if (r.ok) { api('/api/meta/tried', { method: 'POST', body: { id: t.id } }).catch(() => {}); batch.errs = 0; }
  else if (r.tooFrequent) { $('#taskText').textContent = '查询太频繁，休息一分钟后继续…'; batch.timer = setTimeout(batchNext, 60000); return; }
  else if (++batch.errs >= 3) { stopBatch(`识别已暂停：${r.error || '网络错误'}。已改 ${batch.ok} 首，稍后可以继续`); refreshSettings(); return; }
  batch.i++;
  if (batch.i % 10 === 0) await reloadKeep();
  batchNext();
}
$('#taskCancel').onclick = (e) => { e.stopPropagation(); stopBatch(); refreshSettings(); };

// ---------------- player ----------------
const curTrack = () => (S.qi >= 0 && S.qi < S.queue.length) ? S.byId.get(S.queue[S.qi]) : null;

function shuffled(arr, firstId) {
  const a = arr.slice();
  for (let i = a.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [a[i], a[j]] = [a[j], a[i]]; }
  if (firstId) { const k = a.indexOf(firstId); if (k > 0) { a.splice(k, 1); a.unshift(firstId); } }
  return a;
}

function playList(list, index, mode) {
  if (!list || !list.length) return;
  if (mode) S.mode = mode;
  const ids = list.map((t) => t.id);
  S.base = ids;
  if (S.mode === 'shuffle') {
    const first = index >= 0 ? ids[index] : ids[Math.floor(Math.random() * ids.length)];
    S.queue = shuffled(ids, first); S.qi = 0;
  } else {
    S.queue = ids.slice(); S.qi = Math.max(0, index);
  }
  updateModeBtn();
  loadCurrent(true);
}
function playNext(t) {
  if (!curTrack()) return playList([t], 0);
  if (S.queue[S.qi] === t.id) return toast('正在播放这首歌');
  const at = S.queue.indexOf(t.id);
  if (at >= 0) { S.queue.splice(at, 1); if (at < S.qi) S.qi--; }
  S.queue.splice(S.qi + 1, 0, t.id);
  if (!S.base.includes(t.id)) S.base.push(t.id);
  toast('将在下一首播放'); saveStateSoon(); renderQueue();
}
function addToQueue(t) {
  if (!curTrack()) return playList([t], 0);
  if (S.queue[S.qi] === t.id) return toast('正在播放这首歌');
  const at = S.queue.indexOf(t.id);
  if (at >= 0) { S.queue.splice(at, 1); if (at < S.qi) S.qi--; }
  S.queue.push(t.id);
  if (!S.base.includes(t.id)) S.base.push(t.id);
  toast('已添加到播放队列'); saveStateSoon(); renderQueue();
}
function stopPlayback() {
  audio.pause(); audio.removeAttribute('src'); audio.load();
  S.queue = []; S.base = []; S.qi = -1;
  closePlayer(); updateNowPlaying(); updatePlayBtn(); updateProgress(); markCurrent(); renderQueue(); saveStateSoon(); deskPush(true);
}
function removeFromQueue(i) {
  const id = S.queue[i]; if (id === undefined) return;
  S.base = S.base.filter((x) => x !== id);
  if (i === S.qi) {
    const wasPlaying = !audio.paused;
    S.queue.splice(i, 1);
    if (!S.queue.length) return stopPlayback();
    if (S.qi >= S.queue.length) S.qi = 0;
    loadCurrent(wasPlaying);
  } else {
    S.queue.splice(i, 1);
    if (i < S.qi) S.qi--;
    renderQueue(); saveStateSoon();
  }
}
function moveInQueue(from, to) {
  if (from === to || from < 0 || from >= S.queue.length) return;
  const cur = S.queue[S.qi];
  const [id] = S.queue.splice(from, 1);
  if (to > from) to--;
  S.queue.splice(Math.max(0, Math.min(to, S.queue.length)), 0, id);
  S.qi = S.queue.indexOf(cur);
  if (S.mode !== 'shuffle') S.base = S.queue.slice();
  renderQueue(); saveStateSoon();
}

let pendingSeek = 0;
function loadCurrent(autoplay, seekTo = 0) {
  const t = curTrack();
  if (!t) return;
  pendingSeek = seekTo;
  audio.src = '/media?id=' + t.id;
  audio.volume = S.vol; audio.muted = S.muted;
  if (autoplay) audio.play().catch((e) => { if (e.name !== 'AbortError') onPlayError(); });
  updateNowPlaying(); markCurrent(); loadLyrics(t); saveStateSoon(); renderQueue();
}
function onPlayError() {
  const t = curTrack(); if (!t) return;
  toast(`无法播放「${t.dTitle}」，跳到下一首`);
  errCount++;
  if (errCount < Math.min(5, S.queue.length)) setTimeout(() => next(true), 800);
}
let errCount = 0;
audio.addEventListener('error', () => { if (audio.getAttribute('src')) onPlayError(); });
audio.addEventListener('playing', () => { errCount = 0; });
audio.addEventListener('loadedmetadata', () => {
  if (pendingSeek > 0 && pendingSeek < audio.duration) audio.currentTime = pendingSeek;
  pendingSeek = 0;
  const t = curTrack();
  if (t && isFinite(audio.duration) && audio.duration > 0) {
    const ms = Math.round(audio.duration * 1000);
    if (!t.duration || Math.abs(t.duration - ms) > 2000) {
      t.duration = ms; api('/api/duration', { method: 'POST', body: { id: t.id, duration: ms } }).catch(() => {});
      const r = document.querySelector(`.row[data-id="${t.id}"] .d`); if (r) r.textContent = fmtTime(audio.duration);
    }
  }
  updateProgress();
});
audio.addEventListener('ended', () => {
  if (S.mode === 'one') { audio.currentTime = 0; audio.play(); return; }
  next(true);
});
audio.addEventListener('play', () => { updatePlayBtn(); deskPush(false); });
audio.addEventListener('pause', () => { updatePlayBtn(); saveStateSoon(); deskPush(false); });
['playing', 'seeked', 'ratechange', 'loadedmetadata'].forEach((ev) => audio.addEventListener(ev, () => deskPush(false)));
audio.addEventListener('timeupdate', () => { updateProgress(); syncLyrics(); if (!audio.paused && Date.now() - DESK.pushed > 15000) deskPush(false); else if (!audio.paused && Date.now() - npPushed > 15000) npPush(); });

function togglePlay() {
  if (!curTrack()) { if (S.tracks.length) playList(currentList.length ? currentList : S.tracks, 0); return; }
  if (!audio.getAttribute('src')) return loadCurrent(true);
  if (audio.paused) audio.play().catch(() => {}); else audio.pause();
}
function next() {
  if (!S.queue.length) return;
  if (S.qi + 1 >= S.queue.length) {
    if (S.mode === 'shuffle') { S.queue = shuffled(S.base.length ? S.base : S.queue); }
    S.qi = 0;
  } else S.qi++;
  loadCurrent(true);
}
function prev() {
  if (!S.queue.length) return;
  if (audio.currentTime > 4) { audio.currentTime = 0; return; }
  S.qi = S.qi - 1 < 0 ? S.queue.length - 1 : S.qi - 1;
  loadCurrent(true);
}
const MODES = ['seq', 'one', 'shuffle'];
const MODE_NAME = { seq: '列表循环', one: '单曲循环', shuffle: '随机播放' };
function cycleMode() {
  const m = MODES[(MODES.indexOf(S.mode) + 1) % 3];
  setMode(m); toast(MODE_NAME[m]);
}
function setMode(m) {
  const cur = curTrack();
  if (m === 'shuffle' && S.mode !== 'shuffle') { S.queue = shuffled(S.base.length ? S.base : S.queue, cur && cur.id); S.qi = cur ? 0 : -1; }
  if (m !== 'shuffle' && S.mode === 'shuffle' && S.base.length) { S.queue = S.base.slice(); S.qi = cur ? S.queue.indexOf(cur.id) : -1; }
  S.mode = m; updateModeBtn(); saveStateSoon(); renderQueue();
}
function updateModeBtn() {
  const b = $('#btnMode');
  b.innerHTML = S.mode === 'one' ? ICON.one : S.mode === 'shuffle' ? ICON.shuffle : ICON.repeat;
  b.title = MODE_NAME[S.mode] + '（点击切换）';
  b.classList.toggle('on', S.mode !== 'seq');
}
function updatePlayBtn() {
  const playing = !audio.paused;
  $('#btnPlay').innerHTML = playing ? ICON.pause : ICON.play;
  if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused';
}
function updateNowPlaying() {
  const t = curTrack();
  $('#npTitle').textContent = t ? t.dTitle : '未在播放';
  $('#npArtist').textContent = t ? t.dArtist : '选择一首歌开始播放';
  setCover($('#npCover'), t);
  const fav = $('#npFav'); fav.classList.toggle('hidden', !t); fav.classList.toggle('on', isFav(t)); fav.innerHTML = isFav(t) ? ICON.heartOn : ICON.heart;
  document.title = t ? `${t.dTitle} - ${t.dArtist} · 拾音` : '拾音';
  setCover($('#plCover'), t);
  $('#plTitle').textContent = t ? t.dTitle : '';
  $('#plArtist').textContent = t ? t.dArtist : '';
  $('#plAlbum').textContent = t ? t.dAlbum : '';
  if (t && 'mediaSession' in navigator) {
    try {
      navigator.mediaSession.metadata = new MediaMetadata({ title: t.dTitle, artist: t.dArtist, album: t.dAlbum,
        artwork: t.cover ? [{ src: location.origin + coverUrl(t), sizes: '512x512' }] : [] });
    } catch (_) {}
  }
}
if ('mediaSession' in navigator) {
  const ms = navigator.mediaSession;
  try {
    ms.setActionHandler('play', () => togglePlay());
    ms.setActionHandler('pause', () => audio.pause());
    ms.setActionHandler('previoustrack', prev);
    ms.setActionHandler('nexttrack', () => next());
  } catch (_) {}
}

// sliders
function slider(el, onSet, onDrag) {
  const pct = (e) => { const r = el.querySelector('.track').getBoundingClientRect(); return Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)); };
  el.addEventListener('mousedown', (e) => {
    el.classList.add('drag'); onDrag(pct(e));
    const mv = (ev) => onDrag(pct(ev));
    const up = (ev) => { el.classList.remove('drag'); document.removeEventListener('mousemove', mv); document.removeEventListener('mouseup', up); onSet(pct(ev)); };
    document.addEventListener('mousemove', mv); document.addEventListener('mouseup', up);
  });
}
let seeking = false;
function drawSeek(p) { $('#seekFill').style.width = p * 100 + '%'; $('#seekKnob').style.left = p * 100 + '%'; }
slider($('#seek'), (p) => { seeking = false; if (isFinite(audio.duration)) audio.currentTime = p * audio.duration; lyrUserScrollT = 0; saveStateSoon(); },
  (p) => { seeking = true; drawSeek(p); $('#tCur').textContent = fmtTime(p * (audio.duration || 0)); });
function updateProgress() {
  const d = audio.duration, c = audio.currentTime;
  const t = curTrack();
  const dur = isFinite(d) && d > 0 ? d : (t ? t.duration / 1000 : 0);
  if (!seeking) { drawSeek(dur ? c / dur : 0); $('#tCur').textContent = fmtTime(t ? c : 0); }
  $('#tDur').textContent = fmtTime(dur);
}
function drawVol() {
  const v = S.muted ? 0 : S.vol;
  $('#volFill').style.width = v * 100 + '%'; $('#volKnob').style.left = v * 100 + '%';
  $('#btnMute').innerHTML = v === 0 ? ICON.mute : ICON.vol;
}
slider($('#volume'), () => saveStateSoon(), (p) => { S.vol = p; S.muted = false; audio.volume = p; audio.muted = false; drawVol(); });
$('#btnMute').onclick = () => { S.muted = !S.muted; audio.muted = S.muted; drawVol(); saveStateSoon(); };
$('#btnPlay').onclick = togglePlay;
$('#btnNext').onclick = () => next();
$('#btnPrev').onclick = prev;
$('#btnMode').onclick = cycleMode;
$('#npFav').onclick = (e) => { e.stopPropagation(); toggleFav(curTrack()); };
$('#npInfo').onclick = () => togglePlayer();
$('#npInfo').addEventListener('contextmenu', (e) => { e.preventDefault(); const t = curTrack(); if (t) songMenu(t, e.clientX, e.clientY - 330); });
$('#btnLyr').onclick = () => togglePlayer();
$('#playerClose').onclick = () => closePlayer();
$('#plMore').onclick = (e) => { const t = curTrack(); const r = e.currentTarget.getBoundingClientRect(); if (t) songMenu(t, r.left - 200, r.bottom); };
$('#btnPhone').onclick = () => openPhone();
$('#btnSettings').onclick = () => openSettings();
$('#btnQueue').onclick = () => toggleQueue();

function togglePlayer() { if ($('#player').classList.contains('hidden')) openPlayer(); else closePlayer(); }
function openPlayer() { if (!curTrack()) return; closeQueue(); $('#player').classList.remove('hidden'); $('#btnLyr').classList.add('on'); S.lyrIdx = -1; drawOffset(); syncLyrics(true); }
function closePlayer() { $('#player').classList.add('hidden'); $('#btnLyr').classList.remove('on'); }

// ---------------- 播放队列 ----------------
const queueOpen = () => !$('#queuePanel').classList.contains('hidden');
function toggleQueue() { if (queueOpen()) closeQueue(); else openQueue(); }
function openQueue() {
  $('#queuePanel').classList.remove('hidden'); $('#btnQueue').classList.add('on'); renderQueue(true);
}
function closeQueue() { $('#queuePanel').classList.add('hidden'); $('#btnQueue').classList.remove('on'); }
function renderQueue(scrollToCur) {
  const p = $('#queuePanel');
  if (!queueOpen()) return;
  const list = p.querySelector('.q-list');
  const st = list ? list.scrollTop : 0;
  const rows = S.queue.map((id, i) => {
    const t = S.byId.get(id) || { dTitle: '(已移除)', dArtist: '' };
    return `<div class="q-row ${i === S.qi ? 'cur' : ''}" draggable="true" data-i="${i}"><span class="grip" title="拖动调整顺序">⋮⋮</span>${coverHTML(t.id ? t : null)}<div class="tx"><div class="t">${esc(t.dTitle)}</div><div class="a">${esc(t.dArtist)}</div></div><button class="icon-btn rm" data-rm="${i}" title="从播放队列移除">${ICON.x}</button></div>`;
  }).join('');
  p.innerHTML = `<div class="q-head"><h3>播放队列<span class="count">${S.queue.length} 首 · ${MODE_NAME[S.mode]}</span></h3>${S.queue.length ? '<button class="btn ghost" id="qClear" style="height:30px">清空</button>' : ''}<button class="icon-btn" id="qClose" title="关闭">${ICON.x}</button></div>
    <div class="q-list">${rows || '<div class="q-empty">播放队列是空的<br>在歌曲上点右键 →「添加到播放队列」</div>'}</div>`;
  const nl = p.querySelector('.q-list');
  if (scrollToCur) { const c = nl.querySelector('.q-row.cur'); if (c) c.scrollIntoView({ block: 'center' }); } else nl.scrollTop = st;
}
$('#queuePanel').addEventListener('click', (e) => {
  if (e.target.closest('#qClose')) return closeQueue();
  if (e.target.closest('#qClear')) return confirm2('清空播放队列？', '会停止播放，歌曲和歌单不受影响', '清空', () => { stopPlayback(); toast('已清空播放队列'); });
  const rm = e.target.closest('[data-rm]');
  if (rm) return removeFromQueue(+rm.dataset.rm);
  const row = e.target.closest('.q-row');
  if (row) { const i = +row.dataset.i; if (i === S.qi) { togglePlay(); return; } S.qi = i; loadCurrent(true); }
});
$('#queuePanel').addEventListener('contextmenu', (e) => {
  const row = e.target.closest('.q-row'); if (!row) return;
  e.preventDefault();
  const i = +row.dataset.i; const t = S.byId.get(S.queue[i]); if (!t) return;
  showMenu([{ label: '播放', fn: () => { S.qi = i; loadCurrent(true); } }, { label: '从播放队列移除', fn: () => removeFromQueue(i) }, '-',
    { label: '查看专辑', r: t.dAlbum, fn: () => openView({ type: 'album', name: t.dAlbum }) }, { label: '歌曲信息', fn: () => songInfo(t) }], e.clientX, e.clientY);
});
let dragFrom = -1;
$('#queuePanel').addEventListener('dragstart', (e) => {
  const row = e.target.closest('.q-row'); if (!row) return;
  dragFrom = +row.dataset.i; row.classList.add('drag');
  e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(dragFrom));
});
const clearOver = () => document.querySelectorAll('.q-row.over-top, .q-row.over-bot').forEach((r) => r.classList.remove('over-top', 'over-bot'));
$('#queuePanel').addEventListener('dragover', (e) => {
  const row = e.target.closest('.q-row'); if (!row || dragFrom < 0) return;
  e.preventDefault(); clearOver();
  const r = row.getBoundingClientRect();
  row.classList.add(e.clientY < r.top + r.height / 2 ? 'over-top' : 'over-bot');
});
$('#queuePanel').addEventListener('drop', (e) => {
  const row = e.target.closest('.q-row'); if (!row || dragFrom < 0) return;
  e.preventDefault();
  const r = row.getBoundingClientRect();
  const to = +row.dataset.i + (e.clientY < r.top + r.height / 2 ? 0 : 1);
  const from = dragFrom; dragFrom = -1; clearOver();
  moveInQueue(from, to);
});
$('#queuePanel').addEventListener('dragend', () => { dragFrom = -1; clearOver(); document.querySelectorAll('.q-row.drag').forEach((r) => r.classList.remove('drag')); });

// keyboard
document.addEventListener('keydown', (e) => {
  const inInput = e.target.matches('input, textarea');
  if (e.key === 'Escape') {
    if (!$('#menu').classList.contains('hidden')) return hideMenu();
    if (modalOpen() && !$('#overlay').dataset.sticky) return closeModal();
    if (queueOpen()) return closeQueue();
    if (!$('#player').classList.contains('hidden')) return closePlayer();
  }
  if (inInput || modalOpen()) return;
  if (e.code === 'Space') { e.preventDefault(); togglePlay(); }
  else if (e.ctrlKey && e.key === 'ArrowRight') { e.preventDefault(); next(); }
  else if (e.ctrlKey && e.key === 'ArrowLeft') { e.preventDefault(); prev(); }
  else if (e.ctrlKey && e.key === 'ArrowUp') { e.preventDefault(); S.vol = Math.min(1, S.vol + 0.05); audio.volume = S.vol; drawVol(); saveStateSoon(); }
  else if (e.ctrlKey && e.key === 'ArrowDown') { e.preventDefault(); S.vol = Math.max(0, S.vol - 0.05); audio.volume = S.vol; drawVol(); saveStateSoon(); }
  else if (e.ctrlKey && e.key.toLowerCase() === 'f') { e.preventDefault(); searchInp.focus(); searchInp.select(); }
});

// ---------------- lyrics ----------------
function parseLrc(text) {
  const lines = [];
  let offset = 0;
  const om = text.match(/\[offset:\s*([+-]?\d+)\s*]/i); if (om) offset = +om[1] / 1000;
  const re = /\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?]/g;
  for (const raw of text.split(/\r?\n/)) {
    const times = []; let m; re.lastIndex = 0; let last = 0;
    while ((m = re.exec(raw))) { if (m.index !== last) break; times.push(+m[1] * 60 + +m[2] + (m[3] ? +('0.' + m[3].padStart(m[3].length === 3 ? 3 : 2, '0')) : 0)); last = re.lastIndex; }
    if (!times.length) continue;
    const txt = raw.slice(last).replace(/<\d+:\d+(?:[.:]\d+)?>/g, '').trim();
    for (const t of times) lines.push({ t: Math.max(0, t - offset), text: txt });
  }
  lines.sort((a, b) => a.t - b.t);
  // merge same-timestamp lines (original + translation)
  const out = [];
  for (const l of lines) {
    const p = out[out.length - 1];
    if (p && Math.abs(p.t - l.t) < 0.01 && l.text) { if (!p.text) p.text = l.text; else p.tr = p.tr ? p.tr + ' ' + l.text : l.text; }
    else out.push({ ...l });
  }
  return out.filter((l, i) => l.text || (out[i + 1] && out[i + 1].t - l.t > 2));
}
let lyrSeq = 0;
const SRC_NAME = { file: '本地 .lrc 歌词', embedded: '歌曲内嵌歌词', cache: '已保存的歌词', manual: '手动选择', qq: 'QQ音乐', kugou: '酷狗', netease: '网易云音乐', lrclib: 'LRCLIB' };
async function loadLyrics(t, force) {
  const seq = ++lyrSeq;
  S.lyr = null; S.lyrIdx = -1; S.lyrTrack = t.id; deskPush(true);
  const box = $('#lyrics');
  box.className = 'lyrics'; box.innerHTML = '<div class="msg">正在查找歌词…</div>'; $('#lyrSrc').textContent = '';
  drawOffset();
  let r = null;
  try { r = await api(`/api/lyrics?id=${t.id}${force ? '&force=1&online=1' : ''}`); } catch (_) {}
  if (seq !== lyrSeq) return;
  if ((!r || r.source === 'none') && S.cfg.onlineLyrics && !force) {
    box.innerHTML = '<div class="msg">正在网上搜索歌词…</div>';
    try { r = await api(`/api/lyrics?id=${t.id}&online=1`); } catch (_) {}
    if (seq !== lyrSeq) return;
  }
  showLyrics(r);
}
function showLyrics(r) {
  try { showLyrics0(r); } finally { deskPush(true); }
}
function showLyrics0(r) {
  const box = $('#lyrics');
  if (!r || r.source === 'none' || !r.text || !r.text.trim()) {
    S.lyr = null;
    box.className = 'lyrics';
    box.innerHTML = `<div class="msg">暂无歌词${S.cfg.onlineLyrics ? '' : '<br><span style="font-size:13px">（设置里可以打开「自动在线搜索歌词」）</span>'}<br><button class="btn ghost" id="lyrRetry">重试</button> <button class="btn ghost" id="lyrSearch2">手动搜索歌词</button></div>`;
    $('#lyrRetry').onclick = () => { const t = curTrack(); if (t) loadLyrics(t, true); };
    $('#lyrSearch2').onclick = openLyricSearch;
    $('#lyrSrc').textContent = '';
    return;
  }
  const text = t2s(r.text);
  $('#lyrSrc').textContent = '歌词来源：' + (SRC_NAME[r.source] || r.source);
  const parsed = r.synced ? parseLrc(text) : null;
  if (parsed && parsed.length) {
    S.lyr = parsed;
    box.className = 'lyrics';
    box.innerHTML = '<div class="pad"></div>' + parsed.map((l, i) => `<p data-i="${i}">${esc(l.text) || '&nbsp;'}</p>${l.tr ? `<p class="tr" data-i="${i}">${esc(l.tr)}</p>` : ''}`).join('') + '<div class="pad"></div>';
    syncLyrics(true);
  } else {
    S.lyr = null;
    box.className = 'lyrics plain';
    const lines = text.replace(/\[[a-z]+:[^\]]*]/gi, '').replace(/\[\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?]/g, '').split(/\r?\n/);
    box.innerHTML = '<div style="height:20px"></div>' + lines.map((l) => `<p>${esc(l) || '&nbsp;'}</p>`).join('') + '<div style="height:40px"></div>';
  }
}
// 歌词偏移（每首歌单独保存；正数 = 歌词提前）
const lyrOffset = () => { const t = curTrack(); return t && S.prefs.lyricOffsets ? (S.prefs.lyricOffsets[t.key] || 0) : 0; };
function drawOffset() { const v = lyrOffset(); $('#offLabel').textContent = `偏移 ${v > 0 ? '+' : ''}${(v / 1000).toFixed(1)}s`; }
function shiftOffset(d) {
  const t = curTrack(); if (!t) return;
  const offs = Object.assign({}, S.prefs.lyricOffsets || {});
  const v = d === null ? 0 : Math.round(((offs[t.key] || 0) + d) / 100) * 100;
  if (v) offs[t.key] = v; else delete offs[t.key];
  savePrefs({ lyricOffsets: offs });
  drawOffset(); lyrUserScrollT = 0; S.lyrIdx = -2; syncLyrics(true); deskPush(false);
  if (d === null) toast('已清除歌词偏移');
}
$('#offMinus').onclick = () => shiftOffset(-500);
$('#offPlus').onclick = () => shiftOffset(500);
$('#offReset').onclick = () => shiftOffset(null);
$('#lyrSearchBtn').onclick = () => openLyricSearch();
$('#lyrics').addEventListener('click', (e) => {
  const p = e.target.closest('p[data-i]'); if (!p || !S.lyr) return;
  audio.currentTime = Math.max(0, S.lyr[+p.dataset.i].t - lyrOffset() / 1000 + 0.05); lyrUserScrollT = 0; if (audio.paused) audio.play();
});
let lyrUserScrollT = 0;
$('#lyrics').addEventListener('wheel', () => { lyrUserScrollT = Date.now(); });
function syncLyrics(force) {
  if (!S.lyr || $('#player').classList.contains('hidden')) return;
  const t = audio.currentTime + 0.25 + lyrOffset() / 1000;
  let i = -1;
  for (let k = 0; k < S.lyr.length; k++) { if (S.lyr[k].t <= t) i = k; else break; }
  if (i === S.lyrIdx && !force) return;
  S.lyrIdx = i;
  const box = $('#lyrics');
  box.querySelectorAll('p.on').forEach((p) => p.classList.remove('on'));
  const el = box.querySelector(`p[data-i="${i}"]:not(.tr)`);
  if (el) {
    el.classList.add('on');
    if (Date.now() - lyrUserScrollT > 3000) box.scrollTop = el.offsetTop - box.clientHeight / 2 + el.clientHeight / 2;
  } else if (force) box.scrollTop = 0;
}

// 手动搜索歌词：同时查找 QQ音乐、酷狗、网易云和 LRCLIB
let lyrReq = 0;
function openLyricSearch() {
  const t = curTrack(); if (!t) return;
  let results = [];
  const m = modal(`<h3>搜索歌词<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body">
    <div style="color:var(--fg3);font-size:13px">同时查找 QQ音乐、酷狗、网易云和 LRCLIB，选中后会保存在本机</div>
    <div class="row2"><div class="field"><label>歌名</label><input type="text" id="lsTitle" value="${esc(t.dTitle)}"></div><div class="field"><label>歌手（可留空）</label><input type="text" id="lsArtist" value="${esc(t.dArtist === '未知歌手' ? '' : t.dArtist)}"></div></div>
    <div class="res-list" id="lsRes"></div></div>
    <div class="foot"><button class="btn ghost" id="lsReset" style="margin-right:auto">恢复自动匹配</button><button class="btn ghost" data-close>取消</button><button class="btn primary" id="lsGo">搜索</button></div>`, 'wide');
  m.style.height = 'auto';
  const box = $('#lsRes', m);
  const go = async () => {
    const title = $('#lsTitle', m).value.trim(), artist = $('#lsArtist', m).value.trim();
    if (!title) return toast('请输入歌名');
    const req = ++lyrReq;
    box.innerHTML = '<div class="note"><div class="spin"></div>正在搜索…</div>';
    let r; try { r = await api(`/api/lyrics/search?title=${encodeURIComponent(title)}&artist=${encodeURIComponent(artist)}`); } catch (e) { r = { ok: false, error: e.message }; }
    if (req !== lyrReq || !document.body.contains(box)) return;
    if (!r.ok) { box.innerHTML = `<div class="note">搜索失败：${esc(r.error || '')}</div>`; return; }
    const d = t.duration / 1000;
    results = (r.items || []).sort((a, b) => (b.synced - a.synced) || (Math.abs(a.duration - d) - Math.abs(b.duration - d)));
    box.innerHTML = results.length ? results.map((x, i) => {
      const diff = d && x.duration ? Math.round(x.duration - d) : null;
      const pv = t2s(String(x.text)).replace(/\[[^\]]*\]/g, '').split('\n').map((s) => s.trim()).filter(Boolean).slice(0, 3).join(' / ');
      return `<button class="res" data-i="${i}"><span class="mt"><div class="t1">${esc(t2s(x.title))}${x.synced ? '<span class="tag best">滚动</span>' : '<span class="tag">纯文本</span>'}<span class="tag">${esc(x.src || '')}</span></div><div class="t2">${esc(t2s(x.artist))}${x.album ? ' · ' + esc(t2s(x.album)) : ''} · ${fmtTime(x.duration)}${diff !== null ? `（${diff >= 0 ? '+' : ''}${diff}s）` : ''}</div><div class="pv">${esc(pv)}</div></span></button>`;
    }).join('') : '<div class="note">没有找到，换个关键词试试</div>';
  };
  $('#lsGo', m).onclick = go;
  m.querySelectorAll('#lsTitle, #lsArtist').forEach((i) => { i.onkeydown = (e) => { if (e.key === 'Enter') go(); }; });
  $('#lsReset', m).onclick = async () => { try { await api('/api/lyrics/reset', { method: 'POST', body: { id: t.id } }); } catch (_) {} closeModal(); if (curTrack() === t) loadLyrics(t); toast('已恢复自动匹配'); };
  box.onclick = async (e) => {
    const b = e.target.closest('.res'); if (!b) return;
    const x = results[+b.dataset.i]; if (!x) return;
    try { await api('/api/lyrics/save', { method: 'POST', body: { id: t.id, text: x.text } }); } catch (er) { return toast(er.message); }
    closeModal();
    if (curTrack() === t) { lyrSeq++; showLyrics({ source: 'manual', text: x.text, synced: x.synced }); openPlayer(); }
    toast('已应用并保存歌词');
  };
  go();
}

// ---------------- persist state ----------------
let saveT;
function stateObj() {
  const keys = (ids) => ids.map((id) => { const t = S.byId.get(id); return t ? t.key : ''; });
  return { queue: S.queue, base: S.base, qi: S.qi, qkeys: keys(S.queue), bkeys: keys(S.base), pos: audio.currentTime || 0, vol: S.vol, muted: S.muted, mode: S.mode, tab: S.tab };
}
function saveStateSoon() { clearTimeout(saveT); saveT = setTimeout(saveState, 800); }
function saveState() { clearTimeout(saveT); return fetch('/api/state', { method: 'PUT', body: JSON.stringify(stateObj()), keepalive: true }).catch(() => {}); }
setInterval(() => { if (!audio.paused) saveState(); }, 5000);
window.addEventListener('beforeunload', saveState);
window.addEventListener('pagehide', saveState);

// ids change when files move (整理) — fall back to the stable key (file name + size)
function remapIds(ids, keys) {
  const out = [];
  (Array.isArray(ids) ? ids : []).forEach((id, i) => {
    let v = S.byId.has(id) ? id : null;
    if (!v && keys && keys[i] && S.byKey.has(keys[i])) v = S.byKey.get(keys[i]).id;
    if (v && !out.includes(v)) out.push(v);
  });
  return out;
}
async function restoreState() {
  let st = {};
  try { st = await api('/api/state'); } catch (_) {}
  S.vol = typeof st.vol === 'number' ? st.vol : 0.8; S.muted = !!st.muted;
  S.mode = MODES.includes(st.mode) ? st.mode : 'seq';
  S.tab = 'songs'; // 0.2.9: always open on 歌曲
  const curArr = remapIds(Array.isArray(st.queue) ? [st.queue[st.qi]] : [], Array.isArray(st.qkeys) ? [st.qkeys[st.qi]] : []);
  S.queue = remapIds(st.queue, st.qkeys); S.base = remapIds(st.base, st.bkeys);
  if (!S.base.length) S.base = S.queue.slice();
  S.qi = curArr.length ? S.queue.indexOf(curArr[0]) : -1;
  audio.volume = S.vol; audio.muted = S.muted;
  drawVol(); updateModeBtn(); updatePlayBtn();
  if (S.qi >= 0) loadCurrent(false, st.pos || 0);
  render();
}

// ---------------- scanning ----------------
let manualScan = false;
async function rescan() {
  try { await api('/api/scan', { method: 'POST' }); toast('开始扫描'); } catch (e) { return toast(e.message); }
  manualScan = true;
  pollScan();
}
let scanPoll;
function pollScan() {
  clearInterval(scanPoll);
  scanPoll = setInterval(async () => {
    let s; try { s = await api('/api/scan'); } catch (_) { return; }
    S.scan = s;
    const sb = document.querySelector('.scanbar');
    if (s.running) {
      if (sb) sb.innerHTML = `<div class="spin"></div>正在扫描音乐… 已找到 ${s.found} 首${s.total ? `，读取中 ${s.done}/${s.total}` : ''}`; else if (!S.stack.length) render(true);
      const ss = $('#scanStatus'); if (ss) ss.textContent = `扫描中… ${s.done}/${s.total || s.found}`;
    } else {
      clearInterval(scanPoll);
      const before = S.tracks.length;
      await loadLibrary();
      const n = S.tracks.length;
      toast(`扫描完成，共 ${n} 首` + (before && n !== before ? `（${n > before ? '+' : ''}${n - before}）` : '') + (s.usb ? ` · ${s.usb}` : ''), s.usb ? 5000 : undefined);
      const wasManual = manualScan; manualScan = false;
      if (wasManual && n > 0 && S.cfg.autoOrganize && S.outside > 0) setTimeout(() => organize(true), 600);
    }
  }, 700);
}

// ---------------- 整理到拾音文件夹 ----------------
async function organize(auto) {
  if (S.org && S.org.running) return;
  const ctx = { st: stateObj(), playing: !audio.paused, pos: audio.currentTime || 0 };
  const cur = curTrack();
  // 正在播放的文件也要移动：先释放它
  if (cur) { audio.pause(); audio.removeAttribute('src'); audio.load(); }
  let o;
  try { o = await api('/api/organize', { method: 'POST', body: { auto: !!auto } }); } catch (e) { toast(e.message); if (cur) loadCurrent(ctx.playing, ctx.pos); return; }
  S.org = o; render(true); refreshSettings();
  const seq0 = o.seq;
  while (true) {
    await new Promise((r) => setTimeout(r, 500));
    try { o = await api('/api/organize'); } catch (_) { continue; }
    S.org = o;
    const sb = document.querySelector('.scanbar');
    if (o.running) { if (sb) sb.innerHTML = `<div class="spin"></div>正在整理${o.total ? ` ${o.done}/${o.total}` : ''}…`; else render(true); continue; }
    if (o.seq !== seq0 || !o.running) break;
  }
  await loadLibrary();
  S.queue = remapIds(ctx.st.queue, ctx.st.qkeys); S.base = remapIds(ctx.st.base, ctx.st.bkeys);
  const curId = remapIds([ctx.st.queue[ctx.st.qi]], [ctx.st.qkeys[ctx.st.qi]])[0];
  S.qi = curId ? S.queue.indexOf(curId) : (S.queue.length ? 0 : -1);
  if (S.qi >= 0) loadCurrent(ctx.playing, curId ? ctx.pos : 0); else { updateNowPlaying(); renderQueue(); }
  if (o.error) return toast(o.error, 4000);
  try { S.cfg = await api('/api/config'); } catch (_) {}
  refreshSettings();
  const parts = [`已移入 ${o.moved} 首`];
  if (o.dup) parts.push(`${o.dup} 首已存在`);
  if (o.failed) parts.push(`${o.failed} 首移动失败`);
  toast((auto ? '新歌已整理：' : '整理完成：') + parts.join('，'), 4000);
}

// ---------------- 设置（项目、顺序和用词与手机版一致；手机独有的权限/电池等项不显示） ----------------
const settingsOpen = () => modalKind() === 'settings';
function refreshSettings() { if (settingsOpen()) renderSettings(); }
async function openSettings() {
  try { S.cfg = await api('/api/config'); } catch (_) {}
  const m = modal(`<h3>设置<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body" id="setBody"></div>`, 'wide');
  m.style.height = 'auto';
  $('#overlay').dataset.kind = 'settings';
  renderSettings();
  $('#setBody', m).addEventListener('click', onSettingsClick);
}
function setRow(a, ttl, sub, right, extra) {
  return `<div class="set-row ${a ? 'click' : ''}" ${a ? `data-a="${a}"` : ''} ${extra || ''}><div class="main"><div class="ttl">${ttl}</div>${sub ? `<div class="sub">${sub}</div>` : ''}</div>${right || ''}</div>`;
}
function renderSettings() {
  const body = $('#setBody'); if (!body) return;
  const st = body.scrollTop;
  const c = S.cfg;
  const albums = albumGroups().size, artists = artistGroups().size;
  const when = S.scanned ? new Date(S.scanned * 1000).toLocaleString('zh-CN', { hour12: false }) : '从未';
  const pending = S.tracks.filter(needsZh).length, autoCount = S.tracks.filter((t) => t.auto).length;
  const orgRunning = S.org && S.org.running;
  const sw = (on, id) => `<button class="switch ${on ? 'on' : ''}" ${id ? `id="${id}"` : ''} tabindex="-1"></button>`;
  const theme = c.theme || 'dark';
  body.innerHTML = `
    <div class="set-sec"><h4>音乐库</h4>
      ${setRow('scan', '重新扫描', `<span id="scanStatus">共 ${S.tracks.length} 首 · ${albums} 张专辑 · ${artists} 位歌手</span><br>上次扫描：${esc(when)}<br>音乐库会保存下来，打开时不再自动扫描；下载新歌后点这里`, `<button class="btn primary" id="sScan" data-a="scan">重新扫描</button>`)}
      ${setRow('addf', '添加文件夹', '把其他文件夹（比如 D:\\音乐）也加入音乐库（同目录的 .lrc 歌词也会被读取）', `<button class="btn ghost" id="sAdd" data-a="addf">${ICON.plus}添加</button>`)}
      ${setRow('org', '整理到拾音文件夹', `把歌曲和同名歌词文件移到 ${esc(c.musicDir || '')}，方便管理。${orgRunning ? '正在整理…' : S.outside ? `还有 ${S.outside} 首不在里面` : '全部歌曲都已在里面'}`, `<span class="r ${S.outside ? '' : 'ok'}">${orgRunning ? '整理中…' : S.outside ? '整理' : '✓'}</span>`)}
      ${setRow('autoorg', '扫描后自动整理新歌', '手动扫描找到的新歌自动移入拾音文件夹', sw(c.autoOrganize, 'sAutoOrg'))}
      ${setRow('zh', '识别中文歌名', `英文歌名/歌手名的华语歌，联网找到对应的中文名和封面（不修改文件）。${batch.running ? `进行中 ${batch.i}/${batch.list.length}` : `待识别 ${pending} 首`}`, `<span class="r">${batch.running ? '停止' : '开始'}</span>`, 'id="sZh"')}
      ${autoCount ? setRow('zhundo', '撤销自动识别', `把 ${autoCount} 首自动改过名字的歌恢复成文件里的原名（手动改的不受影响）`, '', 'id="sZhUndo"') : ''}
      ${setRow('', '拾音文件夹', `${esc(c.musicDir || '')}<br>主文件夹：从手机导入的歌、整理后的歌都放在这里`, `<button class="btn ghost" id="sOpen" data-a="open">打开</button><button class="btn ghost" id="sChange" data-a="change">更改</button>`)}
      <div id="sExtra">${(c.extraDirs || []).map((d, i) => setRow('', esc(d), '已添加的文件夹', `<button class="btn ghost" data-a="rmf" data-i="${i}">移除</button>`)).join('')}</div>
    </div>
    <div class="set-sec"><h4>手机</h4>
      ${setRow('phone', '从手机导入', '手机和电脑连同一个 WiFi，在手机拾音打开「设置 → 传到电脑」，一键导入全部歌曲、歌词、中文名、封面和歌单', '<span class="r">打开 ›</span>')}
      ${setRow('netfix', '网络修复', '电脑开过热点后死机、重启后代理或网络一直超时时的修复步骤', '<span class="r">查看 ›</span>')}
    </div>
    <div class="set-sec"><h4>外观</h4>
      ${setRow('', '主题', '深色、浅色或跟随 Windows 系统设置（浅色为白底黑字、红色点缀，对比度高）', `<div class="seg" id="sTheme">${['dark', 'light', 'system'].map((k) => `<button data-a="theme" data-v="${k}" class="${theme === k ? 'on' : ''}">${THEME_NAME[k]}</button>`).join('')}</div>`)}
    </div>
    <div class="set-sec"><h4>歌词</h4>
      ${setRow('t2s', '歌词转简体', '把繁体歌词、歌名、歌手和专辑名显示为简体中文（不修改文件）', sw(c.t2s, 'sT2S'))}
      ${setRow('desk', '桌面歌词', '在桌面上显示当前歌词（置顶、透明背景）；鼠标移上去可切歌、调字号和颜色、锁定' + (DESK.hotkey ? `<br><span id="sDeskHotkey">快捷键 ${esc(DESK.hotkey)} 锁定/解锁</span>` : ''), sw(deskOn(), 'sDesk'))}
      ${deskOn() && DESK.prefs.locked ? setRow('deskunlock', '解锁桌面歌词', '锁定后桌面歌词可点穿；鼠标移到歌词上会出现「解锁」按钮，也可以点播放栏的「词」' + (DESK.hotkey ? '或按 ' + esc(DESK.hotkey) : '') + '解锁', '<span class="r">解锁</span>') : ''}
      ${deskOn() ? setRow('deskreset', '桌面歌词回到默认位置', '找不到桌面歌词时用：移回主屏幕底部居中', '<span class="r">复位</span>') : ''}
      ${setRow('online', '自动在线搜索歌词', '本地没有歌词时依次搜索 QQ音乐 → 酷狗 → 网易云 → LRCLIB，优先滚动歌词，找到后保存', sw(c.onlineLyrics, 'sOnline'))}
    </div>
    <div class="set-sec"><h4>其他</h4>
      ${setRow('upd', '检查更新', `当前版本 ${esc(S.info ? S.info.version : '')}<span id="updSetMsg" class="${upd.msgCls}">${upd.msg ? ' · ' + esc(upd.msg) : ''}</span>`, `<span class="r ${upd.info ? 'ok' : ''}" id="updSetR">${updSetLabel()}</span>`, 'id="sUpd"')}
      ${setRow('cover', '清除封面缓存', '更换了歌曲封面后可用', '', 'id="sCover"')}
      ${setRow('', `拾音 电脑版 ${esc(S.info ? S.info.version : '')}`, `本地播放器，只播放电脑里的音乐。在线歌词来自 QQ音乐、酷狗、网易云和 LRCLIB；中文歌名和封面来自 Apple Music 目录。<br>快捷键：空格 播放/暂停 · Ctrl+← / → 上一首/下一首 · Ctrl+↑ / ↓ 音量 · Ctrl+F 搜索<br>数据保存在：${esc(S.info ? S.info.dataDir : '')}`, '', 'id="sAbout"')}
    </div>`;
  body.scrollTop = st;
}
async function setCfg(body) {
  try { S.cfg = await api('/api/config', { method: 'POST', body }); return true; } catch (e) { toast(e.message); return false; }
}
async function onSettingsClick(e) {
  const b = e.target.closest('[data-a]'); if (!b) return;
  e.stopPropagation();
  const a = b.dataset.a;
  if (a === 'scan') { rescan(); const ss = $('#scanStatus'); if (ss) ss.textContent = '扫描中…'; return; }
  if (a === 'addf') return pickFolder('', async (p) => { const ex = (S.cfg.extraDirs || []).concat([p]); if (await setCfg({ extraDirs: ex })) { toast('已添加：' + p); rescan(); } openSettings(); }, openSettings);
  if (a === 'org') { if (S.org && S.org.running) return; if (S.outside) organize(false); else toast('全部歌曲都已在拾音文件夹'); return; }
  if (a === 'autoorg') { await setCfg({ autoOrganize: !S.cfg.autoOrganize }); return renderSettings(); }
  if (a === 'zh') { if (batch.running) stopBatch(); else startBatch(); return renderSettings(); }
  if (a === 'zhundo') return confirm2('撤销自动识别？', '自动改成的中文名会恢复成文件里的原名', '确定', async () => { let n = 0; try { n = (await api('/api/meta/clearAuto', { method: 'POST' })).count; } catch (er) { return toast(er.message); } await reloadKeep(); toast(`已恢复 ${n} 首`); openSettings(); });
  if (a === 'open') return api('/api/reveal?path=' + encodeURIComponent(S.cfg.musicDir), { method: 'POST' });
  if (a === 'change') return pickFolder(S.cfg.musicDir, async (p) => { if (await setCfg({ musicDir: p })) { toast('已更改，正在重新扫描'); rescan(); } openSettings(); }, openSettings);
  if (a === 'rmf') { const i = +b.dataset.i; return confirm2('移除这个文件夹？', '文件不会被删除，只是不再加入音乐库', '移除', async () => { const ex = (S.cfg.extraDirs || []).filter((_, k) => k !== i); if (await setCfg({ extraDirs: ex })) rescan(); openSettings(); }); }
  if (a === 'phone') { closeModal(); return openPhone(); }
  if (a === 'netfix') return showNetRepair();
  if (a === 'theme') return setTheme(b.dataset.v);
  if (a === 't2s') {
    if (!(await setCfg({ t2s: !S.cfg.t2s }))) return;
    S.tracks.forEach(decorate); render(true); updateNowPlaying(); renderQueue(); renderSettings();
    const t = curTrack(); if (t) loadLyrics(t);
    return toast(S.cfg.t2s ? '歌词将显示为简体中文' : '歌词按原文显示');
  }
  if (a === 'desk') { await setDesk({ on: !deskOn() }); return renderSettings(); }
  if (a === 'deskunlock') { await setDesk({ locked: false }); toast('已解锁桌面歌词'); return renderSettings(); }
  if (a === 'deskreset') { await setDesk({ resetPos: true }); return toast('桌面歌词已回到默认位置'); }
  if (a === 'online') { await setCfg({ onlineLyrics: !S.cfg.onlineLyrics }); return renderSettings(); }
  if (a === 'upd') { if (upd.status && ['download', 'verify', 'ready'].includes(upd.status.phase)) showUpdateDialog(); else checkUpdate(false); return; }
  if (a === 'cover') {
    try { await api('/api/covers/clean', { method: 'POST' }); } catch (_) {}
    await savePrefs({ coverEpoch: Date.now() });
    render(true); updateNowPlaying(); renderQueue(); toast('封面缓存已清除');
  }
}

async function pickFolder(start, ok, cancel) {
  const m = modal(`<h3>选择文件夹</h3><div class="body"><div class="fs-quick" id="fsQuick"></div><div class="fs-path" id="fsPath"></div><div class="fs-list" id="fsList"></div><div class="field"><label>或直接输入路径</label><input type="text" id="fsInp"></div></div><div class="foot"><button class="btn ghost" id="fsCancel">取消</button><button class="btn primary" id="fsOk">选择这个文件夹</button></div>`, 'wide');
  m.style.height = 'auto';
  let cur = '';
  const go = async (p) => {
    let d; try { d = await api('/api/fs?path=' + encodeURIComponent(p)); } catch (e) { return toast(e.message); }
    cur = d.path; if (document.activeElement !== $('#fsInp', m)) $('#fsInp', m).value = cur;
    $('#fsPath', m).textContent = cur ? '当前：' + cur : '选择磁盘';
    $('#fsQuick', m).innerHTML = (d.quick || []).map((q) => `<button data-p="${esc(q.path)}">${esc(q.name)}</button>`).join('');
    let h = d.parent !== null && d.parent !== undefined && cur ? `<div class="fs-item" data-p="${esc(d.parent)}">${ICON.back}<span>上一级</span></div>` : '';
    if (d.error) h += `<div class="fs-item" style="color:var(--err)">${esc(d.error)}</div>`;
    h += d.dirs.map((x) => `<div class="fs-item" data-p="${esc(x.path)}">${ICON.folder}<span>${esc(x.name)}</span></div>`).join('');
    if (!d.dirs.length && !d.error) h += '<div class="fs-item" style="color:var(--fg3)">（没有子文件夹）</div>';
    $('#fsList', m).innerHTML = h;
  };
  m.addEventListener('click', (e) => { const it = e.target.closest('[data-p]'); if (it) go(it.dataset.p); });
  $('#fsInp', m).onkeydown = (e) => { if (e.key === 'Enter') go($('#fsInp', m).value.trim()); };
  $('#fsCancel', m).onclick = () => { closeModal(); if (cancel) cancel(); };
  $('#fsOk', m).onclick = () => { const p = $('#fsInp', m).value.trim() || cur; if (!p) return toast('请先选择一个文件夹'); closeModal(); ok(p); };
  go(start || '');
}

// ---------------- 内置更新 ----------------
const upd = { info: null, checking: false, status: null, msg: '', msgCls: '', poll: 0 };
const mb = (n) => (n / 1048576).toFixed(1) + ' MB';
function updSetLabel() {
  const ph = upd.status && upd.status.phase;
  if (ph === 'download') return upd.status.total ? `正在下载 ${Math.floor(upd.status.got * 100 / upd.status.total)}%` : '正在下载…';
  if (ph === 'verify') return '正在校验…';
  if (ph === 'ready') return '已下载，点击安装';
  if (upd.checking) return '检查中…';
  if (upd.info) return `有新版本 ${esc(upd.info.version)}`;
  return '›';
}
function refreshUpd() {
  const r = $('#updSetR'); if (r) { r.innerHTML = updSetLabel(); r.classList.toggle('ok', !!upd.info); }
  const m = $('#updSetMsg'); if (m) { m.textContent = upd.msg ? ' · ' + upd.msg : ''; m.className = upd.msgCls; }
  $('#btnSettings').classList.toggle('dot', !!upd.info);
  if (modalKind() === 'update') drawUpdateDialog();
}
function setUpdMsg(msg, cls) { upd.msg = msg; upd.msgCls = cls || ''; refreshUpd(); }
async function checkUpdate(silent) {
  if (upd.checking) return;
  upd.checking = true; if (!silent) setUpdMsg('检查中…');
  let r;
  try { r = await api('/api/update/check' + (silent ? '?silent=1' : '')); } catch (e) { r = { ok: false, message: e.message }; }
  upd.checking = false;
  const hhmm = new Date().toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' });
  if (r.ok && r.hasUpdate) {
    upd.info = r.info;
    if (!silent) { setUpdMsg(`发现新版本 ${r.info.version}`, 'ok'); showUpdateDialog(); return; }
    refreshUpd();
    // 启动时最多每天提示一次同一个版本
    const last = S.prefs.updPrompt || {};
    if (last.v === r.info.version && Date.now() - (last.at || 0) < 20 * 3600 * 1000) return;
    savePrefs({ updPrompt: { v: r.info.version, at: Date.now() } });
    if (!modalOpen()) showUpdateDialog();
  } else if (r.ok) {
    upd.info = null;
    if (!silent) { setUpdMsg(`已是最新版本（${hhmm} 检查）`, 'ok'); toast('已是最新版本'); } else refreshUpd();
  } else if (!silent) {
    const why = r.message || '请检查网络';
    setUpdMsg('检查失败：' + why, 'bad'); toast('检查更新失败：' + why, 4500);
  }
}
function showUpdateDialog() {
  if (!upd.info) return;
  modal(`<h3 id="updTitle"></h3><div class="body"><div class="upd-ver" id="updVer"></div><div class="upd-notes" id="updNotes"></div><div class="upd-prog hidden" id="updProg"><div class="bar"><i id="updBar"></i></div><div class="lbl" id="updLbl"></div></div></div><div class="foot" id="updBtns"></div>`);
  $('#overlay').dataset.kind = 'update';
  drawUpdateDialog();
}
function drawUpdateDialog() {
  const i = upd.info; if (!i || !$('#updTitle')) return;
  const s = upd.status || { phase: 'idle' };
  $('#updTitle').textContent = `发现新版本 ${i.version}`;
  $('#updVer').textContent = `当前版本 ${S.info ? S.info.version : ''} · 安装包 ${mb(i.size)}`;
  const notes = $('#updNotes'); if (notes.textContent !== (i.notes || '修复问题，提升体验')) notes.textContent = i.notes || '修复问题，提升体验';
  const prog = $('#updProg'), bar = $('#updBar'), lbl = $('#updLbl');
  const busy = s.phase === 'download' || s.phase === 'verify';
  prog.classList.toggle('hidden', s.phase === 'idle' || s.phase === 'cancelled');
  lbl.classList.toggle('bad', s.phase === 'error');
  const pct = s.total ? Math.min(100, Math.floor(s.got * 100 / s.total)) : 0;
  bar.style.width = (s.phase === 'ready' || s.phase === 'installing' || s.phase === 'verify' ? 100 : pct) + '%';
  lbl.textContent = s.phase === 'download' ? `正在下载 ${pct}% · ${mb(s.got || 0)} / ${mb(s.total || i.size)}${s.try ? `（第 ${s.try + 1} 次尝试）` : ''}`
    : s.phase === 'verify' ? '正在校验安装包…'
      : s.phase === 'ready' ? '校验通过，可以安装了'
        : s.phase === 'installing' ? '正在安装，拾音会自动关闭并重新打开…'
          : s.phase === 'error' ? s.message || '下载失败' : '';
  const btns = $('#updBtns');
  const want = busy ? 'busy' : s.phase === 'ready' ? 'ready' : s.phase === 'installing' ? 'inst' : s.phase === 'error' ? 'err' : 'idle';
  if (btns.dataset.st === want) return;
  btns.dataset.st = want;
  btns.innerHTML = want === 'busy' ? '<button class="btn ghost" data-u="bg">后台下载</button><button class="btn ghost" data-u="cancel">取消下载</button>'
    : want === 'ready' ? '<button class="btn ghost" data-u="later">稍后</button><button class="btn primary" data-u="install">立即安装</button>'
      : want === 'inst' ? ''
        : want === 'err' ? '<button class="btn ghost" data-u="later">稍后</button><button class="btn primary" data-u="go">重试</button>'
          : '<button class="btn ghost" data-u="later">稍后</button><button class="btn primary" data-u="go">立即更新</button>';
  btns.onclick = async (e) => {
    const b = e.target.closest('[data-u]'); if (!b) return;
    const a = b.dataset.u;
    if (a === 'later' || a === 'bg') return closeModal();
    if (a === 'cancel') { try { upd.status = await api('/api/update/cancel', { method: 'POST' }); } catch (_) {} return; }
    if (a === 'go') { try { upd.status = await api('/api/update/start', { method: 'POST' }); } catch (er) { return toast(er.message); } drawUpdateDialog(); pollUpdate(); }
    if (a === 'install') installUpdate();
  };
}
function pollUpdate() {
  clearInterval(upd.poll);
  upd.poll = setInterval(async () => {
    let s; try { s = await api('/api/update/status'); } catch (_) { return; }
    const prev = upd.status && upd.status.phase;
    upd.status = s; refreshUpd();
    if (s.phase === 'download' || s.phase === 'verify') return;
    clearInterval(upd.poll);
    if (s.phase === 'ready') {
      if (modalKind() === 'update') installUpdate(); // 下载完直接安装（与手机版一致）
      else if (prev !== 'ready') { toast('新版本已下载好'); if (!modalOpen()) showUpdateDialog(); }
    } else if (s.phase === 'cancelled') { toast('已取消下载'); }
    else if (s.phase === 'error' && modalKind() !== 'update') { toast(s.message || '下载失败', 4500); }
  }, 400);
}
async function installUpdate() {
  await saveState();
  try { await api('/api/update/install', { method: 'POST' }); } catch (e) { upd.status = { phase: 'error', message: e.message }; return refreshUpd(); }
  upd.status = { phase: 'installing' };
  if (modalKind() !== 'update') showUpdateDialog();
  $('#overlay').dataset.sticky = '1';
  refreshUpd();
}

// ---------------- 从手机导入 ----------------
let phoneTimer = null;
const PH = { lib: null, sel: new Set(), filter: 'missing', host: '', job: null };

async function openPhone() {
  if (PH.job && PH.job.running) return showPhoneLibrary();
  if (PH.lib) { // still connected: refresh which songs the PC has
    try { const d = await api('/api/phone/library'); PH.lib = d.library; return showPhoneLibrary(); } catch (_) { PH.lib = null; }
  }
  const last = S.cfg.lastPhone || '';
  const m = modal(`<h3>从手机导入<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body"><div class="ph-connect">
    <div class="ph-steps">1. 手机打开拾音 → <b>设置 → 传到电脑</b>，打开服务<br>2. 手机和电脑连同一个 Wi-Fi<br>3. 下面选中你的手机（或手动输入手机上显示的 <b>IP:端口</b>），再输入手机上的 <b>6 位配对码</b></div>
    <div style="display:flex;align-items:center;gap:10px"><b style="flex:1">附近的手机</b><button class="btn ghost" id="phFind">重新搜索</button></div>
    <div class="ph-found" id="phFound"></div>
    <div class="field"><label>手机地址（IP:端口）</label><input type="text" id="phHost" placeholder="例如 192.168.1.23:18520" value="${esc(last)}"></div>
    <div class="field"><label>配对码</label><input type="text" class="code-inp" id="phCode" maxlength="6" inputmode="numeric" placeholder="······"></div>
    <div class="err" id="phErr"></div>
    <button class="btn primary" id="phGo" style="width:100%;justify-content:center;height:44px">连接手机</button>
    <div class="ph-rev" id="phRev">
      <b>找不到手机、或者连不上？改用手机连电脑</b>
      <div class="ph-rev-txt">在手机拾音「设置 → 传到电脑」页面下方点 <b>「找不到？改用手机连电脑」</b>，搜索电脑（或输入下面的电脑 IP），再输入这个配对码：</div>
      <div class="ph-rev-box"><div><div class="l">电脑 IP</div><div class="v" id="revIps">…</div></div><div><div class="l">配对码</div><div class="v code" id="revCode">······</div></div></div>
      <div class="ph-rev-st" id="revSt">正在开启…</div>
      <div class="ph-rev-fw">如果 Windows 弹出防火墙提示，请勾选“专用网络”（用电脑热点时也勾选“公用网络”）并允许。</div>
    </div>
    ${phoneGuideHtml()}
    <div class="ph-diag"><button class="btn ghost" id="phDiag">网络诊断</button><span class="ph-diag-h">用上面填的手机 IP 检查网络，生成可复制的报告</span><button class="ph-link" id="phRepair">电脑断网/代理失效？</button></div>
    <div id="phDiagOut"></div>
  </div></div>`, 'wide');
  startReverse(m);
  const find = async () => {
    const box = $('#phFound', m);
    box.innerHTML = '<div style="color:var(--fg3);padding:8px 0;display:flex;gap:10px;align-items:center"><div class="spin" style="width:18px;height:18px;border-width:2px"></div>正在搜索局域网里的手机…</div>';
    let d; try { d = await api('/api/phone/discover', { method: 'POST' }); } catch (e) { box.innerHTML = `<div class="err">${esc(e.message)}</div>`; return; }
    if (!document.body.contains(box)) return;
    if (!d.phones || !d.phones.length) { box.innerHTML = '<div style="color:var(--fg3);padding:8px 0">没有找到手机。请确认手机已打开「传到电脑」并连着同一个 Wi-Fi，或在下面手动输入地址。</div>'; return; }
    box.innerHTML = d.phones.map((p) => `<div class="ph-dev" data-host="${esc(p.ip + ':' + p.port)}">${ICON.list}<div class="n">${esc(p.name || '拾音手机')}</div><div class="s">${esc(p.ip)}:${p.port}${p.count ? ' · ' + p.count + ' 首' : ''}</div></div>`).join('');
    if (d.phones.length === 1) selDev(box.firstElementChild);
  };
  const selDev = (el) => { m.querySelectorAll('.ph-dev').forEach((x) => x.classList.toggle('on', x === el)); $('#phHost', m).value = el.dataset.host; $('#phCode', m).focus(); };
  $('#phFound', m).onclick = (e) => { const d = e.target.closest('.ph-dev'); if (d) selDev(d); };
  $('#phFind', m).onclick = find;
  const code = $('#phCode', m);
  code.oninput = () => { code.value = code.value.replace(/\D/g, '').slice(0, 6); if (code.value.length === 6) connect(); };
  code.onkeydown = (e) => { if (e.key === 'Enter') connect(); };
  const connect = async () => {
    const host = $('#phHost', m).value.trim(), c = code.value.trim();
    const err = $('#phErr', m);
    if (!host) { err.textContent = '请先选中手机，或输入手机上显示的 IP:端口'; return; }
    if (c.length !== 6) { err.textContent = '请输入手机上显示的 6 位配对码'; return code.focus(); }
    err.textContent = '';
    const body = m.querySelector('.body');
    const keep = body.innerHTML;
    body.innerHTML = '<div class="loading"><div class="spin"></div>正在读取手机曲库…<span style="font-size:12px;color:var(--fg3)">歌多的话第一次可能要半分钟</span></div>';
    $('#overlay').dataset.sticky = '1';
    try {
      const d = await api('/api/phone/connect', { method: 'POST', body: { host, code: c } });
      delete $('#overlay').dataset.sticky;
      PH.lib = d.library; PH.host = host; S.cfg.lastPhone = host;
      PH.sel = new Set(PH.lib.tracks.filter((t) => !t.present).map((t) => t.key));
      PH.filter = PH.lib.tracks.some((t) => !t.present) ? 'missing' : 'all';
      showPhoneLibrary();
    } catch (e) {
      delete $('#overlay').dataset.sticky;
      if (!modalOpen()) return;
      await openPhone();
      $('#phHost').value = host; $('#phErr').textContent = e.message;
      const ci = $('#phCode'); ci.value = ''; ci.focus();
      // 0.2.3: 连不上（不是配对码错）→ 提示 AP 隔离并自动诊断
      if (/连不上手机/.test(e.message)) { const mm = $('#phGuide').closest('.modal'); showIsolation(mm, false); runPhoneDiag(mm, true); }
    }
  };
  $('#phGo', m).onclick = connect;
  phoneGuideWire(m);
  $('#phRepair', m).onclick = () => showNetRepair();
  $('#phDiag', m).onclick = () => runPhoneDiag(m, false);
  if (last) code.focus();
  find();
}

// 0.2.3「连不上？按这个顺序试」：路由器开了 AP 隔离时的推荐顺序（电脑热点放最后并标为不推荐）
function phoneGuideHtml() {
  const dir = esc(S.cfg.musicDir || '音乐\\拾音');
  return `<div class="ph-guide" id="phGuide">
    <div class="ph-iso hidden" id="phIso"></div>
    <div class="ph-g-ttl">同一个 Wi-Fi 还是连不上？多半是<b>路由器开了 AP 隔离</b>，按这个顺序试：</div>
    <ol class="ph-g-list">
      <li><div class="ph-g-h">手机开热点，电脑连手机的热点 <span class="ph-tag rec">推荐</span></div>
        <div class="ph-g-b">手机打开 <b>设置 → 个人热点</b>；电脑点右下角 Wi-Fi 图标，连上手机的热点；手机拾音「设置 → 传到电脑」会显示热点 IP，这里点「重新搜索」或输入地址后连接。<br>
        传歌只在手机和电脑之间进行，<b>不耗手机流量</b>。电脑连着手机热点时自己上网会用手机流量，保险起见可以先<b>关掉手机的移动数据</b>（传歌不受影响）。</div></li>
      <li><div class="ph-g-h">用数据线拷贝</div>
        <div class="ph-g-b">手机拾音「设置」里打开 <b>「导出歌单和改名到拾音文件夹」</b>；数据线连电脑，手机上选「传输文件」；把手机 <b>内部存储/Music/拾音</b> 整个文件夹复制到电脑的拾音文件夹（${dir}），再点「重新扫描」。歌单、中文歌名和封面会自动合并。
        <button class="ph-link" id="phOpenDir">打开拾音文件夹</button></div></li>
      <li><div class="ph-g-h">关闭路由器的 AP 隔离</div>
        <div class="ph-g-b">浏览器打开路由器管理页（<b id="phGw">例如 192.168.0.1</b>，光猫背面的标签上有地址和密码）→ <b>无线设置 / 高级设置</b> → 关闭 <b>“AP 隔离”/“客户端隔离”/“无线隔离”</b> → 保存。手机和电脑要连同一个 Wi-Fi 名称、同一个频段（例如都连 5G），不要连访客网络。</div></li>
      <li class="ph-g-bad"><div class="ph-g-h">电脑开热点 <span class="ph-tag bad">不推荐</span></div>
        <div class="ph-g-b"><span class="ph-warn">部分笔记本的无线网卡驱动（例如 Intel AX201）在开热点传大量文件时可能让<b>电脑死机</b>，死机后还可能断网。</span>上面三种都不行时再用：拾音会限速、一次传一首，传完 1 分钟后自动关闭热点，退出拾音时也会关闭。用之前请先关掉代理软件的 TUN 模式。</div>
        <div class="ph-hs" id="phHs">
          <div class="ph-hs-row"><b style="flex:1">电脑热点</b><button class="btn ghost" id="hsBtn" disabled>开启电脑热点</button></div>
          <div class="ph-hs-info" id="hsInfo">正在读取电脑热点状态…</div>
        </div></li>
    </ol>
  </div>`;
}
function phoneGuideWire(m) {
  const od = $('#phOpenDir', m); if (od) od.onclick = () => api('/api/reveal?path=' + encodeURIComponent(S.cfg.musicDir), { method: 'POST' }).catch(() => {});
  // 0.2.2 一键电脑热点（Windows 移动热点）；0.2.3 先检查代理 TUN/VPN，拾音开的热点会自动关闭
  let hsOn = false, hsTimer = null;
  const hsShow = (st, busy) => {
    const btn = $('#hsBtn', m), info = $('#hsInfo', m); if (!btn || !info) return;
    hsOn = !!st.on; btn.disabled = !!busy;
    btn.textContent = busy ? (hsOn ? '正在关闭…' : '正在开启…') : (hsOn ? '关闭电脑热点' : '开启电脑热点');
    let h = '';
    if (hsOn) h = `<div class="ph-hs-ok">热点已开启${st.clients ? ` · 已连接 ${st.clients} 台设备` : ''}</div>
      <div class="ph-hs-cred"><div><span class="l">热点名称</span><b class="sel">${esc(st.ssid)}</b></div><div><span class="l">密码</span><b class="sel">${esc(st.pass)}</b></div></div>
      <div class="ph-hs-h">手机连上这个热点，然后在手机拾音「设置 → 传到电脑」里重新搜索；上面的电脑 IP 会优先显示 192.168.137.1。${st.owned ? `<br>经电脑热点传输会限速（约 6 MB/s、一次一首）；传完 ${Math.round((st.autoOff || 60) / 60)} 分钟后自动关闭热点，退出拾音时也会关闭。` : ''}</div>`;
    else if (!busy) h = '<div class="ph-hs-h">开启后手机连电脑的热点（不耗手机流量）。</div>';
    if (st.error) h += `<div class="err">${esc(st.error)}</div><div class="ph-hs-h">${esc(st.manual || '也可以手动开启：设置 → 网络和 Internet → 移动热点。')}</div>`;
    info.innerHTML = h;
  };
  const hsLoad = async () => { try { hsShow(await api('/api/phone/hotspot')); } catch (e) { hsShow({ error: e.message }); } };
  const hsSet = async (want, force) => {
    hsShow({ on: hsOn }, true);
    let st; try { st = await api('/api/phone/hotspot?on=' + (want ? 1 : 0) + (force ? '&force=1' : ''), { method: 'POST' }); } catch (e) { return hsShow({ on: hsOn, error: e.message }); }
    if (st.needConfirm) { hsShow(st); return tunWarnDialog(st.tunWarn || [], () => hsSet(true, true)); }
    hsShow(st);
  };
  const hb = $('#hsBtn', m); if (hb) hb.onclick = () => hsSet(!hsOn, false);
  hsLoad();
  hsTimer = setInterval(() => { if (!document.body.contains(m) || !$('#phHs', m)) { clearInterval(hsTimer); return; } if (hsOn) hsLoad(); }, 8000);
  // 路由器地址（电脑的默认网关）
  api('/api/phone/reverse/status').then((st) => { const g = (st.ips || []).find((x) => x.gwIp); const el = $('#phGw', m); if (g && el) el.textContent = g.gwIp; }).catch(() => {});
}
// 开热点前发现代理的 TUN / VPN 网卡：提醒先关掉（可以继续）
function tunWarnDialog(list, go) {
  const m = document.createElement('div');
  m.className = 'confirm-layer';
  m.innerHTML = `<div class="modal"><h3>先关掉代理的 TUN 模式</h3><div class="body"><div class="ph-warn-box">
    电脑上正在运行代理/VPN 的虚拟网卡：<ul>${list.map((x) => `<li>${esc(x)}</li>`).join('')}</ul>
    它和 Windows 移动热点同时运行时，容易让无线网卡驱动出错，甚至电脑死机、重启后代理全部超时。<br>
    请先在代理软件（例如 Clash）里<b>关闭 TUN 模式</b>或退出它，再开启电脑热点。<br>
    更推荐：<b>手机开热点，电脑连手机的热点</b>。</div></div>
    <div class="foot"><button class="btn ghost" data-a="go">仍然开启</button><button class="btn primary" data-a="no">好的，先去关闭</button></div></div>`;
  document.body.appendChild(m);
  m.onclick = (e) => { const b = e.target.closest('[data-a]'); if (!b) return; m.remove(); if (b.dataset.a === 'go') go(); };
}
// 「网络修复」：电脑开热点死机后，代理/网络一直超时
function showNetRepair() {
  const m = document.createElement('div');
  m.className = 'confirm-layer';
  m.innerHTML = `<div class="modal"><h3>网络修复<button class="icon-btn x" data-a="x">${ICON.x}</button></h3><div class="body">${netRepairHtml()}</div>
    <div class="foot"><button class="btn primary" data-a="x">知道了</button></div></div>`;
  document.body.appendChild(m);
  m.onclick = (e) => { if (e.target === m || e.target.closest('[data-a="x"]')) m.remove(); };
}
function netRepairHtml() {
  return `<div class="net-repair">如果电脑开过热点后死机，重启后代理或网络一直超时，可以这样修复：<ol>
    <li>打开 <b>控制面板 → 网络和 Internet → 网络连接</b>，右键 <b>“本地连接*”</b>（Microsoft Wi-Fi Direct Virtual Adapter）→ <b>禁用</b>。</li>
    <li>右键开始菜单 → <b>终端（管理员）</b>，依次运行：<code class="sel">netsh winsock reset</code><code class="sel">netsh int ip reset</code>出现一行“拒绝访问”是正常的，不影响修复。</li>
    <li><b>重启电脑</b>。</li></ol></div>`;
}
// 网络诊断；auto = 连接失败后自动诊断（只在发现 AP 隔离时提示）
async function runPhoneDiag(m, auto) {
  const out = $('#phDiagOut', m), btn = $('#phDiag', m); if (!out || !btn) return;
  const ip = ($('#phHost', m).value || '').trim().replace('：', ':').split(':')[0];
  if (!ip) { if (!auto) out.innerHTML = '<div class="err">请先在「手机地址」里填手机上显示的 IP（例如 192.168.0.104）</div>'; return; }
  btn.disabled = true; out.innerHTML = `<div class="ph-diag-h">${auto ? '连不上手机，正在自动检查网络' : '正在诊断'}（约 10–20 秒）…</div>`;
  try {
    const d = await api('/api/phone/diag?ip=' + encodeURIComponent(ip));
    if (!document.body.contains(out)) return;
    out.innerHTML = `<textarea class="ph-diag-rep" readonly rows="12">${esc(d.report)}</textarea><button class="btn ghost" id="phDiagCopy">复制报告</button>`;
    $('#phDiagCopy', m).onclick = () => { const t = $('.ph-diag-rep', m); t.select(); try { navigator.clipboard.writeText(t.value).then(() => toast('已复制'), () => { document.execCommand('copy'); toast('已复制'); }); } catch (_) { document.execCommand('copy'); toast('已复制'); } };
    if (d.isolation) showIsolation(m, true);
  } catch (e) { out.innerHTML = `<div class="err">${esc(e.message)}</div>`; }
  btn.disabled = false;
}
function showIsolation(m, sure) {
  const el = $('#phIso', m); if (!el) return;
  el.innerHTML = sure
    ? '<b>诊断结果：路由器开了 AP 隔离</b>（同一个 Wi-Fi 下的设备互相访问不了，手机和电脑都没问题）。请按下面的顺序换一种方式：'
    : '<b>连不上手机，可能是路由器开了 AP 隔离。</b>点下面的「网络诊断」确认，或直接按下面的顺序试：';
  el.classList.remove('hidden');
  $('#phGuide', m).classList.add('alert');
  el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}

// 「手机连电脑」：对话框打开期间电脑监听 18522，手机主动连过来；对话框每 1.5 秒查询一次状态（也是心跳）。
let revTimer = null, revConnecting = false;
async function startReverse(m) {
  if (revTimer) clearInterval(revTimer);
  const show = (st) => {
    const ips = $('#revIps', m), code = $('#revCode', m), el = $('#revSt', m);
    if (!el) return;
    if (st.error) { el.textContent = st.error; el.className = 'ph-rev-st err'; return; }
    const port = st.port && st.port !== 18522 ? ':' + st.port : '';
    // 0.2.2: 真实网络（有默认网关的 Wi-Fi/有线）排第一并加粗；Windows 移动热点（192.168.137.x）排最后并注明
    const ipList = st.ips || [];
    const multi = ipList.length > 1;
    ips.innerHTML = ipList.length ? ipList.map((x, i) => {
      const lab = x.label ? ` <span class="ph-ip-k">${esc(x.label)}${x.kind === 'hotspot' ? '（仅手机连着电脑热点时用）' : ''}</span>` : '';
      const main = i === 0 && multi && x.kind !== 'hotspot' ? ' <span class="ph-ip-k">优先试这个</span>' : '';
      return `<span class="${i ? 'ph-ip-alt' : 'ph-ip-main'}">${esc(x.ip + port)}</span>${lab}${main}`;
    }).join('<br>') : '没有找到局域网 IP（电脑连 Wi-Fi 或网线了吗？）';
    code.textContent = String(st.code || '').replace(/(\d{3})(\d{3})/, '$1 $2');
    if (st.phone) { el.textContent = `手机「${st.phone.name}」已连上，正在读取曲库…`; el.className = 'ph-rev-st on'; }
    else { el.textContent = st.listening ? '等待手机连接…' : '未开启'; el.className = 'ph-rev-st'; }
  };
  let st; try { st = await api('/api/phone/reverse/start', { method: 'POST' }); } catch (e) { st = { error: e.message }; }
  show(st);
  revTimer = setInterval(async () => {
    if (!document.querySelector('.ph-connect, #phList') && !(PH.job && PH.job.running)) { clearInterval(revTimer); revTimer = null; return; }
    let s2; try { s2 = await api('/api/phone/reverse/status'); } catch (_) { return; }
    if (!s2.listening && document.querySelector('.ph-connect')) { try { s2 = await api('/api/phone/reverse/start', { method: 'POST' }); } catch (_) {} }
    if (!document.body.contains(m)) return;
    show(s2);
    if (s2.phone && !revConnecting && $('#phRev', m)) connectReverse(m);
  }, 1500);
}
async function connectReverse(m) {
  revConnecting = true;
  const body = m.querySelector('.body');
  body.innerHTML = '<div class="loading"><div class="spin"></div>手机已连上，正在读取手机曲库…<span style="font-size:12px;color:var(--fg3)">歌多的话第一次可能要半分钟</span></div>';
  $('#overlay').dataset.sticky = '1';
  try {
    const d = await api('/api/phone/reverse/connect', { method: 'POST' });
    delete $('#overlay').dataset.sticky;
    PH.lib = d.library; PH.host = 'reverse';
    PH.sel = new Set(PH.lib.tracks.filter((t) => !t.present).map((t) => t.key));
    PH.filter = PH.lib.tracks.some((t) => !t.present) ? 'missing' : 'all';
    revConnecting = false;
    showPhoneLibrary();
    keepReverseAlive();
  } catch (e) {
    delete $('#overlay').dataset.sticky;
    revConnecting = false;
    if (!modalOpen()) return;
    openPhone();
    const el = $('#revSt'); if (el) { el.textContent = e.message; el.className = 'ph-rev-st err'; }
  }
}
// while the phone's library (reverse) is shown or importing, keep the listener's heartbeat going
function keepReverseAlive() {
  if (revTimer) clearInterval(revTimer);
  revTimer = setInterval(async () => {
    if (PH.host !== 'reverse' || (!document.querySelector('#phList') && !(PH.job && PH.job.running))) { clearInterval(revTimer); revTimer = null; return; }
    try { await api('/api/phone/reverse/status'); } catch (_) {}
  }, 5000);
}

function showPhoneLibrary() {
  const lib = PH.lib; if (!lib) return openPhone();
  const total = lib.tracks.length, have = lib.tracks.filter((t) => t.present).length;
  const m = modal(`<h3>${esc(lib.name || '手机')} 的歌曲<button class="icon-btn x" data-close>${ICON.x}</button></h3><div class="body" style="display:flex;flex-direction:column">
    <div class="ph-head"><div class="info"><b>${total}</b> 首 · 电脑上已有 <b>${have}</b> 首 · 未导入 <b>${total - have}</b> 首${lib.playlists && lib.playlists.length ? ` · ${lib.playlists.length} 个歌单` : ''}</div>
      <div class="seg" id="phSeg"><button data-f="missing">未导入</button><button data-f="all">全部</button></div>
      <label class="chk"><input type="checkbox" id="phAll">全选</label>
      <button class="btn ghost" id="phImpSel">导入所选</button>
      <button class="btn primary" id="phImpAll">全部导入</button></div>
    <div class="ph-prog hidden" id="phProg"><div class="txt"><span id="phProgTxt"></span><span><span id="phProgPct"></span> <button class="btn ghost" id="phCancel" style="height:28px;margin-left:8px">取消</button></span></div><div class="big-bar"><div id="phBar" style="width:0"></div></div></div>
    <div class="ph-list" id="phList" style="flex:1;overflow-y:auto"></div></div>
    <div class="foot" style="padding-top:6px"><span style="flex:1;color:var(--fg3);font-size:12px;align-self:center">保存到：${esc(S.cfg.musicDir || '')} · 中文歌名、封面、歌词和歌单会一起同步，不修改音乐文件</span><button class="btn ghost" id="phBack">换一台手机</button></div>`, 'wide');
  const drawList = () => {
    const job = PH.job;
    const states = new Map(job && job.items ? job.items.map((it) => [it.key, it]) : []);
    const list = lib.tracks.filter((t) => PH.filter === 'all' || !t.present || states.has(t.key));
    m.querySelectorAll('#phSeg button').forEach((b) => b.classList.toggle('on', b.dataset.f === PH.filter));
    $('#phAll', m).checked = list.length > 0 && list.every((t) => PH.sel.has(t.key));
    $('#phList', m).innerHTML = list.length ? list.map((t) => {
      const it = states.get(t.key);
      let st = t.present ? '<span class="tag have">电脑已有</span>' : '';
      if (it) {
        if (it.state === 'wait') st = '<span class="tag">等待中</span>';
        else if (it.state === 'down') st = `<span class="tag down">${it.size ? Math.floor(it.got / it.size * 100) : 0}%${it.try ? ' 重试' + it.try : ''}</span><div class="mini-bar"><div style="width:${it.size ? it.got / it.size * 100 : 0}%"></div></div>`;
        else if (it.state === 'done') st = '<span class="tag ok">已导入</span>';
        else if (it.state === 'skip') st = '<span class="tag have">已有·已同步</span>';
        else if (it.state === 'fail') st = `<span class="tag fail" title="${esc(it.error)}">失败</span>`;
      }
      const cov = t.hasCover ? `<div class="cover" style="background-image:url('/api/phone/cover?key=${encodeURIComponent(t.key)}&v=${t.coverVer || 0}')"></div>` : `<div class="cover">${ICON.note}</div>`;
      const extras = [t.ext ? t.ext.toUpperCase() : '', t.hasLrc ? (t.lrcSynced ? '滚动歌词' : '歌词') : ''].filter(Boolean).join(' · ');
      return `<div class="ph-row" data-key="${esc(t.key)}"><input type="checkbox" ${PH.sel.has(t.key) ? 'checked' : ''}>${cov}<div style="min-width:0"><div class="t">${esc(t2s(t.title))}</div><div class="a">${esc(t2s(t.artist))} · ${esc(extras)}${it && it.state === 'fail' ? ' · ' + esc(it.error) : ''}</div></div><div class="sz">${fmtSize(t.size)}</div><div class="st">${st}</div></div>`;
    }).join('') : `<div class="empty">${PH.filter === 'missing' ? '手机上的歌电脑上都有了 🎉<br>点「全部」可以查看全部歌曲，「全部导入」会同步中文歌名、封面和歌单。' : '手机上没有歌曲'}</div>`;
  };
  const drawProg = () => {
    const j = PH.job; const box = $('#phProg', m); if (!box) return;
    if (!j || !j.total) { box.classList.add('hidden'); return; }
    box.classList.remove('hidden');
    const fin = j.done + j.skipped + j.failed;
    const pct = j.totalBytes ? Math.min(100, j.bytes / j.totalBytes * 100) : (fin / j.total * 100);
    $('#phBar', m).style.width = pct + '%';
    $('#phProgTxt', m).textContent = j.running ? `正在导入 ${fin}/${j.total} 首 · ${fmtSize(j.bytes)} / ${fmtSize(j.totalBytes)}${j.slow ? ' · 经电脑热点传输，已限速约 6 MB/s' : ''}` : j.message;
    $('#phProgPct', m).textContent = j.running ? Math.floor(pct) + '%' : '';
    $('#phCancel', m).classList.toggle('hidden', !j.running);
    ['#phImpSel', '#phImpAll', '#phBack'].forEach((s) => { $(s, m).disabled = !!j.running; });
  };
  m.querySelector('#phSeg').onclick = (e) => { const b = e.target.closest('button'); if (b) { PH.filter = b.dataset.f; drawList(); } };
  $('#phAll', m).onchange = (e) => {
    const list = lib.tracks.filter((t) => PH.filter === 'all' || !t.present);
    list.forEach((t) => e.target.checked ? PH.sel.add(t.key) : PH.sel.delete(t.key)); drawList();
  };
  $('#phList', m).onclick = (e) => {
    const row = e.target.closest('.ph-row'); if (!row) return;
    const k = row.dataset.key;
    if (PH.sel.has(k)) PH.sel.delete(k); else PH.sel.add(k);
    const cb = row.querySelector('input'); cb.checked = PH.sel.has(k);
  };
  const start = async (keys) => {
    if (!keys.length) return toast('请先勾选要导入的歌曲');
    try { PH.job = await api('/api/phone/import', { method: 'POST', body: { keys } }); } catch (e) { return toast(e.message); }
    drawProg(); drawList(); poll();
  };
  $('#phImpSel', m).onclick = () => start([...PH.sel]);
  $('#phImpAll', m).onclick = () => start(lib.tracks.map((t) => t.key));
  $('#phCancel', m).onclick = async () => { try { PH.job = await api('/api/phone/cancel', { method: 'POST' }); } catch (_) {} };
  $('#phBack', m).onclick = async () => {
    if (PH.host === 'reverse') { try { await api('/api/phone/reverse/stop', { method: 'POST' }); } catch (_) {} }
    PH.lib = null; PH.job = null; PH.host = ''; openPhone();
  };
  const poll = () => {
    if (phoneTimer) clearInterval(phoneTimer);
    phoneTimer = setInterval(async () => {
      let j; try { j = await api('/api/phone/import'); } catch (_) { return; }
      PH.job = j;
      if (!$('#phList')) { clearInterval(phoneTimer); phoneTimer = null; return; }
      const sc = $('#phList').scrollTop; drawProg(); drawList(); $('#phList').scrollTop = sc;
      if (!j.running) {
        clearInterval(phoneTimer); phoneTimer = null;
        try { const d = await api('/api/phone/library'); PH.lib = d.library; } catch (_) {}
        await loadLibrary();
        toast(j.message, 4000);
        if ($('#phList')) showPhoneLibrary();
      }
    }, 500);
  };
  drawList(); drawProg();
  if (PH.job && PH.job.running) poll();
}

// background import keeps going when the dialog is closed
setInterval(async () => {
  if (!PH.job || !PH.job.running || phoneTimer) return;
  let j; try { j = await api('/api/phone/import'); } catch (_) { return; }
  PH.job = j;
  const b = $('#btnPhone');
  if (j.running) b.lastChild.textContent = `导入中 ${j.done + j.skipped + j.failed}/${j.total}`;
  else { b.lastChild.textContent = '从手机导入'; try { const d = await api('/api/phone/library'); PH.lib = d.library; } catch (_) {} loadLibrary(); toast(j.message, 4000); }
}, 1500);

// ---------------- 桌面歌词 (0.2.4) ----------------
// The desk window (web/desk.html) gets the same parsed lyrics (offset, 歌词转简体) and the play position from here.
const DESK = { v: -1, prefs: {}, cmdSeen: null, pushed: 0, polling: false, hotkey: '' };
const deskOn = () => !!(DESK.prefs && DESK.prefs.on);
// 0.3.0: now-playing for 「驿」 (GET /api/np, np.go) — pushed on every player change, whether 桌面歌词 is on or not.
let npPushed = 0;
function npPush() {
  const t = curTrack(); npPushed = Date.now();
  const dur = isFinite(audio.duration) && audio.duration > 0 ? audio.duration : (t && t.duration ? t.duration / 1000 : 0);
  const body = { title: t ? t.dTitle : '', artist: t ? t.dArtist : '', album: t ? (t.dAlbum || '') : '',
    playing: !!(t && audio.getAttribute('src') && !audio.paused), pos: audio.currentTime || 0, dur, rate: audio.playbackRate || 1 };
  api('/api/np/now', { method: 'POST', body }).catch(() => {});
}
function deskPush(full) {
  npPush();
  if (!deskOn()) return;
  const t = curTrack();
  const body = { id: t ? t.id : null, title: t ? t.dTitle : '', artist: t ? t.dArtist : '', playing: !!(t && audio.getAttribute('src') && !audio.paused),
    pos: audio.currentTime || 0, at: Date.now(), rate: audio.playbackRate || 1, offset: t ? lyrOffset() : 0 };
  if (full) body.lines = (t && S.lyr && S.lyrTrack === t.id) ? S.lyr.map((l) => ({ t: l.t, text: l.text })) : null;
  DESK.pushed = Date.now();
  api('/api/desklyrics/now', { method: 'POST', body }).catch(() => {});
}
function drawDesk() {
  const b = $('#btnDesk'); const on = deskOn(), locked = on && DESK.prefs.locked;
  b.classList.toggle('on', on); b.classList.toggle('locked', !!locked);
  b.title = locked ? '桌面歌词已锁定（点击解锁）' : on ? '关闭桌面歌词' : '打开桌面歌词';
}
async function setDesk(p) {
  try { DESK.prefs = await api('/api/desklyrics/prefs', { method: 'POST', body: p }); } catch (e) { toast(e.message); }
  drawDesk(); if (p.on) deskPush(true);
}
$('#btnDesk').onclick = async () => {
  if (!deskOn()) { await setDesk({ on: true }); return toast('已打开桌面歌词'); }
  if (DESK.prefs.locked) { await setDesk({ locked: false }); return toast('已解锁桌面歌词'); }
  await setDesk({ on: false }); toast('已关闭桌面歌词');
};
function runDeskCmd(c) {
  if (c === 'prev') prev(); else if (c === 'next') next(); else if (c === 'toggle') togglePlay();
  else if (c === 'play') { if (!curTrack() || !audio.getAttribute('src') || audio.paused) togglePlay(); } // 驿 /api/np/ctl
  else if (c === 'pause') audio.pause();
}
async function deskPoll() {
  if (DESK.polling) return; DESK.polling = true;
  for (;;) {
    let d;
    try { d = await api('/api/desklyrics?since=' + DESK.v + '&wait=25'); } catch (_) { await new Promise((r) => setTimeout(r, 3000)); continue; }
    const wasOn = deskOn(), wasLocked = !!DESK.prefs.locked, hk0 = DESK.hotkey;
    DESK.v = d.v; DESK.prefs = d.prefs || {}; DESK.hotkey = d.hotkey || '';
    if (DESK.cmdSeen === null) DESK.cmdSeen = d.cmdN; // commands from before this page loaded are stale
    for (const c of d.cmds || []) if (c.n > DESK.cmdSeen) { DESK.cmdSeen = c.n; runDeskCmd(c.cmd); }
    drawDesk();
    if (deskOn() && !wasOn) deskPush(true);
    if ((wasOn !== deskOn() || wasLocked !== !!DESK.prefs.locked || hk0 !== DESK.hotkey) && $('#setBody')) renderSettings();
  }
}

// ---------------- boot ----------------
(async function boot() {
  try {
    S.info = await api('/api/info');
    S.cfg = S.info.config;
    applyTheme(S.cfg.theme);
    try { S.prefs = await api('/api/prefs') || {}; } catch (_) { S.prefs = {}; }
    await loadLibrary();
    await restoreState();
    deskPoll(); npPush();
  } catch (e) {
    $('#main').innerHTML = `<div class="empty"><h3>启动失败</h3>${esc(e.message)}</div>`;
    return;
  }
  // 启动时静默检查更新（每天最多联网一次）；如果上次已在后台下载好，直接提示安装
  try { const s = await api('/api/update/status'); upd.status = s; if (s.phase === 'download' || s.phase === 'verify') pollUpdate(); } catch (_) {}
  setTimeout(() => checkUpdate(true), 2500);
})();
