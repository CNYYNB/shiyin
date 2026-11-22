package com.louis.myplayer

import android.content.ContentUris
import android.content.Context
import android.media.MediaMetadataRetriever
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.DocumentsContract
import android.provider.MediaStore
import android.util.Log
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

object Library {
    private const val TAG = "Library"
    lateinit var app: Context
        private set

    @Volatile var tracks: List<Track> = emptyList()
        private set
    @Volatile private var byId: Map<String, Track> = emptyMap()
    @Volatile var scannedAt: Long = 0
        private set
    @Volatile var scanning = false
        private set


    fun init(ctx: Context) {
        app = ctx.applicationContext
        load()
        try { Kv.get(app, "scan_report")?.let { lastReport = JSONObject(it) } } catch (_: Exception) {}
    }

    fun get(id: String): Track? = byId[id]

    private fun libFile() = File(app.filesDir, "library.json")

    @Synchronized
    private fun load() {
        try {
            val f = libFile()
            if (!f.exists()) return
            val o = JSONObject(f.readText())
            val arr = o.getJSONArray("tracks")
            val list = ArrayList<Track>(arr.length())
            for (i in 0 until arr.length()) list.add(Track.fromJson(arr.getJSONObject(i)))
            setTracks(list)
            scannedAt = o.optLong("scannedAt")
        } catch (e: Exception) {
            Log.w(TAG, "load failed", e)
        }
    }

    private fun setTracks(list: List<Track>) {
        tracks = list
        byId = list.associateBy { it.id }
    }

    @Synchronized
    private fun save() {
        val arr = JSONArray()
        tracks.forEach { arr.put(it.toJson()) }
        val o = JSONObject().put("scannedAt", scannedAt).put("tracks", arr)
        val tmp = File(app.filesDir, "library.json.tmp")
        tmp.writeText(o.toString())
        tmp.renameTo(libFile())
    }

    fun libraryJson(): String {
        val arr = JSONArray()
        val home = homeDir().absolutePath + "/"
        tracks.forEach { t ->
            val d = display(t)
            val o = d.toJson()
            o.put("key", t.key).put("rTitle", t.title).put("rArtist", t.artist).put("rAlbum", t.album)
            o.put("edited", Meta.get(t.key)?.let { it.optString("title").isNotEmpty() || it.optString("artist").isNotEmpty() || it.optString("album").isNotEmpty() } ?: false)
            o.put("cv", Covers.customVersion(app, t))
            o.put("home", t.path.startsWith(home))
            o.put("tried", Meta.get(t.key)?.optLong("tried") ?: 0L)
            arr.put(o)
        }
        return JSONObject()
            .put("scannedAt", scannedAt)
            .put("tracks", arr)
            .put("folders", JSONArray(folders()))
            .put("report", lastReport)
            .toString()
    }

    /** Track with the user's name overrides (Chinese names etc.) applied; raw tags stay in [tracks]. */
    fun display(t: Track): Track {
        val m = Meta.get(t.key) ?: return t
        val ti = m.optString("title"); val ar = m.optString("artist"); val al = m.optString("album")
        if (ti.isEmpty() && ar.isEmpty() && al.isEmpty()) return t
        return t.copy(title = ti.ifEmpty { t.title }, artist = ar.ifEmpty { t.artist }, album = al.ifEmpty { t.album })
    }

    /** 拾音's own music folder: 内部存储/Music/拾音 */
    fun homeDir(): File = homeOverride ?: File(Environment.getExternalStorageDirectory(), "Music/拾音")
    @Volatile var homeOverride: File? = null // unit tests only

    fun outsideHome(): List<Track> {
        val home = homeDir().absolutePath + "/"
        return tracks.filter { it.path.isNotEmpty() && !it.path.startsWith(home) }
    }

