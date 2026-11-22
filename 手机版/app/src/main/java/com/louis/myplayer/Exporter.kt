package com.louis.myplayer

import android.content.Context
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

/**
 * 「导出歌单和改名到拾音文件夹」(2.2, default on): keeps 内部存储/Music/拾音/拾音数据.json (+ custom covers in
 * .拾音封面/) up to date, so copying the 拾音 folder to a PC by USB cable carries playlists, 我喜欢, 改过的歌名
 * and 自定义封面. The PC merges it on scan like a phone import. Format: shiyin-sync-protocol.md「USB 数据文件」.
 * Audio files are never touched. Needs write access to Music/拾音 (所有文件访问 on Android 11+).
 */
object Exporter {
    private const val TAG = "Exporter"
    const val FILE_NAME = "拾音数据.json"
    const val COVER_DIR = ".拾音封面"
    private val exec = Executors.newSingleThreadScheduledExecutor()
    private var pending: ScheduledFuture<*>? = null
    @Volatile var lastError = ""; private set
    @Volatile var lastWritten = 0L; private set

    fun enabled(ctx: Context): Boolean = Kv.get(ctx, "exportData")?.trim() != "false"

    /** Debounced (2 s): call after anything the file contains changed. */
    @Synchronized
    fun schedule(ctx: Context, delayMs: Long = 2000) {
        val app = ctx.applicationContext
        if (!enabled(app)) return
        pending?.cancel(false)
        pending = exec.schedule({ try { export(app) } catch (e: Throwable) { lastError = e.message ?: "导出失败"; Log.w(TAG, "export", e) } }, delayMs, TimeUnit.MILLISECONDS)
    }

    fun status(ctx: Context): JSONObject = JSONObject().put("on", enabled(ctx)).put("error", lastError).put("at", lastWritten)
        .put("path", "内部存储/Music/拾音/$FILE_NAME")

    /** Builds the document; [coverOut] receives fileName → source file for the covers to copy. */
    fun build(ctx: Context, tracks: List<Track>, coverOut: MutableMap<String, File>): JSONObject {
        val byKey = LinkedHashMap<String, Track>()
        for (t in tracks) byKey.putIfAbsent(t.key, t)
        val entries = JSONObject()
        for ((key, t) in byKey) {
            val o = JSONObject()
            Meta.get(key)?.let { m ->
                for (f in arrayOf("title", "artist", "album")) m.optString(f).takeIf { it.isNotEmpty() }?.let { o.put(f, it) }
                if (o.length() > 0) o.put("src", m.optString("src").ifEmpty { "manual" })
            }
            val cf = Covers.customFile(ctx, t)
            if (cf.exists() && cf.length() > 0) {
                o.put("cover", cf.name).put("coverVer", cf.lastModified())
                coverOut[cf.name] = cf
            }
            if (o.length() > 0) entries.put(key, o)
        }
        val pls = JSONArray()
        try {
            val p = JSONArray(Kv.get(ctx, "playlists") ?: "[]")
            for (n in 0 until p.length()) {
                val pl = p.getJSONObject(n)
                val ids = pl.optJSONArray("ids") ?: JSONArray()
                val keys = JSONArray()
                for (j in 0 until ids.length()) Library.get(ids.optString(j))?.let { keys.put(it.key) }
                pls.put(JSONObject().put("name", pl.optString("name")).put("fav", pl.optString("id") == "fav").put("keys", keys))
            }
        } catch (_: Exception) {}
        return JSONObject().put("app", "shiyin").put("kind", "shiyin-data").put("format", 1)
            .put("device", SyncServer.deviceName()).put("version", BuildConfig.VERSION_NAME)
            .put("tracks", entries).put("playlists", pls)
    }

    @Synchronized
    fun export(ctx: Context) {
        if (!enabled(ctx)) return
        val tracks = Library.tracks
        if (tracks.isEmpty()) return // not scanned yet: don't overwrite a good file with an empty one
        val home = Library.homeDir()
        val covers = LinkedHashMap<String, File>()
        val doc = build(ctx, tracks, covers)
        try {
            if (!home.isDirectory && !home.mkdirs()) throw Exception("无法创建 ${home.path}")
            val target = File(home, FILE_NAME)
            // the file content without the timestamp decides whether anything changed
            val body = doc.toString(1)
            val old = try { if (target.exists()) JSONObject(target.readText()).apply { remove("updated") }.toString(1) else "" } catch (_: Exception) { "" }
            val cdir = File(home, COVER_DIR)
            if (covers.isNotEmpty()) cdir.mkdirs()
            for ((name, src) in covers) {
                val dst = File(cdir, name)
                if (dst.exists() && dst.length() == src.length() && dst.lastModified() >= src.lastModified()) continue
                val tmp = File(cdir, "$name.tmp")
                src.copyTo(tmp, overwrite = true)
                if (!tmp.renameTo(dst)) { dst.delete(); tmp.renameTo(dst) }
            }
            // drop covers we no longer reference (only our own md5-named jpgs)
            cdir.listFiles()?.forEach { f -> if (f.name.matches(Regex("[0-9a-f]{32}\\.jpg")) && f.name !in covers) f.delete() }
            if (cdir.isDirectory && (cdir.list()?.isEmpty() == true)) cdir.delete()
            if (body != old) {
                doc.put("updated", System.currentTimeMillis())
                val tmp = File(home, "$FILE_NAME.tmp")
                tmp.writeText(doc.toString(1))
                if (!tmp.renameTo(target)) { target.writeText(doc.toString(1)); tmp.delete() }
            }
            lastWritten = System.currentTimeMillis(); lastError = ""
        } catch (e: Exception) {
            lastError = if (e is SecurityException || e.message?.contains("EACCES") == true || e.message?.contains("Permission") == true)
                "没有写入权限：请在 设置 → 所有文件访问 里开启" else (e.message ?: "导出失败")
            throw e
        }
    }
}
