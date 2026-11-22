package com.louis.myplayer

import android.content.Context
import org.json.JSONObject
import java.io.File

/**
 * User edits per song, keyed by [Track.key] (survives moves / rescans):
 * {title, artist, album} display overrides, plus "tried" = time an automatic online lookup ran.
 * Stored in filesDir/meta.json. Audio files are never modified.
 */
object Meta {
    private var map: JSONObject? = null
    private lateinit var file: File

    @Synchronized
    private fun m(): JSONObject {
        map?.let { return it }
        file = File(Library.app.filesDir, "meta.json")
        val o = try { if (file.exists()) JSONObject(file.readText()) else JSONObject() } catch (_: Exception) { JSONObject() }
        map = o
        return o
    }

    @Synchronized
    fun get(key: String): JSONObject? = m().optJSONObject(key)

    @Synchronized
    fun set(key: String, title: String?, artist: String?, album: String?, src: String) {
        val o = m().optJSONObject(key) ?: JSONObject()
        if (title != null) o.put("title", title.trim())
        if (artist != null) o.put("artist", artist.trim())
        if (album != null) o.put("album", album.trim())
        o.put("src", src).put("at", System.currentTimeMillis())
        m().put(key, o); save()
    }

    @Synchronized
    fun markTried(key: String) {
        val o = m().optJSONObject(key) ?: JSONObject()
        o.put("tried", System.currentTimeMillis()); m().put(key, o); save()
    }

    @Synchronized
    fun clear(key: String) { m().remove(key); save() }

    /** Removes every automatic (online) override; manual edits stay. Returns how many were removed. */
    @Synchronized
    fun clearAuto(): Int {
        val o = m(); var n = 0
        for (k in o.keys().asSequence().toList()) {
            val e = o.optJSONObject(k) ?: continue
            if (e.optString("src") == "online") { o.remove(k); n++ } else if (e.has("tried")) { e.remove("tried") }
        }
        save(); return n
    }

    private fun save() {
        try { Exporter.schedule(Library.app) } catch (_: Exception) {}
        val tmp = File(file.parentFile, "meta.json.tmp")
        tmp.writeText(m().toString())
        if (!tmp.renameTo(file)) { file.writeText(m().toString()); tmp.delete() }
    }
}
