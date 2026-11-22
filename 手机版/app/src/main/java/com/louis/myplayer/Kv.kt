package com.louis.myplayer

import android.content.Context
import java.io.File

/** Tiny persistent key/value store: one file per key in filesDir/kv. */
object Kv {
    private fun dir(ctx: Context): File = File(ctx.filesDir, "kv").apply { mkdirs() }
    private fun safe(key: String) = key.replace(Regex("[^A-Za-z0-9_.-]"), "_")

    @Synchronized
    fun get(ctx: Context, key: String): String? {
        val f = File(dir(ctx), safe(key))
        return if (f.exists()) f.readText() else null
    }

    @Synchronized
    fun set(ctx: Context, key: String, value: String?) {
        val f = File(dir(ctx), safe(key))
        if (value == null) { f.delete() } else {
            val tmp = File(dir(ctx), safe(key) + ".tmp")
            tmp.writeText(value)
            if (!tmp.renameTo(f)) { f.writeText(value); tmp.delete() }
        }
        if (key == "playlists" || key == "exportData") Exporter.schedule(ctx)
    }
}