    /**
     * Moves every song (and its same-name .lrc) that isn't in [homeDir] there, then rescans.
     * Needs 所有文件访问 on Android 11+. Returns a JSON summary.
     */
    fun organize(progress: (String) -> Unit): JSONObject {
        val home = homeDir()
        home.mkdirs()
        if (!home.isDirectory || !home.canWrite()) return JSONObject().put("ok", false).put("error", "无法写入 内部存储/Music/拾音，请先开启「所有文件访问」")
        val todo = outsideHome()
        var moved = 0; var dup = 0; var failed = 0
        val touched = ArrayList<String>()
        for ((i, t) in todo.withIndex()) {
            if (i % 5 == 0) progress("正在整理 ${i + 1}/${todo.size}…")
            val src = File(t.path)
            if (!src.exists()) continue
            var dest = File(home, src.name)
            if (dest.exists()) {
                if (dest.length() == src.length()) { dup++; continue } // already there (same file copied earlier)
                val base = src.name.substringBeforeLast('.'); val ext = src.name.substringAfterLast('.', "")
                var n = 2
                while (dest.exists()) { dest = File(home, "$base ($n)" + (if (ext.isNotEmpty()) ".$ext" else "")); n++ }
            }
            if (!moveFile(src, dest)) { failed++; continue }
            moved++
            touched.add(src.absolutePath); touched.add(dest.absolutePath)
            // same-name lyrics travel with the song
            val sBase = src.absolutePath.substringBeforeLast('.'); val dBase = dest.absolutePath.substringBeforeLast('.')
            for (ext in listOf(".lrc", ".LRC", ".Lrc")) {
                val l = File(sBase + ext)
                if (l.exists()) { val dl = File("$dBase.lrc"); if (!dl.exists() && moveFile(l, dl)) { touched.add(l.absolutePath); touched.add(dl.absolutePath) } }
            }
        }
        if (touched.isNotEmpty()) {
            progress("正在更新系统媒体库…")
            val latch = java.util.concurrent.CountDownLatch(touched.size)
            try {
                MediaScannerConnection.scanFile(app, touched.toTypedArray(), null) { _, _ -> latch.countDown() }
                latch.await(15, java.util.concurrent.TimeUnit.SECONDS)
            } catch (e: Exception) { Log.w(TAG, "scanFile after organize failed", e) }
        }
        return JSONObject().put("ok", true).put("moved", moved).put("dup", dup).put("failed", failed).put("total", todo.size)
    }

    private fun moveFile(src: File, dest: File): Boolean {
        try { if (src.renameTo(dest)) return true } catch (_: Exception) {}
        return try {
            src.inputStream().use { i -> dest.outputStream().use { o -> i.copyTo(o, 1 shl 16) } }
            if (dest.length() == src.length()) { src.delete(); true } else { dest.delete(); false }
        } catch (e: Exception) { Log.w(TAG, "move failed ${src.path}", e); try { dest.delete() } catch (_: Exception) {}; false }
    }

    // ---- folders (SAF trees) ----
    fun folders(): List<String> {
        val s = Kv.get(app, "folders") ?: return emptyList()
        return try { val a = JSONArray(s); (0 until a.length()).map { a.getString(it) } } catch (e: Exception) { emptyList() }
    }

    fun setFolders(list: List<String>) = Kv.set(app, "folders", JSONArray(list.distinct()).toString())

    fun folderLabel(tree: String): String = try {
        val docId = DocumentsContract.getTreeDocumentId(Uri.parse(tree))
        docId.substringAfter(':').ifEmpty { docId }.let { if (docId.startsWith("primary:")) "内部存储/$it" else it }
    } catch (e: Exception) { tree }

    // ---- scanning ----
    /** Extensions ExoPlayer (Media3 1.4.1 + platform decoders) can play. */
    val playableExt = setOf("mp3", "flac", "m4a", "m4b", "aac", "wav", "ogg", "oga", "opus", "amr", "mka", "3ga", "webm")
    /** Audio formats Media3 has no extractor for (WMA/ASF, APE, DSD, AIFF, WavPack, TTA): listed as skipped. */
    val unsupportedExt = setOf("wma", "ape", "dsf", "dff", "aif", "aiff", "aifc", "wv", "tta")

    private fun extOf(name: String) = name.substringAfterLast('.', "").lowercase()

