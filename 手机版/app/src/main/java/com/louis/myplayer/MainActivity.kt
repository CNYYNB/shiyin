package com.louis.myplayer

import android.Manifest
import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.view.View
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.core.view.WindowCompat
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import androidx.webkit.WebViewAssetLoader
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.MoreExecutors
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.FileInputStream
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicBoolean

class MainActivity : ComponentActivity() {
    private lateinit var web: WebView
    private var controllerFuture: ListenableFuture<MediaController>? = null
    private var controller: MediaController? = null
    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newFixedThreadPool(3)
    private val lyricExec = Executors.newSingleThreadExecutor()
    /** Library scans run here, never on [io], so a long scan can't starve anything else. */
    private val scanExec = Executors.newSingleThreadExecutor()
    /** Update checks/downloads get their own threads (cached pool: a hung DNS lookup can't block the next check). */
    private val updExec = Executors.newCachedThreadPool()
    private val scanQueued = AtomicBoolean(false)
    @Volatile private var lastResumeCheck = 0L

    @Volatile private var stateJson = "{}"
    @Volatile private var queueJson = "[]"
    @Volatile private var pageReady = false
    private var exportedOnce = false

    private val permLauncher = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        val ok = hasAudioPermission()
        // Denied and the system won't show the dialog again → the UI sends the user to app settings instead.
        val blocked = !ok && !shouldShowRequestPermissionRationale(audioPermission())
        if (ok) startScan()
        js("window.App && App.onPermission && App.onPermission($ok, $blocked)")
    }

    @Volatile private var pendingCoverIds: List<String> = emptyList()
    private val coverLauncher = registerForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        val ids = pendingCoverIds; pendingCoverIds = emptyList()
        if (uri == null) { js("App.onCoverChanged && App.onCoverChanged(false, '')"); return@registerForActivityResult }
        io.execute {
            val ok = try {
                val data = contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: ByteArray(0)
                Covers.setCustom(this, ids.mapNotNull { Library.get(it) }, data)
            } catch (e: Exception) { false }
            js("App.onCoverChanged && App.onCoverChanged($ok, ${JSONObject.quote(if (ok) "" else "无法读取这张图片")})")
        }
    }

    private fun idsOf(json: String): List<String> = try { val a = JSONArray(json); (0 until a.length()).map { a.getString(it) } } catch (_: Exception) { emptyList() }

    private val folderLauncher = registerForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri: Uri? ->
        if (uri != null) {
            try {
                contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
            } catch (_: Exception) {}
            Library.setFolders(Library.folders() + uri.toString())
            js("App.onFolderAdded(${JSONObject.quote(Library.folderLabel(uri.toString()))})")
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val startLight = (Kv.get(this, "theme") ?: "").contains("light")
        applyBars(startLight)

        val loader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this))
            .addPathHandler("/cover/") { path -> coverResponse(path) }
            .build()

        web = WebView(this)
        web.setBackgroundColor(Color.parseColor(if (startLight) "#F6F6F8" else "#121214"))
        web.overScrollMode = View.OVER_SCROLL_NEVER
        with(web.settings) {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            textZoom = 100
            mediaPlaybackRequiresUserGesture = true
        }
        web.webChromeClient = WebChromeClient()
        web.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? =
                loader.shouldInterceptRequest(request.url)

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val u = request.url
                if (u.host == "appassets.androidplatform.net") return false
                try { startActivity(Intent(Intent.ACTION_VIEW, u)) } catch (_: Exception) {}
                return true
            }

            override fun onPageFinished(view: WebView?, url: String?) { pageReady = true }
        }
        web.addJavascriptInterface(Bridge(), "Android")
        setContentView(web)
        web.loadUrl("https://appassets.androidplatform.net/assets/web/index.html")

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                web.evaluateJavascript("(window.App && App.onBack) ? App.onBack() : false") { r ->
                    if (r != "true") moveTaskToBack(true)
                }
            }
        })
        connectController()
    }

    private fun connectController() {
        val token = SessionToken(this, ComponentName(this, PlaybackService::class.java))
        val f = MediaController.Builder(this, token).buildAsync()
        controllerFuture = f
        f.addListener({
            try {
                val c = f.get()
                controller = c
                c.addListener(object : Player.Listener {
                    override fun onEvents(player: Player, events: Player.Events) {
                        updateState()
                        if (events.containsAny(Player.EVENT_MEDIA_ITEM_TRANSITION, Player.EVENT_IS_PLAYING_CHANGED,
                                Player.EVENT_REPEAT_MODE_CHANGED, Player.EVENT_SHUFFLE_MODE_ENABLED_CHANGED,
                                Player.EVENT_PLAYBACK_STATE_CHANGED, Player.EVENT_TIMELINE_CHANGED)) {
                            js("App.onState && App.onState()")
                        }
                    }
                    override fun onTimelineChanged(timeline: Timeline, reason: Int) { updateQueue() }
                })
                updateQueue(); updateState()
                js("App.onController && App.onController(${c.mediaItemCount})")
            } catch (e: Exception) {
                toast("播放服务连接失败：${e.message}")
            }
        }, MoreExecutors.directExecutor())
    }

    private val ticker = object : Runnable {
        override fun run() { updateState(); main.postDelayed(this, 250) }
    }

    override fun onResume() {
        super.onResume(); main.post(ticker); Updater.onResume(this)
        if (!exportedOnce) { exportedOnce = true; Exporter.schedule(this, 4000) } // first resume: refresh 拾音数据.json
        checkLibraryChanged()
    }

    /** Runs a library scan off the UI thread; JS hears App.onScanProgress / App.onScanDone. */
    private fun startScan() {
        if (!scanQueued.compareAndSet(false, true)) return
        js("window.App && App.onScanProgress && App.onScanProgress('正在扫描…')")
        scanExec.execute {
            val n = try {
                Library.scan { msg -> js("window.App && App.onScanProgress && App.onScanProgress(${JSONObject.quote(msg)})") }
            } catch (e: Throwable) { -1 } finally { scanQueued.set(false) }
            Exporter.schedule(this@MainActivity)
            js("window.App && App.onScanDone && App.onScanDone($n)")
        }
    }

    /** On resume: rescan if MediaStore's audio changed (or 所有文件访问 was toggled) since the last scan. */
    private fun checkLibraryChanged() {
        if (!hasAudioPermission() || Library.scanning || scanQueued.get()) return
        val now = System.currentTimeMillis()
        if (now - lastResumeCheck < 3000) return
        lastResumeCheck = now
        io.execute {
            try {
                // 只在从没扫描过时自动扫描；之后音乐库直接用保存的结果，新歌由用户手动「重新扫描」
                if (Library.scannedAt == 0L) startScan()
            } catch (_: Throwable) {}
        }
    }
    override fun onPause() { super.onPause(); main.removeCallbacks(ticker); js("App.saveSession && App.saveSession()") }

    override fun onDestroy() {
        main.removeCallbacks(ticker)
        controllerFuture?.let { MediaController.releaseFuture(it) }
        web.destroy()
        super.onDestroy()
    }

    private fun updateQueue() {
        val c = controller ?: return
        val arr = JSONArray()
        for (i in 0 until c.mediaItemCount) arr.put(c.getMediaItemAt(i).mediaId)
        queueJson = arr.toString()
    }

    private fun updateState() {
        val c = controller ?: return
        val mode = when {
            c.shuffleModeEnabled -> "shuffle"
            c.repeatMode == Player.REPEAT_MODE_ONE -> "one"
            else -> "all"
        }
        val dur = c.duration
        stateJson = JSONObject()
            .put("id", c.currentMediaItem?.mediaId ?: "")
            .put("playing", c.isPlaying)
            .put("playWhenReady", c.playWhenReady)
            .put("buffering", c.playbackState == Player.STATE_BUFFERING)
            .put("ended", c.playbackState == Player.STATE_ENDED)
            .put("pos", c.currentPosition)
            .put("dur", if (dur > 0) dur else 0)
            .put("index", c.currentMediaItemIndex)
            .put("count", c.mediaItemCount)
            .put("mode", mode)
            .put("t", System.currentTimeMillis())
            .toString()
    }

    /** Android 12+: make the system launch screen (drawn before our code runs) use the app's own theme. */
    private fun syncNightMode(light: Boolean) {
        if (Build.VERSION.SDK_INT < 31) return
        try {
            val um = getSystemService(android.app.UiModeManager::class.java) ?: return
            val want = if (light) android.app.UiModeManager.MODE_NIGHT_NO else android.app.UiModeManager.MODE_NIGHT_YES
            um.setApplicationNightMode(want)
        } catch (_: Exception) {}
    }

    private fun applyBars(light: Boolean) {
        syncNightMode(light)
        val c = Color.parseColor(if (light) "#F6F6F8" else "#121214")
        window.statusBarColor = c
        window.navigationBarColor = c
        window.decorView.setBackgroundColor(c)
        WindowCompat.getInsetsController(window, window.decorView).apply {
            isAppearanceLightStatusBars = light
            isAppearanceLightNavigationBars = light
        }
    }

    private fun js(code: String) {
        main.post { if (!isDestroyed) web.evaluateJavascript(code, null) }
    }

    private fun toast(s: String) = main.post { Toast.makeText(this, s, Toast.LENGTH_SHORT).show() }

    private fun audioPermission() =
        if (Build.VERSION.SDK_INT >= 33) Manifest.permission.READ_MEDIA_AUDIO else Manifest.permission.READ_EXTERNAL_STORAGE

    private fun hasAudioPermission(): Boolean =
        ContextCompat.checkSelfPermission(this, audioPermission()) == PackageManager.PERMISSION_GRANTED

    private fun coverResponse(path: String): WebResourceResponse {
        val id = path.substringBefore('?').substringBefore('.').trim('/')
        val t = Library.get(id)
        val f = try { t?.let { Covers.file(this, it) } } catch (_: Exception) { null }
        return if (f != null) {
            WebResourceResponse("image/jpeg", null, 200, "OK", mapOf("Cache-Control" to "max-age=86400"), FileInputStream(f))
        } else {
            WebResourceResponse("text/plain", "utf-8", 404, "Not Found", mapOf("Cache-Control" to "max-age=600"), ByteArrayInputStream(ByteArray(0)))
        }
    }

    private fun onMain(block: (MediaController) -> Unit) = main.post { controller?.let(block) }

    private fun syncInfo(): JSONObject {
        var cands: List<JSONObject> = emptyList()
        val vpn = try {
            val cm = getSystemService(CONNECTIVITY_SERVICE) as android.net.ConnectivityManager
            cm.allNetworks.any { cm.getNetworkCapabilities(it)?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN) == true }
        } catch (_: Exception) { false }
        return JSONObject().put("running", SyncServer.running).put("service", SyncService.running)
            .put("stopReason", SyncService.stopReason)
            .put("port", SyncServer.port).put("code", SyncServer.code).put("name", SyncServer.deviceName())
            .put("served", SyncServer.served.get()).put("client", SyncServer.lastClient)
            .put("ips", JSONArray(SyncServer.addresses())).put("cands", JSONArray(SyncServer.candidates().also { cands = it }))
            .put("subnets", JSONArray(cands))
            .put("vpn", vpn).put("rev", ReverseClient.statusJson()).put("lastPc", Kv.get(this, "lastPc") ?: "")
            .put("hotspot", hotspotInfo(cands)).put("gw", wifiGateway())
    }

    /** 2.3: is this phone's own Wi-Fi hotspot on, and its address (the PC joins it and connects to this IP). */
    private fun hotspotInfo(cands: List<JSONObject>): JSONObject {
        val hs = cands.filter { it.optString("kind") == "hotspot" }
        val apOn = try {
            val wm = applicationContext.getSystemService(WIFI_SERVICE) as android.net.wifi.WifiManager
            wm.javaClass.getMethod("isWifiApEnabled").invoke(wm) as? Boolean
        } catch (_: Throwable) { null }
        // some phones run the hotspot on wlan0 when Wi-Fi is off: then it's the .1 address
        val ips = hs.ifEmpty { if (apOn == true) cands.filter { it.optString("kind") in setOf("wifi", "other") && it.optString("ip").endsWith(".1") } else emptyList() }
            .map { it.optString("ip") }
        return JSONObject().put("on", apOn == true || hs.isNotEmpty()).put("ips", JSONArray(ips))
    }

    /** The router's address on the Wi-Fi network (for the 关闭 AP 隔离 guide), or "". */
    private fun wifiGateway(): String = try {
        Net.wifiLink(this)?.routes?.firstOrNull { it.isDefaultRoute && it.gateway is java.net.Inet4Address }?.gateway?.hostAddress ?: ""
    } catch (_: Throwable) { "" }

    inner class Bridge {
        @JavascriptInterface fun hasPermission(): Boolean = hasAudioPermission()

        @JavascriptInterface fun requestPermission() {
            val perms = if (Build.VERSION.SDK_INT >= 33)
                arrayOf(Manifest.permission.READ_MEDIA_AUDIO, Manifest.permission.POST_NOTIFICATIONS)
            else arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE)
            main.post { permLauncher.launch(perms) }
        }

        @JavascriptInterface fun openAppSettings() = main.post {
            startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
        }

        @JavascriptInterface fun hasAllFiles(): Boolean =
            if (Build.VERSION.SDK_INT >= 30) Environment.isExternalStorageManager() else hasAudioPermission()

        /** Whether 所有文件访问 is a separate grant on this Android version (11+). */
        @JavascriptInterface fun allFilesApplies(): Boolean = Build.VERSION.SDK_INT >= 30

        @JavascriptInterface fun requestAllFiles() = main.post {
            if (Build.VERSION.SDK_INT >= 30) {
                try {
                    startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:$packageName")))
                } catch (_: Exception) {
                    startActivity(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
                }
            }
        }

        @JavascriptInterface fun isIgnoringBattery(): Boolean =
            (getSystemService(POWER_SERVICE) as PowerManager).isIgnoringBatteryOptimizations(packageName)

        @SuppressLint("BatteryLife")
        @JavascriptInterface fun requestIgnoreBattery() = main.post {
            try {
                startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
            } catch (_: Exception) {
                startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
            }
        }

        @JavascriptInterface fun getLibrary(): String = Library.libraryJson()
        @JavascriptInterface fun isScanning(): Boolean = Library.scanning

        @JavascriptInterface fun rescan() { if (!Library.scanning) startScan() }

        @JavascriptInterface fun addFolder() = main.post { folderLauncher.launch(null) }

        @JavascriptInterface fun getFolders(): String {
            val arr = JSONArray()
            Library.folders().forEach { arr.put(JSONObject().put("uri", it).put("label", Library.folderLabel(it))) }
            return arr.toString()
        }

        @JavascriptInterface fun removeFolder(uri: String) {
            Library.setFolders(Library.folders().filter { it != uri })
            try { contentResolver.releasePersistableUriPermission(Uri.parse(uri), Intent.FLAG_GRANT_READ_URI_PERMISSION) } catch (_: Exception) {}
        }

        @JavascriptInterface fun clearCoverCache() { io.execute { Covers.clear(this@MainActivity) } }

        @JavascriptInterface fun setTheme(light: Boolean) = main.post {
            applyBars(light)
            web.setBackgroundColor(Color.parseColor(if (light) "#F6F6F8" else "#121214"))
        }
        @JavascriptInterface fun kvGet(key: String): String? = Kv.get(this@MainActivity, key)
        @JavascriptInterface fun kvSet(key: String, value: String?) = Kv.set(this@MainActivity, key, value)

        // ---- playback ----
        @JavascriptInterface fun play(idsJson: String, index: Int, posMs: Long, autoplay: Boolean) {
            val ids = JSONArray(idsJson)
            val items = ArrayList<MediaItem>(ids.length())
            var start = index
            for (i in 0 until ids.length()) {
                val t = Library.get(ids.getString(i))
                if (t == null) { if (i < index) start--; continue }
                items.add(Library.mediaItem(t))
            }
            if (items.isEmpty()) return
            start = start.coerceIn(0, items.size - 1)
            onMain { c ->
                c.setMediaItems(items, start, maxOf(0L, posMs))
                c.prepare()
                if (autoplay) c.play()
            }
        }

        @JavascriptInterface fun playAt(index: Int) = onMain { c ->
            if (index in 0 until c.mediaItemCount) { c.seekTo(index, 0); c.play() }
        }

        @JavascriptInterface fun playNext(id: String) {
            val t = Library.get(id) ?: return
            onMain { c ->
                if (c.mediaItemCount == 0) { c.setMediaItem(Library.mediaItem(t)); c.prepare(); c.play() }
                else c.addMediaItem(c.currentMediaItemIndex + 1, Library.mediaItem(t))
            }
        }

        @JavascriptInterface fun addToQueue(id: String) {
            val t = Library.get(id) ?: return
            onMain { c ->
                if (c.mediaItemCount == 0) { c.setMediaItem(Library.mediaItem(t)); c.prepare() }
                else c.addMediaItem(Library.mediaItem(t))
            }
        }

        @JavascriptInterface fun removeFromQueue(index: Int) = onMain { c -> if (index in 0 until c.mediaItemCount) c.removeMediaItem(index) }

        @JavascriptInterface fun toggle() = onMain { c ->
            if (c.isPlaying) c.pause() else {
                if (c.playbackState == Player.STATE_ENDED) c.seekTo(c.currentMediaItemIndex, 0)
                if (c.playbackState == Player.STATE_IDLE) c.prepare()
                c.play()
            }
        }
        @JavascriptInterface fun pause() = onMain { it.pause() }
        @JavascriptInterface fun next() = onMain { c -> if (c.hasNextMediaItem()) c.seekToNextMediaItem() else if (c.mediaItemCount > 0) c.seekTo(0, 0) }
        @JavascriptInterface fun prev() = onMain { c ->
            if (c.currentPosition > 3000 || !c.hasPreviousMediaItem()) c.seekTo(0) else c.seekToPreviousMediaItem()
        }
        @JavascriptInterface fun seek(ms: Long) = onMain { it.seekTo(maxOf(0L, ms)) }

        @JavascriptInterface fun setMode(mode: String) = onMain { c ->
            when (mode) {
                "one" -> { c.shuffleModeEnabled = false; c.repeatMode = Player.REPEAT_MODE_ONE }
                "shuffle" -> { c.repeatMode = Player.REPEAT_MODE_ALL; c.shuffleModeEnabled = true }
                else -> { c.shuffleModeEnabled = false; c.repeatMode = Player.REPEAT_MODE_ALL }
            }
        }

        @JavascriptInterface fun getState(): String = stateJson
        @JavascriptInterface fun getQueue(): String = queueJson

        // ---- lyrics ----
        @JavascriptInterface fun getLyrics(id: String, force: Boolean) {
            val t = Library.get(id) ?: return
            lyricExec.execute {
                val r = try { Lyrics.load(this@MainActivity, t, force) } catch (e: Exception) {
                    JSONObject().put("source", "none").put("text", "").put("synced", false).put("error", e.message ?: "错误")
                }
                js("App.onLyrics(${JSONObject.quote(id)}, ${JSONObject.quote(r.toString())})")
            }
        }

        @JavascriptInterface fun searchLyrics(title: String, artist: String, reqId: Int) {
            io.execute {
                val r = try { JSONObject().put("ok", true).put("items", Lyrics.search(title, artist)) }
                        catch (e: Exception) { JSONObject().put("ok", false).put("error", e.message ?: "网络错误") }
                js("App.onLyricSearch($reqId, ${JSONObject.quote(r.toString())})")
            }
        }

        @JavascriptInterface fun saveLyrics(id: String, text: String, synced: Boolean) {
            val t = Library.get(id) ?: return
            Lyrics.saveManual(this@MainActivity, t, text, synced)
        }

        @JavascriptInterface fun resetLyrics(id: String) {
            val t = Library.get(id) ?: return
            Lyrics.clearManual(this@MainActivity, t)
        }

        // ---- 歌曲信息（中文名）/ 封面 ----
        @JavascriptInterface fun setMeta(id: String, title: String, artist: String, album: String, src: String) {
            val t = Library.get(id) ?: return
            Meta.set(t.key, title, artist, album, src)
        }
        /** field = title | artist | album; empty value = back to the file's own tag. */
        @JavascriptInterface fun setMetaField(id: String, field: String, value: String, src: String) {
            val t = Library.get(id) ?: return
            when (field) {
                "title" -> Meta.set(t.key, value, null, null, src)
                "artist" -> Meta.set(t.key, null, value, null, src)
                "album" -> Meta.set(t.key, null, null, value, src)
            }
        }
        @JavascriptInterface fun clearMeta(id: String) { Library.get(id)?.let { Meta.clear(it.key) } }
        @JavascriptInterface fun clearAutoMeta(): Int = Meta.clearAuto()
        @JavascriptInterface fun markTried(id: String) { Library.get(id)?.let { Meta.markTried(it.key) } }
        @JavascriptInterface fun hasArt(id: String): Boolean = Library.get(id)?.let { try { Covers.file(this@MainActivity, it) != null } catch (_: Exception) { false } } ?: false

        /** Online (Apple HK/TW catalogue) lookup for a song's Chinese names. → App.onMatch(reqId, json) */
        @JavascriptInterface fun matchOnline(id: String, title: String, artist: String, reqId: Int) {
            val t = Library.get(id)
            io.execute {
                val r = try {
                    val ti = title.ifBlank { t?.title ?: "" }; val ar = if (title.isBlank()) (t?.artist ?: "") else artist
                    val items = Online.candidates(ti, ar, t?.duration ?: 0L)
                    JSONObject().put("ok", true).put("items", items).put("best", Online.bestIndex(items, t?.title ?: ti, t?.duration ?: 0L))
                } catch (e: Exception) { JSONObject().put("ok", false).put("error", e.message ?: "网络错误") }
                js("App.onMatch && App.onMatch($reqId, ${JSONObject.quote(r.toString())})")
            }
        }

        @JavascriptInterface fun pickCover(idsJson: String) = main.post {
            pendingCoverIds = idsOf(idsJson)
            try { coverLauncher.launch("image/*") } catch (e: Exception) { toast("无法打开相册") }
        }
        @JavascriptInterface fun setCoverUrl(idsJson: String, url: String) {
            val list = idsOf(idsJson).mapNotNull { Library.get(it) }
            io.execute {
                val ok = try { val r = Online.get(url); r.code == 200 && Covers.setCustom(this@MainActivity, list, r.body) } catch (_: Exception) { false }
                js("App.onCoverChanged && App.onCoverChanged($ok, ${JSONObject.quote(if (ok) "" else "封面下载失败")})")
            }
        }
        @JavascriptInterface fun resetCover(idsJson: String) {
            Covers.clearCustom(this@MainActivity, idsOf(idsJson).mapNotNull { Library.get(it) })
        }

        // ---- 整理到 Music/拾音 ----
        @JavascriptInterface fun homePath(): String = Library.homeDir().absolutePath.removePrefix("/storage/emulated/0/").let { "内部存储/$it" }
        @JavascriptInterface fun outsideCount(): Int = Library.outsideHome().size
        @JavascriptInterface fun organize() {
            if (!scanQueued.compareAndSet(false, true)) { toast("正在扫描，请稍后"); return }
            js("window.App && App.onScanProgress && App.onScanProgress('正在整理…')")
            scanExec.execute {
                val r = try { Library.organize { msg -> js("App.onScanProgress && App.onScanProgress(${JSONObject.quote(msg)})") } }
                        catch (e: Throwable) { JSONObject().put("ok", false).put("error", e.message ?: "整理失败") }
                val n = try { Library.scan { msg -> js("App.onScanProgress && App.onScanProgress(${JSONObject.quote(msg)})") } } catch (e: Throwable) { -1 }
                scanQueued.set(false)
                js("App.onOrganizeDone && App.onOrganizeDone(${JSONObject.quote(r.toString())}, $n)")
            }
        }

        @JavascriptInterface fun toast(msg: String) = this@MainActivity.toast(msg)
        @JavascriptInterface fun exitApp() = main.post { moveTaskToBack(true) }
        // ---- 传到电脑 ----
        // 2.2: the server lives in SyncService (foreground, WifiLock + wake lock, 30 min idle stop), so it
        // keeps running when the app is in the background or the screen is off.
        @JavascriptInterface fun syncStart(): String {
            val ok = SyncServer.start(this@MainActivity)
            if (ok) SyncService.start(this@MainActivity)
            runOnUiThread { window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) }
            return syncInfo().put("ok", ok).toString()
        }
        @JavascriptInterface fun syncStop() {
            SyncService.stop(this@MainActivity)
            runOnUiThread { window.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) }
        }
        @JavascriptInterface fun syncStatus(): String = syncInfo().toString()
        /** 「手机连电脑」: find PCs (UDP 18523) → App.onPcFound(json). */
        @JavascriptInterface fun pcDiscover() {
            updExec.execute {
                val list = ReverseClient.discover()
                js("App.onPcFound && App.onPcFound(${JSONObject.quote(org.json.JSONArray(list).toString())})")
            }
        }
        /** → App.onPcConnect(json {ok,msg,pc,host}) */
        @JavascriptInterface fun pcConnect(host: String, code: String) {
            if (!SyncServer.running) { SyncServer.start(this@MainActivity) }
            SyncService.start(this@MainActivity)
            Kv.set(this@MainActivity, "lastPc", host.trim())
            updExec.execute {
                val r = try { ReverseClient.connect(host, code) } catch (e: Throwable) { JSONObject().put("ok", false).put("msg", e.message ?: "连接失败") }
                js("App.onPcConnect && App.onPcConnect(${JSONObject.quote(r.toString())})")
            }
        }
        @JavascriptInterface fun pcDisconnect() = ReverseClient.reset()
        /** 2.2.1 网络诊断 → App.onNetDiag(report) */
        @JavascriptInterface fun netDiag(pcHost: String) {
            updExec.execute {
                val r = try { Diag.run(this@MainActivity, pcHost) } catch (e: Throwable) { "诊断出错：${e.message}" }
                js("App.onNetDiag && App.onNetDiag(${JSONObject.quote(r)})")
            }
        }
        /** 2.3: open the system hotspot (tethering) settings; falls back to the wireless settings page. */
        @JavascriptInterface fun openHotspotSettings() = main.post {
            val tries = listOf(Intent("android.settings.TETHER_SETTINGS"),
                Intent().setComponent(ComponentName("com.android.settings", "com.android.settings.TetherSettings")),
                Intent(Settings.ACTION_WIRELESS_SETTINGS))
            for (i in tries) { try { startActivity(i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)); break } catch (_: Throwable) {} }
        }
        @JavascriptInterface fun exportStatus(): String = Exporter.status(this@MainActivity).toString()
        @JavascriptInterface fun exportNow() { Exporter.schedule(this@MainActivity, 0) }

        @JavascriptInterface fun version(): String = BuildConfig.VERSION_NAME
        @JavascriptInterface fun versionCode(): Int = BuildConfig.VERSION_CODE

        // ---- in-app update ----
        /** silent=true: launch check (≤ once per 12h). Result → App.onUpdateCheck(json, silent). */
        @JavascriptInterface fun checkUpdate(silent: Boolean) {
            updExec.execute {
                // Hard 12 s ceiling on top of the connect/read timeouts (DNS lookups aren't covered by those).
                val f = updExec.submit<String> { Updater.check(this@MainActivity, silent) }
                val r = try { f.get(12, TimeUnit.SECONDS) }
                    catch (e: TimeoutException) { f.cancel(true); Updater.failure(e).toString() }
                    catch (e: java.util.concurrent.ExecutionException) { Updater.failure(e.cause ?: e).toString() }
                    catch (e: Throwable) { Updater.failure(e).toString() }
                js("window.App && App.onUpdateCheck && App.onUpdateCheck(${JSONObject.quote(r)}, $silent)")
            }
        }

        @JavascriptInterface fun isUpdating(): Boolean = Updater.downloading

        /** Download → verify sha256 → install. Status → App.onUpdateStatus(kind, pct, bytes, msg). */
        @JavascriptInterface fun startUpdate() {
            if (Updater.downloading) return
            updExec.execute {
                Updater.downloadAndInstall(this@MainActivity) { kind, pct, bytes, msg ->
                    js("App.onUpdateStatus && App.onUpdateStatus(${JSONObject.quote(kind)}, $pct, $bytes, ${if (msg == null) "null" else JSONObject.quote(msg)})")
                }
            }
        }

        @JavascriptInterface fun cancelUpdate() = Updater.cancel()
    }
}
