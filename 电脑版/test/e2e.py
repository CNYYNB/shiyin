#!/usr/bin/env python3
"""End-to-end UI test: drives the real app server (Linux build) with Playwright + the fake phone."""
import json, os, re, sys, time, urllib.request
urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
from playwright.sync_api import sync_playwright

T = os.path.dirname(os.path.abspath(__file__))
SHOTS = os.path.join(T, 'shots'); os.makedirs(SHOTS, exist_ok=True)
url = re.search(r'SHIYIN_URL (\S+)', open('/tmp/app.log').read()).group(1)
results = []
def check(name, ok, info=''):
    results.append((name, bool(ok), info)); print(('PASS ' if ok else 'FAIL ') + name, info, flush=True)

with sync_playwright() as p:
    b = p.chromium.launch(args=['--autoplay-policy=no-user-gesture-required'])
    pg = b.new_page(viewport={'width': 1280, 'height': 800})
    errors = []
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.on('console', lambda m: m.type == 'error' and errors.append(m.text))
    pg.goto(url)
    pg.wait_for_selector('.row', timeout=15000)
    time.sleep(0.5)
    rows = pg.eval_on_selector_all('.row', 'els => els.map(e => e.querySelector(".t").textContent + " | " + e.querySelector(".a").textContent)')
    print('\n'.join(rows))
    check('library scanned 8 tracks', len(rows) == 8, len(rows))
    check('tag title 晴天/周杰伦', any(r.startswith('晴天 | 周杰伦') for r in rows))
    check('filename fallback Artist - Title', any(r == 'Song M4A | Artist B' for r in rows))
    check('chinese filename fallback', any(r == '作品 | 歌手丙' for r in rows))
    check('embedded cover shown', pg.eval_on_selector('.row', 'e => true') and pg.evaluate('''() => [...document.querySelectorAll('.row')].some(r => r.querySelector('.cover').style.backgroundImage.includes('/api/cover'))'''))
    pg.screenshot(path=f'{SHOTS}/01_songs.png')

    # Range support on /media
    status = pg.evaluate('''async () => { const id = document.querySelector('.row').dataset.id; const r = await fetch('/media?id=' + id, {headers: {Range: 'bytes=100-199'}}); const b = await r.arrayBuffer(); return [r.status, b.byteLength, r.headers.get('content-range')]; }''')
    check('media Range → 206', status[0] == 206 and status[1] == 100, status)
    # token protection
    code = urllib.request.urlopen(urllib.request.Request(url.split('/?')[0] + '/api/whoami')).status
    try:
        urllib.request.urlopen(url.split('/?')[0] + '/api/library'); unauth = 200
    except urllib.error.HTTPError as e: unauth = e.code
    check('api requires token', unauth == 401, unauth)

    # play 晴天 (mp3)
    pg.click('.row:has-text("晴天")')
    pg.wait_for_function('() => !document.getElementById("audio").paused && document.getElementById("audio").currentTime > 0.3', timeout=10000)
    check('mp3 plays', True)
    check('now playing bar', pg.inner_text('#npTitle') == '晴天', pg.inner_text('#npTitle'))
    pg.keyboard.press('Space'); time.sleep(0.3)
    check('space pauses', pg.evaluate('document.getElementById("audio").paused'))
    pg.keyboard.press('Space'); time.sleep(0.3)
    check('space resumes', not pg.evaluate('document.getElementById("audio").paused'))
    # open player → embedded lyrics
    pg.click('#btnLyr')
    pg.wait_for_selector('#lyrics p', timeout=8000)
    time.sleep(1.5)
    check('embedded lyrics shown', '故事的小黄花' in pg.inner_text('#lyrics'), pg.inner_text('#lyrSrc'))
    pg.screenshot(path=f'{SHOTS}/02_player_embedded.png')
    # ctrl+right → next
    before = pg.inner_text('#npTitle')
    pg.keyboard.press('Control+ArrowRight'); time.sleep(0.8)
    check('ctrl+→ next track', pg.inner_text('#npTitle') != before, pg.inner_text('#npTitle'))
    pg.keyboard.press('Control+ArrowLeft'); time.sleep(0.8)
    check('ctrl+← previous track', pg.inner_text('#npTitle') == before, pg.inner_text('#npTitle'))
    pg.click('#playerClose')

    # wav with .lrc sidecar, synced highlight
    pg.click('.row:has-text("wavfile")')
    pg.click('#btnLyr')
    pg.wait_for_function('() => document.querySelector("#lyrics p.on")', timeout=10000)
    time.sleep(2.2)
    on = pg.inner_text('#lyrics p.on')
    check('synced .lrc highlight follows time', on in ('第二行歌词', '第三行'), on)
    pg.screenshot(path=f'{SHOTS}/03_player_lrc.png')
    pg.click('#playerClose')

    # modes
    pg.click('#btnMode'); m1 = pg.get_attribute('#btnMode', 'title')
    pg.click('#btnMode'); m2 = pg.get_attribute('#btnMode', 'title')
    pg.click('#btnMode'); m3 = pg.get_attribute('#btnMode', 'title')
    check('play mode cycles 单曲循环→随机→列表循环', m1.startswith('单曲循环') and m2.startswith('随机') and m3.startswith('列表循环'), [m1, m2, m3])

    # 随机播放 button
    pg.click('button:has-text("随机播放")'); time.sleep(0.5)
    check('随机播放 sets shuffle mode', pg.get_attribute('#btnMode', 'title').startswith('随机'))
    pg.click('button:has-text("播放全部")'); time.sleep(0.5)
    check('播放全部 starts first song, sequential', pg.get_attribute('#btnMode', 'title').startswith('列表循环') and pg.inner_text('#npTitle') == rows[0].split(' | ')[0], pg.inner_text('#npTitle'))

    # search
    pg.fill('#search', '周杰伦'); time.sleep(0.4)
    check('search filters', pg.eval_on_selector_all('.row', 'e => e.length') == 1)
    pg.fill('#search', ''); time.sleep(0.3)

    # albums / artists tabs
    pg.click('#tabs button[data-tab=albums]'); time.sleep(0.3)
    pg.screenshot(path=f'{SHOTS}/04_albums.png')
    check('albums tab', pg.eval_on_selector_all('.card', 'e => e.length') >= 3)
    pg.click('.card:has-text("叶惠美")'); time.sleep(0.3)
    check('album detail', '叶惠美' in pg.inner_text('.detail-hero'))
    pg.click('[data-act=back]')
    pg.click('#tabs button[data-tab=artists]'); time.sleep(0.3)
    check('artists tab', pg.eval_on_selector_all('.arow', 'e => e.length') >= 3)
    pg.click('#tabs button[data-tab=songs]'); time.sleep(0.3)

    # edit meta
    pg.click('.row:has-text("Vorbis Song")', button='right')
    pg.click('#menu button:has-text("编辑歌名")')
    pg.fill('#eT', '沃比斯'); pg.fill('#eA', '测试歌手'); pg.click('#eOk'); time.sleep(0.5)
    check('edit meta override', pg.locator('.row:has-text("沃比斯")').count() == 1)
    # change cover
    pg.click('.row:has-text("沃比斯")', button='right')
    pg.click('#menu button:has-text("更换封面")')
    with pg.expect_file_chooser() as fc:
        pg.click('.pick[data-a=pick]')
    fc.value.set_files(os.path.join(T, 'green.jpg')); time.sleep(0.8)
    bg = pg.eval_on_selector('.row:has-text("沃比斯") .cover', 'e => e.style.backgroundImage')
    check('custom cover', '/api/cover' in bg, bg)
    # favourite + playlist
    pg.click('.row:has-text("沃比斯") [data-act=fav]'); time.sleep(0.3)
    pg.click('.row:has-text("沃比斯")', button='right')
    pg.click('#menu button:has-text("添加到歌单")')
    pg.click('.pick[data-new]'); pg.fill('#pv', '学习'); pg.click('#pok'); time.sleep(0.4)
    pg.click('#tabs button[data-tab=playlists]'); time.sleep(0.3)
    check('playlists created', '学习' in pg.inner_text('#main') and '我喜欢' in pg.inner_text('#main'))
    pg.screenshot(path=f'{SHOTS}/05_playlists.png')
    pg.click('#tabs button[data-tab=songs]')

    # ---------- 从手机导入 ----------
    pg.click('#btnPhone')
    pg.wait_for_selector('.ph-dev', timeout=6000)
    check('UDP discovery finds phone', 'Redmi K70E' in pg.inner_text('#phFound'))
    pg.screenshot(path=f'{SHOTS}/06_phone_connect.png')
    pg.fill('#phCode', '111111'); time.sleep(0.2)
    pg.wait_for_selector('#phErr:has-text("配对码")', timeout=10000)
    check('wrong code error', '配对码不对' in pg.inner_text('#phErr'), pg.inner_text('#phErr'))
    pg.fill('#phHost', '127.0.0.1:18520')
    pg.fill('#phCode', '123456')
    pg.wait_for_selector('.loading', timeout=3000)
    check('loading text shown', '正在读取手机曲库' in pg.inner_text('.loading'))
    pg.wait_for_selector('.ph-head', timeout=20000)
    info = pg.inner_text('.ph-head .info'); print(info)
    check('present counted (qingtian by key + Eason same name/size)', '电脑上已有 2 首' in info, info)
    pg.click('#phSeg button[data-f=all]'); time.sleep(0.3)
    pg.screenshot(path=f'{SHOTS}/07_phone_list.png')
    pg.click('#phImpAll')
    pg.wait_for_function('() => { const t = document.getElementById("phProgTxt"); return t && t.textContent.startsWith("完成"); }', timeout=60000)
    msg = pg.inner_text('#phProgTxt'); print(msg)
    check('import finished', '新导入 3 首' in msg and '已有 2 首' in msg and '失败' not in msg, msg)
    pg.screenshot(path=f'{SHOTS}/08_phone_done.png')
    pg.click('.modal .x'); time.sleep(1)
    pg.click('#tabs button[data-tab=songs]'); time.sleep(0.5)
    rows2 = pg.eval_on_selector_all('.row', 'els => els.map(e => e.querySelector(".t").textContent + " | " + e.querySelector(".a").textContent)')
    print('\n'.join(rows2))
    check('library grew to 11', len(rows2) == 11, len(rows2))
    check('phone Chinese names applied', any(r == '十年 | 陈奕迅' for r in rows2) and any(r == '红豆 | 王菲' for r in rows2) and any(r.startswith('晴天 | 周杰伦') for r in rows2))
    pg.screenshot(path=f'{SHOTS}/09_after_import.png')
    pg.click('#tabs button[data-tab=playlists]'); time.sleep(0.3)
    txt = pg.inner_text('#main')
    check('phone playlist 跑步 imported', '跑步' in txt, txt.replace('\n', ' '))
    pg.click('.card:has-text("跑步")'); time.sleep(0.3)
    check('跑步 has 2 songs', pg.eval_on_selector_all('.row', 'e => e.length') == 2)
    pg.click('[data-act=back]')
    pg.click('.card.fav'); time.sleep(0.3)
    favn = pg.eval_on_selector_all('.row', 'e => e.length')
    check('我喜欢 merged (1 local + 2 phone)', favn == 3, favn)
    # synced lyrics from phone converted to simplified
    pg.click('.row:has-text("晴天")')
    pg.click('#btnLyr'); time.sleep(1.5)
    lt = pg.inner_text('#lyrics')
    check('phone .lrc saved + 繁→简', '从出生那年就飘着' in lt, lt[:80])
    pg.click('#playerClose')

    # re-open import → everything present now
    pg.click('#btnPhone'); pg.wait_for_selector('.ph-head', timeout=5000)
    info2 = pg.inner_text('.ph-head .info')
    check('after import all present', '电脑上已有 5 首' in info2, info2)
    pg.click('.modal .x')

    # persistence: reload page
    pg.click('#tabs button[data-tab=songs]')
    pg.click('.row:has-text("十年")'); time.sleep(1.5)
    pg.evaluate('document.getElementById("audio").currentTime = 6')
    time.sleep(0.5)
    pg.evaluate('saveState()'); time.sleep(0.5)
    pg.reload(); pg.wait_for_selector('.row'); time.sleep(1.5)
    check('queue restored after restart', pg.inner_text('#npTitle') == '十年', pg.inner_text('#npTitle'))
    pos = pg.evaluate('document.getElementById("audio").currentTime')
    check('position restored', 5.5 < pos < 9, pos)

    # settings
    pg.click('#btnSettings'); time.sleep(0.4)
    pg.screenshot(path=f'{SHOTS}/10_settings.png')
    pg.click('#sChange'); time.sleep(0.5)
    pg.screenshot(path=f'{SHOTS}/11_folder.png')
    pg.click('#fsCancel'); time.sleep(0.3)
    pg.keyboard.press('Escape')
    errors = [e for e in errors if '400 (Bad Request)' not in e]  # 400 = the deliberate wrong pairing code
    check('no JS errors', not errors, errors[:5])
    b.close()

fails = [r for r in results if not r[1]]
print(f'\n{len(results) - len(fails)}/{len(results)} passed')
json.dump(results, open(os.path.join(T, 'e2e_results.json'), 'w'), ensure_ascii=False, indent=1)
sys.exit(1 if fails else 0)