    /** Common places downloaded / recorded music ends up (only those that exist). label to dir. */
    fun commonDirs(): List<Pair<String, File>> {
        val root = Environment.getExternalStorageDirectory()
        val names = listOf(
            "Download" to "下载 (Download)", "Music" to "音乐 (Music)", "Documents" to "文档 (Documents)",
            "Recordings" to "录音 (Recordings)", "MIUI/sound_recorder" to "小米录音机", "Podcasts" to "Podcasts", "Audiobooks" to "Audiobooks",
            "qqmusic" to "QQ音乐", "QQMusic" to "QQ音乐", "netease/cloudmusic" to "网易云音乐", "Netease" to "网易云音乐",
            "kgmusic" to "酷狗音乐", "KuGou" to "酷狗音乐", "kugou" to "酷狗音乐", "KuwoMusic" to "酷我音乐",
            "Tencent/QQfile_recv" to "QQ 接收的文件",
        )
        val out = ArrayList<Pair<String, File>>()
        val seen = HashSet<String>()
        try { homeDir().mkdirs() } catch (_: Exception) {}
        for ((rel, label) in names) {
            val f = File(root, rel)
            try { if (f.isDirectory && seen.add(f.canonicalPath)) out.add(label to f) } catch (_: Exception) {}
        }
        return out
    }

    fun hasAllFiles(): Boolean = Build.VERSION.SDK_INT >= 30 && Environment.isExternalStorageManager()

    @Volatile var lastReport: JSONObject = JSONObject()
        private set

    fun scan(progress: (String) -> Unit): Int {
        scanning = true
        try {
            val result = ArrayList<Track>()
            val knownPaths = HashSet<String>()
            val dirs = commonDirs()
            var skipped = 0
            val skippedExt = sortedSetOf<String>()

            // 1. Ask the system scanner to (re)index the common folders and any audio file it hasn't seen,
            //    so a file downloaded a minute ago shows up even if MediaStore hasn't caught it yet.
            progress("正在刷新系统媒体索引…")
            val before = try { mediaStorePaths() } catch (e: Exception) { emptySet() }
            val walked = ArrayList<File>()
            try { walk(dirs.map { it.second }, System.currentTimeMillis() + 5000, walked) } catch (e: Exception) { Log.w(TAG, "walk failed", e) }
            val unindexed = walked.filter { it.absolutePath !in before && extOf(it.name) in playableExt }
            val toScan = (dirs.map { it.second.absolutePath } + unindexed.take(300).map { it.absolutePath }).toTypedArray()
            if (toScan.isNotEmpty()) {
                val latch = java.util.concurrent.CountDownLatch(toScan.size)
                try {
                    MediaScannerConnection.scanFile(app, toScan, null) { _, _ -> latch.countDown() }
                    latch.await(if (unindexed.isEmpty()) 4L else 10L, java.util.concurrent.TimeUnit.SECONDS)
                } catch (e: Exception) { Log.w(TAG, "scanFile failed", e) }
            }

            // 2. MediaStore: every audio row (no IS_MUSIC / duration>=30s filter), plus odd-MIME files by extension.
            progress("正在读取媒体库…")
            try {
                val (ms, sk) = scanMediaStore(progress)
                result.addAll(ms); skipped += sk.size; skippedExt.addAll(sk)
            } catch (e: SecurityException) { Log.w(TAG, "no permission", e) }
            catch (e: Exception) { Log.w(TAG, "mediastore scan failed", e) }
            result.forEach { if (it.path.isNotEmpty()) knownPaths.add(it.path) }
            try { knownPaths.addAll(mediaStorePaths()) } catch (_: Exception) {} // incl. rows filtered out above
            val msCount = result.size

            // 3. Files the walk can see that MediaStore still doesn't have (needs 所有文件访问 on Android 11+, or the
            //    scanner hasn't finished yet): play them straight from the file path.
            var fileCount = 0
            for (f in walked) {
                if (f.absolutePath in knownPaths) continue
                val ext = extOf(f.name)
                if (ext in unsupportedExt) { skipped++; skippedExt.add(ext); continue }
                if (ext !in playableExt || !f.canRead()) continue
                knownPaths.add(f.absolutePath)
                val t = fileTrack(f) ?: continue
                result.add(t); fileCount++
                if (fileCount % 20 == 0) progress("已读取 $fileCount 个文件…")
            }

            // 4. User-added SAF folders.
            val known = HashSet<String>()
            result.forEach { known.add(it.path.substringAfterLast('/') + "|" + it.size) }
            var safCount = 0
            for (tree in folders()) {
                progress("正在扫描文件夹 ${folderLabel(tree)}…")
                try {
                    val r = scanTree(Uri.parse(tree), known, progress)
                    result.addAll(r); safCount += r.size
                } catch (e: Exception) { Log.w(TAG, "tree scan failed $tree", e) }
            }
            scannedAt = System.currentTimeMillis()
            setTracks(result)
            save()
            lastReport = JSONObject()
                .put("at", scannedAt)
                .put("searched", JSONArray(dirs.map { it.first }.distinct()))
                .put("mediaStore", msCount).put("files", fileCount).put("saf", safCount)
                .put("folders", folders().size)
                .put("skipped", skipped).put("skippedExt", skippedExt.joinToString("/") { it.uppercase() })
                .put("allFiles", hasAllFiles())
            try { Kv.set(app, "scan_report", lastReport.toString()) } catch (_: Exception) {}
            try { Kv.set(app, "audio_fp", audioFingerprint()) } catch (_: Exception) {}
            return result.size
        } finally {
            scanning = false
        }
    }

    /** Cheap signature of the audio part of MediaStore (+ all-files state): changes when audio is added/removed/edited. */
    fun audioFingerprint(): String {
        val collection = if (Build.VERSION.SDK_INT >= 29)
            MediaStore.Audio.Media.getContentUri(MediaStore.VOLUME_EXTERNAL) else MediaStore.Audio.Media.EXTERNAL_CONTENT_URI
        val col = if (Build.VERSION.SDK_INT >= 30) MediaStore.MediaColumns.GENERATION_MODIFIED else MediaStore.MediaColumns.DATE_MODIFIED
        var n = 0; var max = 0L; var sum = 0L
        try {
            app.contentResolver.query(collection, arrayOf(MediaStore.MediaColumns._ID, col), null, null, null)?.use { c ->
                while (c.moveToNext()) { n++; val g = c.getLong(1); if (g > max) max = g; sum += c.getLong(0) }
            }
        } catch (_: Exception) { return "err" }
        val ver = if (Build.VERSION.SDK_INT >= 30) try { MediaStore.getVersion(app) } catch (_: Exception) { "" } else ""
        return "$ver|$n|$max|$sum|${hasAllFiles()}"
    }

    fun storedFingerprint(): String? = try { Kv.get(app, "audio_fp") } catch (_: Exception) { null }

    private fun mediaStorePaths(): Set<String> {
        val out = HashSet<String>()
        val collection = if (Build.VERSION.SDK_INT >= 29)
            MediaStore.Audio.Media.getContentUri(MediaStore.VOLUME_EXTERNAL) else MediaStore.Audio.Media.EXTERNAL_CONTENT_URI
        app.contentResolver.query(collection, arrayOf(MediaStore.Audio.Media.DATA), null, null, null)?.use { c ->
            while (c.moveToNext()) c.getString(0)?.let { out.add(it) }
        }
        return out
    }

    private fun walk(dirs: List<File>, deadline: Long, out: MutableList<File>) {
        val stack = ArrayDeque<Pair<File, Int>>()
        dirs.forEach { stack.add(it to 0) }
        val seenDirs = HashSet<String>(); val seenFiles = HashSet<String>()
        var visited = 0
        while (stack.isNotEmpty() && visited < 40000 && System.currentTimeMillis() < deadline) {
            val (d, depth) = stack.removeLast()
            val canon = try { d.canonicalPath } catch (_: Exception) { d.absolutePath }
            if (!seenDirs.add(canon)) continue
            val list = try { d.listFiles() } catch (_: Exception) { null } ?: continue
            for (f in list) {
                visited++
                val name = f.name
                if (name.startsWith(".")) continue
                if (f.isDirectory) { if (depth < 6 && name != "Android") stack.add(f to depth + 1); continue }
                val ext = extOf(name)
                if ((ext in playableExt || ext in unsupportedExt) && seenFiles.add(f.absolutePath)) out.add(f)
            }
        }
    }

    private class ProbeMeta(val title: String?, val artist: String?, val album: String?, val duration: Long, val trackNo: Int)

    private fun probe(uri: Uri): ProbeMeta? {
        val mmr = MediaMetadataRetriever()
        return try {
            if (uri.scheme == "file") mmr.setDataSource(uri.path) else mmr.setDataSource(app, uri)
            ProbeMeta(
                mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_TITLE),
                mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ARTIST)
                    ?: mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ALBUMARTIST),
                mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ALBUM),
                mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull() ?: 0L,
                mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_CD_TRACK_NUMBER)
                    ?.substringBefore('/')?.trim()?.toIntOrNull() ?: 0,
            )
        } catch (e: Exception) {
            Log.w(TAG, "probe failed $uri"); null
        } finally { try { mmr.release() } catch (_: Exception) {} }
    }

    private fun folderOf(path: String) = path.substringBeforeLast('/', "").removePrefix("/storage/emulated/0").ifEmpty { "/" }

    /** .webm is often a video: only keep audio-only ones (no video track). */
    private fun isVideo(setSrc: (MediaMetadataRetriever) -> Unit): Boolean {
        val mmr = MediaMetadataRetriever()
        return try { setSrc(mmr); mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_HAS_VIDEO) == "yes" }
        catch (_: Exception) { false } finally { try { mmr.release() } catch (_: Exception) {} }
    }

    private fun fileTrack(f: File): Track? {
        val uri = Uri.fromFile(f)
        if (extOf(f.name) == "webm" && isVideo { it.setDataSource(f.absolutePath) }) return null
        val m = probe(uri)
        val dur = m?.duration ?: 0L
        if (dur in 1..4999) return null
        val alb = cleanAlbum(m?.album)
        return Track(
            id = "f" + Integer.toHexString(f.absolutePath.hashCode()) + java.lang.Long.toHexString(f.length()),
            uri = uri.toString(), title = cleanTitle(m?.title, f.name), artist = cleanArtist(m?.artist),
            album = alb, albumId = -(alb.hashCode().toLong() and 0xffffffffL) - 1, duration = dur,
            path = f.absolutePath, folder = folderOf(f.absolutePath), dateAdded = f.lastModified(), trackNo = m?.trackNo ?: 0,
            size = f.length(), lrcUri = "", fileName = f.name,
        )
    }

    /** Returns tracks + list of skipped (unsupported) extensions, one entry per skipped file. */
    private fun scanMediaStore(progress: (String) -> Unit): Pair<List<Track>, List<String>> {
        val out = ArrayList<Track>()
        val skipped = ArrayList<String>()
        val seenIds = HashSet<Long>()
        val collection = if (Build.VERSION.SDK_INT >= 29)
            MediaStore.Audio.Media.getContentUri(MediaStore.VOLUME_EXTERNAL) else MediaStore.Audio.Media.EXTERNAL_CONTENT_URI
        val proj = arrayOf(
            MediaStore.Audio.Media._ID, MediaStore.Audio.Media.TITLE, MediaStore.Audio.Media.ARTIST,
            MediaStore.Audio.Media.ALBUM, MediaStore.Audio.Media.ALBUM_ID, MediaStore.Audio.Media.DURATION,
            MediaStore.Audio.Media.DATA, MediaStore.Audio.Media.DATE_ADDED, MediaStore.Audio.Media.TRACK,
            MediaStore.Audio.Media.SIZE, MediaStore.Audio.Media.DISPLAY_NAME, MediaStore.Audio.Media.MIME_TYPE,
            MediaStore.Audio.Media.IS_MUSIC, MediaStore.Audio.Media.IS_RINGTONE,
            MediaStore.Audio.Media.IS_NOTIFICATION, MediaStore.Audio.Media.IS_ALARM,
        )
        val needProbe = ArrayList<Int>() // indexes into out with unknown duration
        app.contentResolver.query(collection, proj, null, null, null)?.use { c ->
            while (c.moveToNext()) {
                val id = c.getLong(0)
                val path = c.getString(6) ?: ""
                val name = c.getString(10) ?: path.substringAfterLast('/')
                val mime = c.getString(11) ?: ""
                val ext = extOf(name.ifEmpty { path })
                if (ext in unsupportedExt || mime == "audio/x-ms-wma" || mime == "audio/ape") { skipped.add(ext.ifEmpty { "wma" }); continue }
                val isMusic = c.getInt(12) != 0
                val sysSound = c.getInt(13) != 0 || c.getInt(14) != 0 || c.getInt(15) != 0
                if (sysSound && !isMusic) continue
                val lp = path.lowercase()
                if (!isMusic && (lp.contains("/ringtones/") || lp.contains("/notifications/") || lp.contains("/alarms/") || lp.contains("/android/media/"))) continue
                val dur = c.getLong(5)
                if (dur in 1..4999) continue // clips / UI sounds
                seenIds.add(id)
                val uri = ContentUris.withAppendedId(MediaStore.Audio.Media.EXTERNAL_CONTENT_URI, id)
                if (dur <= 0) needProbe.add(out.size)
                out.add(Track(
                    id = "m$id", uri = uri.toString(),
                    title = cleanTitle(c.getString(1), name),
                    artist = cleanArtist(c.getString(2)),
                    album = cleanAlbum(c.getString(3)),
                    albumId = c.getLong(4), duration = dur, path = path, folder = folderOf(path),
                    dateAdded = c.getLong(7) * 1000, trackNo = c.getInt(8) % 1000,
                    size = c.getLong(9), lrcUri = "", fileName = name,
                ))
            }
        }

        // Files whose MIME MediaStore didn't classify as audio (only visible with 所有文件访问 on Android 11+).
        if (Build.VERSION.SDK_INT >= 29) {
            try {
                val files = MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL)
                val exts = playableExt.toList()
                val sel = "${MediaStore.Files.FileColumns.MEDIA_TYPE} != ${MediaStore.Files.FileColumns.MEDIA_TYPE_AUDIO} AND (" +
                    exts.joinToString(" OR ") { "${MediaStore.MediaColumns.DISPLAY_NAME} LIKE ?" } + ")"
                val args = exts.map { "%.$it" }.toTypedArray()
                app.contentResolver.query(files, arrayOf(MediaStore.MediaColumns._ID, MediaStore.MediaColumns.DATA,
                        MediaStore.MediaColumns.DISPLAY_NAME), sel, args, null)?.use { c ->
                    while (c.moveToNext()) {
                        val id = c.getLong(0); if (!seenIds.add(id)) continue
                        val path = c.getString(1) ?: continue
                        val f = File(path)
                        if (!f.canRead()) continue
                        fileTrack(f)?.let { out.add(it) }
                    }
                }
            } catch (e: Exception) { Log.w(TAG, "files query failed", e) }
        }

        // Fill in unknown durations (e.g. a FLAC that DownloadManager registered before the scanner read it).
        val deadline = System.currentTimeMillis() + 20_000
        for ((n, i) in needProbe.withIndex()) {
            if (System.currentTimeMillis() > deadline) break
            if (n % 10 == 0) progress("正在读取歌曲信息 ${n + 1}/${needProbe.size}…")
            val t = out[i]
            val m = probe(Uri.parse(t.uri)) ?: continue
            out[i] = t.copy(
                duration = m.duration,
                title = if (t.title == cleanTitle(null, t.path.substringAfterLast('/'))) cleanTitle(m.title, t.path.substringAfterLast('/')) else t.title,
                artist = if (t.artist == "未知歌手") cleanArtist(m.artist) else t.artist,
                album = if (t.album == "未知专辑") cleanAlbum(m.album) else t.album,
            )
        }
        return out to skipped
    }

    private fun scanTree(tree: Uri, known: Set<String>, progress: (String) -> Unit): List<Track> {
        val out = ArrayList<Track>()
        val rootDoc = DocumentsContract.getTreeDocumentId(tree)
        val label = folderLabel(tree.toString())
        val stack = ArrayDeque<Pair<String, String>>() // docId, display folder
        stack.add(rootDoc to label)
        var count = 0
        while (stack.isNotEmpty()) {
            val (docId, folderName) = stack.removeLast()
            val children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, docId)
            val files = ArrayList<Array<Any?>>()
            app.contentResolver.query(children, arrayOf(
                DocumentsContract.Document.COLUMN_DOCUMENT_ID, DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                DocumentsContract.Document.COLUMN_MIME_TYPE, DocumentsContract.Document.COLUMN_SIZE,
                DocumentsContract.Document.COLUMN_LAST_MODIFIED,
            ), null, null, null)?.use { c ->
                while (c.moveToNext()) {
                    files.add(arrayOf(c.getString(0), c.getString(1) ?: "", c.getString(2) ?: "", c.getLong(3), c.getLong(4)))
                }
            }
            val lrcByBase = HashMap<String, String>()
            for (f in files) {
                val name = f[1] as String
                if (name.endsWith(".lrc", true)) {
                    lrcByBase[name.substringBeforeLast('.').lowercase()] =
                        DocumentsContract.buildDocumentUriUsingTree(tree, f[0] as String).toString()
                }
            }
            for (f in files) {
                val cid = f[0] as String; val name = f[1] as String; val mime = f[2] as String
                if (mime == DocumentsContract.Document.MIME_TYPE_DIR) {
                    if (!name.startsWith(".")) stack.add(cid to "$folderName/$name"); continue
                }
                val ext = name.substringAfterLast('.', "").lowercase()
                if (ext in unsupportedExt || mime == "audio/x-ms-wma") continue
                if (!(mime.startsWith("audio/") || ext in playableExt)) continue
                if (known.contains(name + "|" + f[3])) continue
                val docUri = DocumentsContract.buildDocumentUriUsingTree(tree, cid)
                if (ext == "webm" && isVideo { it.setDataSource(app, docUri) }) continue
                count++
                if (count % 20 == 0) progress("已读取 $count 首（$label）…")
                val mmr = MediaMetadataRetriever()
                var title: String? = null; var artist: String? = null; var album: String? = null
                var dur = 0L; var trackNo = 0
                try {
                    mmr.setDataSource(app, docUri)
                    title = mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_TITLE)
                    artist = mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ARTIST)
                        ?: mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ALBUMARTIST)
                    album = mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ALBUM)
                    dur = mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull() ?: 0
                    trackNo = mmr.extractMetadata(MediaMetadataRetriever.METADATA_KEY_CD_TRACK_NUMBER)
                        ?.substringBefore('/')?.trim()?.toIntOrNull() ?: 0
                } catch (e: Exception) {
                    Log.w(TAG, "mmr failed $name")
                } finally { try { mmr.release() } catch (_: Exception) {} }
                if (dur in 1..9999) continue
                val alb = cleanAlbum(album)
                out.add(Track(
                    id = "s" + Integer.toHexString(docUri.toString().hashCode()) + java.lang.Long.toHexString(f[3] as Long),
                    uri = docUri.toString(), title = cleanTitle(title, name), artist = cleanArtist(artist),
                    album = alb, albumId = -(alb.hashCode().toLong() and 0xffffffffL) - 1, duration = dur,
                    path = "", folder = folderName, dateAdded = f[4] as Long, trackNo = trackNo,
                    size = f[3] as Long, lrcUri = lrcByBase[name.substringBeforeLast('.').lowercase()] ?: "", fileName = name,
                ))
            }
        }
        return out
    }

    private fun cleanTitle(t: String?, fileName: String): String {
        val s = t?.trim().orEmpty()
        return if (s.isEmpty() || s == "<unknown>") fileName.substringBeforeLast('.') else s
    }
    private fun cleanArtist(a: String?): String {
        val s = a?.trim().orEmpty()
        return if (s.isEmpty() || s == "<unknown>") "未知歌手" else s
    }
    private fun cleanAlbum(a: String?): String {
        val s = a?.trim().orEmpty()
        return if (s.isEmpty() || s == "<unknown>") "未知专辑" else s
    }

    // ---- media items ----
    fun mediaItem(raw: Track): MediaItem {
        val t = display(raw)
        val md = MediaMetadata.Builder()
            .setTitle(t.title).setArtist(t.artist).setAlbumTitle(t.album)
            .setDisplayTitle(t.title).setSubtitle(t.artist)
            .setArtworkUri(Uri.parse("myplayer-cover://track/${t.id}"))
            .setIsPlayable(true).setIsBrowsable(false)
            .build()
        return MediaItem.Builder().setMediaId(t.id).setUri(Uri.parse(t.uri)).setMediaMetadata(md).build()
    }

    fun resolve(item: MediaItem): MediaItem? {
        if (item.localConfiguration != null && item.mediaMetadata.title != null) return item
        val t = get(item.mediaId) ?: return null
        return mediaItem(t)
    }
}
